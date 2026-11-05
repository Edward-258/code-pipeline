package exec

import (
	"log"
	"sync"

	"k8s.io/client-go/kubernetes"

	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// 执行器子包（对照 Zadig jobcontroller 的 JobCtl 注册表）：engine 编排核心通过
// 三件套接口驱动每种 job 类型的生命周期，自己不碰 K8s 细节。
// 依赖方向：exec → step（脚本/日志协议）、k8s（资源 I/O）、engine/cancel（取消
// sentinel）、repo（env 解析默认实现）、models——绝不回 import engine 本体。

// Ctx —— 单个 job 执行期的运行环境（engine.taskCtx 的导出视图）。
// Mu 与 engine 侧 taskCtx.mu 是同一把锁的指针：并行 job 的整树写回互斥
// （Zadig 同款问题同款解）靠指针同一性保证，执行器内不得另立锁。
type Ctx struct {
	Client   kubernetes.Interface
	NS       string
	Task     *models.WorkflowTask // ack 的对象（整树写回的目标）
	Mu       *sync.Mutex
	CancelCh <-chan struct{} // 取消信号（close 广播；nil = 永不触发）
}

// JobCtl —— 对照 Zadig jobcontroller/job.go:44 的 JobCtl 三件套（Run/Clean/SaveInfo）。
// 学习版首发 K8sJobCtl（freestyle）+ DeployJobCtl（deploy）；新 job 类型在 NewJobCtl
// 的 switch 里长出来（对照 Zadig initJobCtl 的 50 个 case——"极具扩展性"的真身就是这张注册表）。
//
// ⚠️ 顺序铁律：Run → SaveInfo → Clean。SaveInfo 要拉 Pod 日志切分/门禁，
// Clean 一删 Pod 日志就没了——先收割再销毁（资源与数据分离）。
type JobCtl interface {
	// Run：创建 K8s Job → 轮询到终态；期间每拍顺手刮 Pod 日志维护步骤状态（live 分界面）。
	// 返回 job 级错误（创建失败/退出码/超时）。
	Run(c *Ctx, jt *models.JobTask) error
	// SaveInfo：终态善后：最后一次全量日志 → banner 补齐 → 日志切分 → 步骤收口（junit 门禁）。
	// 返回步骤级失败（门禁翻红）——job 级没失败但步骤失败也要把 job 拖红。
	SaveInfo(c *Ctx, jt *models.JobTask) error
	// Clean：删 K8s Job（Background 级联，含 Pod）。
	Clean(c *Ctx, jt *models.JobTask)
}

// NewJobCtl —— job 类型 → 执行器注册表
func NewJobCtl(jt *models.JobTask) JobCtl {
	switch jt.JobType {
	case models.JobTypeDeploy:
		return DeployJobCtl{}
	default:
		return K8sJobCtl{}
	}
}

// ackTask —— 任务每前进一步，整棵树写回 Mongo（engine.ack 的同构副本：exec 不
// import engine，直呼 repo；engine 侧的 ack 走 taskCtx.mu 互斥，执行器内的调用点
// 都已在调用方锁内）
func ackTask(t *models.WorkflowTask) {
	if err := repo.UpdateTask(t); err != nil {
		log.Printf("exec: ack task %s/%d failed: %v", t.WorkflowName, t.TaskID, err)
	}
}
