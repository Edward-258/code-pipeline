package api

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"pipeline/pkg/repo"
)

// 产物上传/下载（T2.4，对照 Zadig step_archive 的 S3 环节砍成 GridFS）。
// 上传方 = Pod 内 archive 步骤的 curl（MP_ENGINE_URL/MP_TASK_WF/MP_TASK_ID 注入，
// 见 engine/stepctl.go ArchiveCtl）；下载方 = 人 / 前端 / 下游消费。

const maxArtifactSize = 32 << 20 // 32MB 上限（学习版定死；GridFS 不是网盘）

// artifactKey —— GridFS 键：同一 (wf, taskID, step) 重跑覆盖取最新
func artifactKey(wf string, taskID int64, step string) string {
	return fmt.Sprintf("%s-%d-%s", wf, taskID, step)
}

// PutArtifact POST /api/tasks/:name/:id/artifact/:step
// body = 原始 tar.gz 字节流（curl --data-binary）。任务必须存在（挡野上传），
// 步骤名只做键的一部分不强校验（允许步骤还没跑完就先传的极端时序——GridFS 覆盖语义兜底）。
func PutArtifact(c *gin.Context) {
	taskID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task id must be a number"})
		return
	}
	if _, err := repo.FindTask(c.Param("name"), taskID); err == repo.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 超限拒绝：MaxBytesReader 顶到 32MB+1，超了读的时候直接断
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxArtifactSize+1)
	if c.Request.ContentLength > maxArtifactSize {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": fmt.Sprintf("artifact exceeds %d bytes", maxArtifactSize)})
		return
	}

	key := artifactKey(c.Param("name"), taskID, c.Param("step"))
	if err := repo.PutArtifact(key, c.Request.Body); err != nil {
		if isTooLarge(err) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": fmt.Sprintf("artifact exceeds %d bytes", maxArtifactSize)})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"stored": key})
}

// GetArtifact GET /api/tasks/:name/:id/artifact/:step —— 流式下载（不落盘不缓冲全量）
func GetArtifact(c *gin.Context) {
	taskID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task id must be a number"})
		return
	}
	key := artifactKey(c.Param("name"), taskID, c.Param("step"))
	r, err := repo.GetArtifact(key)
	if err == repo.ErrArtifactNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "artifact not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer r.Close()
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", key))
	c.Header("Content-Type", "application/gzip")
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, r) // 写失败 = 客户端断开，无事可做
}

// isTooLarge —— GridFS 上传被 MaxBytesReader 截断的报错识别（mongo 驱动会包成各色 error，
// 抓 "request body too large"/"unexpected EOF" 等主流形态）
func isTooLarge(err error) bool {
	if err == nil {
		return false
	}
	if err == io.ErrUnexpectedEOF {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "request body too large") ||
		strings.Contains(msg, "request entity too large") ||
		strings.Contains(msg, "unexpected EOF")
}
