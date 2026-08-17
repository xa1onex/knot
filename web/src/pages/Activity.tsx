import { useEffect, useState } from 'react'
import { api } from '../api'

type Event = {
  id: string
  actor: string
  action: string
  resource: string
  detail: string
  result: string
  created_at: string
}

export default function Activity() {
  const [events, setEvents] = useState<Event[]>([])
  const [error, setError] = useState('')

  useEffect(() => {
    api<{ events: Event[] }>('/v1/activity')
      .then((r) => setEvents(r.events || []))
      .catch((e) => setError(e.message))
  }, [])

  return (
    <div>
      <h1>Activity</h1>
      <p className="muted">Audit log of control-plane actions.</p>
      {error && <div className="error">{error}</div>}
      <table className="table">
        <thead>
          <tr>
            <th>Time</th>
            <th>Actor</th>
            <th>Action</th>
            <th>Resource</th>
            <th>Result</th>
          </tr>
        </thead>
        <tbody>
          {events.map((e) => (
            <tr key={e.id}>
              <td className="muted">{new Date(e.created_at).toLocaleString()}</td>
              <td>{e.actor}</td>
              <td className="mono">{e.action}</td>
              <td className="mono">{e.resource || e.detail || '—'}</td>
              <td>{e.result}</td>
            </tr>
          ))}
          {events.length === 0 && (
            <tr>
              <td colSpan={5} className="muted">No activity yet.</td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  )
}
