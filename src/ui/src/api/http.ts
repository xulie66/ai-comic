// 统一 HTTP 层：所有请求走 /api，自动带 token，401 回登录页。
// 与老前端（public/web-api.js）共用同一套后端，行为保持一致：失败时补 userMessage，别让界面出现 undefined。

const TOKEN_KEY = 'ac_token_v2'

export function getToken(): string {
  try { return localStorage.getItem(TOKEN_KEY) || '' } catch { return '' }
}

export function setToken(token: string) {
  try {
    if (token) localStorage.setItem(TOKEN_KEY, token)
    else localStorage.removeItem(TOKEN_KEY)
  } catch { /* 隐私模式下忽略 */ }
}

export class ApiError extends Error {
  status: number
  payload: any
  constructor(message: string, status: number, payload: any) {
    super(message)
    this.status = status
    this.payload = payload
  }
}

let onUnauthorized: (() => void) | null = null
export function setUnauthorizedHandler(fn: () => void) { onUnauthorized = fn }

type Query = Record<string, string | number | boolean | undefined>

function buildUrl(path: string, query?: Query) {
  const url = '/api' + path
  if (!query) return url
  const parts = Object.entries(query)
    .filter(([, v]) => v !== undefined && v !== '')
    .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`)
  return parts.length ? `${url}?${parts.join('&')}` : url
}

export async function request<T = any>(
  path: string,
  options: { method?: 'GET' | 'POST'; body?: any; rawBody?: BodyInit; query?: Query; timeoutMs?: number } = {}
): Promise<T> {
  const method = options.method || (options.body === undefined && options.rawBody === undefined ? 'GET' : 'POST')
  const headers: Record<string, string> = { Accept: 'application/json' }
  const token = getToken()
  if (token) headers.Authorization = 'Bearer ' + token
  // FormData keeps the browser-generated multipart boundary and shares all auth/error handling.
  let body: BodyInit | undefined = options.rawBody
  if (options.rawBody === undefined && options.body !== undefined) {
    headers['Content-Type'] = 'application/json'
    body = JSON.stringify(options.body)
  }

  const controller = new AbortController()
  const timer = options.timeoutMs ? setTimeout(() => controller.abort(), options.timeoutMs) : null
  let res: Response
  try {
    res = await fetch(buildUrl(path, options.query), {
      method, headers, body, credentials: 'same-origin', signal: controller.signal
    })
  } catch (err: any) {
    if (err?.name === 'AbortError') {
      throw new ApiError('请求超时，服务端可能还在生成，稍后刷新看看', 0, null)
    }
    throw new ApiError('请求没发到服务器（网络或服务重启中），稍等几秒再试', 0, null)
  } finally {
    if (timer) clearTimeout(timer)
  }

  if (res.status === 401) {
    setToken('')
    onUnauthorized?.()
    throw new ApiError('登录已过期，请重新登录', 401, null)
  }

  const text = await res.text()
  let data: any = {}
  if (text) {
    try { data = JSON.parse(text) } catch { data = { success: false, error: text.slice(0, 400) } }
  }
  if (!res.ok) {
    const message = data?.userMessage || data?.error || `HTTP ${res.status}`
    throw new ApiError(String(message), res.status, data)
  }
  if (data && data.success === false) {
    throw new ApiError(String(data.userMessage || data.error || '服务端返回失败'), res.status, data)
  }
  return data as T
}

export const get = <T = any>(path: string, query?: Query) => request<T>(path, { method: 'GET', query })
export const post = <T = any>(path: string, body: any = {}, query?: Query) => request<T>(path, { method: 'POST', body, query })
export const upload = <T = any>(path: string, form: FormData) => request<T>(path, { method: 'POST', rawBody: form })
