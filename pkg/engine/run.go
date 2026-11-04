package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"pipeline/pkg/config"
	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

const (
	jobTimeout = 10 * time.Minute // 兜底超时
)

// pollInterval —— 轮询节奏（决断 #2：轮询不上 watch）。var 而非 const：单测缩短节拍，
// 别让表测陪跑 2s 一拍的真节奏。
var pollInterval = 2 * time.Second

// taskCtx —— 任务运行期共享环境。
// mu 保 ack 互斥：并行 job 各自回写时，整棵树的字段写 + BSON 序列化 + ReplaceOne
// 必须串行（Zadig 同款问题同款解：jobcontroller 并行跑 job，ack 全量写）。
type taskCtx struct {
	client    kubernetes.Interface
	ns        string
	t         *models.WorkflowTask // ack 的对象（整树写回的目标）
	mu        sync.Mutex
	cancelCh  <-chan struct{} // T1.5 取消信号（close 广播；nil = 永不触发）
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

	// T1.5：注册取消信号，goroutine 退出即注销（注册表只装活人）
	cancelCh, unregister := registerCancel(t.ID)
	defer unregister()

	cfg := config.Load()
	client, err := NewClient(cfg.K8SContext)
	if err != nil {
		finish(t, models.StatusFailed, err.Error())
		return
	}

	t.Status = models.StatusRunning
	if t.StartTime == 0 { // 恢复路径不覆盖首次开始时间（M3：release 续跑 = 重新 Run 同一棵树）
		t.StartTime = time.Now().Unix()
	}
	ack(t)
	log.Printf("engine: task %s/%d running", t.WorkflowName, t.TaskID)

	runStages(&taskCtx{client: client, ns: cfg.K8SNamespace, t: t, cancelCh: cancelCh}, t)
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

		// T1.5 取消检查点（stage 边界）：取消信号在 stage 间隙到的，当场收口——
		// 本 stage 置 skipped，后续 stage 一并 skipped，任务 cancelled
		if cancelled(tc.cancelCh) {
			st.Status = models.StatusSkipped
			markRestSkipped(t, st)
			finish(t, models.StatusCancelled, "cancelled by user")
			log.Printf("engine: task %s/%d cancelled at stage %q boundary", t.WorkflowName, t.TaskID, st.Name)
			return
		}

		// M3 闸门（对照 Zadig stage.go waitForManualExec）：stage 开跑前若启用手动执行且未放行
		// → stage/task 置 pause 落库 → goroutine 直接 return（不 sleep 不占线程）。
		// durable 的全部秘密：此刻起世上只剩 Mongo 里这份 pause 档案，进程死活与它无关；
		// release 端点标记 Executed=true 后重新 Run，凭上面的跳过逻辑从暂停的 stage 接着跑。
		if stageGated(st) {
			st.Status = models.StatusPause
			if st.ManualExec != nil {
				st.ManualExec.NotificationSent = true // 对照 Zadig 通知去重位；学习版只置位不发送
			}
			t.Status = models.StatusPause
			tc.ack(t)
			log.Printf("engine: task %s/%d paused at stage %q (waiting release)", t.WorkflowName, t.TaskID, st.Name)
			return // ⭐ 不走 finish：任务没结束，只是暂停
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

		// T1.9：stage 级刹车只认"硬失败"——全靠 ignore 策略失败堆出来的 failed stage
		// 不刹车，后续 stage 照跑（控制/上报分离：状态如实红，控制流放行）
		if models.StatusStopped(st.Status) && stageHasHardFailure(st) {
			markRestSkipped(t, st)
			finish(t, st.Status, fmt.Sprintf("stage %q %s: %s", st.Name, st.Status, st.Error))
			log.Printf("engine: task %s/%d %s (braked at stage %q)", t.WorkflowName, t.TaskID, st.Status, st.Name)
			return
		}
	}

	// 尾态不再硬编码 passed：按各 stage 的最差者胜如实收口
	// （全 passed → passed；含 ignore 失败 → failed，但流水线跑到了终点）
	statuses := make([]models.Status, 0, len(t.Stages))
	for _, st := range t.Stages {
		statuses = append(statuses, st.Status)
	}
	finish(t, models.WorstOf(statuses...), "")
	log.Printf("engine: task %s/%d %s", t.WorkflowName, t.TaskID, t.Status)
}

