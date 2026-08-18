import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { Button } from '@/components/ui/button'
import { createClient, setToken } from '@/lib/client'

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
    } catch {
      setError('Could not sign in. Use the email and password you set when installing Node.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <main className="relative min-h-screen overflow-hidden bg-background">
      <div className="pointer-events-none absolute inset-0 -z-10">
        <div className="absolute left-1/2 top-0 h-[520px] w-[520px] -translate-x-1/2 rounded-full bg-foreground/[0.035] blur-[140px]" />
        <div className="absolute bottom-0 right-0 h-[360px] w-[360px] rounded-full bg-foreground/[0.025] blur-[120px]" />
      </div>
      <div className="relative flex min-h-screen items-center justify-center px-6">
        <form
          onSubmit={onSubmit}
          className="w-full max-w-md space-y-6 rounded-2xl border border-border/40 bg-background/60 p-8 backdrop-blur"
        >
          <div className="space-y-2">
            <p className="text-xs font-semibold uppercase tracking-[0.25em] text-foreground/40">Node</p>
            <h1 className="text-3xl font-semibold tracking-tight">Sign in</h1>
            <p className="text-foreground/70">
              This opens the panel for your computers and files. Nothing here is public.
            </p>
          </div>
          <label className="block space-y-2 text-sm">
            <span className="font-medium text-foreground/70">Email</span>
            <input
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              autoComplete="username"
              placeholder="you@example.com"
            />
          </label>
          <label className="block space-y-2 text-sm">
            <span className="font-medium text-foreground/70">Password</span>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="current-password"
              placeholder="The password from install"
            />
          </label>
          <Button type="submit" className="w-full" disabled={busy || !email || !password}>
            {busy ? 'Signing in…' : 'Open Node'}
          </Button>
          {error && <p className="text-sm text-red-600">{error}</p>}
        </form>
      </div>
    </main>
  )
}
