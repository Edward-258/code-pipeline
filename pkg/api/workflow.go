package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"pipeline/pkg/models"
	"pipeline/pkg/repo"
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
			switch jb.JobType {
			case models.JobTypeFreestyle: // 兼容历史图纸：空 type 视作 freestyle
			case models.JobTypeDeploy:
			case "":
				jb.JobType = models.JobTypeFreestyle
			default:
				return errNew("stage[%d].job[%d]: only freestyle|deploy is supported, got %q", i, j, jb.JobType)
			}
			// T2.2：deploy job 必须带 DeploySpec；freestyle 必须带 Spec（历史图纸走默认分支已归位）
			if jb.JobType == models.JobTypeDeploy {
				if jb.DeploySpec == nil {
					return errNew("stage[%d].job[%d]: deploy job requires deploy_spec", i, j)
				}
				if jb.DeploySpec.Env == "" || jb.DeploySpec.DeploymentName == "" || jb.DeploySpec.ContainerName == "" || jb.DeploySpec.Image == "" {
					return errNew("stage[%d].job[%d]: deploy_spec requires env/deployment_name/container_name/image", i, j)
				}
				continue
			}
			if jb.Spec == nil {
				return errNew("stage[%d].job[%d]: spec is required", i, j)
			}
			// CI/CD 起 image 可空：引擎按步骤需要选默认（git/docker_build→mini-runner，纯 shell→busybox）。
			// steps/scripts 至少其一——引擎只认 steps，scripts 会翻译成隐式 step "main"（EffectiveSteps）
			if len(jb.Spec.Steps) == 0 && len(jb.Spec.Scripts) == 0 {
				return errNew("stage[%d].job[%d]: spec.steps or spec.scripts is required", i, j)
			}
			// T1.9：错误策略白名单（拼错的 "ignor" 悄悄变 stop 比报错更危险）
			switch jb.Spec.ErrorPolicy {
			case "", models.ErrorPolicyStop, models.ErrorPolicyRetry, models.ErrorPolicyIgnore:
			default:
				return errNew("stage[%d].job[%d]: unknown error_policy %q (want stop|retry|ignore)", i, j, jb.Spec.ErrorPolicy)
			}
			for _, ss := range jb.Spec.Steps {
				if ss.Name == "" {
					return errNew("stage[%d].job[%d]: step name is required", i, j)
				}
				switch ss.Type {
				case models.StepShell, models.StepGit, models.StepDockerBuild, models.StepJunitReport:
				default:
					return errNew("stage[%d].job[%d]: unknown step type %q", i, j, ss.Type)
				}
			}
		}
	}
	return nil
}
