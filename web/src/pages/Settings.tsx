import { useEffect, useState } from 'react'

export default function Settings() {
  const desktop = typeof window !== 'undefined' ? window.nodeDesktop : undefined
  const [apiUrl, setApiUrl] = useState(
    import.meta.env.VITE_API_URL || (desktop ? '…' : 'same-origin via Vite proxy (/v1 → knotd)'),
  )
  const [draft, setDraft] = useState('')
  const [saving, setSaving] = useState(false)
  const [msg, setMsg] = useState('')

  useEffect(() => {
    if (!desktop) return
    void desktop.getApiUrl().then((u) => {
      setApiUrl(u)
      setDraft(u)
    })
  }, [desktop])

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
    </div>
  )
}
