package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"agentflow/internal/domain"
	"agentflow/internal/gitwork"
)

func TestProjectCredentialsReserveRuntimeSettings(t *testing.T) {
	reserved := slices.Concat(workspaceGitSecretEnvironment, agentCLIHostEnv, []string{
		"ANTHROPIC_BASE_URL", "CODEBUDDY_BASE_URL", "OPENAI_BASE_URL", "HTTPS_PROXY", "GIT_SSH_COMMAND", "PATH",
	})
	for _, name := range reserved {
		for _, spelling := range []string{name, strings.ToLower(name)} {
			if err := domain.ValidateProjectCredentialVariable(spelling); err == nil {
				t.Errorf("platform setting %s can be saved as a project credential", spelling)
			}
		}
	}
	for _, name := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "SERVICE_API_KEY", "PROJECT_GITHUB_TOKEN"} {
		if err := domain.ValidateProjectCredentialVariable(name); err != nil {
			t.Errorf("project variable %s is rejected: %v", name, err)
		}
	}
}

func TestProjectCredentialEnvironmentRejectsOccupiedNames(t *testing.T) {
	ctx := WithProjectCredentialEnv(t.Context(), map[string]string{"FIXTURE_SETTING": "fixture-value"})
	command := CommandSpec{environment: map[string]string{"FIXTURE_SETTING": ""}}
	if _, err := projectCommandEnvironment(ctx, command, nil); err == nil || !strings.Contains(err.Error(), "冲突") {
		t.Fatalf("empty command binding did not reserve its name: %v", err)
	}
	profile := map[string]string{"fixture_setting": ""}
	if _, err := projectCommandEnvironment(ctx, CommandSpec{}, profile); err == nil || !strings.Contains(err.Error(), "冲突") {
		t.Fatalf("case variant of a sandbox setting did not reserve its name: %v", err)
	}
}

func TestCodexToolEnvironmentScopesCredentialsByName(t *testing.T) {
	for _, tail := range [][]string{{"task"}, {"resume", "thread-1", "-"}, {"--stdio"}} {
		command := CommandSpec{
			Provider: "codex",
			Argv:     append([]string{"codex", "exec", "-c", `shell_environment_policy.inherit="core"`}, tail...),
			environment: map[string]string{
				SessionGatewayEnv:  "http://relay.invalid/private-session",
				"GIT_CONFIG_COUNT": "2", "GIT_CONFIG_VALUE_1": "!helper git-credential",
				"CODEX_API_KEY": "fixture-model-secret", "UNSELECTED_TOKEN": "fixture-host-secret",
			},
		}
		ctx := WithProjectCredentialEnv(t.Context(), map[string]string{"SERVICE_PASSWORD": "fixture-project-secret"})
		bound, err := projectCommandEnvironment(ctx, command, nil)
		if err != nil {
			t.Fatal(err)
		}
		var only string
		for _, arg := range bound.Argv {
			if strings.HasPrefix(arg, "shell_environment_policy.include_only=") {
				only = strings.TrimPrefix(arg, "shell_environment_policy.include_only=")
			}
		}
		var names []string
		if err := json.Unmarshal([]byte(only), &names); err != nil {
			t.Fatalf("missing tool environment allowlist: %v", err)
		}
		for _, name := range []string{SessionGatewayEnv, "GIT_CONFIG_COUNT", "GIT_CONFIG_VALUE_1", "GIT_EXEC_PATH", "SERVICE_PASSWORD", "PATH", "HOME"} {
			if !slices.Contains(names, name) {
				t.Errorf("tool shell lost selected binding %s", name)
			}
		}
		if slices.Contains(names, "CODEX_API_KEY") || slices.Contains(names, "UNSELECTED_TOKEN") {
			t.Fatal("tool shell can inherit model or unrelated host credentials")
		}
		joined := strings.Join(bound.Argv, " ")
		for _, value := range []string{"private-session", "fixture-model-secret", "fixture-project-secret", "fixture-host-secret"} {
			if strings.Contains(joined, value) {
				t.Fatal("credential value was copied into argv")
			}
		}
		if !slices.Equal(bound.Argv[len(bound.Argv)-len(tail):], tail) || !slices.Equal(command.Argv[len(command.Argv)-len(tail):], tail) {
			t.Fatal("shell policy changed resume/prompt ordering or mutated input")
		}
		gitOnly, err := projectCommandEnvironment(t.Context(), command, nil)
		if err != nil || !strings.Contains(strings.Join(gitOnly.Argv, " "), "GIT_CONFIG_VALUE_1") || strings.Contains(strings.Join(gitOnly.Argv, " "), "SERVICE_PASSWORD") {
			t.Fatal("Git relay requires a project credential or inherited an unselected binding")
		}
	}
	ssh := withCodexToolEnvironment(CommandSpec{Provider: "codex", Argv: []string{"codex", "exec", "task"}, environment: map[string]string{"GIT_SSH_COMMAND": "ssh -F bound-config"}}, nil)
	if !strings.Contains(strings.Join(ssh.Argv, " "), "SSH_AUTH_SOCK") {
		t.Fatal("Codex tool shell lost the command-bound SSH agent")
	}
	for _, command := range []CommandSpec{
		{Provider: "codex", Argv: []string{"codex", "exec", "task"}},
		{Provider: "codebuddy", Argv: []string{"codebuddy", "task"}, environment: map[string]string{SessionGatewayEnv: "relay"}},
	} {
		bound, err := projectCommandEnvironment(t.Context(), command, nil)
		if err != nil || !slices.Equal(bound.Argv, command.Argv) {
			t.Fatal("unbound or non-Codex command changed")
		}
	}
}

