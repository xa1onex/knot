import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { createClient, setToken } from '../lib/client'

export default function Login() {
  const nav = useNavigate()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
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
      setError(err instanceof Error ? err.message : 'Could not sign in. Check email and password.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="login-page">
      <div className="glass-blobs" aria-hidden />
      <form className="login-card" onSubmit={onSubmit}>
        <div className="brand-mark login-brand">Node</div>
        <p className="login-lead">Sign in to open files on your computers — Mac, PC, or the server this panel runs on.</p>
        <div className="field">
          <label htmlFor="email">Email</label>
          <input
            id="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            autoComplete="username"
            placeholder="you@example.com"
          />
        </div>
        <div className="field">
          <label htmlFor="password">Password</label>
          <input
            id="password"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            placeholder="The password you set at install"
          />
        </div>
        <button type="submit" disabled={busy || !email || !password}>
          {busy ? 'Signing in…' : 'Open Node'}
        </button>
        {error && <div className="error">{error}</div>}
        <p className="help-note">First time? This is the email and password you typed when you installed the Main Node.</p>
      </form>
    </div>
  )
}
