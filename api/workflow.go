package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"pipeline/models"
	"pipeline/repo"
)

// ---------- Workflow CRUD（对照 Zadig handler/workflow_v4.go，砍掉鉴权/参数/通知） ----------

// CreateWorkflow POST /api/workflows
func CreateWorkflow(c *gin.Context) {
	var wf models.Workflow
	if err := c.ShouldBindJSON(&wf); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateWorkflow(&wf); err != nil {
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
func GetWorkflow(c *gin.Context) {
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
func ListWorkflows(c *gin.Context) {
	list, err := repo.ListWorkflows()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

// DeleteWorkflow DELETE /api/workflows/:name
func DeleteWorkflow(c *gin.Context) {
	if err := repo.DeleteWorkflow(c.Param("name")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// ---------- 校验（对照 Zadig workflow.Validate 的极简版） ----------

func validateWorkflow(wf *models.Workflow) error {
	if wf.Name == "" {
		return errNew("workflow name is required")
	}
	if len(wf.Stages) == 0 {
		return errNew("at least one stage is required")
	}
	for i, st := range wf.Stages {
		if st.Name == "" {
			return errNew("stage[%d]: name is required", i)
		}
		if len(st.Jobs) == 0 {
			return errNew("stage[%d]: at least one job is required", i)
		}
		for j, jb := range st.Jobs {
			if jb.Name == "" {
				return errNew("stage[%d].job[%d]: name is required", i, j)
			}
			if jb.JobType != "freestyle" {
				return errNew("stage[%d].job[%d]: only freestyle is supported, got %q", i, j, jb.JobType)
			}
			if jb.Spec == nil || jb.Spec.Image == "" {
				return errNew("stage[%d].job[%d]: spec.image is required", i, j)
			}
			if len(jb.Spec.Scripts) == 0 {
				return errNew("stage[%d].job[%d]: spec.scripts is required", i, j)
			}
		}
	}
	return nil
}