func TestLocalProjectCredentialEnvironment(t *testing.T) {
	t.Setenv("AGENTFLOW_PROJECT_CREDENTIAL_KEY", "platform-test-key")
	sandbox := &localSandbox{id: "project-credentials", workspace: t.TempDir()}
	assertProjectCredentialProcess(t, t.Context(), sandbox)
}

func TestSandboxGitPreparationExcludesProjectCredentialEnvironment(t *testing.T) {
	workspace := t.TempDir()
	git := []byte("#!/bin/sh\n" + `test -z "${PROJECT_TEST_TOKEN+x}" && test "$GIT_TERMINAL_PROMPT" = 0 || exit 1
printf git-probe
printf diagnostic >&2
`)
	if err := os.WriteFile(filepath.Join(workspace, "git"), git, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", workspace+string(os.PathListSeparator)+os.Getenv("PATH"))
	sandbox := &localSandbox{id: "project-credentials", workspace: workspace}
	ctx := WithProjectCredentialEnv(t.Context(), map[string]string{"PROJECT_TEST_TOKEN": "fixture-value"})
	ctx = WithSandboxGit(ctx, sandbox, RunCredentials{})
	out, err := gitwork.RunWorkspaceCommand(ctx, workspace, gitwork.WorkspaceCommand{Args: []string{"--version"}, StdoutOnly: true})
	if err != nil || out != "git-probe" {
		t.Fatalf("Git preparation = %q, %v", out, err)
	}
}

func TestDockerProjectCredentialEnvironment(t *testing.T) {
	ctx, sandbox := dockerScriptTestSandbox(t)
	assertProjectCredentialProcess(t, ctx, sandbox)
}

func TestConversationProcessUsesProjectCredentials(t *testing.T) {
	if !NativeSandboxAvailable(domain.SandboxDriverNative) {
		t.Skip("native sandbox unavailable")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	root := t.TempDir()
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "codebuddy")
	content := "#!/bin/sh\n" + `test -n "$CHAT_TEST_TOKEN" || exit 1
printf '{"type":"result","subtype":"success","is_error":false,"result":"%s"}\n' "$CHAT_TEST_TOKEN"
`
	if err := os.WriteFile(binary, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTFLOW_CODEBUDDY_BIN", binary)
	spec := &domain.RunnerSandboxSpec{Profile: domain.SandboxProfile{Driver: domain.SandboxDriverNative}}
	pool := &ConversationProcessPool{}
	const secret = "fixture-chat-value"
	started, err := pool.Handle(ctx, root, workspace, spec, ConversationProcessRequest{
		Operation: "start", Command: &ConversationCommand{
			Argv: []string{binary}, Provider: "codebuddy", ProjectCredentialEnv: map[string]string{"CHAT_TEST_TOKEN": secret},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	var after int64
	for {
		result, err := pool.Handle(ctx, root, "", nil, ConversationProcessRequest{Operation: "poll", ID: started.ID, After: after}, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range result.Events {
			output.WriteString(event.Message)
		}
		after = result.Sequence
		if result.Done {
			if result.Error != "" {
				t.Fatalf("conversation process: %s, output %q", result.Error, output.String())
			}
			break
		}
	}
	if !strings.Contains(output.String(), `"result":"[redacted]"`) || strings.Contains(output.String(), secret) {
		t.Fatal("conversation process did not read and redact the credential")
	}
}

func TestProjectCredentialRedactorPreservesJSON(t *testing.T) {
	mask := projectCredentialRedactor(map[string]string{"CREDENTIAL": `"`, "PIN": "839265"})
	got := mask(`{"type":"result","result":"\"","pin":839265}`)
	var event map[string]string
	if err := json.Unmarshal([]byte(got), &event); err != nil || event["type"] != "result" || event["result"] != "[redacted]" || event["pin"] != "[redacted]" {
		t.Fatalf("redacted event remains valid JSON: %v", err)
	}
}

func assertProjectCredentialProcess(t *testing.T, ctx context.Context, sandbox Sandbox) {
	t.Helper()
	const secret = "fixture-private-value"
	scoped := WithProjectCredentialEnv(ctx, map[string]string{"PROJECT_TEST_TOKEN": secret})
	command := CommandSpec{Provider: "script", Argv: []string{"sh", "-c", `test -n "$PROJECT_TEST_TOKEN" && test -z "$AGENTFLOW_PROJECT_CREDENTIAL_KEY" && printf '%s\n' "$PROJECT_TEST_TOKEN"`}}
	var logs []string
	got, err := sandbox.Exec(scoped, command, func(event domain.TelemetryEvent) { logs = append(logs, event.Message) })
	if err != nil || strings.TrimSpace(got) != "[redacted]" || strings.Contains(strings.Join(logs, "\n"), secret) {
		t.Fatalf("scoped execution did not read and redact the credential: %v", err)
	}
	plain := CommandSpec{Provider: "script", Argv: []string{"sh", "-c", `test -z "${PROJECT_TEST_TOKEN+x}" && printf clean`}}
	if got, err := sandbox.Exec(ctx, plain, nil); err != nil || strings.TrimSpace(got) != "clean" {
		t.Fatalf("an unbound command inherited project credentials: %v", err)
	}
}
