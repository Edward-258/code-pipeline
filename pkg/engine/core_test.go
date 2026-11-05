package engine

import (
	"testing"

	"pipeline/pkg/models"
)

// TestIgnorePolicyBrake —— T1.9：ignore 失败不触发刹车（串行续跑 + stage 间放行），
// stop 失败刹车，取消永远刹车（策略不可豁免）
func TestIgnorePolicyBrake(t *testing.T) {
	mk := func(policy string) *models.StageTask {
		return &models.StageTask{Jobs: []*models.JobTask{
			{Name: "a", Status: models.StatusFailed, Spec: &models.FreestyleSpec{ErrorPolicy: policy}},
			{Name: "b", Status: models.StatusCreated},
		}}
	}
	if s := mk(models.ErrorPolicyIgnore); stageBraked(s) || stageHasHardFailure(s) {
		t.Error("ignore failure must not brake (serial or stage-level)")
	}
	if s := mk(models.ErrorPolicyStop); !stageBraked(s) || !stageHasHardFailure(s) {
		t.Error("stop failure must brake")
	}
	if s := mk(""); !stageBraked(s) || !stageHasHardFailure(s) {
		t.Error("unset policy defaults to stop semantics")
	}
	s := mk(models.ErrorPolicyIgnore)
	s.Jobs[0].Status = models.StatusCancelled
	if !stageBraked(s) || !stageHasHardFailure(s) {
		t.Error("cancel must brake regardless of policy")
	}
}
