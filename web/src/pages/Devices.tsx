import { useEffect, useState } from 'react'
import { api, type Device } from '../api'

export default function Devices() {
  const [devices, setDevices] = useState<Device[]>([])
  const [nameHint, setNameHint] = useState('')
  const [regToken, setRegToken] = useState('')
  const [error, setError] = useState('')
  const panelUrl = typeof window !== 'undefined' ? window.location.origin : ''

  async function refresh() {
    const res = await api<{ devices: Device[] }>('/v1/devices')
    setDevices(res.devices || [])
  }

  useEffect(() => {
    refresh().catch((e) => setError(e.message))
    const t = setInterval(() => {
      refresh().catch(() => {})
    }, 5000)
    return () => clearInterval(t)
  }, [])

  async function createRegToken() {
    setError('')
    try {
      const res = await api<{ token: string }>('/v1/devices/registration-tokens', {
        method: 'POST',
        body: JSON.stringify({ name_hint: nameHint, ttl_hours: 24 }),
      })
      setRegToken(res.token)
      setNameHint('')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed')
    }
  }

  return (
    <div>
      <h1>Devices</h1>
      <p className="muted">Registered nodes and live presence.</p>
      {error && <div className="error">{error}</div>}

      <div className="panel" style={{ marginTop: '1.25rem' }}>
        <h2 style={{ fontSize: '1.1rem' }}>Add Device Node</h2>
        <p className="muted">Create a one-time join token, then run the installer on that computer as Device Node.</p>
        <div className="row" style={{ marginTop: '0.75rem' }}>
          <input
            style={{ maxWidth: 260 }}
            placeholder="name hint (e.g. home-pc)"
            value={nameHint}
            onChange={(e) => setNameHint(e.target.value)}
          />
          <button type="button" onClick={createRegToken}>Create token</button>
        </div>
        {regToken && (
          <div className="token-once">
            <div className="muted">Copy once — one-time join code for a Device Node:</div>
            <div className="mono" style={{ marginTop: '0.4rem' }}>{regToken}</div>
            <pre className="mono" style={{ marginTop: '0.75rem', whiteSpace: 'pre-wrap' }}>
{`# On the other computer:
bash <(curl -fsSL https://raw.githubusercontent.com/xa1onex/knot/main/scripts/install.sh)
# choose  2) Device Node
# URL:    ${panelUrl}
# Token:  ${regToken}

# or, if the agent is already installed:
knot-agent \\
  -control-url ${panelUrl} \\
  -registration-token ${regToken}`}
            </pre>
          </div>
        )}
      </div>

      <table className="table">
        <thead>
          <tr>
            <th>Status</th>
            <th>Name</th>
            <th>Host</th>
            <th>OS</th>
            <th>Last seen</th>
          </tr>
        </thead>
        <tbody>
          {devices.map((d) => (
            <tr key={d.id}>
              <td>
                <span className={`badge ${d.online ? 'online' : 'offline'}`}>
                  {d.online ? 'online' : 'offline'}
                </span>
              </td>
              <td>{d.name}</td>
              <td className="mono">{d.hostname || '—'}</td>
              <td>{d.os}/{d.arch}</td>
              <td className="muted">{d.last_seen_at ? new Date(d.last_seen_at).toLocaleString() : '—'}</td>
            </tr>
          ))}
          {devices.length === 0 && (
            <tr>
              <td colSpan={5} className="muted">No devices yet.</td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  )
}
