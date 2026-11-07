import { useState } from 'react'
import { Button, Popconfirm, Tag, Tooltip, Typography } from 'antd'
import { DeleteOutlined, ExclamationCircleFilled, PlusOutlined } from '@ant-design/icons'
import type { Job } from '../../types'

const { Text } = Typography

interface Props {
  job: Job
  err?: string // collectErrors 的逐 job 错误（有值 = 火山式红标）
  onOpen: () => void
  onRemove: () => void
  onInsertBefore: () => void
  onInsertAfter: () => void
}

// job 条框：画布上的紧凑节点（列内竖向堆叠）——详情全在右侧抽屉（JobDrawer），
// 条框只做「是什么 + 有没有错」速览。悬停浮出左右 ⊕（插入 job）和删除。
export default function JobBar({ job, err, onOpen, onRemove, onInsertBefore, onInsertAfter }: Props) {
  const [hover, setHover] = useState(false)
  const freestyle = (job.type ?? 'freestyle') === 'freestyle'
  const spec = job.spec ?? { steps: [] }
  const steps = spec.steps ?? []

  const summary = freestyle
    ? [
        `${steps.length} 步骤`,
        (spec.hosts?.length ?? 0) > 0 ? `主机×${spec.hosts!.length}` : '',
        (spec.retry ?? 0) > 0 ? `重试${spec.retry}` : '',
        `超时${spec.timeout_minutes ?? 10}分`,
        spec.error_policy && spec.error_policy !== 'stop' ? spec.error_policy : '',
      ].filter(Boolean).join(' · ')
    : `→ ${job.deploy_spec?.env || '未选 env'}`

  return (
    <div
      style={{ position: 'relative' }}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
    >
      {hover && (
        <>
          <Button
            shape="circle" size="small" type="primary" ghost icon={<PlusOutlined />} title="在前面插入 job"
            onClick={(e) => { e.stopPropagation(); onInsertBefore() }}
            style={{ position: 'absolute', left: -11, top: '50%', transform: 'translateY(-50%)', zIndex: 2 }}
          />
          <Button
            shape="circle" size="small" type="primary" ghost icon={<PlusOutlined />} title="在后面插入 job"
            onClick={(e) => { e.stopPropagation(); onInsertAfter() }}
            style={{ position: 'absolute', right: -11, top: '50%', transform: 'translateY(-50%)', zIndex: 2 }}
          />
        </>
      )}
      <div
        onClick={onOpen}
        style={{
          border: err ? '1px solid #ff4d4f' : '1px solid #d9d9d9',
          background: err ? '#fff1f0' : '#fff',
          borderRadius: 8, padding: '7px 10px', cursor: 'pointer',
          boxShadow: hover ? '0 1px 4px rgba(0,0,0,0.12)' : 'none',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
          <Text strong ellipsis style={{ flex: 1 }}>{job.name || '(未命名 job)'}</Text>
          {freestyle
            ? <Tag color="cyan" style={{ marginRight: 0 }}>K8s Job</Tag>
            : <Tag color="purple" style={{ marginRight: 0 }}>K8s 直 patch</Tag>}
          {err && <Tooltip title={err}><ExclamationCircleFilled style={{ color: '#ff4d4f' }} /></Tooltip>}
          {hover && (
            <Popconfirm title={`删除 job ${job.name}？`} onConfirm={onRemove}>
              <Button size="small" type="text" danger icon={<DeleteOutlined />} onClick={(e) => e.stopPropagation()} />
            </Popconfirm>
          )}
        </div>
        <Text type="secondary" style={{ fontSize: 12 }}>{summary}</Text>
      </div>
    </div>
  )
}
