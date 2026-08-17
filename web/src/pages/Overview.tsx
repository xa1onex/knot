import { useEffect, useState } from 'react'
import { api } from '../api'

type OverviewData = {
  devices_total: number
  devices_online: number
  devices_offline: number
}

export default function Overview() {
  const [data, setData] = useState<OverviewData | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    api<OverviewData>('/v1/overview')
      .then(setData)
      .catch((e) => setError(e.message))
  }, [])

  return (
    <div>
      <h1>Overview</h1>
      <p className="muted">Presence across your Node network.</p>
      {error && <div className="error">{error}</div>}
      <div className="grid-stats">
        <div className="panel stat">
          <span className="muted">Devices</span>
          <strong>{data?.devices_total ?? '—'}</strong>
        </div>
        <div className="panel stat">
          <span className="muted">Online</span>
          <strong>{data?.devices_online ?? '—'}</strong>
        </div>
        <div className="panel stat">
          <span className="muted">Offline</span>
          <strong>{data?.devices_offline ?? '—'}</strong>
        </div>
      </div>
    </div>
  )
}