// runStage —— stage 内执行策略（对照 Zadig stage.go runStage → CustomStageCtl.Run）：
//   - Parallel=true：jobs 全员齐飞（goroutine + WaitGroup），等全部落地再聚合
//   - Parallel=false：逐个放行；任一 job 命中刹车态 → 剩余未启动的直接 skipped
// 刹车语义：不再起新的，已在跑的让它跑完（并行模式天然如此：WaitGroup 等全员）。
func runStage(tc *taskCtx, t *models.WorkflowTask, st *models.StageTask) {
	if st.Parallel {
		var wg sync.WaitGroup
		for _, jt := range st.Jobs {
			if jt.Status == models.StatusPassed {
				continue // T1.8 整任务重试：passed job 原样保留不重跑（retried stage 里混着 passed 子树）
			}
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
		if jt.Status == models.StatusPassed {
			continue // 同上：重试场景下 passed job 不重跑
		}
		if stageBraked(st) {
			jt.Status = models.StatusSkipped // 不再起新的：未启动的直接置 skipped
			tc.ack(t)
			continue
		}
		runJobTask(tc, t, st, jt)
	}
}

// stageGated —— M3 闸门判断（对照 Zadig waitForManualExec 的三个前置）：
// 启用了手动执行、且人还没放行过（Executed 由 release 端点翻 true）
func stageGated(st *models.StageTask) bool {
	return st.ManualExec != nil && st.ManualExec.Enabled && !st.ManualExec.Executed
}

// stageBraked —— 串行模式下刹车判断：stage 内存在"硬失败"的 job。
// T1.9：ignore 策略的失败 job 不算硬失败——记红但继续下一个 job。
func stageBraked(st *models.StageTask) bool {
	for _, jt := range st.Jobs {
		if jt.Status == models.StatusFailed && !isIgnorePolicy(jt) {
			return true
		}
		if jt.Status == models.StatusCancelled { // 取消是最高指令，任何策略都刹
			return true
		}
	}
	return false
}

// stageHasHardFailure —— stage 级刹车依据（对照 stageBraked 的 stage 间版本）
func stageHasHardFailure(st *models.StageTask) bool {
	for _, jt := range st.Jobs {
		if (jt.Status == models.StatusFailed && !isIgnorePolicy(jt)) || jt.Status == models.StatusCancelled {
			return true
		}
	}
	return false
}

// isIgnorePolicy —— 该 job 的失败是否走 ignore 放行
func isIgnorePolicy(jt *models.JobTask) bool {
	return jt.Spec != nil && jt.Spec.ErrorPolicy == models.ErrorPolicyIgnore
}

// runJobTask —— 单个 job 的状态机封装（对照 Zadig jobcontroller.job.Run）。
// 三段式编排：Run（K8s 生命周期+live 步骤状态）→ SaveInfo（日志切分+junit 门禁+步骤收口）
// → Clean（删执行物）。job 级失败 > 步骤级失败（门禁翻红）> passed。
// ⚠️ jt 字段写全部在锁内：并行时兄弟 goroutine 的 ack 正在整树序列化，
// 裸写 jt.Status = 数据竞争。K8s 侧调用（ctl 内部）不碰共享内存的放锁外（ctl 自己管锁的粒度）。
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
	if jt.JobType != models.JobTypeDeploy { // deploy 无执行物：不占用 K8s Job 名（Recover 泄漏清理按名删，不能给它发号）
		jt.K8sJobName = jobName(t.WorkflowName, t.TaskID, st.Name, jt.Name)
	}
	// T1.3/T2.2 变量渲染：生成 driver 前（freestyle）/patch 前（deploy）把该 job 的
	// spec 渲染掉（{{.global.x}} / {{.job.key}}——deploy 的 image 可引用上游 output）。
	// 渲染在锁内的原因：BuildVarsCtx 读 t.GlobalContext，并行 stage 里兄弟 goroutine 的
	// 终态收割会并发写它——读写都圈进同一把锁。渲染失败 = 规格非法，job 直接红，
	// 不浪费一次 K8s 调度（与 GenerateDriverScript 的生成期报错同一哲学）。
	ctx := BuildVarsCtx(t)
	var renderErr error
	if jt.Spec != nil {
		renderErr = RenderVars(jt.Spec, ctx)
	}
	if renderErr == nil && jt.DeploySpec != nil {
		renderErr = RenderVars(jt.DeploySpec, ctx)
	}
	if renderErr != nil {
		jt.Status = models.StatusFailed
		jt.Error = fmt.Sprintf("render vars: %v", renderErr)
		ack(t)
		tc.mu.Unlock()
		return
	}
	ack(t)
	tc.mu.Unlock()
	log.Printf("engine: task %s/%d job %q -> %s", t.WorkflowName, t.TaskID, jt.Name, jt.K8sJobName)

	ctl := newJobCtl(jt)
	runErr := ctl.Run(tc, jt) // K8s 级：创建/轮询/退出码/超时（期间 live 刷步骤状态）
	infoErr := error(nil)
	if !errors.Is(runErr, errCancelled) {
		// 取消路径跳过 SaveInfo：finalizeSteps 会把 running 步骤翻成 failed（"did not
		// finish"），与取消语义打架；步骤收口由下面的 cancelled 分支接管。日志不切分
		// 是接受的代价（Job 已删，档案里保留 live 期间的步骤状态就够诊断）。
		infoErr = ctl.SaveInfo(tc, jt) // 步骤级：日志切分、junit 门禁、created→skipped
	}
	ctl.Clean(tc, jt) // 资源与数据分离：执行物销毁，档案长存

	tc.mu.Lock()
	switch {
	case errors.Is(runErr, errCancelled):
		// T1.5：取消——job 与步骤收口 cancelled/skipped，不走 failed 语义
		jt.Status = models.StatusCancelled
		jt.Error = "cancelled by user"
		for _, sp := range jt.Steps {
			switch sp.Status {
			case models.StatusRunning:
				sp.Status = models.StatusCancelled
				sp.Error = "cancelled by user"
			case models.StatusCreated:
				sp.Status = models.StatusSkipped
			}
		}
	case runErr != nil:
		jt.Status = models.StatusFailed
		jt.Error = runErr.Error()
	case infoErr != nil: // 退出码 0 但步骤翻红（junit 门禁）：job 仍然算失败
		jt.Status = models.StatusFailed
		jt.Error = infoErr.Error()
	default:
		jt.Status = models.StatusPassed
		harvestOutputs(t, jt) // T1.3：只收成功 job 的输出（失败 job 的输出半途而废，不可信）
	}
	ack(t)
	tc.mu.Unlock()
}

