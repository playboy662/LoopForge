package repo

import (
	"errors"
	"slices"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// JenkinsRepo owns controller connections, project bindings and delivery receipts.
type JenkinsRepo struct{ *deps }

type JenkinsConnection struct {
	ID          string   `json:"id" gorm:"size:80;primaryKey;not null;comment:连接标识"`
	Name        string   `json:"name" gorm:"size:120;not null;default:'';comment:连接名称"`
	URL         string   `json:"url" gorm:"size:500;not null;default:'';comment:Jenkins服务地址"`
	Username    string   `json:"username" gorm:"size:160;not null;default:'';comment:服务账号"`
	Token       string   `json:"-" gorm:"size:300;not null;default:'';comment:Jenkins API令牌"`
	ProjectIDs  []string `json:"projectIds" gorm:"serializer:json;size:4000;not null;default:'[]';comment:授权项目标识"`
	AllProjects bool     `json:"allProjects" gorm:"not null;default:false;comment:允许所有项目使用"`
}

func (c JenkinsConnection) AllowsProject(projectID string) bool {
	return c.AllProjects || slices.Contains(c.ProjectIDs, projectID)
}

type JenkinsBinding struct {
	ProjectID     string            `json:"projectId" gorm:"size:100;primaryKey;not null;comment:所属项目"`
	ID            string            `json:"id" gorm:"size:80;primaryKey;not null;comment:项目内绑定标识"`
	ConnectionID  string            `json:"connectionId" gorm:"size:80;not null;default:'';comment:连接标识"`
	Service       string            `json:"service" gorm:"size:120;not null;default:'';comment:服务名称"`
	Environment   string            `json:"environment" gorm:"size:80;not null;default:'';comment:部署环境"`
	Job           string            `json:"job" gorm:"size:500;not null;default:'';comment:Jenkins完整任务名"`
	Parameter     string            `json:"parameter" gorm:"size:80;not null;default:'';comment:可选任务参数名"`
	Value         string            `json:"value" gorm:"size:240;not null;default:'';comment:可选任务参数值"`
	Parameters    map[string]string `json:"parameters,omitempty" gorm:"serializer:json;size:4000;not null;default:'{}';comment:任务参数JSON对象"`
	Parameterized bool              `json:"parameterized,omitempty" gorm:"not null;default:false;comment:参数化任务标记"`
	Unavailable   bool              `json:"unavailable,omitempty" gorm:"-"`
}

func (b JenkinsBinding) JobParameters() map[string]string {
	if len(b.Parameters) > 0 {
		return b.Parameters
	}
	if b.Parameter != "" {
		return map[string]string{b.Parameter: b.Value}
	}
	return nil
}

type JenkinsReceipt struct {
	RunID   string `gorm:"size:100;primaryKey;not null;comment:SOP运行标识"`
	NodeID  string `gorm:"size:100;primaryKey;not null;comment:发布节点标识"`
	Attempt int    `gorm:"primaryKey;autoIncrement:false;not null;default:0;comment:旧版节点尝试号或固定发布记录"`
	URL     string `gorm:"size:1500;not null;default:'';comment:Jenkins接单链接"`
	State   string `gorm:"size:16;not null;default:'pending';comment:发布请求状态"`
	Queued  bool   `gorm:"not null;default:false;comment:接单链接指向队列"`
}

const (
	JenkinsPending  = "pending"
	JenkinsAccepted = "accepted"
	JenkinsRejected = "rejected"
)

func (r *JenkinsRepo) JenkinsConnections() ([]JenkinsConnection, error) {
	items := []JenkinsConnection{}
	err := r.DB.Order("name, id").Find(&items).Error
	return items, err
}

func (r *JenkinsRepo) JenkinsController(id string) (JenkinsConnection, error) {
	var item JenkinsConnection
	err := r.DB.First(&item, "id = ?", id).Error
	return item, err
}

func (r *JenkinsRepo) SaveJenkinsConnection(item JenkinsConnection) error {
	return r.DB.Transaction(func(tx *gorm.DB) error {
		var saved JenkinsConnection
		err := r.locking(tx).First(&saved, "id = ?", item.ID).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if item.Token == "" {
			if err != nil || saved.Token == "" || item.URL != saved.URL || item.Username != saved.Username {
				return errors.New("新增连接或修改地址、账号时必须填写 API Token")
			}
			item.Token = saved.Token
		}
		return tx.Clauses(clause.OnConflict{DoUpdates: clause.AssignmentColumns([]string{
			"name", "url", "username", "token", "project_ids", "all_projects",
		})}).Create(&item).Error
	})
}

func (r *JenkinsRepo) JenkinsBindings(projectID string) ([]JenkinsBinding, error) {
	items := []JenkinsBinding{}
	err := r.DB.Where("project_id = ?", projectID).Order("environment, service, id").Find(&items).Error
	return items, err
}

func (r *JenkinsRepo) JenkinsBinding(projectID, id string) (JenkinsBinding, error) {
	var item JenkinsBinding
	err := r.DB.First(&item, "project_id = ? AND id = ?", projectID, id).Error
	return item, err
}

func (r *JenkinsRepo) SaveJenkinsBinding(item JenkinsBinding) error {
	if item.Parameters == nil {
		item.Parameters = map[string]string{}
	}
	return r.DB.Clauses(clause.OnConflict{DoUpdates: clause.AssignmentColumns([]string{
		"connection_id", "service", "environment", "job", "parameter", "value", "parameters", "parameterized",
	})}).Create(&item).Error
}

func (r *JenkinsRepo) DeleteJenkinsBinding(projectID, id string) error {
	return r.DB.Delete(&JenkinsBinding{}, "project_id = ? AND id = ?", projectID, id).Error
}

func (r *JenkinsRepo) JenkinsDeliveryReceipt(runID, nodeID string) (JenkinsReceipt, error) {
	var receipt JenkinsReceipt
	err := r.DB.Where("run_id = ? AND node_id = ?", runID, nodeID).Order("attempt").First(&receipt).Error
	return receipt, err
}

// ReserveJenkinsReceipt uses the same key for every attempt of a logical node.
// Legacy attempts with ambiguous outcomes also prevent automatic resubmission.
func (r *JenkinsRepo) ReserveJenkinsReceipt(item JenkinsReceipt) (JenkinsReceipt, bool, error) {
	previous, err := r.JenkinsDeliveryReceipt(item.RunID, item.NodeID)
	switch {
	case err == nil && previous.State == JenkinsRejected && previous.Attempt == 0:
		result := r.DB.Model(&JenkinsReceipt{}).
			Where("run_id = ? AND node_id = ? AND attempt = 0 AND state = ?", item.RunID, item.NodeID, JenkinsRejected).
			Update("state", JenkinsPending)
		if result.Error != nil {
			return previous, false, result.Error
		}
		previous.State = JenkinsPending
		return previous, result.RowsAffected == 1, nil
	case err == nil:
		return previous, false, nil
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return item, false, err
	}
	item.Attempt, item.State = 0, JenkinsPending
	if err := r.DB.Create(&item).Error; !errors.Is(err, gorm.ErrDuplicatedKey) {
		return item, err == nil, err
	}
	previous, err = r.JenkinsDeliveryReceipt(item.RunID, item.NodeID)
	return previous, false, err
}

func (r *JenkinsRepo) SaveJenkinsReceipt(item JenkinsReceipt) error {
	// Attempt 0 is intentionally part of the predicate. GORM omits zero-valued
	// fields when deriving predicates from the model.
	result := r.DB.Model(&JenkinsReceipt{}).
		Where("run_id = ? AND node_id = ? AND attempt = ? AND state = ?", item.RunID, item.NodeID, item.Attempt, JenkinsPending).
		Updates(map[string]any{"url": item.URL, "state": item.State, "queued": item.Queued})
	if result.Error == nil && result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}
