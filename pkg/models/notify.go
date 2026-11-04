package models

import "time"

// NotifyConfig —— IM 通知配置（T3.3，对照 Zadig imnotify 的单路极简版）：
// 任务终态 → IM 机器人 webhook POST markdown。一路实现、飞书/钉钉两种报文格式。
type NotifyConfig struct {
	Name       string    `bson:"_id"          json:"name"`
	Type       string    `bson:"type"         json:"type"`  // feishu | dingtalk
	WebhookURL string    `bson:"webhook_url"  json:"webhook_url"`
	Secret     string    `bson:"secret,omitempty" json:"secret,omitempty"` // 加签密钥（空 = 不加签）
	Enabled    bool      `bson:"enabled"      json:"enabled"`
	CreatedAt  time.Time `bson:"created_at"   json:"created_at"`
	UpdatedAt  time.Time `bson:"updated_at"   json:"updated_at"`
}

const (
	NotifyFeishu   = "feishu"
	NotifyDingTalk = "dingtalk"
)
