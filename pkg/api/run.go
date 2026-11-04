package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"pipeline/pkg/engine"
	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// ---------- Run / Task 查询（对照 Zadig workflow controller 的 RunWorkflowTask） ----------

// RunWorkflow POST /api/workflows/:name/run
// T3.1 起走 engine.TriggerWorkflow 统一入口（cron/webhook 与手动 run 同一条
// 发号→快照→入队路径），creator 标记区分来源。
func RunWorkflow(c *gin.Context) {
	t, err := engine.TriggerWorkflow(c.Param("name"), "api")
	if errors.Is(err, engine.ErrWorkflowNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "workflow not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, t)
}

// GetTask GET /api/tasks/:name/:id —— 整棵任务树
func GetTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task id must be a number"})
		return
	}
	t, err := repo.FindTask(c.Param("name"), id)
	if err == repo.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, t)
}

// ListTasks GET /api/tasks/:name?status=&creator=&from=&to=&page=&size=
// T4.1 过滤+分页版：响应包一层 envelope {items,total,page,size}（前端分页三件套）。
// ⚠️ 响应形状从裸数组改为 envelope——test-mvp1/3 的 jq 消费方已同步更新。
func ListTasks(c *gin.Context) {
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	f.Normalize() // 响应里的 page/size 要是归一化后的真实值
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": f.Page, "size": f.Size})
}

// ---------- T1.5：取消任务（对照 Zadig DELETE /v4/workflowtask/...） ----------

// CancelTask DELETE /api/tasks/:name/:id
// 有看护 goroutine（注册表有号）：close 信号，看护人在一拍轮询内收口 → 202 Accepted；
// 无看护人（pause 稳态 / queued / 孤儿幸存）：API 侧 CancelTree 纯数据收口 → 200；
// 已终态：409（终态不可取消，幂等语义交还给调用方）。
func CancelTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task id must be a number"})
		return
	}
	t, err := repo.FindTask(c.Param("name"), id)
	if err == repo.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if models.StatusFinished(t.Status) {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("task is already %s", t.Status)})
		return
	}

	if engine.CancelRequested(t.ID) {
		c.JSON(http.StatusAccepted, gin.H{"accepted": true, "note": "caretaker will collect within one poll"})
		return
	}
	engine.CancelTree(t)
	if err := repo.UpdateTask(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, t)
}

// ---------- M3：durable 暂停的放行端点（灵魂端点） ----------

// releaseMu 把"检查 pause → 翻闸门 → 落库"圈成临界区（防手抖双击放出两个续跑 goroutine）
var releaseMu sync.Mutex

// ReleaseStage POST /api/tasks/:name/:id/stages/:stage/release
// 放行暂停中的 stage：置 ManualExec.Executed=true → 落库 → 重新 engine.Run 续跑。
// ⚠️ 职责单一：这里只翻闸门位；task 的 pause→running 翻转只发生在 engine.Run（单一处，别做两处）。
func ReleaseStage(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task id must be a number"})
		return
	}

	releaseMu.Lock()
	defer releaseMu.Unlock()

	t, err := repo.FindTask(c.Param("name"), id)
	if err == repo.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var st *models.StageTask
	for _, s := range t.Stages {
		if s.Name == c.Param("stage") {
			st = s
			break
		}
	}
	if st == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "stage not found"})
		return
	}
	if st.Status != models.StatusPause || st.ManualExec == nil || st.ManualExec.Executed {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("stage %q is not waiting for release (status=%s)", st.Name, st.Status)})
		return
	}

	st.ManualExec.Executed = true // ⭐ 闸门放行位：续跑的 runStages 凭它跳过闸门
	if err := repo.UpdateTask(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// T1.6：release 续跑走正常入队（pause → queued → 派发器 → running）。
	// pause 不占 running 名额的另一半：续跑也要重新排队，不搞插队。
	if err := engine.Enqueue(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, t)
}

// ---------- T1.7/T1.8：克隆与整任务重试（对照 Zadig clone/retry 端点） ----------

