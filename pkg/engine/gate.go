package engine

import (
	"fmt"
	"sync"
	"time"

	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// 闸门与审批域（M3 durable 暂停的放行 + T3.4 原生审批）——独立文件 = 独立锁面：
// gateMu 只保护"闸门位翻转 → 落库 → 入队"这条临界序列，生命周期操作（ops.go）
// 与状态机（run/stages）都不碰这把锁。release 与 approve 抢同一个闸门位，必须互斥。

// gateMu 把"检查 pause → 翻闸门 → 落库 → 入队"圈成临界区（防手抖双击放出两个续跑
// goroutine）。
var gateMu sync.Mutex

// ---------- M3：durable 暂停的放行（灵魂端点的领域侧） ----------

// ReleaseGate —— 放行暂停中的 stage：置 ManualExec.Executed=true → 落库 → 重新入队
// 续跑（pause → queued → 派发器 → running；pause 不占 running 名额的另一半：续跑也
// 排队，不插队）。⚠️ 这里只翻闸门位；task 的 pause→running 翻转只发生在 engine.Run
// （单一处，别做两处）。
// ⚠️ 整棵树的加载圈进 gateMu 之内：api 层"先读后调"的旧形态让"检查+写回"跑在过期
// 副本上——双击 release 的第二发会把第一发刚写进的 queued 盖回 pause（任务卡死），
// 读改写必须整体临界区化。
func ReleaseGate(workflowName string, taskID int64, stageName string) (*models.WorkflowTask, error) {
	gateMu.Lock()
	defer gateMu.Unlock()

	t, err := repo.FindTask(workflowName, taskID)
	if err != nil {
		return nil, err // repo.ErrNotFound 透传，api 层翻 404
	}
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
	return t, nil
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
// 树加载同 ReleaseGate 圈进锁内（读改写临界区化）。
func RecordApproval(workflowName string, taskID int64, stageName, approver, comment string) (*ApprovalResult, error) {
	gateMu.Lock()
	defer gateMu.Unlock()

	t, err := repo.FindTask(workflowName, taskID)
	if err != nil {
		return nil, err
	}
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

// ---------- 树内查找（闸门域专用；通用查找件在 ops.go） ----------

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
