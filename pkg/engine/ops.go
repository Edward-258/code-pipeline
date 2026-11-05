package engine

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"pipeline/pkg/engine/cancel"
	"pipeline/pkg/k8s"
	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// 任务运维操作面：clone / retry / cancel / release / approve / log。
// 与 run.go（状态机）、trigger.go（触发）分立——这是 API 层的领域后端，
// handler 只剩 HTTP 形状翻译（参数解析、状态码映射），不再碰 K8s/cancel 注册表。

// sentinel 错误：API 层 errors.Is 翻译成 404/409，其余一律 500
var (
	ErrNoSnapshot    = errors.New("task has no workflow snapshot (legacy archive?)")
	ErrTaskFinal     = errors.New("task is already finished")
	ErrTaskNotFinal  = errors.New("only finished tasks can be retried")
	ErrStageNotFound = errors.New("stage not found")
	ErrJobNotFound   = errors.New("stage/job not found")
	ErrNotWaiting    = errors.New("stage is not waiting for release")
)

// gateMu 把"检查 pause → 翻闸门 → 落库 → 入队"圈成临界区（防手抖双击放出两个续跑
// goroutine）。release 与 approve 抢同一个闸门位，必须互斥。
var gateMu sync.Mutex

// ---------- T1.7：克隆（对照 Zadig clone 端点） ----------

// CloneTask —— 一键原样重跑（历史即模板）：快照再快照（WorkflowArgs 是当时的完整
// 图纸快照，BuildTask 等价构造新号任务，两层快照互不共享指针，各是各的档案）、
// 全新 GlobalContext（不继承旧任务收割的 outputs——它们由新一轮运行重新产生）、
// 全新 created 树。克隆任何状态的任务都合法。
func CloneTask(src *models.WorkflowTask) (*models.WorkflowTask, error) {
	if src.WorkflowArgs == nil {
		return nil, ErrNoSnapshot
	}
	return spawn(src.WorkflowArgs, "clone:"+src.ID)
}

// ---------- T1.8：整任务重试（对照 Zadig retry 端点） ----------

// RetryTask —— 从失败处续跑：仅终态任务可用（running/queued/pause 有看护人或排队
// 语义，返回 ErrTaskNotFinal）。树内手术见 resetForRetryRun；GlobalContext 保留：
// 上游 passed job 的 outputs 继续可用，失败 job 重跑后重新收割。
func RetryTask(t *models.WorkflowTask) (*models.WorkflowTask, error) {
	if !models.StatusFinished(t.Status) {
		return nil, fmt.Errorf("%w (task is %s)", ErrTaskNotFinal, t.Status)
	}
	resetForRetryRun(t)
	if err := repo.UpdateTask(t); err != nil { // 重置后的树先落库（入队前档案先就位）
		return nil, err
	}
	if err := Enqueue(t); err != nil {
		return nil, err
	}
	return t, nil
}

// resetForRetryRun —— 整任务重试的树内手术（纯内存，可单测）。
// 与单 job 重试（exec.resetForRetry）的分工：那是 job 内一步重来的现场清理，这是任务级
// 选择性复活——passed 子树不动，其余全部拉回 created。
func resetForRetryRun(t *models.WorkflowTask) {
	for _, st := range t.Stages {
		if st.Status == models.StatusPassed {
			continue // passed stage 整棵保留（对照 runStages 的跳过逻辑）
		}
		st.Status = models.StatusCreated
		st.Error = ""
		st.EndTime = 0
		// StartTime 保留：它是"第一次触到这里"的历史（Run 的 StartTime 首标语义同源）
		for _, jt := range st.Jobs {
			if jt.Status == models.StatusPassed {
				continue // passed job 不重跑（runStage 的跳过逻辑）
			}
			jt.Status = models.StatusCreated
			jt.Error = ""
			jt.Retry = 0 // 重试预算重置
			for _, sp := range jt.Steps {
				sp.Status = models.StatusCreated
				sp.StartTime, sp.EndTime = 0, 0
				sp.Error = ""
				sp.Log = ""
				sp.TestResult = nil
			}
		}
	}
	t.Status = models.StatusCreated // 入队时 Enqueue 会翻 queued
	t.EndTime = 0
	t.Error = ""
}

