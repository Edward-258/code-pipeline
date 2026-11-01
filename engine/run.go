package engine

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"pipeline/config"
	"pipeline/models"
	"pipeline/repo"
)

const (
	pollInterval = 2 * time.Second  // 决断 #2：轮询不上 watch，节奏对照 Zadig
	jobTimeout   = 10 * time.Minute // 兜底超时
)

// taskCtx —— 任务运行期共享环境。
// mu 保 ack 互斥：并行 job 各自回写时，整棵树的字段写 + BSON 序列化 + ReplaceOne
// 必须串行（Zadig 同款问题同款解：jobcontroller 并行跑 job，ack 全量写）。
type taskCtx struct {
	client kubernetes.Interface
	ns     string
	mu     sync.Mutex
}

// ack —— 互斥版整树写回（并行场景下多 goroutine 共用）
func (tc *taskCtx) ack(t *models.WorkflowTask) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	ack(t)
}

// Run —— 对照 Zadig workflowcontroller.RunTask → RunStages 的极简版。
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

	runStages(&taskCtx{client: client, ns: cfg.K8SNamespace}, t)
}

// runStages —— 对照 Zadig RunStages：stage 严格按序，跑完聚合，失败刹车。
//   - 跳过 passed/skipped 的 stage（重跑恢复的入口，M3 暂停恢复全靠它）
//   - stage 终态 = 其 jobs 的最差者（WorstOf）
//   - stage 终态命中 StatusStopped → 后续 stage 全部 skipped，任务收口
func runStages(tc *taskCtx, t *models.WorkflowTask) {
	for _, st := range t.Stages {
		if st.Status == models.StatusPassed || st.Status == models.StatusSkipped {
			continue
		}

		st.Status = models.StatusRunning
		st.StartTime = time.Now().Unix()
		tc.ack(t)
		log.Printf("engine: task %s/%d stage %q running", t.WorkflowName, t.TaskID, st.Name)

		runStage(tc, t, st)

		st.Status = jobsWorst(st)
		if st.Status == models.StatusFailed && st.Error == "" {
			for _, jt := range st.Jobs { // 把第一个失败 job 的错误冒泡到 stage 层
				if jt.Status == models.StatusFailed {
					st.Error = fmt.Sprintf("job %q failed: %s", jt.Name, jt.Error)
					break
				}
			}
		}
		st.EndTime = time.Now().Unix()
		tc.ack(t)
		log.Printf("engine: task %s/%d stage %q %s", t.WorkflowName, t.TaskID, st.Name, st.Status)

		if models.StatusStopped(st.Status) {
			markRestSkipped(t, st)
			finish(t, st.Status, fmt.Sprintf("stage %q %s: %s", st.Name, st.Status, st.Error))
			log.Printf("engine: task %s/%d %s (braked at stage %q)", t.WorkflowName, t.TaskID, st.Status, st.Name)
			return
		}
	}

	finish(t, models.StatusPassed, "")
	log.Printf("engine: task %s/%d passed", t.WorkflowName, t.TaskID)
}

// runStage —— stage 内执行策略（对照 Zadig stage.go runStage → CustomStageCtl.Run）：
//   - Parallel=true：jobs 全员齐飞（goroutine + WaitGroup），等全部落地再聚合
//   - Parallel=false：逐个放行；任一 job 命中刹车态 → 剩余未启动的直接 skipped
// 刹车语义：不再起新的，已在跑的让它跑完（并行模式天然如此：WaitGroup 等全员）。
func runStage(tc *taskCtx, t *models.WorkflowTask, st *models.StageTask) {
	if st.Parallel {
		var wg sync.WaitGroup
		for _, jt := range st.Jobs {
			wg.Add(1)
			go func(jt *models.JobTask) {
				defer wg.Done()
				runJobTask(tc, t, st, jt)
			}(jt)
		}
		wg.Wait()
		return
	}

	for _, jt := range st.Jobs {
		if stageBraked(st) {
			jt.Status = models.StatusSkipped // 不再起新的：未启动的直接置 skipped
			tc.ack(t)
			continue
		}
		runJobTask(tc, t, st, jt)
	}
}

// stageBraked —— 串行模式下刹车判断：stage 内已有 job 命中 StatusStopped
func stageBraked(st *models.StageTask) bool {
	for _, jt := range st.Jobs {
		if models.StatusStopped(jt.Status) {
			return true
		}
	}
	return false
}

// runJobTask —— 单个 job 的状态机封装（对照 Zadig jobcontroller.job.Run）。
// ⚠️ jt 字段写全部在锁内：并行时兄弟 goroutine 的 ack 正在整树序列化，
// 裸写 jt.Status = 数据竞争。K8s 侧调用（runJob）不碰共享内存，放锁外。
func runJobTask(tc *taskCtx, t *models.WorkflowTask, st *models.StageTask, jt *models.JobTask) {
	defer func() { // 并行 goroutine 里的 panic 进程会直接崩，必须就地回收并置 failed
		if r := recover(); r != nil {
			tc.mu.Lock()
			jt.Status = models.StatusFailed
			jt.Error = fmt.Sprintf("engine panic: %v", r)
			ack(t)
			tc.mu.Unlock()
			log.Printf("engine: job %q panic (recovered, marked failed): %v", jt.Name, r)
		}
	}()

	tc.mu.Lock()
	jt.Status = models.StatusRunning
	jt.K8sJobName = jobName(t.TaskID, st.Name, jt.Name)
	ack(t)
	tc.mu.Unlock()
	log.Printf("engine: task %s/%d job %q -> %s", t.WorkflowName, t.TaskID, jt.Name, jt.K8sJobName)

	err := runJob(tc.client, tc.ns, jt)

	tc.mu.Lock()
	if err != nil {
		jt.Status = models.StatusFailed
		jt.Error = err.Error()
	} else {
		jt.Status = models.StatusPassed
	}
	ack(t)
	tc.mu.Unlock()
}

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

// runJob —— 一个 K8s Job 的完整生命周期：创建 → 轮询 → 清理。
// 资源与数据分离：K8s Job 是消耗品（结束就删），Mongo 档案才是真相源。
// ⚠️ 只碰 K8s API 不碰共享内存——这是并行场景并发安全的前提。
func runJob(client kubernetes.Interface, ns string, jt *models.JobTask) error {
	// 幂等：残留同名 Job 先清（为 M4 单 job 重试 / 崩溃恢复扫清脏资源）
	_ = client.BatchV1().Jobs(ns).Delete(context.TODO(), jt.K8sJobName, deleteOpts())

	if _, err := client.BatchV1().Jobs(ns).Create(context.TODO(), buildJob(ns, jt), metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create k8s job: %w", err)
	}

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
