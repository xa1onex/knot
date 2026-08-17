import { NodeClient } from '@node-infra/client'

const TOKEN_KEY = 'knot_token'

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function setToken(token: string | null) {
  if (token) localStorage.setItem(TOKEN_KEY, token)
  else localStorage.removeItem(TOKEN_KEY)
}

export function createClient(token?: string | null): NodeClient {
  return new NodeClient({
    baseUrl: import.meta.env.VITE_API_URL ?? '',
    token: token ?? getToken() ?? '',
  })
}

export async function sha256Hex(data: ArrayBuffer): Promise<string> {
  const dig = await crypto.subtle.digest('SHA-256', data)
  return [...new Uint8Array(dig)].map((b) => b.toString(16).padStart(2, '0')).join('')
}

export type ConflictMode = 'overwrite' | 'rename' | 'skip'

export async function putLocalFile(
  _cl: NodeClient,
  deviceId: string,
  destPath: string,
  file: File,
  conflict: ConflictMode,
  onProgress?: (pct: number) => void,
): Promise<void> {
  const buf = await file.arrayBuffer()
  const sha = await sha256Hex(buf)
  onProgress?.(5)
  const q = new URLSearchParams({
    device_id: deviceId,
    path: destPath,
    sha256: sha,
  })
  if (conflict === 'overwrite') q.set('overwrite', 'true')
  if (conflict === 'rename') q.set('conflict', 'rename')
  const token = getToken()
  const res = await fetch(`/v1/storage/content?${q}`, {
    method: 'PUT',
    headers: {
      Authorization: token ? `Bearer ${token}` : '',
      'Content-Type': 'application/octet-stream',
    },
    body: buf,
  })
  onProgress?.(100)
  if (!res.ok) {
    const data = await res.json().catch(() => ({})) as { error?: { code?: string; message?: string } }
    const err = new Error(data.error?.message || res.statusText) as Error & { code?: string }
    err.code = data.error?.code
    throw err
  }
}
