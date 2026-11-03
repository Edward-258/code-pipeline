package api

import (
	"fmt"

	"github.com/gin-gonic/gin"
)

func NewRouter() *gin.Engine {
	r := gin.Default()

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	api := r.Group("/api")
	{
		api.POST("/workflows", CreateWorkflow)
		api.GET("/workflows", ListWorkflows)
		api.GET("/workflows/:name", GetWorkflow)
		api.DELETE("/workflows/:name", DeleteWorkflow)

		// M1：run / task 查询
		api.POST("/workflows/:name/run", RunWorkflow)
		api.GET("/tasks/:name", ListTasks)
		api.GET("/tasks/:name/:id", GetTask)
		// M3：放行暂停的 stage（durable 暂停的灵魂端点）
		api.POST("/tasks/:name/:id/stages/:stage/release", ReleaseStage)
	}
	return r
}

// 小工具：带格式化的 error 构造
func errNew(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}
