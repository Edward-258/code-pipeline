package k8s

import "testing"

// TestJobName —— T1.6 抓的老雷回归：不同 workflow 的同号任务必须得到不同的 K8s Job 名
func TestJobName(t *testing.T) {
	a := JobName("t16capA", 1, "s1", "j")
	b := JobName("t16capB", 1, "s1", "j")
	if a == b {
		t.Fatalf("job name collision across workflows: %q", a)
	}
	if a != "mp-t16capa-1-s1-j" {
		t.Errorf("jobName = %q", a)
	}
	// 非法字符清洗 + 超长截断
	if got := JobName("Wf With Space/Ümlaut", 2, "St age", "J*ob"); got != "mp-wf-with-space-mlaut-2-st-age-j-ob" {
		t.Errorf("sanitize: %q", got)
	}
	if got := JobName("averyveryverylongworkflownamethatkeepsgoing", 3, "s", "j"); len(got) > 63 {
		t.Errorf("truncate: len=%d %q", len(got), got)
	}
}
