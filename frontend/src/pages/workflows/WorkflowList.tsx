import { Button, Card, message, Popconfirm, Space, Table, Tag } from 'antd'
import { PlusOutlined, PlayCircleOutlined, EditOutlined } from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import { usePolling } from '../../hooks/usePolling'
import TimeCell from '../../components/TimeCell'
import { deleteWorkflow, listWorkflows, runWorkflow } from '../../api/resources'
import type { Workflow } from '../../types'

// 工作流列表 —— 入口页：跑一条 / 进编辑器 / 删图纸
// 图纸不是热数据，10s 慢轮询足够（活任务树才需要 2s）
export default function WorkflowList() {
  const nav = useNavigate()
  const { data, error, setData } = usePolling(listWorkflows, { interval: 10000 })
  const [msg, ctx] = message.useMessage()

  const onRun = async (name: string) => {
    try {
      const t = await runWorkflow(name)
      msg.success(`已入队 #${t.task_id}，跳转详情`)
      nav(`/tasks/${name}/${t.task_id}`)
    } catch (e) {
      msg.error((e as Error).message)
    }
  }

  const onDelete = async (name: string) => {
    try {
      await deleteWorkflow(name)
      msg.success(`已删除 ${name}（历史任务档案保留）`)
      setData(data?.filter((w) => w.name !== name))
    } catch (e) {
      msg.error((e as Error).message)
    }
  }

  return (
    <Card title="工作流" extra={<Button type="primary" icon={<PlusOutlined />} onClick={() => nav('/workflows/new')}>新建</Button>}>
      {ctx}
      {error && <p style={{ color: '#ef4444' }}>加载失败：{error}</p>}
      <Table<Workflow>
        rowKey="name"
        size="middle"
        loading={!data}
        dataSource={data ?? []}
        pagination={false}
        columns={[
          { title: '名称', dataIndex: 'name', render: (v, r) => <a onClick={() => nav(`/workflows/${r.name}/edit`)}>{v}</a> },
          { title: '阶段', render: (_, r) => r.stages?.length ?? 0 },
          {
            title: '闸门/触发', render: (_, r) => (
              <Space size={4}>
                {r.stages?.some((s) => s.manual_exec?.enabled) && <Tag color="orange">审批</Tag>}
                {(r.webhooks?.length ?? 0) > 0 && <Tag color="blue">webhook×{r.webhooks!.length}</Tag>}
                {Object.keys(r.global_context ?? {}).length > 0 && <Tag color="purple">变量</Tag>}
              </Space>
            ),
          },
          { title: '创建时间', dataIndex: 'created_at', render: (v) => <TimeCell value={v} /> },
          {
            title: '操作', width: 240, render: (_, r) => (
              <Space>
                <Button size="small" type="primary" icon={<PlayCircleOutlined />} onClick={() => onRun(r.name)}>运行</Button>
                <Button size="small" icon={<EditOutlined />} onClick={() => nav(`/workflows/${r.name}/edit`)}>编辑</Button>
                <Popconfirm title={`删除图纸 ${r.name}？`} onConfirm={() => onDelete(r.name)}>
                  <Button size="small" danger>删除</Button>
                </Popconfirm>
              </Space>
            ),
          },
        ]}
      />
    </Card>
  )
}
