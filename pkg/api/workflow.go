package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// ---------- Workflow CRUD（对照 Zadig handler/workflow_v4.go，砍掉鉴权/参数/通知） ----------

// CreateWorkflow POST /api/workflows
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

// GetWorkflow GET /api/workflows/:name
func getWorkflow(c *gin.Context) {
	wf, err := repo.FindWorkflow(c.Param("name"))
	if err == repo.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "workflow not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, wf)
}

// ListWorkflows GET /api/workflows
func listWorkflows(c *gin.Context) {
	list, err := repo.ListWorkflows()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

// UpdateWorkflow PUT /api/workflows/:name —— 全量更新（前端"保存=删除重建"的正规化：
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
	if err == repo.ErrNotFound {
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

// DeleteWorkflow DELETE /api/workflows/:name
func deleteWorkflow(c *gin.Context) {
	if err := repo.DeleteWorkflow(c.Param("name")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

