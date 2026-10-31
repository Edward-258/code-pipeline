package models

import "time"

// Workflow —— 图纸（对照 Zadig models/workflow_v4.go 的 WorkflowV4）
// 学习版差异：44 种 job 类型砍到只剩 freestyle，Spec 直接是具体结构体而非 interface{}
type Workflow struct {
	Name      string    `bson:"_id"       json:"name"` // 图纸名即主键
	Stages    []*Stage  `bson:"stages"    json:"stages"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time `bson:"updated_at" json:"updated_at"`
}

// Stage —— 阶段（对照 Zadig WorkflowStage）
// 两个"阀门"保留其一：ManualExec（手动分步执行，M3 灵魂）；审批砍掉
type Stage struct {
	Name       string      `bson:"name"                 json:"name"`
	Parallel   bool        `bson:"parallel"             json:"parallel"` // 阶段内 job 并行/串行
	ManualExec *ManualExec `bson:"manual_exec,omitempty" json:"manual_exec,omitempty"`
	Jobs       []*Job      `bson:"jobs"                 json:"jobs"`
}

// ManualExec —— 手动分步执行（对照 Zadig stage.ManualExec）
// Enabled: 停下来等人;  Executed: 人已经放行过;  NotificationSent: 通知去重（学习版占位）
type ManualExec struct {
	Enabled          bool `bson:"enabled"           json:"enabled"`
	Executed         bool `bson:"executed"          json:"executed"`
	NotificationSent bool `bson:"notification_sent" json:"notification_sent"`
}

// Job —— 任务单元（对照 Zadig Job）
type Job struct {
	Name    string         `bson:"name" json:"name"`
	JobType string         `bson:"type" json:"type"` // 学习版恒为 freestyle
	Spec    *FreestyleSpec `bson:"spec" json:"spec"`
}

// FreestyleSpec —— freestyle 作业规格：镜像 + shell 脚本
// 对照 Zadig 的 JobTaskFreestyleSpec（Properties+Steps 简化成两字段）
type FreestyleSpec struct {
	Image   string   `bson:"image"   json:"image"`   // M1 用 busybox；M5 换 fat runner
	Scripts []string `bson:"scripts" json:"scripts"` // 逐行执行的 shell
}
