package engine

import (
	"errors"
	"fmt"

	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// ErrWorkflowNotFound —— 触发目标图纸不存在（API 侧翻译成 404）
var ErrWorkflowNotFound = errors.New("workflow not found")

// TriggerWorkflow —— 触发器统一入口（T3.1）：cron / webhook / 手动 run 全走这里。
// 发号 → 快照建树 → 落库 → 入队——触发器不许有第二条快照实现（图纸/实例分离的
// 灵魂只允许一个构造器，见 engine/task.go 与下面的 spawn）。
func TriggerWorkflow(wfName, creator string) (*models.WorkflowTask, error) {
	wf, err := repo.FindWorkflow(wfName)
	if err == repo.ErrNotFound {
		return nil, fmt.Errorf("%w: %q", ErrWorkflowNotFound, wfName)
	}
	if err != nil {
		return nil, err
	}
	return spawn(wf, creator)
}

// spawn —— 触发唯一构造序列：发号 → 快照建树 → 落库 → 入队。
// cron/webhook/手动 run（经 TriggerWorkflow）与 clone（经 CloneTask）共用此路——
// 新触发来源长出来时也必须走这里，不许自建序列。
func spawn(wf *models.Workflow, creator string) (*models.WorkflowTask, error) {
	taskID, err := repo.NextID("workflow_task:" + wf.Name)
	if err != nil {
		return nil, err
	}
	t := BuildTask(wf, taskID)
	t.Creator = creator
	if err := repo.CreateTask(t); err != nil {
		return nil, err
	}
	if err := Enqueue(t); err != nil {
		return nil, err
	}
	return t, nil
}
