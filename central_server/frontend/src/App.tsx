import { BrowserRouter, Routes, Route, NavLink, Navigate } from 'react-router-dom'
import { Workflow, History, Globe, Server, FileCode2 } from 'lucide-react'

import { useTranslation } from './i18n'
import { ThemeToggle } from './components/ThemeToggle'
import ActionGraph from './pages/ActionGraph'
import PDDL from './pages/PDDL'
import TaskHistory from './pages/TaskHistory'
import AgentDashboard from './pages/AgentDashboard'

function App() {
  const { t, language, setLanguage } = useTranslation()

  const toggleLanguage = () => {
    setLanguage(language === 'ko' ? 'en' : 'ko')
  }

  return (
    <BrowserRouter>
      <div className="flex h-screen bg-base">
        {/* Sidebar */}
        <nav className="w-64 bg-surface text-primary flex flex-col border-r border-primary">
          <div className="h-12 border-b border-primary flex items-center justify-between px-4">
            <span className="text-sm font-semibold text-secondary">Fleet UI</span>
            <ThemeToggle />
          </div>

          <ul className="mt-4 flex-1">
            <NavItem to="/flows" icon={<Workflow size={20} />} label={t('nav.actionGraph')} />
            <NavItem to="/pddl" icon={<FileCode2 size={20} />} label={t('nav.pddl')} />
            <NavItem to="/agents" icon={<Server size={20} />} label={t('nav.agents')} />
            <NavItem to="/tasks" icon={<History size={20} />} label={t('nav.taskHistory')} />
          </ul>

          {/* Language Switcher */}
          <div className="p-4 border-t border-primary">
            <button
              onClick={toggleLanguage}
              className="w-full flex items-center justify-center gap-2 px-4 py-2 bg-elevated hover:bg-sunken rounded-lg transition-colors"
            >
              <Globe size={18} />
              <span>{language === 'ko' ? '한국어' : 'English'}</span>
            </button>
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

function NavItem({ to, icon, label }: { to: string; icon: React.ReactNode; label: string }) {
  return (
    <li>
      <NavLink
        to={to}
        className={({ isActive }) =>
          `flex items-center gap-3 px-4 py-3 hover:bg-elevated transition-colors ${
            isActive ? 'bg-elevated border-l-4 border-blue-500 text-blue-400' : 'text-secondary'
          }`
        }
      >
        {icon}
        <span>{label}</span>
      </NavLink>
    </li>
  )
}

export default App
