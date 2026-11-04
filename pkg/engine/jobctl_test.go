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

// TestJobName —— T1.6 抓的老雷回归：不同 workflow 的同号任务必须得到不同的 K8s Job 名
func TestJobName(t *testing.T) {
	a := jobName("t16capA", 1, "s1", "j")
	b := jobName("t16capB", 1, "s1", "j")
	if a == b {
		t.Fatalf("job name collision across workflows: %q", a)
	}
	if a != "mp-t16capa-1-s1-j" {
		t.Errorf("jobName = %q", a)
	}
	// 非法字符清洗 + 超长截断
	if got := jobName("Wf With Space/Ümlaut", 2, "St age", "J*ob"); got != "mp-wf-with-space-mlaut-2-st-age-j-ob" {
		t.Errorf("sanitize: %q", got)
	}
	if got := jobName("averyveryverylongworkflownamethatkeepsgoing", 3, "s", "j"); len(got) > 63 {
		t.Errorf("truncate: len=%d %q", len(got), got)
	}
}
