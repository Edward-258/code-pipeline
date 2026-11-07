package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"pipeline/pkg/engine"
	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// 任务域的全部 HTTP 面（查询/取消/放行/克隆/重试/审批/日志）。领域逻辑全部在
// engine（run/stages/jobs 状态机 + ops.go 运维操作面 + trigger.go 触发），这里只做
// 形状翻译：参数解析、状态码映射。api 包按资源域一文件：workflows/tasks/envs/
// crons/notifies/artifacts/webhooks + router。

// ---------- 共用小件 ----------

// taskFromPath —— "ParseInt(:id) → FindTask → 404/500" 样板的单点：
// 返回 nil 时响应已写出（400/404/500），调用方直接 return。
// 只服务只读端点（getTask/log/clone——clone 不写源档案）；cancel/release/approve/retry
// 这些**写路径**不再预读整树：树加载挪进引擎侧（gateMu 临界区内 / 条件写回守卫），
// 这里只解析 id 形状（taskIDParam），读改写竞态收口在 engine。
func taskFromPath(c *gin.Context) *models.WorkflowTask {
	id, ok := taskIDParam(c)
	if !ok {
		return nil
	}
	t, err := repo.FindTask(c.Param("name"), id)
	if errors.Is(err, repo.ErrNotFound) {
		respond(c, http.StatusNotFound, "task not found")
		return nil
	}
	if err != nil {
		respond(c, http.StatusInternalServerError, err.Error())
		return nil
	}
	return t
}

// taskIDParam —— :id 的形状解析（400 单点）
func taskIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respond(c, http.StatusBadRequest, "task id must be a number")
		return 0, false
	}
	return id, true
}

// respond —— 统一错误响应体 {"error": …}
func respond(c *gin.Context, code int, msg string) {
	c.JSON(code, gin.H{"error": msg})
}

// getTask GET /api/tasks/:name/:id —— 整棵任务树
func getTask(c *gin.Context) {
	if t := taskFromPath(c); t != nil {
		c.JSON(http.StatusOK, t)
	}
}

