package exec

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"pipeline/pkg/engine/cancel"
	"pipeline/pkg/k8s"
	"pipeline/pkg/models"
	"pipeline/pkg/step"
)

// freestyle job 的 K8s 生命周期执行器（原 engine/jobctl.go 随模块化迁入 exec 子包）。

const (
	jobTimeout = 10 * time.Minute // 兜底超时（T1.9 per-job 化的默认值）
)

// K8sJobCtl —— freestyle job 执行器（无状态，全部上下文走 Ctx）
type K8sJobCtl struct{}

// pollInterval —— 轮询节奏（决断 #2：轮询不上 watch）。var 而非 const：单测缩短节拍，
// 别让表测陪跑 2s 一拍的真节奏。
var pollInterval = 2 * time.Second

// Run —— T1.4 重试包装：单次尝试失败且重试预算未用完 → 现场重置再来。
// T1.9 预算来源由策略决定：stop=0（显式刹车不重试）/ retry 与未设策略=spec.Retry
// （旧图纸的 T1.4 语义保持）/ ignore=spec.Retry（先试够再放行）。
func (K8sJobCtl) Run(c *Ctx, jt *models.JobTask) error {
	maxRetry := 0
	if jt.Spec != nil {
		switch jt.Spec.ErrorPolicy {
		case models.ErrorPolicyStop:
			maxRetry = 0
		case models.ErrorPolicyIgnore, models.ErrorPolicyRetry:
			maxRetry = jt.Spec.Retry
		default: // 未设策略（"" / 拼错值）：T1.4 老语义兜底
			maxRetry = jt.Spec.Retry
		}
	}
	for attempt := 0; ; attempt++ {
		err := runK8sJobOnce(c, jt)
		if err == nil {
			return nil
		}
		// 取消是最高指令：不重试，原样上抛（重试循环会在重建 Job 后下一拍才感知，
		// 白费一次 create/delete——短路掉）
		if errors.Is(err, cancel.ErrCancelled) {
			return err
		}
		if attempt >= maxRetry {
			return err
		}
		// 重试：计数 ack + 清上一轮现场。旧 Job 的清理交给 runK8sJobOnce 开头的幂等 Delete
		c.Mu.Lock()
		jt.Retry++
		resetForRetry(jt)
		c.Ack(c.Task)
		c.Mu.Unlock()
		log.Printf("exec: task %s/%d job %q failed (%v), retry %d/%d", c.Task.WorkflowName, c.Task.TaskID, jt.Name, err, jt.Retry, maxRetry)
	}
}

// resetForRetry —— 重试前清空上一轮现场：步骤树回 created、产出与门禁结果作废。
// 新 Pod 全新开始，banner 重放要求步骤状态可重新驱动（START 只认 created、END 只认
// running）；旧日志/旧输出不能代表最后一轮，不重置就是化石数据。
func resetForRetry(jt *models.JobTask) {
	for _, st := range jt.Steps {
		st.Status = models.StatusCreated
		st.StartTime, st.EndTime = 0, 0
		st.Error = ""
		st.Log = ""
		st.TestResult = nil
	}
	jt.Outputs = nil
}

