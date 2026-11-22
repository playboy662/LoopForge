// Package jenkins implements the controller API used by release steps.
package jenkins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type Client struct {
	BaseURL  string
	Username string
	Token    string
}

var httpClient = &http.Client{
	Timeout:       15 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return errors.New("请输入不含凭据、查询参数或片段的 Jenkins 地址")
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." || strings.Contains(part, "\\") {
			return errors.New("Jenkins 地址路径无效")
		}
	}
	return nil
}

func (c Client) request(ctx context.Context, method, suffix string, form url.Values) (*http.Response, error) {
	if err := ValidateURL(c.BaseURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+suffix, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, errors.New("无法创建 Jenkins 请求")
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.SetBasicAuth(c.Username, c.Token)
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, errors.New("无法连接 Jenkins，请检查网络与 TLS 配置")
	}
	return res, nil
}

type listedJob struct {
	Name      string      `json:"name"`
	Class     string      `json:"_class"`
	Buildable bool        `json:"buildable"`
	Jobs      []listedJob `json:"jobs"`
}

func (c Client) Jobs(ctx context.Context) ([]string, error) {
	tree := "jobs[name,_class,buildable,jobs[name,_class,buildable,jobs[name,_class,buildable,jobs[name,_class,buildable]]]]"
	res, err := c.request(ctx, http.MethodGet, "/api/json?"+url.Values{"tree": {tree}}.Encode(), nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Jenkins 读取 Job 列表失败（HTTP %d）", res.StatusCode)
	}
	var root listedJob
	if err := json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&root); err != nil {
		return nil, errors.New("Jenkins 返回的 Job 列表无效或过大")
	}
	jobs := []string{}
	var collect func(string, []listedJob)
	collect = func(prefix string, items []listedJob) {
		for _, item := range items {
			if !validSegment(item.Name) {
				continue
			}
			fullName := prefix + item.Name
			if item.Buildable && !strings.Contains(item.Class, "MultiBranch") && !strings.Contains(item.Class, "OrganizationFolder") {
				jobs = append(jobs, fullName)
			}
			collect(fullName+"/", item.Jobs)
		}
	}
	collect("", root.Jobs)
	return jobs, nil
}

func validSegment(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, "/\\\r\n\x00")
}

func jobPath(fullName string) (string, error) {
	var path strings.Builder
	for _, segment := range strings.Split(fullName, "/") {
		if !validSegment(segment) {
			return "", errors.New("Jenkins Job 路径无效")
		}
		path.WriteString("/job/")
		path.WriteString(url.PathEscape(segment))
	}
	return path.String(), nil
}

var queuePath = regexp.MustCompile(`/queue/item/[0-9]+/?$`)

func (c Client) Trigger(ctx context.Context, fullName, parameter, value string) (string, error) {
	path, err := jobPath(fullName)
	if err != nil {
		return "", err
	}
	var form url.Values
	action := "/build"
	if parameter != "" {
		form = url.Values{parameter: {value}}
		action = "/buildWithParameters"
	}
	res, err := c.request(ctx, http.MethodPost, path+action, form)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("Jenkins 未确认接单（HTTP %d），请核对权限、Job 和参数", res.StatusCode)
	}
	base, _ := url.Parse(strings.TrimRight(c.BaseURL, "/") + "/")
	if location, err := base.Parse(res.Header.Get("Location")); err == nil &&
		location.Scheme == base.Scheme && location.Host == base.Host && location.User == nil &&
		strings.HasPrefix(location.Path, base.Path) && queuePath.MatchString(location.Path) {
		location.RawQuery, location.Fragment = "", ""
		return location.String(), nil
	}
	return strings.TrimRight(c.BaseURL, "/") + path + "/", nil
}
