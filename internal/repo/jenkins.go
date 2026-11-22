package repo

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// JenkinsRepo owns controller connections, project bindings and delivery receipts.
type JenkinsRepo struct{ *deps }

type JenkinsConnection struct {
	ID         string   `json:"id" gorm:"size:80;primaryKey;not null;comment:连接标识"`
	Name       string   `json:"name" gorm:"size:120;not null;default:'';comment:连接名称"`
	URL        string   `json:"url" gorm:"size:500;not null;default:'';comment:Jenkins服务地址"`
	Username   string   `json:"username" gorm:"size:160;not null;default:'';comment:服务账号"`
	Token      string   `json:"-" gorm:"size:300;not null;default:'';comment:Jenkins API令牌"`
	ProjectIDs []string `json:"projectIds" gorm:"serializer:json;size:4000;not null;default:'[]';comment:授权项目标识"`
}

type JenkinsBinding struct {
	ProjectID    string `json:"projectId" gorm:"size:100;primaryKey;not null;comment:所属项目"`
	ID           string `json:"id" gorm:"size:80;primaryKey;not null;comment:项目内绑定标识"`
	ConnectionID string `json:"connectionId" gorm:"size:80;not null;default:'';comment:连接标识"`
	Service      string `json:"service" gorm:"size:120;not null;default:'';comment:服务名称"`
	Environment  string `json:"environment" gorm:"size:80;not null;default:'';comment:部署环境"`
	Job          string `json:"job" gorm:"size:500;not null;default:'';comment:Jenkins完整任务名"`
	Parameter    string `json:"parameter" gorm:"size:80;not null;default:'';comment:可选任务参数名"`
	Value        string `json:"value" gorm:"size:240;not null;default:'';comment:可选任务参数值"`
}

type JenkinsReceipt struct {
	RunID   string `gorm:"size:100;primaryKey;not null;comment:SOP运行标识"`
	NodeID  string `gorm:"size:100;primaryKey;not null;comment:发布节点标识"`
	Attempt int    `gorm:"primaryKey;autoIncrement:false;not null;comment:节点尝试次数"`
	URL     string `gorm:"size:1500;not null;default:'';comment:Jenkins接单链接"`
}

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
		return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&item).Error
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
	return r.DB.Clauses(clause.OnConflict{UpdateAll: true}).Create(&item).Error
}

func (r *JenkinsRepo) DeleteJenkinsBinding(projectID, id string) error {
	return r.DB.Delete(&JenkinsBinding{}, "project_id = ? AND id = ?", projectID, id).Error
}

// ReserveJenkinsReceipt persists intent first. A lost response may have
// enqueued a job, so resuming that attempt must never send another POST.
func (r *JenkinsRepo) ReserveJenkinsReceipt(item JenkinsReceipt) (JenkinsReceipt, bool, error) {
	result := r.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&item)
	if result.Error != nil || result.RowsAffected != 0 {
		return item, result.RowsAffected != 0, result.Error
	}
	err := r.DB.First(&item, "run_id = ? AND node_id = ? AND attempt = ?", item.RunID, item.NodeID, item.Attempt).Error
	return item, false, err
}

func (r *JenkinsRepo) SaveJenkinsReceipt(item JenkinsReceipt) error {
	return r.DB.Model(&item).Update("url", item.URL).Error
}
