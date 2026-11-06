import {
  Button, Col, Drawer, Dropdown, Input, InputNumber, Popconfirm, Radio, Row, Select, Space, Switch, Tag, Typography,
} from 'antd'
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons'
import StepForm from './StepForm'
import {
  addStep, moveStep, patchDeploy, patchFreestyle, patchJob, patchStep, removeStep,
} from './tree'
import type { Env, ErrorPolicy, Job, JobType, Registry, StepType, Workflow } from '../../types'

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
  envs: Env[] // 编辑器页拉一次下传（旧实现在每个 deploy JobCard 各拉）
  registries: Registry[]
  onChange: (next: Workflow) => void
  onClose: () => void
  onRemove: () => void
}

// job 详情抽屉（火山「编辑任务」同位）——画布条框的完整编辑面。
// live-patch：改动即时进 wf，关抽屉即保留（无确定/取消缓冲，误改走 JSON 模式兜底）。
export default function JobDrawer({ wf, si, ji, envs, registries, onChange, onClose, onRemove }: Props) {
  const job: Job = wf.stages[si].jobs[ji]
  const spec = job.spec ?? { steps: [] }
  const steps = spec.steps ?? []

  return (
    <Drawer
      open
      width={560}
      onClose={onClose}
      title={
        <Space size={10}>
          <Text strong>编辑任务</Text>
          <Text type="secondary" style={{ fontSize: 12 }}>stage: {wf.stages[si].name}</Text>
        </Space>
      }
      extra={
        <Popconfirm title={`删除 job ${job.name}？`} onConfirm={onRemove}>
          <Button danger icon={<DeleteOutlined />}>删除</Button>
        </Popconfirm>
      }
    >
      <Space direction="vertical" size={14} style={{ width: '100%' }}>
        <div>
          <Text type="secondary" style={{ fontSize: 12 }}>任务名</Text>
          <Input
            value={job.name} placeholder="job 名"
            onChange={(e) => onChange(patchJob(wf, si, ji, { name: e.target.value }))}
          />
        </div>

        <div>
          <Text type="secondary" style={{ fontSize: 12 }}>类型</Text>{' '}
          <Radio.Group
            value={job.type ?? 'freestyle'}
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
        </div>

        {(job.type ?? 'freestyle') === 'freestyle' ? (
          <>
            <div>
              <Text type="secondary" style={{ fontSize: 12 }}>执行镜像</Text>
              <Select
                style={{ width: '100%' }}
                value={spec.image ? 'custom' : 'auto'}
                onChange={(v) => onChange(patchFreestyle(wf, si, ji, { image: v === 'auto' ? '' : 'busybox:latest' }))}
                options={[
                  { value: 'auto', label: '自动（git/构建/归档→runner，纯 shell→busybox）' },
                  { value: 'custom', label: '自定义镜像…' },
                ]}
              />
            </div>
            {spec.image && (
              <Input
                value={spec.image} placeholder="镜像全名，如 alpine:3.20"
                onChange={(e) => onChange(patchFreestyle(wf, si, ji, { image: e.target.value }))}
              />
            )}
            <Row gutter={10}>
              <Col span={6}>
                <Text type="secondary" style={{ fontSize: 12 }}>重试</Text>
                <InputNumber style={{ width: '100%' }} min={0} max={5} value={spec.retry ?? 0}
                  onChange={(v) => onChange(patchFreestyle(wf, si, ji, { retry: v ?? 0 }))} />
              </Col>
              <Col span={6}>
                <Text type="secondary" style={{ fontSize: 12 }}>超时(分)</Text>
                <InputNumber style={{ width: '100%' }} min={1} max={120} value={spec.timeout_minutes ?? 10}
                  onChange={(v) => onChange(patchFreestyle(wf, si, ji, { timeout_minutes: v ?? 10 }))} />
              </Col>
              <Col span={12}>
                <Text type="secondary" style={{ fontSize: 12 }}>错误策略</Text>
                <Select style={{ width: '100%' }} value={spec.error_policy ?? 'stop'}
                  onChange={(v) => onChange(patchFreestyle(wf, si, ji, { error_policy: v as ErrorPolicy }))}
                  options={[
                    { value: 'stop', label: 'stop 刹车' },
                    { value: 'retry', label: 'retry 重试' },
                    { value: 'ignore', label: 'ignore 记红不刹' },
                  ]} />
              </Col>
            </Row>

            <div>
              <Text type="secondary" style={{ display: 'block', marginBottom: 6, fontSize: 12 }}>
                步骤（自上而下执行）
              </Text>
              {steps.map((step, pi) => (
                <StepForm
                  key={`${si}-${ji}-${pi}-${step.name}`}
                  step={step} index={pi} total={steps.length} registries={registries}
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
            </div>
          </>
        ) : (
          <Space direction="vertical" size={10} style={{ width: '100%' }}>
            <div>
              <Text type="secondary" style={{ fontSize: 12 }}>环境</Text>
              <Select
                showSearch style={{ width: '100%' }} placeholder="选 env"
                value={job.deploy_spec?.env || undefined}
                options={envs.map((e) => ({ value: e.name, label: `${e.name} → ${e.namespace}` }))}
                onChange={(v) => onChange(patchDeploy(wf, si, ji, { env: v }))}
                notFoundContent="先到「环境」页建 env"
              />
            </div>
            <div>
              <Text type="secondary" style={{ fontSize: 12 }}>Deployment</Text>
              <Input placeholder="my-app" value={job.deploy_spec?.deployment_name}
                onChange={(e) => onChange(patchDeploy(wf, si, ji, { deployment_name: e.target.value }))} />
            </div>
            <div>
              <Text type="secondary" style={{ fontSize: 12 }}>容器名</Text>
              <Input placeholder="main" value={job.deploy_spec?.container_name}
                onChange={(e) => onChange(patchDeploy(wf, si, ji, { container_name: e.target.value }))} />
            </div>
            <div>
              <Text type="secondary" style={{ fontSize: 12 }}>镜像（可插变量）</Text>
              <Input placeholder='{{.build.IMG}} 或字面量' value={job.deploy_spec?.image}
                onChange={(e) => onChange(patchDeploy(wf, si, ji, { image: e.target.value }))} />
            </div>
            <div>
              <Text type="secondary" style={{ fontSize: 12, marginRight: 8 }}>等 rollout</Text>
              <Switch checked={job.deploy_spec?.wait_rollout ?? true}
                onChange={(v) => onChange(patchDeploy(wf, si, ji, { wait_rollout: v }))} />
            </div>
          </Space>
        )}
      </Space>
    </Drawer>
  )
}
