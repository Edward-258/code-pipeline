import { useEffect, useRef, useState } from 'react'

// 轮询 hook —— 语义搬自旧前端 auto()：挂载立即拉一次，之后按间隔刷新，卸载/失能即停。
// 用 setTimeout 链而非 setInterval：慢请求不会重叠堆积（2s 间隔 < 请求耗时时的护栏）。
export function usePolling<T>(
  fetcher: () => Promise<T>,
  opts: { interval?: number; enabled?: boolean } = {},
) {
  const { interval = 2000, enabled = true } = opts
  const [data, setData] = useState<T>()
  const [error, setError] = useState<string>()
  const fnRef = useRef(fetcher)
  fnRef.current = fetcher // 始终调用最新闭包（filter 变化不用重建轮询循环）

  useEffect(() => {
    if (!enabled) return
    let alive = true
    let timer: ReturnType<typeof setTimeout>
    const tick = async () => {
      try {
        const d = await fnRef.current()
        if (alive) {
          setData(d)
          setError(undefined)
        }
      } catch (e) {
        if (alive) setError(e instanceof Error ? e.message : String(e))
      } finally {
        if (alive) timer = setTimeout(tick, interval)
      }
    }
    void tick()
    return () => {
      alive = false
      clearTimeout(timer)
    }
  }, [interval, enabled])

  return { data, error, setData }
}
