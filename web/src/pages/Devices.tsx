import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type Device } from '../api'
import PageHeader from '../components/PageHeader'
import { formatWhen } from '../lib/format'

export default function Devices() {
  const [devices, setDevices] = useState<Device[]>([])
  const [nameHint, setNameHint] = useState('')
  const [regToken, setRegToken] = useState('')
  const [error, setError] = useState('')
  const [copied, setCopied] = useState('')
  const panelUrl = typeof window !== 'undefined' ? window.location.origin : ''
  const online = devices.filter((d) => d.online).length

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
    setCopied('')
    try {
      const res = await api<{ token: string }>('/v1/devices/registration-tokens', {
        method: 'POST',
        body: JSON.stringify({ name_hint: nameHint, ttl_hours: 24 }),
      })
      setRegToken(res.token)
      setNameHint('')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not create a join code')
    }
  }

  async function copy(text: string, label: string) {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(label)
    } catch {
      setCopied('')
    }
  }

  return (
    <div>
      <PageHeader
        kicker="Network"
        live={online > 0}
        liveLabel={online ? `${online} connected` : 'No computers yet'}
        title="Computers"
        description="These are the machines Node can open files on. This panel lives on your server; a home Mac or PC joins with a one-time code."
        actions={<Link to="/files" className="ghost" style={{ display: 'inline-flex', alignItems: 'center' }}>Open files</Link>}
      />
      {error && <div className="error">{error}</div>}

      <div className="panel">
        <p className="page-kicker" style={{ marginBottom: '0.5rem' }}>Add another computer</p>
        <h2 style={{ fontSize: '1.25rem' }}>Join a Mac or PC</h2>
        <ol className="steps">
          <li>Give it a nickname (optional), then create a join code.</li>
          <li>On that computer, open Terminal and run the installer. Choose <strong>Device Node</strong>.</li>
          <li>Paste this panel’s URL and the join code. After it connects, the computer appears below and in Files.</li>
        </ol>
        <div className="row" style={{ marginTop: '0.25rem' }}>
          <input
            style={{ maxWidth: 280 }}
            placeholder="Nickname, e.g. Home Mac"
            value={nameHint}
            onChange={(e) => setNameHint(e.target.value)}
          />
          <button type="button" onClick={() => void createRegToken()}>Create join code</button>
        </div>
        {regToken && (
          <div className="token-once">
            <strong>Join code — copy it now, it is shown once</strong>
            <p className="muted" style={{ margin: '0.35rem 0 0.7rem' }}>
              Valid for 24 hours. Anyone with this code can add a computer to your Node.
            </p>
            <div className="mono" style={{ wordBreak: 'break-all' }}>{regToken}</div>
            <div className="row" style={{ marginTop: '0.75rem' }}>
              <button type="button" className="secondary" onClick={() => void copy(regToken, 'code')}>
                {copied === 'code' ? 'Copied code' : 'Copy code'}
              </button>
              <button type="button" className="secondary" onClick={() => void copy(panelUrl, 'url')}>
                {copied === 'url' ? 'Copied URL' : 'Copy panel URL'}
              </button>
            </div>
            <p className="help-note">
              On the other computer:
              <br />
              <code>bash &lt;(curl -fsSL https://raw.githubusercontent.com/xa1onex/knot/main/scripts/install.sh)</code>
              <br />
              Choose <strong>2) Device Node</strong>. URL: <code>{panelUrl}</code>
            </p>
          </div>
        )}
      </div>

      <div style={{ marginTop: '1.5rem' }}>
        <p className="page-kicker">Your computers</p>
        {devices.length === 0 ? (
          <div className="empty-state">
            <h2>None connected yet</h2>
            <p>The panel is running. Add a home computer so photos, projects, and backups have a place to live.</p>
          </div>
        ) : (
          <div className="computer-list">
            {devices.map((d) => (
              <article key={d.id} className={`computer-card ${d.online ? 'is-online' : ''}`}>
                <span className="pulse" aria-hidden />
                <div>
                  <h3>{d.name}</h3>
                  <p>
                    {d.hostname || 'No hostname'} · {d.os || 'OS unknown'}
                    {d.arch ? `/${d.arch}` : ''}
                    {d.last_seen_at ? ` · last seen ${formatWhen(d.last_seen_at)}` : ''}
                  </p>
                </div>
                <span className={`status-pill ${d.online ? 'online' : 'offline'}`}>
                  {d.online ? 'Connected now' : 'Not connected'}
                </span>
              </article>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
