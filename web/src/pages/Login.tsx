import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { createClient, setToken } from '../lib/client'

export default function Login() {
  const nav = useNavigate()
  const [email, setEmail] = useState('admin@node.local')
  const [password, setPassword] = useState('admin')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const cl = createClient('')
      const res = await cl.login(email, password)
      setToken(res.access_token || res.token || '')
      nav('/')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'login failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="login-page">
      <form className="login-card" onSubmit={onSubmit}>
        <div className="brand-mark login-brand">Node</div>
        <p className="login-lead">Your devices. Your files. One network.</p>
        <div className="field">
          <label htmlFor="email">Email</label>
          <input id="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="username" />
        </div>
        <div className="field">
          <label htmlFor="password">Password</label>
          <input
            id="password"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
          />
        </div>
        <button type="submit" disabled={busy}>{busy ? 'Signing in…' : 'Open Node'}</button>
        {error && <div className="error">{error}</div>}
      </form>
    </div>
  )
}
