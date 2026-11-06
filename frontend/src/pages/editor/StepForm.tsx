import { useEffect, useState } from 'react'
import { Button, Col, Input, Row, Select, Slider, Typography, Upload } from 'antd'
import { DeleteOutlined, ImportOutlined, ArrowUpOutlined, ArrowDownOutlined } from '@ant-design/icons'
import type { Registry, StepSpec, StepType } from '../../types'
import { listRegistries } from '../../api/resources'

// 单个步骤的编辑表单 —— 5 种类型共享骨架，按 type 显隐专属字段（后端 StepSpec 就是扁平结构，
// 前端如实呈现：字段即协议，不搞每类型一个子 schema 的过度设计）

const { Text } = Typography

const STEP_TYPES: { value: StepType; label: string }[] = [
  { value: 'shell', label: 'shell 脚本' },
  { value: 'git', label: 'git 拉代码' },
  { value: 'docker_build', label: '镜像构建' },
  { value: 'junit_report', label: '测试报告' },
  { value: 'archive', label: '产物归档' },
]

interface Props {
  step: StepSpec
  index: number
  total: number
  onChange: (patch: Partial<StepSpec>) => void
  onRemove: () => void
  onMove: (to: number) => void
}

export default function StepForm({ step, index, total, onChange, onRemove, onMove }: Props) {
  // 仓库下拉数据源（docker_build 目标仓库）：挂载时拉一次，编辑器页不需轮询
  const [registries, setRegistries] = useState<Registry[]>([])
  useEffect(() => {
    listRegistries().then(setRegistries).catch(() => { /* 引擎不可达时下拉留内置项 */ })
  }, [])

  // 本地脚本导入：读文件内容填进 script 字段（纯前端 FileReader，不上传）
  const importScript = (file: File) => {
    const reader = new FileReader()
    reader.onload = () => onChange({ script: String(reader.result ?? '') })
    reader.readAsText(file)
    return false // 阻止 antd Upload 自行上传
  }

  return (
    <div style={{ border: '1px solid #e5e7eb', borderRadius: 8, padding: '10px 14px', marginBottom: 10 }}>
      <Row gutter={10} align="middle" style={{ marginBottom: 8 }}>
        <Col flex="180px">
          <Input
            placeholder="步骤名" value={step.name}
            onChange={(e) => onChange({ name: e.target.value })}
            addonBefore={`#${index + 1}`}
          />
        </Col>
        <Col flex="150px">
          <Select
            value={step.type} options={STEP_TYPES}
            onChange={(t) => onChange({ type: t })}
          />
        </Col>
        <Col flex="auto" style={{ textAlign: 'right' }}>
          <Button.Group size="small">
            <Button icon={<ArrowUpOutlined />} disabled={index === 0} onClick={() => onMove(index - 1)} />
            <Button icon={<ArrowDownOutlined />} disabled={index === total - 1} onClick={() => onMove(index + 1)} />
            <Button danger icon={<DeleteOutlined />} onClick={onRemove} />
          </Button.Group>
        </Col>
      </Row>

      {step.type === 'shell' && (
        <>
          <Input.TextArea
            rows={6} value={step.script} placeholder="#!/bin/sh&#10;echo hello"
            onChange={(e) => onChange({ script: e.target.value })}
            style={{ fontFamily: 'ui-monospace, monospace', fontSize: 12 }}
          />
          <div style={{ marginTop: 6 }}>
            <Upload accept=".sh,.txt,.py,.yaml,.yml,.json" showUploadList={false} beforeUpload={importScript}>
              <Button size="small" icon={<ImportOutlined />}>导入本地脚本</Button>
            </Upload>
            <Text type="secondary" style={{ marginLeft: 10, fontSize: 12 }}>
              文件内容原样读入脚本框，可继续手写修改
            </Text>
          </div>
        </>
      )}

      {step.type === 'git' && (
        <Row gutter={10}>
          <Col span={10}><Input addonBefore="仓库" placeholder="https://github.com/o/r.git" value={step.repo} onChange={(e) => onChange({ repo: e.target.value })} /></Col>
          <Col span={5}><Input addonBefore="分支" placeholder="main" value={step.branch} onChange={(e) => onChange({ branch: e.target.value })} /></Col>
          <Col span={5}><Input.Password addonBefore="令牌" placeholder="私有仓库用" value={step.token} onChange={(e) => onChange({ token: e.target.value })} /></Col>
          <Col span={4}><Input addonBefore="目录" placeholder="自动" value={step.context_dir} onChange={(e) => onChange({ context_dir: e.target.value })} /></Col>
        </Row>
      )}

      {step.type === 'docker_build' && (
        <Row gutter={10}>
          <Col span={6}>
            <Select
              style={{ width: '100%' }} value={step.registry ?? ''}
              onChange={(v) => onChange({ registry: v || undefined })}
              options={[
                { value: '', label: '内置集群 registry' },
                ...registries.map((r) => ({
                  value: r.name, label: r.name + (r.is_default ? '（默认）' : ''),
                })),
              ]}
            />
          </Col>
          <Col span={6}><Input addonBefore="镜像" placeholder="mini-demo / 相对仓库名" value={step.image} onChange={(e) => onChange({ image: e.target.value })} /></Col>
          <Col span={3}><Input addonBefore="tag" placeholder="latest" value={step.tag} onChange={(e) => onChange({ tag: e.target.value })} /></Col>
          <Col span={5}><Input addonBefore="Dockerfile" placeholder="Dockerfile" value={step.dockerfile} onChange={(e) => onChange({ dockerfile: e.target.value })} /></Col>
          <Col span={4}><Input addonBefore="上下文" placeholder="." value={step.context_dir} onChange={(e) => onChange({ context_dir: e.target.value })} /></Col>
        </Row>
      )}

      {step.type === 'junit_report' && (
        <Row gutter={10} align="middle">
          <Col span={8}><Input addonBefore="glob" placeholder="*.xml" value={step.report_glob} onChange={(e) => onChange({ report_glob: e.target.value })} /></Col>
          <Col span={12}>
            <Text type="secondary" style={{ fontSize: 12, marginRight: 8 }}>门禁通过率</Text>
            <Slider
              style={{ display: 'inline-block', width: '70%' }}
              min={0} max={1} step={0.05} value={step.pass_rate ?? 1}
              onChange={(v) => onChange({ pass_rate: v })}
              tooltip={{ formatter: (v) => `${Math.round((v ?? 1) * 100)}%` }}
            />
          </Col>
        </Row>
      )}

      {step.type === 'archive' && (
        <Row gutter={10}>
          <Col span={10}><Input addonBefore="产物" placeholder="dist/*.tgz" value={step.artifact_glob} onChange={(e) => onChange({ artifact_glob: e.target.value })} /></Col>
          <Col span={8}><Input addonBefore="名称" placeholder="默认 <步骤名>.tgz" value={step.artifact_name} onChange={(e) => onChange({ artifact_name: e.target.value })} /></Col>
          <Col span={6}><Text type="secondary" style={{ fontSize: 12 }}>tar 后上传引擎 GridFS，详情页可下载</Text></Col>
        </Row>
      )}
    </div>
  )
}
