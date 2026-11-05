package cancel

import (
	"errors"
	"log"
	"sync"
	"time"

	"pipeline/pkg/models"
)

// 取消任务（T1.5，对照 Zadig CancelWorkflowTaskV4：mongo 标记 + queue 侧取消的合体）。
//
// 学习版的两条取消路径（不变量：任何非终态任务，cancel 都要在一拍轮询内送进终态）：
//  1. 活着的任务（有看护 goroutine）：close(chan) 广播信号，看护人在 stage 边界和
//     K8s 轮询循环的每一拍感知 → 删 Job → 树内收口 cancelled。响应 ≤ pollInterval。
//  2. 没有看护人的任务（pause 稳态 / queued / 引擎重启后 Recover 宣判过的幸存者）：
//     注册表里没号，API 侧直接 Tree 收口 Mongo——pause 没有 goroutine 这正是
//     M3 的设计（暂停=落库+goroutine 退出），取消自然也走纯数据路径。
//
// 注册表是进程内的，与 M3.5 Recover 不冲突：Recover 只在启动时跑一次，之后所有
// running 都有看护人或已是终态（M3.5 不变量）。

var (
	cancelMu    sync.Mutex
	cancelChans = map[string]*cancelEntry{} // key = task 自然键 <workflow>-<taskID>
)

// cancelEntry —— chan 包一层 closed 标志：close 必须 exactly-once（Go 的 close 二次即
// panic），且注销前要保持"有看护人"的可见性（二次 cancel 在 goroutine 收尾窗口内
// 仍应答 202，而不是掉进数据路径与看护人并发写树）。
type cancelEntry struct {
	ch     chan struct{}
	closed bool
}

// ErrCancelled —— 看护人感知到取消信号的 sentinel（K8s 轮询循环返回，runJobTask 翻译成树内状态）
var ErrCancelled = errors.New("task cancelled")

// Register —— Run 入口注册取消信号；返回 (信号 chan, 注销函数)。
// chan 由这里交出去而不是让调用方回查 map：map 的读写都必须在锁内。
func Register(taskKey string) (<-chan struct{}, func()) {
	e := &cancelEntry{ch: make(chan struct{})}
	cancelMu.Lock()
	cancelChans[taskKey] = e
	cancelMu.Unlock()
	return e.ch, func() {
		cancelMu.Lock()
		delete(cancelChans, taskKey)
		cancelMu.Unlock()
	}
}

// Request —— 给有看护人的任务发取消信号。返回 false = 没有看护人（走纯数据路径）。
// 幂等：重复 cancel 只广播一次，不炸（closed 标志挡住二次 close）。
func Request(taskKey string) bool {
	cancelMu.Lock()
	e, ok := cancelChans[taskKey]
	if ok && !e.closed {
		e.closed = true
		close(e.ch)
	}
	cancelMu.Unlock()
	return ok
}

// Signalled —— 看护人侧非阻塞检查（stage 边界 / 轮询循环每拍）
func Signalled(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// Tree —— 纯数据收口（无看护人路径）：树内所有非终态节点翻 cancelled。
// 对照 Zadig cancel 的 mongo 标记半边；资源半边（删 Job）对 pause/queued 不存在，
// 对孤儿由 Recover 兜过。zadig 思想：cancelled 是最差终态（权重 7），树内不掺假。
func Tree(t *models.WorkflowTask) {
	now := time.Now().Unix()
	for _, st := range t.Stages {
		if models.StatusFinished(st.Status) {
			continue
		}
		st.Status = models.StatusCancelled
		if st.EndTime == 0 {
			st.EndTime = now
		}
		for _, jt := range st.Jobs {
			if models.StatusFinished(jt.Status) {
				continue
			}
			jt.Status = models.StatusCancelled
			for _, sp := range jt.Steps {
				if !models.StatusFinished(sp.Status) {
					sp.Status = models.StatusCancelled
				}
			}
		}
	}
	t.Status = models.StatusCancelled
	t.EndTime = now
	t.Error = "cancelled by user"
	log.Printf("engine: task %s cancelled (no caretaker, data-only)", t.ID)
}
