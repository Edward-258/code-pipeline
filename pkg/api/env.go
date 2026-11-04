package api

import (
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"

	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// 部署目标环境 API（T2.1）。env = 名字 → namespace 的映射，deploy job（T2.2）的靶标来源。
// 对照 Zadig environment 模块的 50+ 端点砍到三个：增/列/删。

var envNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`) // 与 K8s namespace 命名规则同构（小写数字中划线）

// CreateEnv POST /api/envs
func CreateEnv(c *gin.Context) {
	var e models.Env
	if err := c.ShouldBindJSON(&e); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !envNameRe.MatchString(e.Name) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "env name must match [a-z0-9][a-z0-9-]{0,62}"})
		return
	}
	if e.Namespace == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "namespace is required"})
		return
	}
	if err := repo.CreateEnv(&e); err == repo.ErrExists {
		c.JSON(http.StatusConflict, gin.H{"error": "env " + e.Name + " already exists"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, e)
}

// ListEnvs GET /api/envs
func ListEnvs(c *gin.Context) {
	list, err := repo.ListEnvs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if list == nil {
		list = []*models.Env{}
	}
	c.JSON(http.StatusOK, list)
}

// DeleteEnv DELETE /api/envs/:name
func DeleteEnv(c *gin.Context) {
	if err := repo.DeleteEnv(c.Param("name")); err == repo.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "env not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": c.Param("name")})
}
