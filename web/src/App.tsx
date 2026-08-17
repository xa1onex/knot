import { Navigate, Outlet, Route, Routes, useNavigate } from 'react-router-dom'
import { getToken, setToken } from './lib/client'
import Login from './pages/Login'
import NodeApp from './shell/NodeApp'
import Credentials from './pages/Credentials'
import Activity from './pages/Activity'
import Settings from './pages/Settings'
import Sync from './pages/Sync'
import Services from './pages/Services'
import Compute from './pages/Compute'

function RequireAuth() {
  if (!getToken()) return <Navigate to="/login" replace />
  return <Outlet />
}

function AdminShell() {
  const nav = useNavigate()
  return (
    <div className="admin-shell">
      <header className="admin-bar">
        <a className="brand-mark" href="/">Node</a>
        <nav>
          <a href="/">Files</a>
          <a href="/settings">Settings</a>
          <a href="/settings/sync">Sync</a>
          <a href="/settings/services">Services</a>
          <a href="/settings/compute">Compute</a>
          <a href="/settings/credentials">Credentials</a>
          <a href="/settings/activity">Activity</a>
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
      <main className="admin-main">
        <Outlet />
      </main>
    </div>
  )
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route element={<RequireAuth />}>
        <Route path="/" element={<NodeApp />} />
        <Route element={<AdminShell />}>
          <Route path="/settings" element={<Settings />} />
          <Route path="/settings/sync" element={<Sync />} />
          <Route path="/settings/services" element={<Services />} />
          <Route path="/settings/compute" element={<Compute />} />
          <Route path="/settings/credentials" element={<Credentials />} />
          <Route path="/settings/activity" element={<Activity />} />
        </Route>
      </Route>
    </Routes>
  )
}
