package models

import (
	"strings"
	"time"
)

// Workflow —— 图纸（对照 Zadig models/workflow_v4.go 的 WorkflowV4）
// 学习版差异：44 种 job 类型砍到只剩 freestyle，Spec 直接是具体结构体而非 interface{}
type Workflow struct {
	Name          string            `bson:"_id"       json:"name"` // 图纸名即主键
	Stages        []*Stage          `bson:"stages"    json:"stages"`
	GlobalContext map[string]string `bson:"global_context,omitempty" json:"global_context,omitempty"` // 图纸级变量初始值（开跑时拷进 task.GlobalContext，M4）
	CreatedAt     time.Time         `bson:"created_at" json:"created_at"`
	UpdatedAt     time.Time         `bson:"updated_at" json:"updated_at"`
}

// Stage —— 阶段（对照 Zadig WorkflowStage）
// 两个"阀门"保留其一：ManualExec（手动分步执行，M3 灵魂）；审批砍掉
type Stage struct {
	Name       string      `bson:"name"                 json:"name"`
	Parallel   bool        `bson:"parallel"             json:"parallel"` // 阶段内 job 并行/串行
	ManualExec *ManualExec `bson:"manual_exec,omitempty" json:"manual_exec,omitempty"`
	Jobs       []*Job      `bson:"jobs"                 json:"jobs"`
}

// ManualExec —— 手动分步执行（对照 Zadig stage.ManualExec）
// Enabled: 停下来等人;  Executed: 人已经放行过;  NotificationSent: 通知去重（学习版占位）
type ManualExec struct {
	Enabled          bool `bson:"enabled"           json:"enabled"`
	Executed         bool `bson:"executed"          json:"executed"`
	NotificationSent bool `bson:"notification_sent" json:"notification_sent"`
}

// Job —— 任务单元（对照 Zadig Job）
type Job struct {
	Name       string      `bson:"name" json:"name"`
	JobType    string      `bson:"type" json:"type"` // freestyle（跑步骤）| deploy（patch 部署，T2.2）
	Spec       *FreestyleSpec `bson:"spec" json:"spec"` // freestyle 专用；deploy 时可为 nil
	DeploySpec *DeploySpec `bson:"deploy_spec,omitempty" json:"deploy_spec,omitempty"` // deploy 专用
}

// JobType 常量（对照 Zadig 47 种 Job 里我们只长两种）
const (
	JobTypeFreestyle = "freestyle"
	JobTypeDeploy    = "deploy" // CD 本体：引擎直接 patch Deployment（不变量3：不起 K8s Job 不挂 sock）
)

// DeploySpec —— 部署作业规格（T2.2，对照 Zadig job_deploy.go 的极简版）：
// 目标 env（=namespace）+ Deployment 名 + 容器名 + 新镜像（可 {{.jobName.output}} 引用上游）。
type DeploySpec struct {
	Env            string `bson:"env"                 json:"env"`             // env 集合里的名字（T2.1）→ 解析出 namespace
	DeploymentName string `bson:"deployment_name"     json:"deployment_name"`
	ContainerName  string `bson:"container_name"      json:"container_name"`
	Image          string `bson:"image"               json:"image"`
	WaitRollout    *bool  `bson:"wait_rollout,omitempty" json:"wait_rollout,omitempty"` // 默认 true；自举发布自己更新自己时设 false 防绞杀
}

// ShouldWaitRollout —— WaitRollout 缺省 true 的取值助手
func (d *DeploySpec) ShouldWaitRollout() bool {
	return d.WaitRollout == nil || *d.WaitRollout
}

// FreestyleSpec —— freestyle 作业规格：镜像 + 步骤列表
// 对照 Zadig 的 JobTaskFreestyleSpec（Properties+Steps 简化成两字段）
type FreestyleSpec struct {
	Image          string      `bson:"image,omitempty"           json:"image,omitempty"`           // 空 = 按步骤需要选默认（见 engine.buildJob）
	Scripts        []string    `bson:"scripts,omitempty"         json:"scripts,omitempty"`         // 旧图纸兼容：翻译成隐式 step "main"
	Steps          []*StepSpec `bson:"steps,omitempty"           json:"steps,omitempty"`           // ⭐ 步骤列表（CI/CD 核心）
	Retry          int         `bson:"retry,omitempty"           json:"retry,omitempty"`           // 单 job 重试次数（默认 0=不重试，M4）
	TimeoutMinutes int         `bson:"timeout_minutes,omitempty" json:"timeout_minutes,omitempty"` // 单 job 超时（分钟，默认 10，T1.9）
	ErrorPolicy    string      `bson:"error_policy,omitempty"    json:"error_policy,omitempty"`    // 错误策略（默认 stop，T1.9）
}

