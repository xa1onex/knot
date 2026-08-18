import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type DragEvent,
  type MouseEvent as ReactMouseEvent,
} from 'react'
import { Link } from 'react-router-dom'
import type { ComputeDevice, Device, StorageEntry, Transfer } from '@node-infra/client'
import { createClient, putLocalFile, type ConflictMode } from '../lib/client'
import { formatBytes, formatEta, formatWhen, joinPath, parentPath } from '../lib/format'
import { newQueueId, type QueueItem } from '../lib/queue'
import Chrome from '../components/Chrome'

type DragPayload = {
  fromDeviceId: string
  fromPath: string
  name: string
  isDir: boolean
}

type AllFile = StorageEntry & { device_id: string; device_name: string }

type CtxMenu = {
  x: number
  y: number
  entries: StorageEntry[]
}

type Toast = { id: string; text: string; tone: 'ok' | 'err' | 'info' }
type Thumb = { url: string; mime: string }
type PreviewState = {
  url?: string
  name: string
  mime: string
  text?: string
  generic?: boolean
}

const ROOTS = ['photos', 'projects', 'backups', 'shared'] as const

function thumbKey(deviceId: string, path: string): string {
  return `${deviceId}:${path}`
}

function canThumb(entry: StorageEntry): boolean {
  if (entry.is_directory) return false
  const mime = entry.mime_type || ''
  if (mime.startsWith('image/') || mime.startsWith('video/') || mime === 'application/pdf') return true
  return /\.(jpe?g|png|webp|gif|pdf|mp4|mov|m4v|webm)$/i.test(entry.name)
}

function canTextPreview(entry: StorageEntry): boolean {
  const mime = entry.mime_type || ''
  if (mime.startsWith('text/') || mime.includes('json') || mime.includes('xml')) return true
  return /\.(txt|md|json|csv|ya?ml|toml|xml|html?|css|js|ts|tsx|jsx|go|rs|py|sh|env|ini|conf|log)$/i.test(entry.name)
}

