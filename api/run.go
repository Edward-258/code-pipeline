package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"pipeline/engine"
	"pipeline/models"
	"pipeline/repo"
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

	// 先响应后启动：JSON 序列化完成后 goroutine 才会改 t，避免数据竞争
	c.JSON(http.StatusOK, t)
	go engine.Run(t)
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
			spec := *jb.Spec // spec 深拷贝
			stt.Jobs = append(stt.Jobs, &models.JobTask{
				Name:    jb.Name,
				JobType: jb.JobType,
				Status:  models.StatusCreated,
				Spec:    &spec,
			})
		}
		stages = append(stages, stt)
	}

	return &models.WorkflowTask{
		ID:            fmt.Sprintf("%s-%d", wf.Name, taskID), // 自然键：插入前定好，ack 全靠它
		TaskID:        taskID,
		WorkflowName: wf.Name,
		Status:       models.StatusCreated,
		Creator:      "api",
		WorkflowArgs: snapshotWorkflow(wf),
		Stages:       stages,
		CreatedAt:    time.Now(),
	}
}

// snapshotWorkflow —— 图纸深拷贝（Stage/Job/Spec 逐层复制，绝不共享指针）
func snapshotWorkflow(wf *models.Workflow) *models.Workflow {
	out := &models.Workflow{
		Name:      wf.Name,
		Stages:    make([]*models.Stage, 0, len(wf.Stages)),
		CreatedAt: wf.CreatedAt,
		UpdatedAt: wf.UpdatedAt,
	}
	for _, st := range wf.Stages {
		s := &models.Stage{Name: st.Name, Parallel: st.Parallel}
		if st.ManualExec != nil {
			me := *st.ManualExec
			s.ManualExec = &me
		}
		for _, jb := range st.Jobs {
			spec := *jb.Spec
			s.Jobs = append(s.Jobs, &models.Job{Name: jb.Name, JobType: jb.JobType, Spec: &spec})
		}
		out.Stages = append(out.Stages, s)
	}
	return out
}
