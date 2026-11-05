package step

import (
	"fmt"
	"regexp"
	"strconv"

	"pipeline/pkg/models"
)

// junit_report 步骤：收集 XML 进步骤日志 + 引擎侧门禁（AfterRun 解析判定）。

type junitReportCtl struct{ spec *models.StepSpec }

func (c *junitReportCtl) PreRun() (string, error) {
	glob := c.spec.ReportGlob
	if glob == "" {
		glob = "*.xml"
	}
	prefix := ""
	if d := c.spec.ContextDir; d != "" {
		prefix = fmt.Sprintf("cd '%s' && ", d)
	}
	// 报告全文 cat 进步骤日志 → 引擎 AfterRun 从日志里解析（密封回路，不需要 Pod 传文件出来）。
	// glob 不命中时 cat 拿到字面量报 No such file → 步骤失败（测试没产出报告就该红）
	return prefix + "cat " + glob, nil
}

// AfterRun —— 门禁判定（引擎侧）：解析步骤日志里的 junit XML → TestResult 落档案 →
// 通过率 < PassRate 则把步骤翻成 failed（对照 Zadig 默认 100% 门禁）。
// ⚠️ 必须在日志截断之前调用（引擎要读完整 XML）——见 FinalizeSteps 的顺序。
func (c *junitReportCtl) AfterRun(st *models.StepTask) error {
	res, err := parseJunitXML(st.Log)
	if err != nil {
		st.Status = models.StatusFailed
		st.Error = fmt.Sprintf("junit parse: %v", err)
		return nil // 门禁失败 = 步骤失败，不是引擎错误（对照 Zadig：不往上抛）
	}
	st.TestResult = res

	gate := c.spec.PassRate
	if gate <= 0 {
		gate = 1.0 // 默认 100%：一个用例都不许挂
	}
	if res.Tests == 0 {
		return nil // 合法 XML 但 0 用例：无事发生（是否算失败交给用户自己写 shell 断言）
	}
	passed := res.Tests - res.Failures - res.Errors
	if rate := float64(passed) / float64(res.Tests); rate+1e-9 < gate {
		st.Status = models.StatusFailed
		st.Error = fmt.Sprintf("test pass rate %.1f%% below gate %.1f%% (%d/%d passed)",
			rate*100, gate*100, passed, res.Tests)
	}
	return nil
}

// ---------- junit XML 解析（正则流派：容错拼接/不要求整体 well-formed） ----------

// 测试统计长在 <testsuite ...> 开标签的属性上（go-junit/pytest/surefire 同构）；
// <testsuites> 只是包装层不累计，跳过防双算。多文件 cat 拼接 = 多个开标签，逐个累加。
var (
	testsuiteTagRe = regexp.MustCompile(`(?s)<testsuite\b[^>]*>`)
	attrRe         = func(name string) *regexp.Regexp {
		return regexp.MustCompile(`\b` + name + `\s*=\s*"([^"]*)"`)
	}
)

func parseJunitXML(log string) (*models.TestResult, error) {
	res := &models.TestResult{}
	found := false
	for _, tag := range testsuiteTagRe.FindAllString(log, -1) {
		found = true
		res.Tests += attrInt(tag, "tests")
		res.Failures += attrInt(tag, "failures")
		res.Errors += attrInt(tag, "errors")
		res.Skipped += attrInt(tag, "skipped")
		res.Time += attrFloat(tag, "time")
	}
	if !found {
		return nil, fmt.Errorf("no <testsuite> element found in step log")
	}
	return res, nil
}

func attrInt(tag, name string) int {
	m := attrRe(name).FindStringSubmatch(tag)
	if m == nil {
		return 0
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	return int(f + 0.5)
}

func attrFloat(tag, name string) float64 {
	m := attrRe(name).FindStringSubmatch(tag)
	if m == nil {
		return 0
	}
	f, _ := strconv.ParseFloat(m[1], 64)
	return f
}
