import { useEffect, useState } from 'react'
import {
  Alert, Button, Card, Input, message, Segmented, Space, Tag, Typography,
} from 'antd'
import { ArrowLeftOutlined, CheckOutlined, PlusOutlined } from '@ant-design/icons'
import { useNavigate, useParams } from 'react-router-dom'
import StageCard from './StageCard'
import { addStage, emptyWorkflow, moveStage, removeStage } from './tree'
import { createWorkflow, getWorkflow, updateWorkflow } from '../../api/resources'
import type { Workflow } from '../../types'

// 可视化多阶段编辑器 —— 编辑 Workflow{stages[].jobs[].spec.steps[]} 四层。
// 表单/JSON 双模：JSON 是逃生舱（老用户习惯 + 后端字段全暴露），
// 两模共享同一份 state（切 JSON 模式时序列化快照，应用时 parse 回写）。

const { Text } = Typography

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

  useEffect(() => {
    if (!name) return
    getWorkflow(name)
      .then(setWf)
      .catch((e) => msg.error('加载失败：' + (e as Error).message))
      .finally(() => setLoading(false))
  }, [name])

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

  // 前端预检：只拦最常见的坑（后端 validateWorkflow 是最终裁决）
  const precheck = (): string | undefined => {
    if (!wf.name.trim()) return '工作流名称必填'
    for (const [i, st] of wf.stages.entries()) {
      if (!st.name.trim()) return `stage[${i}] 缺名称`
      if (st.jobs.length === 0) return `stage[${i}] 至少一个 job`
      for (const [j, jb] of st.jobs.entries()) {
        if (!jb.name.trim()) return `stage[${i}].job[${j}] 缺名称`
        if ((jb.type ?? 'freestyle') === 'freestyle') {
          const steps = jb.spec?.steps ?? []
          if (steps.length === 0) return `job「${jb.name}」缺步骤（至少一个 shell/git/…）`
          for (const [p, sp] of steps.entries()) if (!sp.name.trim()) return `job「${jb.name}」step[${p}] 缺名称`
        } else if (jb.deploy_spec && (!jb.deploy_spec.env || !jb.deploy_spec.deployment_name || !jb.deploy_spec.container_name || !jb.deploy_spec.image)) {
          return `deploy job「${jb.name}」靶标四项（env/dep/容器/镜像）必填`
        }
      }
    }
    return undefined
  }

  const save = async () => {
    const problem = precheck()
    if (problem) {
      msg.error(problem)
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
          stage 之间串行，stage 内可并行；失败刹车：串行剩余跳过、后续 stage 全跳过
        </Text>
      </Space>

      {mode === 'form' ? (
        <>
          <div style={{ display: 'flex', gap: 12, overflowX: 'auto', paddingBottom: 12 }}>
            {wf.stages.map((_, si) => (
              <StageCard
                key={si}
                wf={wf} si={si} total={wf.stages.length}
                onChange={setWf}
                onRemove={() => setWf(removeStage(wf, si))}
                onMove={(to) => setWf(moveStage(wf, si, to))}
              />
            ))}
            <Button
              type="dashed" icon={<PlusOutlined />} style={{ width: 120, height: 'auto', minHeight: 120 }}
              onClick={() => setWf(addStage(wf))}
            >
              添加 stage
            </Button>
          </div>
        </>
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
    </Card>
  )
}
