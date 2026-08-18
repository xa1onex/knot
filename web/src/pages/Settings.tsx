import { useEffect, useState } from 'react'
import { api } from '../api'

type UpdateComponentStatus = {
  component: string
  current_ref?: string
  latest_ref?: string
  available: boolean
  can_apply: boolean
  source_dir?: string
  current_build?: string
  error?: string
}

type DeviceUpdateStatus = {
  device_id: string
  name: string
  online: boolean
  status?: UpdateComponentStatus
  error?: string
}

type FleetUpdateStatus = {
  control_plane: UpdateComponentStatus
  devices: DeviceUpdateStatus[]
}

export default function Settings() {
  const desktop = typeof window !== 'undefined' ? window.nodeDesktop : undefined
  const [apiUrl, setApiUrl] = useState(
    import.meta.env.VITE_API_URL || (desktop ? '…' : 'same-origin via Vite proxy (/v1 → knotd)'),
  )
  const [draft, setDraft] = useState('')
  const [saving, setSaving] = useState(false)
  const [msg, setMsg] = useState('')
  const [updates, setUpdates] = useState<FleetUpdateStatus | null>(null)
  const [updateMsg, setUpdateMsg] = useState('')
  const [updating, setUpdating] = useState<string>('')

  useEffect(() => {
    if (!desktop) return
    void desktop.getApiUrl().then((u) => {
      setApiUrl(u)
      setDraft(u)
    })
  }, [desktop])

  useEffect(() => {
    api<FleetUpdateStatus>('/v1/system/update')
      .then(setUpdates)
      .catch((e) => setUpdateMsg(e instanceof Error ? e.message : 'Failed to load updates'))
  }, [])

  async function saveApiUrl() {
    if (!desktop) return
    setSaving(true)
    setMsg('')
    try {
      const next = await desktop.setApiUrl(draft.trim())
      setApiUrl(next)
      setDraft(next)
      setMsg('Saved. Reloading against the new Control Plane…')
    } catch (e) {
      setMsg(e instanceof Error ? e.message : 'Failed to save')
    } finally {
      setSaving(false)
    }
  }

  async function refreshUpdates() {
    setUpdateMsg('')
    try {
      setUpdates(await api<FleetUpdateStatus>('/v1/system/update'))
    } catch (e) {
      setUpdateMsg(e instanceof Error ? e.message : 'Failed to load updates')
    }
  }

  async function applyControlPlane() {
    setUpdating('control-plane')
    setUpdateMsg('')
    try {
      await api('/v1/system/update/control-plane', { method: 'POST' })
      setUpdateMsg('Control plane update started. The panel may reload during restart.')
      await refreshUpdates()
    } catch (e) {
      setUpdateMsg(e instanceof Error ? e.message : 'Update failed')
    } finally {
      setUpdating('')
    }
  }

  async function applyDevice(deviceID: string) {
    setUpdating(deviceID)
    setUpdateMsg('')
    try {
      await api(`/v1/system/update/devices/${deviceID}`, { method: 'POST' })
      setUpdateMsg('Device update started.')
      await refreshUpdates()
    } catch (e) {
      setUpdateMsg(e instanceof Error ? e.message : 'Update failed')
    } finally {
      setUpdating('')
    }
  }

  return (
    <div>
      <h1>Settings</h1>
      <p className="muted">Account and control-plane details. Day-to-day file work lives on the main Files screen.</p>
      <div className="panel" style={{ marginTop: '1.25rem' }}>
        <div className="muted">API URL</div>
        {desktop ? (
          <>
            <input
              className="mono"
              style={{ marginTop: '0.4rem', width: '100%', maxWidth: 480, padding: '0.5rem' }}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              placeholder="http://127.0.0.1:8787"
            />
            <div style={{ marginTop: '0.75rem', display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
              <button type="button" disabled={saving || draft.trim() === apiUrl} onClick={() => void saveApiUrl()}>
                Save
              </button>
              {msg && <span className="muted">{msg}</span>}
            </div>
            <p className="muted" style={{ marginTop: '0.75rem' }}>
              Desktop proxies <code>/v1</code> to this Control Plane. Also: menu → Control Plane URL… or{' '}
              <code>~/.node-desktop.json</code>.
            </p>
          </>
        ) : (
          <div className="mono" style={{ marginTop: '0.4rem' }}>{apiUrl}</div>
        )}
        <p className="muted" style={{ marginTop: '1rem' }}>
          Product: <strong>Node</strong>. CLI / repo codename: <strong>knot</strong>.
          {desktop ? ` · Desktop (${desktop.platform})` : ''}
        </p>
      </div>
      <div className="panel" style={{ marginTop: '1.25rem' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', gap: '1rem', alignItems: 'center' }}>
          <div>
            <div className="muted">Self-update</div>
            <p className="muted" style={{ marginTop: '0.4rem' }}>Checks the repo and lets Node update itself when a newer commit exists.</p>
          </div>
          <button type="button" className="ghost" onClick={() => void refreshUpdates()}>Refresh</button>
        </div>
        {updateMsg && <div className="muted" style={{ marginTop: '0.75rem' }}>{updateMsg}</div>}
        {updates && (
          <>
            <div style={{ marginTop: '1rem' }}>
              <strong>Main Node</strong>
              <div className="muted" style={{ marginTop: '0.35rem' }}>
                Current: <span className="mono">{updates.control_plane.current_ref || 'unknown'}</span>
                {' · '}Latest: <span className="mono">{updates.control_plane.latest_ref || 'unknown'}</span>
              </div>
              {updates.control_plane.error && <div className="error" style={{ marginTop: '0.5rem' }}>{updates.control_plane.error}</div>}
              <div style={{ marginTop: '0.75rem' }}>
                <button
                  type="button"
                  disabled={!updates.control_plane.available || !updates.control_plane.can_apply || updating === 'control-plane'}
                  onClick={() => void applyControlPlane()}
                >
                  {updating === 'control-plane' ? 'Updating…' : updates.control_plane.available ? 'Update Main Node' : 'Up to date'}
                </button>
              </div>
            </div>
            <div style={{ marginTop: '1.25rem' }}>
              <strong>Devices</strong>
              <div style={{ marginTop: '0.75rem', display: 'grid', gap: '0.75rem' }}>
                {updates.devices.map((d) => (
                  <div key={d.device_id} style={{ borderTop: '1px solid var(--border)', paddingTop: '0.75rem' }}>
                    <div><strong>{d.name}</strong> <span className="muted">({d.online ? 'online' : 'offline'})</span></div>
                    <div className="muted" style={{ marginTop: '0.35rem' }}>
                      Current: <span className="mono">{d.status?.current_ref || 'unknown'}</span>
                      {' · '}Latest: <span className="mono">{d.status?.latest_ref || 'unknown'}</span>
                    </div>
                    {(d.error || d.status?.error) && <div className="error" style={{ marginTop: '0.5rem' }}>{d.error || d.status?.error}</div>}
                    <div style={{ marginTop: '0.75rem' }}>
                      <button
                        type="button"
                        disabled={!d.status?.available || !d.status?.can_apply || updating === d.device_id}
                        onClick={() => void applyDevice(d.device_id)}
                      >
                        {updating === d.device_id ? 'Updating…' : d.status?.available ? 'Update Device' : 'Up to date'}
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </>
        )}
      </div>
    </div>
  )
}
