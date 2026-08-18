import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { HardDrive, Monitor, RefreshCw, Zap } from 'lucide-react'
import { ChartCard } from '@/components/dashboard/ChartCard'
import { DetailedCard } from '@/components/dashboard/DetailedCard'
import { MetricCard } from '@/components/dashboard/MetricCard'
import { Button } from '@/components/ui/button'
import { api } from '@/api'
import { createClient } from '@/lib/client'
import { formatWhen } from '@/lib/format'
import type { Device, Transfer } from '@node-infra/client'
import DashboardShell, { PageHero } from '@/shell/DashboardShell'

type OverviewData = {
  devices_total: number
  devices_online: number
  devices_offline: number
}

type Event = {
  id: string
  actor: string
  action: string
  resource: string
  result: string
  created_at: string
}

function bucketsFrom(dates: string[], labels: number) {
  const now = Date.now()
  const step = 60 * 60 * 1000
  const counts = Array.from({ length: labels }, () => 0)
  for (const iso of dates) {
    const t = new Date(iso).getTime()
    if (Number.isNaN(t)) continue
    const idx = labels - 1 - Math.floor((now - t) / step)
    if (idx >= 0 && idx < labels) counts[idx] += 1
  }
  return counts.map((value, i) => ({
    name: `${labels - 1 - i}h`,
    value,
  }))
}

export default function Overview() {
  const nav = useNavigate()
  const cl = useMemo(() => createClient(), [])
  const [overview, setOverview] = useState<OverviewData | null>(null)
  const [devices, setDevices] = useState<Device[]>([])
  const [events, setEvents] = useState<Event[]>([])
  const [transfers, setTransfers] = useState<Transfer[]>([])
  const [updatesReady, setUpdatesReady] = useState(0)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const [ov, devs, act, xfer] = await Promise.all([
          api<OverviewData>('/v1/overview'),
          cl.listDevices(),
          api<{ events: Event[] }>('/v1/activity').catch(() => ({ events: [] })),
          cl.listTransfers().catch(() => []),
        ])
        if (cancelled) return
        setOverview(ov)
        setDevices(devs.filter((d) => !d.revoked_at))
        setEvents(act.events || [])
        setTransfers(xfer.slice(0, 20))
        try {
          const fleet = await api<{
            control_plane: { available: boolean }
            devices: { status?: { available: boolean } }[]
          }>('/v1/system/update')
          const n =
            (fleet.control_plane.available ? 1 : 0) +
            fleet.devices.filter((d) => d.status?.available).length
          setUpdatesReady(n)
        } catch {
          setUpdatesReady(0)
        }
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : 'Could not load')
      }
    })()
    return () => {
      cancelled = true
    }
  }, [cl])

  const live = (overview?.devices_online ?? 0) > 0
  const activitySeries = useMemo(
    () => bucketsFrom(events.map((e) => e.created_at), 13),
    [events],
  )
  const transferSeries = useMemo(
    () => bucketsFrom(transfers.map((t) => t.created_at || ''), 13),
    [transfers],
  )
  const activeTransfers = transfers.filter((t) =>
    ['pending', 'offered', 'negotiating', 'transferring'].includes(t.status),
  ).length

  return (
    <DashboardShell>
      <PageHero
        live={live}
        liveLabel={live ? 'Live' : 'Waiting'}
        title="Your network at a glance"
        description="See which computers are connected, what moved recently, and whether Node itself needs an update. Click a card when you want to do something."
        actions={
          <>
            <Button variant="outline" size="icon" aria-label="Refresh" onClick={() => window.location.reload()}>
              <RefreshCw className="h-4 w-4" />
            </Button>
            <Button variant="outline" onClick={() => nav('/files')}>
              Open files
            </Button>
          </>
        }
      />

      {error && <p className="mb-6 text-sm text-red-600">{error}</p>}

      <div className="space-y-6">
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <MetricCard
            label="Computers"
            value={String(overview?.devices_total ?? '—')}
            change={overview ? `${overview.devices_online} online` : undefined}
            trend={overview && overview.devices_online > 0 ? 'up' : 'neutral'}
            icon={<Monitor className="h-6 w-6 text-primary" />}
          />
          <MetricCard
            label="Connected now"
            value={String(overview?.devices_online ?? '—')}
            change={overview && overview.devices_offline ? `${overview.devices_offline} away` : 'all here'}
            trend={overview && overview.devices_offline ? 'down' : 'up'}
            icon={<Zap className="h-6 w-6 text-primary" />}
          />
          <MetricCard
            label="Transfers"
            value={String(activeTransfers)}
            change={activeTransfers ? 'in progress' : 'idle'}
            trend={activeTransfers ? 'up' : 'neutral'}
            icon={<HardDrive className="h-6 w-6 text-primary" />}
          />
          <MetricCard
            label="Updates"
            value={updatesReady ? String(updatesReady) : 'None'}
            change={updatesReady ? 'ready to install' : 'up to date'}
            trend={updatesReady ? 'down' : 'up'}
            icon={<RefreshCw className="h-6 w-6 text-primary" />}
          />
        </div>

        <div className="grid gap-6 lg:grid-cols-2">
          <ChartCard
            title="Activity"
            description="Actions on this panel, last 13 hours"
            data={activitySeries}
          />
          <ChartCard
            title="Transfers"
            description="File sends started in the last 13 hours"
            data={transferSeries}
          />
        </div>

        <div className="grid gap-6 lg:grid-cols-3">
          <DetailedCard
            title="Computers"
            items={devices.slice(0, 5).map((d) => ({
              label: d.name,
              value: d.online ? 'Online' : 'Away',
              subtitle: d.os || 'Computer',
              href: '/files',
            }))}
            onSelect={() => nav('/computers')}
          />
          <DetailedCard
            title="Recent activity"
            items={events.slice(0, 5).map((e) => ({
              label: e.action.replaceAll('.', ' '),
              value: e.result || 'ok',
              subtitle: formatWhen(e.created_at),
              href: '/settings/activity',
            }))}
            onSelect={() => nav('/settings/activity')}
          />
          <DetailedCard
            title="What to do next"
            items={[
              { label: 'Open files', value: 'Go', subtitle: 'Browse a connected computer', href: '/files' },
              { label: 'Add a computer', value: 'Go', subtitle: 'Join a Mac or PC with a code', href: '/computers' },
              { label: 'Check updates', value: 'Go', subtitle: 'Keep the panel current', href: '/settings' },
            ]}
            onSelect={(item) => item.href && nav(item.href)}
          />
        </div>
      </div>
    </DashboardShell>
  )
}
