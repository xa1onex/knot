import { useCallback, useEffect, useState } from 'react'
import type { ComputeDevice, ComputeJob } from '@node-infra/client'
import { createClient } from '../lib/client'
import { formatAgo, formatBytes } from '../lib/format'
import PageHeader from '../components/PageHeader'

function gpuLabel(d: ComputeDevice): string {
  if (d.gpu === null || d.gpu === undefined) return 'unavailable'
  if (d.gpu.length === 0) return 'none'
  return d.gpu
    .map((g) => {
      const vram = g.vram_bytes ? ` (${formatBytes(g.vram_bytes)})` : ''
      return `${g.vendor} ${g.model}${vram}`.trim()
    })
    .join(', ')
}

function diskFree(d: ComputeDevice): string {
  if (!d.disks?.length) return '—'
  const free = d.disks.reduce((n, x) => n + (x.free_bytes || 0), 0)
  const total = d.disks.reduce((n, x) => n + (x.total_bytes || 0), 0)
  return `${formatBytes(free)} free / ${formatBytes(total)}`
}

function statusClass(status: string): string {
  if (status === 'available') return 'online'
  if (status === 'stale') return 'stale'
  return 'offline'
}

function statusDot(status: string): string {
  if (status === 'available') return '🟢'
  if (status === 'stale') return '🟡'
  return '⚪'
}

export default function Compute() {
  const cl = createClient()
  const [list, setList] = useState<ComputeDevice[]>([])
  const [jobs, setJobs] = useState<ComputeJob[]>([])
  const [error, setError] = useState('')

  const refresh = useCallback(async () => {
    try {
      const [devs, jobList] = await Promise.all([cl.listComputeDevices(), cl.listJobs()])
      setList(devs)
      setJobs(jobList)
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load compute')
    }
  }, [cl])

  useEffect(() => {
    void refresh()
    const t = setInterval(() => void refresh().catch(() => {}), 8000)
    return () => clearInterval(t)
  }, [refresh])

  return (
    <div>
      <PageHeader
        kicker="Hardware"
        title="What each computer can do"
        description="CPU, memory, and disk from the last time that computer checked in. Jobs below are one-off tasks Node ran on a machine."
      />
      {error && <div className="error">{error}</div>}

      {list.map((d) => (
        <div key={d.device_id} className="panel" style={{ marginTop: '1.25rem' }}>
          <div className="row" style={{ justifyContent: 'space-between' }}>
            <h2 style={{ fontSize: '1.15rem', margin: 0 }}>{d.name}</h2>
            <span className={`badge ${statusClass(d.status)}`}>
              {statusDot(d.status)} {d.status}
            </span>
          </div>
          <p className="muted" style={{ marginTop: '0.35rem' }}>
            {d.os}/{d.arch} · agent {d.agent_version || '—'} · Last telemetry: {formatAgo(d.last_telemetry_at)}
          </p>
          <dl className="compute-dl">
            <div><dt>CPU</dt><dd>{d.cpu ? `${d.cpu.cores} cores · ${d.cpu.architecture}${d.cpu.usage_percent != null ? ` · ${Math.round(d.cpu.usage_percent)}%` : ''}` : '—'}</dd></div>
            <div><dt>RAM</dt><dd>{d.memory?.total_bytes ? `${formatBytes(d.memory.total_bytes)} · ${formatBytes(d.memory.available_bytes || 0)} available` : '—'}</dd></div>
            <div><dt>GPU</dt><dd>{gpuLabel(d)}</dd></div>
            <div><dt>Storage</dt><dd>{diskFree(d)}</dd></div>
            {d.labels && Object.keys(d.labels).length > 0 && (
              <div><dt>Labels</dt><dd>{Object.entries(d.labels).map(([k, v]) => `${k}=${v}`).join(' ')}</dd></div>
            )}
          </dl>
          {d.disks?.length > 1 && (
            <ul className="muted" style={{ marginTop: '0.75rem' }}>
              {d.disks.map((disk) => (
                <li key={disk.mount}>
                  {disk.mount} — {formatBytes(disk.free_bytes)} free / {formatBytes(disk.total_bytes)}
                </li>
              ))}
            </ul>
          )}
        </div>
      ))}
      {list.length === 0 && !error && (
        <div className="empty-state">
          <h2>No hardware reports yet</h2>
          <p>Add a computer and wait a few seconds. It will show CPU, memory, and free disk here.</p>
        </div>
      )}

      <h2 style={{ fontSize: '1.2rem', marginTop: '2rem' }}>Jobs</h2>
      <p className="muted">One-off tasks Node ran (or will run) on a computer.</p>
      {jobs.length === 0 && <p className="muted">No jobs yet — that is normal if you only use Files.</p>}
      {jobs.map((j) => (
        <div key={j.id} className="panel" style={{ marginTop: '0.75rem' }}>
          <div className="row" style={{ justifyContent: 'space-between' }}>
            <strong>{j.image}</strong>
            <span className={`badge ${j.status === 'succeeded' || j.status === 'artifacts_committed' ? 'online' : j.status === 'running' || j.status === 'queued' || j.status === 'assigned' || j.status === 'waiting_for_resource' ? 'stale' : 'offline'}`}>
              {j.status}
            </span>
          </div>
          <p className="muted" style={{ marginTop: '0.35rem' }}>
            {j.device_name || j.device_id || 'unassigned'} · {j.placement || 'pinned'} · cpu {j.resources?.cpu ?? '—'} · ram {j.resources?.memory_mb ?? '—'} MB
            {j.reason ? ` · ${j.reason}` : ''}
            {j.output_path ? ` · ${j.output_path}` : ''}
          </p>
        </div>
      ))}
    </div>
  )
}
