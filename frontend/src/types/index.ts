// 领域类型 —— 对齐 pkg/models 的 json tag（bson 不关心，前端只见 json）
// 字段拼写是契约：Job/Step 的类型字段是 `type`，不是 job_type/step_type

export type Status =
  | 'created' | 'queued' | 'running' | 'passed'
  | 'failed' | 'cancelled' | 'skipped' | 'pause'

export type JobType = 'freestyle' | 'deploy'
export type StepType = 'shell' | 'git' | 'docker_build' | 'junit_report' | 'archive'
export type ErrorPolicy = 'stop' | 'retry' | 'ignore'
export type ApprovePolicy = 'any' | 'all'

// ---------- 图纸（Workflow / Stage / Job / Step 四层） ----------

export interface StepSpec {
  name: string
  type: StepType
  // shell
  script?: string
  // git
  repo?: string
  branch?: string
  token?: string
  context_dir?: string
  // docker_build
  image?: string
  tag?: string
  dockerfile?: string
  // junit_report
  report_glob?: string
  pass_rate?: number
  // archive
  artifact_glob?: string
  artifact_name?: string
}

export interface FreestyleSpec {
  image?: string
  scripts?: string[]
  steps?: StepSpec[]
  retry?: number
  timeout_minutes?: number
  error_policy?: ErrorPolicy
}

export interface DeploySpec {
  env: string
  deployment_name: string
  container_name: string
  image: string
  wait_rollout?: boolean
}

export interface Job {
  name: string
  type?: JobType
  spec?: FreestyleSpec
  deploy_spec?: DeploySpec
}

export interface Approval {
  approver: string
  comment?: string
  time?: number
}

export interface ManualExec {
  enabled?: boolean
  executed?: boolean
  notification_sent?: boolean
  approvers?: string[]
  policy?: ApprovePolicy
  approvals?: Approval[]
}

export interface Stage {
  name: string
  parallel?: boolean
  manual_exec?: ManualExec
  jobs: Job[]
}

export interface WebhookConfig {
  repo: string
  branch: string
  secret?: string
}

export interface Workflow {
  name: string
  stages: Stage[]
  global_context?: Record<string, string>
  webhooks?: WebhookConfig[]
  created_at?: string
  updated_at?: string
}

// ---------- 运行时（WorkflowTask 三级树） ----------

export interface TestResult {
  tests?: number
  failures?: number
  errors?: number
  skipped?: number
  time?: number
}

export interface StepTask {
  name: string
  type?: StepType
  status?: Status
  log?: string
  error?: string
  test_result?: TestResult
  start_time?: number
  end_time?: number
}

export interface JobTask {
  name: string
  type?: JobType
  status?: Status
  k8s_job_name?: string
  spec?: FreestyleSpec
  deploy_spec?: DeploySpec
  steps?: StepTask[]
  outputs?: Record<string, string>
  error?: string
  retry?: number
}

export interface StageTask {
  name: string
  status?: Status
  parallel?: boolean
  manual_exec?: ManualExec
  start_time?: number
  end_time?: number
  error?: string
  jobs?: JobTask[]
}

export interface WorkflowTask {
  id: string
  task_id: number
  workflow_name: string
  status: Status
  creator?: string
  workflow_args?: Workflow
  global_context?: Record<string, string>
  stages?: StageTask[]
  start_time?: number
  end_time?: number
  error?: string
  created_at?: string
}

export interface TaskListResp {
  items: WorkflowTask[]
  total: number
  page: number
  size: number
}

// ---------- 周边资源 ----------

export interface Env {
  name: string
  namespace: string
  desc?: string
  created_at?: string
}

export interface NotifyCfg {
  name: string
  type: 'feishu' | 'dingtalk'
  webhook_url: string
  secret?: string
  enabled?: boolean
  created_at?: string
  updated_at?: string
}

export interface CronCfg {
  name: string
  workflow_name: string
  crontab: string
  enabled: boolean
  created_at?: string
  updated_at?: string
}

export const TERMINAL_STATUSES: Status[] = ['passed', 'failed', 'cancelled', 'skipped']

export const isTerminal = (s?: Status): boolean => !!s && TERMINAL_STATUSES.includes(s)
