// fetch 封装 —— 对齐后端约定：
// - 业务错误统一 {"error": "..."} envelope（非 2xx）
// - 日志端点是 text/plain（不走 JSON 解析）
// - 相对路径：dev 走 vite proxy，生产走 nginx 反代，同 host 零配置

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message)
  }
}

async function parseError(res: Response): Promise<ApiError> {
  const text = await res.text()
  let msg = text
  try {
    const j = JSON.parse(text)
    if (j && typeof j.error === 'string') msg = j.error
  } catch {
    /* 纯文本错误体，原样保留 */
  }
  return new ApiError(res.status, msg)
}

export async function api<T>(path: string, opt?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json', ...(opt?.headers ?? {}) },
    ...opt,
  })
  if (!res.ok) throw await parseError(res)
  const text = await res.text()
  return (text ? JSON.parse(text) : undefined) as T
}

export async function apiText(path: string, opt?: RequestInit): Promise<string> {
  const res = await fetch(path, opt)
  if (!res.ok) throw await parseError(res)
  return res.text()
}

export const post = <T>(path: string, body?: unknown): Promise<T> =>
  api<T>(path, { method: 'POST', body: JSON.stringify(body ?? {}) })

export const put = <T>(path: string, body?: unknown): Promise<T> =>
  api<T>(path, { method: 'PUT', body: JSON.stringify(body ?? {}) })

export const del = <T>(path: string): Promise<T> =>
  api<T>(path, { method: 'DELETE' })
