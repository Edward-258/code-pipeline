package engine

import (
	"testing"

	"pipeline/models"
)

// TestDeclareDead —— M3.5 宣判真值表：
// running→cancelled（收 leaked）、created→skipped、终态原样保留、pause 根本不进扫描集
func TestDeclareDead(t *testing.T) {
	t.Run("mixed tree", func(t *testing.T) {
		tree := &models.WorkflowTask{
			WorkflowName: "w",
			TaskID:       1,
			Status:       models.StatusRunning,
			Stages: []*models.StageTask{
				{Name: "s1", Status: models.StatusPassed, Jobs: []*models.JobTask{
					{Name: "j1", Status: models.StatusPassed, K8sJobName: "mp-1-s1-j1"},
				}},
				{Name: "s2", Status: models.StatusRunning, Jobs: []*models.JobTask{
					{Name: "j2", Status: models.StatusRunning, K8sJobName: "mp-1-s2-j2"},
					{Name: "j3", Status: models.StatusCreated, K8sJobName: "mp-1-s2-j3"}, // 未启动：名字已定但 Job 未创建
				}},
				{Name: "s3", Status: models.StatusCreated, Jobs: []*models.JobTask{
					{Name: "j4", Status: models.StatusCreated},
				}},
				{Name: "s4", Status: models.StatusSkipped, Jobs: []*models.JobTask{
					{Name: "j5", Status: models.StatusSkipped},
				}},
			},
		}

		leaked := declareDead(tree)

		if tree.Status != models.StatusCancelled {
			t.Fatalf("task status = %s, want cancelled", tree.Status)
		}
		if tree.EndTime == 0 || tree.Error == "" {
			t.Fatal("task end_time/error not stamped")
		}
		// 已终态的原样保留
		if tree.Stages[0].Status != models.StatusPassed || tree.Stages[0].Jobs[0].Status != models.StatusPassed {
			t.Fatalf("passed stage rewritten: %s/%s", tree.Stages[0].Status, tree.Stages[0].Jobs[0].Status)
		}
		if tree.Stages[3].Status != models.StatusSkipped {
			t.Fatalf("skipped stage rewritten: %s", tree.Stages[3].Status)
		}
		// running → cancelled；同 stage 内 created job → skipped
		s2 := tree.Stages[1]
		if s2.Status != models.StatusCancelled || s2.Jobs[0].Status != models.StatusCancelled {
			t.Fatalf("running stage/job not cancelled: %s/%s", s2.Status, s2.Jobs[0].Status)
		}
		if s2.Jobs[1].Status != models.StatusSkipped {
			t.Fatalf("created job in running stage = %s, want skipped", s2.Jobs[1].Status)
		}
		// 未启动的 stage → 全 skipped
		if tree.Stages[2].Status != models.StatusSkipped || tree.Stages[2].Jobs[0].Status != models.StatusSkipped {
			t.Fatalf("created stage not skipped: %s/%s", tree.Stages[2].Status, tree.Stages[2].Jobs[0].Status)
		}
		// 只收 running job 的执行物；created job 的名字不该进清理名单（它的 Job 根本没创建过）
		if len(leaked) != 1 || leaked[0] != "mp-1-s2-j2" {
			t.Fatalf("leaked = %v, want [mp-1-s2-j2]", leaked)
		}
	})

	t.Run("fresh task never started", func(t *testing.T) {
		tree := &models.WorkflowTask{
			Status: models.StatusCreated,
			Stages: []*models.StageTask{
				{Name: "s1", Status: models.StatusCreated, Jobs: []*models.JobTask{
					{Name: "j1", Status: models.StatusCreated},
				}},
			},
		}
		if leaked := declareDead(tree); len(leaked) != 0 {
			t.Fatalf("leaked = %v, want empty", leaked)
		}
		if tree.Stages[0].Jobs[0].Status != models.StatusSkipped {
			t.Fatalf("created job = %s, want skipped", tree.Stages[0].Jobs[0].Status)
		}
	})
}
