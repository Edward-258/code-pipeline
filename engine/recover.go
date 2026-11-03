package engine

import (
	"context"
	"log"
	"time"

	"pipeline/config"
	"pipeline/models"
	"pipeline/repo"
)

// Recover —— M3.5 启动宣判（对照 Zadig queue.go:110 InitQueue："如果 Queue 重新初始化,
// 取消所有 running tasks"，revoker="system"）。
//
// 为什么不复活：adopt 一棵半跑过的任务树（部分 stage 已完成、K8s Job 可能已终态、
// 轮询到一半）是脏活且不确定；宣判死亡是确定性的，CI 语义下用户重跑一次任务成本≈0。
// 补齐的不变量：running 的任务要么有活着的看护人，要么是终态——不许永远卡 running。
//
// ⚠️ 绝不动 pause：暂停 = 世上本无执行流（M3 灵魂），不需要看护人，跨重启存活。
// 已知边界：滚动发布时新 Pod 宣判的瞬间旧 Pod 可能还有残影 goroutine 在 ack——
// 档案最终仍会落到终态（cancelled 被旧 Pod 的最后一次 ack 覆盖成 failed/passed 而已），
// 学习版接受；Zadig 用 cancel 消息队列让旧进程自己动手，我们没有那套基建。
func Recover() {
	cfg := config.Load()
	client, err := NewClient(cfg.K8SContext)
	if err != nil {
		log.Printf("engine: recover skipped (k8s client): %v", err)
		return
	}

	tasks, err := repo.InCompletedTasks()
	if err != nil {
		log.Printf("engine: recover scan failed: %v", err)
		return
	}
	if len(tasks) == 0 {
		return
	}

	for _, t := range tasks {
		leaked := declareDead(t)
		for _, jobName := range leaked { // 清理执行物：K8s Job 是消耗品，档案才是真相源
			_ = client.BatchV1().Jobs(cfg.K8SNamespace).Delete(context.TODO(), jobName, deleteOpts())
		}
		if err := repo.UpdateTask(t); err != nil {
			log.Printf("engine: recover task %s/%d ack failed: %v", t.WorkflowName, t.TaskID, err)
			continue
		}
		log.Printf("engine: recover: task %s/%d declared cancelled (engine restarted), %d k8s job(s) cleaned",
			t.WorkflowName, t.TaskID, len(leaked))
	}
}

// declareDead —— 把一棵被看护人抛下的任务树宣判为终态（纯内存操作，可单测）：
//   - running 的 stage/job → cancelled（被打断），running job 的 K8s Job 名收集进 leaked
//   - 从未启动的（created stage/job）→ skipped（无事发生）
//   - 已终态的原样保留——历史不许改写
func declareDead(t *models.WorkflowTask) (leaked []string) {
	now := time.Now().Unix()
	t.Status = models.StatusCancelled
	t.EndTime = now
	t.Error = "engine restarted while task was in flight (declared dead by startup recovery)"

	for _, st := range t.Stages {
		switch st.Status {
		case models.StatusRunning:
			st.Status = models.StatusCancelled
			st.EndTime = now
			for _, jt := range st.Jobs {
				switch jt.Status {
				case models.StatusRunning:
					jt.Status = models.StatusCancelled
					jt.Error = "engine restarted"
					if jt.K8sJobName != "" {
						leaked = append(leaked, jt.K8sJobName)
					}
				case models.StatusCreated:
					jt.Status = models.StatusSkipped
				}
			}
		case models.StatusCreated:
			st.Status = models.StatusSkipped
			st.EndTime = now
			for _, jt := range st.Jobs {
				jt.Status = models.StatusSkipped
			}
		}
	}
	return leaked
}
