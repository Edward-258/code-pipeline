package models

// 状态机常量 —— 对照 Zadig config/consts.go:106-125
// 学习版只保留 8 个里最核心的 7 个
type Status string

const (
	StatusCreated   Status = "created"   // 任务已创建未开始
	StatusQueued    Status = "queued"    // T1.6：已入队待派发（队列态=档案态，无第二真相源）
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

// StatusFinished —— 终态判定：不会再变的状态（取消/重试端点的守卫、CancelTree 的跳过集）
func StatusFinished(s Status) bool {
	switch s {
	case StatusPassed, StatusFailed, StatusCancelled, StatusSkipped:
		return true
	}
	return false
}

// FinishedStatuses —— 终态集的枚举形态（repo 条件写回的 $in 参数，与 StatusFinished 同源；
// 两处必须同步改——判定用函数、写回用集合，这里只放一个真相源给集合侧）
var FinishedStatuses = []Status{StatusPassed, StatusFailed, StatusCancelled, StatusSkipped}

// statusWeight —— 最差者胜的权重（对照 Zadig consts.go:106-125 的偏序，学习版 7 态映射）
// 值越大越"差"：聚合取权重最大者，一个 failed 就足以把 stage/task 拖成 failed。
// running 排在 passed 之前：还有 job 在跑就不许宣布成功；
// skipped 垫底：被刹车跳过的 job 不改变聚合结果（failed+skipped 仍是 failed）。
func statusWeight(s Status) int {
	switch s {
	case StatusCancelled:
		return 7
	case StatusFailed:
		return 6
	case StatusPause:
		return 5
	case StatusRunning:
		return 4
	case StatusPassed:
		return 3
	case StatusCreated, StatusQueued:
		return 2 // 还没跑起来的都不改变聚合结果
	case StatusSkipped:
		return 1
	}
	return 0 // 未知状态最无害，交给上层日志兜底
}

// WorstOf —— 最差者胜聚合（stage 聚合 jobs / task 聚合 stages 通用）。
// 空集返回 passed：没有任何 job 的 stage 视为无事发生地成功（学习版语义）。
func WorstOf(statuses ...Status) Status {
	if len(statuses) == 0 {
		return StatusPassed
	}
	worst := statuses[0]
	for _, s := range statuses[1:] {
		if statusWeight(s) > statusWeight(worst) {
			worst = s
		}
	}
	return worst
}
