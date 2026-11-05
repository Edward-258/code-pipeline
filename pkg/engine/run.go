package engine

import (
	"fmt"
	"log"
	"sync"
	"time"

	"k8s.io/client-go/kubernetes"

	"pipeline/pkg/config"
	"pipeline/pkg/engine/cancel"
	"pipeline/pkg/k8s"
	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// engine 包的任务执行核心按职责分四个文件（同一状态机的四层切面）：
//   run.go     —— Run 入口：装配环境、注册取消、panic 兜底（本文件）
//   stages.go  —— stage 级状态机：闸门/取消检查点/聚合/刹车 + 调度策略
//   jobs.go    —— job 级状态机：变量渲染、执行器三件套编排、输出收割
//   finish.go  —— 终态收口：markRestSkipped/jobsWorst/finish（含 IM 通知）

// taskCtx —— 任务运行期共享环境。
// mu 保 ack 互斥：并行 job 各自回写时，整棵树的字段写 + BSON 序列化 + ReplaceOne
// 必须串行（Zadig 同款问题同款解：jobcontroller 并行跑 job，ack 全量写）。
type taskCtx struct {
	client   kubernetes.Interface
	ns       string
	t        *models.WorkflowTask // ack 的对象（整树写回的目标）
	mu       sync.Mutex
	cancelCh <-chan struct{} // T1.5 取消信号（close 广播；nil = 永不触发）
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
	cancelCh, unregister := cancel.Register(t.ID)
	defer unregister()

	cfg := config.Load()
	client, err := k8s.NewClient(cfg.K8SContext)
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

// ack —— 任务每前进一步，整棵树写回 Mongo（对照 Zadig 的 ack() 全量写）。
// 也是注入给 exec.Ctx.Ack 的那一把（见 jobs.go runJobTask）——整树写回的互斥协议
// 归 engine 单点定义：持锁内才许写共享树字段，exec 侧经 Ctx.Mu/Ack 消费同一协议。
func ack(t *models.WorkflowTask) {
	if err := repo.UpdateTask(t); err != nil {
		log.Printf("engine: ack task %s/%d failed: %v", t.WorkflowName, t.TaskID, err)
	}
}
