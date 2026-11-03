package engine

import (
	"os/exec"
	"strings"
	"testing"

	"pipeline/pkg/models"
)

// TestDriverRoundtrip —— 端到端自洽：生成的脚本真的在 POSIX sh（Ubuntu 的 /bin/sh=dash）
// 里跑一遍，stdout 喂回 banner 解析器，步骤状态必须与剧本一致。
// 这是我们"jobexecutor"的仿真验收：代码生成 → 执行 → 刮日志 → 状态树，一条线全过。
func TestDriverRoundtrip(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh available")
	}

	steps := []*models.StepSpec{
		{Name: "first", Type: models.StepShell, Script: "echo working"},
		{Name: "boom", Type: models.StepShell, Script: "echo about-to-fail >&2; exit 7"}, // stderr 混流不许破坏协议
		{Name: "never", Type: models.StepShell, Script: "echo SHOULD-NOT-RUN"},
	}
	script, err := GenerateDriverScript(steps)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	out, err := exec.Command(sh, "-c", script).CombinedOutput() // Pod 日志本来就混流 stdout+stderr
	// 期望脚本 exit 7（失败退出码透传）；err 非 nil 且 ExitError
	if err == nil {
		t.Fatalf("script should exit non-zero, got 0:\n%s", out)
	}
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 7 {
		t.Fatalf("want exit 7, got %v", err)
	}

	jt := jobWithSteps("first", "boom", "never")
	applyStepBanners(jt, string(out), now)
	sliceStepLogs(jt, string(out))

	if jt.Steps[0].Status != models.StatusPassed {
		t.Errorf("first: %s (log=%q)", jt.Steps[0].Status, jt.Steps[0].Log)
	}
	if jt.Steps[1].Status != models.StatusFailed || jt.Steps[1].Error != "exit=7" {
		t.Errorf("boom: %+v", jt.Steps[1])
	}
	if !strings.Contains(jt.Steps[1].Log, "about-to-fail") { // stderr 也进步骤日志
		t.Errorf("boom log missing stderr: %q", jt.Steps[1].Log)
	}
	if strings.Contains(string(out), "SHOULD-NOT-RUN") {
		t.Error("fail-stop broken: step after failure ran")
	}
	summary := finalizeSteps(jt)
	if jt.Steps[2].Status != models.StatusSkipped {
		t.Errorf("never: %s", jt.Steps[2].Status)
	}
	if !strings.Contains(summary, "boom") {
		t.Errorf("summary should name failed step: %q", summary)
	}
}
