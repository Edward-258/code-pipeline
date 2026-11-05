import { useState } from 'react'
import { Button, Card, Form, Input, message, Modal, Popconfirm, Select, Space, Switch, Table, Tag } from 'antd'
import { EditOutlined, PlusOutlined } from '@ant-design/icons'
import { usePolling } from '../../hooks/usePolling'
import TimeCell from '../../components/TimeCell'
import { createNotify, deleteNotify, listNotifies, updateNotify } from '../../api/resources'
import type { NotifyCfg } from '../../types'

// 通知配置 —— 任务终态 fire-and-forget IM 通知（飞书/钉钉，两家加签约定不同，引擎侧已封装）

interface FormValues {
  name: string
  type: 'feishu' | 'dingtalk'
  webhook_url: string
  secret?: string
  enabled: boolean
}

export default function NotifyList() {
  const [msg, ctx] = message.useMessage()
  const { data, setData } = usePolling(listNotifies, { interval: 15000 })
  const [editing, setEditing] = useState<{ open: boolean; original?: NotifyCfg }>({ open: false })
  const [form] = Form.useForm<FormValues>()

  const openCreate = () => {
    form.resetFields()
    form.setFieldsValue({ type: 'feishu', enabled: true })
    setEditing({ open: true })
  }
  const openEdit = (n: NotifyCfg) => {
    form.setFieldsValue({
      name: n.name, type: n.type, webhook_url: n.webhook_url,
      secret: n.secret, enabled: n.enabled ?? false,
    })
    setEditing({ open: true, original: n })
  }

  const submit = async (v: FormValues) => {
    try {
      if (editing.original) {
        await updateNotify(editing.original.name, { ...editing.original, ...v })
        msg.success('已更新')
      } else {
        await createNotify(v)
        msg.success('已创建')
      }
      setEditing({ open: false })
      setData(await listNotifies())
    } catch (e) {
      msg.error((e as Error).message)
    }
  }

  const remove = async (name: string) => {
    try {
      await deleteNotify(name)
      msg.success(`已删除 ${name}`)
      setData(data?.filter((n) => n.name !== name))
    } catch (e) {
      msg.error((e as Error).message)
    }
  }

  const toggle = async (n: NotifyCfg, enabled: boolean) => {
    try {
      await updateNotify(n.name, { ...n, enabled })
      setData((data ?? []).map((x) => (x.name === n.name ? { ...x, enabled } : x)))
    } catch (e) {
      msg.error((e as Error).message)
    }
  }

  return (
    <Card
      title="通知配置"
      extra={<Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>新建通知</Button>}
    >
      {ctx}
      <Table<NotifyCfg>
        rowKey="name" size="middle" loading={!data} dataSource={data ?? []} pagination={false}
        columns={[
          { title: '名称', dataIndex: 'name' },
          {
            title: '平台', dataIndex: 'type', width: 100,
            render: (v) => <Tag color={v === 'feishu' ? 'blue' : 'geekblue'}>{v === 'feishu' ? '飞书' : '钉钉'}</Tag>,
          },
          { title: 'Webhook', dataIndex: 'webhook_url', ellipsis: true, render: (v) => <code style={{ fontSize: 12 }}>{v}</code> },
          { title: '加签', width: 80, render: (_, r) => (r.secret ? <Tag color="purple">有</Tag> : <Tag>无</Tag>) },
          {
            title: '启用', dataIndex: 'enabled', width: 80,
            render: (v, r) => <Switch size="small" checked={v ?? false} onChange={(b) => toggle(r, b)} />,
          },
          { title: '更新时间', dataIndex: 'updated_at', width: 180, render: (v) => <TimeCell value={v} /> },
          {
            title: '操作', width: 160, render: (_, r) => (
              <Space>
                <Button size="small" icon={<EditOutlined />} onClick={() => openEdit(r)}>编辑</Button>
                <Popconfirm title={`删除通知配置 ${r.name}？`} onConfirm={() => remove(r.name)}>
                  <Button size="small" danger>删除</Button>
                </Popconfirm>
              </Space>
            ),
          },
        ]}
      />

      <Modal
        title={editing.original ? `编辑通知 ${editing.original.name}` : '新建通知'}
        open={editing.open}
        onCancel={() => setEditing({ open: false })}
        onOk={() => form.submit()}
        destroyOnClose
      >
        <Form form={form} layout="vertical" onFinish={submit}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input disabled={!!editing.original} placeholder="team-im-group" />
          </Form.Item>
          <Form.Item name="type" label="平台" rules={[{ required: true }]}>
            <Select options={[{ value: 'feishu', label: '飞书' }, { value: 'dingtalk', label: '钉钉' }]} />
          </Form.Item>
          <Form.Item name="webhook_url" label="Webhook URL" rules={[{ required: true }]}>
            <Input placeholder="https://open.feishu.cn/open-apis/bot/v2/hook/…" />
          </Form.Item>
          <Form.Item name="secret" label="加签密钥（可选）">
            <Input.Password placeholder="机器人安全设置里的加签 secret" />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
