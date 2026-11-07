package api

import (
	"errors"

	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"

	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// 部署目标环境 API（T2.1）。env = 名字 → namespace 的映射，deploy job（T2.2）的靶标来源。
// 对照 Zadig environment 模块的 50+ 端点砍到三个：增/列/删。

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`) // 与 K8s namespace 命名规则同构（小写数字中划线）

// EnsureNamespaceHook —— env 落库前的 namespace 随建钩子（Zadig 同款语义的极简版）。
// main.go 装配 k8s.EnsureNamespace 真身；var 而非直呼 K8s：api 层不 import 任何 K8s
// 东西（router.go 教义，同 exec 包 envResolver 的钩子模式）。
// nil = 未装配（单测路由）= 纯登记模式，行为与旧版一致。
var EnsureNamespaceHook func(ns string) error

// createEnv POST /api/envs
func createEnv(c *gin.Context) {
	var e models.Env
	if err := c.ShouldBindJSON(&e); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !nameRe.MatchString(e.Name) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "env name must match [a-z0-9][a-z0-9-]{0,62}"})
		return
	}
	if e.Namespace == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "namespace is required"})
		return
	}
	if !nameRe.MatchString(e.Namespace) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "namespace must match [a-z0-9][a-z0-9-]{0,62}"})
		return
	}
	// 随建先行（失败不落库）→ 不变量：落库的 env 其 namespace 必然已存在；
	// 重复 POST 幂等重建同 ns 无害，409 路径照旧在 repo 层
	if EnsureNamespaceHook != nil {
		if err := EnsureNamespaceHook(e.Namespace); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "ensure namespace: " + err.Error()})
			return
		}
	}
	if err := repo.CreateEnv(&e); errors.Is(err, repo.ErrExists) {
		c.JSON(http.StatusConflict, gin.H{"error": "env " + e.Name + " already exists"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, e)
}

// listEnvs GET /api/envs
func listEnvs(c *gin.Context) {
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

// updateEnv PUT /api/envs/:name —— name 是主键不可改（改=新建+删旧），
// 只更新 namespace/desc；CreatedAt 从旧档带过来
func updateEnv(c *gin.Context) {
	var e models.Env
	if err := c.ShouldBindJSON(&e); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if e.Name != "" && e.Name != c.Param("name") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "env name in body does not match URL path"})
		return
	}
	e.Name = c.Param("name")
	if !nameRe.MatchString(e.Name) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "env name must match [a-z0-9][a-z0-9-]{0,62}"})
		return
	}
	if e.Namespace == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "namespace is required"})
		return
	}
	if !nameRe.MatchString(e.Namespace) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "namespace must match [a-z0-9][a-z0-9-]{0,62}"})
		return
	}
	old, err := repo.FindEnv(e.Name)
	if errors.Is(err, repo.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "env not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// PUT 改 ns 目标同样保证已存在（不变量与 createEnv 一致）；幂等放过未变更的 ns
	if EnsureNamespaceHook != nil {
		if err := EnsureNamespaceHook(e.Namespace); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "ensure namespace: " + err.Error()})
			return
		}
	}
	e.CreatedAt = old.CreatedAt
	if err := repo.UpdateEnv(&e); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, e)
}

// deleteEnv DELETE /api/envs/:name
func deleteEnv(c *gin.Context) {
	if err := repo.DeleteEnv(c.Param("name")); errors.Is(err, repo.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "env not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": c.Param("name")})
}
