import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { Button } from '@/components/ui/button'
import { createClient, setToken } from '@/lib/client'
import { usePrefs } from '@/lib/prefs'

export default function Login() {
  const nav = useNavigate()
  const { t, lang, setLang, theme, setTheme } = usePrefs()
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
      setError(t('login_error'))
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
            <div className="flex justify-end gap-2">
              <Button variant="ghost" size="sm" onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}>
                {theme === 'dark' ? t('theme_light') : t('theme_dark')}
              </Button>
              <Button variant="ghost" size="sm" onClick={() => setLang(lang === 'en' ? 'ru' : 'en')}>
                {lang === 'en' ? 'RU' : 'EN'}
              </Button>
            </div>
          <div className="space-y-2">
            <p className="text-xs font-semibold uppercase tracking-[0.25em] text-foreground/40">{t('brand')}</p>
            <h1 className="text-3xl font-semibold tracking-tight">{t('login_title')}</h1>
            <p className="text-foreground/70">{t('login_lead')}</p>
          </div>
          <label className="block space-y-2 text-sm">
            <span className="font-medium text-foreground/70">{t('email')}</span>
            <input
              className="w-full"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              autoComplete="username"
              placeholder="you@example.com"
            />
          </label>
          <label className="block space-y-2 text-sm">
            <span className="font-medium text-foreground/70">{t('password')}</span>
            <input
              className="w-full"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="current-password"
              placeholder="The password from install"
            />
          </label>
          <Button type="submit" className="w-full" disabled={busy || !email || !password}>
            {busy ? '…' : t('open_node')}
          </Button>
          {error && <p className="text-sm text-red-600">{error}</p>}
        </form>
      </div>
    </main>
  )
}
