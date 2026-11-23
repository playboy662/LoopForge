package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"agentflow/internal/domain"
	"agentflow/internal/jenkins"
	"agentflow/internal/repo"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type jenkinsHandlers struct{ *deps }

var jenkinsKey = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,79}$`)

func (h *jenkinsHandlers) connections(c *gin.Context) {
	if !authRequired() || !h.isPlatformAdmin(c.Request) {
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
			len(item.URL) > 500 || len(item.Token) > 300 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请填写有效的连接标识、名称、Jenkins 地址和账号"})
			return
		}
		if err := jenkins.ValidateURL(item.URL); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if item.ProjectIDs == nil {
			item.ProjectIDs = []string{}
		}
		slices.Sort(item.ProjectIDs)
		item.ProjectIDs = slices.Compact(item.ProjectIDs)
		projectJSON, _ := json.Marshal(item.ProjectIDs)
		if len(projectJSON) > 4000 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "授权项目列表过长"})
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

func (h *jenkinsHandlers) testConnection(c *gin.Context) {
	if !authRequired() || !h.isPlatformAdmin(c.Request) {
		c.JSON(http.StatusForbidden, gin.H{"error": "只有平台管理员可以测试 Jenkins 连接"})
		return
	}
	var input struct {
		ID       string `json:"id"`
		URL      string `json:"url"`
		Username string `json:"username"`
		Token    string `json:"token"`
	}
	if !decodeLimit(c, &input, 4<<10) {
		return
	}
	address := strings.TrimRight(strings.TrimSpace(input.URL), "/")
	username, token := strings.TrimSpace(input.Username), strings.TrimSpace(input.Token)
	if len(address) > 500 || username == "" || len(username) > 160 || strings.ContainsAny(username, ":\r\n") ||
		len(token) > 300 || input.ID != "" && !jenkinsKey.MatchString(input.ID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写有效的 Jenkins 地址、账号和 API Token"})
		return
	}
	if err := jenkins.ValidateURL(address); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if token == "" && input.ID != "" {
		saved, err := h.repo.JenkinsController(input.ID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			respond(c, err)
			return
		}
		if err == nil && saved.URL == address && saved.Username == username {
			token = saved.Token
		}
	}
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "新增连接或修改地址、账号时需要填写 API Token"})
		return
	}
	client := jenkins.Client{BaseURL: address, Username: username, Token: token}
	count, err := client.CheckConnection(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"jobCount": count})
}

func (h *jenkinsHandlers) projectAllowed(c *gin.Context, ownerOnly bool) bool {
	p, ok := h.repo.Project(c.Param("id"))
	if !ok || p.ArchivedAt != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "项目不存在或已归档"})
		return false
	}
	if authRequired() && h.isPlatformAdmin(c.Request) {
		return true
	}
	user, signedIn := h.requestUser(c.Request, true)
	if authRequired() && signedIn && (p.OwnerID == user.ID || !ownerOnly && slices.ContainsFunc(p.Members, func(member domain.ProjectMember) bool {
		return member.UserID == user.ID && !strings.EqualFold(strings.TrimSpace(member.Role), "viewer")
	})) {
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
		if item.AllowsProject(c.Param("id")) && jenkins.ValidateURL(item.URL) == nil {
			out = append(out, option{item.ID, item.Name})
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *jenkinsHandlers) authorizedJenkins(c *gin.Context, connectionID string) (jenkins.Client, bool) {
	item, err := h.repo.JenkinsController(connectionID)
	if err != nil || !item.AllowsProject(c.Param("id")) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Jenkins 连接不存在或未授权给当前项目"})
		return jenkins.Client{}, false
	}
	return jenkins.Client{BaseURL: item.URL, Username: item.Username, Token: item.Token}, true
}

func (h *jenkinsHandlers) jobs(c *gin.Context) {
	if !h.projectAllowed(c, true) {
		return
	}
	client, ok := h.authorizedJenkins(c, c.Query("connectionId"))
	if !ok {
		return
	}
	if job := c.Query("job"); job != "" {
		config, err := client.JobConfiguration(c.Request.Context(), job)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, config)
		return
	}
	jobs, err := client.Jobs(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, jobs)
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
		item.RepositoryID = strings.TrimSpace(item.RepositoryID)
		item.Service, item.Environment = strings.TrimSpace(item.Service), strings.TrimSpace(item.Environment)
		item.Parameter = strings.TrimSpace(item.Parameter)
		if !jenkinsKey.MatchString(item.ID) || len([]rune(item.Service)) > 120 ||
			item.Environment == "" || len([]rune(item.Environment)) > 80 || len([]rune(item.Job)) > 500 ||
			(item.Parameter != "" && !jenkinsKey.MatchString(item.Parameter)) || len([]rune(item.Value)) > 240 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请填写有效的绑定标识、环境、Job 和可选参数"})
			return
		}
		repository, found := h.repo.Repository(item.RepositoryID)
		if item.RepositoryID == "" || !found || repository.ProjectID != projectID {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请选择当前项目的代码仓库"})
			return
		}
		client, ok := h.authorizedJenkins(c, item.ConnectionID)
		if !ok {
			return
		}
		config, err := client.JobConfiguration(c.Request.Context(), item.Job)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		item.Parameters = item.JobParameters()
		item.Parameter, item.Value = "", ""
		item.Parameterized = len(config.Parameters) > 0
		parameters, _ := json.Marshal(item.Parameters)
		if len(parameters) > 4000 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Job 参数配置过长"})
			return
		}
		for name, value := range item.Parameters {
			if !slices.Contains(config.Parameters, name) || len(value) > 1000 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Job 参数不存在或值过长"})
				return
			}
		}
		for _, name := range config.Required {
			if strings.TrimSpace(item.Parameters[name]) == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "请填写 Jenkins 必填参数 " + name})
				return
			}
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
	connections, err := h.repo.JenkinsConnections()
	if err != nil {
		respond(c, err)
		return
	}
	allowed := make(map[string]string, len(connections))
	for _, item := range connections {
		if item.AllowsProject(projectID) && jenkins.ValidateURL(item.URL) == nil {
			allowed[item.ID] = item.Name
		}
	}
	type bindingOption struct {
		repo.JenkinsBinding
		RepositoryName string `json:"repositoryName,omitempty"`
		ConnectionName string `json:"connectionName,omitempty"`
	}
	out := make([]bindingOption, 0, len(items))
	for _, item := range items {
		connectionName, available := allowed[item.ConnectionID]
		option := bindingOption{JenkinsBinding: item, ConnectionName: connectionName}
		option.Unavailable = !available
		if item.RepositoryID != "" {
			repository, found := h.repo.Repository(item.RepositoryID)
			if found && repository.ProjectID == projectID {
				option.RepositoryName = repository.Name
			} else {
				option.Unavailable = true
			}
		}
		out = append(out, option)
	}
	c.JSON(http.StatusOK, out)
}
