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

const JobListDepth = 8

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
		u.User != nil || u.ForceQuery || strings.Contains(raw, "#") || u.RawQuery != "" || u.Opaque != "" {
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

// CheckConnection exercises the same read operations used to select a job.
func (c Client) CheckConnection(ctx context.Context) (int, error) {
	res, err := c.request(ctx, http.MethodGet, "/whoAmI/api/json", nil)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 0, readStatusError("验证账号", res.StatusCode)
	}
	var identity struct {
		Authenticated bool
		Anonymous     bool
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&identity); err != nil {
		return 0, errors.New("Jenkins 返回的身份信息无效，请检查 Jenkins 地址")
	}
	if !identity.Authenticated || identity.Anonymous {
		return 0, errors.New("Jenkins 未识别服务账号，请检查账号和 API Token")
	}
	jobs, err := c.Jobs(ctx)
	return len(jobs), err
}

func readStatusError(action string, status int) error {
	switch status {
	case http.StatusUnauthorized:
		return errors.New("Jenkins 认证失败（HTTP 401），请检查账号和 API Token")
	case http.StatusForbidden:
		return fmt.Errorf("Jenkins %s被拒绝（HTTP 403），请检查账号的读取权限", action)
	case http.StatusNotFound:
		return fmt.Errorf("Jenkins %s失败（HTTP 404），请检查地址和路径", action)
	default:
		return fmt.Errorf("Jenkins %s失败（HTTP %d）", action, status)
	}
}

func (c Client) Jobs(ctx context.Context) ([]string, error) {
	tree := "jobs[name,_class,buildable]"
	for range JobListDepth - 1 {
		tree = "jobs[name,_class,buildable," + tree + "]"
	}
	res, err := c.request(ctx, http.MethodGet, "/api/json?"+url.Values{"tree": {tree}}.Encode(), nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, readStatusError("读取 Job 列表", res.StatusCode)
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

type JobConfiguration struct {
	Parameters []string `json:"parameters"`
	Required   []string `json:"required"`
}

func (c Client) JobConfiguration(ctx context.Context, name string) (JobConfiguration, error) {
	var out JobConfiguration
	path, err := jobPath(name)
	if err != nil {
		return out, err
	}
	query := url.Values{"tree": {"buildable,property[parameterDefinitions[name,required,defaultParameterValue[name]]]"}}
	res, err := c.request(ctx, http.MethodGet, path+"/api/json?"+query.Encode(), nil)
	if err != nil {
		return out, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return out, fmt.Errorf("Jenkins 读取 Job 参数失败（HTTP %d）", res.StatusCode)
	}
	var job struct {
		Buildable bool
		Property  []struct {
			ParameterDefinitions []struct {
				Name                  string
				Required              bool
				DefaultParameterValue *struct{ Name string }
			}
		}
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&job); err != nil || !job.Buildable {
		return out, errors.New("Jenkins Job 不可构建或参数定义无效")
	}
	out.Parameters, out.Required = []string{}, []string{}
	for _, property := range job.Property {
		for _, definition := range property.ParameterDefinitions {
			out.Parameters = append(out.Parameters, definition.Name)
			if definition.Required && definition.DefaultParameterValue == nil {
				out.Required = append(out.Required, definition.Name)
			}
		}
	}
	return out, nil
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

var queuePath = regexp.MustCompile(`^queue/item/[0-9]+/?$`)

type TriggerResult struct {
	URL      string
	Queued   bool
	Rejected bool
}

func (c Client) Trigger(ctx context.Context, fullName string, parameters map[string]string, parameterized bool) (TriggerResult, error) {
	path, err := jobPath(fullName)
	if err != nil {
		return TriggerResult{Rejected: true}, err
	}
	var form url.Values
	action := "/build"
	if parameterized || len(parameters) > 0 {
		form = url.Values{}
		for name, value := range parameters {
			form.Set(name, value)
		}
		action = "/buildWithParameters"
	}
	res, err := c.request(ctx, http.MethodPost, path+action, form)
	if err != nil {
		return TriggerResult{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		switch res.StatusCode {
		case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusUnprocessableEntity:
			return TriggerResult{Rejected: true}, fmt.Errorf("Jenkins 拒绝请求（HTTP %d），请核对权限、Job 和参数", res.StatusCode)
		default:
			return TriggerResult{}, fmt.Errorf("Jenkins 未确认接单（HTTP %d）", res.StatusCode)
		}
	}
	base, _ := url.Parse(strings.TrimRight(c.BaseURL, "/") + "/")
	if location, err := base.Parse(res.Header.Get("Location")); err == nil &&
		location.Scheme == base.Scheme && location.Host == base.Host && location.User == nil &&
		strings.HasPrefix(location.Path, base.Path) && queuePath.MatchString(strings.TrimPrefix(location.Path, base.Path)) {
		location.RawQuery, location.Fragment = "", ""
		return TriggerResult{URL: location.String(), Queued: true}, nil
	}
	return TriggerResult{URL: strings.TrimRight(c.BaseURL, "/") + path + "/"}, nil
}
