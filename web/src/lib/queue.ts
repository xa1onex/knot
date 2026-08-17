export type QueueItem = {
  id: string
  kind: 'upload' | 'download' | 'transfer'
  label: string
  deviceId?: string
  path?: string
  status: 'queued' | 'running' | 'done' | 'error' | 'cancelled'
  percent: number
  error?: string
  speed?: number
  eta?: string
  retries: number
}

export function newQueueId(): string {
  return crypto.randomUUID()
}
