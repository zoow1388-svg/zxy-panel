import { runtimeBasePath } from './runtimeBase'

const BASE_PATH = runtimeBasePath()
const DEFAULT_API_BASE = BASE_PATH.replace(/\/$/, '')
const API_BASE = import.meta.env.VITE_API_BASE || DEFAULT_API_BASE

export function getToken() { return localStorage.getItem('zxy_token') || '' }
export function setToken(token: string) { localStorage.setItem('zxy_token', token) }
export function clearToken() { localStorage.removeItem('zxy_token') }

export type ApiConflict = { kind: string; resource_id?: string; message: string }

export class ApiError extends Error {
  readonly conflicts: ApiConflict[]

  constructor(message: string, readonly status: number, conflicts: unknown = []) {
    super(message)
    this.conflicts = checkedConflicts(conflicts)
  }
}

function checkedConflicts(value: unknown): ApiConflict[] {
  try {
    if (!Array.isArray(value)) return []
    return value.flatMap(item => {
      if (!item || typeof item !== 'object' || typeof item.kind !== 'string' || !item.kind.trim() || typeof item.message !== 'string' || !item.message.trim()) return []
      if (item.resource_id !== undefined && typeof item.resource_id !== 'string') return []
      return [{ kind: item.kind, message: item.message, ...(item.resource_id === undefined ? {} : { resource_id: item.resource_id }) }]
    })
  } catch {
    // Optional conflict details must never replace the original HTTP error.
    return []
  }
}

function errorConflicts(text: string): unknown {
  try { return JSON.parse(text)?.conflicts } catch { return [] }
}

export function formatMutationError(error: unknown): string {
  if (!(error instanceof ApiError)) return '请求结果尚未确认，请手动刷新页面核对。不要自动重试写入。'
  let label = `操作失败（HTTP ${error.status}）`
  if (error.status === 400) label = '请求参数错误（HTTP 400）'
  if (error.status === 500) label = '服务器处理失败（HTTP 500）'
  if (error.status === 409) {
    const references = new Set(['client_node_reference', 'relay_landing_reference', 'client_relay_reference'])
    const identities = new Set(['node_identity_inconsistent', 'node_server_invalid', 'relay_identity', 'server_parent', 'node_identity', 'client_identity', 'relay_mode', 'landing_exit_identity'])
    label = error.conflicts.length && error.conflicts.every(item => references.has(item.kind)) ? '引用冲突（HTTP 409）'
      : error.conflicts.length && error.conflicts.every(item => identities.has(item.kind)) ? '身份或父级归属冲突（HTTP 409）'
      : '资源冲突（HTTP 409）'
  }
  const details = error.conflicts.map(item => `冲突类型：${item.kind}；资源ID：${item.resource_id || '未提供'}；原因：${item.message}`)
  return [`${label}：${error.message}`, ...details].join('\n')
}

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
    throw new ApiError(msg, res.status, errorConflicts(text))
  }
  const type = res.headers.get('content-type') || ''
  if (type.includes('application/json')) return res.json()
  return res.text()
}

export { API_BASE }
