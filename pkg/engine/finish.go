package engine

import (
	"time"

	"pipeline/pkg/engine/notify"
	"pipeline/pkg/models"
)

// 终态收口：聚合、失败刹车的树内标注、任务收尾（含 IM 通知副作用）。

// jobsWorst —— stage 终态 = jobs 最差者
func jobsWorst(st *models.StageTask) models.Status {
	statuses := make([]models.Status, 0, len(st.Jobs))
	for _, jt := range st.Jobs {
		statuses = append(statuses, jt.Status)
	}
	return models.WorstOf(statuses...)
}

// markRestSkipped —— 失败刹车：failedStage 之后的所有 stage（含其 jobs）标记 skipped 并落库
func markRestSkipped(t *models.WorkflowTask, failedStage *models.StageTask) {
	after := false
	for _, st := range t.Stages {
		if st == failedStage {
			after = true
			continue
		}
		if !after {
			continue
		}
		st.Status = models.StatusSkipped
		st.EndTime = time.Now().Unix()
		for _, jt := range st.Jobs {
			jt.Status = models.StatusSkipped
		}
	}
	ack(t)
}

// finish —— 终态收口：置状态、记时间、最后一次 ack、IM 通知（fire-and-forget）。
// ⚠️ 通知只挂在这条引擎自然终态路径上；CancelTree 的纯数据收口（T1.5）不走这里
// （没有 goroutine 在场，通知让位给调用方 API——学习版明确接受这个不对称）。
func finish(t *models.WorkflowTask, status models.Status, errMsg string) {
	t.Status = status
	t.EndTime = time.Now().Unix()
	t.Error = errMsg
	ack(t)
	notify.NotifyOnFinish(t)
}
