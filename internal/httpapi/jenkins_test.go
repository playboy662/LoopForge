package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"agentflow/internal/domain"
)

func TestJenkinsConfigurationAndSOPDispatch(t *testing.T) {
	t.Setenv("AGENTFLOW_AUTH_REQUIRED", "true")
	f := cvBNewHTTPFixture(t)
	t.Setenv("AGENTFLOW_PLATFORM_ADMINS", f.user.LoginName)
	const fakeToken = "jenkins-fixture-token"
	var triggers atomic.Int32
	var dropResponse atomic.Bool
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, token, ok := r.BasicAuth()
		if !ok || user != "delivery" || token != fakeToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/ci/api/json":
			fmt.Fprint(w, `{"jobs":[{"name":"team","jobs":[{"name":"deploy","buildable":true},{"name":"disabled","buildable":false}]}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/ci/job/team/job/deploy/api/json":
			fmt.Fprint(w, `{"buildable":true,"property":[{"parameterDefinitions":[{"name":"BRANCH","required":true},{"name":"ENV"}]}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/ci/job/team/job/disabled/api/json":
			fmt.Fprint(w, `{"buildable":false}`)
		case r.Method == http.MethodPost && r.URL.Path == "/ci/job/team/job/deploy/buildWithParameters":
			triggers.Add(1)
			if dropResponse.Load() {
				if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
					_ = conn.Close()
				}
				return
			}
			if r.FormValue("BRANCH") != "test" || r.FormValue("ENV") != "qa" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Location", "/ci/queue/item/42/")
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer controller.Close()
	const projectID = "platform"
	const base = "/api/projects/" + projectID + "/jenkins"
	request := func(method, path string, body any, want int) string {
		t.Helper()
		res := cvBRequest(t, f, method, path, body, true)
		if res.Code != want {
			t.Fatalf("%s %s: HTTP %d %s", method, path, res.Code, res.Body.String())
		}
		if strings.Contains(res.Body.String(), fakeToken) {
			t.Fatal("Jenkins token leaked in API response")
		}
		return strings.TrimSpace(res.Body.String())
	}
	if response := cvBRequest(t, f, http.MethodGet, "/api/platform/jenkins", nil, false); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated Jenkins management: HTTP %d", response.Code)
	}
	request(http.MethodPut, "/api/platform/jenkins", map[string]any{
		"id": "controller", "name": "Jenkins", "url": controller.URL + "/ci",
		"username": "delivery", "token": fakeToken, "allProjects": true,
	}, http.StatusOK)
	if options := request(http.MethodGet, base+"/connections", nil, http.StatusOK); options != `[{"id":"controller","name":"Jenkins"}]` {
		t.Fatalf("connections = %s", options)
	}
	if jobs := request(http.MethodGet, base+"/jobs?connectionId=controller", nil, http.StatusOK); jobs != `["team/deploy"]` {
		t.Fatalf("jobs = %s", jobs)
	}
	if err := f.s.PutProject(domain.Project{ID: "other-project", Name: "Other"}); err != nil {
		t.Fatal(err)
	}
	for _, repository := range []domain.Repository{
		{ID: "own-repo", ProjectID: projectID, Name: "后端仓库"},
		{ID: "other-repo", ProjectID: "other-project", Name: "其他仓库"},
	} {
		if err := f.s.PutRepository(repository); err != nil {
			t.Fatal(err)
		}
	}
	binding := map[string]any{"id": "deploy-test", "connectionId": "controller", "service": "后端", "environment": "测试", "job": "team/disabled"}
	for _, repositoryID := range []string{"", "other-repo"} {
		binding["repositoryId"] = repositoryID
		if body := request(http.MethodPut, base+"/bindings", binding, http.StatusBadRequest); !strings.Contains(body, "请选择当前项目的代码仓库") {
			t.Fatalf("invalid repository accepted or wrong error: %s", body)
		}
	}
	binding["repositoryId"] = "own-repo"
	request(http.MethodPut, base+"/bindings", binding, http.StatusBadRequest)
	binding["job"] = "team/deploy"
	request(http.MethodPut, base+"/bindings", binding, http.StatusBadRequest)
	binding["parameters"] = map[string]string{"BRANCH": "test", "ENV": "qa"}
	if body := request(http.MethodPut, base+"/bindings", binding, http.StatusOK); !strings.Contains(body, `"repositoryName":"后端仓库"`) || !strings.Contains(body, `"connectionName":"Jenkins"`) {
		t.Fatalf("binding labels missing: %s", body)
	}

	wf := domain.WorkflowDefinition{
		ID: "jenkins-smoke", ProjectID: projectID, Name: "发布测试", SandboxProfileID: "local",
		Nodes: []domain.WorkflowNode{{ID: "deploy", Name: "触发 Jenkins", Type: "jenkins", JenkinsBindingID: "deploy-test"}},
	}
	if err := f.s.PutWorkflow(wf); err != nil {
		t.Fatal(err)
	}
	start := func(workflowID string) domain.Run {
		t.Helper()
		run, err := f.e.StartWithExecutionContext(workflowID, "发布", domain.RepositoryContext{}, domain.TelemetryContext{ProjectID: projectID, ResponsibleUserID: f.user.ID})
		if err != nil {
			t.Fatal(err)
		}
		return run
	}
	wait := func(runID, status string) domain.Run {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		var current domain.Run
		for time.Now().Before(deadline) {
			current, _ = f.s.RunState(runID)
			if current.Status == status {
				return current
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("SOP %s wanted %s, got %s, node %+v", runID, status, current.Status, current.Nodes["deploy"])
		return current
	}
	run := start(wf.ID)
	result := wait(run.ID, "succeeded").Nodes["deploy"]
	if result.JenkinsURL != controller.URL+"/ci/queue/item/42/" || !result.JenkinsQueued || triggers.Load() != 1 {
		t.Fatalf("Jenkins result = %+v; calls = %d", result, triggers.Load())
	}
	dropResponse.Store(true)
	run = start(wf.ID)
	result = wait(run.ID, "failed").Nodes["deploy"]
	if !strings.Contains(result.Error, "接单状态待确认") || triggers.Load() != 2 {
		t.Fatalf("ambiguous result = %+v; calls = %d", result, triggers.Load())
	}
	if err := f.e.Retry(run.ID, "deploy", "", ""); err != nil {
		t.Fatal(err)
	}
	result = wait(run.ID, "failed").Nodes["deploy"]
	if result.Attempt != 2 || !strings.Contains(result.Error, "接单状态待确认") || triggers.Load() != 2 {
		t.Fatalf("retry repeated Jenkins request: %+v; calls = %d", result, triggers.Load())
	}
	parent := domain.WorkflowDefinition{
		ID: "jenkins-parent", ProjectID: projectID, Name: "Jenkins 子流程", SandboxProfileID: "local",
		Nodes: []domain.WorkflowNode{{ID: "child", Name: "发布子流程", Type: "sop", SOPWorkflowID: wf.ID}},
	}
	if err := f.s.PutWorkflow(parent); err != nil {
		t.Fatal(err)
	}
	run = start(parent.ID)
	firstChild := wait(run.ID, "failed").Nodes["child"].ChildRunID
	if firstChild == "" || triggers.Load() != 3 {
		t.Fatalf("child %q, calls = %d", firstChild, triggers.Load())
	}
	if err := f.e.Retry(run.ID, "child", "", ""); err != nil {
		t.Fatal(err)
	}
	secondChild := wait(run.ID, "failed").Nodes["child"].ChildRunID
	if secondChild == "" || secondChild == firstChild || triggers.Load() != 3 {
		t.Fatalf("child replay: before=%q after=%q calls=%d", firstChild, secondChild, triggers.Load())
	}

	gated := wf
	gated.ID = "jenkins-with-approval"
	gated.Nodes = append([]domain.WorkflowNode{{ID: "approve", Name: "审批", Type: "approval"}}, wf.Nodes...)
	gated.Edges = []domain.WorkflowEdge{{ID: "release-after-approval", Source: "approve", Target: "deploy", When: "success"}}
	if err := f.s.PutWorkflow(gated); err != nil {
		t.Fatal(err)
	}
	run = start(gated.ID)
	wait(run.ID, "waiting")
	binding["parameters"] = map[string]string{"BRANCH": "test", "ENV": "changed"}
	request(http.MethodPut, base+"/bindings", binding, http.StatusOK)
	if err := f.e.Approve(run.ID, "approve", ""); err != nil {
		t.Fatal(err)
	}
	result = wait(run.ID, "failed").Nodes["deploy"]
	if !strings.Contains(result.Error, "目标未冻结或已变更") || triggers.Load() != 3 {
		t.Fatalf("edited target was submitted: %+v; calls = %d", result, triggers.Load())
	}
	run = start(gated.ID)
	wait(run.ID, "waiting")
	request(http.MethodPut, "/api/platform/jenkins", map[string]any{
		"id": "controller", "name": "Jenkins", "url": controller.URL + "/ci",
		"username": "delivery", "projectIds": []string{},
	}, http.StatusOK)
	if list := request(http.MethodGet, base+"/bindings", nil, http.StatusOK); !strings.Contains(list, `"unavailable":true`) {
		t.Fatalf("revoked binding was not marked unavailable: %s", list)
	}
	if err := f.e.Approve(run.ID, "approve", ""); err != nil {
		t.Fatal(err)
	}
	result = wait(run.ID, "failed").Nodes["deploy"]
	if !strings.Contains(result.Error, "未获该 Jenkins 连接授权") || triggers.Load() != 3 {
		t.Fatalf("revoked target was submitted: %+v; calls = %d", result, triggers.Load())
	}
	request(http.MethodPut, "/api/platform/jenkins", map[string]any{
		"id": "controller", "name": "Jenkins", "url": controller.URL + "/ci",
		"username": "delivery", "allProjects": true,
	}, http.StatusOK)
	run = start(gated.ID)
	wait(run.ID, "waiting")
	if err := f.s.DeleteRepository("own-repo"); err != nil {
		t.Fatal(err)
	}
	if body := request(http.MethodGet, base+"/bindings", nil, http.StatusOK); !strings.Contains(body, `"unavailable":true`) {
		t.Fatalf("binding to deleted repository was not marked unavailable: %s", body)
	}
	if err := f.e.Approve(run.ID, "approve", ""); err != nil {
		t.Fatal(err)
	}
	result = wait(run.ID, "failed").Nodes["deploy"]
	if !strings.Contains(result.Error, "代码仓库已不属于当前项目") || triggers.Load() != 3 {
		t.Fatalf("target for deleted repository was submitted: %+v; calls = %d", result, triggers.Load())
	}
}
