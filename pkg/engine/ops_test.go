package engine

import (
	"testing"

	"pipeline/pkg/models"
)

// TestResetForRetryRun —— T1.8 树内手术：failed/cancelled/skipped 复活 created，
// passed 子树原样保留，Retry 计数与步骤现场归零
func TestResetForRetryRun(t *testing.T) {
	tk := &models.WorkflowTask{Stages: []*models.StageTask{
		{Name: "ok", Status: models.StatusPassed, Jobs: []*models.JobTask{
			{Name: "j1", Status: models.StatusPassed, Outputs: map[string]string{"A": "1"}},
		}},
		{Name: "boom", Status: models.StatusFailed, Jobs: []*models.JobTask{
			{Name: "j2", Status: models.StatusFailed, Retry: 2, Error: "exit 7"},
			{Name: "j3", Status: models.StatusPassed},
		}},
		{Name: "braked", Status: models.StatusSkipped, Jobs: []*models.JobTask{
			{Name: "j4", Status: models.StatusSkipped},
		}},
	}}
	tk.Status = models.StatusFailed
	tk.Error = "stage boom failed"
	tk.EndTime = 123

	resetForRetryRun(tk)

	if tk.Stages[0].Status != models.StatusPassed || tk.Stages[0].Jobs[0].Status != models.StatusPassed {
		t.Errorf("passed subtree must stay: %+v", tk.Stages[0])
	}
	b := tk.Stages[1]
	if b.Status != models.StatusCreated || b.Jobs[0].Status != models.StatusCreated || b.Jobs[0].Retry != 0 || b.Jobs[0].Error != "" {
		t.Errorf("failed job not revived: %+v", b.Jobs[0])
	}
	if b.Jobs[1].Status != models.StatusPassed {
		t.Errorf("passed job inside failed stage must stay: %+v", b.Jobs[1])
	}
	if tk.Stages[2].Status != models.StatusCreated || tk.Stages[2].Jobs[0].Status != models.StatusCreated {
		t.Errorf("skipped (brake residue) must revive: %+v", tk.Stages[2])
	}
	if tk.Status != models.StatusQueued || tk.Error != "" || tk.EndTime != 0 {
		t.Errorf("task shell not reset to queued (retry=单次条件写回): %s %q %d", tk.Status, tk.Error, tk.EndTime)
	}
}

// TestTreeLookup —— ops.go 的树内查找件：findStage 精确命中、findWaitingStage
// 只认 pause 且闸门未放的 stage、未知名一律 miss
func TestTreeLookup(t *testing.T) {
	tk := &models.WorkflowTask{
		ID:     "w-1",
		Status: models.StatusPause,
		Stages: []*models.StageTask{{
			Name:   "gate",
			Status: models.StatusPause,
			ManualExec: &models.ManualExec{
				Enabled:   true,
				Approvers: []string{"alice", "bob"},
				Policy:    models.ApprovalPolicyAll,
			},
		}},
	}

	// 纯内存形态校验：findWaitingStage 能找到等审批的 stage
	st := findWaitingStage(tk, "")
	if st == nil || st.Name != "gate" {
		t.Fatalf("waiting stage not found: %+v", st)
	}
	if findStage(tk, "nope") != nil {
		t.Fatal("findStage must miss unknown stage")
	}
	if findWaitingStage(tk, "nope") != nil {
		t.Fatal("findWaitingStage must miss unknown stage")
	}
}
