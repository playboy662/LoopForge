package gitwork

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var (
	httpsHostLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
	allDigits      = regexp.MustCompile(`^[0-9]+$`)

	errHTTPSHost = errors.New("请输入 HTTPS Git 主机名，可附带端口，例如 github.com 或 git.example.org:8443")
)

// HTTPSHostScope normalizes the host a personal HTTPS credential is bound to.
// The result has the form Git passes to a credential helper as "host": a
// lower-case ASCII host name, with the port only when it is not 443. A
// leading "https://" and a trailing slash are accepted; any other scheme, a
// path, user info, wildcards and IP literals are rejected so a credential can
// only ever match one named host.
func HTTPSHostScope(raw string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if scheme, rest, ok := strings.Cut(s, "://"); ok {
		if scheme != "https" {
			return "", errHTTPSHost
		}
		s = rest
	}
	s = strings.TrimSuffix(s, "/")
	if s == "" || strings.ContainsAny(s, "/@?#*%[] \t\r\n") {
		return "", errHTTPSHost
	}
	host, port := s, ""
	if strings.Contains(s, ":") {
		h, p, err := net.SplitHostPort(s)
		if err != nil {
			return "", errHTTPSHost
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != p {
			return "", errHTTPSHost
		}
		host = h
		if n != 443 {
			port = p
		}
	}
	if !validHTTPSHostName(host) {
		return "", errHTTPSHost
	}
	if port == "" {
		return host, nil
	}
	return host + ":" + port, nil
}

func validHTTPSHostName(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return false
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if len(label) > 63 || !httpsHostLabel.MatchString(label) {
			return false
		}
	}
	return !allDigits.MatchString(labels[len(labels)-1])
}

// HTTPSRemoteHost returns the credential host scope of an https:// remote URL.
func HTTPSRemoteHost(remote string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(remote))
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" {
		return "", false
	}
	host, err := HTTPSHostScope(u.Host)
	if err != nil {
		return "", false
	}
	return host, true
}

// HTTPGitAllowlistEnv is the server-side opt-in for specific plaintext Git endpoints.
const HTTPGitAllowlistEnv = "AGENTFLOW_GIT_HTTP_ALLOWED_HOSTS"

// GitCredentialScope keeps plaintext credentials distinct from HTTPS credentials.
func GitCredentialScope(protocol, authority string) (string, error) {
	switch protocol {
	case "https":
		return HTTPSHostScope(authority)
	case "http":
		authority = strings.ToLower(authority)
		host, port, err := net.SplitHostPort(authority)
		number, portErr := strconv.Atoi(port)
		ip, ipErr := netip.ParseAddr(host)
		if err == nil && portErr == nil && number > 0 && number <= 65535 &&
			strconv.Itoa(number) == port && (validHTTPSHostName(host) || ipErr == nil && ip.Is4()) {
			return "http://" + authority, nil
		}
	}
	return "", errors.New("HTTP Git 必须指定放行的主机和端口")
}

// ConfiguredGitCredentialScope is used when saving or loading credentials on the platform.
func ConfiguredGitCredentialScope(raw string) (string, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	authority, plain := strings.CutPrefix(strings.TrimSuffix(raw, "/"), "http://")
	if !plain {
		return HTTPSHostScope(raw)
	}
	scope, err := GitCredentialScope("http", authority)
	if err != nil {
		return "", err
	}
	for _, allowed := range strings.Split(os.Getenv(HTTPGitAllowlistEnv), ",") {
		if authority == strings.ToLower(strings.TrimSpace(allowed)) {
			return scope, nil
		}
	}
	return "", errors.New("此 HTTP Git 主机和端口未经管理员放行")
}
