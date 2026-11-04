package models

import "time"

// Cron —— 定时触发配置（T3.1，对照 Zadig cron 子服务的 CronList 砍到骨头）：
// 一条 cron = 一个工作流的触发规则。5 段 crontab 表达式（分 时 日 月 周），
// 进程内 1min ticker 匹配当前分钟即触发（走 TriggerWorkflow 正常入队，不吃并发插队）。
type Cron struct {
	Name         string    `bson:"_id"           json:"name"` // cron 名即主键
	WorkflowName string    `bson:"workflow_name" json:"workflow_name"`
	Crontab      string    `bson:"crontab"       json:"crontab"` // 5 段：分 时 日 月 周
	Enabled      bool      `bson:"enabled"       json:"enabled"`
	CreatedAt    time.Time `bson:"created_at"    json:"created_at"`
	UpdatedAt    time.Time `bson:"updated_at"    json:"updated_at"`
}
