package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"pipeline/pkg/engine"
	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// 原生审批（T3.4，对照 Zadig POST /v4/workflowtask/approve 的极简版）：
// 给 pause 中的 stage 记一笔审批 → 满足策略翻 Executed → 正常入队续跑。
// 老图纸（无 approvers）走同一端点 = 一次审批即放行（any + 空名单 ≡ 原 release）。

type approveBody struct {
	Stage    string `json:"stage"`    // 可选：默认取第一个 pause 中的 stage
	Approver string `json:"approver"` // 自由文本（硬约束 #2：不做身份系统）
	Comment  string `json:"comment,omitempty"`
}

// ApproveTask POST /api/tasks/:name/:id/approve
func ApproveTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task id must be a number"})
		return
	}
	var body approveBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.Approver == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "approver is required (free text)"})
		return
	}

	// 与 ReleaseStage 同一把锁：检查 pause → 记审批 → 翻闸门 → 落库圈成临界区，
	// 防手抖双击放出两个续跑 goroutine（审批与 release 抢同一个闸门位，必须互斥）
	releaseMu.Lock()
	defer releaseMu.Unlock()

	t, err := repo.FindTask(c.Param("name"), id)
	if err == repo.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var st *models.StageTask
	for _, s := range t.Stages {
		if body.Stage != "" && s.Name != body.Stage {
			continue
		}
		if s.Status == models.StatusPause && s.ManualExec != nil && s.ManualExec.Enabled && !s.ManualExec.Executed {
			st = s
			break
		}
	}
	if st == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "no stage is waiting for approval (status!=pause or already executed)"})
		return
	}
	me := st.ManualExec

	// 记审批：同名重批覆盖 comment（人手抖重发不产生两条记录）
	for _, a := range me.Approvals {
		if a.Approver == body.Approver {
			a.Comment, a.Time = body.Comment, time.Now().Unix()
			goto recorded
		}
	}
	me.Approvals = append(me.Approvals, &models.Approval{Approver: body.Approver, Comment: body.Comment, Time: time.Now().Unix()})
recorded:

	if !st.ManualExec.Satisfied() {
		if err := repo.UpdateTask(t); err != nil { // 未满足也要落库（审批记录是档案）
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"satisfied": false,
			"approvals": len(me.Approvals),
			"policy":    policyName(me),
			"stage":     st.Name,
		})
		return
	}

	me.Executed = true // ⭐ 闸门放行位：续跑的 runStages 凭它跳过闸门（与 release 同一单点）
	if err := repo.UpdateTask(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := engine.Enqueue(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"satisfied": true,
		"policy":    policyName(me),
		"stage":     st.Name,
		"approvals": len(me.Approvals),
	})
}

func policyName(me *models.ManualExec) string {
	if me.Policy == models.ApprovalPolicyAll {
		return fmt.Sprintf("all(%d)", len(me.Approvers))
	}
	return "any"
}
