import { useState } from 'react'
import { Button, Card, Form, Input, message, Modal, Popconfirm, Select, Space, Switch, Table, Tooltip } from 'antd'
import { EditOutlined, FireOutlined, PlusOutlined } from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import { usePolling } from '../../hooks/usePolling'
import TimeCell from '../../components/TimeCell'
import {
  createCron, deleteCron, fireCron, listCrons, listWorkflows, updateCron,
} from '../../api/resources'
import type { CronCfg } from '../../types'

// 定时触发 —— 5 段 crontab（分 时 日 月 周）；fire 手动点火（验收免等钟点）

interface FormValues {
  name: string
  workflow_name: string
  crontab: string
  enabled: boolean
}

export default function CronList() {
  const [msg, ctx] = message.useMessage()
  const nav = useNavigate()
  const { data, setData } = usePolling(listCrons, { interval: 15000 })
  const { data: workflows } = usePolling(listWorkflows, { interval: 60000 })
  const [editing, setEditing] = useState<{ open: boolean; original?: CronCfg }>({ open: false })
  const [form] = Form.useForm<FormValues>()

  const openCreate = () => {
    form.resetFields()
    form.setFieldsValue({ workflow_name: workflows?.[0]?.name, crontab: '*/5 * * * *', enabled: true })
    setEditing({ open: true })
  }
  const openEdit = (c: CronCfg) => {
    form.setFieldsValue({
      name: c.name, workflow_name: c.workflow_name,
      crontab: c.crontab, enabled: c.enabled,
    })
    setEditing({ open: true, original: c })
  }

  const submit = async (v: FormValues) => {
    try {
      if (editing.original) {
        await updateCron(editing.original.name, { ...editing.original, ...v })
        msg.success('已更新')
      } else {
        await createCron(v)
        msg.success('已创建')
      }
      setEditing({ open: false })
      setData(await listCrons())
    } catch (e) {
      msg.error((e as Error).message)
    }
  }

  const remove = async (name: string) => {
    try {
      await deleteCron(name)
      msg.success(`已删除 ${name}`)
      setData(data?.filter((c) => c.name !== name))
    } catch (e) {
      msg.error((e as Error).message)
    }
  }

  const fire = async (c: CronCfg) => {
    try {
      const t = await fireCron(c.name)
      msg.success(`已点火 #${t.task_id}`)
      nav(`/tasks/${t.workflow_name}/${t.task_id}`)
    } catch (e) {
      msg.error((e as Error).message)
    }
  }

  const toggle = async (c: CronCfg, enabled: boolean) => {
    try {
      await updateCron(c.name, { ...c, enabled })
      setData((data ?? []).map((x) => (x.name === c.name ? { ...x, enabled } : x)))
    } catch (e) {
      msg.error((e as Error).message)
    }
  }

  return (
    <Card
      title="定时触发"
      extra={<Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>新建定时</Button>}
    >
      {ctx}
      <Table<CronCfg>
        rowKey="name" size="middle" loading={!data} dataSource={data ?? []} pagination={false}
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: '工作流', dataIndex: 'workflow_name' },
          { title: 'Crontab', dataIndex: 'crontab', render: (v) => <code>{v}</code> },
          {
            title: '启用', dataIndex: 'enabled', width: 80,
            render: (v, r) => <Switch size="small" checked={v} onChange={(b) => toggle(r, b)} />,
          },
          { title: '更新时间', dataIndex: 'updated_at', width: 180, render: (v) => <TimeCell value={v} /> },
          {
            title: '操作', width: 220, render: (_, r) => (
              <Space>
                <Tooltip title="立即触发一次（验收免等钟点）">
                  <Button size="small" icon={<FireOutlined />} onClick={() => fire(r)}>点火</Button>
                </Tooltip>
                <Button size="small" icon={<EditOutlined />} onClick={() => openEdit(r)}>编辑</Button>
                <Popconfirm title={`删除定时 ${r.name}？`} onConfirm={() => remove(r.name)}>
                  <Button size="small" danger>删除</Button>
                </Popconfirm>
              </Space>
            ),
          },
        ]}
      />

      <Modal
        title={editing.original ? `编辑定时 ${editing.original.name}` : '新建定时'}
        open={editing.open}
        onCancel={() => setEditing({ open: false })}
        onOk={() => form.submit()}
        destroyOnClose
      >
        <Form form={form} layout="vertical" onFinish={submit}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input disabled={!!editing.original} placeholder="nightly-build" />
          </Form.Item>
          <Form.Item name="workflow_name" label="工作流" rules={[{ required: true }]}>
            <Select
              showSearch
              options={(workflows ?? []).map((w) => ({ value: w.name, label: w.name }))}
              placeholder="选择要触发的工作流"
            />
          </Form.Item>
          <Form.Item
            name="crontab" label="Crontab（分 时 日 月 周）"
            rules={[{ required: true }, { pattern: /^(\S+\s+){4}\S+$/, message: '5 段格式，如 */5 * * * *' }]}
          >
            <Input placeholder="*/5 * * * *" />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
