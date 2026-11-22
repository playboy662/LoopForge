package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentflow/internal/domain"
	"agentflow/internal/repo"
	"agentflow/internal/runtime"
)

func TestClarifyProgressPreservesStreamMetadata(t *testing.T) {
	var events []assistantStreamEvent
	progress := clarifyProgress(func(name string, payload any) {
		if name != "thought" {
			t.Fatalf("event=%s", name)
		}
		events = append(events, payload.(assistantStreamEvent))
	})
	for _, event := range []assistantStreamEvent{
		{Kind: "thinkingStart"}, {Kind: "thinking", Text: "我先"},
		{Kind: "answer", Text: " 读取原单", Full: true},
		{Kind: "tool", ID: "read-1", Tool: "clarify_repository_read", Detail: "src/auth.go"},
		{Kind: "toolDone", ID: "read-1"}, {Kind: "draftDelta", Detail: "hidden"},
	} {
		progress(event)
	}
	if len(events) != 5 || events[0].Kind != "thinkingStart" || !events[2].Full || events[3].ID != "read-1" || events[3].Detail != "src/auth.go" {
		t.Fatalf("events=%+v", events)
	}
	if detail := clarifyToolDetail("clarify_repository_read", json.RawMessage(`{"path":"backend/src","query":"Login"}`)); detail != "Login @ backend/src" {
		t.Fatalf("repository read detail=%q", detail)
	}
	clarifyProgress(nil)(assistantStreamEvent{Kind: "thinkingStart"})
}

func TestRunEvalAcceptsConfiguredCLI(t *testing.T) {
	f := cvBNewHTTPFixture(t)
	if _, err := f.s.UpdateUserCredentials(f.user.ID, repo.UserCredentialUpdate{CodexAPIKey: "eval-key"}); err != nil {
		t.Fatal(err)
	}
	done := domain.Run{ID: "eval-codex-run", Status: "succeeded", WorkflowID: "missing", ExecutionContext: domain.TelemetryContext{ProjectID: "platform"}, Nodes: map[string]*domain.NodeRun{}}
	if err := f.s.PutRun(done); err != nil {
		t.Fatal(err)
	}
	called := make(chan string, 1)
	setRunEvalExecForTest(func(ctx context.Context, rec domain.RunEvalRecord, run domain.Run, user domain.User, emit func(string, any)) (domain.RunEvalResult, error) {
		called <- rec.Model
		return domain.RunEvalResult{Score: 80, Summary: "ok"}, nil
	})
	t.Cleanup(func() { setRunEvalExecForTest(nil) })
	response := cvBRequest(t, f, http.MethodPost, "/api/runs/eval-codex-run/eval", map[string]any{"model": "codex:gpt-custom"}, true)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	select {
	case id := <-called:
		if id != "codex:gpt-custom" {
			t.Fatalf("model=%s", id)
		}
	case <-time.After(time.Second):
		t.Fatal("evaluation did not start")
	}
}

func TestClarifyModelSessionSwitch(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	assessment := domain.ClarificationAssessment{SessionUserID: 2, SessionEngine: "codex"}
	root := clarifyModelSessionRoot(home, "codex", "s1")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeCodexThread(root, "s1", "thread-one"); err != nil {
		t.Fatal(err)
	}
	if !canResumeClarifyModelSession(assessment, "codex:other-model", home, "s1", 2) {
		t.Fatal("same CLI model switch must resume")
	}
	for _, model := range []string{"default", "claude-code:sonnet"} {
		if canResumeClarifyModelSession(assessment, model, home, "s1", 2) {
			t.Fatal("cross CLI session resumed")
		}
	}
	if canResumeClarifyModelSession(assessment, "codex:gpt", home, "s1", 3) {
		t.Fatal("different owner resumed")
	}
	if canResumeClarifyModelSession(assessment, "codex:gpt", home, "../../s1", 2) {
		t.Fatal("unsafe session accepted")
	}
}

func TestClarifyAlternateCLIUsesPersonalModel(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "fake-claude")
	script := `#!/bin/sh
cat >/dev/null
printf '%s\n' '{"type":"result","result":"{\"summary\":\"ok\",\"ready\":true}"}'
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTFLOW_CLAUDE_BIN", bin)
	config := filepath.Join(root, "mcp.json")
	if err := os.WriteFile(config, []byte(`{"mcpServers":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := &streamHandlers{}
	_, raw, sid, err := h.runClarifyTurn(ctx, root, bin, "claude-code:my-custom-model", "clarify", "run", "s1", filepath.Join(root, "home"), false, runtime.RunCredentials{}, nil, clarifyTurnOpts{Native: true, MCPConfig: config, User: domain.User{ClaudeCodeAPIKey: "claude-key"}})
	if err != nil || !strings.Contains(raw, "summary") || sid != "s1" {
		t.Fatalf("raw=%q sid=%q err=%v", raw, sid, err)
	}
	settings, err := os.ReadFile(filepath.Join(clarifyModelSessionRoot(filepath.Join(root, "home"), "claude-code", "s1"), "claude-settings.json"))
	if err != nil || !strings.Contains(string(settings), "Write") {
		t.Fatalf("settings=%s err=%v", settings, err)
	}
}

