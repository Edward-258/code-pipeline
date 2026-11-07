package step

import (
	"strings"
	"testing"

	"pipeline/pkg/models"
)

// TestGenerateDriverScript —— 驱动脚本代码生成：骨架、banner、失败即停、注册顺序
func TestGenerateDriverScript(t *testing.T) {
	steps := []*models.StepSpec{
		{Name: "checkout", Type: models.StepGit, Repo: "https://github.com/o/r.git", Branch: "dev"},
		{Name: "build", Type: models.StepDockerBuild, Image: "demo", Tag: "1.0"},
		{Name: "report", Type: models.StepJunitReport, ReportGlob: "*.xml"},
		{Name: "hi", Type: models.StepShell, Script: "echo hi"},
	}
	script, err := GenerateDriverScript(steps, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	for _, want := range []string{
		`say "STEP-START $1"`, // banner 前奏
		`say "STEP-END $1 failed exit=$rc"`,
		`mp_output() { say "OUTPUT $1"; }`, // T1.2：OUTPUT 产出助手
		`exit "$rc"`,                       // 失败即停 + 退出码透传
		"git clone --depth 1 -b 'dev'",     // git 片段
		`"$MP_NODE_REGISTRY"/'demo:1.0'`,   // docker_build 片段（前缀双引号可展开，幕1 首跑教训）
		"cat *.xml",                        // junit 片段
		"echo hi",                          // shell 片段
		"mp_run 'checkout' mp_step_1",      // 按序注册
		"mp_run 'build' mp_step_2",
		"mp_run 'report' mp_step_3",
		"mp_run 'hi' mp_step_4",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q\n--- script ---\n%s", want, script)
		}
	}

	// POSIX sh 纪律：不许 bash 方言溜进来
	for _, bashism := range []string{"[[", " local ", "function ", "$((("} {
		if strings.Contains(script, bashism) {
			t.Errorf("script contains bashism %q (alpine is ash!)", bashism)
		}
	}
}

// TestGenerateDriverScriptBadSpec —— 非法规格在生成期报错（别等 Pod 起来才炸）
func TestGenerateDriverScriptBadSpec(t *testing.T) {
	if _, err := GenerateDriverScript([]*models.StepSpec{{Name: "g", Type: models.StepGit}}, nil); err == nil {
		t.Fatal("git step without repo should fail at generate time")
	}
	if _, err := GenerateDriverScript([]*models.StepSpec{{Name: "d", Type: models.StepDockerBuild}}, nil); err == nil {
		t.Fatal("docker_build step without image should fail at generate time")
	}
}
