package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"pipeline/pkg/models"
	"pipeline/pkg/repo"
)

// 远程主机 API（2026-11-08，对照 Zadig system/handler 的 ssh key 管理极简版）。
// host = freestyle job 可 ssh 的目标（名字 + 登录用户 + 地址 + 私钥）。
// 砍掉的 Zadig 能力：连通性探活、项目授权、VM agent。
// ⚠️ 明文姿态：私钥随列表回传（与 registry.password / mongo 密码同档，学习版知情接受）。

// createHost POST /api/hosts
func createHost(c *gin.Context) {
	var h models.Host
	if err := c.ShouldBindJSON(&h); err != nil {
		respond(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Validate(); err != nil {
		respond(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := repo.CreateHost(&h); errors.Is(err, repo.ErrExists) {
		respond(c, http.StatusConflict, "host "+h.Name+" already exists")
		return
	} else if err != nil {
		respond(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, h)
}

// listHosts GET /api/hosts
func listHosts(c *gin.Context) {
	list, err := repo.ListHosts()
	if err != nil {
		respond(c, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*models.Host{}
	}
	c.JSON(http.StatusOK, list)
}

// updateHost PUT /api/hosts/:name —— name 是主键不可改；CreatedAt 从旧档带回
func updateHost(c *gin.Context) {
	var h models.Host
	if err := c.ShouldBindJSON(&h); err != nil {
		respond(c, http.StatusBadRequest, err.Error())
		return
	}
	if h.Name != "" && h.Name != c.Param("name") {
		respond(c, http.StatusBadRequest, "host name in body does not match URL path")
		return
	}
	h.Name = c.Param("name")
	if err := h.Validate(); err != nil {
		respond(c, http.StatusBadRequest, err.Error())
		return
	}
	old, err := repo.FindHost(h.Name)
	if errors.Is(err, repo.ErrNotFound) {
		respond(c, http.StatusNotFound, "host not found")
		return
	} else if err != nil {
		respond(c, http.StatusInternalServerError, err.Error())
		return
	}
	h.CreatedAt = old.CreatedAt
	if err := repo.UpdateHost(&h); err != nil {
		respond(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, h)
}

// deleteHost DELETE /api/hosts/:name —— 引用它的图纸下次执行时 resolve 报错点名
func deleteHost(c *gin.Context) {
	if err := repo.DeleteHost(c.Param("name")); errors.Is(err, repo.ErrNotFound) {
		respond(c, http.StatusNotFound, "host not found")
		return
	} else if err != nil {
		respond(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": c.Param("name")})
}