// harvestOutputs —— job 终态收割：jt.Outputs → t.GlobalContext（"<job>.<output>" 点分键，
// 对照 Zadig writeOutputs 把 job 输出写进 workflowCtx）。存全局而非只存 jt.Outputs 的
// 原因：渲染上下文只有 GlobalContext 一个真相源（BuildVarsCtx 单参），jt.Outputs 是
// 分界面展示用的 job 级副本。下游 job 的 Run 天然拿到——它的脚本生成在收割之后。
func harvestOutputs(t *models.WorkflowTask, jt *models.JobTask) {
	if len(jt.Outputs) == 0 {
		return
	}
	if t.GlobalContext == nil {
		t.GlobalContext = map[string]string{}
	}
	for k, v := range jt.Outputs {
		t.GlobalContext[jt.Name+"."+k] = v
	}
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

// runJob 的完整生命周期（创建→轮询→清理）已搬进 jobctl.go 的 K8sJobCtl（Run/SaveInfo/Clean 三段式）。

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

// finish —— 终态收口：置状态、记时间、最后一次 ack、IM 通知（fire-and-forget）。
// ⚠️ 通知只挂在这条引擎自然终态路径上；CancelTree 的纯数据收口（T1.5）不走这里
// （没有 goroutine 在场，通知让位给调用方 API——学习版明确接受这个不对称）。
func finish(t *models.WorkflowTask, status models.Status, errMsg string) {
	t.Status = status
	t.EndTime = time.Now().Unix()
	t.Error = errMsg
	ack(t)
	notifyOnFinish(t)
}