// CloneTask POST /api/tasks/:name/:id/clone —— 一键原样重跑（对照 Zadig GET .../clone/...）。
// 灵魂复用：WorkflowArgs 是【当时的完整图纸快照】，buildTask 等价构造新 task（新号），
// 全新 GlobalContext（不继承旧任务收割的 outputs——它们由新一轮运行重新产生）、
// 全新 created 树。克隆任何状态的任务都合法（历史即模板）。
func CloneTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task id must be a number"})
		return
	}
	t, err := repo.FindTask(c.Param("name"), id)
	if err == repo.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if t.WorkflowArgs == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "task has no workflow snapshot (legacy archive?)"})
		return
	}

	taskID, err := repo.NextID("workflow_task:" + t.WorkflowName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	nt := engine.BuildTask(t.WorkflowArgs, taskID) // 快照再快照：两层快照互不共享指针，各是各的档案
	nt.Creator = "clone:" + t.ID            // 出身标记：从哪个任务克隆来的
	if err := repo.CreateTask(nt); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := engine.Enqueue(nt); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, nt)
}

// RetryTask POST /api/tasks/:name/:id/retry —— 从失败处续跑（对照 Zadig POST .../retry/...）。
// 仅终态任务可用（running/queued/pause 有看护人或排队语义，409）。
// 树内手术：failed/cancelled/skipped 的 stage/job 重置 created（skipped 多为刹车残留，
// 重试 = 补跑它们），passed 原样保留不重跑（runStage 凭 jt.Status==passed 跳过）；
// 重置 job 的 Retry 计数归零（新一轮的重试预算）、步骤树回 created。
// GlobalContext 保留：上游 passed job 的 outputs 继续可用，失败 job 重跑后重新收割。
func RetryTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task id must be a number"})
		return
	}
	t, err := repo.FindTask(c.Param("name"), id)
	if err == repo.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !models.StatusFinished(t.Status) {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("task is %s, only finished tasks can be retried", t.Status)})
		return
	}

	resetForRetryRun(t)
	if err := repo.UpdateTask(t); err != nil { // 重置后的树先落库（入队前档案先就位）
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := engine.Enqueue(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, t)
}

// resetForRetryRun —— 整任务重试的树内手术（纯内存，可单测）。
// 与单 job 重试（resetForRetry）的分工：那是 job 内一步重来的现场清理，这是任务级
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

// ---------- 步骤化日志（CI/CD）：任务在跑透传 Pod 日志尾部，已结束回档案分步日志 ----------

// GetTaskLog GET /api/tasks/:name/:id/log?stage=S&job=J[&step=X]
func GetTaskLog(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task id must be a number"})
		return
	}
	t, err := repo.FindTask(c.Param("name"), id)
	if err == repo.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var jt *models.JobTask
	for _, s := range t.Stages {
		if s.Name != c.Query("stage") {
			continue
		}
		for _, j := range s.Jobs {
			if j.Name == c.Query("job") {
				jt = j
				break
			}
		}
	}
	if jt == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "stage/job not found"})
		return
	}

	// 活的：K8s Job 还在 → 实时尾部（分界面 live 日志的来源）
	if txt, live := engine.TailJobLog(jt.K8sJobName); live {
		c.String(http.StatusOK, txt)
		return
	}

	// 死的：档案里的分步日志（banner 切分结果，engine/logs.go）
	stepFilter := c.Query("step")
	var b strings.Builder
	for _, st := range jt.Steps {
		if stepFilter != "" && st.Name != stepFilter {
			continue
		}
		fmt.Fprintf(&b, "=== step %s [%s] %s", st.Name, st.Type, st.Status)
		if st.Error != "" {
			fmt.Fprintf(&b, " — %s", st.Error)
		}
		b.WriteString(" ===\n")
		if st.TestResult != nil {
			fmt.Fprintf(&b, "tests=%d failures=%d errors=%d skipped=%d time=%.2fs\n",
				st.TestResult.Tests, st.TestResult.Failures, st.TestResult.Errors, st.TestResult.Skipped, st.TestResult.Time)
		}
		if st.Log != "" {
			b.WriteString(st.Log)
			b.WriteString("\n")
		}
	}
	c.String(http.StatusOK, b.String())
}

// ---------- 图纸 → 运行实例 ----------

