package engine

import (
	"testing"

	"pipeline/pkg/models"
)

// TestResetForRetry —— T1.4：重试前现场重置。banner 重放的准入条件（START 只认
// created、END 只认 running）决定了旧状态不清就驱动不了新一轮；旧日志/旧输出/旧
// 门禁结果是化石数据，不能代表最后一轮。
func TestResetForRetry(t *testing.T) {
	jt := &models.JobTask{
		Outputs: map[string]string{"STALE": "old"},
		Steps: []*models.StepTask{
			{Status: models.StatusPassed, StartTime: 1, EndTime: 2, Log: "old-log"},
			{Status: models.StatusFailed, StartTime: 3, EndTime: 4, Error: "exit=7", Log: "old-err",
				TestResult: &models.TestResult{Tests: 2, Failures: 1}},
			{Status: models.StatusRunning},
		},
	}
	resetForRetry(jt)

	for i, st := range jt.Steps {
		if st.Status != models.StatusCreated || st.StartTime != 0 || st.EndTime != 0 ||
			st.Error != "" || st.Log != "" || st.TestResult != nil {
			t.Errorf("step %d not reset: %+v", i, st)
		}
	}
	if jt.Outputs != nil {
		t.Errorf("outputs must be discarded on retry: %v", jt.Outputs)
	}
}
