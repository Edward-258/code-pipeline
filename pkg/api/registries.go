package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// 镜像仓库 API（2026-11-07，对照 Zadig system/handler/registry.go 的极简版）。
// registry = docker_build 的推送目标（地址+凭证+默认标记）。砍掉的 Zadig 能力：
// RSA 加密回传、云厂商动态凭证、连通性 validate 端点、项目授权、镜像 tag 浏览。
// ⚠️ 明文姿态：凭证随列表回传（与 mongo/ACR values 同档，学习版知情接受）。

// createRegistry POST /api/registries
func createRegistry(c *gin.Context) {
	var r models.Registry
	if err := c.ShouldBindJSON(&r); err != nil {
		respond(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := r.Validate(); err != nil {
		respond(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := repo.CreateRegistry(&r); errors.Is(err, repo.ErrExists) {
		respond(c, http.StatusConflict, "registry "+r.Name+" already exists")
		return
	} else if err != nil {
		respond(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, r)
}

// listRegistries GET /api/registries
func listRegistries(c *gin.Context) {
	list, err := repo.ListRegistries()
	if err != nil {
		respond(c, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*models.Registry{}
	}
	c.JSON(http.StatusOK, list)
}

// updateRegistry PUT /api/registries/:name —— name 是主键不可改；CreatedAt 从旧档带回
func updateRegistry(c *gin.Context) {
	var r models.Registry
	if err := c.ShouldBindJSON(&r); err != nil {
		respond(c, http.StatusBadRequest, err.Error())
		return
	}
	if r.Name != "" && r.Name != c.Param("name") {
		respond(c, http.StatusBadRequest, "registry name in body does not match URL path")
		return
	}
	r.Name = c.Param("name")
	if err := r.Validate(); err != nil {
		respond(c, http.StatusBadRequest, err.Error())
		return
	}
	old, err := repo.FindRegistry(r.Name)
	if errors.Is(err, repo.ErrNotFound) {
		respond(c, http.StatusNotFound, "registry not found")
		return
	} else if err != nil {
		respond(c, http.StatusInternalServerError, err.Error())
		return
	}
	r.CreatedAt = old.CreatedAt
	if err := repo.UpdateRegistry(&r); err != nil {
		respond(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, r)
}

// deleteRegistry DELETE /api/registries/:name —— 删到默认自动选举补位（repo 层语义）
func deleteRegistry(c *gin.Context) {
	if err := repo.DeleteRegistry(c.Param("name")); errors.Is(err, repo.ErrNotFound) {
		respond(c, http.StatusNotFound, "registry not found")
		return
	} else if err != nil {
		respond(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": c.Param("name")})
}
