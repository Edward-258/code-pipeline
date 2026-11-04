package models

import "time"

// WorkflowTask —— 施工档案（对照 Zadig models/wokflow_task_v4.go，文件名拼写错误是上游的）
// 关键设计：WorkflowArgs 内嵌【当时的完整图纸快照】，改图纸不影响查历史、克隆重跑
type WorkflowTask struct {
	ID            string            `bson:"_id"               json:"id"` // 自然键 <workflow_name>-<task_id>，插入前就定好
	TaskID        int64             `bson:"task_id"           json:"task_id"` // counter 发的号
	WorkflowName  string            `bson:"workflow_name"     json:"workflow_name"`
	Status        Status            `bson:"status"            json:"status"`
	Creator       string            `bson:"creator"           json:"creator"`
	WorkflowArgs  *Workflow         `bson:"workflow_args"     json:"workflow_args"` // ⭐ 快照
	GlobalContext map[string]string `bson:"global_context,omitempty" json:"global_context,omitempty"` // M4: {{.jobX.output}}
	Stages        []*StageTask      `bson:"stages"            json:"stages"`
	StartTime     int64             `bson:"start_time,omitempty" json:"start_time,omitempty"`
	EndTime       int64             `bson:"end_time,omitempty"   json:"end_time,omitempty"`
	Error         string            `bson:"error,omitempty"      json:"error,omitempty"`
	CreatedAt     time.Time         `bson:"created_at"        json:"created_at"`
}

// StageTask —— 阶段运行时（对照 Zadig StageTask）：独立状态、独立起止时间
type StageTask struct {
	Name       string      `bson:"name"                 json:"name"`
	Status     Status      `bson:"status"               json:"status"`
	Parallel   bool        `bson:"parallel"             json:"parallel"`
	ManualExec *ManualExec `bson:"manual_exec,omitempty" json:"manual_exec,omitempty"` // 运行时副本（Executed 会在这里翻 true）
	StartTime  int64       `bson:"start_time,omitempty" json:"start_time,omitempty"`
	EndTime    int64       `bson:"end_time,omitempty"   json:"end_time,omitempty"`
	Error      string      `bson:"error,omitempty"      json:"error,omitempty"`
	Jobs       []*JobTask  `bson:"jobs"                 json:"jobs"`
}

// JobTask —— 作业运行时（对照 Zadig JobTask）
type JobTask struct {
	Name       string            `bson:"name"            json:"name"`
	JobType    string            `bson:"type"            json:"type"`
	Status     Status            `bson:"status"          json:"status"`
	K8sJobName string            `bson:"k8s_job_name"    json:"k8s_job_name"` // freestyle：真实 K8s Job 名；deploy 无执行物（留空语义但字段保留）
	Spec       *FreestyleSpec    `bson:"spec"            json:"spec"`
	DeploySpec *DeploySpec       `bson:"deploy_spec,omitempty" json:"deploy_spec,omitempty"` // deploy job 专用（T2.2）
	Steps      []*StepTask       `bson:"steps,omitempty" json:"steps,omitempty"` // ⭐ 分界面的叶子层
	Outputs    map[string]string `bson:"outputs,omitempty" json:"outputs,omitempty"` // M4
	Error      string            `bson:"error,omitempty" json:"error,omitempty"`
	Retry      int               `bson:"retry"           json:"retry"`
}

// StepTask —— 步骤运行时（对照 Zadig StepTask）：三级状态树 stages→jobs→steps 的叶子，
// "分节点分界面"的数据源。状态由引擎刮 Pod 日志 banner 反解（engine/logs.go），非 runner 回报。
type StepTask struct {
	Name       string      `bson:"name"                 json:"name"`
	Type       string      `bson:"type"                 json:"type"`
	Status     Status      `bson:"status"               json:"status"`
	StartTime  int64       `bson:"start_time,omitempty" json:"start_time,omitempty"`
	EndTime    int64       `bson:"end_time,omitempty"   json:"end_time,omitempty"`
	Error      string      `bson:"error,omitempty"      json:"error,omitempty"`
	Log        string      `bson:"log,omitempty"        json:"log,omitempty"` // 分步日志（banner 切分，存档前截断 64KB）
	TestResult *TestResult `bson:"test_result,omitempty" json:"test_result,omitempty"` // junit_report 步骤的统计
}

// TestResult —— junit XML 聚合统计（分界面展示源；门禁 = 通过率 ≥ PassRate）
type TestResult struct {
	Tests    int     `bson:"tests"    json:"tests"`
	Failures int     `bson:"failures" json:"failures"`
	Errors   int     `bson:"errors"   json:"errors"`
	Skipped  int     `bson:"skipped"  json:"skipped"`
	Time     float64 `bson:"time"     json:"time"`
}
