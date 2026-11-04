package engine

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"pipeline/pkg/models"
)

// 变量渲染内核（M4/T1.1）。
// 思想出处：Zadig workflowcontroller/workflow.go 的 GlobalContext Get/Set + jobcontroller 的
// "整体 JSON marshal → 字符串替换 → unmarshal" 粗暴渲染协议。学习版差异：
//   - Zadig 扁平键用 "@?" 做分隔（GetContextKey），我们简化为 "."（job 名约束：不含点）；
//   - Zadig 用 template 包逐步渲染，我们一次 marshal 全量替换——多文件字段一次到位，无需遍历。
//
// 上下文两级命名空间（VarsCtx）：
//   - {{.global.<k>}}   → 图纸级/任务级全局变量（t.GlobalContext 里无 "." 的键）
//   - {{.<job>.<out>}}  → 上游 job 的输出（t.GlobalContext 里 "<job>.<out>" 形态的键，T1.3 收割）

// VarsCtx —— 渲染上下文：命名空间 → 键 → 值
type VarsCtx = map[string]map[string]string

// placeholderRe —— 两级键占位符：{{.ns.key}}。键字符集 = 字母数字下划线中划线
// （job 名与变量名都不含点，这正是扁平存储能用 "." 做分隔的前提）。
var placeholderRe = regexp.MustCompile(`\{\{\.([0-9A-Za-z_-]+)\.([0-9A-Za-z_-]+)\}\}`)

// RenderVars —— 把 spec（指针）里所有字符串字段中的占位符替换为上下文值。
// 原地渲染：unmarshal 回同一指针。未命中的占位符保留原文（半配置半渲染的图纸更可诊断）。
func RenderVars(spec any, ctx VarsCtx) error {
	raw, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshal spec: %w", err)
	}
	out := placeholderRe.ReplaceAllFunc(raw, func(m []byte) []byte {
		sub := placeholderRe.FindSubmatch(m)
		if ns, ok := ctx[string(sub[1])]; ok {
			if v, ok := ns[string(sub[2])]; ok {
				return []byte(jsonEscapeBody(v))
			}
		}
		return m // 未命中：保留原文
	})
	if err := json.Unmarshal(out, spec); err != nil {
		return fmt.Errorf("unmarshal rendered spec: %w", err)
	}
	return nil
}

// jsonEscapeBody —— 把任意字符串变成"可安全嵌进 JSON 字符串字面量的转义体"。
// 关键：SetEscapeHTML(false)——脚本内容里 <、>、&&、|| 是常客，Go 默认的 HTML 转义
// 会把它们变 \u003c 之类，渲染出来的脚本就没法看了。
func jsonEscapeBody(v string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return v // string 编码不会失败，防御性兜底
	}
	s := strings.TrimSuffix(b.String(), "\n") // Encode 尾巴上带换行
	return s[1 : len(s)-1]                    // 剥掉包裹引号，只留转义体
}

// BuildVarsCtx —— 渲染上下文装配：t.GlobalContext 是唯一真相源（图纸级初始值 + T1.3 收割的 job 输出）。
// 扁平键的两种形态（约定）：
//   - 无 "."        → 全局变量，暴露为 {{.global.<k>}}
//   - "<job>.<out>" → job 输出，暴露为 {{.<job>.<out>}}
func BuildVarsCtx(t *models.WorkflowTask) VarsCtx {
	ctx := VarsCtx{"global": map[string]string{}}
	for k, v := range t.GlobalContext {
		if i := strings.Index(k, "."); i > 0 && i < len(k)-1 {
			job, key := k[:i], k[i+1:]
			if ctx[job] == nil {
				ctx[job] = map[string]string{}
			}
			ctx[job][key] = v
		} else {
			ctx["global"][k] = v
		}
	}
	return ctx
}
