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
			spec := *jb.Spec // spec 浅拷贝 + steps 逐个深拷贝（快照绝不共享指针）
			if len(jb.Spec.Steps) > 0 {
				spec.Steps = make([]*models.StepSpec, len(jb.Spec.Steps))
				for i, ss := range jb.Spec.Steps {
					cp := *ss
					spec.Steps[i] = &cp
				}
			}
			// 步骤翻译：图纸 steps → 运行时 StepTask（与 EffectiveSteps 同源：旧图纸 Scripts → 隐式 "main"）
			eff := spec.EffectiveSteps()
			steps := make([]*models.StepTask, len(eff))
			for i, ss := range eff {
				steps[i] = &models.StepTask{Name: ss.Name, Type: ss.Type, Status: models.StatusCreated}
			}
			stt.Jobs = append(stt.Jobs, &models.JobTask{
				Name:    jb.Name,
				JobType: jb.JobType,
				Status:  models.StatusCreated,
				Spec:    &spec,
				Steps:   steps,
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
			spec := *jb.Spec
			if len(jb.Spec.Steps) > 0 {
				spec.Steps = make([]*models.StepSpec, len(jb.Spec.Steps))
				for i, ss := range jb.Spec.Steps {
					cp := *ss
					spec.Steps[i] = &cp
				}
			}
			s.Jobs = append(s.Jobs, &models.Job{Name: jb.Name, JobType: jb.JobType, Spec: &spec})
		}
		out.Stages = append(out.Stages, s)
	}
	return out
}
