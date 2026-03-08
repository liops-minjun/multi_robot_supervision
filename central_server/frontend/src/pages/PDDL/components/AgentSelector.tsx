import { useState, useMemo } from 'react'
import { CheckSquare, Square, Wifi, WifiOff, Search, Sparkles, Wand2 } from 'lucide-react'
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
  recommendedIds?: string[]
  onSelectRecommended?: () => void
}

export default function AgentSelector({
  agents,
  selectedIds,
  onToggle,
  requiredActionTypes,
  recommendedIds = [],
  onSelectRecommended,
}: Props) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')

  const filtered = useMemo(() => {
    let result = agents
    if (search.trim()) {
      const q = search.toLowerCase()
      result = result.filter(a =>
        a.agent.name.toLowerCase().includes(q) ||
        a.agent.id.toLowerCase().includes(q) ||
        a.capabilities.some(c => c.toLowerCase().includes(q))
      )
    }
    // Sort: selected first, then by match score desc, then online first
    return [...result].sort((a, b) => {
      const aSelected = selectedIds.includes(a.agent.id) ? 1 : 0
      const bSelected = selectedIds.includes(b.agent.id) ? 1 : 0
      if (aSelected !== bSelected) return bSelected - aSelected
      const aMatch = requiredActionTypes?.filter(at => a.capabilities.includes(at)).length ?? 0
      const bMatch = requiredActionTypes?.filter(at => b.capabilities.includes(at)).length ?? 0
      if (aMatch !== bMatch) return bMatch - aMatch
      if (a.isOnline !== b.isOnline) return a.isOnline ? -1 : 1
      return 0
    })
  }, [agents, search, selectedIds, requiredActionTypes])

  const hasRequiredTask = (requiredActionTypes?.length ?? 0) > 0
  const recommendedSet = useMemo(() => new Set(recommendedIds), [recommendedIds])
  const recommendedAgents = useMemo(
    () => agents.filter(({ agent }) => recommendedSet.has(agent.id)),
    [agents, recommendedSet]
  )
  const recommendedCount = recommendedAgents.length
  const filteredRecommended = filtered.filter(({ agent }) => recommendedSet.has(agent.id))
  const filteredOthers = filtered.filter(({ agent }) => !recommendedSet.has(agent.id))
  const unselectedRecommendedCount = recommendedAgents.filter(({ agent }) => !selectedIds.includes(agent.id)).length

  return (
    <div className="space-y-4">
      <div className="rounded-2xl border border-border bg-base/70 p-3">
        <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
          <div className="relative flex-1">
            <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-muted" />
            <input
              type="text"
              className="w-full rounded-2xl border border-border bg-surface pl-10 pr-3 py-3 text-sm text-primary outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
              placeholder={t('pddl.searchAgents')}
              value={search}
              onChange={e => setSearch(e.target.value)}
            />
          </div>
          <div className="flex flex-wrap gap-2 text-[11px]">
            <span className="rounded-full bg-accent/10 px-3 py-1 font-medium text-accent">
              {t('pddl.agentSelectionSummary', { selected: String(selectedIds.length), total: String(agents.length) })}
            </span>
            <span className="rounded-full bg-emerald-500/10 px-3 py-1 font-medium text-emerald-400">
              {t('pddl.recommendedSummary', { count: String(recommendedCount) })}
            </span>
          </div>
        </div>
      </div>

      {hasRequiredTask && (
        <div className="rounded-2xl border border-blue-500/20 bg-blue-500/8 p-4">
          <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
            <div>
              <div className="flex items-center gap-2 text-sm font-semibold text-primary">
                <Sparkles size={16} className="text-blue-400" />
                {t('pddl.recommendedAgentsTitle')}
              </div>
              <p className="mt-1 text-sm leading-6 text-secondary">
                {recommendedAgents.length > 0
                  ? t('pddl.recommendedAgentsHint')
                  : t('pddl.noRecommendedAgentsHint')}
              </p>
            </div>
            {recommendedAgents.length > 0 && onSelectRecommended && (
              <button
                onClick={onSelectRecommended}
                disabled={unselectedRecommendedCount === 0}
                className="inline-flex items-center gap-2 rounded-2xl border border-blue-500/20 bg-white/5 px-4 py-3 text-sm font-medium text-blue-400 transition hover:bg-blue-500/10 disabled:cursor-not-allowed disabled:opacity-40"
              >
                <Wand2 size={16} />
                {t('pddl.selectRecommended')}
              </button>
            )}
          </div>

          {recommendedAgents.length > 0 && (
            <div className="mt-4 flex flex-wrap gap-2">
              {recommendedAgents.map(({ agent }) => (
                <button
                  key={`recommended-${agent.id}`}
                  onClick={() => onToggle(agent.id)}
                  className={`rounded-full border px-3 py-1.5 text-[11px] font-medium transition ${
                    selectedIds.includes(agent.id)
                      ? 'border-blue-500/30 bg-blue-500/15 text-blue-400'
                      : 'border-border bg-surface text-secondary hover:border-blue-500/20 hover:text-primary'
                  }`}
                >
                  {agent.name}
                </button>
              ))}
            </div>
          )}
        </div>
      )}

      <div className="grid max-h-[460px] gap-2 overflow-y-auto pr-1">
        {[...filteredRecommended, ...filteredOthers].map(({ agent, capabilities, isOnline }) => {
          const selected = selectedIds.includes(agent.id)
          const matchCount = requiredActionTypes?.filter(at => capabilities.includes(at)).length ?? 0
          const totalRequired = requiredActionTypes?.length ?? 0
          const missingCaps = requiredActionTypes?.filter(at => !capabilities.includes(at)) ?? []
          const isRecommended = recommendedSet.has(agent.id)

          return (
            <button
              key={agent.id}
              onClick={() => onToggle(agent.id)}
              className={`w-full rounded-2xl border p-3 text-left transition ${
                selected
                  ? 'border-accent/40 bg-accent/10 shadow-sm shadow-accent/10'
                  : 'border-border bg-base/70 hover:border-border-secondary hover:bg-surface'
              }`}
            >
              <div className="flex items-start gap-3">
                <div className={`mt-0.5 rounded-xl border p-2 ${selected ? 'border-accent/30 bg-accent/15 text-accent' : 'border-border bg-surface text-muted'}`}>
                  {selected ? (
                    <CheckSquare size={16} className="shrink-0" />
                  ) : (
                    <Square size={16} className="shrink-0" />
                  )}
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="text-sm font-semibold text-primary">{agent.name}</span>
                    <span className={`inline-flex items-center gap-1 rounded-full px-2 py-1 text-[11px] font-medium ${
                      isOnline ? 'bg-emerald-500/10 text-emerald-400' : 'bg-red-500/10 text-red-400'
                    }`}>
                      {isOnline ? <Wifi size={12} /> : <WifiOff size={12} />}
                      {isOnline ? t('agent.online') : t('agent.offline')}
                    </span>
                    {totalRequired > 0 && (
                      <span className={`rounded-full px-2 py-1 text-[11px] font-medium ${
                        matchCount === totalRequired ? 'bg-emerald-500/10 text-emerald-400' :
                        matchCount > 0 ? 'bg-amber-500/10 text-amber-400' :
                        'bg-red-500/10 text-red-400'
                      }`}>
                        {matchCount}/{totalRequired} {t('pddl.requiredActions')}
                      </span>
                    )}
                    {isRecommended && (
                      <span className="inline-flex items-center gap-1 rounded-full bg-blue-500/10 px-2 py-1 text-[11px] font-medium text-blue-400">
                        <Sparkles size={12} />
                        {t('pddl.recommended')}
                      </span>
                    )}
                  </div>

                  <div className="mt-1 truncate font-mono text-[11px] text-muted">{agent.id}</div>

                  {capabilities.length > 0 && (
                    <div className="mt-3 flex flex-wrap gap-2">
                      {capabilities.slice(0, 4).map(c => {
                        const capName = c.split('/').pop()
                        const matchesRequired = requiredActionTypes?.includes(c)
                        return (
                          <span
                            key={c}
                            className={`rounded-full px-2.5 py-1 text-[11px] ${
                              matchesRequired ? 'bg-emerald-500/10 text-emerald-400' : 'bg-surface text-secondary'
                            }`}
                          >
                            {capName}
                          </span>
                        )
                      })}
                      {capabilities.length > 4 && (
                        <span className="rounded-full bg-surface px-2.5 py-1 text-[11px] text-muted">
                          +{capabilities.length - 4}
                        </span>
                      )}
                    </div>
                  )}

                  {missingCaps.length > 0 && (
                    <div className="mt-3 rounded-2xl bg-red-500/8 px-3 py-2 text-[11px] text-red-400">
                      <span className="font-medium">{t('pddl.missingCapabilities')}:</span>{' '}
                      {missingCaps.map(c => c.split('/').pop()).join(', ')}
                    </div>
                  )}
                </div>
              </div>
            </button>
          )
        })}
        {agents.length === 0 && (
          <p className="rounded-2xl border border-dashed border-border bg-base/40 py-8 text-center text-sm italic text-muted">
            {t('pddl.noAgentsAvailable')}
          </p>
        )}
        {agents.length > 0 && filtered.length === 0 && (
          <p className="rounded-2xl border border-dashed border-border bg-base/40 py-8 text-center text-sm italic text-muted">
            {t('pddl.noMatchingAgents')}
          </p>
        )}
      </div>
    </div>
  )
}
