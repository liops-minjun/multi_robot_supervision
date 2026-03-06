import { useState, useEffect, useCallback, useMemo } from 'react'
import { RefreshCw, Play, Eye } from 'lucide-react'
import { useTranslation } from '../../i18n'
import { behaviorTreeApi, agentApi, capabilityApi, pddlApi } from '../../api/client'
import type { BehaviorTree, Agent, PlanResult, PlanningStateVar, GraphListItem } from '../../types'
import BehaviorTreeSelector from './components/BehaviorTreeSelector'
import AgentSelector from './components/AgentSelector'
import GoalEditor from './components/GoalEditor'
import PlanVisualization from './components/PlanVisualization'

interface AgentWithCaps {
  agent: Agent
  capabilities: string[]
  isOnline: boolean
}

export default function PDDL() {
  const { t } = useTranslation()

  const [treeList, setTreeList] = useState<GraphListItem[]>([])
  const [selectedBT, setSelectedBT] = useState<BehaviorTree | null>(null)
  const [agents, setAgents] = useState<AgentWithCaps[]>([])
  const [selectedAgentIds, setSelectedAgentIds] = useState<string[]>([])
  const [goalState, setGoalState] = useState<Record<string, string>>({})
  const [initialState, setInitialState] = useState<Record<string, string>>({})
  const [plan, setPlan] = useState<PlanResult | null>(null)
  const [isLoading, setIsLoading] = useState(false)
  const [isSolving, setIsSolving] = useState(false)

  const planningStates: PlanningStateVar[] = selectedBT?.planning_states ?? []

  // Extract required action types from selected BT
  const requiredActionTypes = useMemo(() => {
    if (!selectedBT) return []
    const types = new Set<string>()
    for (const step of selectedBT.steps) {
      if (step.action?.type) types.add(step.action.type)
    }
    return Array.from(types)
  }, [selectedBT])

  const loadData = useCallback(async () => {
    setIsLoading(true)
    try {
      const [btList, agentList, capData] = await Promise.all([
        behaviorTreeApi.list(),
        agentApi.list(),
        capabilityApi.listAll(),
      ])

      setTreeList(btList)

      // Build agent capabilities from listAll response
      const agentsWithCaps: AgentWithCaps[] = agentList.map((agent: Agent) => {
        const agentCaps = capData.action_types
          .filter(c => c.agent_ids.includes(agent.id))
          .map(c => c.action_type)
        return {
          agent,
          capabilities: agentCaps,
          isOnline: agent.status === 'online',
        }
      })
      setAgents(agentsWithCaps)
    } catch (err) {
      console.error('Failed to load data:', err)
    } finally {
      setIsLoading(false)
    }
  }, [])

  useEffect(() => {
    loadData()
  }, [loadData])

  // Fetch full BT when selection changes
  const handleSelectBT = useCallback(async (id: string) => {
    if (!id) {
      setSelectedBT(null)
      return
    }
    try {
      const bt = await behaviorTreeApi.get(id)
      setSelectedBT(bt)
    } catch (err) {
      console.error('Failed to load BT:', err)
      setSelectedBT(null)
    }
  }, [])

  // Reset goal/initial state when BT changes
  useEffect(() => {
    setGoalState({})
    setInitialState({})
    setPlan(null)
  }, [selectedBT?.id])

  const toggleAgent = (id: string) => {
    setSelectedAgentIds(prev =>
      prev.includes(id) ? prev.filter(a => a !== id) : [...prev, id]
    )
  }

  const handlePreview = async () => {
    if (!selectedBT || selectedAgentIds.length === 0 || Object.keys(goalState).length === 0) return
    setIsSolving(true)
    setPlan(null)
    try {
      const result = await pddlApi.preview({
        behavior_tree_id: selectedBT.id,
        initial_state: Object.keys(initialState).length > 0 ? initialState : undefined,
        goal_state: goalState,
        agent_ids: selectedAgentIds,
      })
      setPlan(result)
    } catch (err) {
      console.error('Preview failed:', err)
      setPlan({ assignments: [], is_valid: false, error_message: String(err), total_steps: 0, parallel_groups: 0 })
    } finally {
      setIsSolving(false)
    }
  }

  const handleSaveAndSolve = async () => {
    if (!selectedBT || selectedAgentIds.length === 0 || Object.keys(goalState).length === 0) return
    setIsSolving(true)
    setPlan(null)
    try {
      const problem = await pddlApi.createProblem({
        name: `${selectedBT.name} - ${new Date().toLocaleString()}`,
        behavior_tree_id: selectedBT.id,
        initial_state: Object.keys(initialState).length > 0 ? initialState : undefined,
        goal_state: goalState,
        agent_ids: selectedAgentIds,
      })
      const solved = await pddlApi.solveProblem(problem.id)
      if (solved.plan_result) {
        setPlan(solved.plan_result)
      }
    } catch (err) {
      console.error('Solve failed:', err)
      setPlan({ assignments: [], is_valid: false, error_message: String(err), total_steps: 0, parallel_groups: 0 })
    } finally {
      setIsSolving(false)
    }
  }

  const canSolve = selectedBT && selectedAgentIds.length > 0 && Object.keys(goalState).length > 0

  return (
    <div className="flex flex-col h-full bg-base">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-border">
        <h1 className="text-lg font-semibold text-primary">{t('pddl.title')}</h1>
        <button
          onClick={loadData}
          className="p-1.5 rounded-lg hover:bg-hover text-secondary transition-colors"
          title="Refresh"
        >
          <RefreshCw size={16} className={isLoading ? 'animate-spin' : ''} />
        </button>
      </div>

      {/* 3-Panel Layout */}
      <div className="flex-1 flex overflow-hidden">
        {/* Left Panel: BT Selection */}
        <div className="w-72 border-r border-border p-3 overflow-y-auto">
          <BehaviorTreeSelector
            treeList={treeList}
            selectedBT={selectedBT}
            onSelect={handleSelectBT}
          />
        </div>

        {/* Center Panel: Agent Selection + Goal */}
        <div className="flex-1 p-3 overflow-y-auto space-y-4">
          <AgentSelector
            agents={agents}
            selectedIds={selectedAgentIds}
            onToggle={toggleAgent}
            requiredActionTypes={requiredActionTypes}
          />

          <GoalEditor
            label={t('pddl.goalState')}
            stateVars={planningStates}
            values={goalState}
            onChange={setGoalState}
          />

          <GoalEditor
            label={t('pddl.initialState')}
            stateVars={planningStates}
            values={initialState}
            onChange={setInitialState}
          />

          {/* Action Buttons */}
          <div className="flex gap-2">
            <button
              onClick={handlePreview}
              disabled={!canSolve || isSolving}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-hover text-secondary text-sm hover:bg-accent/10 hover:text-accent disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
            >
              <Eye size={14} />
              {t('pddl.preview')}
            </button>
            <button
              onClick={handleSaveAndSolve}
              disabled={!canSolve || isSolving}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-accent text-white text-sm hover:bg-accent/80 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
            >
              <Play size={14} />
              {t('pddl.solve')}
            </button>
          </div>
        </div>

        {/* Right Panel: Results */}
        <div className="w-80 border-l border-border p-3 overflow-y-auto">
          <div className="text-xs font-medium text-secondary mb-2">{t('pddl.result')}</div>
          <PlanVisualization plan={plan} isLoading={isSolving} />
        </div>
      </div>
    </div>
  )
}
