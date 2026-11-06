package step

import (
	"strings"
	"testing"

	"pipeline/pkg/models"
)

// TestGitCtlPreRun —— git 片段渲染：token 注入、目录推断、默认分支
func TestGitCtlPreRun(t *testing.T) {
	get := func(s *models.StepSpec) string {
		frag, err := (&gitCtl{spec: s}).PreRun()
		if err != nil {
			t.Fatalf("PreRun: %v", err)
		}
		return frag
	}

	frag := get(&models.StepSpec{Name: "g", Type: models.StepGit, Repo: "https://github.com/o/r.git"})
	if !strings.Contains(frag, "git clone --depth 1 -b 'main'") { // 默认分支 main
		t.Errorf("default branch missing: %s", frag)
	}
	if !strings.Contains(frag, "'r'") { // 目录名从 URL 尾段推（剥 .git）
		t.Errorf("dir name not inferred: %s", frag)
	}
	if !strings.Contains(frag, "cd 'r'") { // clone 后留在仓库目录（后续步骤在源码根上跑）
		t.Errorf("cd missing: %s", frag)
	}

	frag = get(&models.StepSpec{Type: models.StepGit, Repo: "https://github.com/o/r.git", Token: "ghp_x"})
	if !strings.Contains(frag, "https://ghp_x@github.com/o/r.git") {
		t.Errorf("token not injected: %s", frag)
	}
	frag = get(&models.StepSpec{Type: models.StepGit, Repo: "https://github.com/o/r", Token: "user:pass"})
	if !strings.Contains(frag, "https://user:pass@github.com/o/r") {
		t.Errorf("user:pass token not injected: %s", frag)
	}
	frag = get(&models.StepSpec{Type: models.StepGit, Repo: "https://gitee.com/a/b.git", ContextDir: "src"})
	if !strings.Contains(frag, "'src'") || strings.Contains(frag, "'b'") {
		t.Errorf("context_dir override failed: %s", frag)
	}
}

// TestDockerBuildCtlPreRun —— 构建片段：registry 前缀注入与已带前缀的豁免
// ⚠️ 前缀段必须是 "$MP_NODE_REGISTRY"/ 双引号形态（sh 不展开单引号里的变量，
// 幕1 首跑时单引号形态让 docker 收到字面量报 exit=125——测试要钉住的是 shell 语义不是字符串长相）
func TestDockerBuildCtlPreRun(t *testing.T) {
	frag, _ := (&dockerBuildCtl{spec: &models.StepSpec{Type: models.StepDockerBuild, Image: "mini-demo"}}).PreRun()
	if !strings.Contains(frag, `-t "$MP_NODE_REGISTRY"/'mini-demo:latest'`) ||
		!strings.Contains(frag, `docker push "$MP_NODE_REGISTRY"/'mini-demo:latest'`) {
		t.Errorf("bare image should get expandable registry prefix + latest tag: %s", frag)
	}

	frag, _ = (&dockerBuildCtl{spec: &models.StepSpec{Type: models.StepDockerBuild, Image: "172.29.237.250:3050/x", Tag: "v2", Dockerfile: "build/Dockerfile", ContextDir: "cmd/app"}}).PreRun()
	if strings.Contains(frag, "MP_NODE_REGISTRY") { // 已带前缀不重复加
		t.Errorf("prefixed image must not get double prefix: %s", frag)
	}
	if !strings.Contains(frag, "-f 'build/Dockerfile'") || !strings.Contains(frag, "'cmd/app'") {
		t.Errorf("dockerfile/context not rendered: %s", frag)
	}
}

