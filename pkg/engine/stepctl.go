package engine

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"pipeline/pkg/models"
)

// StepCtl —— aslan 侧步骤控制器的极简注册表（对照 stepcontroller/step.go:29 的 17 种，
// 学习版首发 4 种；新步骤类型 = 实现接口 + newStepCtl 加 case，这就是"极具扩展性"的落点）。
//
// PreRun：把步骤规格渲染成一段 POSIX sh 片段（driver.go 拼进驱动脚本，Pod 内执行）
// AfterRun：终态善后——只有 junit_report 用到（引擎侧解析 XML → TestResult → 门禁；
// Zadig 的门禁在执行器侧 jobexecutor step_junit_report.go:118-130，语义等价）
type StepCtl interface {
	PreRun() (string, error)
	AfterRun(st *models.StepTask) error
}

func newStepCtl(spec *models.StepSpec) StepCtl {
	switch spec.Type {
	case models.StepGit:
		return &GitCtl{spec: spec}
	case models.StepDockerBuild:
		return &DockerBuildCtl{spec: spec}
	case models.StepJunitReport:
		return &JunitReportCtl{spec: spec}
	case models.StepArchive:
		return &ArchiveCtl{spec: spec}
	default:
		return &ShellCtl{spec: spec}
	}
}

// ---------- shell：任意脚本直通 ----------

type ShellCtl struct{ spec *models.StepSpec }

func (c *ShellCtl) PreRun() (string, error) { return c.spec.Script, nil }
func (c *ShellCtl) AfterRun(*models.StepTask) error { return nil }

// ---------- git：clone --depth 1 后把 shell 留在仓库目录（后续步骤天然在源码根上跑） ----------

type GitCtl struct{ spec *models.StepSpec }

func (c *GitCtl) PreRun() (string, error) {
	if c.spec.Repo == "" {
		return "", fmt.Errorf("git step needs repo")
	}
	repo := c.spec.Repo
	if c.spec.Token != "" {
		repo = injectToken(repo, c.spec.Token) // 私有仓库：令牌进 URL，不落 ps 可见的命令行……好吧还是命令行，学习版
	}
	dir := c.spec.ContextDir
	if dir == "" {
		dir = repoDirName(c.spec.Repo) // 未指定目录 = 从 URL 尾段推
	}
	branch := c.spec.Branch
	if branch == "" {
		branch = "main"
	}
	return fmt.Sprintf("rm -rf '%s' && git clone --depth 1 -b '%s' '%s' '%s' && cd '%s'",
		dir, branch, repo, dir, dir), nil
}
func (c *GitCtl) AfterRun(*models.StepTask) error { return nil }

// injectToken —— https://host/o/r(.git) → https://<token>@host/o/r(.git)；
// token 形如 "user:pass" 时用 UserPassword，纯 token 用 User
func injectToken(repo, token string) string {
	u, err := url.Parse(repo)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return repo // 解析不动就原样返回（clone 自己会报错，错误信息足够定位）
	}
	if i := strings.Index(token, ":"); i > 0 {
		u.User = url.UserPassword(token[:i], token[i+1:])
	} else {
		u.User = url.User(token)
	}
	return u.String()
}

var unsafeDirChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// repoDirName —— URL 尾段当目录名（github.com/o/r.git → r），非法字符压成 '-'
func repoDirName(repo string) string {
	s := strings.TrimSuffix(repo, ".git")
	if i := strings.LastIndexAny(s, "/:"); i >= 0 {
		s = s[i+1:]
	}
	s = unsafeDirChars.ReplaceAllString(s, "-")
	if s == "" {
		s = "repo"
	}
	return s
}

// ---------- docker_build：遥控宿主 daemon 构建 + 推集群内 registry:2 ----------

type DockerBuildCtl struct{ spec *models.StepSpec }

