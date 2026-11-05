package step

import (
	"bytes"
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
		{Name: "first", Type: models.StepShell, Script: "echo working\nmp_output \"BUILD_ID=42\"\necho '===MP=== OUTPUT RAW=ok'"}, // mp_output + 裸 echo 双路产出（T1.2）
		{Name: "boom", Type: models.StepShell, Script: "echo about-to-fail >&2; exit 7"}, // stderr 混流不许破坏协议
		{Name: "never", Type: models.StepShell, Script: "echo SHOULD-NOT-RUN"},
	}
	script, err := GenerateDriverScript(steps)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// ⚠️ 分离捕获（不再 CombinedOutput）：驱动必须单流（prelude 的 exec 2>&1）。
	// containerd 两条 copier 乱序追写同一文件，stderr 块可能落在任何 banner 前
	// （幕1 首跑实录：gonna-die 落在 STEP-START 前，按 banner 切分掉进黑洞）。
	// 单流不变量 = stderr 缓冲必须为空，全部输出按写出顺序进 stdout。
	cmd := exec.Command(sh, "-c", script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	out := stdout.String()

	// 期望脚本 exit 7（失败退出码透传）；err 非 nil 且 ExitError
	if runErr == nil {
		t.Fatalf("script should exit non-zero, got 0:\n%s", out)
	}
	if ee, ok := runErr.(*exec.ExitError); !ok || ee.ExitCode() != 7 {
		t.Fatalf("want exit 7, got %v", runErr)
	}
	if stderr.Len() != 0 {
		t.Errorf("driver must be single-stream (exec 2>&1): %d bytes leaked to stderr: %q", stderr.Len(), stderr.String())
	}

	jt := jobWithSteps("first", "boom", "never")
	ApplyStepBanners(jt, string(out), now)
	SliceStepLogs(jt, string(out))

	// T1.2：OUTPUT banner 收割——mp_output 助手与裸 echo 双路都要进 jt.Outputs
	if jt.Outputs["BUILD_ID"] != "42" || jt.Outputs["RAW"] != "ok" {
		t.Errorf("outputs not harvested: %v", jt.Outputs)
	}

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
	summary := FinalizeSteps(jt)
	if jt.Steps[2].Status != models.StatusSkipped {
		t.Errorf("never: %s", jt.Steps[2].Status)
	}
	if !strings.Contains(summary, "boom") {
		t.Errorf("summary should name failed step: %q", summary)
	}
}
