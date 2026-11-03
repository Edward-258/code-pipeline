package engine

import (
	"testing"

	"pipeline/pkg/models"
)

// TestStageGated —— M3 闸门判断的真值表（对照 Zadig waitForManualExec 的三个前置）
func TestStageGated(t *testing.T) {
	cases := []struct {
		name string
		me   *models.ManualExec
		want bool
	}{
		{"nil manual_exec", nil, false},
		{"disabled", &models.ManualExec{Enabled: false}, false},
		{"enabled not executed", &models.ManualExec{Enabled: true}, true},
		{"already executed", &models.ManualExec{Enabled: true, Executed: true}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := &models.StageTask{Name: "s", ManualExec: c.me}
			if got := stageGated(st); got != c.want {
				t.Fatalf("stageGated = %v, want %v", got, c.want)
			}
		})
	}
}
