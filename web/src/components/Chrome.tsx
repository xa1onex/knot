import type { ReactNode } from 'react'
import { NavLink, useLocation, useNavigate } from 'react-router-dom'
import { setToken } from '../lib/client'

const PRIMARY = [
  { to: '/', label: 'Files', hint: 'Open and send files', end: true },
  { to: '/computers', label: 'Computers', hint: 'Who is online' },
  { to: '/settings', label: 'Settings', hint: 'Updates and extras' },
] as const

const SETTINGS = [
  { to: '/settings', label: 'Updates', hint: 'Keep Node current', end: true },
  { to: '/settings/sync', label: 'Folder sync', hint: 'Keep two computers in step' },
  { to: '/settings/services', label: 'Websites', hint: 'Publish a site from a computer' },
  { to: '/settings/compute', label: 'Hardware', hint: 'CPU, RAM, jobs' },
  { to: '/settings/credentials', label: 'API keys', hint: 'For CLI and apps' },
  { to: '/settings/activity', label: 'History', hint: 'What changed' },
] as const

export default function Chrome({
  children,
  search,
  fill,
}: {
  children: ReactNode
  search?: ReactNode
  fill?: boolean
}) {
  const loc = useLocation()
  const nav = useNavigate()
  const inSettings = loc.pathname.startsWith('/settings')

  return (
    <div className={`app-shell ${fill ? 'fill' : ''}`}>
      <div className="glass-blobs" aria-hidden />
      <header className="app-nav" role="banner">
        <NavLink to="/" className="brand-lockup" aria-label="Node home">
          <span className="brand-mark">Node</span>
          <span className="brand-tag">your computers · your files</span>
        </NavLink>
        {search && <div className="app-nav-search">{search}</div>}
        <nav className="app-nav-links" aria-label="Main">
          {PRIMARY.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={'end' in item ? item.end : false}
              className={({ isActive }) => `nav-pill${isActive ? ' active' : ''}`}
              title={item.hint}
            >
              {item.label}
            </NavLink>
          ))}
        </nav>
        <button
          type="button"
          className="ghost"
          onClick={() => {
            setToken(null)
            nav('/login')
          }}
        >
          Log out
        </button>
      </header>
      {inSettings && (
        <nav className="subnav" aria-label="Settings">
          {SETTINGS.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={'end' in item ? item.end : false}
              className={({ isActive }) => `subnav-link${isActive ? ' active' : ''}`}
              title={item.hint}
            >
              {item.label}
            </NavLink>
          ))}
        </nav>
      )}
      {children}
    </div>
  )
}