// runK8sJobOnce —— 单次尝试的 K8s 生命周期：创建 → 轮询到终态；期间每拍顺手刮
// Pod 日志维护步骤状态（live 分界面）。返回 job 级错误（创建失败/退出码/超时）。
func runK8sJobOnce(c *Ctx, jt *models.JobTask) error {
	// 幂等：残留同名 Job 先清（M4 单 job 重试 / 崩溃恢复扫清脏资源，run.go 的老规矩）
	_ = c.Client.BatchV1().Jobs(c.NS).Delete(context.TODO(), jt.K8sJobName, k8s.DeleteOpts())

	// 推送目标解析（registry.go）：每次 attempt 一次——凭证不落档案，retry 拿到的是
	// 仓库记录的当前值。规格非法（含 registry 引用不存在）在这里报，不浪费一次调度
	steps, err := resolveRegistrySteps(jt.Spec.EffectiveSteps())
	if err != nil {
		return err
	}
	// 远程主机解析（hosts.go）：同一时序——私钥只在 host 集合与驱动脚本里，档案只存名字
	hosts, err := resolveHosts(jt.Spec.Hosts)
	if err != nil {
		return err
	}
	script, err := step.GenerateDriverScript(steps, hosts)
	if err != nil {
		return err
	}
	if _, err := c.Client.BatchV1().Jobs(c.NS).Create(context.TODO(), k8s.BuildJob(c.NS, c.Task, jt, script), metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create k8s job: %w", err)
	}

	// T1.9 超时 per-job 化（默认 10 分钟 = jobTimeout 常量语义）
	timeout := jobTimeout
	if jt.Spec != nil && jt.Spec.TimeoutMinutes > 0 {
		timeout = time.Duration(jt.Spec.TimeoutMinutes) * time.Minute
	}
	deadline := time.Now().Add(timeout)
	for {
		// T1.5 取消检查点（每拍）：感知即删 Job（K8s 侧立即停）→ sentinel 上抛
		if cancel.Signalled(c.CancelCh) {
			_ = c.Client.BatchV1().Jobs(c.NS).Delete(context.TODO(), jt.K8sJobName, k8s.DeleteOpts())
			return cancel.ErrCancelled
		}
		job, err := c.Client.BatchV1().Jobs(c.NS).Get(context.TODO(), jt.K8sJobName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("poll k8s job: %w", err)
		}
		// 顺手刮日志：banner → 步骤状态。Pod 没起来拉不到就跳过，下一拍再来
		if logText, lerr := k8s.PodLog(c.Client, c.NS, jt.K8sJobName); lerr == nil {
			c.Mu.Lock()
			if step.ApplyStepBanners(jt, logText, time.Now()) {
				c.Ack(c.Task)
			}
			c.Mu.Unlock()
		}
		if job.Status.Succeeded >= 1 {
			return nil
		}
		if job.Status.Failed >= 1 {
			return fmt.Errorf("exit %s", k8s.PodExitInfo(c.Client, c.NS, jt.K8sJobName))
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout after %s", timeout)
		}
		time.Sleep(pollInterval)
	}
}

// SaveInfo —— 终态收割（稳定探测）：containerd 用两条读线程分别收 stdout/stderr 再拼进
// 同一日志文件，stderr 行可能比 stdout 晚 1~2 秒才落盘。单次等长就收手不够——幕1 首跑
// （2026-11-04）实测节点有负载时 stderr 可超过 ~3s 窗口，boom 步骤的报错行整条丢失。
// 改为连续 2 次等长（首拉 + 2 拍一致 = 3 次同长）才算稳定，上限 ~6s 兜底。
func (K8sJobCtl) SaveInfo(c *Ctx, jt *models.JobTask) error {
	fullLog, _ := k8s.PodLog(c.Client, c.NS, jt.K8sJobName)
	stable := 0
	for i := 0; i < 10 && stable < 2; i++ {
		time.Sleep(600 * time.Millisecond)
		l2, err := k8s.PodLog(c.Client, c.NS, jt.K8sJobName)
		if err != nil {
			break
		}
		if len(l2) == len(fullLog) {
			stable++
		} else {
			stable = 0
			fullLog = l2
		}
	}

	c.Mu.Lock()
	step.ApplyStepBanners(jt, fullLog, time.Now()) // 补最后一拍可能漏掉的 END banner
	step.SliceStepLogs(jt, fullLog)                // 切分要在 finalize 前：AfterRun 读的是切好的 Log
	stepErr := step.FinalizeSteps(jt)              // created→skipped / 悬空 running→failed / AfterRun 门禁 / 截断
	c.Ack(c.Task)
	c.Mu.Unlock()
	if stepErr != "" {
		return fmt.Errorf("%s", stepErr)
	}
	return nil
}

// Clean —— 删 K8s Job（Background 级联，含 Pod）
func (K8sJobCtl) Clean(c *Ctx, jt *models.JobTask) {
	_ = c.Client.BatchV1().Jobs(c.NS).Delete(context.TODO(), jt.K8sJobName, k8s.DeleteOpts())
}
