import { useCallback, useEffect, useMemo, useRef, useState, type DragEvent } from 'react'
import { Link } from 'react-router-dom'
import { ArrowUp, File as FileIcon, Folder, Monitor, Upload } from 'lucide-react'
import type { Device, StorageEntry, Transfer } from '@node-infra/client'
import { Button } from '@/components/ui/button'
import { createClient, putLocalFile } from '@/lib/client'
import { cn } from '@/lib/cn'
import { formatBytes, formatWhen, joinPath, parentPath } from '@/lib/format'
import { usePrefs } from '@/lib/prefs'
import { newQueueId, type QueueItem } from '@/lib/queue'

type DragPayload = { fromDeviceId: string; fromPath: string; name: string }

function usePane(cl: ReturnType<typeof createClient>, deviceId: string | null, path: string) {
  const [entries, setEntries] = useState<StorageEntry[]>([])
  const [error, setError] = useState('')
  const refresh = useCallback(async () => {
    if (!deviceId) {
      setEntries([])
      return
    }
    try {
      setEntries(await cl.storageList(deviceId, path))
      setError('')
    } catch (e) {
      setEntries([])
      setError(e instanceof Error ? e.message : 'error')
    }
  }, [cl, deviceId, path])
  useEffect(() => {
    void refresh()
  }, [refresh])
  return { entries, error, refresh }
}

