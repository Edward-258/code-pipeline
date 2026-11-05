import type { ThemeConfig } from 'antd'

// 主题 —— 白底清爽版（antd 默认 light 算法），主色自成一体：
// teal 系（#14b8a6）区别于 Zadig 的科技蓝，这就是 mini-pipeline 自己的样子。
// 浅灰画布（colorBgLayout）托白卡片，一层叩出内容层次。
export const theme: ThemeConfig = {
  token: {
    colorPrimary: '#14b8a6',
    colorInfo: '#14b8a6',
    borderRadius: 6,
    fontSize: 14,
    colorBgLayout: '#f5f7f8',
  },
}
