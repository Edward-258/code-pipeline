import { useState } from 'react'
import { Button, Card, Form, Input, message, Modal, Popconfirm, Space, Switch, Table, Tag } from 'antd'
import { EditOutlined, PlusOutlined, StarFilled } from '@ant-design/icons'
import { usePolling } from '../../hooks/usePolling'
import TimeCell from '../../components/TimeCell'
import { createRegistry, deleteRegistry, listRegistries, updateRegistry } from '../../api/resources'
import type { Registry } from '../../types'

// 镜像仓库管理 —— docker_build 的推送目标（地址+凭证+默认标记，2026-11-07）。
// is_default 是 Zadig 全量制的核心：设为默认后，所有【裸名】docker_build 的推送目标
// 静默切到这里（引擎烘焙凭证），没有默认记录时回落集群内置 registry——切换前必须知情。

interface FormValues {
  name: string
  address: string
  username?: string
  password?: string
  is_default?: boolean
  desc?: string
}

export default function RegistryList() {
  const [msg, ctx] = message.useMessage()
  const { data, setData } = usePolling(listRegistries, { interval: 15000 })
  const [editing, setEditing] = useState<{ open: boolean; original?: Registry }>({ open: false })
  const [form] = Form.useForm<FormValues>()

  const openCreate = () => {
    form.resetFields()
    setEditing({ open: true })
  }
  const openEdit = (r: Registry) => {
    form.setFieldsValue({
      name: r.name, address: r.address, username: r.username,
      password: r.password, is_default: r.is_default, desc: r.desc,
    })
    setEditing({ open: true, original: r })
  }

  const submit = async (v: FormValues) => {
    const body = {
      name: v.name, address: v.address, username: v.username,
      password: v.password, is_default: v.is_default ?? false, desc: v.desc,
    }
    try {
      if (editing.original) {
        await updateRegistry(editing.original.name, body)
        msg.success('已更新')
      } else {
        await createRegistry(body)
        msg.success('已创建')
      }
      if (body.is_default) {
        msg.warning('已设为默认仓库：所有裸名 docker_build 的推送目标已切换至此', 5)
      }
      setEditing({ open: false })
      setData(await listRegistries())
    } catch (e) {
      msg.error((e as Error).message)
    }
  }

  const remove = async (name: string) => {
    try {
      await deleteRegistry(name)
      msg.success(`已删除 ${name}（删到默认仓库时引擎自动选举首个剩余补位）`)
      setData(data?.filter((r) => r.name !== name))
    } catch (e) {
      msg.error((e as Error).message)
    }
  }

  return (
    <Card
      title="镜像仓库"
      extra={<Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>新建仓库</Button>}
    >
      {ctx}
      <Table<Registry>
        rowKey="name" size="middle" loading={!data} dataSource={data ?? []} pagination={false}
        columns={[
          {
            title: '名称', dataIndex: 'name', render: (v, r) => (
              <Space>
                <Tag color="teal">{v}</Tag>
                {r.is_default && <Tag icon={<StarFilled />} color="gold">默认</Tag>}
              </Space>
            ),
          },
          { title: '地址', dataIndex: 'address', render: (v) => <code>{v}</code> },
          { title: '用户名', dataIndex: 'username', render: (v) => v || <Tag>免认证</Tag> },
          { title: '说明', dataIndex: 'desc', ellipsis: true },
          { title: '创建时间', dataIndex: 'created_at', width: 180, render: (v) => <TimeCell value={v} /> },
          {
            title: '操作', width: 160, render: (_, r) => (
              <Space>
                <Button size="small" icon={<EditOutlined />} onClick={() => openEdit(r)}>编辑</Button>
                <Popconfirm
                  title={`删除仓库 ${r.name}？（引用它的 docker_build 图纸会推不出去）`}
                  onConfirm={() => remove(r.name)}
                >
                  <Button size="small" danger>删除</Button>
                </Popconfirm>
              </Space>
            ),
          },
        ]}
      />

      <Modal
        title={editing.original ? `编辑仓库 ${editing.original.name}` : '新建仓库'}
        open={editing.open}
        onCancel={() => setEditing({ open: false })}
        onOk={() => form.submit()}
        destroyOnClose
      >
        <Form form={form} layout="vertical" onFinish={submit}>
          <Form.Item
            name="name" label="名称"
            rules={[{ required: true }, { pattern: /^[a-z0-9][a-z0-9-]{0,62}$/, message: '小写字母数字中划线（主键，图纸用 registry 字段引用它）' }]}
          >
            <Input disabled={!!editing.original} placeholder="acr / harbor-prod / internal" />
          </Form.Item>
          <Form.Item
            name="address" label="地址"
            rules={[{ required: true }]}
            extra="host[:port][/namespace]，http(s):// 前缀会自动剥掉"
          >
            <Input placeholder="crpi-xxx.aliyuncs.com / registry.local:5000" />
          </Form.Item>
          <Form.Item name="username" label="用户名" extra="留空 = 免认证仓库（跳过 docker login）">
            <Input placeholder="仓库访问账号" />
          </Form.Item>
          <Form.Item name="password" label="密码">
            <Input.Password placeholder="学习版明文存库（与 mongo/ACR 同档姿态）" autoComplete="new-password" />
          </Form.Item>
          <Form.Item
            name="is_default" label="设为默认仓库" valuePropName="checked"
            extra="⚠️ 裸名（无仓库前缀）docker_build 将跟随此仓库推送；无默认记录时回落集群内置 registry"
          >
            <Switch />
          </Form.Item>
          <Form.Item name="desc" label="说明"><Input /></Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
