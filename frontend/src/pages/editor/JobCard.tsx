import { useEffect, useState } from 'react'
import {
  Button, Card, Col, Dropdown, Input, InputNumber, Popconfirm, Radio, Row, Select, Space, Switch, Tag, Typography,
} from 'antd'
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons'
import StepForm from './StepForm'
import {
  addStep, moveStep, patchDeploy, patchFreestyle, patchJob, patchStep, removeStep,
} from './tree'
import type { Env, ErrorPolicy, Job, JobType, StepType, Workflow } from '../../types'
import { listEnvs } from '../../api/resources'

const { Text } = Typography

const STEP_ADD_OPTIONS: { value: StepType; label: string }[] = [
  { value: 'shell', label: '+ shell' },
  { value: 'git', label: '+ git' },
  { value: 'docker_build', label: '+ 镜像构建' },
  { value: 'junit_report', label: '+ 测试报告' },
  { value: 'archive', label: '+ 归档' },
]

interface Props {
  wf: Workflow
  si: number
  ji: number
  onChange: (next: Workflow) => void
  onRemove: () => void
}

// job 卡片：freestyle（镜像三档 + 步骤列表 + 重试/超时/错误策略）或 deploy（靶标表单）
export default function JobCard({ wf, si, ji, onChange, onRemove }: Props) {
  const job: Job = wf.stages[si].jobs[ji]
  const [envs, setEnvs] = useState<Env[]>([])

  // deploy 靶标需要 env 名单（编辑器打开时拉一次即可，env 不是热数据）
  useEffect(() => {
    if (job.type === 'deploy') void listEnvs().then(setEnvs).catch(() => setEnvs([]))
  }, [job.type])

  const spec = job.spec ?? { steps: [] }
  const steps = spec.steps ?? []

  return (
    <Card
      size="small"
      title={
        <Space size={10} wrap>
          <Input
            size="small" style={{ width: 160 }} placeholder="job 名" value={job.name}
            onChange={(e) => onChange(patchJob(wf, si, ji, { name: e.target.value }))}
          />
          <Radio.Group
            size="small" value={job.type ?? 'freestyle'}
            onChange={(e) => {
              const t = e.target.value as JobType
              // 切换类型时重置对应 spec（旧 spec 残留会让后端校验迷惑）
              onChange(patchJob(wf, si, ji,
                t === 'freestyle'
                  ? { type: t, spec: { steps: [] }, deploy_spec: undefined }
                  : { type: t, spec: undefined, deploy_spec: { env: '', deployment_name: '', container_name: '', image: '' } }))
            }}
          >
            <Radio.Button value="freestyle">freestyle</Radio.Button>
            <Radio.Button value="deploy">deploy</Radio.Button>
          </Radio.Group>
          {job.type === 'deploy'
            ? <Tag color="purple">K8s 直 patch</Tag>
            : <Tag color="cyan">K8s Job</Tag>}
        </Space>
      }
      extra={
        <Popconfirm title={`删除 job ${job.name}？`} onConfirm={onRemove}>
          <Button size="small" danger icon={<DeleteOutlined />} />
        </Popconfirm>
      }
    >
      {(job.type ?? 'freestyle') === 'freestyle' ? (
        <>
          <Row gutter={10} style={{ marginBottom: 12 }} align="middle">
            <Col>
              <Text type="secondary">执行镜像</Text>{' '}
              <Select
                size="small" style={{ width: 280 }}
                value={spec.image ? 'custom' : 'auto'}
                onChange={(v) => onChange(patchFreestyle(wf, si, ji, { image: v === 'auto' ? '' : 'busybox:latest' }))}
                options={[
                  { value: 'auto', label: '自动（git/构建/归档→runner，纯 shell→busybox）' },
                  { value: 'custom', label: '自定义镜像…' },
                ]}
              />
            </Col>
            {spec.image && (
              <Col>
                <Input
                  size="small" style={{ width: 260 }} value={spec.image} placeholder="镜像全名，如 alpine:3.20"
                  onChange={(e) => onChange(patchFreestyle(wf, si, ji, { image: e.target.value }))}
                />
              </Col>
            )}
            <Col>
              <Text type="secondary">重试</Text>{' '}
              <InputNumber size="small" min={0} max={5} value={spec.retry ?? 0}
                onChange={(v) => onChange(patchFreestyle(wf, si, ji, { retry: v ?? 0 }))} />
            </Col>
            <Col>
              <Text type="secondary">超时(分)</Text>{' '}
              <InputNumber size="small" min={1} max={120} value={spec.timeout_minutes ?? 10}
                onChange={(v) => onChange(patchFreestyle(wf, si, ji, { timeout_minutes: v ?? 10 }))} />
            </Col>
            <Col>
              <Text type="secondary">错误策略</Text>{' '}
              <Select size="small" style={{ width: 110 }} value={spec.error_policy ?? 'stop'}
                onChange={(v) => onChange(patchFreestyle(wf, si, ji, { error_policy: v as ErrorPolicy }))}
                options={[
                  { value: 'stop', label: 'stop 刹车' },
                  { value: 'retry', label: 'retry 重试' },
                  { value: 'ignore', label: 'ignore 记红不刹' },
                ]} />
            </Col>
          </Row>

          {steps.map((step, pi) => (
            <StepForm
              key={`${si}-${ji}-${pi}-${step.name}`}
              step={step} index={pi} total={steps.length}
              onChange={(patch) => onChange(patchStep(wf, si, ji, pi, patch))}
              onRemove={() => onChange(removeStep(wf, si, ji, pi))}
              onMove={(to) => onChange(moveStep(wf, si, ji, pi, to))}
            />
          ))}

          <Dropdown
            menu={{
              items: STEP_ADD_OPTIONS.map((o) => ({ key: o.value, label: o.label })),
              onClick: ({ key }) => onChange(addStep(wf, si, ji, key as StepType)),
            }}
          >
            <Button size="small" icon={<PlusOutlined />}>添加步骤</Button>
          </Dropdown>
        </>
      ) : (
        <Row gutter={10}>
          <Col span={5}>
            <Text type="secondary">环境</Text>
            <Select
              style={{ width: '100%' }} placeholder="选 env" value={job.deploy_spec?.env || undefined}
              options={envs.map((e) => ({ value: e.name, label: `${e.name} → ${e.namespace}` }))}
              onChange={(v) => onChange(patchDeploy(wf, si, ji, { env: v }))}
              notFoundContent="先到「环境」页建 env"
            />
          </Col>
          <Col span={5}>
            <Text type="secondary">Deployment</Text>
            <Input placeholder="my-app" value={job.deploy_spec?.deployment_name}
              onChange={(e) => onChange(patchDeploy(wf, si, ji, { deployment_name: e.target.value }))} />
          </Col>
          <Col span={5}>
            <Text type="secondary">容器名</Text>
            <Input placeholder="main" value={job.deploy_spec?.container_name}
              onChange={(e) => onChange(patchDeploy(wf, si, ji, { container_name: e.target.value }))} />
          </Col>
          <Col span={6}>
            <Text type="secondary">镜像（可插变量）</Text>
            <Input placeholder='{{.build.IMG}} 或字面量' value={job.deploy_spec?.image}
              onChange={(e) => onChange(patchDeploy(wf, si, ji, { image: e.target.value }))} />
          </Col>
          <Col span={3}>
            <Text type="secondary">等 rollout</Text>
            <div><Switch checked={job.deploy_spec?.wait_rollout ?? true}
              onChange={(v) => onChange(patchDeploy(wf, si, ji, { wait_rollout: v }))} /></div>
          </Col>
        </Row>
      )}
    </Card>
  )
}
