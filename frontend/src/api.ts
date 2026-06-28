const BASE_PATH = import.meta.env.BASE_URL || '/'
const DEFAULT_API_BASE = BASE_PATH.replace(/\/$/, '')
const API_BASE = import.meta.env.VITE_API_BASE || DEFAULT_API_BASE

export function getToken() { return localStorage.getItem('zxy_token') || '' }
export function setToken(token: string) { localStorage.setItem('zxy_token', token) }
export function clearToken() { localStorage.removeItem('zxy_token') }

function normalizeError(text: string) {
  try {
    const data = JSON.parse(text)
    return data?.error || text
  } catch {
    return text
  }
}

export async function api(path: string, options: RequestInit = {}) {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  const token = getToken()
  if (token) headers.Authorization = `Bearer ${token}`
  const res = await fetch(`${API_BASE}${path}`, { ...options, headers: { ...headers, ...(options.headers as any || {}) } })
  if (!res.ok) {
    const text = await res.text()
    const msg = normalizeError(text || `HTTP ${res.status}`)
    if (res.status === 401 && (msg.includes('invalid token') || msg.includes('missing bearer token') || msg.includes('expired'))) {
      clearToken()
      if (!location.pathname.includes('/login')) {
        alert('登录状态已过期，请重新登录。')
        location.href = `${BASE_PATH}login`
      }
    }
    throw new Error(msg)
  }
  const type = res.headers.get('content-type') || ''
  if (type.includes('application/json')) return res.json()
  return res.text()
}

export { API_BASE }
