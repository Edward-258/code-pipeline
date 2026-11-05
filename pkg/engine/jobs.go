package engine

import (
	"errors"
	"fmt"
	"log"

	"pipeline/pkg/engine/cancel"
	"pipeline/pkg/engine/exec"
	"pipeline/pkg/k8s"
	"pipeline/pkg/models"
)

// job 级状态机（对照 Zadig jobcontroller/job.go 的 Run）：变量渲染 → 执行器
// 三件套（Run→SaveInfo→Clean）→ 终态翻译 → 输出收割。

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
		jt.K8sJobName = k8s.JobName(t.WorkflowName, t.TaskID, st.Name, jt.Name)
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

	// 执行器视图：Mu/Ack 注入 taskCtx 的同一把锁与同一个整树写回——并行 job 的
	// 互斥协议由 engine 单点定义，exec 消费不另立（Ack 是无锁版：调用方必须已持 Mu）
	ec := &exec.Ctx{Client: tc.client, NS: tc.ns, Task: t, Mu: &tc.mu, Ack: ack, CancelCh: tc.cancelCh}
	ctl := exec.NewJobCtl(jt)
	runErr := ctl.Run(ec, jt) // K8s 级：创建/轮询/退出码/超时（期间 live 刷步骤状态）
	infoErr := error(nil)
	if !errors.Is(runErr, cancel.ErrCancelled) {
		// 取消路径跳过 SaveInfo：finalizeSteps 会把 running 步骤翻成 failed（"did not
		// finish"），与取消语义打架；步骤收口由下面的 cancelled 分支接管。日志不切分
		// 是接受的代价（Job 已删，档案里保留 live 期间的步骤状态就够诊断）。
		infoErr = ctl.SaveInfo(ec, jt) // 步骤级：日志切分、junit 门禁、created→skipped
	}
	ctl.Clean(ec, jt) // 资源与数据分离：执行物销毁，档案长存

	tc.mu.Lock()
	switch {
	case errors.Is(runErr, cancel.ErrCancelled):
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
