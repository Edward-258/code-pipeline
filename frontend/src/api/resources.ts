// API 资源层 —— 每个资源一个模块化函数组，路径与 pkg/api/router.go 一一对应
import { api, apiText, post, put, del } from './http'
import type {
  Workflow, WorkflowTask, TaskListResp, Env, Registry, Host, NotifyCfg, CronCfg,
} from '../types'

// ---------- workflow ----------

export const listWorkflows = () => api<Workflow[]>('/api/workflows')
export const getWorkflow = (name: string) => api<Workflow>(`/api/workflows/${name}`)
export const createWorkflow = (wf: Workflow) => post<Workflow>('/api/workflows', wf)
export const updateWorkflow = (name: string, wf: Workflow) =>
  put<Workflow>(`/api/workflows/${name}`, wf)
export const deleteWorkflow = (name: string) => del<{ message: string }>(`/api/workflows/${name}`)
export const runWorkflow = (name: string) =>
  post<WorkflowTask>(`/api/workflows/${name}/run`)

// ---------- task ----------

export interface TaskFilter {
  status?: string
  creator?: string
  from?: string
  to?: string
  page?: number
  size?: number
}

export const listTasks = (wf: string, f: TaskFilter = {}) => {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(f)) if (v !== undefined && v !== '') q.set(k, String(v))
  return api<TaskListResp>(`/api/tasks/${wf}?${q.toString()}`)
}
export const getTask = (wf: string, id: number | string) =>
  api<WorkflowTask>(`/api/tasks/${wf}/${id}`)
export const cancelTask = (wf: string, id: number | string) =>
  del<{ accepted?: boolean }>(`/api/tasks/${wf}/${id}`)
export const cloneTask = (wf: string, id: number | string) =>
  post<WorkflowTask>(`/api/tasks/${wf}/${id}/clone`)
export const retryTask = (wf: string, id: number | string) =>
  post<WorkflowTask>(`/api/tasks/${wf}/${id}/retry`)
export const releaseStage = (wf: string, id: number | string, stage: string) =>
  post<WorkflowTask>(`/api/tasks/${wf}/${id}/stages/${stage}/release`)
export const approveTask = (
  wf: string, id: number | string, body: { approver: string; comment?: string },
) => post<{ satisfied: boolean; approvals?: unknown[]; policy?: string }>(
  `/api/tasks/${wf}/${id}/approve`, body)
export const taskStepLog = (wf: string, id: number | string, stage: string, job: string, step: string) =>
  apiText(`/api/tasks/${wf}/${id}/log?stage=${encodeURIComponent(stage)}&job=${encodeURIComponent(job)}&step=${encodeURIComponent(step)}`)
export const artifactUrl = (wf: string, id: number | string, step: string) =>
  `/api/tasks/${wf}/${id}/artifact/${step}`

// ---------- env ----------

export const listEnvs = () => api<Env[]>('/api/envs')
export const createEnv = (e: Env) => post<Env>('/api/envs', e)
export const updateEnv = (name: string, e: Env) => put<Env>(`/api/envs/${name}`, e)
export const deleteEnv = (name: string) => del<{ deleted: string }>(`/api/envs/${name}`)

// ---------- registry ----------

export const listRegistries = () => api<Registry[]>('/api/registries')
export const createRegistry = (r: Registry) => post<Registry>('/api/registries', r)
export const updateRegistry = (name: string, r: Registry) => put<Registry>(`/api/registries/${name}`, r)
export const deleteRegistry = (name: string) => del<{ deleted: string }>(`/api/registries/${name}`)

// ---------- host ----------

export const listHosts = () => api<Host[]>('/api/hosts')
export const createHost = (h: Host) => post<Host>('/api/hosts', h)
export const updateHost = (name: string, h: Host) => put<Host>(`/api/hosts/${name}`, h)
export const deleteHost = (name: string) => del<{ deleted: string }>(`/api/hosts/${name}`)

// ---------- notify ----------

export const listNotifies = () => api<NotifyCfg[]>('/api/notifies')
export const createNotify = (n: NotifyCfg) => post<NotifyCfg>('/api/notifies', n)
export const updateNotify = (name: string, n: NotifyCfg) => put<NotifyCfg>(`/api/notifies/${name}`, n)
export const deleteNotify = (name: string) => del(`/api/notifies/${name}`)

// ---------- cron ----------

export const listCrons = () => api<CronCfg[]>('/api/crons')
export const createCron = (c: CronCfg) => post<CronCfg>('/api/crons', c)
export const updateCron = (name: string, c: CronCfg) => put<CronCfg>(`/api/crons/${name}`, c)
export const deleteCron = (name: string) => del(`/api/crons/${name}`)
export const fireCron = (name: string) => post<WorkflowTask>(`/api/crons/${name}/fire`)
