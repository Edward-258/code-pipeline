// Package step —— 脚本执行层：把步骤规格编译成 POSIX sh 驱动脚本（driver.go）、
// 五种 StepCtl 的 PreRun 片段渲染（各步骤一文件）、banner 协议解析/切分/收口
// （logs.go——Pod stdout 就是步骤状态信道）。只 import models。
package step

import (
	"regexp"

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
		return &gitCtl{spec: spec}
	case models.StepDockerBuild:
		return &dockerBuildCtl{spec: spec}
	case models.StepJunitReport:
		return &junitReportCtl{spec: spec}
	case models.StepArchive:
		return &archiveCtl{spec: spec}
	default:
		return &shellCtl{spec: spec}
	}
}

// unsafeDirChars —— 目录/文件名消毒白名单（git 目录名与 archive 产物名共用）
var unsafeDirChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
