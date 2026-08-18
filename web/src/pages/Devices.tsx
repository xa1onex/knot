import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type Device } from '../api'
import PageHeader from '../components/PageHeader'
import { formatWhen } from '../lib/format'
import { usePrefs } from '../lib/prefs'

export default function Devices() {
  const { t } = usePrefs()
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
        live={online > 0}
        liveLabel={online ? t('connected') : t('waiting')}
        title={t('computers_title')}
        description={t('computers_lead')}
        actions={<Link to="/files" className="ghost" style={{ display: 'inline-flex', alignItems: 'center' }}>{t('open_files')}</Link>}
      />
      {error && <div className="error">{error}</div>}

      <div className="panel">
        <p className="page-kicker" style={{ marginBottom: '0.5rem' }}>Add another computer</p>
        <h2 style={{ fontSize: '1.25rem' }}>{t('join_title')}</h2>
        <ol className="steps">
          <li>{t('join_step_1')}</li>
          <li>{t('join_step_2')}</li>
          <li>{t('join_step_3')}</li>
        </ol>
        <div className="row" style={{ marginTop: '0.25rem' }}>
          <input
            style={{ maxWidth: 280 }}
            placeholder={t('nickname')}
            value={nameHint}
            onChange={(e) => setNameHint(e.target.value)}
          />
          <button type="button" onClick={() => void createRegToken()}>{t('create_code')}</button>
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
                  {d.online ? t('connected') : t('not_connected')}
                </span>
              </article>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
