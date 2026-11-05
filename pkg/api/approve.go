package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"pipeline/pkg/engine"
)

// 原生审批（T3.4，对照 Zadig POST /v4/workflowtask/approve 的极简版）：
// 给 pause 中的 stage 记一笔审批 → 满足策略翻 Executed → 正常入队续跑。
// 老图纸（无 approvers）走同一端点 = 一次审批即放行（any + 空名单 ≡ 原 release）。
// 审批记录/去重/策略判定/放行序列全在 engine.RecordApproval，这里只剩 HTTP 形状。

type approveBody struct {
	Stage    string `json:"stage"`    // 可选：默认取第一个 pause 中的 stage
	Approver string `json:"approver"` // 自由文本（硬约束 #2：不做身份系统）
	Comment  string `json:"comment,omitempty"`
}

// approveTask POST /api/tasks/:name/:id/approve
func approveTask(c *gin.Context) {
	t := taskFromPath(c)
	if t == nil {
		return
	}
	var body approveBody
	if err := c.ShouldBindJSON(&body); err != nil {
		respond(c, http.StatusBadRequest, err.Error())
		return
	}
	if body.Approver == "" {
		respond(c, http.StatusBadRequest, "approver is required (free text)")
		return
	}

	res, err := engine.RecordApproval(t, body.Stage, body.Approver, body.Comment)
	if errors.Is(err, engine.ErrNotWaiting) {
		respond(c, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		respond(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, res)
}