// listTasks GET /api/tasks/:name?status=&creator=&from=&to=&page=&size=
// T4.1 过滤+分页版：响应包一层 envelope {items,total,page,size}（前端分页三件套）。
// ⚠️ 响应形状从裸数组改为 envelope——jq 消费方须走 .items（验收脚本已存档 tag acceptance-suite）。
func listTasks(c *gin.Context) {
	f := repo.TaskFilter{
		Status:  c.Query("status"),
		Creator: c.Query("creator"),
	}
	f.From, _ = strconv.ParseInt(c.Query("from"), 10, 64)
	f.To, _ = strconv.ParseInt(c.Query("to"), 10, 64)
	f.Page, _ = strconv.ParseInt(c.Query("page"), 10, 64)
	f.Size, _ = strconv.ParseInt(c.Query("size"), 10, 64)
	items, total, f, err := repo.ListTasksFiltered(c.Param("name"), f)
	if err != nil {
		respond(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": f.Page, "size": f.Size})
}

// ---------- T1.5：取消任务（对照 Zadig DELETE /v4/workflowtask/...） ----------

// cancelTask DELETE /api/tasks/:name/:id
// 有看护 goroutine（注册表有号）：close 信号，看护人在一拍轮询内收口 → 202 Accepted；
// 无看护人（created/queued/pause 稳态）：引擎侧纯数据收口（条件写回）→ 200；
// 已终态 / 文档状态在读到写回窗口被翻走：409（终态不可取消 / ErrTaskMoved 重读再试）。
// 两条路径的分派在 engine.CancelTask（树加载在引擎侧）。
func cancelTask(c *gin.Context) {
	id, ok := taskIDParam(c)
	if !ok {
		return
	}
	accepted, t, err := engine.CancelTask(c.Param("name"), id)
	switch {
	case errors.Is(err, repo.ErrNotFound):
		respond(c, http.StatusNotFound, "task not found")
		return
	case errors.Is(err, engine.ErrTaskFinal), errors.Is(err, engine.ErrTaskMoved):
		respond(c, http.StatusConflict, err.Error())
		return
	case err != nil:
		respond(c, http.StatusInternalServerError, err.Error())
		return
	}
	if accepted {
		c.JSON(http.StatusAccepted, gin.H{"accepted": true, "note": "caretaker will collect within one poll"})
		return
	}
	c.JSON(http.StatusOK, t)
}

// ---------- M3：durable 暂停的放行端点（灵魂端点） ----------

// releaseStage POST /api/tasks/:name/:id/stages/:stage/release
// 放行暂停中的 stage：引擎侧（gateMu 临界区内）加载树 → 翻闸门位 → 落库 → 正常入队续跑。
// 404/409 的判据由引擎 sentinel 给出；ErrTaskMoved 现阶段只在 retry/cancel 出现，
// 统一映射防未来 passGate 换条件写回时漏接。
func releaseStage(c *gin.Context) {
	id, ok := taskIDParam(c)
	if !ok {
		return
	}
	t, err := engine.ReleaseGate(c.Param("name"), id, c.Param("stage"))
	switch {
	case errors.Is(err, repo.ErrNotFound):
		respond(c, http.StatusNotFound, "task not found")
		return
	case errors.Is(err, engine.ErrStageNotFound):
		respond(c, http.StatusNotFound, "stage not found")
		return
	case errors.Is(err, engine.ErrNotWaiting), errors.Is(err, engine.ErrTaskMoved):
		respond(c, http.StatusConflict, err.Error())
		return
	case err != nil:
		respond(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, t)
}

// ---------- T1.7/T1.8：克隆与整任务重试（对照 Zadig clone/retry 端点） ----------

// cloneTask POST /api/tasks/:name/:id/clone —— 一键原样重跑（历史即模板，
// 任何状态的任务都合法；快照/发号/入队序列在 engine.CloneTask）。
func cloneTask(c *gin.Context) {
	t := taskFromPath(c)
	if t == nil {
		return
	}
	nt, err := engine.CloneTask(t)
	if errors.Is(err, engine.ErrNoSnapshot) {
		respond(c, http.StatusInternalServerError, err.Error())
		return
	}
	if err != nil {
		respond(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, nt)
}

// retryTask POST /api/tasks/:name/:id/retry —— 从失败处续跑：passed 子树不动，
// failed/cancelled/skipped 刹车残留复活（树内手术在 engine.RetryTask）；
// 非终态 / 双击竞态（条件写回未命中）：409。
func retryTask(c *gin.Context) {
	id, ok := taskIDParam(c)
	if !ok {
		return
	}
	t, err := engine.RetryTask(c.Param("name"), id)
	switch {
	case errors.Is(err, repo.ErrNotFound):
		respond(c, http.StatusNotFound, "task not found")
		return
	case errors.Is(err, engine.ErrTaskNotFinal), errors.Is(err, engine.ErrTaskMoved):
		respond(c, http.StatusConflict, err.Error())
		return
	case err != nil:
		respond(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, t)
}

// ---------- 步骤化日志（CI/CD）：任务在跑透传 Pod 日志尾部，已结束回档案分步日志 ----------

// getTaskLog GET /api/tasks/:name/:id/log?stage=S&job=J[&step=X]
func getTaskLog(c *gin.Context) {
	t := taskFromPath(c)
	if t == nil {
		return
	}
	txt, err := engine.TaskLog(t, c.Query("stage"), c.Query("job"), c.Query("step"))
	if errors.Is(err, engine.ErrJobNotFound) {
		respond(c, http.StatusNotFound, "stage/job not found")
		return
	}
	if err != nil {
		respond(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.String(http.StatusOK, txt)
}

// ---------- T3.4：原生审批（对照 Zadig POST .../approve 的极简版；领域侧在 engine.RecordApproval） ----------

type approveBody struct {
	Stage    string `json:"stage"`    // 可选：默认取第一个 pause 中的 stage
	Approver string `json:"approver"` // 自由文本（硬约束 #2：不做身份系统）
	Comment  string `json:"comment,omitempty"`
}

// approveTask POST /api/tasks/:name/:id/approve
func approveTask(c *gin.Context) {
	id, ok := taskIDParam(c)
	if !ok {
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

	res, err := engine.RecordApproval(c.Param("name"), id, body.Stage, body.Approver, body.Comment)
	switch {
	case errors.Is(err, repo.ErrNotFound):
		respond(c, http.StatusNotFound, "task not found")
		return
	case errors.Is(err, engine.ErrNotWaiting), errors.Is(err, engine.ErrTaskMoved):
		respond(c, http.StatusConflict, err.Error())
		return
	case err != nil:
		respond(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, res)
}
