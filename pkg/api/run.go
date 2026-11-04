package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"pipeline/pkg/engine"
	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// ---------- Run / Task 查询（对照 Zadig workflow controller 的 RunWorkflowTask） ----------

// RunWorkflow POST /api/workflows/:name/run
// 查图纸 → counter 发 taskID → 快照整棵图纸进 workflow_args → 落库 → 异步执行
func RunWorkflow(c *gin.Context) {
	wf, err := repo.FindWorkflow(c.Param("name"))
	if err == repo.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "workflow not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	taskID, err := repo.NextID("workflow_task:" + wf.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	t := buildTask(wf, taskID)
	if err := repo.CreateTask(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// T1.6：入队（status=queued 先落库），执行交给派发器——API 永远不直接 go Run。
	// 先落库后响应：调用方看到的已经是 queued 态。
	if err := engine.Enqueue(t); err != nil {
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

// ListTasks GET /api/tasks/:name —— 列表（repo 层已投影掉 stages/workflow_args 大字段）
func ListTasks(c *gin.Context) {
	limit, err := strconv.ParseInt(c.DefaultQuery("limit", "20"), 10, 64)
	if err != nil || limit < 0 {
		limit = 20
	}
	list, err := repo.ListTasks(c.Param("name"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
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
	nt := buildTask(t.WorkflowArgs, taskID) // 快照再快照：两层快照互不共享指针，各是各的档案
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

// buildTask —— 发号后的实例构造。⭐灵魂：WorkflowArgs 内嵌【当时的完整图纸快照】，
// 此后改图纸/删图纸都不影响查历史、克隆重跑。
func buildTask(wf *models.Workflow, taskID int64) *models.WorkflowTask {
	stages := make([]*models.StageTask, 0, len(wf.Stages))
	for _, st := range wf.Stages {
		stt := &models.StageTask{
			Name:     st.Name,
			Parallel: st.Parallel,
			Status:   models.StatusCreated,
		}
		if st.ManualExec != nil {
			// 运行时副本：Executed 归零（M3 放行时在这里翻 true）
			stt.ManualExec = &models.ManualExec{Enabled: st.ManualExec.Enabled}
		}
		for _, jb := range st.Jobs {
			var specCopy models.FreestyleSpec
			if jb.Spec != nil { // deploy job 无 freestyle spec，跳过
				specCopy = *jb.Spec // 浅拷贝 + steps 逐个深拷贝（快照绝不共享指针）
				if len(jb.Spec.Steps) > 0 {
					specCopy.Steps = make([]*models.StepSpec, len(jb.Spec.Steps))
					for i, ss := range jb.Spec.Steps {
						cp := *ss
						specCopy.Steps[i] = &cp
					}
				}
			}
			var ds *models.DeploySpec
			if jb.DeploySpec != nil {
				cp := *jb.DeploySpec // 值结构浅拷贝即深拷贝（全标量字段）
				ds = &cp
			}
			// 步骤翻译：图纸 steps → 运行时 StepTask（与 EffectiveSteps 同源：旧图纸 Scripts → 隐式 "main"）
			var specPtr *models.FreestyleSpec
			var steps []*models.StepTask
			if jb.Spec != nil {
				specPtr = &specCopy
				eff := specCopy.EffectiveSteps()
				steps = make([]*models.StepTask, len(eff))
				for i, ss := range eff {
					steps[i] = &models.StepTask{Name: ss.Name, Type: ss.Type, Status: models.StatusCreated}
				}
			}
			stt.Jobs = append(stt.Jobs, &models.JobTask{
				Name:       jb.Name,
				JobType:    jb.JobType,
				Status:     models.StatusCreated,
				Spec:       specPtr,
				DeploySpec: ds,
				Steps:      steps,
			})
		}
		stages = append(stages, stt)
	}

	return &models.WorkflowTask{
		ID:            fmt.Sprintf("%s-%d", wf.Name, taskID), // 自然键：插入前定好，ack 全靠它
		TaskID:        taskID,
		WorkflowName:  wf.Name,
		Status:        models.StatusCreated,
		Creator:       "api",
		WorkflowArgs:  snapshotWorkflow(wf),
		GlobalContext: copyStrings(wf.GlobalContext), // M4：图纸级变量 → 任务初始上下文（快照独立副本，改图纸不影响在跑任务）
		Stages:        stages,
		CreatedAt:     time.Now(),
	}
}

// copyStrings —— string map 浅值深拷贝（值是 string，拷 map 结构即可）
func copyStrings(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// snapshotWorkflow —— 图纸深拷贝（Stage/Job/Spec 逐层复制，绝不共享指针）
func snapshotWorkflow(wf *models.Workflow) *models.Workflow {
	out := &models.Workflow{
		Name:          wf.Name,
		Stages:        make([]*models.Stage, 0, len(wf.Stages)),
		GlobalContext: copyStrings(wf.GlobalContext), // 快照连变量初始值一起封存
		CreatedAt:     wf.CreatedAt,
		UpdatedAt:     wf.UpdatedAt,
	}
	for _, st := range wf.Stages {
		s := &models.Stage{Name: st.Name, Parallel: st.Parallel}
		if st.ManualExec != nil {
			me := *st.ManualExec
			s.ManualExec = &me
		}
		for _, jb := range st.Jobs {
			var specCopy models.FreestyleSpec
			if jb.Spec != nil { // deploy job 无 freestyle spec，跳过
				specCopy = *jb.Spec
				if len(jb.Spec.Steps) > 0 {
					specCopy.Steps = make([]*models.StepSpec, len(jb.Spec.Steps))
					for i, ss := range jb.Spec.Steps {
						cp := *ss
						specCopy.Steps[i] = &cp
					}
				}
			}
			var ds *models.DeploySpec
			if jb.DeploySpec != nil {
				cp := *jb.DeploySpec
				ds = &cp
			}
			var specPtr *models.FreestyleSpec
			if jb.Spec != nil {
				specPtr = &specCopy
			}
			s.Jobs = append(s.Jobs, &models.Job{Name: jb.Name, JobType: jb.JobType, Spec: specPtr, DeploySpec: ds})
		}
		out.Stages = append(out.Stages, s)
	}
	return out
}
