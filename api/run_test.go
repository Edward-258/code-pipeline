package api

import (
	"testing"

	"pipeline/models"
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
	tk := buildTask(wf, 1)

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
