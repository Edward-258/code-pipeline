package models

// 状态机常量 —— 对照 Zadig config/consts.go:106-125
// 学习版只保留 8 个里最核心的 7 个
type Status string

const (
	StatusCreated   Status = "created"   // 任务已创建未开始
	StatusRunning   Status = "running"   // 执行中
	StatusPassed    Status = "passed"    // 成功
	StatusFailed    Status = "failed"    // 失败
	StatusCancelled Status = "cancelled" // 取消
	StatusSkipped   Status = "skipped"   // 跳过
	StatusPause     Status = "pause"     // ⭐ 暂停（等人放行，durable）
)

// statusStopped —— 对照 Zadig stage.go: 遇到这些状态整条流水线刹车
func StatusStopped(s Status) bool {
	switch s {
	case StatusCancelled, StatusFailed, StatusPause:
		return true
	}
	return false
}
