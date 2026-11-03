package engine

import (
	"strings"
	"testing"
	"time"

	"pipeline/pkg/models"
)

var now = time.Unix(1700000000, 0)

func jobWithSteps(names ...string) *models.JobTask {
	jt := &models.JobTask{Name: "j"}
	for _, n := range names {
		jt.Steps = append(jt.Steps, &models.StepTask{Name: n, Type: models.StepShell, Status: models.StatusCreated})
	}
	return jt
}

// TestApplyStepBanners —— banner 重放真值表：START/END、幂等、畸形行、未知步骤
func TestApplyStepBanners(t *testing.T) {
	jt := jobWithSteps("a", "b")
	log1 := "===MP=== STEP-START a\ngit clone ...\n===MP=== STEP-END a passed exit=0\n"
	if !applyStepBanners(jt, log1, now) {
		t.Fatal("first feed should report change")
	}
	if jt.Steps[0].Status != models.StatusPassed || jt.Steps[0].StartTime != now.Unix() || jt.Steps[0].EndTime != now.Unix() {
		t.Fatalf("step a after END: %+v", jt.Steps[0])
	}

	// 幂等：重放同一份日志不产生变化、不覆盖时间戳
	jt.Steps[0].EndTime = 1
	if applyStepBanners(jt, log1, now.Add(time.Hour)) {
		t.Fatal("replay should be a no-op")
	}
	if jt.Steps[0].EndTime != 1 {
		t.Fatal("replay must not overwrite terminal state")
	}

	// 失败 verdict + 带空格的步骤名 + 畸形行 + 未知步骤名：都不许 panic
	log2 := "garbage line\n===MP=== STEP-START b\n===MP=== BOGUS x\n===MP=== STEP-END b failed exit=3\n===MP=== STEP-END nobody passed exit=0\n"
	applyStepBanners(jt, log2, now)
	if jt.Steps[1].Status != models.StatusFailed || jt.Steps[1].Error != "exit=3" {
		t.Fatalf("step b should be failed with exit code: %+v", jt.Steps[1])
	}

	// START 后暴死（有始无终）：保持 running，交给 finalizeSteps 收口
	jt2 := jobWithSteps("x")
	applyStepBanners(jt2, "===MP=== STEP-START x\n...", now)
	if jt2.Steps[0].Status != models.StatusRunning {
		t.Fatalf("step x should be running: %s", jt2.Steps[0].Status)
	}

	// 黏连 banner（真实事故：printf 无尾换行的 XML 把 END 黏在同一行）——行中 banner 也要认
	jt3 := jobWithSteps("gen")
	glued := "===MP=== STEP-START gen\n<?xml version=\"1.0\"?><testsuite tests=\"1\"></testsuite>===MP=== STEP-END gen passed exit=0\n"
	applyStepBanners(jt3, glued, now)
	if jt3.Steps[0].Status != models.StatusPassed {
		t.Fatalf("glued banner should parse: %s", jt3.Steps[0].Status)
	}
	sliceStepLogs(jt3, glued)
	if !strings.Contains(jt3.Steps[0].Log, "<testsuite") {
		t.Fatalf("glued step log should keep xml: %q", jt3.Steps[0].Log)
	}
}

// TestSliceStepLogs —— 日志按 banner 切分：各步拿各的，未知步骤进黑洞
func TestSliceStepLogs(t *testing.T) {
	jt := jobWithSteps("a", "b", "c")
	full := "===MP=== STEP-START a\nout-a1\nout-a2\n===MP=== STEP-END a passed exit=0\n" +
		"===MP=== STEP-START b\nout-b1\n===MP=== STEP-END b failed exit=9\n" +
		"===MP=== STEP-START ghost\nout-ghost\n===MP=== STEP-END ghost passed exit=0\n"
	sliceStepLogs(jt, full)

	if !strings.Contains(jt.Steps[0].Log, "out-a1") || !strings.Contains(jt.Steps[0].Log, "STEP-END a") || strings.Contains(jt.Steps[0].Log, "out-b1") {
		t.Errorf("step a log wrong: %q", jt.Steps[0].Log)
	}
	if !strings.Contains(jt.Steps[1].Log, "out-b1") || strings.Contains(jt.Steps[1].Log, "out-a1") {
		t.Errorf("step b log wrong: %q", jt.Steps[1].Log)
	}
	if jt.Steps[2].Log != "" { // c 没跑过：没日志
		t.Errorf("step c should have no log: %q", jt.Steps[2].Log)
	}
}

// TestFinalizeSteps —— 终态收口：created→skipped、悬空 running→failed、门禁翻红冒泡、64KB 截断
func TestFinalizeSteps(t *testing.T) {
	jt := &models.JobTask{
		Spec: &models.FreestyleSpec{Steps: []*models.StepSpec{
			{Name: "ok", Type: models.StepShell},
			{Name: "hung", Type: models.StepShell},                     // 有 START 无 END（Pod 暴死）
			{Name: "never", Type: models.StepShell},                    // 没轮到（失败即停）
			{Name: "tests", Type: models.StepJunitReport, PassRate: 1}, // 退出码 0 但门禁要翻红
		}},
	}
	jt.Steps = []*models.StepTask{
		{Name: "ok", Status: models.StatusPassed},
		{Name: "hung", Status: models.StatusRunning},
		{Name: "never", Status: models.StatusCreated},
		{Name: "tests", Status: models.StatusPassed, Log: `<testsuite tests="2" failures="1" time="0.1"></testsuite>`},
	}

	errSummary := finalizeSteps(jt)
	if jt.Steps[1].Status != models.StatusFailed || !strings.Contains(jt.Steps[1].Error, "did not finish") {
		t.Errorf("hung step: %+v", jt.Steps[1])
	}
	if jt.Steps[2].Status != models.StatusSkipped {
		t.Errorf("never step: %s", jt.Steps[2].Status)
	}
	if jt.Steps[3].Status != models.StatusFailed || jt.Steps[3].TestResult == nil || jt.Steps[3].TestResult.Tests != 2 {
		t.Errorf("gate step should flip red with TestResult: %+v", jt.Steps[3])
	}
	if !strings.Contains(errSummary, "step") || !strings.Contains(errSummary, "hung") {
		t.Errorf("summary should name first failed step: %q", errSummary)
	}

	// 截断：>64KB 的日志保尾部
	jt2 := &models.JobTask{}
	jt2.Steps = []*models.StepTask{{Name: "big", Status: models.StatusPassed, Log: strings.Repeat("x", maxStepLog+4096)}}
	finalizeSteps(jt2)
	if len(jt2.Steps[0].Log) > maxStepLog+64 || !strings.HasPrefix(jt2.Steps[0].Log, "\n...[log truncated") {
		t.Errorf("truncation failed: len=%d head=%q", len(jt2.Steps[0].Log), jt2.Steps[0].Log[:40])
	}
}
