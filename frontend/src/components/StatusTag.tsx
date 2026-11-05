import { Tag } from 'antd'
import type { Status } from '../types'

// 8 态徽章 —— 颜色语义与 WorstOf 权重系（cancelled>failed>pause>running>passed>created>skipped）对齐
const MAP: Record<Status, { color: string; label: string }> = {
  passed: { color: 'green', label: '通过' },
  failed: { color: 'red', label: '失败' },
  running: { color: 'processing', label: '运行中' },
  queued: { color: 'cyan', label: '排队' },
  created: { color: 'default', label: '创建' },
  pause: { color: 'orange', label: '暂停' },
  cancelled: { color: 'default', label: '已取消' },
  skipped: { color: 'default', label: '跳过' },
}

export default function StatusTag({ status }: { status?: Status }) {
  if (!status) return <Tag>未知</Tag>
  const m = MAP[status] ?? { color: 'default', label: status }
  return <Tag color={m.color}>{m.label}</Tag>
}
