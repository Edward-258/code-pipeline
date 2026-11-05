import dayjs from 'dayjs'

// 时间双格式归一 —— created_at 是 RFC3339 字符串，start/end_time 是 unix 秒（旧前端 ts() 的教训）
export default function TimeCell({ value }: { value?: string | number }) {
  if (value === undefined || value === null || value === 0 || value === '') return <span>-</span>
  const d = typeof value === 'number' ? dayjs.unix(value) : dayjs(value)
  if (!d.isValid()) return <span>-</span>
  return <span>{d.format('YYYY-MM-DD HH:mm:ss')}</span>
}
