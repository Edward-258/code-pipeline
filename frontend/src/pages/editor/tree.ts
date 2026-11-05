// 编辑器的状态层：Workflow 四层树的不可变更新 + 空对象工厂。
// 只做数据操作，不做 UI——组件里 import 这些函数，不自己写展开运算符嵌套地狱。

import type {
  DeploySpec, FreestyleSpec, Job, JobType, ManualExec, Stage, StepSpec, StepType, Workflow,
} from '../../types'

// ---------- 工厂：新节点带各类型的合理默认值（省得用户从零填） ----------

export function emptyStep(i: number, type: StepType = 'shell'): StepSpec {
  const base = { name: `step-${i + 1}`, type }
  switch (type) {
    case 'shell': return { ...base, script: '' }
    case 'git': return { ...base, repo: '', branch: 'main' }
    case 'docker_build': return { ...base, image: '', tag: 'latest' }
    case 'junit_report': return { ...base, report_glob: '*.xml', pass_rate: 1 }
    case 'archive': return { ...base, artifact_glob: '' }
  }
}

export function emptyJob(i: number, type: JobType = 'freestyle'): Job {
  const job: Job = { name: `job-${i + 1}`, type }
  if (type === 'freestyle') job.spec = { steps: [] }
  else job.deploy_spec = { env: '', deployment_name: '', container_name: '', image: '' }
  return job
}

export function emptyStage(i: number): Stage {
  return { name: `stage-${i + 1}`, jobs: [emptyJob(0)] }
}

export function emptyWorkflow(): Workflow {
  return { name: '', stages: [emptyStage(0)] }
}

// ---------- 不可变更新：只克隆路径上的节点，兄弟引用保持原样 ----------

const replaceAt = <T,>(arr: T[], i: number, v: T): T[] =>
  arr.map((x, idx) => (idx === i ? v : x))

export function patchStage(wf: Workflow, si: number, patch: Partial<Stage>): Workflow {
  return { ...wf, stages: replaceAt(wf.stages, si, { ...wf.stages[si], ...patch }) }
}

export function patchJob(wf: Workflow, si: number, ji: number, patch: Partial<Job>): Workflow {
  const stage = wf.stages[si]
  return patchStage(wf, si, { jobs: replaceAt(stage.jobs, ji, { ...stage.jobs[ji], ...patch }) })
}

export function patchFreestyle(wf: Workflow, si: number, ji: number, patch: Partial<FreestyleSpec>): Workflow {
  const job = wf.stages[si].jobs[ji]
  return patchJob(wf, si, ji, { spec: { ...(job.spec ?? { steps: [] }), ...patch } })
}

export function patchDeploy(wf: Workflow, si: number, ji: number, patch: Partial<DeploySpec>): Workflow {
  const job = wf.stages[si].jobs[ji]
  return patchJob(wf, si, ji, {
    deploy_spec: { ...(job.deploy_spec ?? { env: '', deployment_name: '', container_name: '', image: '' }), ...patch },
  })
}

export function patchStep(wf: Workflow, si: number, ji: number, pi: number, patch: Partial<StepSpec>): Workflow {
  const spec = wf.stages[si].jobs[ji].spec ?? { steps: [] }
  const steps = spec.steps ?? []
  return patchFreestyle(wf, si, ji, { steps: replaceAt(steps, pi, { ...steps[pi], ...patch }) })
}

export function patchManualExec(wf: Workflow, si: number, me: ManualExec | undefined): Workflow {
  return patchStage(wf, si, { manual_exec: me })
}

// 列表操作：add/remove/move（move 供步骤/作业排序按钮用）
export function addStage(wf: Workflow): Workflow {
  return { ...wf, stages: [...wf.stages, emptyStage(wf.stages.length)] }
}

export function removeStage(wf: Workflow, si: number): Workflow {
  return { ...wf, stages: wf.stages.filter((_, i) => i !== si) }
}

export function addJob(wf: Workflow, si: number): Workflow {
  const stage = wf.stages[si]
  return patchStage(wf, si, { jobs: [...stage.jobs, emptyJob(stage.jobs.length)] })
}

export function removeJob(wf: Workflow, si: number, ji: number): Workflow {
  const stage = wf.stages[si]
  return patchStage(wf, si, { jobs: stage.jobs.filter((_, i) => i !== ji) })
}

export function addStep(wf: Workflow, si: number, ji: number, type: StepType): Workflow {
  const spec = wf.stages[si].jobs[ji].spec ?? { steps: [] }
  const steps = spec.steps ?? []
  return patchFreestyle(wf, si, ji, { steps: [...steps, emptyStep(steps.length, type)] })
}

export function removeStep(wf: Workflow, si: number, ji: number, pi: number): Workflow {
  const spec = wf.stages[si].jobs[ji].spec ?? { steps: [] }
  const steps = spec.steps ?? []
  return patchFreestyle(wf, si, ji, { steps: steps.filter((_, i) => i !== pi) })
}

export function moveItem<T>(arr: T[], from: number, to: number): T[] {
  if (to < 0 || to >= arr.length) return arr
  const next = [...arr]
  const [x] = next.splice(from, 1)
  next.splice(to, 0, x)
  return next
}

export function moveStep(wf: Workflow, si: number, ji: number, from: number, to: number): Workflow {
  const spec = wf.stages[si].jobs[ji].spec ?? { steps: [] }
  return patchFreestyle(wf, si, ji, { steps: moveItem(spec.steps ?? [], from, to) })
}

export function moveStage(wf: Workflow, from: number, to: number): Workflow {
  return { ...wf, stages: moveItem(wf.stages, from, to) }
}
