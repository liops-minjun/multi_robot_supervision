import { BrowserRouter, Routes, Route, NavLink, Navigate } from 'react-router-dom'
import { Workflow, History, Globe, Server, FileCode2 } from 'lucide-react'

import { useTranslation } from './i18n'
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
      <div className="flex h-screen bg-[#0f0f1a]">
        {/* Sidebar */}
        <nav className="w-64 bg-[#16162a] text-white flex flex-col border-r border-[#2a2a4a]">
          <div className="h-12 border-b border-[#2a2a4a]" />

          <ul className="mt-4 flex-1">
            <NavItem to="/flows" icon={<Workflow size={20} />} label={t('nav.actionGraph')} />
            <NavItem to="/pddl" icon={<FileCode2 size={20} />} label={t('nav.pddl')} />
            <NavItem to="/agents" icon={<Server size={20} />} label={t('nav.agents')} />
            <NavItem to="/tasks" icon={<History size={20} />} label={t('nav.taskHistory')} />
          </ul>

          {/* Language Switcher */}
          <div className="p-4 border-t border-[#2a2a4a]">
            <button
              onClick={toggleLanguage}
              className="w-full flex items-center justify-center gap-2 px-4 py-2 bg-[#1a1a2e] hover:bg-[#2a2a4a] rounded-lg transition-colors"
            >
              <Globe size={18} />
              <span>{language === 'ko' ? '한국어' : 'English'}</span>
            </button>
          </div>
        </nav>

        {/* Main Content */}
        <main className="flex-1 overflow-auto">
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
          `flex items-center gap-3 px-4 py-3 hover:bg-[#1a1a2e] transition-colors ${
            isActive ? 'bg-[#1a1a2e] border-l-4 border-blue-500 text-blue-400' : 'text-gray-400'
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
