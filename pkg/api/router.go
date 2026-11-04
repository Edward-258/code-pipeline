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

		// T3.3：IM 通知配置
		api.POST("/notifies", CreateNotifyCfg)
		api.GET("/notifies", ListNotifyCfgs)
		api.PUT("/notifies/:name", UpdateNotifyCfg)
		api.DELETE("/notifies/:name", DeleteNotifyCfg)
		// T3.2：git webhook 触发（GitHub/Gitee push，HMAC 验签）
		api.POST("/webhook/:workflow", Webhook)
		// T3.1：定时触发（cron 配置 + 手动点火）
		api.POST("/crons", CreateCron)
		api.GET("/crons", ListCrons)
		api.PUT("/crons/:name", UpdateCron)
		api.DELETE("/crons/:name", DeleteCron)
		api.POST("/crons/:name/fire", TriggerCron)
		// T2.1：部署目标环境（env = 名字 → namespace，deploy job 的靶标）
		api.POST("/envs", CreateEnv)
		api.GET("/envs", ListEnvs)
		api.DELETE("/envs/:name", DeleteEnv)

		// M1：run / task 查询
		api.POST("/workflows/:name/run", RunWorkflow)
		api.GET("/tasks/:name", ListTasks)
		api.GET("/tasks/:name/:id", GetTask)
		// T1.5：取消任务（活任务信号广播，pause/queued 纯数据收口）
		api.DELETE("/tasks/:name/:id", CancelTask)
		// T1.7/T1.8：克隆重跑 / 整任务重试
		api.POST("/tasks/:name/:id/clone", CloneTask)
		api.POST("/tasks/:name/:id/retry", RetryTask)
		// T2.4：产物上传（Pod 内 archive 步骤 curl）与下载
		api.POST("/tasks/:name/:id/artifact/:step", PutArtifact)
		api.GET("/tasks/:name/:id/artifact/:step", GetArtifact)
		// CI/CD：分步日志（活任务透传 Pod 尾部，死任务回档案分步日志）
		api.GET("/tasks/:name/:id/log", GetTaskLog)
		// M3：放行暂停的 stage（durable 暂停的灵魂端点）
		api.POST("/tasks/:name/:id/stages/:stage/release", ReleaseStage)
		// T3.4：原生审批（pause 闸门的多人版，any/all 策略；老图纸无 approvers 时等价 release）
		api.POST("/tasks/:name/:id/approve", ApproveTask)
	}
	return r
}

// 小工具：带格式化的 error 构造
func errNew(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}