// ---------- T1.5：取消（信号广播 / 纯数据收口的两条路径封装） ----------

// CancelTask —— 取消语义单点（对照 Zadig CancelWorkflowTaskV4）：
//   - 有看护 goroutine（注册表有号）：close 信号广播，看护人在一拍轮询内收口
//     → 返回 accepted=true（API 侧 202）
//   - 无看护人（pause 稳态 / queued / 孤儿幸存）：Tree 纯数据收口 + 落库
//     → 返回 accepted=false（API 侧 200）
//
// 终态任务返回 ErrTaskFinal（API 侧 409：终态不可取消，幂等语义交还给调用方）。
func CancelTask(t *models.WorkflowTask) (accepted bool, err error) {
	if models.StatusFinished(t.Status) {
		return false, fmt.Errorf("%w (%s)", ErrTaskFinal, t.Status)
	}
	if cancel.Request(t.ID) {
		return true, nil
	}
	cancel.Tree(t)
	return false, repo.UpdateTask(t)
}

// ---------- M3：durable 暂停的放行（灵魂端点的领域侧） ----------

// ReleaseGate —— 放行暂停中的 stage：置 ManualExec.Executed=true → 落库 → 重新入队
// 续跑（pause → queued → 派发器 → running；pause 不占 running 名额的另一半：续跑也
// 排队，不插队）。⚠️ 这里只翻闸门位；task 的 pause→running 翻转只发生在 engine.Run
// （单一处，别做两处）。
func ReleaseGate(t *models.WorkflowTask, stageName string) (*models.StageTask, error) {
	gateMu.Lock()
	defer gateMu.Unlock()

	st := findStage(t, stageName)
	if st == nil {
		return nil, ErrStageNotFound
	}
	if st.Status != models.StatusPause || st.ManualExec == nil || st.ManualExec.Executed {
		return nil, fmt.Errorf("%w: stage %q is not waiting for release (status=%s)", ErrNotWaiting, st.Name, st.Status)
	}
	if err := passGate(t, st); err != nil {
		return nil, err
	}
	return st, nil
}

// passGate —— 放行序列单点：翻闸门位 → 落库 → 正常入队（release/approve 共用；
// 调用方必须已持 gateMu）。闸门放行位 Executed 是续跑的 runStages 凭它跳过闸门的钥匙。
func passGate(t *models.WorkflowTask, st *models.StageTask) error {
	st.ManualExec.Executed = true
	if err := repo.UpdateTask(t); err != nil {
		return err
	}
	return Enqueue(t)
}

// ---------- T3.4：原生审批（对照 Zadig POST .../approve 的极简版） ----------

// ApprovalResult —— 审批动作的结果视图（API 侧直接 JSON 化）
type ApprovalResult struct {
	Satisfied bool   `json:"satisfied"`
	Approvals int    `json:"approvals"`
	Policy    string `json:"policy"`
	Stage     string `json:"stage"`
}

