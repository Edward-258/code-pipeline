package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"pipeline/pkg/engine"
	"pipeline/pkg/engine/cron"
	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// 定时触发配置 API（T3.1）：POST/GET/PUT/DELETE /api/crons。
// 表达式在写入时校验（坏 cron 留在库里 = 每分钟刷错误日志的哑弹）。

// CreateCron POST /api/crons
func CreateCron(c *gin.Context) {
	var cr models.Cron
	if err := c.ShouldBindJSON(&cr); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateCron(&cr); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := repo.CreateCron(&cr); err == repo.ErrExists {
		c.JSON(http.StatusConflict, gin.H{"error": "cron " + cr.Name + " already exists"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cr)
}

// ListCrons GET /api/crons
func ListCrons(c *gin.Context) {
	list, err := repo.ListCrons()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if list == nil {
		list = []*models.Cron{}
	}
	c.JSON(http.StatusOK, list)
}

// UpdateCron PUT /api/crons/:name —— 全量更新（enabled 开关 / 换表达式 / 换图纸）
func UpdateCron(c *gin.Context) {
	var cr models.Cron
	if err := c.ShouldBindJSON(&cr); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, err := repo.FindCron(c.Param("name")); err == repo.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "cron not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	cr.Name = c.Param("name") // 路径名为准
	if err := validateCron(&cr); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := repo.UpdateCron(&cr); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cr)
}

// DeleteCron DELETE /api/crons/:name
func DeleteCron(c *gin.Context) {
	if err := repo.DeleteCron(c.Param("name")); err == repo.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "cron not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": c.Param("name")})
}

// TriggerCron POST /api/crons/:name/fire —— 手动点火（验收用：不等整分钟就能触发一次）
func TriggerCron(c *gin.Context) {
	cr, err := repo.FindCron(c.Param("name"))
	if err == repo.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "cron not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 手动点火无视 Enabled（验收/调试用途）；但表达式必须合法——坏表达式在这里当场暴露
	if _, err := cron.Match(cr.Crontab, time.Now()); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	t, err := engine.TriggerWorkflow(cr.WorkflowName, "cron:"+cr.Name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, t)
}

func validateCron(cr *models.Cron) error {
	if !envNameRe.MatchString(cr.Name) {
		return fmt.Errorf("cron name must match [a-z0-9][a-z0-9-]{0,62}")
	}
	if cr.WorkflowName == "" {
		return fmt.Errorf("workflow_name is required")
	}
	if _, err := cron.Match(cr.Crontab, time.Now()); err != nil {
		return fmt.Errorf("bad crontab: %v", err)
	}
	return nil
}
