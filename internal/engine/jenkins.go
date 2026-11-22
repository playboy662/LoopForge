package engine

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"agentflow/internal/domain"
	"agentflow/internal/jenkins"
	"agentflow/internal/repo"
	"gorm.io/gorm"
)

func jenkinsNodeKey(path, node string) string {
	if path == "" {
		return node
	}
	encoded, _ := json.Marshal([2]string{path, node})
	return fmt.Sprintf("jenkins-%x", sha256.Sum256(encoded))
}

func (e *Engine) jenkinsTarget(ec domain.TelemetryContext, node domain.WorkflowNode) (repo.JenkinsBinding, repo.JenkinsConnection, error) {
	project, found := e.repo.Project(ec.ProjectID)
	user, signedIn := e.repo.User(ec.ResponsibleUserID)
	auth, _ := strconv.ParseBool(strings.TrimSpace(os.Getenv("AGENTFLOW_AUTH_REQUIRED")))
	if !auth || !found || project.ArchivedAt != nil || !signedIn ||
		(user.ID != project.OwnerID && !e.repo.IsPlatformAdmin(user.LoginName) &&
			!slices.ContainsFunc(project.Members, func(member domain.ProjectMember) bool {
				return member.UserID == user.ID && !strings.EqualFold(strings.TrimSpace(member.Role), "viewer")
			})) {
		return repo.JenkinsBinding{}, repo.JenkinsConnection{}, errors.New("Jenkins 发布需要有效项目及项目执行权限")
	}
	binding, err := e.repo.JenkinsBinding(ec.ProjectID, node.JenkinsBindingID)
	if err != nil {
		return binding, repo.JenkinsConnection{}, errors.New("当前项目的 Jenkins Job 绑定不可用")
	}
	controller, err := e.repo.JenkinsController(binding.ConnectionID)
	if err != nil || !controller.AllowsProject(ec.ProjectID) {
		return binding, controller, errors.New("当前项目未获该 Jenkins 连接授权")
	}
	return binding, controller, jenkins.ValidateURL(controller.URL)
}

func jenkinsTargetRevision(workflowID string, binding repo.JenkinsBinding, controller repo.JenkinsConnection) string {
	value, _ := json.Marshal(struct {
		Workflow, URL, Username string
		Binding                 repo.JenkinsBinding
	}{workflowID, controller.URL, controller.Username, binding})
	return fmt.Sprintf("%x", sha256.Sum256(value))
}

func (e *Engine) freezeJenkinsTargets(wf domain.WorkflowDefinition, ec domain.TelemetryContext) (map[string]string, error) {
	var targets map[string]string
	var walk func(domain.WorkflowDefinition, string, int) error
	walk = func(current domain.WorkflowDefinition, path string, depth int) error {
		for _, node := range current.Nodes {
			key := jenkinsNodeKey(path, node.ID)
			switch node.Type {
			case "jenkins":
				binding, controller, err := e.jenkinsTarget(ec, node)
				if err != nil {
					return fmt.Errorf("节点 %s：%w", node.Name, err)
				}
				if targets == nil {
					targets = make(map[string]string)
				}
				targets[key] = jenkinsTargetRevision(current.ID, binding, controller)
			case "sop":
				if depth < maxSOPNesting {
					if child, ok := e.repo.Workflow(node.SOPWorkflowID); ok {
						if err := walk(child, key, depth+1); err != nil {
							return err
						}
					}
				}
			}
		}
		return nil
	}
	err := walk(wf, "", 0)
	return targets, err
}

