package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"agentflow/internal/domain"
	"agentflow/internal/gitwork"
)

func TestUnknownHTTPSRepositoryNeedsNoHostingAdapter(t *testing.T) {
	f := cvBNewHTTPFixture(t)
	resp := cvBRequest(t, f, http.MethodPost, "/api/repositories", map[string]string{"projectId": "platform", "repoUrl": "https://unfamiliar.example.org/team/code.git"}, true)
	var r domain.Repository
	if resp.Code != http.StatusOK || json.Unmarshal(resp.Body.Bytes(), &r) != nil || r.Provider != domain.RepositoryProviderHTTPS || r.CloneURL != "https://unfamiliar.example.org/team/code.git" || r.GongfengProjectID != "" {
		t.Fatalf("generic HTTPS repository: HTTP %d %s", resp.Code, resp.Body.String())
	}
	for _, address := range []string{"https://user:token@unfamiliar.example.org/team/code", "https://unfamiliar.example.org/team/code?token=x", "https://unfamiliar.example.org/team/../code"} {
		if response := cvBRequest(t, f, http.MethodPost, "/api/repositories", map[string]string{"projectId": "platform", "repoUrl": address}, true); response.Code != http.StatusBadRequest {
			t.Fatalf("unsafe URL accepted: %s", address)
		}
	}
}

func TestPlainHTTPRepositoryRequiresConfiguredHostAndPort(t *testing.T) {
	f := cvBNewHTTPFixture(t)
	const remote = "http://192.0.2.10:9433/team/code.git"
	register := func(url string) int {
		return cvBRequest(t, f, http.MethodPost, "/api/repositories",
			map[string]string{"projectId": "platform", "repoUrl": url}, true).Code
	}
	t.Setenv(gitwork.HTTPGitAllowlistEnv, "")
	if status := register(remote); status != http.StatusBadRequest {
		t.Fatalf("unconfigured endpoint: HTTP %d", status)
	}
	t.Setenv(gitwork.HTTPGitAllowlistEnv, "192.0.2.10:9433")
	if status := register(remote); status != http.StatusOK {
		t.Fatalf("configured endpoint: HTTP %d", status)
	}
	if status := register("http://192.0.2.10:9434/team/code.git"); status != http.StatusBadRequest {
		t.Fatalf("different port: HTTP %d", status)
	}
	for _, clone := range []string{remote, "http://192.0.2.10:9434/team/code.git"} {
		response := cvBRequest(t, f, http.MethodPost, "/api/repositories", map[string]string{
			"projectId": "platform", "repoUrl": "https://github.com/team/code", "cloneUrl": clone,
		}, true)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("mismatched cloneUrl: HTTP %d %s", response.Code, response.Body.String())
		}
	}
}
