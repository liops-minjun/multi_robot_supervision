import { CheckSquare, Square, Wifi, WifiOff } from 'lucide-react'
import type { Agent } from '../../../types'
import { useTranslation } from '../../../i18n'

interface AgentWithCaps {
  agent: Agent
  capabilities: string[]
  isOnline: boolean
}

interface Props {
  agents: AgentWithCaps[]
  selectedIds: string[]
  onToggle: (id: string) => void
  requiredActionTypes?: string[]
}

export default function AgentSelector({ agents, selectedIds, onToggle, requiredActionTypes }: Props) {
  const { t } = useTranslation()

  return (
    <div>
      <label className="text-xs font-medium text-secondary mb-1 block">{t('pddl.selectAgents')}</label>
      <div className="space-y-1 max-h-[250px] overflow-y-auto">
        {agents.map(({ agent, capabilities, isOnline }) => {
          const selected = selectedIds.includes(agent.id)
          const missingCaps = requiredActionTypes?.filter(at => !capabilities.includes(at)) ?? []

          return (
            <button
              key={agent.id}
              onClick={() => onToggle(agent.id)}
              className={`w-full flex items-start gap-2 px-2 py-1.5 rounded text-xs text-left transition-colors ${
                selected ? 'bg-accent/10 border border-accent/30' : 'bg-hover border border-transparent'
              }`}
            >
              {selected ? (
                <CheckSquare size={14} className="text-accent mt-0.5 shrink-0" />
              ) : (
                <Square size={14} className="text-muted mt-0.5 shrink-0" />
              )}
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-1.5">
                  <span className="font-medium text-primary truncate">{agent.name}</span>
                  {isOnline ? (
                    <Wifi size={10} className="text-green-400 shrink-0" />
                  ) : (
                    <WifiOff size={10} className="text-red-400 shrink-0" />
                  )}
                </div>
                <div className="text-muted truncate">{agent.id}</div>
                {capabilities.length > 0 && (
                  <div className="flex flex-wrap gap-1 mt-0.5">
                    {capabilities.slice(0, 3).map(c => (
                      <span
                        key={c}
                        className={`px-1 py-0 rounded text-[10px] ${
                          missingCaps.length === 0 ? 'bg-green-500/10 text-green-400' : 'bg-base text-muted'
                        }`}
                      >
                        {c.split('/').pop()}
                      </span>
                    ))}
                    {capabilities.length > 3 && (
                      <span className="text-muted text-[10px]">+{capabilities.length - 3}</span>
                    )}
                  </div>
                )}
                {missingCaps.length > 0 && (
                  <div className="text-red-400 text-[10px] mt-0.5">
                    Missing: {missingCaps.map(c => c.split('/').pop()).join(', ')}
                  </div>
                )}
              </div>
            </button>
          )
        })}
        {agents.length === 0 && (
          <p className="text-xs text-muted italic py-2 text-center">No agents available</p>
        )}
      </div>
    </div>
  )
}
