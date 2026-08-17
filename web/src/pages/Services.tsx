import { useCallback, useEffect, useMemo, useState } from 'react'
import type { Device, EdgeRoute, HostedService, ServiceNode } from '@node-infra/client'
import { createClient } from '../lib/client'

const KINDS = ['web', 'api', 'database', 'worker', 'other'] as const

export default function Services() {
  const cl = createClient()
  const [tree, setTree] = useState<ServiceNode[]>([])
  const [devices, setDevices] = useState<Device[]>([])
  const [routes, setRoutes] = useState<EdgeRoute[]>([])
  const [health, setHealth] = useState<Record<string, HostedService>>({})
  const [error, setError] = useState('')
  const [deviceId, setDeviceId] = useState('')
  const [name, setName] = useState('')
  const [kind, setKind] = useState<(typeof KINDS)[number]>('web')
  const [port, setPort] = useState('3000')
  const [protocol, setProtocol] = useState('http')
  const [bind, setBind] = useState('127.0.0.1')
  const [hostname, setHostname] = useState('')
  const [routeServiceId, setRouteServiceId] = useState('')
  const [edgeDeviceId, setEdgeDeviceId] = useState('')

  const refresh = useCallback(async () => {
    try {
      const [nodes, devs, rts] = await Promise.all([cl.servicesTree(), cl.listDevices(), cl.listRoutes()])
      setTree(nodes)
      setRoutes(rts)
      const live = devs.filter((d) => !d.revoked_at)
      setDevices(live)
      setDeviceId((cur) => cur || live[0]?.id || '')
      setEdgeDeviceId((cur) => cur || live.find((d) => /vps/i.test(d.name))?.id || '')
      const httpSvcs = nodes.flatMap((n) => n.services.filter((s) => s.protocol === 'http' || s.protocol === 'https'))
      setRouteServiceId((cur) => cur || httpSvcs[0]?.id || '')
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load services')
    }
  }, [cl])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const httpServices = useMemo(
    () => tree.flatMap((n) => n.services.filter((s) => s.protocol === 'http' || s.protocol === 'https')),
    [tree],
  )

  async function add() {
    setError('')
    const n = Number(port)
    if (!deviceId || !name.trim() || !Number.isFinite(n)) {
      setError('Node, name, and port are required')
      return
    }
    try {
      await cl.registerService({
        device_id: deviceId,
        name: name.trim(),
        kind,
        protocol,
        port: n,
        bind: bind.trim() || undefined,
      })
      setName('')
      await refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to register')
    }
  }

  async function remove(id: string) {
    setError('')
    try {
      await cl.deleteService(id)
      await refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to delete')
    }
  }

  async function addRoute() {
    setError('')
    if (!hostname.trim() || !routeServiceId) {
      setError('Hostname and HTTP service are required')
      return
    }
    try {
      await cl.createRoute({
        hostname: hostname.trim(),
        service_id: routeServiceId,
        edge_device_id: edgeDeviceId || undefined,
      })
      setHostname('')
      await refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to add route')
    }
  }

  async function removeRoute(id: string) {
    setError('')
    try {
      await cl.deleteRoute(id)
      await refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to delete route')
    }
  }

  async function checkHealth(id: string) {
    setError('')
    try {
      const h = await cl.serviceHealth(id)
      setHealth((cur) => ({ ...cur, [id]: h }))
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Health check failed')
    }
  }

  return (
    <div>
      <h1>Services</h1>
      <p className="muted">
        Control Plane knows where a service lives and which public hostname maps to it.
        The process stays on the node. Public HTTPS terminates on the Edge; Home PC is reached only through the outbound agent tunnel.
      </p>
      {error && <div className="error">{error}</div>}

      <div className="panel" style={{ marginTop: '1.25rem' }}>
        <h2 style={{ fontSize: '1.1rem' }}>Register</h2>
        <div className="field">
          <label>Node</label>
          <select value={deviceId} onChange={(e) => setDeviceId(e.target.value)}>
            {devices.map((d) => (
              <option key={d.id} value={d.id}>{d.name}{d.online ? '' : ' (offline)'}</option>
            ))}
          </select>
        </div>
        <div className="row" style={{ gap: '0.75rem', flexWrap: 'wrap' }}>
          <div className="field" style={{ flex: '1 1 140px' }}>
            <label>Name</label>
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder="web-app" />
          </div>
          <div className="field">
            <label>Kind</label>
            <select value={kind} onChange={(e) => {
              const k = e.target.value as (typeof KINDS)[number]
              setKind(k)
              setProtocol(k === 'database' ? 'tcp' : 'http')
            }}>
              {KINDS.map((k) => <option key={k} value={k}>{k}</option>)}
            </select>
          </div>
          <div className="field">
            <label>Port</label>
            <input value={port} onChange={(e) => setPort(e.target.value)} inputMode="numeric" />
          </div>
          <div className="field">
            <label>Protocol</label>
            <select value={protocol} onChange={(e) => setProtocol(e.target.value)}>
              <option value="http">http</option>
              <option value="https">https</option>
              <option value="tcp">tcp</option>
              <option value="udp">udp</option>
            </select>
          </div>
          <div className="field">
            <label>Bind</label>
            <input value={bind} onChange={(e) => setBind(e.target.value)} className="mono" />
          </div>
        </div>
        <button type="button" onClick={() => void add()}>Register</button>
      </div>

      <div className="panel" style={{ marginTop: '1.25rem' }}>
        <h2 style={{ fontSize: '1.1rem' }}>Edge routes</h2>
        <p className="muted">example.com → service on a node. Traffic never dials 192.168.x.x; the home agent opens the tunnel outbound.</p>
        <div className="row" style={{ gap: '0.75rem', flexWrap: 'wrap' }}>
          <div className="field" style={{ flex: '1 1 160px' }}>
            <label>Hostname</label>
            <input value={hostname} onChange={(e) => setHostname(e.target.value)} placeholder="example.com" className="mono" />
          </div>
          <div className="field" style={{ flex: '1 1 180px' }}>
            <label>Service</label>
            <select value={routeServiceId} onChange={(e) => setRouteServiceId(e.target.value)}>
              {httpServices.map((s) => (
                <option key={s.id} value={s.id}>{s.device_name} / {s.name} ({s.listen})</option>
              ))}
            </select>
          </div>
          <div className="field" style={{ flex: '1 1 160px' }}>
            <label>Edge node (optional)</label>
            <select value={edgeDeviceId} onChange={(e) => setEdgeDeviceId(e.target.value)}>
              <option value="">Control Plane listener</option>
              {devices.map((d) => (
                <option key={d.id} value={d.id}>{d.name}</option>
              ))}
            </select>
          </div>
        </div>
        <button type="button" onClick={() => void addRoute()} disabled={httpServices.length === 0}>Add route</button>
        {routes.length > 0 && (
          <ul className="svc-tree" style={{ marginTop: '0.85rem' }}>
            {routes.map((rt) => (
              <li key={rt.id} className="svc-route">
                <span className="mono">{rt.hostname}</span>
                <span>→ {rt.service_name}</span>
                <span className="mono">{rt.listen}</span>
                <span className="muted">Edge: {rt.edge_device_name || 'Control Plane'}</span>
                <button type="button" className="tiny danger" onClick={() => void removeRoute(rt.id)}>Remove</button>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div style={{ marginTop: '1.5rem' }}>
        {tree.map((n) => (
          <div key={n.device_id} className="panel" style={{ marginBottom: '0.85rem' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', gap: '0.75rem' }}>
              <strong>{n.device_name}</strong>
              <span className="muted">{n.online ? 'Online' : 'Offline'}</span>
            </div>
            {n.services.length === 0 ? (
              <p className="muted" style={{ margin: '0.6rem 0 0' }}>No services registered.</p>
            ) : (
              <ul className="svc-tree">
                {n.services.map((svc, i) => {
                  const h = health[svc.id] ?? svc
                  return (
                    <li key={svc.id}>
                      <span className="mono">{i === n.services.length - 1 ? '└──' : '├──'}</span>
                      <span><strong>{svc.name}</strong></span>
                      <span className="muted">{svc.kind}</span>
                      <span className="mono">{svc.listen}</span>
                      <span className="health-bits" title="registered / agent / tunnel / backend">
                        <i className={`health-dot ${h.registered !== false ? 'ok' : 'off'}`} />
                        <i className={`health-dot ${h.agent_online ? 'ok' : 'off'}`} />
                        <i className={`health-dot ${h.tunnel_connected ? 'ok' : 'off'}`} />
                        <i className={`health-dot ${h.backend_reachable ? 'ok' : 'off'}`} />
                      </span>
                      <span className="muted">{(h.hostnames ?? []).join(', ') || 'no hostname'}</span>
                      <span className="muted">{h.edge_device_name ? `Edge: ${h.edge_device_name}` : ''}</span>
                      <button type="button" className="tiny" onClick={() => void checkHealth(svc.id)}>Health</button>
                      <button type="button" className="tiny danger" onClick={() => void remove(svc.id)}>Remove</button>
                    </li>
                  )
                })}
              </ul>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}
