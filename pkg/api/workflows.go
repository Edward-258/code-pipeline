package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"pipeline/pkg/engine"
	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// ---------- Workflow 域：CRUD + 手动触发（对照 Zadig handler/workflow_v4.go，砍掉鉴权/参数/通知） ----------

// createWorkflow POST /api/workflows
func createWorkflow(c *gin.Context) {
	var wf models.Workflow
	if err := c.ShouldBindJSON(&wf); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := models.ValidateWorkflow(&wf); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := repo.CreateWorkflow(&wf); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, wf)
}

// getWorkflow GET /api/workflows/:name
func getWorkflow(c *gin.Context) {
	wf, err := repo.FindWorkflow(c.Param("name"))
	if errors.Is(err, repo.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "workflow not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, wf)
}

// listWorkflows GET /api/workflows
func listWorkflows(c *gin.Context) {
	list, err := repo.ListWorkflows()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

// updateWorkflow PUT /api/workflows/:name —— 全量更新（前端"保存=删除重建"的正规化：
// 图纸名即主键，改名=新建；历史任务档案内嵌快照不受影响）。
// 形态对齐 UpdateCron：路径名为准 + 不存在 404；body.name 与路径不一致显式 400
// （编辑器拿错对象的防呆——静默覆盖比报错危险）。
func updateWorkflow(c *gin.Context) {
	var wf models.Workflow
	if err := c.ShouldBindJSON(&wf); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if wf.Name != "" && wf.Name != c.Param("name") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "workflow name in body does not match URL path"})
		return
	}
	wf.Name = c.Param("name")
	if err := models.ValidateWorkflow(&wf); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	old, err := repo.FindWorkflow(wf.Name)
	if errors.Is(err, repo.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "workflow not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	wf.CreatedAt = old.CreatedAt // 出身不改写
	if err := repo.UpdateWorkflow(&wf); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, wf)
}

// deleteWorkflow DELETE /api/workflows/:name
func deleteWorkflow(c *gin.Context) {
	if err := repo.DeleteWorkflow(c.Param("name")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
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
