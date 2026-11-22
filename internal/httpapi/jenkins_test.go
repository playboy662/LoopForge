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
	t.Setenv("AGENTFLOW_AUTH_REQUIRED", "false")
	f := cvBNewHTTPFixture(t)
	const fakeToken = "jenkins-fixture-token"
	var triggers atomic.Int32
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, token, ok := r.BasicAuth()
		if !ok || user != "delivery" || token != fakeToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/ci/api/json":
			fmt.Fprint(w, `{"jobs":[{"name":"team","jobs":[{"name":"deploy","buildable":true},{"name":"disabled","buildable":false}]}]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/ci/job/team/job/deploy/buildWithParameters":
			triggers.Add(1)
			if r.FormValue("BRANCH") != "test" {
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
	request(http.MethodPut, "/api/platform/jenkins", map[string]any{
		"id": "controller", "name": "Jenkins", "url": controller.URL + "/ci",
		"username": "delivery", "token": fakeToken, "projectIds": []string{projectID},
	}, http.StatusOK)
	if jobs := request(http.MethodGet, base+"/jobs?connectionId=controller", nil, http.StatusOK); jobs != `["team/deploy"]` {
		t.Fatalf("jobs = %s", jobs)
	}
	binding := map[string]string{"id": "deploy-test", "connectionId": "controller", "service": "后端", "environment": "测试", "job": "team/disabled"}
	request(http.MethodPut, base+"/bindings", binding, http.StatusBadRequest)
	binding["job"], binding["parameter"], binding["value"] = "team/deploy", "BRANCH", "test"
	request(http.MethodPut, base+"/bindings", binding, http.StatusOK)

	wf := domain.WorkflowDefinition{
		ID: "jenkins-smoke", ProjectID: projectID, Name: "发布测试", SandboxProfileID: "local",
		Nodes: []domain.WorkflowNode{{ID: "deploy", Name: "触发 Jenkins", Type: "jenkins", JenkinsBindingID: "deploy-test"}},
	}
	if err := f.s.PutWorkflow(wf); err != nil {
		t.Fatal(err)
	}
	run, err := f.e.StartWithExecutionContext(wf.ID, "发布", domain.RepositoryContext{}, domain.TelemetryContext{ProjectID: projectID})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if current, ok := f.s.RunState(run.ID); ok {
			n := current.Nodes["deploy"]
			if n.Status == "failed" {
				t.Fatal(n.Error)
			}
			if n.Status == "succeeded" {
				if n.JenkinsURL != controller.URL+"/ci/queue/item/42/" || triggers.Load() != 1 {
					t.Fatalf("Jenkins result = %+v; calls = %d", n, triggers.Load())
				}
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("SOP did not reach Jenkins")
}