// RecordApproval —— 给 pause 中的 stage 记一笔审批：同名重批覆盖 comment（人手抖
// 重发不产生两条记录）→ 满足策略自动放行（与 release 同锁同闸门位）。
// stageName 可空 = 默认取第一个等审批的 stage。老图纸（无 approvers）= 一条审批即过
// （ManualExec.Satisfied 的空名单语义 ≡ release）。未满足也落库（审批记录是档案）。
func RecordApproval(t *models.WorkflowTask, stageName, approver, comment string) (*ApprovalResult, error) {
	gateMu.Lock()
	defer gateMu.Unlock()

	st := findWaitingStage(t, stageName)
	if st == nil {
		return nil, fmt.Errorf("%w (status!=pause or already executed)", ErrNotWaiting)
	}
	me := st.ManualExec

	// 记审批：同名重批覆盖 comment
	for _, a := range me.Approvals {
		if a.Approver == approver {
			a.Comment, a.Time = comment, time.Now().Unix()
			goto recorded
		}
	}
	me.Approvals = append(me.Approvals, &models.Approval{Approver: approver, Comment: comment, Time: time.Now().Unix()})
recorded:

	res := &ApprovalResult{Policy: approvalPolicyName(me), Stage: st.Name, Approvals: len(me.Approvals)}
	if !me.Satisfied() {
		if err := repo.UpdateTask(t); err != nil {
			return nil, err
		}
		return res, nil
	}
	if err := passGate(t, st); err != nil {
		return nil, err
	}
	res.Satisfied = true
	return res, nil
}

// approvalPolicyName —— 策略的人类可读名（all 带名单长度）
func approvalPolicyName(me *models.ManualExec) string {
	if me.Policy == models.ApprovalPolicyAll {
		return fmt.Sprintf("all(%d)", len(me.Approvers))
	}
	return "any"
}

// ---------- CI/CD 分步日志（日志流=状态信道的对外分界面） ----------

// TaskLog —— 分步日志：活任务透传 Pod 日志尾部（live 分界面），终态回档案渲染
// 分步视图（banner 切分结果 + TestResult 摘要）。step 可空 = 该 job 全部步骤。
func TaskLog(t *models.WorkflowTask, stage, job, step string) (string, error) {
	jt := findJob(t, stage, job)
	if jt == nil {
		return "", ErrJobNotFound
	}

	// 活的：K8s Job 还在 → 实时尾部
	if txt, live := k8s.TailJobLog(jt.K8sJobName); live {
		return txt, nil
	}

	// 死的：档案里的分步日志（banner 切分结果，pkg/step/logs.go）
	var b strings.Builder
	for _, sp := range jt.Steps {
		if step != "" && sp.Name != step {
			continue
		}
		fmt.Fprintf(&b, "=== step %s [%s] %s", sp.Name, sp.Type, sp.Status)
		if sp.Error != "" {
			fmt.Fprintf(&b, " — %s", sp.Error)
		}
		b.WriteString(" ===\n")
		if sp.TestResult != nil {
			fmt.Fprintf(&b, "tests=%d failures=%d errors=%d skipped=%d time=%.2fs\n",
				sp.TestResult.Tests, sp.TestResult.Failures, sp.TestResult.Errors, sp.TestResult.Skipped, sp.TestResult.Time)
		}
		if sp.Log != "" {
			b.WriteString(sp.Log)
			b.WriteString("\n")
		}
	}
	return b.String(), nil
}

// ---------- 树内查找（release/approve/log 共用） ----------

// findStage —— 按名精确查找 stage
func findStage(t *models.WorkflowTask, name string) *models.StageTask {
	for _, st := range t.Stages {
		if st.Name == name {
			return st
		}
	}
	return nil
}

// findJob —— stage/job 双层查找
func findJob(t *models.WorkflowTask, stage, job string) *models.JobTask {
	for _, st := range t.Stages {
		if st.Name != stage {
			continue
		}
		for _, jt := range st.Jobs {
			if jt.Name == job {
				return jt
			}
		}
	}
	return nil
}

// findWaitingStage —— 找等放行的 stage：stageName 可空 = 第一个 pause 且闸门未放的；
// 指定名字则精确匹配（找不到同样返回 nil）
func findWaitingStage(t *models.WorkflowTask, stageName string) *models.StageTask {
	for _, st := range t.Stages {
		if stageName != "" && st.Name != stageName {
			continue
		}
		if st.Status == models.StatusPause && st.ManualExec != nil && st.ManualExec.Enabled && !st.ManualExec.Executed {
			return st
		}
	}
	return nil
}