export default function NodeApp() {
  const cl = useMemo(() => createClient(), [])
  const [devices, setDevices] = useState<Device[]>([])
  const [compute, setCompute] = useState<ComputeDevice[]>([])
  const [view, setView] = useState<'node' | 'all'>('node')
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [path, setPath] = useState('')
  const [entries, setEntries] = useState<StorageEntry[]>([])
  const [allFiles, setAllFiles] = useState<AllFile[]>([])
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [transfers, setTransfers] = useState<Transfer[]>([])
  const [queue, setQueue] = useState<QueueItem[]>([])
  const [error, setError] = useState('')
  const [filter, setFilter] = useState('')
  const [searchQ, setSearchQ] = useState('')
  const [nodeFilter, setNodeFilter] = useState('')
  const [typeFilter, setTypeFilter] = useState('')
  const [dropHint, setDropHint] = useState('')
  const [ctx, setCtx] = useState<CtxMenu | null>(null)
  const [toasts, setToasts] = useState<Toast[]>([])
  const [preview, setPreview] = useState<PreviewState | null>(null)
  const [thumbs, setThumbs] = useState<Record<string, Thumb>>({})
  const [conflictAsk, setConflictAsk] = useState<{
    files: File[]
    deviceId: string
    basePath: string
  } | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const speedRef = useRef<Record<string, { bytes: number; at: number }>>({})

  const selectedDevice = devices.find((d) => d.id === selectedId) ?? null
  const selectedCompute = compute.find((c) => c.device_id === selectedId) ?? null

  function toast(text: string, tone: Toast['tone'] = 'info') {
    const id = newQueueId()
    setToasts((t) => [...t, { id, text, tone }])
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 4200)
  }

  const refreshDevices = useCallback(async () => {
    const list = await cl.listDevices()
    const live = list.filter((d) => !d.revoked_at)
    setDevices(live)
    setSelectedId((cur) => {
      if (cur && live.some((d) => d.id === cur)) return cur
      return live.find((d) => d.online)?.id ?? live[0]?.id ?? null
    })
    try {
      setCompute(await cl.listComputeDevices())
    } catch {
      setCompute([])
    }
  }, [cl])

  const refreshEntries = useCallback(async () => {
    if (view === 'all') {
      try {
        const q = searchQ
        const hits = await cl.filesSearch({
          q: q || undefined,
          folder: q ? undefined : (path || undefined),
          device_id: nodeFilter || undefined,
          type: typeFilter || undefined,
        })
        setAllFiles(hits.map((h) => ({
          name: h.name,
          path: h.path,
          is_directory: h.is_directory,
          size: h.size,
          mtime: h.mtime,
          sha256: h.sha256,
          mime_type: h.mime_type,
          file_id: h.file_id,
          device_id: h.device_id,
          device_name: h.device_name,
        })))
        setEntries([])
        setError('')
      } catch (e) {
        setAllFiles([])
        setError(e instanceof Error ? e.message : 'Search failed')
      }
      return
    }
    if (!selectedId) {
      setEntries([])
      return
    }
    try {
      setEntries(await cl.storageList(selectedId, path))
      setError('')
    } catch (e) {
      setEntries([])
      setError(e instanceof Error ? e.message : 'Failed to list')
    }
  }, [cl, selectedId, path, view, searchQ, nodeFilter, typeFilter])

  const refreshTransfers = useCallback(async () => {
    try {
      setTransfers((await cl.listTransfers()).slice(0, 16))
    } catch { /* */ }
  }, [cl])

  useEffect(() => {
    void refreshDevices().catch((e) => setError(String(e)))
    const id = setInterval(() => void refreshDevices().catch(() => {}), 8000)
    return () => clearInterval(id)
  }, [refreshDevices])

  useEffect(() => {
    void refreshEntries()
    setSelected(new Set())
  }, [refreshEntries])

  useEffect(() => {
    if (view !== 'all') return
    const t = window.setTimeout(() => setSearchQ(filter.trim()), 280)
    return () => window.clearTimeout(t)
  }, [filter, view])

  const didIndex = useRef(false)
  useEffect(() => {
    if (view !== 'all' || didIndex.current) return
    didIndex.current = true
    void cl.filesReindex().catch(() => {}).finally(() => { void refreshEntries() })
  }, [view, cl, refreshEntries])

  useEffect(() => {
    void refreshTransfers()
    const id = setInterval(() => void refreshTransfers(), 1000)
    return () => clearInterval(id)
  }, [refreshTransfers])

  useEffect(() => {
    const close = () => setCtx(null)
    window.addEventListener('click', close)
    return () => window.removeEventListener('click', close)
  }, [])

  const visibleNode = useMemo(() => {
    return entries
      .filter((e) => !filter || e.name.toLowerCase().includes(filter.toLowerCase()))
      .slice()
      .sort((a, b) => {
        if (a.is_directory !== b.is_directory) return a.is_directory ? -1 : 1
        return a.name.localeCompare(b.name)
      })
  }, [entries, filter])

  const visibleAll = useMemo(() => {
    return allFiles
      .slice()
      .sort((a, b) => {
        if (a.is_directory !== b.is_directory) return a.is_directory ? -1 : 1
        return a.name.localeCompare(b.name)
      })
  }, [allFiles])

  useEffect(() => {
    const pending = view === 'all'
      ? visibleAll.slice(0, 48).filter((e) => canThumb(e) && !thumbs[thumbKey(e.device_id, e.path)]).map((e) => ({ entry: e, deviceId: e.device_id }))
      : (selectedId ? visibleNode.slice(0, 48).filter((e) => canThumb(e) && !thumbs[thumbKey(selectedId, e.path)]).map((e) => ({ entry: e, deviceId: selectedId })) : [])
    if (!pending.length) return
    let cancelled = false
    void (async () => {
      for (const item of pending) {
        try {
          const { data, contentType } = await cl.storagePreview(item.deviceId, item.entry.path, { variant: 'thumb', maxPixels: 240 })
          if (cancelled) return
          const url = URL.createObjectURL(data)
          setThumbs((prev) => ({ ...prev, [thumbKey(item.deviceId, item.entry.path)]: { url, mime: contentType } }))
        } catch {
          // Generic file icon fallback is fine.
        }
      }
    })()
    return () => { cancelled = true }
  }, [cl, selectedId, thumbs, view, visibleAll, visibleNode])

  function toggleSelect(pathKey: string, additive: boolean) {
    setSelected((prev) => {
      const next = additive ? new Set(prev) : new Set<string>()
      if (next.has(pathKey)) next.delete(pathKey)
      else next.add(pathKey)
      return next
    })
  }

  function selectedEntries(): StorageEntry[] {
    if (view === 'all') return visibleAll.filter((e) => selected.has(`${e.device_id}:${e.path}`))
    return visibleNode.filter((e) => selected.has(e.path))
  }

  async function runOp(label: string, fn: () => Promise<void>) {
    setError('')
    try {
      await fn()
      await refreshEntries()
      await refreshTransfers()
      toast(label, 'ok')
    } catch (e) {
      const msg = e instanceof Error ? e.message : label
      setError(msg)
      toast(msg, 'err')
    }
  }

  function enqueue(item: Omit<QueueItem, 'retries'> & { retries?: number }) {
    setQueue((q) => [...q, { ...item, retries: item.retries ?? 0 }])
  }

  function patchQueue(id: string, patch: Partial<QueueItem>) {
    setQueue((q) => q.map((x) => (x.id === id ? { ...x, ...patch } : x)))
  }

  async function uploadFiles(files: File[], deviceId: string, basePath: string, conflict: ConflictMode) {
    for (const file of files) {
      if (conflict === 'skip') {
        try {
          await cl.storageStat(deviceId, joinPath(basePath, file.name))
          toast(`${file.name} exists — skipped`, 'info')
          continue
        } catch { /* free */ }
      }
      const id = newQueueId()
      const dest = joinPath(basePath, file.name)
      enqueue({ id, kind: 'upload', label: file.name, deviceId, path: dest, status: 'running', percent: 0 })
      try {
        await putLocalFile(cl, deviceId, dest, file, conflict === 'skip' ? 'rename' : conflict, (pct) =>
          patchQueue(id, { percent: pct }),
        )
        patchQueue(id, { status: 'done', percent: 100 })
        toast(`Uploaded ${file.name}`, 'ok')
      } catch (e) {
        const err = e as Error & { code?: string }
        if (err.code === 'name_conflict') {
          patchQueue(id, { status: 'error', error: 'name conflict' })
          setConflictAsk({ files: [file], deviceId, basePath })
          return
        }
        patchQueue(id, { status: 'error', error: err.message })
        toast(err.message, 'err')
      }
    }
    await refreshEntries()
  }

  function onLocalFiles(files: FileList | File[]) {
    if (!selectedId || view === 'all') {
      toast('Pick a computer on the left first, then upload', 'info')
      return
    }
    if (!selectedDevice?.online) {
      toast('That computer is offline', 'err')
      return
    }
    void uploadFiles([...files], selectedId, path, 'rename')
  }

  async function pickAndUpload() {
    const desk = window.nodeDesktop
    if (desk?.pickUploadFiles) {
      const picked = await desk.pickUploadFiles()
      if (!picked.length) return
      const files = picked.map((p) => {
        const bin = Uint8Array.from(atob(p.dataBase64), (c) => c.charCodeAt(0))
        return new File([bin], p.name)
      })
      onLocalFiles(files)
      return
    }
    fileInputRef.current?.click()
  }

  const pickAndUploadRef = useRef(pickAndUpload)
  pickAndUploadRef.current = pickAndUpload

  useEffect(() => {
    const desk = window.nodeDesktop
    if (!desk?.onMenuUpload) return
    return desk.onMenuUpload(() => {
      void pickAndUploadRef.current()
    })
  }, [])

  function onMkdir() {
    if (!selectedId || view === 'all') return
    const name = window.prompt('New folder name')
    if (!name) return
    void runOp('Folder created', async () => {
      await cl.storageMkdir(selectedId, joinPath(path, name.trim()))
    })
  }

  function onRename(entry: StorageEntry, deviceId: string) {
    const next = window.prompt('Rename to', entry.name)
    if (!next || next === entry.name) return
    const dest = view === 'all'
      ? joinPath(parentPath(entry.path), next.trim())
      : joinPath(path, next.trim())
    void runOp('Renamed', async () => {
      await cl.storageMove(deviceId, entry.path, dest)
    })
  }

  function onDeleteMany(items: { deviceId: string; path: string; name: string }[]) {
    if (!items.length) return
    if (!window.confirm(`Delete ${items.length} item(s)?`)) return
    void runOp('Deleted', async () => {
      for (const it of items) await cl.storageDelete(it.deviceId, it.path)
      setSelected(new Set())
    })
  }

  function onCopy(entry: StorageEntry, deviceId: string) {
    if (entry.is_directory) return
    const next = window.prompt('Copy as', `${entry.name}.copy`)
    if (!next) return
    const to = joinPath(path || parentPath(entry.path), next.trim())
    void runOp('Copied', async () => {
      await cl.storageCopy(deviceId, entry.path, to)
    })
  }

  async function onPreview(entry: StorageEntry, deviceId: string) {
    if (entry.is_directory) return
    try {
      if (canTextPreview(entry)) {
        const { data, contentType } = await cl.storagePreview(deviceId, entry.path, { variant: 'preview', maxPixels: 0 })
        setPreview({ name: entry.name, mime: contentType, text: await data.text() })
        return
      }
      if (canThumb(entry)) {
        const { data, contentType } = await cl.storagePreview(deviceId, entry.path, { variant: 'preview', maxPixels: 1600 })
        const url = URL.createObjectURL(data)
        setPreview({ url, name: entry.name, mime: contentType })
        return
      }
      setPreview({ name: entry.name, mime: entry.mime_type || 'application/octet-stream', generic: true })
    } catch (e) {
      toast(e instanceof Error ? e.message : 'Preview failed', 'err')
    }
  }

  async function onDownload(entry: StorageEntry, deviceId: string) {
    if (entry.is_directory) return
    const id = newQueueId()
    enqueue({ id, kind: 'download', label: entry.name, deviceId, path: entry.path, status: 'running', percent: 10 })
    try {
      const { data, contentType } = await cl.storageContent(deviceId, entry.path)
      const url = URL.createObjectURL(data)
      const a = document.createElement('a')
      a.href = url
      a.download = entry.name
      a.click()
      URL.revokeObjectURL(url)
      patchQueue(id, { status: 'done', percent: 100 })
      toast(`Downloaded ${entry.name}`, 'ok')
      void contentType
    } catch (e) {
      patchQueue(id, { status: 'error', error: e instanceof Error ? e.message : 'download failed' })
      toast('Download failed (file may be too large — try Send to another computer)', 'err')
    }
  }

  function onSendToNode(entry: StorageEntry, fromId: string) {
    const targets = devices.filter((d) => d.id !== fromId && d.online)
    if (!targets.length) {
      toast('No other connected computer to send to', 'info')
      return
    }
    const pick = window.prompt(targets.map((d, i) => `${i + 1}. ${d.name}`).join('\n'), '1')
    const dest = targets[Number(pick) - 1]
    if (!dest) return
    const toPath = joinPath(path && view === 'node' && selectedId === dest.id ? path : 'shared', entry.name)
    const id = newQueueId()
    enqueue({ id, kind: 'transfer', label: `${entry.name} → ${dest.name}`, status: 'running', percent: 0 })
    void (async () => {
      try {
        const t = await cl.storageTransfer(fromId, entry.path, dest.id, toPath)
        if (t.id) {
          await cl.watchTransfer(t.id, {
            pollMs: 250,
            onProgress: (p) => {
              const now = Date.now()
              const prev = speedRef.current[t.id]
              let speed = 0
              if (prev && now > prev.at) speed = Math.max(0, ((p.bytes_received - prev.bytes) * 1000) / (now - prev.at))
              speedRef.current[t.id] = { bytes: p.bytes_received, at: now }
              patchQueue(id, {
                percent: p.percent,
                speed,
                eta: formatEta(Math.max(0, p.size - p.bytes_received), speed),
              })
            },
          })
        }
        patchQueue(id, { status: 'done', percent: 100 })
        toast(`Sent to ${dest.name}`, 'ok')
        await refreshTransfers()
        await refreshEntries()
      } catch (e) {
        patchQueue(id, { status: 'error', error: e instanceof Error ? e.message : 'transfer failed' })
        toast(e instanceof Error ? e.message : 'Transfer failed', 'err')
      }
    })()
  }

  function startDrag(e: DragEvent, entry: StorageEntry, deviceId: string) {
    if (entry.is_directory) return
    const payload: DragPayload = {
      fromDeviceId: deviceId,
      fromPath: entry.path,
      name: entry.name,
      isDir: false,
    }
    e.dataTransfer.setData('application/x-node-file', JSON.stringify(payload))
    e.dataTransfer.effectAllowed = 'copy'
  }

  function onNodeDrop(e: DragEvent, toDeviceId: string) {
    e.preventDefault()
    setDropHint('')
    if (e.dataTransfer.files?.length) {
      setSelectedId(toDeviceId)
      setView('node')
      void uploadFiles([...e.dataTransfer.files], toDeviceId, path, 'rename')
      return
    }
    const raw = e.dataTransfer.getData('application/x-node-file')
    if (!raw) return
    const payload = JSON.parse(raw) as DragPayload
    const toPath = joinPath(path && selectedId === toDeviceId ? path : 'shared', payload.name)
    const id = newQueueId()
    enqueue({ id, kind: 'transfer', label: payload.name, status: 'running', percent: 0 })
    void (async () => {
      try {
        const t = await cl.storageTransfer(payload.fromDeviceId, payload.fromPath, toDeviceId, toPath)
        if (t.id) await cl.watchTransfer(t.id, { pollMs: 250, onProgress: (p) => patchQueue(id, { percent: p.percent }) })
        patchQueue(id, { status: 'done', percent: 100 })
        toast('Transfer complete', 'ok')
        await refreshEntries()
      } catch (err) {
        patchQueue(id, { status: 'error', error: err instanceof Error ? err.message : 'failed' })
      }
    })()
  }

  function onPaneDrop(e: DragEvent) {
    e.preventDefault()
    if (!selectedId || view === 'all') return
    if (e.dataTransfer.files?.length) {
      void uploadFiles([...e.dataTransfer.files], selectedId, path, 'rename')
      return
    }
    onNodeDrop(e, selectedId)
  }

  function openCtx(e: ReactMouseEvent, entry: StorageEntry) {
    e.preventDefault()
    e.stopPropagation()
    const key = view === 'all' ? `${(entry as AllFile).device_id}:${entry.path}` : entry.path
    const picks = selected.has(key) ? selectedEntries() : [entry]
    setCtx({ x: e.clientX, y: e.clientY, entries: picks })
  }

  function retryQueue(item: QueueItem) {
    if (item.kind !== 'upload' || !item.deviceId || !item.path) return
    toast('Re-select the file to retry upload', 'info')
    void pickAndUpload()
  }

  const activeQueue = queue.filter((q) => q.status === 'running' || q.status === 'queued')
  const activeTransfers = transfers.filter((t) =>
    ['pending', 'offered', 'negotiating', 'transferring'].includes(t.status),
  )

  return (
    <Chrome
      fill
      search={
        <input
          placeholder={view === 'all' ? 'Search files on every computer…' : 'Find a file in this folder…'}
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          aria-label={view === 'all' ? 'Search all files' : 'Filter files'}
        />
      }
    >
      <div className="workspace">
        <aside className="nodes-rail">
          <div className="rail-label">Where to look</div>
          <ul className="node-list">
            <li>
              <button
                type="button"
                className={`node-item ${view === 'all' ? 'active' : ''}`}
                onClick={() => { setView('all'); setPath('') }}
              >
                <span className="pulse all" aria-hidden />
                <span className="node-meta">
                  <strong>All computers</strong>
                  <small>Search everything at once</small>
                </span>
              </button>
            </li>
            {devices.map((d) => (
              <li key={d.id}>
                <button
                  type="button"
                  className={`node-item ${view === 'node' && selectedId === d.id ? 'active' : ''} ${d.online ? 'is-online' : 'is-offline'}`}
                  onClick={() => { setView('node'); setSelectedId(d.id); setPath('') }}
                  onDragOver={(e) => {
                    e.preventDefault()
                    setDropHint(`Drop to send into ${d.name}`)
                  }}
                  onDragLeave={() => setDropHint('')}
                  onDrop={(e) => onNodeDrop(e, d.id)}
                >
                  <span className="pulse" aria-hidden />
                  <span className="node-meta">
                    <strong>{d.name}</strong>
                    <small>
                      {d.online ? 'Connected — click to open files' : 'Offline — wake this computer'}
                    </small>
                  </span>
                </button>
              </li>
            ))}
          </ul>
          {devices.length === 0 && (
            <p className="empty-rail">No computers yet. Add a Mac or PC so files have a home.</p>
          )}
          <Link to="/computers" className="card-link" style={{ marginTop: '0.75rem', padding: '0.85rem' }}>
            <strong style={{ fontSize: '0.95rem' }}>Add a computer</strong>
            <span>Join a home Mac or PC with a one-time code.</span>
          </Link>
          {dropHint && <div className="drop-hint">{dropHint}</div>}
        </aside>

        <section
          className="files-pane"
          onDragOver={(e) => e.preventDefault()}
          onDrop={onPaneDrop}
          onClick={() => setCtx(null)}
        >
          <div className="files-head">
            <div>
              <p className="page-kicker">{view === 'all' ? 'Library' : 'This computer'}</p>
              <h1 className="pane-title">{view === 'all' ? 'All files' : selectedDevice?.name || 'Pick a computer'}</h1>
              <nav className="crumbs" aria-label="Folder">
                <button type="button" className="crumb" onClick={() => setPath('')}>
                  {view === 'all' ? 'Everywhere' : 'Files'}
                </button>
                {path.split('/').filter(Boolean).map((seg, i, arr) => {
                  const p = arr.slice(0, i + 1).join('/')
                  return (
                    <span key={p}>
                      <span className="crumb-sep">/</span>
                      <button type="button" className="crumb" onClick={() => setPath(p)}>{seg}</button>
                    </span>
                  )
                })}
              </nav>
            </div>
            <div className="file-actions">
              {view === 'node' && (
                <>
                  <button type="button" className="secondary" disabled={!selectedDevice?.online} onClick={onMkdir}>New folder</button>
                  <button type="button" disabled={!selectedDevice?.online} onClick={() => void pickAndUpload()}>
                    Upload files
                  </button>
                </>
              )}
              {view === 'all' && (
                <>
                  <select
                    className="secondary"
                    value={nodeFilter}
                    onChange={(e) => setNodeFilter(e.target.value)}
                    aria-label="Filter by node"
                  >
                    <option value="">Every computer</option>
                    {devices.map((d) => (
                      <option key={d.id} value={d.id}>{d.name}</option>
                    ))}
                  </select>
                  <select
                    className="secondary"
                    value={typeFilter}
                    onChange={(e) => setTypeFilter(e.target.value)}
                    aria-label="Filter by type"
                  >
                    <option value="">All types</option>
                    <option value="image">Images</option>
                    <option value="video">Video</option>
                    <option value="pdf">PDF</option>
                    <option value="text">Text</option>
                  </select>
                </>
              )}
              <button type="button" className="secondary" onClick={() => void (async () => {
                if (view === 'all') {
                  try { await cl.filesReindex(nodeFilter || undefined) } catch { /* keep last index */ }
                }
                await refreshEntries()
              })()}>Refresh</button>
              <input
                ref={fileInputRef}
                type="file"
                multiple
                hidden
                onChange={(e) => {
                  if (e.target.files?.length) onLocalFiles(e.target.files)
                  e.target.value = ''
                }}
              />
            </div>
          </div>

          {!path && (
            <div className="root-shortcuts">
              {ROOTS.map((r) => (
                <button key={r} type="button" className="root-chip" onClick={() => setPath(r)}>
                  <span className="folder-glyph" aria-hidden />
                  {r.charAt(0).toUpperCase() + r.slice(1)}
                </button>
              ))}
            </div>
          )}

          {path && (
            <button type="button" className="up-link" onClick={() => setPath(parentPath(path))}>← Up</button>
          )}

          {error && <p className="banner-err">{error}</p>}
          {view === 'node' && selectedCompute && (
            <div className="metric-row">
              <div className="metric">
                <span className="lbl">Status</span>
                <span className="val">{selectedDevice?.online ? 'Connected' : 'Offline'}</span>
              </div>
              <div className="metric">
                <span className="lbl">Processor</span>
                <span className="val">{selectedCompute.cpu ? `${selectedCompute.cpu.cores} cores` : '—'}</span>
              </div>
              <div className="metric">
                <span className="lbl">Memory</span>
                <span className="val">{selectedCompute.memory?.total_bytes ? formatBytes(selectedCompute.memory.total_bytes) : '—'}</span>
              </div>
              <div className="metric">
                <span className="lbl">Free disk</span>
                <span className="val">{selectedCompute.disks?.length ? formatBytes(selectedCompute.disks.reduce((n, x) => n + x.free_bytes, 0)) : '—'}</span>
              </div>
            </div>
          )}
          {view === 'node' && selectedDevice && !selectedDevice.online && (
            <p className="banner-warn">This computer is asleep or offline. Wake it, then wait a few seconds.</p>
          )}
          {view === 'node' && selectedDevice?.online && (
            <div className="hint-row">
              <span className="hint-chip">Drop files here to upload</span>
              <span className="hint-chip">Drag a file onto another computer in the list to send it</span>
              <span className="hint-chip">Right-click a file for Open, Download, Send</span>
            </div>
          )}

          <div className="file-table">
            <div className="file-row head">
              <span>Name</span>
              {view === 'all' && <span>Node</span>}
              <span>Size</span>
              <span>Modified</span>
            </div>

            {view === 'node' && visibleNode.map((e) => (
              <div
                key={e.path}
                className={`file-row ${e.is_directory ? 'dir' : 'file'} ${selected.has(e.path) ? 'picked' : ''}`}
                draggable={!e.is_directory}
                onDragStart={(ev) => selectedId && startDrag(ev, e, selectedId)}
                onClick={(ev) => toggleSelect(e.path, ev.metaKey || ev.ctrlKey || ev.shiftKey)}
                onDoubleClick={() => (e.is_directory ? setPath(e.path) : selectedId && void onPreview(e, selectedId))}
                onContextMenu={(ev) => openCtx(ev, e)}
              >
                <span className="name-cell">
                  {e.is_directory ? (
                    <span className="folder-glyph" aria-hidden />
                  ) : thumbs[thumbKey(selectedId!, e.path)] ? (
                    <img src={thumbs[thumbKey(selectedId!, e.path)].url} alt="" className="file-thumb" />
                  ) : (
                    <span className="file-glyph" aria-hidden />
                  )}
                  {e.name}
                </span>
                <span className="mono">{e.is_directory ? '—' : formatBytes(e.size ?? 0)}</span>
                <span className="muted">{formatWhen(e.mtime)}</span>
              </div>
            ))}

            {view === 'all' && visibleAll.map((e) => (
              <div
                key={`${e.device_id}:${e.path}`}
                className={`file-row all ${e.is_directory ? 'dir' : 'file'} ${selected.has(`${e.device_id}:${e.path}`) ? 'picked' : ''}`}
                draggable={!e.is_directory}
                onDragStart={(ev) => startDrag(ev, e, e.device_id)}
                onClick={(ev) => toggleSelect(`${e.device_id}:${e.path}`, ev.metaKey || ev.ctrlKey || ev.shiftKey)}
                onDoubleClick={() => {
                  if (e.is_directory) {
                    setFilter('')
                    setSearchQ('')
                    setPath(e.path)
                  } else void onPreview(e, e.device_id)
                }}
                onContextMenu={(ev) => openCtx(ev, e)}
              >
                <span className="name-cell">
                  {e.is_directory ? (
                    <span className="folder-glyph" aria-hidden />
                  ) : thumbs[thumbKey(e.device_id, e.path)] ? (
                    <img src={thumbs[thumbKey(e.device_id, e.path)].url} alt="" className="file-thumb" />
                  ) : (
                    <span className="file-glyph" aria-hidden />
                  )}
                  {e.name}
                </span>
                <span className="muted">{e.device_name}</span>
                <span className="mono">{e.is_directory ? '—' : formatBytes(e.size ?? 0)}</span>
                <span className="muted">{formatWhen(e.mtime)}</span>
              </div>
            ))}

            {((view === 'node' && !visibleNode.length) || (view === 'all' && !visibleAll.length)) && (
              <div className="empty-state">
                {devices.length === 0 ? (
                  <>
                    <h2>Nothing to show yet</h2>
                    <p>Node is the panel. Files live on computers you add — a home Mac, a PC, or another server.</p>
                    <Link to="/computers"><button type="button">Add a computer</button></Link>
                  </>
                ) : view === 'all' ? (
                  <>
                    <h2>{searchQ ? 'No files match that search' : 'This folder is empty'}</h2>
                    <p>
                      {searchQ
                        ? 'Try another word, or open a computer on the left and browse its folders.'
                        : 'Open a computer on the left, or upload files after you pick one.'}
                    </p>
                  </>
                ) : (
                  <>
                    <h2>{path ? 'This folder is empty' : 'No files here yet'}</h2>
                    <p>
                      {selectedDevice?.online
                        ? 'Upload from this browser, drop files onto this page, or send a file from another computer.'
                        : 'This computer is offline, so Node cannot list its files until it comes back.'}
                    </p>
                    {selectedDevice?.online && (
                      <button type="button" onClick={() => void pickAndUpload()}>Upload files</button>
                    )}
                  </>
                )}
              </div>
            )}
          </div>
        </section>
      </div>

      <footer className="transfers-dock">
        <div className="dock-label">Transfers</div>
        {!activeQueue.length && !activeTransfers.length && (
          <p className="muted dock-empty">Uploads, downloads, and files sent between computers show up here.</p>
        )}
        <ul className="xfer-list">
          {activeQueue.map((q) => (
            <li key={q.id} className="xfer-item active">
              <div className="xfer-main">
                <strong>{q.label}</strong>
                <span className="muted">{q.kind} · {q.status}{q.eta ? ` · ETA ${q.eta}` : ''}{q.speed ? ` · ${formatBytes(q.speed)}/s` : ''}</span>
              </div>
              <div className="bar"><i style={{ width: `${q.percent}%` }} /></div>
              {q.status === 'error' && (
                <button type="button" className="tiny" onClick={() => retryQueue(q)}>Retry</button>
              )}
            </li>
          ))}
          {queue.filter((q) => q.status === 'done' || q.status === 'error').slice(-4).reverse().map((q) => (
            <li key={q.id} className={`xfer-item ${q.status}`}>
              <div className="xfer-main">
                <strong>{q.label}</strong>
                <span className="muted">{q.status}{q.error ? ` · ${q.error}` : ''}</span>
              </div>
            </li>
          ))}
          {activeTransfers.map((t) => (
            <li key={t.id} className="xfer-item active">
              <div className="xfer-main">
                <strong>{t.filename}</strong>
                <span className="muted">{t.status}</span>
              </div>
              <div className="bar">
                <i style={{ width: `${t.size ? Math.min(100, ((t.bytes_received || t.resume_offset) * 100) / t.size) : 0}%` }} />
              </div>
              <button type="button" className="tiny danger" onClick={() => void cl.abortTransfer(t.id).then(refreshTransfers)}>
                Cancel
              </button>
            </li>
          ))}
        </ul>
      </footer>

      {ctx && (
        <ul className="ctx-menu" style={{ left: ctx.x, top: ctx.y }} onClick={(e) => e.stopPropagation()}>
          {ctx.entries.length === 1 && !ctx.entries[0].is_directory && (
            <>
              <li><button type="button" onClick={() => { const e = ctx.entries[0]; const id = view === 'all' ? (e as AllFile).device_id : selectedId!; void onPreview(e, id); setCtx(null) }}>Open</button></li>
              <li><button type="button" onClick={() => { const e = ctx.entries[0]; const id = view === 'all' ? (e as AllFile).device_id : selectedId!; void onDownload(e, id); setCtx(null) }}>Download</button></li>
              <li><button type="button" onClick={() => { const e = ctx.entries[0]; const id = view === 'all' ? (e as AllFile).device_id : selectedId!; onSendToNode(e, id); setCtx(null) }}>Send to another computer…</button></li>
              <li><button type="button" onClick={() => { const e = ctx.entries[0]; const id = view === 'all' ? (e as AllFile).device_id : selectedId!; onCopy(e, id); setCtx(null) }}>Copy</button></li>
            </>
          )}
          {ctx.entries.length === 1 && (
            <li><button type="button" onClick={() => { const e = ctx.entries[0]; const id = view === 'all' ? (e as AllFile).device_id : selectedId!; onRename(e, id); setCtx(null) }}>Rename</button></li>
          )}
          <li><button type="button" className="danger" onClick={() => {
            const items = ctx.entries.map((e) => ({
              deviceId: view === 'all' ? (e as AllFile).device_id : selectedId!,
              path: e.path,
              name: e.name,
            }))
            onDeleteMany(items)
            setCtx(null)
          }}>Delete</button></li>
        </ul>
      )}

      {preview && (
        <div className="modal-back" onClick={() => { if (preview.url) URL.revokeObjectURL(preview.url); setPreview(null) }}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <div className="modal-head">
              <strong>{preview.name}</strong>
              <button type="button" className="ghost" onClick={() => { if (preview.url) URL.revokeObjectURL(preview.url); setPreview(null) }}>Close</button>
            </div>
            {preview.text !== undefined ? (
              <pre className="preview-text">{preview.text}</pre>
            ) : preview.mime.startsWith('image/') && preview.url ? (
              <img src={preview.url} alt={preview.name} className="preview-img" />
            ) : preview.mime.startsWith('text/') && preview.url ? (
              <iframe title="preview" src={preview.url} className="preview-frame" />
            ) : preview.generic ? (
              <p className="muted">Generic file preview only. Metadata stays available in the file list; the original file is unchanged.</p>
            ) : (
              <p className="muted">
                No inline preview. {preview.url ? <a href={preview.url} download={preview.name}>Download</a> : null}
              </p>
            )}
          </div>
        </div>
      )}

      {conflictAsk && (
        <div className="modal-back">
          <div className="modal conflict" onClick={(e) => e.stopPropagation()}>
            <h2>Same name already exists</h2>
            <p className="muted">A file with this name is already in that folder. What should Node do?</p>
            <div className="row">
              <button type="button" onClick={() => {
                const ask = conflictAsk
                setConflictAsk(null)
                void uploadFiles(ask.files, ask.deviceId, ask.basePath, 'overwrite')
              }}>Overwrite</button>
              <button type="button" className="secondary" onClick={() => {
                const ask = conflictAsk
                setConflictAsk(null)
                void uploadFiles(ask.files, ask.deviceId, ask.basePath, 'rename')
              }}>Keep both</button>
              <button type="button" className="ghost" onClick={() => setConflictAsk(null)}>Cancel</button>
            </div>
          </div>
        </div>
      )}

      <div className="toasts">
        {toasts.map((t) => (
          <div key={t.id} className={`toast ${t.tone}`}>{t.text}</div>
        ))}
      </div>
    </Chrome>
  )
}
