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

// 任务运行/查询/运维的 HTTP 面。领域逻辑全部在 engine（run/stages/jobs 状态机 +
// ops.go 运维操作面 + trigger.go 触发），这里只做形状翻译：参数解析、状态码映射。

// ---------- 共用小件 ----------

// taskFromPath —— "ParseInt(:id) → FindTask → 404/500" 样板的单点：
// 返回 nil 时响应已写出（400/404/500），调用方直接 return
func taskFromPath(c *gin.Context) *models.WorkflowTask {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respond(c, http.StatusBadRequest, "task id must be a number")
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

// respond —— 统一错误响应体 {"error": …}
func respond(c *gin.Context, code int, msg string) {
	c.JSON(code, gin.H{"error": msg})
}

// ---------- Run / Task 查询（对照 Zadig workflow controller 的 RunWorkflowTask） ----------

// runWorkflow POST /api/workflows/:name/run
// T3.1 起走 engine.TriggerWorkflow 统一入口（cron/webhook 与手动 run 同一条
// 发号→快照→入队路径），creator 标记区分来源。
func runWorkflow(c *gin.Context) {
	t, err := engine.TriggerWorkflow(c.Param("name"), "api")
	if errors.Is(err, engine.ErrWorkflowNotFound) {
		respond(c, http.StatusNotFound, "workflow not found")
		return
	}
	if err != nil {
		respond(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, t)
}

// getTask GET /api/tasks/:name/:id —— 整棵任务树
func getTask(c *gin.Context) {
	if t := taskFromPath(c); t != nil {
		c.JSON(http.StatusOK, t)
	}
}

// listTasks GET /api/tasks/:name?status=&creator=&from=&to=&page=&size=
// T4.1 过滤+分页版：响应包一层 envelope {items,total,page,size}（前端分页三件套）。
// ⚠️ 响应形状从裸数组改为 envelope——test-mvp1/3 的 jq 消费方已同步更新。
func listTasks(c *gin.Context) {
	f := repo.TaskFilter{
		Status:  c.Query("status"),
		Creator: c.Query("creator"),
	}
	f.From, _ = strconv.ParseInt(c.Query("from"), 10, 64)
	f.To, _ = strconv.ParseInt(c.Query("to"), 10, 64)
	f.Page, _ = strconv.ParseInt(c.Query("page"), 10, 64)
	f.Size, _ = strconv.ParseInt(c.Query("size"), 10, 64)
	items, total, err := repo.ListTasksFiltered(c.Param("name"), f)
	if err != nil {
		respond(c, http.StatusInternalServerError, err.Error())
		return
	}
	f.Normalize() // 响应里的 page/size 要是归一化后的真实值
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": f.Page, "size": f.Size})
}

// ---------- T1.5：取消任务（对照 Zadig DELETE /v4/workflowtask/...） ----------

// cancelTask DELETE /api/tasks/:name/:id
// 有看护 goroutine（注册表有号）：close 信号，看护人在一拍轮询内收口 → 202 Accepted；
// 无看护人（pause 稳态 / queued / 孤儿幸存）：引擎侧纯数据收口 → 200；
// 已终态：409（终态不可取消，幂等语义交还给调用方）。两条路径的分派在 engine.CancelTask。
func cancelTask(c *gin.Context) {
	t := taskFromPath(c)
	if t == nil {
		return
	}
	accepted, err := engine.CancelTask(t)
	if errors.Is(err, engine.ErrTaskFinal) {
		respond(c, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
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
// 放行暂停中的 stage：引擎侧翻闸门位 → 落库 → 正常入队续跑（gateMu 互斥防双击）。
func releaseStage(c *gin.Context) {
	t := taskFromPath(c)
	if t == nil {
		return
	}
	if _, err := engine.ReleaseGate(t, c.Param("stage")); err != nil {
		if errors.Is(err, engine.ErrStageNotFound) {
			respond(c, http.StatusNotFound, "stage not found")
			return
		}
		if errors.Is(err, engine.ErrNotWaiting) {
			respond(c, http.StatusConflict, err.Error())
			return
		}
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
// failed/cancelled/skipped 刹车残留复活（树内手术在 engine.RetryTask）。
func retryTask(c *gin.Context) {
	t := taskFromPath(c)
	if t == nil {
		return
	}
	nt, err := engine.RetryTask(t)
	if errors.Is(err, engine.ErrTaskNotFinal) {
		respond(c, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		respond(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, nt)
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
