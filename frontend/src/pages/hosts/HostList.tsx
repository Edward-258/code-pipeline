import { useState } from 'react'
import { Button, Card, Form, Input, InputNumber, message, Modal, Popconfirm, Space, Table, Tag } from 'antd'
import { EditOutlined, PlusOutlined } from '@ant-design/icons'
import { usePolling } from '../../hooks/usePolling'
import TimeCell from '../../components/TimeCell'
import { createHost, deleteHost, listHosts, updateHost } from '../../api/resources'
import type { Host } from '../../types'

// 远程主机管理 —— freestyle job 的 ssh 靶标（名字 + 登录用户 + 地址 + 私钥，2026-11-08）。
// 图纸 spec.hosts 按名字引用；引擎在 driver 编译期把私钥投影进 Pod（~/.ssh/config +
// IdentityFile），任务档案只存名字引用、永不落密钥。私钥明文存库姿态与 registry.password
// / mongo 密码同档（学习版知情接受）。

interface FormValues {
  name: string
  user_name: string
  ip: string
  port?: number
  private_key: string
  desc?: string
}

// keyHint —— 私钥指纹（表里不回显全文，给首行类型 + 长度）
function keyHint(k?: string) {
  if (!k) return <Tag color="red">未录</Tag>
  const first = k.trimStart().split('\n')[0].trim()
  return <Tag color="geekblue">{first.slice(0, 40)}…（{k.length}B）</Tag>
}

export default function HostList() {
  const [msg, ctx] = message.useMessage()
  const { data, setData } = usePolling(listHosts, { interval: 15000 })
  const [editing, setEditing] = useState<{ open: boolean; original?: Host }>({ open: false })
  const [form] = Form.useForm<FormValues>()

  const openCreate = () => {
    form.resetFields()
    setEditing({ open: true })
  }
  const openEdit = (h: Host) => {
    form.setFieldsValue({
      name: h.name, user_name: h.user_name, ip: h.ip,
      port: h.port, private_key: h.private_key, desc: h.desc,
    })
    setEditing({ open: true, original: h })
  }

  const submit = async (v: FormValues) => {
    const body = {
      name: v.name, user_name: v.user_name, ip: v.ip,
      port: v.port ?? 0, private_key: v.private_key, desc: v.desc,
    }
    try {
      if (editing.original) {
        await updateHost(editing.original.name, body)
        msg.success('已更新')
      } else {
        await createHost(body)
        msg.success('已创建')
      }
      setEditing({ open: false })
      setData(await listHosts())
    } catch (e) {
      msg.error((e as Error).message)
    }
  }

  const remove = async (name: string) => {
    try {
      await deleteHost(name)
      msg.success(`已删除 ${name}（引用它的图纸下次执行会报"resolve host 失败"点名）`)
      setData(data?.filter((h) => h.name !== name))
    } catch (e) {
      msg.error((e as Error).message)
    }
  }

  return (
    <Card
      title="远程主机"
      extra={<Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>录主机</Button>}
    >
      {ctx}
      <Table<Host>
        rowKey="name" size="middle" loading={!data} dataSource={data ?? []} pagination={false}
        columns={[
          {
            title: '名称', dataIndex: 'name', render: (v) => (
              <Tag color="teal">{v}</Tag>
            ),
          },
          {
            title: '登录目标', render: (_, h) => (
              <code>{h.user_name}@{h.ip}:{h.port}</code>
            ),
          },
          { title: '私钥', dataIndex: 'private_key', render: (v) => keyHint(v) },
          { title: '说明', dataIndex: 'desc', ellipsis: true },
          { title: '创建时间', dataIndex: 'created_at', width: 180, render: (v) => <TimeCell value={v} /> },
          {
            title: '操作', width: 160, render: (_, h) => (
              <Space>
                <Button size="small" icon={<EditOutlined />} onClick={() => openEdit(h)}>编辑</Button>
                <Popconfirm
                  title={`删除主机 ${h.name}？（引用它的图纸会跑不起来）`}
                  onConfirm={() => remove(h.name)}
                >
                  <Button size="small" danger>删除</Button>
                </Popconfirm>
              </Space>
            ),
          },
        ]}
      />

      <Modal
        title={editing.original ? `编辑主机 ${editing.original.name}` : '录主机'}
        open={editing.open}
        onCancel={() => setEditing({ open: false })}
        onOk={() => form.submit()}
        width={640}
        destroyOnClose
      >
        <Form form={form} layout="vertical" onFinish={submit}>
          <Form.Item
            name="name" label="名称"
            rules={[{ required: true }, { pattern: /^[a-z0-9][a-z0-9-]{0,62}$/, message: '小写字母数字中划线（主键，图纸用 hosts 字段引用它）' }]}
          >
            <Input disabled={!!editing.original} placeholder="build-node / prod-bastion" />
          </Form.Item>
          <Space size={12} style={{ width: '100%' }}>
            <Form.Item
              name="user_name" label="登录用户"
              rules={[{ required: true }]}
              style={{ width: 180 }}
            >
              <Input placeholder="root" />
            </Form.Item>
            <Form.Item
              name="ip" label="地址（IP 或主机名）"
              rules={[{ required: true }, { pattern: /^[A-Za-z0-9._@:-]+$/, message: '字母数字与 ._@:- （注入面白名单）' }]}
              style={{ flex: 1, minWidth: 220 }}
            >
              <Input placeholder="172.29.237.250 / node1.corp.local" />
            </Form.Item>
            <Form.Item name="port" label="端口" extra="默认 22" style={{ width: 110 }}>
              <InputNumber min={1} max={65535} placeholder="22" style={{ width: '100%' }} />
            </Form.Item>
          </Space>
          <Form.Item
            name="private_key" label="私钥（PEM 全文）"
            rules={[{ required: true, message: '私钥必填（没有凭证的主机记录什么都连不上）' }]}
            extra="OpenSSH / PEM 格式均可；保存后引擎会在 job 编译期投进 Pod（不落任务档案）"
          >
            <Input.TextArea
              rows={8}
              placeholder={'-----BEGIN OPENSSH PRIVATE KEY-----\n…\n-----END OPENSSH PRIVATE KEY-----'}
              style={{ fontFamily: 'ui-monospace, monospace', fontSize: 12 }}
            />
          </Form.Item>
          <Form.Item name="desc" label="说明"><Input /></Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