export default function Files() {
  const { t } = usePrefs()
  const cl = useMemo(() => createClient(), [])
  const [devices, setDevices] = useState<Device[]>([])
  const [leftId, setLeftId] = useState<string | null>(null)
  const [rightId, setRightId] = useState<string | null>(null)
  const [leftPath, setLeftPath] = useState('')
  const [rightPath, setRightPath] = useState('')
  const [queue, setQueue] = useState<QueueItem[]>([])
  const [transfers, setTransfers] = useState<Transfer[]>([])
  const [toasts, setToasts] = useState<{ id: string; text: string; tone: string }[]>([])
  const [dropping, setDropping] = useState<'left' | 'right' | null>(null)
  const leftInput = useRef<HTMLInputElement>(null)
  const rightInput = useRef<HTMLInputElement>(null)

  const left = usePane(cl, leftId, leftPath)
  const right = usePane(cl, rightId, rightPath)

  const toast = (text: string, tone = 'info') => {
    const id = newQueueId()
    setToasts((x) => [...x, { id, text, tone }])
    setTimeout(() => setToasts((x) => x.filter((t) => t.id !== id)), 4000)
  }

  useEffect(() => {
    void (async () => {
      const list = (await cl.listDevices()).filter((d) => !d.revoked_at)
      setDevices(list)
      setLeftId((cur) => cur && list.some((d) => d.id === cur) ? cur : list[0]?.id ?? null)
      setRightId((cur) => {
        if (cur && list.some((d) => d.id === cur)) return cur
        return list.find((d) => d.id !== list[0]?.id)?.id ?? list[1]?.id ?? list[0]?.id ?? null
      })
    })()
    const id = setInterval(() => {
      void cl.listDevices().then((list) => setDevices(list.filter((d) => !d.revoked_at))).catch(() => {})
    }, 8000)
    return () => clearInterval(id)
  }, [cl])

  const refreshTransfers = useCallback(async () => {
    try {
      setTransfers((await cl.listTransfers()).slice(0, 12))
    } catch { /* */ }
  }, [cl])

  useEffect(() => {
    void refreshTransfers()
    const id = setInterval(() => void refreshTransfers(), 1200)
    return () => clearInterval(id)
  }, [refreshTransfers])

  function patchQueue(id: string, patch: Partial<QueueItem>) {
    setQueue((q) => q.map((x) => (x.id === id ? { ...x, ...patch } : x)))
  }

  async function copyBetween(fromId: string, fromPath: string, name: string, toId: string, toPath: string) {
    if (fromId === toId && fromPath === joinPath(toPath, name)) return
    const dest = joinPath(toPath, name)
    const toDev = devices.find((d) => d.id === toId)
    const id = newQueueId()
    setQueue((q) => [...q, { id, kind: 'transfer', label: `${name} → ${toDev?.name || 'PC'}`, status: 'running', percent: 0, retries: 0 }])
    try {
      const job = await cl.storageTransfer(fromId, fromPath, toId, dest)
      if (job.id) await cl.watchTransfer(job.id, { pollMs: 250, onProgress: (p) => patchQueue(id, { percent: p.percent }) })
      patchQueue(id, { status: 'done', percent: 100 })
      toast(t('sent', { name, dest: toDev?.name || '' }), 'ok')
      await left.refresh()
      await right.refresh()
      await refreshTransfers()
    } catch (e) {
      patchQueue(id, { status: 'error', error: e instanceof Error ? e.message : 'failed' })
      toast(e instanceof Error ? e.message : 'failed', 'err')
    }
  }

  async function uploadInto(files: File[], deviceId: string, basePath: string) {
    for (const file of files) {
      const id = newQueueId()
      const dest = joinPath(basePath, file.name)
      setQueue((q) => [...q, { id, kind: 'upload', label: file.name, deviceId, path: dest, status: 'running', percent: 0, retries: 0 }])
      try {
        await putLocalFile(cl, deviceId, dest, file, 'rename', (pct) => patchQueue(id, { percent: pct }))
        patchQueue(id, { status: 'done', percent: 100 })
        toast(t('uploaded', { name: file.name }), 'ok')
      } catch (e) {
        patchQueue(id, { status: 'error', error: e instanceof Error ? e.message : 'failed' })
        toast(e instanceof Error ? e.message : 'failed', 'err')
      }
    }
    await left.refresh()
    await right.refresh()
  }

  function onDrop(side: 'left' | 'right', e: DragEvent) {
    e.preventDefault()
    setDropping(null)
    const deviceId = side === 'left' ? leftId : rightId
    const path = side === 'left' ? leftPath : rightPath
    if (!deviceId) return
    if (e.dataTransfer.files?.length) {
      void uploadInto([...e.dataTransfer.files], deviceId, path)
      return
    }
    const raw = e.dataTransfer.getData('application/x-node-file')
    if (!raw) return
    const payload = JSON.parse(raw) as DragPayload
    void copyBetween(payload.fromDeviceId, payload.fromPath, payload.name, deviceId, path)
  }

  async function mkdir(deviceId: string | null, path: string, refresh: () => Promise<void>) {
    if (!deviceId) return
    const name = window.prompt(t('folder_name'))
    if (!name) return
    await cl.storageMkdir(deviceId, joinPath(path, name.trim()))
    await refresh()
  }

  const active = queue.filter((q) => q.status === 'running' || q.status === 'queued')
  const liveXfer = transfers.filter((x) => ['pending', 'offered', 'negotiating', 'transferring'].includes(x.status))

  if (!devices.length) {
    return (
      <div className="flex flex-1 items-center justify-center p-10">
        <div className="max-w-md space-y-3">
          <h1 className="text-3xl font-semibold tracking-tight">{t('no_computers')}</h1>
          <p className="text-muted-foreground">{t('no_computers_lead')}</p>
          <Link to="/computers">
            <Button>{t('add_computer')}</Button>
          </Link>
        </div>
      </div>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center justify-between gap-4 border-b border-border/40 px-5 py-3">
        <p className="text-sm text-muted-foreground">{t('files_hint')}</p>
      </div>
      <div className="grid min-h-0 flex-1 grid-cols-1 divide-y divide-border/40 md:grid-cols-2 md:divide-x md:divide-y-0">
        <Pane
          side="left"
          devices={devices}
          deviceId={leftId}
          onDevice={setLeftId}
          path={leftPath}
          onPath={setLeftPath}
          entries={left.entries}
          error={left.error}
          dropping={dropping === 'left'}
          onDragOver={() => setDropping('left')}
          onDragLeave={() => setDropping(null)}
          onDrop={(e) => onDrop('left', e)}
          onUpload={() => leftInput.current?.click()}
          onMkdir={() => void mkdir(leftId, leftPath, left.refresh)}
          t={t}
        />
        <Pane
          side="right"
          devices={devices}
          deviceId={rightId}
          onDevice={setRightId}
          path={rightPath}
          onPath={setRightPath}
          entries={right.entries}
          error={right.error}
          dropping={dropping === 'right'}
          onDragOver={() => setDropping('right')}
          onDragLeave={() => setDropping(null)}
          onDrop={(e) => onDrop('right', e)}
          onUpload={() => rightInput.current?.click()}
          onMkdir={() => void mkdir(rightId, rightPath, right.refresh)}
          t={t}
        />
      </div>
      <input ref={leftInput} type="file" multiple hidden onChange={(e) => {
        if (leftId && e.target.files?.length) void uploadInto([...e.target.files], leftId, leftPath)
        e.target.value = ''
      }} />
      <input ref={rightInput} type="file" multiple hidden onChange={(e) => {
        if (rightId && e.target.files?.length) void uploadInto([...e.target.files], rightId, rightPath)
        e.target.value = ''
      }} />

      <footer className="border-t border-border/40 px-5 py-3">
        <p className="mb-2 text-[11px] font-semibold uppercase tracking-[0.18em] text-foreground/40">{t('transfers_dock')}</p>
        {!active.length && !liveXfer.length && (
          <p className="text-sm text-muted-foreground">{t('transfers_empty')}</p>
        )}
        <ul className="grid gap-2">
          {active.map((q) => (
            <li key={q.id}>
              <div className="flex justify-between text-sm">
                <span>{q.label}</span>
                <span className="text-muted-foreground">{q.percent}%</span>
              </div>
              <div className="bar mt-1"><i style={{ width: `${q.percent}%` }} /></div>
            </li>
          ))}
        </ul>
      </footer>

      <div className="toasts">
        {toasts.map((x) => (
          <div key={x.id} className={`toast ${x.tone}`}>{x.text}</div>
        ))}
      </div>
    </div>
  )
}

function Pane({
  devices,
  deviceId,
  onDevice,
  path,
  onPath,
  entries,
  error,
  dropping,
  onDragOver,
  onDragLeave,
  onDrop,
  onUpload,
  onMkdir,
  t,
}: {
  side: 'left' | 'right'
  devices: Device[]
  deviceId: string | null
  onDevice: (id: string) => void
  path: string
  onPath: (p: string) => void
  entries: StorageEntry[]
  error: string
  dropping: boolean
  onDragOver: () => void
  onDragLeave: () => void
  onDrop: (e: DragEvent) => void
  onUpload: () => void
  onMkdir: () => void
  t: (k: string) => string
}) {
  const device = devices.find((d) => d.id === deviceId)
  const visible = [...entries].sort((a, b) => {
    if (a.is_directory !== b.is_directory) return a.is_directory ? -1 : 1
    return a.name.localeCompare(b.name)
  })

  return (
    <section
      className={cn('relative flex min-h-0 flex-col bg-background/40', dropping && 'bg-emerald-500/10')}
      onDragOver={(e) => {
        e.preventDefault()
        onDragOver()
      }}
      onDragLeave={onDragLeave}
      onDrop={onDrop}
    >
      {dropping && (
        <div className="pointer-events-none absolute inset-0 z-10 grid place-items-center rounded-none border-2 border-dashed border-emerald-500/60">
          <span className="rounded-full bg-background/90 px-4 py-2 text-sm font-medium">{t('drop_here')}</span>
        </div>
      )}
      <header className="flex flex-wrap items-center gap-2 border-b border-border/40 px-4 py-3">
        <Monitor className="h-4 w-4 text-muted-foreground" />
        <select
          value={deviceId || ''}
          onChange={(e) => {
            onDevice(e.target.value)
            onPath('')
          }}
          className="min-w-40 flex-1"
        >
          {devices.map((d) => (
            <option key={d.id} value={d.id}>
              {d.name} {d.online ? '' : `(${t('offline')})`}
            </option>
          ))}
        </select>
        <Button variant="outline" size="sm" disabled={!device?.online} onClick={onUpload}>
          <Upload className="h-3.5 w-3.5" />
          {t('upload')}
        </Button>
        <Button variant="ghost" size="sm" disabled={!device?.online} onClick={onMkdir}>
          {t('new_folder')}
        </Button>
      </header>
      <div className="flex items-center gap-2 px-4 py-2 text-sm text-muted-foreground">
        {path ? (
          <button type="button" className="ghost inline-flex items-center gap-1 px-0" onClick={() => onPath(parentPath(path))}>
            <ArrowUp className="h-3.5 w-3.5" /> {t('up')}
          </button>
        ) : null}
        <span className="truncate">{path || t('this_folder')}</span>
      </div>
      {!device?.online && <p className="px-4 pb-2 text-sm text-amber-700 dark:text-amber-400">{t('offline')}</p>}
      {error && <p className="error px-4">{error}</p>}
      <div className="min-h-0 flex-1 overflow-auto px-2 pb-3">
        {visible.length === 0 && <p className="px-2 py-8 text-sm text-muted-foreground">{t('empty_folder')}</p>}
        {visible.map((e) => (
          <div
            key={e.path}
            draggable={!e.is_directory && !!deviceId}
            onDragStart={(ev) => {
              if (!deviceId || e.is_directory) return
              ev.dataTransfer.setData('application/x-node-file', JSON.stringify({
                fromDeviceId: deviceId,
                fromPath: e.path,
                name: e.name,
              } satisfies DragPayload))
              ev.dataTransfer.effectAllowed = 'copy'
            }}
            onDoubleClick={() => {
              if (e.is_directory) onPath(e.path)
            }}
            className={cn(
              'flex cursor-default items-center gap-3 rounded-xl px-2 py-2 transition-colors hover:bg-muted',
              !e.is_directory && 'cursor-grab active:cursor-grabbing',
            )}
          >
            {e.is_directory ? (
              <Folder className="h-4 w-4 shrink-0 text-foreground/50" />
            ) : (
              <FileIcon className="h-4 w-4 shrink-0 text-foreground/50" />
            )}
            <button
              type="button"
              className="ghost min-w-0 flex-1 truncate px-0 py-0 text-left font-medium"
              onClick={() => e.is_directory && onPath(e.path)}
            >
              {e.name}
            </button>
            <span className="hidden w-20 shrink-0 text-right text-xs text-muted-foreground sm:block">
              {e.is_directory ? '—' : formatBytes(e.size ?? 0)}
            </span>
            <span className="hidden w-28 shrink-0 text-right text-xs text-muted-foreground lg:block">
              {formatWhen(e.mtime)}
            </span>
          </div>
        ))}
      </div>
    </section>
  )
}
