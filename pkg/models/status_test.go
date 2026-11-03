package models

import "testing"

// TestWorstOf —— 最差者胜聚合的偏序关系（M2 验收的纯逻辑部分，集群实测在 test-m2.ps1）
func TestWorstOf(t *testing.T) {
	cases := []struct {
		name string
		in   []Status
		want Status
	}{
		{"all passed", []Status{StatusPassed, StatusPassed, StatusPassed}, StatusPassed},
		{"one failed poisons all", []Status{StatusPassed, StatusFailed, StatusPassed}, StatusFailed},
		{"cancelled beats failed", []Status{StatusFailed, StatusCancelled}, StatusCancelled},
		{"failed beats pause", []Status{StatusPause, StatusFailed}, StatusFailed},
		{"serial brake: failed+skipped", []Status{StatusFailed, StatusSkipped, StatusSkipped}, StatusFailed},
		{"all skipped stays skipped", []Status{StatusSkipped, StatusSkipped}, StatusSkipped},
		{"passed absorbs skipped", []Status{StatusPassed, StatusSkipped}, StatusPassed},
		{"running beats passed (still in flight)", []Status{StatusRunning, StatusPassed}, StatusRunning},
		{"failed beats running", []Status{StatusRunning, StatusFailed}, StatusFailed},
		{"empty set is passed", []Status{}, StatusPassed},
		{"single failed", []Status{StatusFailed}, StatusFailed},
	}

	for _, c := range cases {
		if got := WorstOf(c.in...); got != c.want {
			t.Errorf("%s: WorstOf(%v) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// TestStatusStopped —— 刹车态判定
func TestStatusStopped(t *testing.T) {
	stopped := []Status{StatusCancelled, StatusFailed, StatusPause}
	running := []Status{StatusCreated, StatusRunning, StatusPassed, StatusSkipped}

	for _, s := range stopped {
		if !StatusStopped(s) {
			t.Errorf("StatusStopped(%q) = false, want true", s)
		}
	}
	for _, s := range running {
		if StatusStopped(s) {
			t.Errorf("StatusStopped(%q) = true, want false", s)
		}
	}
}