// TestJunitGate —— 门禁判定表：解析（单表/包装层/拼接）+ 通过率翻转
func TestJunitGate(t *testing.T) {
	const oneFail = `<?xml version="1.0"?><testsuite name="t" tests="4" failures="1" errors="0" skipped="0" time="0.5"></testsuite>`
	const allPass = `<testsuite name="t" tests="2" failures="0" errors="0" skipped="0" time="0.1"></testsuite>`

	run := func(log string, rate float64) *models.StepTask {
		st := &models.StepTask{Name: "r", Type: models.StepJunitReport, Status: models.StatusPassed, Log: log}
		ctl := &junitReportCtl{spec: &models.StepSpec{Type: models.StepJunitReport, PassRate: rate}}
		if err := ctl.AfterRun(st); err != nil {
			t.Fatalf("AfterRun returned engine error: %v", err) // 门禁失败是步骤失败，不是引擎错误
		}
		return st
	}

	st := run(oneFail, 1.0) // 3/4 = 75% < 100% → 翻红
	if st.Status != models.StatusFailed || !strings.Contains(st.Error, "75.0%") {
		t.Errorf("gate should fail at 75%% < 100%%: status=%s err=%s", st.Status, st.Error)
	}
	if st.TestResult == nil || st.TestResult.Tests != 4 || st.TestResult.Failures != 1 || st.TestResult.Time != 0.5 {
		t.Errorf("TestResult wrong: %+v", st.TestResult)
	}

	st = run(oneFail, 0.7) // 75% ≥ 70% → 放行
	if st.Status != models.StatusPassed {
		t.Errorf("gate 70%% should pass: %s", st.Status)
	}

	// <testsuites> 包装层不计（防双算），内层两张表拼接累加
	wrapped := `<testsuites tests="6" failures="1"><testsuite tests="3" failures="0" errors="0" time="1"></testsuite><testsuite tests="3" failures="1" errors="0" time="1"></testsuite></testsuites>`
	st = run(wrapped, 0.5) // 5/6 ≈ 83% ≥ 50%
	if st.Status != models.StatusPassed || st.TestResult.Tests != 6 || st.TestResult.Failures != 1 || st.TestResult.Time != 2 {
		t.Errorf("wrapped/concat aggregation wrong: status=%s res=%+v", st.Status, st.TestResult)
	}

	st = run("no xml here at all", 1.0) // 没产出 XML：解析失败 = 步骤失败
	if st.Status != models.StatusFailed || !strings.Contains(st.Error, "junit parse") {
		t.Errorf("missing xml should fail step: %s %s", st.Status, st.Error)
	}
	_ = allPass
}

// TestDockerBuildCtlRegistryRef —— 仓库引用档（2026-11-07）：全名拼接 + stdin 登录 + 单引号转义
func TestDockerBuildCtlRegistryRef(t *testing.T) {
	frag, err := (&dockerBuildCtl{spec: &models.StepSpec{
		Name: "img", Type: models.StepDockerBuild, Image: "codepipeline-learning/app", Tag: "v2",
		RegAddr: "crpi-x.aliyuncs.com", RegUser: "nick", RegPass: "p'a$$",
	}}).PreRun()
	if err != nil {
		t.Fatalf("PreRun: %v", err)
	}
	if !strings.Contains(frag, `-t 'crpi-x.aliyuncs.com/codepipeline-learning/app:v2'`) {
		t.Errorf("full image name addr/relative:tag expected: %s", frag)
	}
	if !strings.Contains(frag, `docker login -u 'nick' --password-stdin 'crpi-x.aliyuncs.com'`) {
		t.Errorf("stdin login expected: %s", frag)
	}
	// login 链路必须包在子 shell + 临时 DOCKER_CONFIG 里（/root/.docker hostPath 只读，login 写不进去）
	if !strings.Contains(frag, "( export DOCKER_CONFIG=/tmp/.docker && ") || !strings.HasSuffix(frag, " )") {
		t.Errorf("login must be subshell-wrapped with temp DOCKER_CONFIG: %s", frag)
	}
	// 密码含单引号必须被 POSIX 转义（p'a$$ → 'p'\''a$$'），否则驱动脚本被掀翻
	if !strings.Contains(frag, "printf '%s' 'p'\\''a$$'") {
		t.Errorf("password must be single-quote escaped: %s", frag)
	}
	if strings.Contains(frag, "MP_NODE_REGISTRY") {
		t.Errorf("ref mode must not use local registry prefix: %s", frag)
	}

	// 免认证仓库：无 username 则跳过 login（registry:2 类内网裸仓）
	frag, _ = (&dockerBuildCtl{spec: &models.StepSpec{
		Name: "img", Type: models.StepDockerBuild, Image: "app",
		RegAddr: "registry.pipeline.svc:5000",
	}}).PreRun()
	if strings.Contains(frag, "docker login") {
		t.Errorf("credential-less registry must skip login: %s", frag)
	}
	if !strings.Contains(frag, "-t 'registry.pipeline.svc:5000/app:latest'") {
		t.Errorf("default tag latest expected: %s", frag)
	}
}
