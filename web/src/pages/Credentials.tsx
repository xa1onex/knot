import { useEffect, useState } from 'react'
import { api, type Credential } from '../api'

const SCOPE_OPTIONS = [
  'devices.read',
  'devices.write',
  'storage.read',
  'storage.write',
  'services.read',
  'services.write',
  'deploy.read',
	'deploy.write',
	'source.read',
	'source.write',
	'build.read',
	'build.write',
	'release.read',
	'release.write',
	'release.activate',
	'traffic.read',
	'traffic.write',
	'logs.read',
	'logs.write',
	'secrets.read',
  'secrets.write',
  'compute.read',
  'compute.write',
  'network.transfer',
  'activity.read',
  'credentials.write',
  'account.admin',
]

export default function CredentialsPage() {
  const [list, setList] = useState<Credential[]>([])
  const [name, setName] = useState('')
  const [scopes, setScopes] = useState<string[]>(['devices.read'])
  const [ttlDays, setTtlDays] = useState(30)
  const [createdToken, setCreatedToken] = useState('')
  const [error, setError] = useState('')

  async function refresh() {
    const res = await api<{ credentials: Credential[] }>('/v1/credentials')
    setList(res.credentials || [])
  }

  useEffect(() => {
    refresh().catch((e) => setError(e.message))
  }, [])

  function toggleScope(s: string) {
    setScopes((prev) => (prev.includes(s) ? prev.filter((x) => x !== s) : [...prev, s]))
  }

  async function create() {
    setError('')
    try {
      const res = await api<{ token: string }>('/v1/credentials', {
        method: 'POST',
        body: JSON.stringify({ name, scopes, ttl_days: ttlDays }),
      })
      setCreatedToken(res.token)
      setName('')
      await refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed')
    }
  }

  async function revoke(id: string) {
    await api(`/v1/credentials/${id}/revoke`, { method: 'POST' })
    await refresh()
  }

  return (
    <div>
      <h1>Credentials</h1>
      <p className="muted">Scoped API tokens for CLI, MCP, and external AI clients.</p>
      {error && <div className="error">{error}</div>}

      <div className="panel" style={{ marginTop: '1.25rem' }}>
        <h2 style={{ fontSize: '1.1rem' }}>Create credential</h2>
        <div className="field">
          <label>Name</label>
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Claude-Code-Project-A" />
        </div>
        <div className="field">
          <label>Scopes</label>
          <div className="row">
            {SCOPE_OPTIONS.map((s) => (
              <label key={s} style={{ display: 'flex', gap: '0.35rem', alignItems: 'center' }}>
                <input
                  type="checkbox"
                  checked={scopes.includes(s)}
                  onChange={() => toggleScope(s)}
                  style={{ width: 'auto' }}
                />
                {s}
              </label>
            ))}
          </div>
        </div>
        <div className="field">
          <label>TTL (days)</label>
          <input
            type="number"
            style={{ maxWidth: 120 }}
            value={ttlDays}
            onChange={(e) => setTtlDays(Number(e.target.value))}
          />
        </div>
        <button type="button" onClick={create} disabled={!name || scopes.length === 0}>
          Create
        </button>
        {createdToken && (
          <div className="token-once">
            <div className="muted">Token shown once:</div>
            <div className="mono" style={{ marginTop: '0.4rem' }}>{createdToken}</div>
          </div>
        )}
      </div>

      <table className="table">
        <thead>
          <tr>
            <th>Name</th>
            <th>Prefix</th>
            <th>Scopes</th>
            <th>Status</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {list.map((c) => (
            <tr key={c.id}>
              <td>{c.name}</td>
              <td className="mono">{c.token_prefix}</td>
              <td className="muted">{(c.scopes || []).join(', ')}</td>
              <td>{c.revoked_at ? 'revoked' : 'active'}</td>
              <td>
                {!c.revoked_at && (
                  <button className="danger" type="button" onClick={() => revoke(c.id)}>
                    Revoke
                  </button>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
