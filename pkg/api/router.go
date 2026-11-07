// Package api —— gin handler 层：HTTP 形状翻译（参数解析、状态码映射、JSON envelope）。
// 领域逻辑全部在 engine/models/repo，这里不碰 K8s 与取消注册表。
package api

import (
	"github.com/gin-gonic/gin"
)

func NewRouter() *gin.Engine {
	r := gin.Default()

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	api := r.Group("/api")
	{
		api.POST("/workflows", createWorkflow)
		api.GET("/workflows", listWorkflows)
		api.GET("/workflows/:name", getWorkflow)
		api.PUT("/workflows/:name", updateWorkflow)
		api.DELETE("/workflows/:name", deleteWorkflow)

		// T3.3：IM 通知配置
		api.POST("/notifies", createNotifyCfg)
		api.GET("/notifies", listNotifyCfgs)
		api.PUT("/notifies/:name", updateNotifyCfg)
		api.DELETE("/notifies/:name", deleteNotifyCfg)
		// T3.2：git webhook 触发（GitHub/Gitee push，HMAC 验签）
		api.POST("/webhook/:workflow", handleWebhook)
		// T3.1：定时触发（cron 配置 + 手动点火）
		api.POST("/crons", createCron)
		api.GET("/crons", listCrons)
		api.PUT("/crons/:name", updateCron)
		api.DELETE("/crons/:name", deleteCron)
		api.POST("/crons/:name/fire", triggerCron)
		// T2.1：部署目标环境（env = 名字 → namespace，deploy job 的靶标）
		api.POST("/envs", createEnv)
		api.GET("/envs", listEnvs)
		api.PUT("/envs/:name", updateEnv)
		api.DELETE("/envs/:name", deleteEnv)
		// 2026-11-07：镜像仓库管理（docker_build 的推送目标；is_default 决定裸名图纸推哪）
		api.POST("/registries", createRegistry)
		api.GET("/registries", listRegistries)
		api.PUT("/registries/:name", updateRegistry)
		api.DELETE("/registries/:name", deleteRegistry)
		// 2026-11-08：远程主机管理（freestyle job 的 ssh 靶标；私钥投影进 Pod 不落档案）
		api.POST("/hosts", createHost)
		api.GET("/hosts", listHosts)
		api.PUT("/hosts/:name", updateHost)
		api.DELETE("/hosts/:name", deleteHost)

		// M1：run / task 查询
		api.POST("/workflows/:name/run", runWorkflow)
		api.GET("/tasks/:name", listTasks)
		api.GET("/tasks/:name/:id", getTask)
		// T1.5：取消任务（活任务信号广播，pause/queued 纯数据收口）
		api.DELETE("/tasks/:name/:id", cancelTask)
		// T1.7/T1.8：克隆重跑 / 整任务重试
		api.POST("/tasks/:name/:id/clone", cloneTask)
		api.POST("/tasks/:name/:id/retry", retryTask)
		// T2.4：产物上传（Pod 内 archive 步骤 curl）与下载
		api.POST("/tasks/:name/:id/artifact/:step", putArtifact)
		api.GET("/tasks/:name/:id/artifact/:step", getArtifact)
		// CI/CD：分步日志（活任务透传 Pod 尾部，死任务回档案分步日志）
		api.GET("/tasks/:name/:id/log", getTaskLog)
		// M3：放行暂停的 stage（durable 暂停的灵魂端点）
		api.POST("/tasks/:name/:id/stages/:stage/release", releaseStage)
		// T3.4：原生审批（pause 闸门的多人版，any/all 策略；老图纸无 approvers 时等价 release）
		api.POST("/tasks/:name/:id/approve", approveTask)
	}
	return r
}
