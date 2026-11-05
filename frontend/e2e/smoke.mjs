// React 产物真执行冒烟 —— §9.19 纪律延续："字符串在位 ≠ 脚本活着"。
// 手法：vite 单 chunk 产物是自执行代码（无 import/export 语句），jsdom 手动 eval
// 即真跑——React 真挂载、antd 真渲染、fetch 真打引擎（node 原生 fetch → MP_BASE）。
// 对照已退役的 deploy/web/smoke.js（jsdom + 真引擎数据 + 9 断言）同款精神升级。
//
// 前提：先 npm run build（吃 dist/ 产物）。Usage: MP_BASE=https://… npm run smoke
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { JSDOM } from 'jsdom'

// 集群 ingress 是自签 TLS（curl -k 同款语义）；node fetch 默认严格校验会全挂
process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0'

const BASE = process.env.MP_BASE ?? 'https://pipeline.47.113.177.198.nip.io'

const distDir = join(fileURLToPath(new URL('../dist/', import.meta.url)))
const html = readFileSync(join(distDir, 'index.html'), 'utf8')
const assetsDir = join(distDir, 'assets')
const jsFile = readdirSync(assetsDir).find((f) => f.endsWith('.js'))
if (!jsFile) throw new Error('no bundle in dist/ — run `npm run build` first')
const bundle = readFileSync(join(assetsDir, jsFile), 'utf8')

// 引擎数据走 node 原生 fetch（jsdom 不带 fetch；bundle 里的相对路径在此拼接 BASE）
const realFetch = globalThis.fetch
const proxyFetch = (u, opt) => realFetch(typeof u === 'string' && u.startsWith('/') ? BASE + u : u, opt)

const dom = new JSDOM(html, { url: BASE + '/', pretendToBeVisual: true })
const { window } = dom
// antd/react 需要而 jsdom 缺的 API（不 stub 会静默崩在 mount 前）
window.matchMedia ??= (q) => ({
  matches: false, media: q,
  addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {},
  dispatchEvent() { return false },
})
window.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} }
window.scrollTo = () => {}
window.fetch = proxyFetch
globalThis.window = window
globalThis.document = window.document
// bundle 在全局作用域裸引用 DOM 类（HTMLElement/MutationObserver/Event…），node 全局没有——
// 把 jsdom window 的全部自有属性一次性桥接过来（testing-library 同款手法；只读的跳过）
for (const key of Object.getOwnPropertyNames(window)) {
  if (key in globalThis) continue
  try {
    globalThis[key] = window[key]
  } catch {
    /* 某些 window 属性在 node 侧不可写，跳过即可 */
  }
}
// node22 的 globalThis.navigator 只读且 bundle 对它的引用走 node 自带实现即可
globalThis.fetch = proxyFetch
// React 18.3 对容器做 instanceof ShadowRoot 检测（jsdom 无 shadow DOM，给空类让检测通过）
globalThis.ShadowRoot ??= class ShadowRoot {}
// antd 懒加载占位用（jsdom/node 都没有）
globalThis.IntersectionObserver ??= class { observe() {} unobserve() {} disconnect() {} takeRecords() { return [] } }

let failures = 0
const ok = (name, cond) => {
  console.log((cond ? '  ✅ ' : '  ❌ ') + name)
  if (!cond) failures++
}

const text = () => window.document.body.textContent
// waitFor 返回 pred 的真值（元素/true/undefined）——命中时提前返回该值本身，
// 断言处直接用、元素查找处拿到的是节点而不是 true
const waitFor = async (pred, ms = 15000) => {
  const deadline = Date.now() + ms
  while (Date.now() < deadline) {
    const v = pred()
    if (v) return v
    await new Promise((r) => setTimeout(r, 300))
  }
  return pred()
}

// ---- 真执行 ----
new Function(bundle)()

// ---- 断言 ----
ok('root 已挂载（React 真渲染）', await waitFor(() => window.document.querySelector('#root')?.children.length > 0))

ok('侧边五导航在位', await waitFor(() =>
  ['工作流', '任务', '环境', '通知', '定时'].every((t) => text().includes(t))))

ok('工作流页拉到真引擎数据', await waitFor(() => {
  const body = text()
  return body.includes('阶段') && (body.includes('新建') || body.includes('demo'))
}))

// 路由真切换：点侧边"任务"，列表页表单出现（React Router + antd Menu 真事件链）
const taskMenu = await waitFor(() =>
  [...window.document.querySelectorAll('.ant-menu-item')].find((el) => el.textContent?.includes('任务')))
if (taskMenu) {
  taskMenu.click()
  ok('菜单点击真切换路由（任务页过滤/表格骨架出现）',
    await waitFor(() => text().includes('状态') || text().includes('选择工作流')))
} else {
  ok('菜单点击真切换路由（任务页过滤/表格骨架出现）', false)
}

// API 面真打通：smoke 自己再直查一次引擎，双保险（bundle fetch + 直查都走同一条链路）
const apiCheck = await realFetch(BASE + '/api/workflows').then((r) => r.ok).catch(() => false)
ok('引擎 API 可达（bundle 数据源同链路）', apiCheck)

console.log(failures === 0 ? '\nSMOKE PASS' : `\nSMOKE FAIL (${failures} failed)`)
if (failures > 0) {
  console.error('--- body dump (first 1500 chars) ---')
  console.error(window.document.body.innerHTML.slice(0, 1500))
}
process.exit(failures === 0 ? 0 : 1)