func TestClarifyRepositoryReadScope(t *testing.T) {
	root := t.TempDir()
	session := &AgentToolSession{Mode: AgentToolClarify, clarifyRepos: map[string]string{"fixture": root}}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("outside secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "handler.go"), []byte("package demo\nfunc Login() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../secret.txt", outside, "escape"} {
		args := map[string]any{"repository": "fixture", "path": path}
		if _, err := readClarifySource(session, "clarify_read_file", args); err == nil {
			t.Fatalf("out of scope %q accepted", path)
		}
	}
	text, err := readClarifySource(session, "clarify_read_file", map[string]any{"repository": "fixture", "path": "handler.go"})
	if err != nil || !strings.Contains(text, "func Login()") {
		t.Fatalf("read=%q err=%v", text, err)
	}
	if _, err := readClarifySource(session, "clarify_read_file", map[string]any{"repository": "outside", "path": "handler.go"}); err == nil {
		t.Fatal("unselected repository accepted")
	}
}

// The real CLI dispatcher must select Codex without needing a CodeBuddy key.
// The fake app-server speaks the native protocol and records thread parameters.
func TestAnalysisCodexExecution(t *testing.T) {
	for _, mode := range []string{"clarify", "eval"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			capture := filepath.Join(root, "request.json")
			answer := `{"summary":"ok","ready":true,"dimensions":{"control":80,"latency":80,"tools":80,"tokens":80,"recovery":80,"delivery":80}}`
			bin := filepath.Join(root, "fake-codex")
			script := fmt.Sprintf(`#!/usr/bin/env python3
import sys,json
capture=%q
answer=%q
def send(value):
 print(json.dumps(value),flush=True)
for line in sys.stdin:
 m=json.loads(line)
 method=m.get('method')
 if method=='initialize':
  send({'id':m['id'],'result':{}})
 elif method in ('thread/start','thread/resume'):
  with open(capture,'w') as f: json.dump(m['params'],f)
  send({'id':m['id'],'result':{'thread':{'id':'thread-test'}}})
 elif method=='turn/start':
  send({'id':m['id'],'result':{'turn':{'id':'turn-test'}}})
  send({'method':'item/completed','params':{'item':{'type':'agentMessage','id':'answer','text':answer}}})
  send({'method':'turn/completed','params':{'turn':{'id':'turn-test','status':'completed'}}})
`, capture, answer)
			if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("AGENTFLOW_CODEX_BIN", bin)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			user := domain.User{ID: 1, CodexAPIKey: "fake-key"}
			if mode == "clarify" {
				config := filepath.Join(root, "mcp.json")
				if err := os.WriteFile(config, []byte(`{"mcpServers":{}}`), 0600); err != nil {
					t.Fatal(err)
				}
				_, raw, _, err := (&streamHandlers{}).runClarifyTurn(ctx, root, bin, "codex:custom-model", "clarify", "r", "s1", filepath.Join(root, "home"), false, runtime.RunCredentials{}, nil, clarifyTurnOpts{Native: true, MCPConfig: config, User: user})
				if err != nil || !strings.Contains(raw, "summary") {
					t.Fatalf("raw=%q err=%v", raw, err)
				}
			} else {
				store, err := repo.OpenTest()
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				h := &runsHandlers{deps: &deps{repo: store}}
				result, err := h.executeRunEval(ctx, domain.RunEvalRecord{ID: "eval-native", RunID: "r", Model: "codex:custom-model"}, domain.Run{ID: "r", Nodes: map[string]*domain.NodeRun{}}, user, func(string, any) {})
				if err != nil || result.Summary != "ok" {
					t.Fatalf("result=%+v err=%v", result, err)
				}
			}
			data, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			var params map[string]any
			if err := json.Unmarshal(data, &params); err != nil {
				t.Fatal(err)
			}
			if params["model"] != "custom-model" || params["sandbox"] != "read-only" || params["approvalPolicy"] != "never" {
				t.Fatalf("thread parameters=%s", data)
			}
		})
	}
}

