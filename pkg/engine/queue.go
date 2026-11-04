package engine

import (
	"log"
	"time"

	"pipeline/pkg/config"
	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// 任务队列（T1.6，对照 Zadig workflowcontroller/queue.go 的 WorfklowTaskSender + 双层并发上限）。
//
// 不变量 1（mvp复现计划 §5）：队列是排序器不是新真相源——档案只有 workflow_task 一份，
// 队列态就是 status=queued，派发器只是调度视图，不存在第二处状态。
//
// 并发模型：
//   - 全局上限 MAX_RUNNING_TASKS（默认 3，env 可调，0 = 停止派发只收尾）
//   - 同 workflow 串行（同一时刻一个 wf 至多一个 running——重跑竞争、互踩输出都杜绝了）
//   - pause 不占名额：它没有 goroutine（M3 灵魂），release 后走正常入队重新排队
//
// 双派防线：派发器对候选任务做 repo.ClaimQueued（queued→running 的 CAS 条件更新），
// MatchedCount==1 才 go Run——同一拍重复扫描、相邻拍竞速、与 cancel 数据收口赛跑，
// 都被 Mongo 单文档原子性挡住。

const dispatchInterval = time.Second // 派发节拍（对照 Zadig 的 queue tick 量级）

// Enqueue —— 入队：status=queued 落库即完事（RunWorkflow / ReleaseStage 续跑的唯一入口）。
// 真正的执行由派发器接管——API 永远不直接 go Run。
func Enqueue(t *models.WorkflowTask) error {
	t.Status = models.StatusQueued
	ack(t)
	log.Printf("engine: task %s/%d queued", t.WorkflowName, t.TaskID)
	return nil
}

// StartDispatcher —— 派发器主循环（main.go 在 Recover 之后拉起，随进程同生共死）。
func StartDispatcher() {
	ticker := time.NewTicker(dispatchInterval)
	defer ticker.Stop()
	for range ticker.C {
		dispatchOnce()
	}
}

// dispatchOnce —— 单拍派发：扫 queued（task_id 升序）→ 逐个过闸 → CAS 抢占 → 起 goroutine。
// 任何一步出错只记日志不退出——下一拍自然重来，队列是轮询出身的，不怕失败重试。
func dispatchOnce() {
	queued, err := repo.ListQueued()
	if err != nil {
		log.Printf("engine: dispatcher scan queued: %v", err)
		return
	}
	if len(queued) == 0 {
		return
	}
	running, byWf, err := repo.RunningStats()
	if err != nil {
		log.Printf("engine: dispatcher scan running: %v", err)
		return
	}
	max := int64(config.Load().MaxRunningTasks)

	dispatched := map[string]bool{} // 本拍内已派发的 wf（同 wf 串行的拍内去重）
	for _, t := range queued {
		if byWf[t.WorkflowName] > 0 || dispatched[t.WorkflowName] {
			continue // 同 wf 串行：已有 running 在场，或本拍已派过——留队下拍
		}
		if running >= max {
			return // 全局名额用完：task_id 升序保证先来先服务，剩下的下拍再看
		}
		ok, err := repo.ClaimQueued(t)
		if err != nil {
			log.Printf("engine: dispatcher claim %s: %v", t.ID, err)
			continue
		}
		if !ok {
			continue // 被抢先翻走了（cancel / 异常路径）：下拍对齐，绝不双跑
		}
		dispatched[t.WorkflowName] = true
		running++
		byWf[t.WorkflowName]++
		tk := t
		log.Printf("engine: dispatcher -> run %s (running=%d/%d)", tk.ID, running, max)
		go Run(tk)
	}
}
