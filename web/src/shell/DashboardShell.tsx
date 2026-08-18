import type { ReactNode } from 'react'
import { NavLink, useLocation, useNavigate } from 'react-router-dom'
import { motion } from 'framer-motion'
import {
  Activity,
  BarChart3,
  FolderOpen,
  LogOut,
  Monitor,
  Settings,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { setToken } from '@/lib/client'
import { cn } from '@/lib/cn'

const NAV = [
  { to: '/', label: 'Overview', icon: BarChart3, end: true },
  { to: '/files', label: 'Files', icon: FolderOpen },
  { to: '/computers', label: 'Computers', icon: Monitor },
  { to: '/settings/activity', label: 'Activity', icon: Activity },
  { to: '/settings', label: 'Settings', icon: Settings, end: true },
]

export default function DashboardShell({
  children,
  wide,
}: {
  children: ReactNode
  wide?: boolean
}) {
  const nav = useNavigate()
  const loc = useLocation()

  function activeFor(item: (typeof NAV)[number], isActive: boolean) {
    if (item.label === 'Settings') {
      return loc.pathname.startsWith('/settings') && loc.pathname !== '/settings/activity'
    }
    if (item.label === 'Activity') return loc.pathname === '/settings/activity'
    return isActive
  }

  return (
    <main className="relative min-h-screen overflow-hidden bg-background">
      <div className="pointer-events-none absolute inset-0 -z-10">
        <div className="absolute left-1/2 top-0 h-[520px] w-[520px] -translate-x-1/2 rounded-full bg-foreground/[0.035] blur-[140px]" />
        <div className="absolute bottom-0 right-0 h-[360px] w-[360px] rounded-full bg-foreground/[0.025] blur-[120px]" />
        <div className="absolute left-1/4 top-1/2 h-[400px] w-[400px] rounded-full bg-primary/[0.02] blur-[150px]" />
      </div>

      <motion.nav
        initial={{ opacity: 0, y: -10 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.4 }}
        className="border-b border-border/40 bg-background/40 backdrop-blur-md"
        aria-label="Main"
      >
        <div className="mx-auto flex max-w-7xl items-center justify-between px-6 py-4">
          <NavLink to="/" className="text-xl font-semibold tracking-tight text-foreground">
            Node
          </NavLink>
          <div className="hidden gap-1 md:flex">
            {NAV.map((item) => {
              const Icon = item.icon
              return (
                <NavLink key={item.to + item.label} to={item.to} end={item.end}>
                  {({ isActive }) => (
                    <span
                      className={cn(
                        'inline-flex items-center gap-2 rounded-lg px-3 py-2 text-xs uppercase tracking-[0.1em] text-foreground/70 hover:bg-background/50 hover:text-foreground',
                        activeFor(item, isActive) && 'bg-background/70 text-foreground',
                      )}
                    >
                      <Icon className="h-4 w-4" aria-hidden />
                      {item.label}
                    </span>
                  )}
                </NavLink>
              )
            })}
          </div>
          <Button
            variant="ghost"
            size="sm"
            className="gap-2"
            onClick={() => {
              setToken(null)
              nav('/login')
            }}
          >
            <LogOut className="h-4 w-4" />
            Log out
          </Button>
        </div>
        <div className="flex gap-1 overflow-x-auto px-4 pb-3 md:hidden">
          {NAV.map((item) => {
            const Icon = item.icon
            return (
              <NavLink key={item.to + item.label} to={item.to} end={item.end} className="shrink-0">
                {({ isActive }) => (
                  <span
                    className={cn(
                      'inline-flex items-center gap-2 rounded-lg px-3 py-2 text-xs uppercase tracking-[0.1em] text-foreground/70',
                      activeFor(item, isActive) && 'bg-background/70 text-foreground',
                    )}
                  >
                    <Icon className="h-4 w-4" />
                    {item.label}
                  </span>
                )}
              </NavLink>
            )
          })}
        </div>
      </motion.nav>

      <div className="relative px-6 py-8 lg:py-12">
        <div className={wide ? 'mx-auto max-w-[1400px]' : 'mx-auto max-w-7xl'}>{children}</div>
      </div>
    </main>
  )
}

export function PageHero({
  live,
  liveLabel = 'Live',
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
  return (
    <motion.div
      initial={{ opacity: 0, y: 20 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.5 }}
      className="mb-12 space-y-4"
    >
      <div className="flex flex-col justify-between gap-4 md:flex-row md:items-center">
        <div className="space-y-2">
          {live != null && (
            <span className="inline-flex items-center gap-2 rounded-full border border-border/50 bg-background/55 px-4 py-1.5 text-xs uppercase tracking-[0.2em] text-foreground/70 backdrop-blur">
              <span className={`h-2 w-2 rounded-full ${live ? 'bg-emerald-500' : 'bg-foreground/30'}`} />
              {liveLabel}
            </span>
          )}
          <h1 className="text-balance text-3xl font-semibold tracking-tight text-foreground md:text-4xl">
            {title}
          </h1>
          <p className="max-w-2xl text-foreground/70">{description}</p>
        </div>
        {actions && <div className="flex gap-2">{actions}</div>}
      </div>
    </motion.div>
  )
}

export function Glass({
  children,
  className,
}: {
  children: ReactNode
  className?: string
}) {
  return (
    <div
      className={cn(
        'group relative overflow-hidden rounded-2xl border border-border/40 bg-background/60 p-6 backdrop-blur transition-all hover:border-border/60 hover:shadow-lg',
        className,
      )}
    >
      <div className="pointer-events-none absolute inset-0 -z-10 bg-gradient-to-br from-foreground/[0.04] via-transparent to-transparent opacity-0 transition-opacity duration-300 group-hover:opacity-100" />
      <div className="relative">{children}</div>
    </div>
  )
}
