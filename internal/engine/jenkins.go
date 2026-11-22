package engine

import (
	"context"
	"errors"
	"slices"
	"time"

	"agentflow/internal/domain"
	"agentflow/internal/jenkins"
	"agentflow/internal/repo"
)

func (e *Engine) triggerJenkins(ctx context.Context, run domain.Run, node domain.WorkflowNode, attempt int) (string, error) {
	projectID := run.ExecutionContext.ProjectID
	if projectID == "" {
		return "", errors.New("Jenkins 发布节点需要绑定项目")
	}
	binding, err := e.repo.JenkinsBinding(projectID, node.JenkinsBindingID)
	if err != nil {
		return "", errors.New("当前项目的 Jenkins Job 绑定不可用")
	}
	controller, err := e.repo.JenkinsController(binding.ConnectionID)
	if err != nil || !slices.Contains(controller.ProjectIDs, projectID) {
		return "", errors.New("当前项目未获该 Jenkins 连接授权")
	}
	receipt, fresh, err := e.repo.ReserveJenkinsReceipt(repo.JenkinsReceipt{RunID: run.ID, NodeID: node.ID, Attempt: attempt})
	if err != nil {
		return "", errors.New("无法记录 Jenkins 调用，请检查平台存储")
	}
	if !fresh {
		if receipt.URL != "" {
			return receipt.URL, nil
		}
		return "", errors.New("本次尝试已经发送或预留了 Jenkins 请求，请先到 Jenkins 确认是否接单")
	}
	client := jenkins.Client{BaseURL: controller.URL, Username: controller.Username, Token: controller.Token}
	receipt.URL, err = client.Trigger(ctx, binding.Job, binding.Parameter, binding.Value)
	if err != nil {
		return "", err
	}
	if err := e.repo.SaveJenkinsReceipt(receipt); err != nil {
		return "", errors.New("Jenkins 已接单，但回执保存失败，请在 Jenkins 检查队列")
	}
	return receipt.URL, nil
}

func (e *Engine) finishJenkinsNode(runID string, node domain.WorkflowNode, attempt, generation int, url string) {
	e.updateRun(runID, func(run *domain.Run) {
		nr := run.Nodes[node.ID]
		if !shouldCommitNodeOutcome(*run, nr, generation, attempt, true) {
			return
		}
		nr.Status = "succeeded"
		nr.FinishedAt = time.Now().UTC()
		if url == "" {
			nr.Output = "基线对齐已跳过 Jenkins 发布。"
			e.event(run, node.ID, "node.succeeded", nr.Output)
			return
		}
		nr.JenkinsURL = url
		nr.Output = "Jenkins 已接单，部署结果请查看 Jenkins。"
		nr.Error = ""
		e.event(run, node.ID, "node.succeeded", node.Name+" 已提交 Jenkins")
	})
}
