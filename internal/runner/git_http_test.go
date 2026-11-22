package runner

import (
	"context"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"agentflow/internal/domain"
	"agentflow/internal/gitwork"
	"agentflow/internal/runtime"
)

func TestMain(m *testing.M) {
	if runtime.RunMCPCallCLI(os.Args[1:]) || runtime.RunGitHelperCLI(os.Args[1:]) {
		return
	}
	os.Exit(m.Run())
}

func TestRunnerPlainHTTPGitCloneFetchAndPush(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	runGit := func(dir string, env map[string]string, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir, cmd.Env = dir, gitwork.Env("")
		for key, value := range env {
			cmd.Env = gitwork.WithEnvValue(cmd.Env, key, value)
		}
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", args[0], err, output)
		}
	}
	runGit(root, nil, "init", "--bare", "--initial-branch=main", "code.git")
	runGit(root, nil, "-C", "code.git", "config", "http.receivepack", "true")
	runGit(root, nil, "init", "--initial-branch=main", "seed")
	runGit(root, nil, "-C", "seed", "-c", "user.name=Test", "-c", "user.email=test@example.org", "commit", "--allow-empty", "-m", "seed")
	runGit(root, nil, "-C", "seed", "push", filepath.Join(root, "code.git"), "main")
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	backend := &cgi.Handler{Path: git, Args: []string{"http-backend"}, Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, token, _ := r.BasicAuth(); user != "runner" || token != "fake-plain-token" {
			w.Header().Set("WWW-Authenticate", `Basic realm="Git test"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		backend.ServeHTTP(w, r)
	}))
	defer server.Close()
	remote := server.URL + "/code.git"
	credential := domain.GitHTTPExecutionCredential{
		GitHTTPCredential: domain.GitHTTPCredential{Host: server.URL, Username: "runner"},
		Token:             "fake-plain-token",
	}
	workspace := t.TempDir()
	env, closeRelay, err := runtime.RunGitEnvironment(ctx, nil, workspace, runtime.RunCredentials{
		GitHTTPCredentials: map[string]domain.GitHTTPExecutionCredential{server.URL: credential},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeRelay()
	runGit(workspace, env, "clone", remote, "checkout")
	checkout := filepath.Join(workspace, "checkout")
	runGit(checkout, env, "fetch", "origin", "main")
	runGit(checkout, env, "push", "origin", "HEAD:refs/heads/http-test")
	runGit(root, nil, "-C", "code.git", "show-ref", "--verify", "refs/heads/http-test")
}

func TestRunnerGitHelperUsesInvocationHostCredentials(t *testing.T) {
	inv := &domain.RunnerInvocation{GitHTTPCredentials: map[string]domain.GitHTTPExecutionCredential{
		"unfamiliar.example.org": {GitHTTPCredential: domain.GitHTTPCredential{Host: "unfamiliar.example.org", Username: "required-user"}, Token: "fake-runner-token"},
	}, Repository: &domain.RunnerRepository{GitToken: "obsolete-token-must-not-answer"}}
	workspace := t.TempDir()
	env, closeRelay, err := runtime.RunGitEnvironment(context.Background(), nil, workspace, invocationGitCredentials(inv))
	if err != nil {
		t.Fatal(err)
	}
	defer closeRelay()
	for _, host := range []string{"unfamiliar.example.org", "other.example.org"} {
		cmd := exec.Command("git", "credential", "fill")
		cmd.Dir, cmd.Env = workspace, os.Environ()
		for k, v := range env {
			cmd.Env = gitwork.WithEnvValue(cmd.Env, k, v)
		}
		cmd.Stdin = strings.NewReader("protocol=https\nhost=" + host + "\n\n")
		out, err := cmd.CombinedOutput()
		if host == "unfamiliar.example.org" {
			if err != nil || !strings.Contains(string(out), "username=required-user\npassword=fake-runner-token") {
				t.Fatalf("configured host failed: %v %s", err, out)
			}
		} else if err == nil || strings.Contains(string(out), "password=") {
			t.Fatal("unconfigured host received a credential")
		}
	}
	for k, v := range env {
		if strings.Contains(v, "fake-runner-token") || strings.Contains(v, "obsolete-token-must-not-answer") {
			t.Fatalf("credential leaked into %s", k)
		}
	}
}
