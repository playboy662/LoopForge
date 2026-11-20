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

func TestConfiguredAnalysisModel(t *testing.T) {
	user := domain.User{CodexAPIKey: "codex-key", ClaudeCodeAPIKey: "claude-key", AgentCLICredentials: map[string]domain.AgentCLICredential{"pi": {APIKey: "pi-key"}}}
	for _, id := range []string{"codex:gpt-custom", "claude-code:sonnet", "pi:custom/model"} {
		got, err := configuredAnalysisModel(id, user)
		if err != nil || got != id {
			t.Fatalf("model %q = %q, %v", id, got, err)
		}
	}
	if got, err := configuredAnalysisModel("", user); err != nil || got != "codex:default" {
		t.Fatalf("default = %q, %v", got, err)
	}
	for _, id := range []string{"codex:", "codex:bad model"} {
		if _, err := configuredAnalysisModel(id, user); err == nil {
			t.Fatalf("invalid %q accepted", id)
		}
	}
	if _, err := configuredAnalysisModel("default", user); err == nil || !strings.Contains(err.Error(), "CodeBuddy") {
		t.Fatalf("missing CodeBuddy key: %v", err)
	}
	if _, err := configuredAnalysisModel("codex:gpt", domain.User{CodeBuddyAPIKey: "cb-key"}); err == nil || !strings.Contains(err.Error(), "Codex") {
		t.Fatalf("missing Codex key: %v", err)
	}
}

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
		args, _ := json.Marshal(map[string]any{"path": path})
		if _, err := readClarifyRepository(context.Background(), root, args); err == nil {
			t.Fatalf("out of scope %q accepted", path)
		}
	}
	for _, args := range []string{`{"path":"handler.go"}`, `{"query":"Login"}`} {
		text, err := readClarifyRepository(context.Background(), root, json.RawMessage(args))
		if err != nil || !strings.Contains(text, "handler.go:2: func Login") {
			t.Fatalf("read=%q err=%v", text, err)
		}
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
