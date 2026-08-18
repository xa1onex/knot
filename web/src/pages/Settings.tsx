import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import PageHeader from '../components/PageHeader'

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

function versionLine(st?: UpdateComponentStatus) {
  const cur = st?.current_ref
  const latest = st?.latest_ref
  if (!cur && !latest) return 'Version not reported yet'
  if (st?.available) return `A newer version is ready (${latest})`
  return `On the latest version (${cur})`
}

export default function Settings() {
  const desktop = typeof window !== 'undefined' ? window.nodeDesktop : undefined
  const [apiUrl, setApiUrl] = useState(
    import.meta.env.VITE_API_URL || (desktop ? '…' : window.location.origin),
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
      .catch((e) => setUpdateMsg(e instanceof Error ? e.message : 'Could not check for updates'))
  }, [])

  async function saveApiUrl() {
    if (!desktop) return
    setSaving(true)
    setMsg('')
    try {
      const next = await desktop.setApiUrl(draft.trim())
      setApiUrl(next)
      setDraft(next)
      setMsg('Saved. Reloading against the new server…')
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
      setUpdateMsg(e instanceof Error ? e.message : 'Could not check for updates')
    }
  }

  async function applyControlPlane() {
    setUpdating('control-plane')
    setUpdateMsg('')
    try {
      await api('/v1/system/update/control-plane', { method: 'POST' })
      setUpdateMsg('Updating the panel. This page may reload in a few seconds.')
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
      setUpdateMsg('Updating that computer. It may disconnect for a moment.')
      await refreshUpdates()
    } catch (e) {
      setUpdateMsg(e instanceof Error ? e.message : 'Update failed')
    } finally {
      setUpdating('')
    }
  }

  const cp = updates?.control_plane

  return (
    <div>
      <PageHeader
        kicker="Settings"
        title="Keep Node in good shape"
        description="Update the panel, add computers, or open extras like folder sync and API keys. Day-to-day file work stays on Files."
      />

      <div className="card-grid">
        <Link to="/computers" className="card-link">
          <strong>Computers</strong>
          <span>See who is online and add a home Mac or PC with a join code.</span>
          <div className="go">Open →</div>
        </Link>
        <Link to="/settings/sync" className="card-link">
          <strong>Folder sync</strong>
          <span>Keep the same folder in step on two computers. You choose if both copies change.</span>
          <div className="go">Open →</div>
        </Link>
        <Link to="/settings/services" className="card-link">
          <strong>Websites</strong>
          <span>Point a hostname at an app running on one of your computers.</span>
          <div className="go">Open →</div>
        </Link>
        <Link to="/settings/credentials" className="card-link">
          <strong>API keys</strong>
          <span>For the command line or another app. Not needed to use this panel.</span>
          <div className="go">Open →</div>
        </Link>
      </div>

      <div className="panel">
        <div className="row" style={{ justifyContent: 'space-between' }}>
          <div>
            <p className="page-kicker" style={{ marginBottom: '0.35rem' }}>Software</p>
            <h2 style={{ fontSize: '1.25rem' }}>Updates</h2>
            <p className="muted" style={{ marginTop: '0.35rem' }}>
              Node checks GitHub for a newer version. Update the panel (this website) separately from each computer’s agent.
            </p>
          </div>
          <button type="button" className="ghost" onClick={() => void refreshUpdates()}>Check again</button>
        </div>
        {updateMsg && <p className="help-note">{updateMsg}</p>}
        {updates && (
          <>
            <div className="update-row">
              <div>
                <h3>This panel (Main Node)</h3>
                <p className="muted version-meta">{versionLine(cp)}</p>
                {cp?.error && <div className="error">{cp.error}</div>}
              </div>
              <div>
                {cp?.available ? (
                  <button
                    type="button"
                    disabled={!cp.can_apply || updating === 'control-plane'}
                    onClick={() => void applyControlPlane()}
                  >
                    {updating === 'control-plane' ? 'Updating…' : 'Update panel'}
                  </button>
                ) : (
                  <span className="status-pill ready">Up to date</span>
                )}
              </div>
            </div>
            <div>
              <h3 style={{ fontSize: '1.05rem', marginTop: '0.5rem' }}>Computers</h3>
              {!updates.devices.length && (
                <p className="muted" style={{ marginTop: '0.5rem' }}>
                  No extra computers yet. <Link to="/computers">Add one</Link> to update agents from here.
                </p>
              )}
              {updates.devices.map((d) => (
                <div key={d.device_id} className="update-row">
                  <div>
                    <h3>{d.name}</h3>
                    <p className="muted version-meta">
                      {d.online ? 'Connected' : 'Offline — turn it on to update'} · {versionLine(d.status)}
                    </p>
                    {(d.error || d.status?.error) && <div className="error">{d.error || d.status?.error}</div>}
                  </div>
                  <div>
                    {d.status?.available && d.online ? (
                      <button
                        type="button"
                        disabled={!d.status.can_apply || updating === d.device_id}
                        onClick={() => void applyDevice(d.device_id)}
                      >
                        {updating === d.device_id ? 'Updating…' : 'Update this computer'}
                      </button>
                    ) : (
                      <span className={`status-pill ${d.online ? 'ready' : 'offline'}`}>
                        {d.online ? (d.status?.available ? 'Update ready' : 'Up to date') : 'Offline'}
                      </span>
                    )}
                  </div>
                </div>
              ))}
            </div>
          </>
        )}
      </div>

      <div className="panel" style={{ marginTop: '1.25rem' }}>
        <p className="page-kicker" style={{ marginBottom: '0.35rem' }}>Connection</p>
        <h2 style={{ fontSize: '1.15rem' }}>Server address</h2>
        {desktop ? (
          <>
            <p className="muted">The desktop app talks to this URL. Change it only if you moved the Main Node.</p>
            <input
              className="mono"
              style={{ marginTop: '0.6rem', maxWidth: 480 }}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              placeholder="https://your-server:4443"
            />
            <div className="row" style={{ marginTop: '0.75rem' }}>
              <button type="button" disabled={saving || draft.trim() === apiUrl} onClick={() => void saveApiUrl()}>
                Save
              </button>
              {msg && <span className="muted">{msg}</span>}
            </div>
          </>
        ) : (
          <p className="muted" style={{ marginTop: '0.4rem' }}>
            You are already on this panel: <span className="mono">{apiUrl}</span>
          </p>
        )}
      </div>
    </div>
  )
}
