import { Fragment, useEffect, useMemo, useState } from 'react'
import {
  Alert, Button, Card, Input, message, Segmented, Space, Tag, Tooltip, Typography,
} from 'antd'
import { ArrowLeftOutlined, CheckOutlined, PlusOutlined } from '@ant-design/icons'
import { useNavigate, useParams } from 'react-router-dom'
import JobDrawer from './JobDrawer'
import StageColumn from './StageColumn'
import { addStage, emptyWorkflow, insertStage, moveStage, removeJob, removeStage } from './tree'
import {
  createWorkflow, getWorkflow, listEnvs, listHosts, listRegistries, updateWorkflow,
} from '../../api/resources'
import type { Env, Host, Registry, Workflow } from '../../types'

// 可视化画布编辑器（火山引擎持续交付同款语义）：横轴 = stage 串行时间轴（从左到右），
// 纵轴 = stage 内 job 堆叠（并行=竖堆，串行=↓ 链）；job 压成条框、详情在右侧抽屉
// （JobDrawer，live-patch）；列间分界线的 ⊕ 插 stage，条框悬停 ⊕ 插 job。
// 表单/JSON 双模：JSON 是逃生舱（老用户习惯 + 后端字段全暴露），
// 两模共享同一份 state（切 JSON 模式时序列化快照，应用时 parse 回写）。

const { Text } = Typography

// 前端预检（后端 ValidateWorkflow 仍是最终裁决）：逐节点错误供画布画红，
// first 供保存 toast——优先级与旧 precheck 一致：名称 → stage → job → step
function collectErrors(wf: Workflow): {
  jobErr: Record<string, string>
  stageErr: Record<number, string>
  first?: string
} {
  const jobErr: Record<string, string> = {}
  const stageErr: Record<number, string> = {}
  let first: string | undefined
  const hit = (m: string) => { if (!first) first = m }
  if (!wf.name.trim()) hit('工作流名称必填')
  for (const [i, st] of wf.stages.entries()) {
    if (!st.name.trim()) {
      stageErr[i] = 'stage 缺名称'
      hit(`stage[${i}] 缺名称`)
    }
    if (st.jobs.length === 0) {
      stageErr[i] = stageErr[i] ? `${stageErr[i]}；至少一个 job` : '至少一个 job'
      hit(`stage[${i}] 至少一个 job`)
    }
    for (const [j, jb] of st.jobs.entries()) {
      if (!jb.name.trim()) {
        jobErr[`${i}-${j}`] = 'job 缺名称'
        hit(`stage[${i}].job[${j}] 缺名称`)
      }
      if ((jb.type ?? 'freestyle') === 'freestyle') {
        const steps = jb.spec?.steps ?? []
        if (steps.length === 0) {
          jobErr[`${i}-${j}`] = '缺步骤（至少一个 shell/git/…）'
          hit(`job「${jb.name}」缺步骤（至少一个 shell/git/…）`)
        }
        for (const [p, sp] of steps.entries()) {
          if (!sp.name.trim()) {
            jobErr[`${i}-${j}`] = `step#${p + 1} 缺名称`
            hit(`job「${jb.name}」step[${p}] 缺名称`)
            break
          }
        }
      } else if (jb.deploy_spec && (!jb.deploy_spec.env || !jb.deploy_spec.deployment_name || !jb.deploy_spec.container_name || !jb.deploy_spec.image)) {
        jobErr[`${i}-${j}`] = '靶标四项（env/dep/容器/镜像）必填'
        hit(`deploy job「${jb.name}」靶标四项（env/dep/容器/镜像）必填`)
      }
    }
  }
  return { jobErr, stageErr, first }
}

