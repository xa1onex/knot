import { useCallback, useEffect, useMemo, useState } from 'react'
import type { Device, SyncConflict, SyncJob } from '@node-infra/client'
import { createClient } from '../lib/client'
import PageHeader from '../components/PageHeader'

function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / (1024 * 1024)).toFixed(1)} MB`
}

function fmtTime(iso: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString(undefined, {
    month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  })
}

function joinPath(root: string, rel: string): string {
  const a = (root || '').replace(/\/+$/, '')
  const b = (rel || '').replace(/^\/+/, '')
  if (!a) return b
  if (!b) return a
  return `${a}/${b}`
}

function isProbablyText(path: string, mime?: string): boolean {
  if (mime && (mime.startsWith('text/') || mime.includes('json') || mime.includes('xml') || mime.includes('javascript'))) {
    return true
  }
  return /\.(txt|md|json|csv|ya?ml|toml|xml|html?|css|js|ts|tsx|jsx|go|rs|py|sh|env|ini|conf|log)$/i.test(path)
}

function simpleDiff(a: string, b: string): string[] {
  const al = a.split(/\r?\n/)
  const bl = b.split(/\r?\n/)
  const max = Math.max(al.length, bl.length)
  const out: string[] = []
  for (let i = 0; i < max; i++) {
    const L = al[i]
    const R = bl[i]
    if (L === R) {
      if (L !== undefined) out.push(`  ${L}`)
    } else {
      if (L !== undefined) out.push(`- ${L}`)
      if (R !== undefined) out.push(`+ ${R}`)
    }
  }
  return out.slice(0, 80)
}

export default function Sync() {
  const cl = createClient()
  const [jobs, setJobs] = useState<SyncJob[]>([])
  const [devices, setDevices] = useState<Device[]>([])
  const [conflicts, setConflicts] = useState<Record<string, SyncConflict[]>>({})
  const [error, setError] = useState('')
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [fromPath, setFromPath] = useState('projects')
  const [toPath, setToPath] = useState('projects')
  const [name, setName] = useState('')
  const [mode, setMode] = useState<'one_way' | 'two_way'>('two_way')
  const [selected, setSelected] = useState<Record<string, boolean>>({})
  const [busy, setBusy] = useState('')
  const [showResolved, setShowResolved] = useState(false)
  const [preview, setPreview] = useState<Record<string, { a?: string; b?: string; binary?: boolean; err?: string }>>({})

  const refresh = useCallback(async () => {
    try {
      const [j, d] = await Promise.all([cl.listSyncJobs(), cl.listDevices()])
      setJobs(j)
      setDevices(d.filter((x) => !x.revoked_at))
      const cmap: Record<string, SyncConflict[]> = {}
      await Promise.all(
        j.filter((job) => job.mode === 'two_way').map(async (job) => {
          try {
            cmap[job.id] = await cl.listSyncConflicts(job.id, { openOnly: false })
          } catch { /* */ }
        }),
      )
      setConflicts(cmap)
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed')
    }
  }, [cl])

  useEffect(() => {
    void refresh()
    const id = setInterval(() => void refresh(), 2500)
    return () => clearInterval(id)
  }, [refresh])

  const openConflicts = useMemo(() => {
    const list: { job: SyncJob; conflict: SyncConflict }[] = []
    for (const j of jobs) {
      for (const c of conflicts[j.id] || []) {
        if (c.status === 'open') list.push({ job: j, conflict: c })
      }
    }
    return list
  }, [jobs, conflicts])

  const resolvedConflicts = useMemo(() => {
    const list: SyncConflict[] = []
    for (const j of jobs) {
      for (const c of conflicts[j.id] || []) {
        if (c.status === 'resolved') list.push(c)
      }
    }
    return list
  }, [jobs, conflicts])

  const openCount = openConflicts.length

  async function onCreate() {
    try {
      await cl.createSyncJob({
        name: name || undefined,
        mode,
        source_device_id: from,
        source_path: fromPath,
        dest_device_id: to,
        dest_path: toPath,
      })
      setName('')
      await refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Create failed')
    }
  }

  async function resolveOne(id: string, resolution: 'keep_a' | 'keep_b' | 'keep_both') {
    setBusy(id)
    try {
      await cl.resolveSyncConflict(id, resolution)
      setSelected((s) => {
        const n = { ...s }
        delete n[id]
        return n
      })
      await refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Resolve failed')
    } finally {
      setBusy('')
    }
  }

  async function resolveBatch(resolution: 'keep_a' | 'keep_b' | 'keep_both') {
    const ids = Object.keys(selected).filter((k) => selected[k])
    if (!ids.length) return
    setBusy('batch')
    try {
      await cl.batchResolveSyncConflicts(ids, resolution)
      setSelected({})
      await refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Batch resolve failed')
    } finally {
      setBusy('')
    }
  }

  async function loadPreview(c: SyncConflict) {
    if (preview[c.id]) return
    const aDev = c.a_device_id
    const bDev = c.b_device_id
    const aPath = joinPath(c.a_root || '', c.rel_path)
    const bPath = joinPath(c.b_root || '', c.rel_path)
    try {
      let aText = ''
      let bText = ''
      let binary = false
      if (c.a_exists && aDev) {
        const { data, contentType } = await cl.storageContent(aDev, aPath)
        if (!isProbablyText(c.rel_path, contentType) || data.size > 200_000) {
          binary = true
        } else {
          aText = await data.text()
        }
      }
      if (!binary && c.b_exists && bDev) {
        const { data, contentType } = await cl.storageContent(bDev, bPath)
        if (!isProbablyText(c.rel_path, contentType) || data.size > 200_000) {
          binary = true
        } else {
          bText = await data.text()
        }
      }
      setPreview((p) => ({ ...p, [c.id]: binary ? { binary: true } : { a: aText, b: bText } }))
    } catch (e) {
      setPreview((p) => ({
        ...p,
        [c.id]: { err: e instanceof Error ? e.message : 'preview failed', binary: true },
      }))
    }
  }

  const selectedCount = Object.values(selected).filter(Boolean).length

  return (
    <div>
      <PageHeader
        kicker="Folders"
        title="Keep two folders in step"
        description="Pick a folder on computer A and the same idea of a folder on computer B. Two-way means both sides can change; if they disagree, Node asks you instead of overwriting."
      />
      {error ? <p className="error">{error}</p> : null}

      {openCount > 0 ? (
        <div className="panel" style={{ marginTop: '1.25rem', borderColor: '#c4a574' }}>
          <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 12, flexWrap: 'wrap' }}>
            <h2 style={{ fontSize: '1.1rem', margin: 0 }}>
              Conflicts
              <span className="muted" style={{ fontWeight: 400 }}> · {openCount} open</span>
            </h2>
            {selectedCount > 0 ? (
              <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                <button type="button" className="secondary" disabled={!!busy} onClick={() => void resolveBatch('keep_a')}>
                  Keep A ({selectedCount})
                </button>
                <button type="button" className="secondary" disabled={!!busy} onClick={() => void resolveBatch('keep_b')}>
                  Keep B ({selectedCount})
                </button>
                <button type="button" className="secondary" disabled={!!busy} onClick={() => void resolveBatch('keep_both')}>
                  Keep Both ({selectedCount})
                </button>
              </div>
            ) : null}
          </div>

          <ul style={{ listStyle: 'none', padding: 0, margin: '0.75rem 0 0' }}>
            {openConflicts.map(({ conflict: c }) => {
              const aName = c.a_device_name || 'Node A'
              const bName = c.b_device_name || 'Node B'
              const pv = preview[c.id]
              return (
                <li
                  key={c.id}
                  style={{
                    borderTop: '1px solid #e5dcc8',
                    padding: '1rem 0',
                  }}
                >
                  <div style={{ display: 'flex', gap: 10, alignItems: 'flex-start' }}>
                    <input
                      type="checkbox"
                      checked={!!selected[c.id]}
                      onChange={(e) => setSelected((s) => ({ ...s, [c.id]: e.target.checked }))}
                      style={{ marginTop: 4 }}
                      aria-label={`Select ${c.rel_path}`}
                    />
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <div style={{ fontWeight: 600, wordBreak: 'break-all' }}>{c.rel_path}</div>
                      <div
                        style={{
                          display: 'grid',
                          gridTemplateColumns: '1fr 1fr',
                          gap: '0.75rem',
                          marginTop: 10,
                        }}
                      >
                        <div style={{ background: '#f7f3ea', borderRadius: 8, padding: '0.75rem' }}>
                          <div style={{ fontWeight: 600 }}>{aName}</div>
                          <div className="muted" style={{ fontSize: 13, marginTop: 4 }}>
                            {c.a_exists ? (
                              <>
                                {fmtTime(c.a_mtime)} · {fmtBytes(c.a_size)}
                                <div>version A · {c.a_sha256.slice(0, 8)}</div>
                              </>
                            ) : c.a_deleted ? (
                              'deleted'
                            ) : (
                              'missing'
                            )}
                          </div>
                        </div>
                        <div style={{ background: '#f7f3ea', borderRadius: 8, padding: '0.75rem' }}>
                          <div style={{ fontWeight: 600 }}>{bName}</div>
                          <div className="muted" style={{ fontSize: 13, marginTop: 4 }}>
                            {c.b_exists ? (
                              <>
                                {fmtTime(c.b_mtime)} · {fmtBytes(c.b_size)}
                                <div>version B · {c.b_sha256.slice(0, 8)}</div>
                              </>
                            ) : c.b_deleted ? (
                              'deleted'
                            ) : (
                              'missing'
                            )}
                          </div>
                        </div>
                      </div>

                      <div style={{ display: 'flex', gap: 8, marginTop: 10, flexWrap: 'wrap' }}>
                        <button type="button" disabled={busy === c.id} onClick={() => void resolveOne(c.id, 'keep_a')}>
                          Keep {aName}
                        </button>
                        <button type="button" disabled={busy === c.id} onClick={() => void resolveOne(c.id, 'keep_b')}>
                          Keep {bName}
                        </button>
                        <button
                          type="button"
                          className="secondary"
                          disabled={busy === c.id}
                          onClick={() => void resolveOne(c.id, 'keep_both')}
                          title={c.keep_both_suggested_name || 'Save B under a conflict copy name'}
                        >
                          Keep Both
                        </button>
                        <button type="button" className="secondary" onClick={() => void loadPreview(c)}>
                          {pv ? 'Preview loaded' : 'Compare versions'}
                        </button>
                      </div>
                      {c.keep_both_suggested_name ? (
                        <div className="muted" style={{ fontSize: 12, marginTop: 6 }}>
                          Keep Both saves B as <code>{c.keep_both_suggested_name}</code>
                        </div>
                      ) : null}

                      {pv ? (
                        <div style={{ marginTop: 10, fontSize: 12 }}>
                          {pv.err ? <p className="error">{pv.err}</p> : null}
                          {pv.binary ? (
                            <p className="muted">Binary file — choose Keep {aName}, Keep {bName}, or Keep Both. No merge.</p>
                          ) : (
                            <pre
                              style={{
                                margin: 0,
                                padding: 10,
                                background: '#1e1e1e',
                                color: '#d4d4d4',
                                borderRadius: 8,
                                overflow: 'auto',
                                maxHeight: 240,
                                whiteSpace: 'pre-wrap',
                              }}
                            >
                              {simpleDiff(pv.a || '', pv.b || '').join('\n') || '(identical text)'}
                            </pre>
                          )}
                        </div>
                      ) : null}
                    </div>
                  </div>
                </li>
              )
            })}
          </ul>
        </div>
      ) : null}

      <div className="panel" style={{ marginTop: '1.25rem' }}>
        <h2 style={{ fontSize: '1.15rem', margin: 0 }}>Start a sync</h2>
        <p className="muted" style={{ marginTop: '0.4rem' }}>Choose two computers and the folder path on each. You can run it whenever you want.</p>
        <div style={{ display: 'grid', gap: '0.6rem', marginTop: '0.75rem', maxWidth: 520 }}>
          <label className="muted">
            Mode
            <select value={mode} onChange={(e) => setMode(e.target.value as 'one_way' | 'two_way')} style={{ display: 'block', width: '100%', marginTop: 4 }}>
              <option value="two_way">Both ways — either computer can change files</option>
              <option value="one_way">One way — A copies into B only</option>
            </select>
          </label>
          <label className="muted">
            Name
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder="optional" style={{ display: 'block', width: '100%', marginTop: 4 }} />
          </label>
          <label className="muted">
            Computer A
            <select value={from} onChange={(e) => setFrom(e.target.value)} style={{ display: 'block', width: '100%', marginTop: 4 }}>
              <option value="">—</option>
              {devices.map((d) => (
                <option key={d.id} value={d.id}>{d.name}{d.online ? '' : ' (offline)'}</option>
              ))}
            </select>
          </label>
          <label className="muted">
            Folder on A
            <input value={fromPath} onChange={(e) => setFromPath(e.target.value)} style={{ display: 'block', width: '100%', marginTop: 4 }} />
          </label>
          <label className="muted">
            Computer B
            <select value={to} onChange={(e) => setTo(e.target.value)} style={{ display: 'block', width: '100%', marginTop: 4 }}>
              <option value="">—</option>
              {devices.map((d) => (
                <option key={d.id} value={d.id}>{d.name}{d.online ? '' : ' (offline)'}</option>
              ))}
            </select>
          </label>
          <label className="muted">
            Folder on B
            <input value={toPath} onChange={(e) => setToPath(e.target.value)} style={{ display: 'block', width: '100%', marginTop: 4 }} />
          </label>
          <button type="button" disabled={!from || !to || !fromPath || !toPath} onClick={() => void onCreate()}>
            Start sync
          </button>
        </div>
      </div>

      <div className="panel" style={{ marginTop: '1.25rem' }}>
        <h2 style={{ fontSize: '1.15rem', margin: 0 }}>Running syncs</h2>
        {!jobs.length ? (
          <p className="muted" style={{ marginTop: '0.75rem' }}>None yet. Start one above after you have two computers.</p>
        ) : (
          <ul style={{ listStyle: 'none', padding: 0, marginTop: '0.75rem' }}>
            {jobs.map((j) => (
              <li key={j.id} style={{ borderTop: '1px solid #ddd', padding: '0.75rem 0' }}>
                <strong>{j.name || j.id.slice(0, 8)}</strong>
                <span className="muted"> · {j.mode}</span>
                <div className="muted" style={{ fontSize: 13 }}>
                  {j.source_path} {j.mode === 'two_way' ? '↔' : '→'} {j.dest_path} · {j.status}
                  {j.files_total ? ` · ${j.files_done}/${j.files_total} files` : ''}
                  {(j.conflicts_open ?? 0) > 0 ? ` · ${j.conflicts_open} conflicts` : ''}
                  {j.last_error ? ` · ${j.last_error}` : ''}
                </div>
                <div style={{ display: 'flex', gap: 8, marginTop: 8, flexWrap: 'wrap' }}>
                  <button type="button" className="secondary" disabled={j.status === 'running'} onClick={() => void cl.runSyncJob(j.id).then(refresh)}>
                    Run
                  </button>
                  <button type="button" className="secondary" disabled={j.status !== 'running' && j.status !== 'canceling'} onClick={() => void cl.cancelSyncJob(j.id).then(refresh)}>
                    Cancel
                  </button>
                  <button type="button" className="secondary" onClick={() => void cl.deleteSyncJob(j.id).then(refresh)}>
                    Delete
                  </button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>

      {resolvedConflicts.length > 0 ? (
        <div className="panel" style={{ marginTop: '1.25rem' }}>
          <button type="button" className="secondary" onClick={() => setShowResolved((v) => !v)}>
            {showResolved ? 'Hide' : 'Show'} resolved ({resolvedConflicts.length})
          </button>
          {showResolved ? (
            <ul style={{ listStyle: 'none', padding: 0, marginTop: 12 }}>
              {resolvedConflicts.map((c) => (
                <li key={c.id} className="muted" style={{ fontSize: 13, padding: '4px 0' }}>
                  {c.rel_path} · {c.resolution} · {fmtTime(c.resolved_at || c.created_at)}
                </li>
              ))}
            </ul>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}
