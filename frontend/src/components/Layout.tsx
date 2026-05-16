import { NavLink, Outlet } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'

export default function Layout() {
  const { logout } = useAuth()

  return (
    <div className="app-layout">
      <aside className="sidebar">
        <div className="sidebar-logo">
          <span className="logo-icon">⚡</span>
          <span>AiRouter</span>
        </div>
        <nav className="sidebar-nav">
          <NavLink to="/" end className={({ isActive }) => (isActive ? 'nav-item active' : 'nav-item')}>
            <span className="nav-icon">📊</span> Dashboard
          </NavLink>
          <NavLink to="/keys" className={({ isActive }) => (isActive ? 'nav-item active' : 'nav-item')}>
            <span className="nav-icon">🔑</span> API Keys
          </NavLink>
          <NavLink to="/logs" className={({ isActive }) => (isActive ? 'nav-item active' : 'nav-item')}>
            <span className="nav-icon">📋</span> Logs
          </NavLink>
        </nav>
        <button className="btn btn-ghost sidebar-logout" onClick={logout}>
          Sign Out
        </button>
      </aside>
      <main className="main-content">
        <Outlet />
      </main>
    </div>
  )
}
