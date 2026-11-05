import { useRef, useState } from 'react'
import {
  Button, Card, Collapse, Descriptions, Drawer, Form, Input, message, Modal, Space, Tag, Typography,
} from 'antd'
import {
  CaretRightOutlined, DownloadOutlined, FileTextOutlined, RedoOutlined,
  CopyOutlined, StopOutlined, CheckOutlined,
} from '@ant-design/icons'
import { useNavigate, useParams } from 'react-router-dom'
import { usePolling } from '../../hooks/usePolling'
import StatusTag from '../../components/StatusTag'
import TimeCell from '../../components/TimeCell'
import {
  approveTask, artifactUrl, cancelTask, cloneTask, getTask,
  releaseStage, retryTask, taskStepLog,
} from '../../api/resources'
import { isTerminal, type Status, type StageTask, type StepTask } from '../../types'

// 任务详情 —— 三级树（stage→job→step）+ 分步日志 + 全量操作。
// 语义全部平移自第一版前端的活体验证结论，不重新发明：
//   cancel 活任务 202 / 终态 409；approve 返回 satisfied；release 自动找第一个 pause stage

const { Text, Paragraph } = Typography

export default function TaskDetail() {
  const { wf, id } = useParams<{ wf: string; id: string }>()
  const nav = useNavigate()
  const [msg, ctx] = message.useMessage()
  const [logOpen, setLogOpen] = useState<{ title: string; text: string }>()
  const [logLoading, setLogLoading] = useState(false)
  const [approveOpen, setApproveOpen] = useState(false)

  // 终态停轮询：enabled 依赖的"最新状态"还没从返回值里来——fetcher 内顺手写 ref，
  // 下一拍渲染时 enabled 翻 false（多轮询一拍无害，少轮询会漏终态）
  const statusRef = useRef<Status | undefined>(undefined)
  const { data: task } = usePolling(
    () => getTask(wf!, id!).then((t) => {
      statusRef.current = t.status
      return t
    }),
    { interval: 2000, enabled: !!wf && !!id && !isTerminal(statusRef.current) },
  )
  const terminal = isTerminal(task?.status)
  const pauseStage = task?.stages?.find((s) => s.status === 'pause')

  const refresh = () => void getTask(wf!, id!).then(() => msg.info('已刷新，轮询下一拍生效'))

  const onAction = async (label: string, fn: () => Promise<unknown>) => {
    try {
      await fn()
      msg.success(`${label} 已受理`)
    } catch (e) {
      msg.error((e as Error).message)
    }
  }

  const openStepLog = async (stage: string, job: string, step: StepTask) => {
    setLogLoading(true)
    setLogOpen({ title: `${stage} / ${job} / ${step.name}`, text: '' })
    try {
      const text = await taskStepLog(wf!, id!, stage, job, step.name)
      setLogOpen({ title: `${stage} / ${job} / ${step.name}`, text: text || '(暂无输出)' })
    } catch (e) {
      setLogOpen({ title: `${stage} / ${job} / ${step.name}`, text: '加载失败：' + (e as Error).message })
    } finally {
      setLogLoading(false)
    }
  }

  const onApprove = async ({ approver, comment }: { approver: string; comment?: string }) => {
    try {
      const r = await approveTask(wf!, id!, { approver, comment })
      if (r.satisfied) {
        msg.success('审批已满足策略，闸门放行')
        setApproveOpen(false)
      } else {
        msg.warning(`已记录（策略 ${r.policy ?? 'any'} 尚未满足）`)
      }
    } catch (e) {
      msg.error((e as Error).message)
    }
  }

  if (!task) return <Card title={`任务 ${wf} #${id}`} loading />

  return (
    <Card
      title={<Space size={12}>任务 {wf} #{task.task_id} <StatusTag status={task.status} /></Space>}
      extra={
        <Space wrap>
          {!terminal && (
            <Button danger icon={<StopOutlined />}
              onClick={() => onAction('取消', () => cancelTask(wf!, id!))}>取消</Button>
          )}
          {terminal && (
            <Button icon={<RedoOutlined />}
              onClick={() => onAction('重试', () => retryTask(wf!, id!))}>重试</Button>
          )}
          <Button icon={<CopyOutlined />}
            onClick={() => onAction('克隆', async () => {
              const t = await cloneTask(wf!, id!)
              msg.success(`已克隆为 #${t.task_id}`)
              nav(`/tasks/${wf}/${t.task_id}`)
            })}>克隆</Button>
          {pauseStage && (
            <>
              <Button type="primary" icon={<CheckOutlined />} onClick={() => setApproveOpen(true)}>审批</Button>
              <Button icon={<CaretRightOutlined />}
                onClick={() => onAction('放行', () => releaseStage(wf!, id!, pauseStage.name))}>
                放行 {pauseStage.name}
              </Button>
            </>
          )}
        </Space>
      }
    >
      {ctx}
      <Descriptions size="small" column={4} style={{ marginBottom: 16 }}>
        <Descriptions.Item label="状态"><StatusTag status={task.status} /></Descriptions.Item>
        <Descriptions.Item label="触发者"><Text code>{task.creator}</Text></Descriptions.Item>
        <Descriptions.Item label="开始"><TimeCell value={task.start_time} /></Descriptions.Item>
        <Descriptions.Item label="结束"><TimeCell value={task.end_time} /></Descriptions.Item>
        {task.error && (
          <Descriptions.Item label="错误" span={4}><Text type="danger">{task.error}</Text></Descriptions.Item>
        )}
      </Descriptions>

      {terminal && <Button size="small" onClick={refresh}>手动刷新</Button>}

      <Collapse
        defaultActiveKey={task.stages?.map((s) => s.name)}
        items={(task.stages ?? []).map((st) => ({
          key: st.name,
          label: <StageHeader st={st} />,
          children: (
            <Collapse
              defaultActiveKey={st.jobs?.map((j) => j.name)}
              items={(st.jobs ?? []).map((jt) => ({
                key: jt.name,
                label: (
                  <Space size={10} wrap>
                    <Text strong>{jt.name}</Text>
                    <StatusTag status={jt.status} />
                    <Tag>{jt.type ?? 'freestyle'}</Tag>
                    {jt.retry && jt.retry > 0 && <Tag color="purple">重试{jt.retry}</Tag>}
                    {Object.entries(jt.outputs ?? {}).map(([k, v]) => (
                      <Tag key={k} color="cyan">{k}={v}</Tag>
                    ))}
                    {jt.error && <Text type="danger">{jt.error}</Text>}
                  </Space>
                ),
                children: (
                  <Collapse
                    items={(jt.steps ?? []).map((sp) => ({
                      key: sp.name,
                      label: <StepHeader st={st.name!} job={jt.name!} step={sp} wf={wf!} id={id!} onLog={openStepLog} />,
                      children: sp.log
                        ? <pre style={{ margin: 0, whiteSpace: 'pre-wrap', fontSize: 12 }}>{sp.log}</pre>
                        : <Text type="secondary">（终态收割后日志落在"日志"按钮 / 档案 log 字段）</Text>,
                    }))} />
                ),
              }))} />
          ),
        }))} />

      <Drawer
        open={!!logOpen} width={720} onClose={() => setLogOpen(undefined)}
        title={<Space><FileTextOutlined />{logOpen?.title}</Space>}
      >
        <Paragraph>
          <pre style={{
            background: '#0d1117', color: '#c9d1d9', padding: 12, borderRadius: 6,
            whiteSpace: 'pre-wrap', fontSize: 12, maxHeight: '75vh', overflow: 'auto',
          }}>
            {logLoading ? '加载中…' : logOpen?.text}
          </pre>
        </Paragraph>
      </Drawer>

      <Modal title="审批（默认 admin）" open={approveOpen} onCancel={() => setApproveOpen(false)} footer={null}>
        <Form layout="vertical" onFinish={onApprove}>
          <Form.Item name="approver" label="审批人" rules={[{ required: true, message: '必填' }]}>
            <Input placeholder="admin" />
          </Form.Item>
          <Form.Item name="comment" label="备注"><Input.TextArea rows={2} /></Form.Item>
          <Button type="primary" htmlType="submit" block>提交审批</Button>
        </Form>
      </Modal>
    </Card>
  )
}

function StageHeader({ st }: { st: StageTask }) {
  return (
    <Space size={10} wrap>
      <Text strong style={{ fontSize: 15 }}>{st.name}</Text>
      <StatusTag status={st.status} />
      {st.parallel && <Tag color="geekblue">并行</Tag>}
      {st.manual_exec?.enabled && (
        <Tag color={st.manual_exec.executed ? 'green' : 'orange'}>
          闸门{st.manual_exec.executed ? '已放行' : `待审批 · ${st.manual_exec.policy ?? 'any'}`}
        </Tag>
      )}
      {st.error && <Text type="danger">{st.error}</Text>}
    </Space>
  )
}

function StepHeader(props: {
  st: string; job: string; step: StepTask; wf: string; id: string
  onLog: (stage: string, job: string, step: StepTask) => void
}) {
  const { st, job, step, wf, id, onLog } = props
  return (
    <Space size={10} wrap>
      <Text>{step.name}</Text>
      <StatusTag status={step.status} />
      <Tag>{step.type}</Tag>
      {step.test_result && (
        <Tag color={(step.test_result.failures ?? 0) + (step.test_result.errors ?? 0) > 0 ? 'red' : 'green'}>
          {step.test_result.tests ?? 0} 用例 · 挂 {(step.test_result.failures ?? 0) + (step.test_result.errors ?? 0)}
        </Tag>
      )}
      {step.error && <Text type="danger">{step.error}</Text>}
      <Space size={4}>
        <Button size="small" type="link" icon={<FileTextOutlined />} onClick={() => onLog(st, job, step)}>日志</Button>
        {step.type === 'archive' && (
          <a href={artifactUrl(wf, id, step.name)} download>
            <Button size="small" type="link" icon={<DownloadOutlined />}>产物</Button>
          </a>
        )}
      </Space>
    </Space>
  )
}
