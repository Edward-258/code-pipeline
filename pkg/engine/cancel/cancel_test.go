package cancel

import (
	"testing"
	"time"

	"pipeline/pkg/models"
)

// TestCancelRegistry —— 注册/广播/注销生命周期 + 二次 close 不炸
func TestCancelRegistry(t *testing.T) {
	ch, unregister := Register("wf-test-1")
	if Fired(ch) {
		t.Fatal("fresh chan must not be Fired")
	}
	if !Request("wf-test-1") {
		t.Fatal("registered task should deliver cancel")
	}
	if !Fired(ch) {
		t.Fatal("chan must be closed after Request")
	}
	// 幂等：二次 cancel 不 panic、无看护人路径不误触发
	if !Request("wf-test-1") { // 看护人未注销：仍答 true（收尾窗口内二次 202）
		t.Fatal("second cancel before unregister should still report caretaker")
	}
	unregister()
	if Request("wf-test-1") {
		t.Fatal("after unregister there is no caretaker")
	}
}

// TestCancelTree —— 纯数据收口：非终态全翻 Fired，终态不动，pause 一样收
func TestCancelTree(t *testing.T) {
	tk := &models.WorkflowTask{Stages: []*models.StageTask{
		{Name: "done", Status: models.StatusPassed, Jobs: []*models.JobTask{
			{Name: "j1", Status: models.StatusPassed},
		}},
		{Name: "working", Status: models.StatusRunning, Jobs: []*models.JobTask{
			{Name: "j2", Status: models.StatusRunning, Steps: []*models.StepTask{
				{Name: "s1", Status: models.StatusRunning},
				{Name: "s2", Status: models.StatusCreated},
			}},
		}},
		{Name: "gated", Status: models.StatusPause, Jobs: []*models.JobTask{
			{Name: "j3", Status: models.StatusCreated},
		}},
	}}
	Tree(tk)

	if tk.Status != models.StatusCancelled {
		t.Errorf("task = %s", tk.Status)
	}
	if tk.Stages[0].Status != models.StatusPassed { // 终态 stage 不改写
		t.Errorf("finished stage rewritten: %s", tk.Stages[0].Status)
	}
	if tk.Stages[1].Status != models.StatusCancelled || tk.Stages[1].Jobs[0].Status != models.StatusCancelled {
		t.Errorf("running tree not Fired: %s/%s", tk.Stages[1].Status, tk.Stages[1].Jobs[0].Status)
	}
	if tk.Stages[1].Jobs[0].Steps[0].Status != models.StatusCancelled || tk.Stages[1].Jobs[0].Steps[1].Status != models.StatusCancelled {
		t.Errorf("steps not Fired: %s/%s", tk.Stages[1].Jobs[0].Steps[0].Status, tk.Stages[1].Jobs[0].Steps[1].Status)
	}
	if tk.Stages[2].Status != models.StatusCancelled { // pause 一样收（取消没有 durable 例外）
		t.Errorf("paused stage not Fired: %s", tk.Stages[2].Status)
	}
	if tk.EndTime == 0 {
		t.Error("EndTime should be stamped")
	}
}

// Fired() 的非阻塞语义：未关闭时必须立即返回 false（轮询循环每拍调用，不许卡）
func TestCancelledNonBlocking(t *testing.T) {
	ch := make(chan struct{})
	done := make(chan bool, 1)
	go func() { done <- Fired(ch) }()
	select {
	case v := <-done:
		if v {
			t.Fatal("open chan must read false")
		}
	case <-time.After(time.Second):
		t.Fatal("Fired() blocked on open chan")
	}
}
