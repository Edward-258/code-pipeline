import { Card, Result } from 'antd'

// 占位：定时 CRUD + 手动点火在 PR-D 落地
export default function CronList() {
  return (
    <Card>
      <Result status="info" title="定时触发建设中" subTitle="5 段 crontab + 手动 fire 点火" />
    </Card>
  )
}
