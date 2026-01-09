import { Trash2, ChevronDown, Activity, Users } from 'lucide-react'

// Condition Types
export interface Condition {
  id: string
  type: 'self_state' | 'agent_state' | 'group'
  operator?: 'AND' | 'OR'
  // For self_state
  state?: string
  stateOperator?: '==' | '!='
  // For agent_state
  agentId?: string  // specific agent or optional scope for all/any
  agentQuantifier?: 'all' | 'any' | 'specific'
  agentState?: string
  // For group (compound conditions)
  children?: Condition[]
}

export interface ConditionBuilderProps {
  conditions: Condition[]
  onChange: (conditions: Condition[]) => void
  availableStates: string[]
  availableAgents?: Array<{ id: string; name: string }>
  compact?: boolean
}

let conditionIdCounter = 0
const generateConditionId = () => `cond_${++conditionIdCounter}`

export default function ConditionBuilder({
  conditions,
  onChange,
  availableStates,
  availableAgents = [],
  compact = false,
}: ConditionBuilderProps) {
  const addCondition = (type: Condition['type']) => {
    const newCondition: Condition = {
      id: generateConditionId(),
      type,
      operator: conditions.length > 0 ? 'AND' : undefined,
    }

    if (type === 'self_state') {
      newCondition.state = availableStates[0] || 'idle'
      newCondition.stateOperator = '=='
    } else if (type === 'agent_state') {
      newCondition.agentId = availableAgents[0]?.id || ''
      newCondition.agentQuantifier = 'all'
      newCondition.agentState = availableStates[0] || 'idle'
    } else if (type === 'group') {
      newCondition.children = []
    }

    onChange([...conditions, newCondition])
  }

  const updateCondition = (id: string, updates: Partial<Condition>) => {
    onChange(conditions.map(c =>
      c.id === id ? { ...c, ...updates } : c
    ))
  }

  const removeCondition = (id: string) => {
    const newConditions = conditions.filter(c => c.id !== id)
    // Remove operator from first condition
    if (newConditions.length > 0 && newConditions[0].operator) {
      newConditions[0] = { ...newConditions[0], operator: undefined }
    }
    onChange(newConditions)
  }

  const renderCondition = (condition: Condition, index: number) => {
    const baseClasses = compact
      ? 'p-2 bg-[#16162a] rounded border border-[#2a2a4a]'
      : 'p-3 bg-[#16162a] rounded-lg border border-[#2a2a4a]'

    return (
      <div key={condition.id} className="space-y-2">
        {/* Operator (AND/OR) */}
        {index > 0 && (
          <div className="flex items-center justify-center">
            <select
              value={condition.operator || 'AND'}
              onChange={(e) => updateCondition(condition.id, { operator: e.target.value as 'AND' | 'OR' })}
              className="px-3 py-1 bg-[#1a1a2e] border border-[#3a3a5a] rounded text-xs text-purple-400 font-semibold focus:outline-none focus:border-purple-500"
            >
              <option value="AND">AND</option>
              <option value="OR">OR</option>
            </select>
          </div>
        )}

        <div className={baseClasses}>
          <div className="flex items-start gap-2">
            {/* Condition Type Icon */}
            <div className={`p-1.5 rounded ${
              condition.type === 'self_state' ? 'bg-blue-500/20' :
              condition.type === 'agent_state' ? 'bg-green-500/20' :
              'bg-purple-500/20'
            }`}>
              {condition.type === 'self_state' && <Activity className="w-3.5 h-3.5 text-blue-400" />}
              {condition.type === 'agent_state' && <Users className="w-3.5 h-3.5 text-green-400" />}
              {condition.type === 'group' && <ChevronDown className="w-3.5 h-3.5 text-purple-400" />}
            </div>

            {/* Condition Content */}
            <div className="flex-1 space-y-2">
              {condition.type === 'self_state' && (
                <SelfStateCondition
                  condition={condition}
                  onChange={(updates) => updateCondition(condition.id, updates)}
                  availableStates={availableStates}
                  compact={compact}
                />
              )}

              {condition.type === 'agent_state' && (
                <AgentStateCondition
                  condition={condition}
                  onChange={(updates) => updateCondition(condition.id, updates)}
                  availableStates={availableStates}
                  availableAgents={availableAgents}
                  compact={compact}
                />
              )}
            </div>

            {/* Remove Button */}
            <button
              onClick={() => removeCondition(condition.id)}
              className="p-1 text-gray-500 hover:text-red-400 hover:bg-red-500/10 rounded transition-colors"
            >
              <Trash2 className="w-3.5 h-3.5" />
            </button>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-3">
      {/* Condition List */}
      {conditions.length > 0 ? (
        <div className="space-y-2">
          {conditions.map((condition, index) => renderCondition(condition, index))}
        </div>
      ) : (
        <div className={`text-center py-4 text-gray-500 text-xs border border-dashed border-[#2a2a4a] rounded-lg`}>
          No conditions set (always execute)
        </div>
      )}

      {/* Add Condition Buttons */}
      <div className={`flex flex-wrap gap-2 ${compact ? 'pt-1' : 'pt-2'}`}>
        <button
          onClick={() => addCondition('self_state')}
          className="flex items-center gap-1.5 px-2.5 py-1.5 bg-blue-500/10 text-blue-400 rounded-lg text-xs hover:bg-blue-500/20 transition-colors border border-blue-500/20"
        >
          <Activity className="w-3 h-3" />
          My State
        </button>
        <button
          onClick={() => addCondition('agent_state')}
          className="flex items-center gap-1.5 px-2.5 py-1.5 bg-green-500/10 text-green-400 rounded-lg text-xs hover:bg-green-500/20 transition-colors border border-green-500/20"
        >
          <Users className="w-3 h-3" />
          Other Agents
        </button>
      </div>
    </div>
  )
}

// Self State Condition
function SelfStateCondition({
  condition,
  onChange,
  availableStates,
  compact,
}: {
  condition: Condition
  onChange: (updates: Partial<Condition>) => void
  availableStates: string[]
  compact: boolean
}) {
  return (
    <div className={`flex items-center gap-2 ${compact ? 'flex-wrap' : ''}`}>
      <span className="text-xs text-gray-400">My state</span>
      <select
        value={condition.stateOperator || '=='}
        onChange={(e) => onChange({ stateOperator: e.target.value as '==' | '!=' })}
        className="px-2 py-1 bg-[#1a1a2e] border border-[#2a2a4a] rounded text-xs text-white focus:outline-none focus:border-blue-500"
      >
        <option value="==">is</option>
        <option value="!=">is not</option>
      </select>
      <select
        value={condition.state || ''}
        onChange={(e) => onChange({ state: e.target.value })}
        className="flex-1 px-2 py-1 bg-[#1a1a2e] border border-blue-500/30 rounded text-xs text-blue-400 focus:outline-none focus:border-blue-500"
      >
        {availableStates.map(state => (
          <option key={state} value={state}>{state}</option>
        ))}
      </select>
    </div>
  )
}

// Agent State Condition
function AgentStateCondition({
  condition,
  onChange,
  availableStates,
  availableAgents,
  compact,
}: {
  condition: Condition
  onChange: (updates: Partial<Condition>) => void
  availableStates: string[]
  availableAgents: Array<{ id: string; name: string }>
  compact: boolean
}) {
  const agentValue = (() => {
    if (!condition.agentId) return ''
    const match = availableAgents.find(agent => agent.id === condition.agentId)
    return match ? match.name : condition.agentId
  })()

  const handleAgentChange = (value: string) => {
    const matchById = availableAgents.find(agent => agent.id === value)
    if (matchById) {
      onChange({ agentId: matchById.id })
      return
    }
    const matchByName = availableAgents.find(agent => agent.name === value)
    if (matchByName) {
      onChange({ agentId: matchByName.id })
      return
    }
    onChange({ agentId: value })
  }

  return (
    <div className="space-y-2">
      <div className={`flex items-center gap-2 ${compact ? 'flex-wrap' : ''}`}>
        <select
          value={condition.agentQuantifier || 'all'}
          onChange={(e) => onChange({ agentQuantifier: e.target.value as 'all' | 'any' | 'specific' })}
          className="px-2 py-1 bg-[#1a1a2e] border border-[#2a2a4a] rounded text-xs text-white focus:outline-none focus:border-green-500"
        >
          <option value="all">All</option>
          <option value="any">Any</option>
          <option value="specific">Specific</option>
        </select>
        <div className="flex-1">
          <input
            type="text"
            list={`agent-options-${condition.id}`}
            value={agentValue}
            onChange={(e) => handleAgentChange(e.target.value)}
            placeholder="Select or enter agent"
            className="w-full px-2 py-1 bg-[#1a1a2e] border border-green-500/30 rounded text-xs text-green-400 focus:outline-none focus:border-green-500"
          />
          <datalist id={`agent-options-${condition.id}`}>
            {availableAgents.map(agent => (
              <option key={agent.id} value={agent.name} />
            ))}
          </datalist>
        </div>
        <span className="text-xs text-gray-400">agents</span>
      </div>
      <div className="flex items-center gap-2">
        <span className="text-xs text-gray-400">are in state</span>
        <select
          value={condition.agentState || ''}
          onChange={(e) => onChange({ agentState: e.target.value })}
          className="flex-1 px-2 py-1 bg-[#1a1a2e] border border-green-500/30 rounded text-xs text-green-400 focus:outline-none focus:border-green-500"
        >
          {availableStates.map(state => (
            <option key={state} value={state}>{state}</option>
          ))}
        </select>
      </div>
      {condition.agentQuantifier === 'specific' && (
        <div className="flex items-center gap-2">
          <span className="text-xs text-gray-400">Agent ID:</span>
          <input
            type="text"
            value={condition.agentId || ''}
            onChange={(e) => onChange({ agentId: e.target.value })}
            placeholder="robot_001"
            className="flex-1 px-2 py-1 bg-[#1a1a2e] border border-[#2a2a4a] rounded text-xs text-white focus:outline-none focus:border-green-500"
          />
        </div>
      )}
    </div>
  )
}

// Helper function to convert conditions to expression string
export function conditionsToExpression(conditions: Condition[]): string {
  if (conditions.length === 0) return ''

  return conditions.map((c, i) => {
    let expr = ''

    if (c.type === 'self_state') {
      expr = `state ${c.stateOperator || '=='} "${c.state}"`
    } else if (c.type === 'agent_state') {
      const quantifier = c.agentQuantifier || 'all'
      const agentId = c.agentId ? `agent("${c.agentId}")` : 'agent'
      if (quantifier === 'specific') {
        expr = `${agentId}.state ${c.stateOperator || '=='} "${c.agentState}"`
      } else {
        expr = `${quantifier}_agents().state == "${c.agentState}"`
      }
    }

    if (i > 0 && c.operator) {
      return ` ${c.operator} ${expr}`
    }
    return expr
  }).join('')
}

// Compact inline preview
export function ConditionPreview({ conditions }: { conditions: Condition[] }) {
  if (conditions.length === 0) {
    return <span className="text-gray-500 text-[10px]">No conditions</span>
  }

  return (
    <div className="flex flex-wrap gap-1">
      {conditions.map((c, i) => (
        <span key={c.id} className="flex items-center gap-1">
          {i > 0 && (
            <span className="text-purple-400 text-[9px] font-semibold">{c.operator}</span>
          )}
          <span className={`px-1.5 py-0.5 rounded text-[9px] ${
            c.type === 'self_state' ? 'bg-blue-500/20 text-blue-400' :
            c.type === 'agent_state' ? 'bg-green-500/20 text-green-400' :
            'bg-purple-500/20 text-purple-400'
          }`}>
            {c.type === 'self_state' && `state ${c.stateOperator} ${c.state}`}
            {c.type === 'agent_state' && `${c.agentQuantifier} ${c.agentId || 'agents'} = ${c.agentState}`}
          </span>
        </span>
      ))}
    </div>
  )
}
