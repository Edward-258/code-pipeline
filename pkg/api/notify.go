package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// IM 通知配置 API（T3.3）：POST/GET/PUT/DELETE /api/notifies（与 cron 同构）。

func CreateNotifyCfg(c *gin.Context) {
	var n models.NotifyConfig
	if err := c.ShouldBindJSON(&n); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateNotify(&n); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := repo.CreateNotify(&n); err == repo.ErrExists {
		c.JSON(http.StatusConflict, gin.H{"error": "notify " + n.Name + " already exists"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, n)
}

func ListNotifyCfgs(c *gin.Context) {
	list, err := repo.ListNotifies()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if list == nil {
		list = []*models.NotifyConfig{}
	}
	c.JSON(http.StatusOK, list)
}

func UpdateNotifyCfg(c *gin.Context) {
	var n models.NotifyConfig
	if err := c.ShouldBindJSON(&n); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, err := repo.FindNotify(c.Param("name")); err == repo.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "notify not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	n.Name = c.Param("name")
	if err := validateNotify(&n); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := repo.UpdateNotify(&n); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, n)
}

func DeleteNotifyCfg(c *gin.Context) {
	if err := repo.DeleteNotify(c.Param("name")); err == repo.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "notify not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": c.Param("name")})
}

func validateNotify(n *models.NotifyConfig) error {
	if !envNameRe.MatchString(n.Name) {
		return fmt.Errorf("notify name must match [a-z0-9][a-z0-9-]{0,62}")
	}
	if n.Type != models.NotifyFeishu && n.Type != models.NotifyDingTalk {
		return fmt.Errorf("type must be feishu|dingtalk")
	}
	if n.WebhookURL == "" {
		return fmt.Errorf("webhook_url is required")
	}
	return nil
}