// 列间连接件：横线 + 居中 ⊕（分界线加号加 stage——条框上的加号才是加 job）
function StageConnector({ onAdd, title }: { onAdd: () => void; title: string }) {
  return (
    <div style={{ width: 48, flexShrink: 0, position: 'relative', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      <div style={{ position: 'absolute', left: 4, right: 4, top: '50%', height: 2, background: '#d9d9d9' }} />
      <Tooltip title={title}>
        <Button
          shape="circle" size="small" type="primary" ghost icon={<PlusOutlined />}
          onClick={onAdd} style={{ position: 'relative', zIndex: 1 }}
        />
      </Tooltip>
    </div>
  )
}

export default function WorkflowEditor() {
  const { name } = useParams<{ name: string }>()
  const nav = useNavigate()
  const [msg, ctx] = message.useMessage()
  const isNew = !name

  const [wf, setWf] = useState<Workflow>(emptyWorkflow())
  const [loading, setLoading] = useState(!!name)
  const [mode, setMode] = useState<'form' | 'json'>('form')
  const [jsonText, setJsonText] = useState('')
  const [jsonError, setJsonError] = useState<string>()
  const [editing, setEditing] = useState<{ si: number; ji: number }>()
  // 抽屉下拉数据源：env + registry + host 名单编辑器页拉一次（旧实现在每个 JobCard/StepForm 各拉一次）
  const [envs, setEnvs] = useState<Env[]>([])
  const [registries, setRegistries] = useState<Registry[]>([])
  const [hosts, setHosts] = useState<Host[]>([])
  const errs = useMemo(() => collectErrors(wf), [wf])

  useEffect(() => {
    if (!name) return
    getWorkflow(name)
      .then(setWf)
      .catch((e) => msg.error('加载失败：' + (e as Error).message))
      .finally(() => setLoading(false))
  }, [name])

  useEffect(() => {
    void listEnvs().then(setEnvs).catch(() => setEnvs([]))
    void listRegistries().then(setRegistries).catch(() => setRegistries([]))
    void listHosts().then(setHosts).catch(() => setHosts([]))
  }, [])

  // 进 JSON 模式时快照当前 state（编辑以"应用"按钮显式回写，防止半吊子 JSON 意外覆盖表单）
  const enterJson = (m: 'form' | 'json') => {
    if (m === 'json') setJsonText(JSON.stringify(wf, null, 2))
    setMode(m)
  }

  const applyJson = () => {
    try {
      const parsed = JSON.parse(jsonText) as Workflow
      setJsonError(undefined)
      setWf(parsed)
      msg.success('JSON 已应用回表单')
    } catch (e) {
      setJsonError((e as Error).message)
    }
  }

  const save = async () => {
    if (errs.first) {
      msg.error(errs.first)
      return
    }
    try {
      if (isNew) {
        await createWorkflow(wf)
        msg.success('已创建')
        nav(`/workflows/${wf.name}/edit`, { replace: true })
      } else {
        await updateWorkflow(name, wf)
        msg.success('已保存')
      }
    } catch (e) {
      msg.error((e as Error).message)
    }
  }

  if (loading) return <Card loading />

  return (
    <Card
      title={
        <Space size={12} wrap>
          <Button icon={<ArrowLeftOutlined />} onClick={() => nav('/workflows')} />
          {isNew ? '新建工作流' : <>编辑 <Text code>{name}</Text></>}
          <Tag color={isNew ? 'green' : 'blue'}>{isNew ? '新建' : '更新'}</Tag>
        </Space>
      }
      extra={
        <Space>
          <Segmented value={mode} onChange={(v) => enterJson(v as 'form' | 'json')} options={[{ label: '表单', value: 'form' }, { label: 'JSON', value: 'json' }]} />
          <Button type="primary" icon={<CheckOutlined />} onClick={save}>{isNew ? '创建' : '保存'}</Button>
        </Space>
      }
    >
      {ctx}

      <Space size={12} style={{ marginBottom: 16 }} wrap>
        <Input
          style={{ width: 240 }} addonBefore="名称" placeholder="如 demo-ci" value={wf.name}
          disabled={!isNew} // 图纸名即主键，改名=新建（PUT 的路径名语义）
          onChange={(e) => setWf({ ...wf, name: e.target.value })}
        />
        <Text type="secondary">
          横 = stage 串行（从左到右）；竖 = job 堆叠（并行同时跑 / 串行 ↓ 链）；点 job 条框在右侧抽屉编辑
        </Text>
      </Space>

      {mode === 'form' ? (
        <div style={{ display: 'flex', alignItems: 'stretch', overflowX: 'auto', paddingBottom: 12 }}>
          {wf.stages.map((_, si) => (
            <Fragment key={si}>
              {si > 0 && <StageConnector title="在此处插入 stage" onAdd={() => setWf(insertStage(wf, si))} />}
              <StageColumn
                wf={wf} si={si} total={wf.stages.length}
                jobErr={errs.jobErr} stageErr={errs.stageErr[si]}
                onChange={setWf}
                onRemove={() => setWf(removeStage(wf, si))}
                onMove={(to) => setWf(moveStage(wf, si, to))}
                onOpenJob={(oSi, oJi) => setEditing({ si: oSi, ji: oJi })}
              />
            </Fragment>
          ))}
          <StageConnector title="添加 stage" onAdd={() => setWf(addStage(wf))} />
        </div>
      ) : (
        <>
          {jsonError && <Alert type="error" message={jsonError} style={{ marginBottom: 8 }} />}
          <Input.TextArea
            rows={24} value={jsonText}
            onChange={(e) => setJsonText(e.target.value)}
            style={{ fontFamily: 'ui-monospace, monospace', fontSize: 12 }}
          />
          <Button style={{ marginTop: 8 }} onClick={applyJson}>应用 JSON 回表单</Button>
        </>
      )}

      {editing && wf.stages[editing.si]?.jobs[editing.ji] && (
        <JobDrawer
          wf={wf} si={editing.si} ji={editing.ji}
          envs={envs} registries={registries} hosts={hosts}
          onChange={setWf}
          onClose={() => setEditing(undefined)}
          onRemove={() => {
            if (!editing) return
            setWf(removeJob(wf, editing.si, editing.ji))
            setEditing(undefined)
          }}
        />
      )}
    </Card>
  )
}
