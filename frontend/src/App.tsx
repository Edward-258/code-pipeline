import { Layout, Menu } from 'antd'
import {
  ApartmentOutlined, ClockCircleOutlined, EnvironmentOutlined,
  NotificationOutlined, PlayCircleOutlined,
} from '@ant-design/icons'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'

// 布局：侧边栏导航（区别于 Zadig 的顶部页签形态）+ 内容区 Outlet
const ITEMS = [
  { key: '/workflows', icon: <ApartmentOutlined />, label: '工作流' },
  { key: '/tasks', icon: <PlayCircleOutlined />, label: '任务' },
  { key: '/envs', icon: <EnvironmentOutlined />, label: '环境' },
  { key: '/notifies', icon: <NotificationOutlined />, label: '通知' },
  { key: '/crons', icon: <ClockCircleOutlined />, label: '定时' },
]

export default function App() {
  const nav = useNavigate()
  const loc = useLocation()
  // 一级路径高亮（/workflows/x/edit 仍亮"工作流"）
  const selected = '/' + (loc.pathname.split('/')[1] ?? 'workflows')

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Layout.Sider theme="light" width={200} style={{ borderRight: '1px solid #f0f0f0' }}>
        <div
          style={{
            color: '#14b8a6', fontWeight: 700, fontSize: 16,
            padding: '20px 24px', letterSpacing: 0.5,
          }}
        >
          mini-pipeline
        </div>
        <Menu
          mode="inline"
          theme="light"
          selectedKeys={[selected]}
          items={ITEMS}
          onClick={({ key }) => nav(key)}
        />
      </Layout.Sider>
      <Layout.Content style={{ padding: 24, overflow: 'auto' }}>
        <Outlet />
      </Layout.Content>
    </Layout>
  )
}
