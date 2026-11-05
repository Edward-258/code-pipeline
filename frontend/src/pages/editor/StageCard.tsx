import { Button, Card, Input, Popconfirm, Select, Space, Switch, Typography } from 'antd'
import { ArrowLeftOutlined, ArrowRightOutlined, DeleteOutlined, PlusOutlined } from '@ant-design/icons'
import JobCard from './JobCard'
import { addJob, patchManualExec, patchStage, removeJob } from './tree'
import type { ApprovePolicy, Workflow } from '../../types'

const { Text } = Typography

interface Props {
  wf: Workflow
  si: number
  total: number
  onChange: (next: Workflow) => void
  onRemove: () => void
  onMove: (to: number) => void
}

// stage 卡片：横向管道流里的一节 —— 名称/串并行/审批闸门 + 内部 jobs
export default function StageCard({ wf, si, total, onChange, onRemove, onMove }: Props) {
  const stage = wf.stages[si]
  const me = stage.manual_exec

  return (
    <Card
      size="small" style={{ width: 460, flexShrink: 0 }}
      title={
        <Space size={8} wrap>
          <Input
            size="small" style={{ width: 130 }} value={stage.name} placeholder="stage 名"
            onChange={(e) => onChange(patchStage(wf, si, { name: e.target.value }))}
          />
          <Switch
            checkedChildren="并行" unCheckedChildren="串行" checked={stage.parallel}
            onChange={(v) => onChange(patchStage(wf, si, { parallel: v }))}
          />
        </Space>
      }
      extra={
        <Button.Group size="small">
          <Button icon={<ArrowLeftOutlined />} disabled={si === 0} onClick={() => onMove(si - 1)} />
          <Button icon={<ArrowRightOutlined />} disabled={si === total - 1} onClick={() => onMove(si + 1)} />
          <Popconfirm title={`删除 stage ${stage.name}？`} onConfirm={onRemove}>
            <Button danger icon={<DeleteOutlined />} />
          </Popconfirm>
        </Button.Group>
      }
    >
      <Space size={12} style={{ marginBottom: 10 }} wrap>
        <Text type="secondary">审批闸门</Text>
        <Switch
          checked={me?.enabled ?? false}
          onChange={(v) => onChange(patchManualExec(wf, si, v ? { enabled: true, policy: 'any' } : undefined))}
        />
        {me?.enabled && (
          <>
            <Select
              size="small" style={{ width: 100 }} value={me.policy ?? 'any'}
              onChange={(p) => onChange(patchManualExec(wf, si, { ...me, policy: p as ApprovePolicy }))}
              options={[
                { value: 'any', label: 'any 任一' },
                { value: 'all', label: 'all 全员' },
              ]}
            />
            <Select
              size="small" mode="tags" style={{ minWidth: 180 }} placeholder="审批人名单（空=一人即过）"
              value={me.approvers ?? []}
              onChange={(list) => onChange(patchManualExec(wf, si, { ...me, approvers: list }))}
            />
          </>
        )}
      </Space>

      {stage.jobs.map((_job, ji) => (
        <div key={ji} style={{ marginBottom: 10 }}>
          <JobCard
            wf={wf} si={si} ji={ji}
            onChange={onChange}
            onRemove={() => onChange(removeJob(wf, si, ji))}
          />
        </div>
      ))}

      <Button
        size="small" type="dashed" block icon={<PlusOutlined />}
        onClick={() => onChange(addJob(wf, si))}
      >
        添加 job
      </Button>
    </Card>
  )
}
