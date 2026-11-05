import type { ThemeConfig } from 'antd'
import { theme as antdTheme } from 'antd'

// 主题 —— 暗色基因延续自第一版前端，但主色自成一体：
// teal 系（#2aa198 系）区别于 Zadig 的科技蓝，这就是 mini-pipeline 自己的样子
export const theme: ThemeConfig = {
  algorithm: antdTheme.darkAlgorithm,
  token: {
    colorPrimary: '#14b8a6',
    colorInfo: '#14b8a6',
    borderRadius: 6,
    fontSize: 14,
  },
  components: {
    Layout: {
      siderBg: '#0d1117',
      headerBg: '#0d1117',
      bodyBg: '#10161d',
    },
    Card: { colorBgContainer: '#161d26' },
  },
}
