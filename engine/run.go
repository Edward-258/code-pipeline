package engine

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"pipeline/config"
	"pipeline/models"
	"pipeline/repo"
)

const (
	pollInterval = 2 * time.Second  // 决断 #2：轮询不上 watch，节奏对照 Zadig
	jobTimeout   = 10 * time.Minute // M1 兜底超时
)

// Run —— 任务的"第一口气"执行器（对照 Zadig workflowcontroller.RunTask 的极简版）
//
// M1 范围：stage 串行、job 串行（Parallel / manual_exec 留给 M2/M3）。
// 铁律：每前进一步立即 ack（整树 ReplaceOne 落库）——进程随时可被杀，
// 已发生的进度不丢，这也是 M3 durable 暂停的地基。
func Run(t *models.WorkflowTask) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("engine: panic in task %s/%d: %v", t.WorkflowName, t.TaskID, r)
			finish(t, models.StatusFailed, fmt.Sprintf("engine panic: %v", r))
		}
	}()

	cfg := config.Load()
	client, err := NewClient(cfg.K8SContext)
	if err != nil {
		finish(t, models.StatusFailed, err.Error())
		return
	}

	t.Status = models.StatusRunning
	t.StartTime = time.Now().Unix()
	ack(t)
	log.Printf("engine: task %s/%d running", t.WorkflowName, t.TaskID)

	for _, st := range t.Stages {
		st.Status = models.StatusRunning
		st.StartTime = time.Now().Unix()
		ack(t)

		for _, jt := range st.Jobs {
			jt.Status = models.StatusRunning
			ack(t)

			if err := runJob(client, cfg.K8SNamespace, t, st, jt); err != nil {
				jt.Status = models.StatusFailed
				jt.Error = err.Error()
				st.Status = models.StatusFailed
				st.Error = fmt.Sprintf("job %q failed: %v", jt.Name, err)
				finish(t, models.StatusFailed, st.Error) // 失败刹车，对照 StatusStopped
				return
			}

			jt.Status = models.StatusPassed
			ack(t)
		}

		st.Status = models.StatusPassed
		st.EndTime = time.Now().Unix()
		ack(t)
	}

	finish(t, models.StatusPassed, "")
	log.Printf("engine: task %s/%d passed", t.WorkflowName, t.TaskID)
}

// runJob —— 一个 JobTask 的完整生命周期：创建 → 轮询 → 回写 → 清理
// 资源与数据分离：K8s Job 是消耗品（结束就删），Mongo 档案才是真相源
func runJob(client kubernetes.Interface, ns string, t *models.WorkflowTask, st *models.StageTask, jt *models.JobTask) error {
	jt.K8sJobName = jobName(t.TaskID, st.Name, jt.Name)

	// 幂等：残留同名 Job 先清（为 M4 单 job 重试 / 崩溃恢复扫清脏资源）
	_ = client.BatchV1().Jobs(ns).Delete(context.TODO(), jt.K8sJobName, deleteOpts())

	if _, err := client.BatchV1().Jobs(ns).Create(context.TODO(), buildJob(ns, jt), metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create k8s job: %w", err)
	}
	log.Printf("engine: task %s/%d created job %s", t.WorkflowName, t.TaskID, jt.K8sJobName)

	defer func() {
		_ = client.BatchV1().Jobs(ns).Delete(context.TODO(), jt.K8sJobName, deleteOpts())
	}()

	deadline := time.Now().Add(jobTimeout)
	for {
		job, err := client.BatchV1().Jobs(ns).Get(context.TODO(), jt.K8sJobName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("poll k8s job: %w", err)
		}
		if job.Status.Succeeded >= 1 {
			return nil
		}
		if job.Status.Failed >= 1 {
			return fmt.Errorf("exit %s", podExitInfo(client, ns, jt.K8sJobName))
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout after %s", jobTimeout)
		}
		time.Sleep(pollInterval)
	}
}

// podExitInfo —— 挖出 Pod 的退出码/原因（决断 #3"Pod 退出码 = job 结果"的落点）
func podExitInfo(client kubernetes.Interface, ns, jobName string) string {
	pods, err := client.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{LabelSelector: "job-name=" + jobName})
	if err != nil || len(pods.Items) == 0 {
		return "unknown (no pod found)"
	}
	for _, cs := range pods.Items[0].Status.ContainerStatuses {
		if term := cs.State.Terminated; term != nil {
			return fmt.Sprintf("code %d: %s", term.ExitCode, strings.TrimSpace(term.Message))
		}
		if w := cs.State.Waiting; w != nil {
			return fmt.Sprintf("%s: %s", w.Reason, w.Message)
		}
	}
	return "unknown"
}

// deleteOpts —— 显式 Background 级联：默认删 Job 会留下孤儿 Pod（k8s 的坑，日志里警告过）
func deleteOpts() metav1.DeleteOptions {
	return metav1.DeleteOptions{PropagationPolicy: func() *metav1.DeletionPropagation {
		p := metav1.DeletePropagationBackground
		return &p
	}()}
}

// ack —— 任务每前进一步，整棵树写回 Mongo（对照 Zadig 的 ack() 全量写）
func ack(t *models.WorkflowTask) {
	if err := repo.UpdateTask(t); err != nil {
		log.Printf("engine: ack task %s/%d failed: %v", t.WorkflowName, t.TaskID, err)
	}
}

// finish —— 终态收口：置状态、记时间、最后一次 ack
func finish(t *models.WorkflowTask, status models.Status, errMsg string) {
	t.Status = status
	t.EndTime = time.Now().Unix()
	t.Error = errMsg
	ack(t)
}