// 错误策略（T1.9，对照 Zadig JobErrorPolicy 的 stop/retry；manual-check 砍进 B5 池）。
// 控制/上报分离：策略只改控制流（刹不刹车、重不重试），失败永远如实上报（job/stage 照红）。
const (
	ErrorPolicyStop   = "stop"   // 失败即刹车（默认；未设策略的旧图纸同此 + Retry 预算照用）
	ErrorPolicyRetry  = "retry"  // 失败按 Retry 预算重试，耗尽即刹车
	ErrorPolicyIgnore = "ignore" // 失败记红不刹车：串行继续下一个 job、后续 stage 照跑（task 终态由最差者胜如实给 failed）
)

// StepSpec —— 步骤规格（图纸层，对照 Zadig 的各 StepModel；字段按 Type 取用）
type StepSpec struct {
	Name       string  `bson:"name"                 json:"name"`
	Type       string  `bson:"type"                 json:"type"` // 见下方 Step* 常量
	Script     string  `bson:"script,omitempty"     json:"script,omitempty"`      // shell：多行脚本
	Repo       string  `bson:"repo,omitempty"       json:"repo,omitempty"`        // git：https 地址
	Branch     string  `bson:"branch,omitempty"     json:"branch,omitempty"`      // git：默认 main
	Token      string  `bson:"token,omitempty"      json:"token,omitempty"`       // git：私有仓库访问令牌（注入 URL）
	ContextDir string  `bson:"context_dir,omitempty" json:"context_dir,omitempty"` // git 检出目录 / docker_build 上下文（相对当前目录）
	Image      string  `bson:"image,omitempty"      json:"image,omitempty"`       // docker_build：产物镜像名
	Tag        string  `bson:"tag,omitempty"        json:"tag,omitempty"`         // docker_build：默认 latest
	Dockerfile string  `bson:"dockerfile,omitempty" json:"dockerfile,omitempty"`  // docker_build：默认 ./Dockerfile
	ReportGlob   string  `bson:"report_glob,omitempty"   json:"report_glob,omitempty"`   // junit_report：XML 通配
	PassRate     float64 `bson:"pass_rate,omitempty"     json:"pass_rate,omitempty"`     // junit_report：门禁 (0,1]，默认 1.0=100%
	ArtifactGlob string  `bson:"artifact_glob,omitempty" json:"artifact_glob,omitempty"` // archive：产物路径 glob（相对工作区）
	ArtifactName string  `bson:"artifact_name,omitempty" json:"artifact_name,omitempty"` // archive：产物名（默认 <step>.tgz；下载键 = <wf>-<taskID>-<step>）
}

// 步骤类型（对照 Zadig stepcontroller 的 17 种，学习版首发 5 种）
const (
	StepShell       = "shell"        // 任意 shell 片段
	StepGit         = "git"          // git clone --depth 1
	StepDockerBuild = "docker_build" // 宿主 docker.sock 构建 + 推 registry:2
	StepJunitReport = "junit_report" // 收集 junit XML + 通过率门禁（门禁判定在引擎侧 AfterRun）
	StepArchive     = "archive"      // T2.4：打包产物 → 上传引擎 GridFS → API 下载（Zadig 三种归档步骤合并为一种）
)

// EffectiveSteps —— 引擎只认 steps：新图纸用 spec.Steps；旧图纸只有 Scripts
// 就地翻译成单个隐式 shell 步骤 "main"（api 翻译层和 engine 驱动脚本共用这一个真相源）。
func (s *FreestyleSpec) EffectiveSteps() []*StepSpec {
	if len(s.Steps) > 0 {
		return s.Steps
	}
	if len(s.Scripts) > 0 {
		return []*StepSpec{{Name: "main", Type: StepShell, Script: strings.Join(s.Scripts, "\n")}}
	}
	return nil
}
