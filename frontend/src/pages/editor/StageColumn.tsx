import { useState } from 'react'
import { Button, Input, Popconfirm, Select, Space, Switch, Tag, Typography } from 'antd'
import {
  ArrowLeftOutlined, ArrowRightOutlined, DeleteOutlined, DownOutlined,
  PauseCircleOutlined, PlusOutlined,
} from '@ant-design/icons'
import JobBar from './JobBar'
import { addJob, insertJob, patchManualExec, patchStage, removeJob } from './tree'
import type { ApprovePolicy, Workflow } from '../../types'

const { Text } = Typography

interface Props {
  wf: Workflow
  si: number
  total: number
  jobErr: Record<string, string> // collectErrors 结果（key = "si-ji"）
  stageErr?: string
  onChange: (next: Workflow) => void
  onRemove: () => void
  onMove: (to: number) => void
  onOpenJob: (si: number, ji: number) => void
}

// stage 列：画布横轴上的一节（横=串行时间轴，竖=job 堆叠）。
// 并行 stage 条框间是短竖线，串行 stage 是 ↓ 链——火山同款语义，一眼读出执行模型。
// 列底常驻审批闸门条：所有 job 过完 → 撞闸 → 放行才到下一列。
export default function StageColumn({ wf, si, total, jobErr, stageErr, onChange, onRemove, onMove, onOpenJob }: Props) {
  const stage = wf.stages[si]
  const me = stage.manual_exec
  const [gateOpen, setGateOpen] = useState(false)

  return (
    <div
      style={{
        width: 300, flexShrink: 0, borderRadius: 8, padding: 10, background: '#fafafa',
        border: stageErr ? '1px solid #ff4d4f' : '1px solid #e0e0e0',
        display: 'flex', flexDirection: 'column',
      }}
    >
      {/* 列头：名称 + 移动/删除 */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 4 }}>
        <Input
          size="small" style={{ flex: 1 }} value={stage.name} placeholder="stage 名"
          onChange={(e) => onChange(patchStage(wf, si, { name: e.target.value }))}
        />
        <Button.Group size="small">
          <Button icon={<ArrowLeftOutlined />} disabled={si === 0} onClick={() => onMove(si - 1)} />
          <Button icon={<ArrowRightOutlined />} disabled={si === total - 1} onClick={() => onMove(si + 1)} />
          <Popconfirm title={`删除 stage ${stage.name}？`} onConfirm={onRemove}>
            <Button danger icon={<DeleteOutlined />} />
          </Popconfirm>
        </Button.Group>
      </div>
      <div style={{ marginBottom: 8 }}>
        <Switch
          size="small" checkedChildren="并行" unCheckedChildren="串行" checked={stage.parallel}
          onChange={(v) => onChange(patchStage(wf, si, { parallel: v }))}
        />
        <Text type={stageErr ? 'danger' : 'secondary'} style={{ fontSize: 12, marginLeft: 8 }}>
          {stageErr ?? (stage.parallel ? 'job 同时跑' : 'job 从上到下依序跑')}
        </Text>
      </div>

      {/* job 条框堆：并行=短竖线连接，串行=↓ 链 */}
      {stage.jobs.map((job, ji) => (
        <div key={ji}>
          {ji > 0 && (stage.parallel
            ? <div style={{ width: 2, height: 10, background: '#bfbfbf', margin: '3px auto', borderRadius: 1 }} />
            : <div style={{ textAlign: 'center', color: '#8c8c8c', margin: '-2px 0' }}><DownOutlined style={{ fontSize: 12 }} /></div>)}
          <JobBar
            job={job} err={jobErr[`${si}-${ji}`]}
            onOpen={() => onOpenJob(si, ji)}
            onRemove={() => onChange(removeJob(wf, si, ji))}
            onInsertBefore={() => onChange(insertJob(wf, si, ji))}
            onInsertAfter={() => onChange(insertJob(wf, si, ji + 1))}
          />
        </div>
      ))}

      <Button
        size="small" type="dashed" block icon={<PlusOutlined />} style={{ marginTop: 8 }}
        onClick={() => onChange(addJob(wf, si))}
      >
        添加 job
      </Button>

      {/* 审批闸门条：灰虚线=未开启（点击开启），橙虚线=已开启（点击展开编辑） */}
      {!me?.enabled ? (
        <Button
          size="small" type="dashed" block icon={<PlusOutlined />}
          style={{ marginTop: 8, color: '#8c8c8c' }}
          onClick={() => onChange(patchManualExec(wf, si, { enabled: true, policy: 'any' }))}
        >
          审批闸门
        </Button>
      ) : (
        <div
          style={{
            marginTop: 8, border: '1px dashed #fa8c16', background: '#fff7e6',
            borderRadius: 6, padding: '6px 10px', cursor: 'pointer',
          }}
          onClick={() => setGateOpen(!gateOpen)}
        >
          <Space size={6} wrap style={{ width: '100%' }}>
            <PauseCircleOutlined style={{ color: '#fa8c16' }} />
            <Text strong style={{ fontSize: 12 }}>审批闸门</Text>
            <Tag color="orange" style={{ marginRight: 0 }}>{me.policy ?? 'any'}</Tag>
            <Text type="secondary" style={{ fontSize: 12 }}>
              {(me.approvers?.length ?? 0) > 0 ? me.approvers!.join('、') : '空名单 = 一人审批即过'}
            </Text>
            <Popconfirm
              title="移除审批闸门？"
              onConfirm={() => { setGateOpen(false); onChange(patchManualExec(wf, si, undefined)) }}
            >
              <Button size="small" type="text" danger icon={<DeleteOutlined />} onClick={(e) => e.stopPropagation()} />
            </Popconfirm>
          </Space>
          {gateOpen && (
            <Space size={6} wrap style={{ marginTop: 6 }} onClick={(e) => e.stopPropagation()}>
              <Select
                size="small" style={{ width: 110 }} value={me.policy ?? 'any'}
                onChange={(p) => onChange(patchManualExec(wf, si, { ...me, policy: p as ApprovePolicy }))}
                options={[
                  { value: 'any', label: 'any 任一' },
                  { value: 'all', label: 'all 全员' },
                ]}
              />
              <Select
                size="small" mode="tags" style={{ minWidth: 150 }} placeholder="审批人名单"
                value={me.approvers ?? []}
                onChange={(list) => onChange(patchManualExec(wf, si, { ...me, approvers: list }))}
              />
            </Space>
          )}
        </div>
      )}
    </div>
  )
}
