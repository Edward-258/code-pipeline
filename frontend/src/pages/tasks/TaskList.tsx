import { useState } from 'react'
import { Button, Card, Input, Select, Space, Table } from 'antd'
import { useNavigate } from 'react-router-dom'
import { usePolling } from '../../hooks/usePolling'
import StatusTag from '../../components/StatusTag'
import TimeCell from '../../components/TimeCell'
import { listTasks, listWorkflows } from '../../api/resources'
import type { Status, WorkflowTask } from '../../types'

// 任务列表 —— 过滤分页（消费 {items,total,page,size} envelope），5s 轮询
const STATUS_OPTIONS: Status[] = ['queued', 'running', 'passed', 'failed', 'cancelled', 'pause']
const SIZE = 20

export default function TaskList() {
  const nav = useNavigate()
  const [wf, setWf] = useState<string>()
  const [status, setStatus] = useState<string>()
  const [creator, setCreator] = useState('')
  const [page, setPage] = useState(1)

  const { data: workflows } = usePolling(listWorkflows, { interval: 30000 })
  const current = wf ?? workflows?.[0]?.name ?? ''
  const { data, error } = usePolling(
    () => (current ? listTasks(current, { status, creator, page, size: SIZE }) : Promise.resolve(undefined)),
    { interval: 5000, enabled: !!current },
  )

  const dur = (t: WorkflowTask) => {
    if (!t.start_time) return '-'
    const end = t.end_time ?? Math.floor(Date.now() / 1000)
    const s = end - t.start_time
    return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m${s % 60}s`
  }

  return (
    <Card title="任务">
      <Space style={{ marginBottom: 16 }} wrap>
        <Select
          style={{ width: 220 }} placeholder="选择工作流" showSearch
          value={current || undefined}
          options={(workflows ?? []).map((w) => ({ value: w.name, label: w.name }))}
          onChange={(v) => { setWf(v); setPage(1) }}
        />
        <Select
          style={{ width: 140 }} placeholder="状态" allowClear
          value={status} onChange={(v) => { setStatus(v); setPage(1) }}
          options={STATUS_OPTIONS.map((s) => ({ value: s, label: s }))}
        />
        <Input.Search
          style={{ width: 200 }} placeholder="creator 过滤" allowClear
          onSearch={(v) => { setCreator(v); setPage(1) }}
        />
      </Space>
      {error && <p style={{ color: '#f87171' }}>加载失败：{error}</p>}
      <Table<WorkflowTask>
        rowKey="id"
        size="middle"
        loading={!data && !!current}
        dataSource={data?.items ?? []}
        pagination={{
          current: data?.page ?? page, pageSize: SIZE, total: data?.total ?? 0,
          onChange: setPage, showTotal: (t) => `共 ${t} 条`,
        }}
        columns={[
          { title: '#', dataIndex: 'task_id', width: 70, render: (v, r) => <a onClick={() => nav(`/tasks/${r.workflow_name}/${r.task_id}`)}>#{v}</a> },
          { title: '工作流', dataIndex: 'workflow_name' },
          { title: '状态', dataIndex: 'status', width: 100, render: (s) => <StatusTag status={s} /> },
          { title: '触发者', dataIndex: 'creator', width: 160 },
          { title: '开始', dataIndex: 'start_time', width: 170, render: (v) => <TimeCell value={v} /> },
          { title: '耗时', width: 90, render: (_, r) => dur(r) },
          {
            title: '操作', width: 90, render: (_, r) => (
              <Button size="small" onClick={() => nav(`/tasks/${r.workflow_name}/${r.task_id}`)}>详情</Button>
            ),
          },
        ]}
      />
    </Card>
  )
}
