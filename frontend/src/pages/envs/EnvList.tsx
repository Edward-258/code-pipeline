import { useState } from 'react'
import { Button, Card, Form, Input, message, Modal, Popconfirm, Space, Table, Tag } from 'antd'
import { EditOutlined, PlusOutlined } from '@ant-design/icons'
import { usePolling } from '../../hooks/usePolling'
import TimeCell from '../../components/TimeCell'
import { createEnv, deleteEnv, listEnvs, updateEnv } from '../../api/resources'
import type { Env } from '../../types'

// 环境管理 —— env = 名字 → namespace 映射（deploy job 的靶标）。
// name 是主键不可改（编辑只改 namespace/desc，走 PR #33 的 PUT）

interface FormValues {
  name: string
  namespace: string
  desc?: string
}

export default function EnvList() {
  const [msg, ctx] = message.useMessage()
  const { data, setData } = usePolling(listEnvs, { interval: 15000 })
  const [editing, setEditing] = useState<{ open: boolean; original?: Env }>({ open: false })
  const [form] = Form.useForm<FormValues>()

  const openCreate = () => {
    form.resetFields()
    setEditing({ open: true })
  }
  const openEdit = (e: Env) => {
    form.setFieldsValue({ name: e.name, namespace: e.namespace, desc: e.desc })
    setEditing({ open: true, original: e })
  }

  const submit = async (v: FormValues) => {
    try {
      if (editing.original) {
        await updateEnv(editing.original.name, { ...editing.original, namespace: v.namespace, desc: v.desc })
        msg.success('已更新')
      } else {
        await createEnv({ name: v.name, namespace: v.namespace, desc: v.desc })
        msg.success('已创建')
      }
      setEditing({ open: false })
      setData(await listEnvs())
    } catch (e) {
      msg.error((e as Error).message)
    }
  }

  const remove = async (name: string) => {
    try {
      await deleteEnv(name)
      msg.success(`已删除 ${name}`)
      setData(data?.filter((e) => e.name !== name))
    } catch (e) {
      msg.error((e as Error).message)
    }
  }

  return (
    <Card
      title="环境"
      extra={<Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>新建环境</Button>}
    >
      {ctx}
      <Table<Env>
        rowKey="name" size="middle" loading={!data} dataSource={data ?? []} pagination={false}
        columns={[
          { title: '名称', dataIndex: 'name', render: (v) => <Tag color="teal">{v}</Tag> },
          { title: 'Namespace', dataIndex: 'namespace', render: (v) => <code>{v}</code> },
          { title: '说明', dataIndex: 'desc', ellipsis: true },
          { title: '创建时间', dataIndex: 'created_at', width: 180, render: (v) => <TimeCell value={v} /> },
          {
            title: '操作', width: 160, render: (_, r) => (
              <Space>
                <Button size="small" icon={<EditOutlined />} onClick={() => openEdit(r)}>编辑</Button>
                <Popconfirm title={`删除环境 ${r.name}？（引用它的 deploy 图纸会失去靶标）`} onConfirm={() => remove(r.name)}>
                  <Button size="small" danger>删除</Button>
                </Popconfirm>
              </Space>
            ),
          },
        ]}
      />

      <Modal
        title={editing.original ? `编辑环境 ${editing.original.name}` : '新建环境'}
        open={editing.open}
        onCancel={() => setEditing({ open: false })}
        onOk={() => form.submit()}
        destroyOnClose
      >
        <Form form={form} layout="vertical" onFinish={submit}>
          <Form.Item
            name="name" label="名称"
            rules={[{ required: true }, { pattern: /^[a-z0-9][a-z0-9-]{0,62}$/, message: '小写字母数字中划线（主键，建后不可改）' }]}
          >
            <Input disabled={!!editing.original} placeholder="dev / qa / prod" />
          </Form.Item>
          <Form.Item
            name="namespace" label="Namespace"
            // 随建语义（引擎侧 EnsureNamespace）：不存在现场造，Terminating 拒绝
            extra={editing.original ? '保存后同样会确保该 namespace 存在（不存在则自动创建）' : '保存后会同时在集群创建该 namespace'}
            rules={[{ required: true }, { pattern: /^[a-z0-9][a-z0-9-]{0,62}$/, message: '小写字母数字中划线' }]}
          >
            <Input placeholder="如 mvp-dev（不存在会自动创建）" />
          </Form.Item>
          <Form.Item name="desc" label="说明"><Input /></Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
