package httpapi

import (
	"net/http"
	"regexp"
	"slices"
	"strings"

	"agentflow/internal/jenkins"
	"agentflow/internal/repo"

	"github.com/gin-gonic/gin"
)

type jenkinsHandlers struct{ *deps }

var jenkinsKey = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,79}$`)

func (h *jenkinsHandlers) connections(c *gin.Context) {
	if authRequired() && !h.isPlatformAdmin(c.Request) {
		c.JSON(http.StatusForbidden, gin.H{"error": "只有平台管理员可以管理 Jenkins 连接"})
		return
	}
	if c.Request.Method == http.MethodPut {
		var input struct {
			repo.JenkinsConnection
			Secret string `json:"token"`
		}
		if !decode(c, &input) {
			return
		}
		item := input.JenkinsConnection
		item.Token = strings.TrimSpace(input.Secret)
		item.Name, item.URL, item.Username = strings.TrimSpace(item.Name), strings.TrimRight(strings.TrimSpace(item.URL), "/"), strings.TrimSpace(item.Username)
		if !jenkinsKey.MatchString(item.ID) || item.Name == "" || len([]rune(item.Name)) > 120 ||
			item.Username == "" || len(item.Username) > 160 || strings.ContainsAny(item.Username, ":\r\n") ||
			len(item.URL) > 500 || len(item.Token) > 300 || len(item.ProjectIDs) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请填写有效的连接标识、名称、Jenkins 地址、账号及至少一个授权项目"})
			return
		}
		if err := jenkins.ValidateURL(item.URL); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		for _, projectID := range item.ProjectIDs {
			project, ok := h.repo.Project(projectID)
			if !ok || project.ArchivedAt != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "授权项目不存在或已归档"})
				return
			}
		}
		if err := h.repo.SaveJenkinsConnection(item); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无法保存连接。新增连接或修改地址、账号时需要填写 API Token"})
			return
		}
	}
	items, err := h.repo.JenkinsConnections()
	if err != nil {
		respond(c, err)
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *jenkinsHandlers) projectAllowed(c *gin.Context, ownerOnly bool) bool {
	p, ok := h.repo.Project(c.Param("id"))
	if !ok || p.ArchivedAt != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "项目不存在或已归档"})
		return false
	}
	if !authRequired() || h.isPlatformAdmin(c.Request) {
		return true
	}
	user, signedIn := h.requestUser(c.Request, true)
	if signedIn && (p.OwnerID == user.ID || !ownerOnly && h.projectMemberWrites(p.ID, user.ID)) {
		return true
	}
	c.JSON(http.StatusForbidden, gin.H{"error": "没有管理或使用该项目 Jenkins 配置的权限"})
	return false
}

func (h *jenkinsHandlers) projectConnections(c *gin.Context) {
	if !h.projectAllowed(c, true) {
		return
	}
	items, err := h.repo.JenkinsConnections()
	if err != nil {
		respond(c, err)
		return
	}
	type option struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	out := []option{}
	for _, item := range items {
		if slices.Contains(item.ProjectIDs, c.Param("id")) {
			out = append(out, option{item.ID, item.Name})
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *jenkinsHandlers) authorizedJobs(c *gin.Context, connectionID string) ([]string, bool) {
	item, err := h.repo.JenkinsController(connectionID)
	if err != nil || !slices.Contains(item.ProjectIDs, c.Param("id")) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Jenkins 连接不存在或未授权给当前项目"})
		return nil, false
	}
	jobs, err := (jenkins.Client{BaseURL: item.URL, Username: item.Username, Token: item.Token}).Jobs(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return nil, false
	}
	return jobs, true
}

func (h *jenkinsHandlers) jobs(c *gin.Context) {
	if !h.projectAllowed(c, true) {
		return
	}
	if items, ok := h.authorizedJobs(c, c.Query("connectionId")); ok {
		c.JSON(http.StatusOK, items)
	}
}

func (h *jenkinsHandlers) bindings(c *gin.Context) {
	if !h.projectAllowed(c, c.Request.Method != http.MethodGet) {
		return
	}
	projectID := c.Param("id")
	switch c.Request.Method {
	case http.MethodPut:
		var item repo.JenkinsBinding
		if !decode(c, &item) {
			return
		}
		item.ProjectID = projectID
		item.Service, item.Environment = strings.TrimSpace(item.Service), strings.TrimSpace(item.Environment)
		item.Parameter = strings.TrimSpace(item.Parameter)
		if !jenkinsKey.MatchString(item.ID) || item.Service == "" || len([]rune(item.Service)) > 120 ||
			item.Environment == "" || len([]rune(item.Environment)) > 80 || len([]rune(item.Job)) > 500 ||
			(item.Parameter != "" && !jenkinsKey.MatchString(item.Parameter)) || len([]rune(item.Value)) > 240 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请填写有效的绑定标识、服务、环境、Job 和可选参数"})
			return
		}
		jobs, ok := h.authorizedJobs(c, item.ConnectionID)
		if !ok {
			return
		}
		if !slices.Contains(jobs, item.Job) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Job 不在该连接可见的可构建列表中"})
			return
		}
		if err := h.repo.SaveJenkinsBinding(item); err != nil {
			respond(c, err)
			return
		}
	case http.MethodDelete:
		if err := h.repo.DeleteJenkinsBinding(projectID, c.Param("binding")); err != nil {
			respond(c, err)
			return
		}
	}
	items, err := h.repo.JenkinsBindings(projectID)
	if err != nil {
		respond(c, err)
		return
	}
	c.JSON(http.StatusOK, items)
}