func TestChatClarifyKeepsConfiguredModel(t *testing.T) {
	called := make(chan string, 1)
	setClarifyExecForTest(func(ctx context.Context, issue domain.Issue, request chatClarifyRequest, emit func(string, any)) (any, string, map[string]any, error) {
		called <- request.Model
		parsed := map[string]any{"summary": "ok", "ready": true}
		return parsed, `{"summary":"ok","ready":true}`, nil, nil
	})
	t.Cleanup(func() { setClarifyExecForTest(nil) })
	store, h := clarifyTestServer(t)
	if err := store.PutIssue(domain.Issue{ID: "model-issue", ProjectID: "platform", Title: "model test"}); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"message":"分析需求","model":"codex:custom-model"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/issues/model-issue/chat-clarify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	select {
	case model := <-called:
		if model != "codex:custom-model" {
			t.Fatal(model)
		}
	default:
		t.Fatal("model was not dispatched")
	}
	issue, _ := store.Issue("model-issue")
	if issue.Clarification.Model != "codex:custom-model" {
		t.Fatalf("saved model=%s", issue.Clarification.Model)
	}
}

func TestChatClarifyRejectsInvalidModel(t *testing.T) {
	called := false
	setClarifyExecForTest(func(context.Context, domain.Issue, chatClarifyRequest, func(string, any)) (any, string, map[string]any, error) {
		called = true
		return map[string]any{}, "{}", map[string]any{}, nil
	})
	t.Cleanup(func() { setClarifyExecForTest(nil) })

	_, h := clarifyTestServer(t)
	body, _ := json.Marshal(map[string]any{"projectId": "platform", "message": "加个徽章", "model": "codex:"})
	req := httptest.NewRequest(http.MethodPost, "/api/chat-clarify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "请选择有效的 CLI 模型") {
		t.Fatalf("error does not explain invalid model: %s", rec.Body.String())
	}
	if called {
		t.Fatal("clarification started with an invalid model")
	}
}

func TestRunEvalRequiresSelectedCLICredentials(t *testing.T) {
	f := cvBNewHTTPFixture(t)
	if _, err := f.s.UpdateUserCredentials(f.user.ID, repo.UserCredentialUpdate{CodeBuddyAPIKey: "eval-key"}); err != nil {
		t.Fatal(err)
	}
	done := domain.Run{ID: "eval-codex-run", Status: "succeeded", WorkflowID: "missing",
		ExecutionContext: domain.TelemetryContext{ProjectID: "platform"}, Nodes: map[string]*domain.NodeRun{}}
	if err := f.s.PutRun(done); err != nil {
		t.Fatal(err)
	}
	rec := cvBRequest(t, f, http.MethodPost, "/api/runs/eval-codex-run/eval", map[string]any{"model": "codex:gpt-5.5"}, true)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Codex API Key") {
		t.Fatalf("status = %d body = %s, want 400 asking for the selected CLI credential", rec.Code, rec.Body.String())
	}
	if _, busy := f.s.ActiveRunEval(done.ID); busy {
		t.Fatal("a model without matching credentials must not start an eval")
	}
}

func TestRunEvalPreservesSelectedModelInChunkedRequest(t *testing.T) {
	f := cvBNewHTTPFixture(t)
	if _, err := f.s.UpdateUserCredentials(f.user.ID, repo.UserCredentialUpdate{CodexAPIKey: "eval-fixture"}); err != nil {
		t.Fatal(err)
	}
	run := domain.Run{ID: "eval-chunked", Status: "succeeded", WorkflowID: "missing", ExecutionContext: domain.TelemetryContext{ProjectID: "platform"}}
	if err := f.s.PutRun(run); err != nil {
		t.Fatal(err)
	}
	selected := make(chan string, 1)
	setRunEvalExecForTest(func(_ context.Context, rec domain.RunEvalRecord, _ domain.Run, user domain.User, _ func(string, any)) (domain.RunEvalResult, error) {
		if user.ID == f.user.ID && user.CodexAPIKey == "eval-fixture" {
			selected <- rec.Model
		}
		return domain.RunEvalResult{}, nil
	})
	t.Cleanup(func() { setRunEvalExecForTest(nil) })
	req := httptest.NewRequest(http.MethodPost, "/api/runs/eval-chunked/eval", strings.NewReader(`{"model":"codex:private-model"}`))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = -1
	req.TransferEncoding = []string{"chunked"}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, authedRequest(t, req, f.user))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	select {
	case model := <-selected:
		if model != "codex:private-model" {
			t.Fatalf("job received the wrong explicit model: %q", model)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("eval job did not receive the requesting account's credential")
	}
}
