const TOKEN_KEY = 'knot_token'
const API_BASE = import.meta.env.VITE_API_URL ?? ''

export function getToken() {
  return localStorage.getItem(TOKEN_KEY)
}

export function setToken(token: string | null) {
  if (token) localStorage.setItem(TOKEN_KEY, token)
  else localStorage.removeItem(TOKEN_KEY)
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Content-Type', 'application/json')
  const token = getToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)
  const res = await fetch(`${API_BASE}${path}`, { ...init, headers })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    throw new Error((data as { error?: string }).error || res.statusText)
  }
  return data as T
}

export type Device = {
  id: string
  name: string
  hostname: string
  os: string
  arch: string
  cpus: number
  ram_mb: number
  online: boolean
  last_seen_at?: string
  created_at: string
}

export type Credential = {
  id: string
  name: string
  token_prefix: string
  scopes: string[]
  expires_at?: string
  revoked_at?: string
  created_at: string
}

export type AuditEvent = {
  ID: string
  Actor: string
  Action: string
  Resource: string
  Detail: string
  Result: string
  CreatedAt: string
}