func (c *DockerBuildCtl) PreRun() (string, error) {
	if c.spec.Image == "" {
		return "", fmt.Errorf("docker_build step needs image")
	}
	name := c.spec.Image
	prefix := ""
	if !hasRegistryPrefix(name) {
		// 决断 #3：产物进集群 registry:2，kubelet 走 localhost:5000 mirror 拉取。
		// ⚠️ 前缀段必须放单引号外用双引号包住——sh 不展开单引号里的变量，
		// 整段单引号会让 docker 收到字面量 "$MP_NODE_REGISTRY/..."（exit=125 invalid reference，
		// 幕1 首跑实录）。相邻引号段拼接是一个词："$MP_NODE_REGISTRY"/'mini-demo':'0.1.0'。
		prefix = `"$MP_NODE_REGISTRY"/`
	}
	tag := c.spec.Tag
	if tag == "" {
		tag = "latest"
	}
	df := c.spec.Dockerfile
	if df == "" {
		df = "Dockerfile"
	}
	ctx := c.spec.ContextDir
	if ctx == "" {
		ctx = "."
	}
	return fmt.Sprintf("docker build -f '%s' -t %s'%s:%s' '%s' && docker push %s'%s:%s'",
		df, prefix, name, tag, ctx, prefix, name, tag), nil
}
func (c *DockerBuildCtl) AfterRun(*models.StepTask) error { return nil }

// hasRegistryPrefix —— 首段含 '.'/':' 或就是 localhost 视为已带仓库前缀
// （"mini-demo" → 要加 $MP_NODE_REGISTRY/ 前缀；"172.29.x.x:3050/foo" → 原样）
func hasRegistryPrefix(image string) bool {
	first := image
	if i := strings.Index(image, "/"); i >= 0 {
		first = image[:i]
	}
	return first == "localhost" || strings.ContainsAny(first, ".:")
}

// ---------- junit_report：收集 XML 进步骤日志 + 引擎侧门禁 ----------

type JunitReportCtl struct{ spec *models.StepSpec }

func (c *JunitReportCtl) PreRun() (string, error) {
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
// ⚠️ 必须在日志截断之前调用（引擎要读完整 XML）——见 finalizeSteps 的顺序。
func (c *JunitReportCtl) AfterRun(st *models.StepTask) error {
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

// ---------- archive：打包产物 → curl 上传引擎 GridFS（T2.4） ----------

type ArchiveCtl struct{ spec *models.StepSpec }

// PreRun —— tar czf 工作区产物 → --data-binary POST 引擎 artifact 端点。
// 上传走日志流之外的第二信道（HTTP），产物不进日志（64KB 截断装不下二进制）。
// Zadig 的三种归档步骤（step_archive/tar_archive/html_report）在 S3 语义上等价，
// 学习版砍 S3 用引擎 GridFS，三种合一种。
func (c *ArchiveCtl) PreRun() (string, error) {
	if c.spec.ArtifactGlob == "" {
		return "", fmt.Errorf("archive step needs artifact_glob")
	}
	name := c.spec.ArtifactName
	if name == "" {
		name = c.spec.Name + ".tgz"
	}
	name = unsafeDirChars.ReplaceAllString(name, "-") // 进 URL 路径段：白名单消毒（$ 等在双引号里照样会被 shell 展开）
	prefix := ""
	if d := c.spec.ContextDir; d != "" {
		prefix = fmt.Sprintf("cd '%s' && ", d)
	}
	// glob 不命中：busybox tar 报 can't find → 步骤失败（产物缺席就该红，不该静默空包）
	// 上传用 curl --data-binary（0.6.x 活体抓的：busybox wget --post-file 在引擎响应到达时
	// 提前断开，POST body 截成 0~41 字节还回 200——静默腐败；archive 步骤的 job 因此落
	// runner 镜像（jobNeedsRunner），curl 在 runner 里）。-f：非 2xx 直接非零退出，步骤翻红
	// URL 整体双引号：$ 变量在双引号内照常展开（docker_build 前缀的分段拼接是单引号场景的特例，这里用不上）
	return prefix + fmt.Sprintf(
		"tar czf /tmp/mp-artifact.tgz %s && curl -s -f -X POST -H 'Content-Type: application/gzip' --data-binary @/tmp/mp-artifact.tgz \"$MP_ENGINE_URL/api/tasks/$MP_TASK_WF/$MP_TASK_ID/artifact/%s\" && rm -f /tmp/mp-artifact.tgz",
		c.spec.ArtifactGlob, name), nil
}

func (c *ArchiveCtl) AfterRun(*models.StepTask) error { return nil }

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
