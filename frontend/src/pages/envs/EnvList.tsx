import { Card, Result } from 'antd'

// 占位：环境 CRUD（含编辑，打通 PUT /api/envs/:name）在 PR-D 落地
export default function EnvList() {
  return (
    <Card>
      <Result status="info" title="环境管理建设中" subTitle="env = 名字 → namespace 映射（deploy job 的靶标）" />
    </Card>
  )
}