func (e *Engine) triggerJenkins(ctx context.Context, run domain.Run, node domain.WorkflowNode, attempt int) (repo.JenkinsReceipt, error) {
	var empty repo.JenkinsReceipt
	root := run
	if run.ExecutionContext.ParentRunID != "" {
		var ok bool
		root, ok = e.repo.RunState(run.ExecutionContext.WorkspaceRunID)
		if !ok || run.ExecutionContext.JenkinsPath == "" || root.ExecutionContext.ProjectID != run.ExecutionContext.ProjectID {
			return empty, errors.New("无法确认 Jenkins 发布所属的父 SOP")
		}
	}
	key := jenkinsNodeKey(run.ExecutionContext.JenkinsPath, node.ID)
	previous, err := e.repo.JenkinsDeliveryReceipt(root.ID, key)
	if err == nil && previous.URL != "" {
		return previous, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return empty, errors.New("无法读取 Jenkins 发布回执")
	}
	binding, controller, err := e.jenkinsTarget(run.ExecutionContext, node)
	if err != nil {
		return empty, err
	}
	if root.WorkflowSnapshot.JenkinsTargets[key] != jenkinsTargetRevision(run.WorkflowID, binding, controller) {
		return empty, errors.New("Jenkins 发布目标未冻结或已变更，请核对配置并发起新运行")
	}
	receipt, fresh, err := e.repo.ReserveJenkinsReceipt(repo.JenkinsReceipt{RunID: root.ID, NodeID: key})
	if err != nil {
		return empty, errors.New("无法记录 Jenkins 调用，请检查平台存储")
	}
	if !fresh {
		if receipt.URL != "" {
			return receipt, nil
		}
		return empty, errors.New("Jenkins 接单状态待确认。请核对 Jenkins 队列及构建记录，确认需要再次发布后发起新运行")
	}
	check, ok := e.repo.RunState(run.ID)
	if !ok || !e.ownsRun(check) || check.Generation != run.Generation || check.CancelRequested || check.PauseRequested ||
		!shouldCommitNodeOutcome(check, check.Nodes[node.ID], run.Generation, attempt, false) || ctx.Err() != nil {
		receipt.State = repo.JenkinsRejected
		_ = e.repo.SaveJenkinsReceipt(receipt)
		return empty, errors.New("执行租约或节点状态已变化，Jenkins 请求未发送")
	}
	binding, controller, err = e.jenkinsTarget(run.ExecutionContext, node)
	if err != nil || root.WorkflowSnapshot.JenkinsTargets[key] != jenkinsTargetRevision(run.WorkflowID, binding, controller) {
		receipt.State = repo.JenkinsRejected
		_ = e.repo.SaveJenkinsReceipt(receipt)
		return empty, errors.New("发送前 Jenkins 授权或目标已变更")
	}
	client := jenkins.Client{BaseURL: controller.URL, Username: controller.Username, Token: controller.Token}
	result, err := client.Trigger(ctx, binding.Job, binding.JobParameters(), binding.Parameterized)
	if err != nil {
		if result.Rejected {
			receipt.State = repo.JenkinsRejected
			if err := e.repo.SaveJenkinsReceipt(receipt); err != nil {
				return empty, errors.New("无法保存 Jenkins 拒绝回执，请核对 Jenkins 记录")
			}
			return empty, err
		}
		return empty, fmt.Errorf("Jenkins 接单状态待确认，请核对 Jenkins 队列及构建记录：%w", err)
	}
	receipt.URL, receipt.Queued, receipt.State = result.URL, result.Queued, repo.JenkinsAccepted
	if err := e.repo.SaveJenkinsReceipt(receipt); err != nil {
		return empty, errors.New("Jenkins 已接单，但回执保存失败，请在 Jenkins 检查队列")
	}
	return receipt, nil
}

func (e *Engine) finishJenkinsNode(runID string, node domain.WorkflowNode, attempt, generation int, receipt repo.JenkinsReceipt) {
	err := e.updateRunErr(runID, func(run *domain.Run) {
		nr := run.Nodes[node.ID]
		if !shouldCommitNodeOutcome(*run, nr, generation, attempt, false) {
			return
		}
		nr.Status = "succeeded"
		nr.FinishedAt = time.Now().UTC()
		if receipt.URL == "" {
			nr.Output = "基线对齐已跳过 Jenkins 发布。"
			e.event(run, node.ID, "node.succeeded", nr.Output)
			return
		}
		nr.JenkinsURL, nr.JenkinsQueued = receipt.URL, receipt.Queued
		nr.Output = "Jenkins 已接单，部署结果请查看 Jenkins。"
		nr.Error = ""
		e.event(run, node.ID, "node.succeeded", node.Name+" 已提交 Jenkins")
	})
	if err != nil {
		e.requeueInterruptedNode(runID, node.ID, attempt, generation)
	}
}
