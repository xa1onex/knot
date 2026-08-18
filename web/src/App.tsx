import { Navigate, Outlet, Route, Routes } from 'react-router-dom'
import { getToken } from './lib/client'
import Login from './pages/Login'
import Overview from './pages/Overview'
import NodeApp from './shell/NodeApp'
import Credentials from './pages/Credentials'
import Activity from './pages/Activity'
import Settings from './pages/Settings'
import Sync from './pages/Sync'
import Services from './pages/Services'
import Compute from './pages/Compute'
import Devices from './pages/Devices'
import DashboardShell from './shell/DashboardShell'

function RequireAuth() {
  if (!getToken()) return <Navigate to="/login" replace />
  return <Outlet />
}

function PageFrame() {
  return (
    <DashboardShell>
      <Outlet />
    </DashboardShell>
  )
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route element={<RequireAuth />}>
        <Route path="/" element={<Overview />} />
        <Route path="/files" element={<NodeApp />} />
        <Route element={<PageFrame />}>
          <Route path="/computers" element={<Devices />} />
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
