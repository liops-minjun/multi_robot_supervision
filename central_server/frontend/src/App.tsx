import { useState } from 'react'
import { BrowserRouter, Routes, Route, NavLink, Navigate } from 'react-router-dom'
import { Workflow, History, Globe, Server, FileCode2, User } from 'lucide-react'

import { useTranslation } from './i18n'
import { useUserStore } from './stores/userStore'
import { ThemeToggle } from './components/ThemeToggle'
import { UsernameDialog } from './components/UsernameDialog'
import ActionGraph from './pages/ActionGraph'
import PDDL from './pages/PDDL'
import TaskHistory from './pages/TaskHistory'
import AgentDashboard from './pages/AgentDashboard'

function App() {
  const { t, language, setLanguage } = useTranslation()
  const { username, clearUser } = useUserStore()
  const [isNavExpanded, setIsNavExpanded] = useState(false)

  const toggleLanguage = () => {
    setLanguage(language === 'ko' ? 'en' : 'ko')
  }

  return (
    <BrowserRouter>
      <UsernameDialog />
      <div className="flex h-screen bg-base">
        {/* Edge hover trigger for collapsed nav */}
        {!isNavExpanded && (
          <div
            className="fixed left-0 top-0 z-40 h-screen w-3"
            onMouseEnter={() => setIsNavExpanded(true)}
          />
        )}

        {/* Sidebar */}
        <nav
          className={`bg-surface text-primary flex flex-col border-r border-primary transition-[width] duration-200 ease-out ${
            isNavExpanded ? 'w-64' : 'w-[68px]'
          }`}
          onMouseEnter={() => setIsNavExpanded(true)}
          onMouseLeave={() => setIsNavExpanded(false)}
        >
          <div className={`h-12 border-b border-primary flex items-center ${isNavExpanded ? 'justify-between px-4' : 'justify-center px-2'}`}>
            {isNavExpanded ? (
              <span className="text-sm font-semibold text-secondary">Fleet UI</span>
            ) : (
              <span className="text-xs font-semibold text-secondary tracking-wide">FMS</span>
            )}
            <ThemeToggle />
          </div>

          <ul className="mt-4 flex-1">
            <NavItem to="/flows" icon={<Workflow size={20} />} label={t('nav.actionGraph')} expanded={isNavExpanded} />
            <NavItem to="/pddl" icon={<FileCode2 size={20} />} label={t('nav.pddl')} expanded={isNavExpanded} />
            <NavItem to="/agents" icon={<Server size={20} />} label={t('nav.agents')} expanded={isNavExpanded} />
            <NavItem to="/tasks" icon={<History size={20} />} label={t('nav.taskHistory')} expanded={isNavExpanded} />
          </ul>

          {/* User & Language */}
          <div className={`border-t border-primary ${isNavExpanded ? 'p-4 space-y-2' : 'p-2 space-y-1.5'}`}>
            {isNavExpanded ? (
              <>
                {username && (
                  <div className="flex items-center justify-between px-2 py-1">
                    <div className="flex items-center gap-2 min-w-0">
                      <User size={14} className="text-blue-400 shrink-0" />
                      <span className="text-sm text-secondary truncate">{username}</span>
                    </div>
                    <button
                      onClick={clearUser}
                      className="text-xs text-muted hover:text-secondary shrink-0 ml-2"
                    >
                      변경
                    </button>
                  </div>
                )}
                <button
                  onClick={toggleLanguage}
                  className="w-full flex items-center justify-center gap-2 px-4 py-2 bg-elevated hover:bg-sunken rounded-lg transition-colors"
                >
                  <Globe size={18} />
                  <span>{language === 'ko' ? '한국어' : 'English'}</span>
                </button>
              </>
            ) : (
              <>
                {username && (
                  <button
                    onClick={clearUser}
                    className="w-full h-10 flex items-center justify-center bg-elevated hover:bg-sunken rounded-lg transition-colors text-secondary"
                    title={`${username} (사용자 변경)`}
                  >
                    <User size={16} className="text-blue-400" />
                  </button>
                )}
                <button
                  onClick={toggleLanguage}
                  className="w-full h-10 flex items-center justify-center bg-elevated hover:bg-sunken rounded-lg transition-colors text-secondary"
                  title={language === 'ko' ? '한국어' : 'English'}
                >
                  <Globe size={18} />
                </button>
              </>
            )}
          </div>
        </nav>

        {/* Main Content */}
        <main className="flex-1 overflow-auto bg-base">
          <Routes>
            <Route path="/" element={<Navigate to="/flows" replace />} />
            <Route path="/flows" element={<ActionGraph />} />
            <Route path="/pddl" element={<PDDL />} />
            <Route path="/agents" element={<AgentDashboard />} />
            <Route path="/tasks" element={<TaskHistory />} />
          </Routes>
        </main>
      </div>
    </BrowserRouter>
  )
}

function NavItem({
  to,
  icon,
  label,
  expanded,
}: {
  to: string
  icon: React.ReactNode
  label: string
  expanded: boolean
}) {
  return (
    <li>
      <NavLink
        to={to}
        title={expanded ? undefined : label}
        className={({ isActive }) =>
          `flex items-center py-3 hover:bg-elevated transition-colors ${
            expanded ? 'gap-3 px-4 justify-start' : 'px-0 justify-center'
          } ${
            isActive
              ? expanded
                ? 'bg-elevated border-l-4 border-blue-500 text-blue-400'
                : 'bg-elevated text-blue-400'
              : 'text-secondary'
          }`
        }
      >
        {icon}
        {expanded && <span>{label}</span>}
      </NavLink>
    </li>
  )
}

export default App
