import type { ReactNode } from 'react'
import { NavLink, useLocation, useNavigate } from 'react-router-dom'
import { AnimatePresence, motion } from 'framer-motion'
import {
  Activity,
  BarChart3,
  FolderOpen,
  Globe,
  HardDrive,
  KeyRound,
  Languages,
  LogOut,
  Monitor,
  Moon,
  RefreshCw,
  Settings,
  Sun,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { setToken } from '@/lib/client'
import { cn } from '@/lib/cn'
import { usePrefs } from '@/lib/prefs'

export default function DashboardShell({ children }: { children: ReactNode }) {
  const nav = useNavigate()
  const loc = useLocation()
  const { t, lang, setLang, theme, setTheme } = usePrefs()
  const fill = loc.pathname === '/files'

  const NAV = [
    { to: '/', label: t('nav_overview'), icon: BarChart3, end: true },
    { to: '/files', label: t('nav_files'), icon: FolderOpen },
    { to: '/computers', label: t('nav_computers'), icon: Monitor },
    { to: '/settings', label: t('nav_updates'), icon: RefreshCw, end: true },
    { to: '/settings/sync', label: t('nav_sync'), icon: Settings },
    { to: '/settings/services', label: t('nav_sites'), icon: Globe },
    { to: '/settings/compute', label: t('nav_hardware'), icon: HardDrive },
    { to: '/settings/credentials', label: t('nav_keys'), icon: KeyRound },
    { to: '/settings/activity', label: t('nav_history'), icon: Activity },
  ]

  return (
    <div className="relative flex h-full min-h-0 bg-background">
      <div className="pointer-events-none absolute inset-0 -z-10 overflow-hidden">
        <motion.div
          className="absolute top-0 left-1/3 h-[420px] w-[420px] rounded-full bg-foreground/[0.04] blur-[140px]"
          animate={{ x: [0, 30, 0], y: [0, 20, 0] }}
          transition={{ duration: 18, repeat: Infinity, ease: 'easeInOut' }}
        />
        <motion.div
          className="absolute right-0 bottom-0 h-[320px] w-[320px] rounded-full bg-foreground/[0.03] blur-[120px]"
          animate={{ x: [0, -20, 0], y: [0, -16, 0] }}
          transition={{ duration: 22, repeat: Infinity, ease: 'easeInOut' }}
        />
      </div>

      <aside className="flex w-[232px] shrink-0 flex-col border-r border-border/40 bg-sidebar/80 backdrop-blur-md">
        <div className="px-5 py-5">
          <NavLink to="/" className="text-xl font-semibold tracking-tight">
            {t('brand')}
          </NavLink>
        </div>
        <nav className="flex min-h-0 flex-1 flex-col gap-0.5 overflow-auto px-3 pb-3" aria-label="Main">
          {NAV.slice(0, 3).map((item) => (
            <SideLink key={item.to} {...item} />
          ))}
          <div className="my-3 h-px bg-border/50" />
          {NAV.slice(3).map((item) => (
            <SideLink key={item.to} {...item} />
          ))}
        </nav>
        <div className="space-y-2 border-t border-border/40 p-3">
          <div className="flex gap-1">
            <Button
              variant={theme === 'light' ? 'outline' : 'ghost'}
              size="sm"
              className="flex-1"
              onClick={() => setTheme('light')}
              aria-label={t('theme_light')}
            >
              <Sun className="h-3.5 w-3.5" />
              {t('theme_light')}
            </Button>
            <Button
              variant={theme === 'dark' ? 'outline' : 'ghost'}
              size="sm"
              className="flex-1"
              onClick={() => setTheme('dark')}
              aria-label={t('theme_dark')}
            >
              <Moon className="h-3.5 w-3.5" />
              {t('theme_dark')}
            </Button>
          </div>
          <Button
            variant="ghost"
            size="sm"
            className="w-full justify-start gap-2"
            onClick={() => setLang(lang === 'en' ? 'ru' : 'en')}
          >
            <Languages className="h-3.5 w-3.5" />
            {lang === 'en' ? 'English · RU' : 'Русский · EN'}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            className="w-full justify-start gap-2"
            onClick={() => {
              setToken(null)
              nav('/login')
            }}
          >
            <LogOut className="h-3.5 w-3.5" />
            {t('logout')}
          </Button>
        </div>
      </aside>

      <div className={cn('min-w-0 flex-1', fill ? 'flex min-h-0 flex-col overflow-hidden' : 'overflow-auto')}>
        <AnimatePresence mode="wait">
          <motion.div
            key={loc.pathname}
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -6 }}
            transition={{ duration: 0.22 }}
            className={fill ? 'flex min-h-0 flex-1 flex-col' : 'mx-auto w-full max-w-6xl px-8 py-8'}
          >
            {children}
          </motion.div>
        </AnimatePresence>
      </div>
    </div>
  )
}

function SideLink({
  to,
  label,
  icon: Icon,
  end,
}: {
  to: string
  label: string
  icon: typeof BarChart3
  end?: boolean
}) {
  return (
    <NavLink to={to} end={end}>
      {({ isActive }) => (
        <span
          className={cn(
            'flex items-center gap-2.5 rounded-xl px-3 py-2 text-sm text-foreground/65 transition-colors hover:bg-background/60 hover:text-foreground',
            isActive && 'bg-background text-foreground shadow-sm',
          )}
        >
          <Icon className="h-4 w-4 shrink-0" />
          {label}
        </span>
      )}
    </NavLink>
  )
}

export function PageHero({
  live,
  liveLabel,
  title,
  description,
  actions,
}: {
  live?: boolean
  liveLabel?: string
  title: string
  description: string
  actions?: ReactNode
}) {
  const { t } = usePrefs()
  return (
    <div className="mb-8 flex flex-col justify-between gap-4 md:flex-row md:items-end">
      <div className="space-y-2">
        {live != null && (
          <span className="inline-flex items-center gap-2 rounded-full border border-border/50 bg-background/55 px-3 py-1 text-[11px] font-semibold uppercase tracking-[0.18em] text-foreground/70">
            <span className={cn('h-2 w-2 rounded-full', live ? 'bg-emerald-500' : 'bg-foreground/30')} />
            {liveLabel || (live ? t('live') : t('waiting'))}
          </span>
        )}
        <h1 className="text-3xl font-semibold tracking-tight">{title}</h1>
        <p className="max-w-xl text-muted-foreground">{description}</p>
      </div>
      {actions && <div className="flex gap-2">{actions}</div>}
    </div>
  )
}
