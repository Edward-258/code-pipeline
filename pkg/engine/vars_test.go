package engine

import (
	"encoding/json"
	"testing"

	"pipeline/pkg/models"
)

// renderVars 表测（T1.1）：命中 / 未命中保留原文 / 嵌套 JSON 转义 / 两级命名空间 / HTML 字符不转义
func TestRenderVars(t *testing.T) {
	tests := []struct {
		name    string
		spec    *models.FreestyleSpec
		ctx     VarsCtx
		wantImg string // 渲染后 spec.Image
		wantScr string // 渲染后首个 step 的 Script
	}{
		{
			name: "命中：global 与 job 两级键",
			spec: &models.FreestyleSpec{
				Image: "{{.global.base_image}}",
				Steps: []*models.StepSpec{{Name: "main", Type: models.StepShell, Script: "echo {{.global.env}} {{.build.tag}}"}},
			},
			ctx: VarsCtx{
				"global": {"env": "prod", "base_image": "alpine:3"},
				"build":  {"tag": "v1.2"},
			},
			wantImg: "alpine:3",
			wantScr: "echo prod v1.2",
		},
		{
			name: "未命中保留原文（全局缺键 + 未知命名空间）",
			spec: &models.FreestyleSpec{
				Image: "{{.global.nope}}",
				Steps: []*models.StepSpec{{Name: "main", Type: models.StepShell, Script: "echo {{.ghost.var}} {{.build.ok}}"}},
			},
			ctx: VarsCtx{
				"global": {"env": "prod"},
				"build":  {"ok": "yes"},
			},
			wantImg: "{{.global.nope}}",
			wantScr: "echo {{.ghost.var}} yes",
		},
		{
			name: "嵌套 JSON 转义：值含引号/花括号不破坏 JSON 文档",
			spec: &models.FreestyleSpec{
				Steps: []*models.StepSpec{{Name: "main", Type: models.StepShell, Script: "render={{.global.conf}}"}},
			},
			ctx:     VarsCtx{"global": {"conf": `{"a":"x\"y","n":1}`}},
			wantScr: `render={"a":"x\"y","n":1}`,
		},
		{
			name: "脚本字符保真：<、>、&& 不被 HTML 转义",
			spec: &models.FreestyleSpec{
				Steps: []*models.StepSpec{{Name: "main", Type: models.StepShell, Script: "{{.global.cmd}}"}},
			},
			ctx:     VarsCtx{"global": {"cmd": `a > b && c < d`}},
			wantScr: `a > b && c < d`,
		},
		{
			name: "换行符安全注入（多行脚本值）",
			spec: &models.FreestyleSpec{
				Steps: []*models.StepSpec{{Name: "main", Type: models.StepShell, Script: "{{.global.multi}}"}},
			},
			ctx:     VarsCtx{"global": {"multi": "l1\nl2"}},
			wantScr: "l1\nl2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := RenderVars(tt.spec, tt.ctx); err != nil {
				t.Fatalf("RenderVars: %v", err)
			}
			if tt.spec.Image != tt.wantImg {
				t.Errorf("Image = %q, want %q", tt.spec.Image, tt.wantImg)
			}
			if got := tt.spec.Steps[0].Script; got != tt.wantScr {
				t.Errorf("Script = %q, want %q", got, tt.wantScr)
			}
		})
	}
}

// 渲染必须不碰兄弟字段：只改占位符所在的字符串，数值/布尔/其余结构原样
func TestRenderVarsPreservesSiblings(t *testing.T) {
	spec := &models.FreestyleSpec{
		Image: "busybox",
		Steps: []*models.StepSpec{
			{Name: "one", Type: models.StepShell, Script: "echo {{.global.v}}", PassRate: 0.5},
			{Name: "two", Type: models.StepJunitReport},
		},
	}
	ctx := VarsCtx{"global": {"v": "hello"}}
	if err := RenderVars(spec, ctx); err != nil {
		t.Fatalf("RenderVars: %v", err)
	}
	if spec.Image != "busybox" {
		t.Errorf("Image = %q, want busybox", spec.Image)
	}
	if spec.Steps[0].PassRate != 0.5 {
		t.Errorf("PassRate = %v, want 0.5", spec.Steps[0].PassRate)
	}
	if spec.Steps[1].Name != "two" || spec.Steps[1].Type != models.StepJunitReport {
		t.Errorf("step two corrupted: %+v", spec.Steps[1])
	}
	if spec.Steps[0].Script != "echo hello" {
		t.Errorf("Script = %q, want %q", spec.Steps[0].Script, "echo hello")
	}
}

// BuildVarsCtx：扁平 GlobalContext → 两级命名空间视图（图纸级键 + job 输出点分键）
func TestBuildVarsCtx(t *testing.T) {
	tk := &models.WorkflowTask{GlobalContext: map[string]string{
		"env":         "prod",
		"build.IMAGE": "reg/app:v9",
	}}
	ctx := BuildVarsCtx(tk)
	if ctx["global"]["env"] != "prod" {
		t.Errorf(`ctx[global][env] = %q, want "prod"`, ctx["global"]["env"])
	}
	if ctx["build"]["IMAGE"] != "reg/app:v9" {
		t.Errorf(`ctx[build][IMAGE] = %q, want "reg/app:v9"`, ctx["build"]["IMAGE"])
	}
	// 空 GlobalContext：至少有 global 命名空间，渲染不炸
	ctx2 := BuildVarsCtx(&models.WorkflowTask{})
	if _, ok := ctx2["global"]; !ok {
		t.Error("empty task should still have global namespace")
	}
}

// jsonEscapeBody 输出必须是合法 JSON 字符串的"引号内转义体"
func TestJSONEscapeBody(t *testing.T) {
	cases := map[string]string{
		`plain`:       `plain`,
		`say "hi"`:    `say \"hi\"`,
		"line1\nline": `line1\nline`,
		`a<b>&c`:      `a<b>&c`, // HTML 字符不转义
		`back\slash`:  `back\\slash`,
	}
	for in, want := range cases {
		got := jsonEscapeBody(in)
		if got != want {
			t.Errorf("jsonEscapeBody(%q) = %q, want %q", in, got, want)
		}
		// 转义体包上引号必须能被 JSON 解析回原值
		var back string
		if err := json.Unmarshal([]byte(`"`+got+`"`), &back); err != nil || back != in {
			t.Errorf("roundtrip %q: got %q err %v", in, back, err)
		}
	}
}
