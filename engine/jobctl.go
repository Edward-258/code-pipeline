package engine

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"pipeline/models"
)

// JobCtl —— 对照 Zadig jobcontroller/job.go:44 的 JobCtl 三件套（Run/Clean/SaveInfo）。
// 学习版首发只有 K8sJobCtl（freestyle）；未来新 job 类型在 newJobCtl 的 switch 里长出来
// （对照 Zadig initJobCtl 的 50 个 case——"极具扩展性"的真身就是这张注册表）。
//
// ⚠️ 顺序铁律：Run → SaveInfo → Clean。SaveInfo 要拉 Pod 日志切分/门禁，
// Clean 一删 Pod 日志就没了——先收割再销毁（资源与数据分离）。
type JobCtl interface {
	// Run：创建 K8s Job → 轮询到终态；期间每拍顺手刮 Pod 日志维护步骤状态（live 分界面）。
	// 返回 job 级错误（创建失败/退出码/超时）。
	Run(tc *taskCtx, jt *models.JobTask) error
	// SaveInfo：终态善后：最后一次全量日志 → banner 补齐 → 日志切分 → 步骤收口（junit 门禁）。
	// 返回步骤级失败（门禁翻红）——job 级没失败但步骤失败也要把 job 拖红。
	SaveInfo(tc *taskCtx, jt *models.JobTask) error
	// Clean：删 K8s Job（Background 级联，含 Pod）。
	Clean(tc *taskCtx, jt *models.JobTask)
}

type K8sJobCtl struct{}

func newJobCtl(jt *models.JobTask) JobCtl {
	switch jt.JobType {
	default:
		return K8sJobCtl{}
	}
}

func (K8sJobCtl) Run(tc *taskCtx, jt *models.JobTask) error {
	// 幂等：残留同名 Job 先清（M4 单 job 重试 / 崩溃恢复扫清脏资源，run.go 的老规矩）
	_ = tc.client.BatchV1().Jobs(tc.ns).Delete(context.TODO(), jt.K8sJobName, deleteOpts())

	script, err := GenerateDriverScript(jt.Spec.EffectiveSteps()) // 规格非法在这里报，不浪费一次调度
	if err != nil {
		return err
	}
	if _, err := tc.client.BatchV1().Jobs(tc.ns).Create(context.TODO(), buildJob(tc.ns, jt, script), metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create k8s job: %w", err)
	}

	deadline := time.Now().Add(jobTimeout)
	for {
		job, err := tc.client.BatchV1().Jobs(tc.ns).Get(context.TODO(), jt.K8sJobName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("poll k8s job: %w", err)
		}
		// 顺手刮日志：banner → 步骤状态。Pod 没起来拉不到就跳过，下一拍再来
		if logText, lerr := podLog(tc.client, tc.ns, jt.K8sJobName); lerr == nil {
			tc.mu.Lock()
			if applyStepBanners(jt, logText, time.Now()) {
				ack(tc.t)
			}
			tc.mu.Unlock()
		}
		if job.Status.Succeeded >= 1 {
			return nil
		}
		if job.Status.Failed >= 1 {
			return fmt.Errorf("exit %s", podExitInfo(tc.client, tc.ns, jt.K8sJobName))
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout after %s", jobTimeout)
		}
		time.Sleep(pollInterval)
	}
}

func (K8sJobCtl) SaveInfo(tc *taskCtx, jt *models.JobTask) error {
	// 终态收割（稳定探测）：containerd 用两条读线程分别收 stdout/stderr 再拼进同一日志文件，
	// stderr 行可能比 stdout 晚 1~2 秒才落盘（实测 Job 终态后 ~1.6s 仍可能没到）。
	// 连续两次长度一致视为收完（早退），最多再等 3s 兜底。
	fullLog, _ := podLog(tc.client, tc.ns, jt.K8sJobName)
	for i := 0; i < 5; i++ {
		time.Sleep(600 * time.Millisecond)
		l2, err := podLog(tc.client, tc.ns, jt.K8sJobName)
		if err != nil || len(l2) == len(fullLog) {
			break
		}
		fullLog = l2
	}

	tc.mu.Lock()
	applyStepBanners(jt, fullLog, time.Now()) // 补最后一拍可能漏掉的 END banner
	sliceStepLogs(jt, fullLog)                // 切分要在 finalize 前：AfterRun 读的是切好的 Log
	stepErr := finalizeSteps(jt)              // created→skipped / 悬空 running→failed / AfterRun 门禁 / 截断
	ack(tc.t)
	tc.mu.Unlock()
	if stepErr != "" {
		return fmt.Errorf("%s", stepErr)
	}
	return nil
}

func (K8sJobCtl) Clean(tc *taskCtx, jt *models.JobTask) {
	_ = tc.client.BatchV1().Jobs(tc.ns).Delete(context.TODO(), jt.K8sJobName, deleteOpts())
}
