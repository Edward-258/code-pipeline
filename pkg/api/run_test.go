package api

import (
	"testing"

	"pipeline/pkg/engine"
	"pipeline/pkg/models"
)

// TestBuildTaskSteps —— 图纸 → 运行时 的步骤翻译（新旧两种图纸同源）
func TestBuildTaskSteps(t *testing.T) {
	wf := &models.Workflow{
		Name: "w",
		Stages: []*models.Stage{{
			Name: "s",
			Jobs: []*models.Job{
				{ // 旧图纸：只有 Scripts
					Name: "legacy", JobType: "freestyle",
					Spec: &models.FreestyleSpec{Image: "busybox", Scripts: []string{"echo a", "echo b"}},
				},
				{ // 新图纸：steps 列表
					Name: "modern", JobType: "freestyle",
					Spec: &models.FreestyleSpec{Steps: []*models.StepSpec{
						{Name: "checkout", Type: models.StepGit, Repo: "https://gitee.com/o/r.git"},
						{Name: "run-tests", Type: models.StepShell, Script: "make test"},
					}},
				},
			},
		}},
	}
	tk := engine.BuildTask(wf, 1)

	legacy := tk.Stages[0].Jobs[0]
	if len(legacy.Steps) != 1 || legacy.Steps[0].Name != "main" || legacy.Steps[0].Type != models.StepShell || legacy.Steps[0].Status != models.StatusCreated {
		t.Fatalf("legacy scripts should translate to implicit step main: %+v", legacy.Steps)
	}
	modern := tk.Stages[0].Jobs[1]
	if len(modern.Steps) != 2 || modern.Steps[0].Name != "checkout" || modern.Steps[1].Status != models.StatusCreated {
		t.Fatalf("blueprint steps should map 1:1: %+v", modern.Steps)
	}

	// 快照独立性：改图纸后档案里的 steps 原封不动（灵魂 #1）
	spec := wf.Stages[0].Jobs[1].Spec
	spec.Steps[0].Repo = "https://evil/x.git"
	if got := tk.WorkflowArgs.Stages[0].Jobs[1].Spec.Steps[0].Repo; got != "https://gitee.com/o/r.git" {
		t.Fatalf("snapshot shares pointers with blueprint: %s", got)
	}
	if got := modern.Spec.Steps[0].Repo; got != "https://gitee.com/o/r.git" {
		t.Fatalf("runtime spec shares pointers with blueprint: %s", got)
	}
}

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
	if tk.Status != models.StatusCreated || tk.Error != "" || tk.EndTime != 0 {
		t.Errorf("task shell not reset: %s %q %d", tk.Status, tk.Error, tk.EndTime)
	}
}
