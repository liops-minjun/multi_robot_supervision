import { memo, useState, useEffect } from 'react'
import { Handle, Position, NodeProps, useReactFlow } from 'reactflow'
import { ChevronDown, ChevronUp, Plus, X, Filter, Settings, Loader2 } from 'lucide-react'
import type { StartStateConfig, EndStateConfig, ActionField, ActionOutcome, DuringStateTarget } from '../../../types'
import { capabilityApi } from '../../../api/client'

// Convert capability goal_schema to ActionField format
const schemaToFields = (schema: Record<string, unknown> | null): ActionField[] => {
  if (!schema) return []

  const fields: ActionField[] = []

  // Handle ROS2 message schema format
  // Schema format: { "field_name": { "type": "ros2_type", ... }, ... }
  for (const [name, fieldInfo] of Object.entries(schema)) {
    if (typeof fieldInfo === 'object' && fieldInfo !== null) {
      const info = fieldInfo as Record<string, unknown>
      fields.push({
        name,
        type: (info.type as string) || 'string',
        default: info.default as string | undefined,
        is_array: Boolean(info.is_array),
        is_constant: false,
      })
    } else if (typeof fieldInfo === 'string') {
      // Simple format: { "field_name": "type" }
      fields.push({
        name,
        type: fieldInfo,
        is_array: false,
        is_constant: false,
      })
    }
  }

  return fields
}

// Map ROS2 types to input types
type InputType = 'number' | 'text' | 'checkbox' | 'pose' | 'trajectory' | 'complex'

const getInputType = (rosType: string, isArray: boolean): InputType => {
  const numericTypes = ['int8', 'int16', 'int32', 'int64', 'uint8', 'uint16', 'uint32', 'uint64', 'float32', 'float64', 'double']

  // Check for complex ROS2 message types (contain '/')
  if (rosType.includes('/')) {
    const lowerType = rosType.toLowerCase()
    // Pose-related types
    if (lowerType.includes('pose') || lowerType.includes('point') || lowerType.includes('transform')) {
      return 'pose'
    }
    // Trajectory types
    if (lowerType.includes('trajectory') || lowerType.includes('jointtrajectory')) {
      return 'trajectory'
    }
    // Other complex types
    return 'complex'
  }

  if (rosType === 'bool') return 'checkbox'
  if (numericTypes.some(t => rosType === t)) return 'number'
  if (rosType === 'string') return 'text'

  // Arrays of primitives
  if (isArray) return 'complex'

  return 'text'
}

// Check if a type can use waypoint selection
const canUseWaypoint = (rosType: string): boolean => {
  const lowerType = rosType.toLowerCase()
  return lowerType.includes('pose') || lowerType.includes('point') || lowerType.includes('transform')
}

// Color palette for end states
const OUTCOME_COLORS: Record<ActionOutcome, string> = {
  success: '#22c55e',
  failed: '#ef4444',
  aborted: '#ef4444',
  cancelled: '#6b7280',
  timeout: '#f59e0b',
  rejected: '#ef4444',
}

const END_STATE_COLORS: Record<string, string> = {
  success: '#22c55e',
  completed: '#22c55e',
  idle: '#22c55e',
  error: '#ef4444',
  failed: '#ef4444',
  timeout: '#f59e0b',
  partial: '#eab308',
  cancelled: '#6b7280',
}

const OUTCOME_OPTIONS: Array<{ value: ActionOutcome; label: string }> = [
  { value: 'success', label: 'Success' },
  { value: 'failed', label: 'Failed' },
  { value: 'aborted', label: 'Aborted' },
  { value: 'cancelled', label: 'Cancelled' },
  { value: 'timeout', label: 'Timeout' },
  { value: 'rejected', label: 'Rejected' },
]

const DURING_TARGET_OPTIONS: Array<{ value: NonNullable<DuringStateTarget['target_type']>; label: string }> = [
  { value: 'self', label: 'Self' },
  { value: 'all', label: 'All' },
  { value: 'agent', label: 'Agent' },
]

const inferOutcomeFromEndState = (endState: EndStateConfig): ActionOutcome => {
  const label = (endState.label || '').toLowerCase()
  const stateValue = (endState.state || '').toLowerCase()

  if (label.includes('timeout') || stateValue.includes('timeout')) return 'timeout'
  if (label.includes('cancel') || stateValue.includes('cancel')) return 'cancelled'
  if (label.includes('abort') || stateValue.includes('abort')) return 'aborted'
  if (label.includes('fail') || label.includes('error') || stateValue.includes('fail') || stateValue.includes('error')) return 'failed'
  if (label.includes('success') || label.includes('complete') || stateValue.includes('idle') || stateValue.includes('ready')) return 'success'
  return 'success'
}

const getEndStateColor = (endState: EndStateConfig): string => {
  if (endState.color) {
    return endState.color
  }
  if (endState.outcome && OUTCOME_COLORS[endState.outcome]) {
    return OUTCOME_COLORS[endState.outcome]
  }
  if (endState.label) {
    const lowerLabel = endState.label.toLowerCase()
    if (lowerLabel.includes('success') || lowerLabel.includes('complete')) return '#22c55e'
    if (lowerLabel.includes('error') || lowerLabel.includes('fail') || lowerLabel.includes('abort')) return '#ef4444'
    if (lowerLabel.includes('timeout')) return '#f59e0b'
  }
  return END_STATE_COLORS[endState.state.toLowerCase()] || '#6b7280'
}

interface StateActionNodeData {
  label: string
  subtype: string
  color: string
  actionType?: string
  server?: string
  // Job configuration
  jobName?: string
  params?: Record<string, unknown>
  // New State ActionGraph Configuration
  startStates?: StartStateConfig[]
  duringStates?: string[]
  duringStateTargets?: DuringStateTarget[]
  endStates?: EndStateConfig[]
  // Available data
  availableStates?: string[]
  availableAgents?: Array<{ id: string; name: string }>
  availableWaypoints?: Array<{ id: string; name: string }>
}

type NormalizedDuringStateTarget = {
  state: string
  target_type: NonNullable<DuringStateTarget['target_type']>
  agent_id?: string
}

const StateActionNode = memo(({ id, data, selected }: NodeProps<StateActionNodeData>) => {
  const color = data.color || '#3b82f6'
  const { setNodes } = useReactFlow()
  const states = data.availableStates || []
  const [expandedSection, setExpandedSection] = useState<string | null>(null)

  // Action fields from API
  const [goalFields, setGoalFields] = useState<ActionField[]>([])
  const [resultFields, setResultFields] = useState<ActionField[]>([])
  const [isLoadingFields, setIsLoadingFields] = useState(false)

  // Generate End States from result fields
  // ROS2 Action Server outcomes: SUCCEEDED, ABORTED, CANCELED
  const generateEndStatesFromResults = (results: ActionField[]): EndStateConfig[] => {
    const endStates: EndStateConfig[] = []
    const now = Date.now()

    // Find appropriate states from available states
    const successState = states.includes('idle') ? 'idle' :
                         states.includes('completed') ? 'completed' : states[0] || 'idle'
    const errorState = states.includes('error') ? 'error' :
                       states.includes('failed') ? 'failed' : states[0] || 'error'

    // 1. SUCCESS - Always present (SUCCEEDED)
    endStates.push({
      id: `end-success-${now}`,
      state: successState,
      label: 'Success',
      outcome: 'success',
      color: OUTCOME_COLORS.success,
    })

    // 2. ABORTED/FAILURE - Always present (action can always fail)
    endStates.push({
      id: `end-failed-${now + 1}`,
      state: errorState,
      label: 'Failed',
      outcome: 'failed',
      color: OUTCOME_COLORS.failed,
    })

    endStates.push({
      id: `end-aborted-${now + 2}`,
      state: errorState,
      label: 'Aborted',
      outcome: 'aborted',
      color: OUTCOME_COLORS.aborted,
    })

    // 3. Check result fields for additional specific outcomes
    const hasErrorCode = results.some(f =>
      f.name.includes('error_code') || f.name.includes('result_code') || f.name.includes('error_id')
    )
    const hasTimeout = results.some(f =>
      f.name.includes('timeout') || f.name.includes('timed_out')
    )

    // Add specific error states if result fields suggest them
    if (hasErrorCode) {
      // If there's an error code, we might have specific error types
      endStates.push({
        id: `end-error-${now + 3}`,
        state: errorState,
        label: 'Error',
        outcome: 'failed',
        color: '#dc2626',
      })
    }

    if (hasTimeout) {
      endStates.push({
        id: `end-timeout-${now + 4}`,
        state: errorState,
        label: 'Timeout',
        outcome: 'timeout',
        color: OUTCOME_COLORS.timeout,
      })
    }

    // 4. CANCELED - Optional but common (client cancellation)
    endStates.push({
      id: `end-cancelled-${now + 5}`,
      state: states.includes('cancelled') ? 'cancelled' : errorState,
      label: 'Cancelled',
      outcome: 'cancelled',
      color: OUTCOME_COLORS.cancelled,
    })

    return endStates
  }

  // Fetch action schema from discovered capabilities
  useEffect(() => {
    if (!data.actionType) {
      setGoalFields([])
      setResultFields([])
      return
    }

    setIsLoadingFields(true)

    // Get schema from discovered capabilities
    capabilityApi.getByActionType(data.actionType)
      .then((capData) => {
        if (capData.robots && capData.robots.length > 0) {
          // Use schema from first robot with this capability
          const firstRobot = capData.robots[0]
          const goalFields = schemaToFields(firstRobot.goal_schema)
          setGoalFields(goalFields)
          setResultFields([])

          // Auto-generate basic End States
          if (!data.endStates || data.endStates.length === 0) {
            const autoEndStates = generateEndStatesFromResults([])
            updateData('endStates', autoEndStates)
          }
        } else {
          setGoalFields([])
          setResultFields([])
        }
      })
      .catch((err) => {
        console.error('Failed to fetch capability schema:', err)
        setGoalFields([])
        setResultFields([])
      })
      .finally(() => {
        setIsLoadingFields(false)
      })
  }, [data.actionType])

  // State configurations with defaults
  const startStates = data.startStates || []
  const duringStateTargets: NormalizedDuringStateTarget[] = (data.duringStateTargets && data.duringStateTargets.length > 0)
    ? data.duringStateTargets.map(target => ({
      ...target,
      target_type: (target.target_type || 'self') as NormalizedDuringStateTarget['target_type'],
    }))
    : (data.duringStates || [])
      .filter(Boolean)
      .slice(0, 1)
      .map(state => ({ state, target_type: 'self' as const }))
  const endStates = data.endStates || []

  const updateData = (field: string, value: unknown) => {
    setNodes((nds) => nds.map((n) =>
      n.id === id ? { ...n, data: { ...n.data, [field]: value } } : n
    ))
  }

  // Available agent types, robots, and waypoints for conditions
  const availableAgents = data.availableAgents || []
  const availableWaypoints = data.availableWaypoints || []

  // Job parameters - always default to empty string
  const jobName = data.jobName ?? ''
  const params = data.params || {}

  // Update job parameter
  const updateParam = (key: string, value: unknown) => {
    updateData('params', { ...params, [key]: value })
  }

  // Add new start state
  const addStartState = () => {
    const newStartState: StartStateConfig = {
      id: `start-${Date.now()}`,
      quantifier: 'self',
      state: states[0] || 'idle',
      operator: startStates.length > 0 ? 'and' : undefined,
    }
    updateData('startStates', [...startStates, newStartState])
  }

  // Remove start state
  const removeStartState = (startStateId: string) => {
    const filtered = startStates.filter(ss => ss.id !== startStateId)
    // Remove operator from first item if it exists
    if (filtered.length > 0 && filtered[0].operator) {
      filtered[0] = { ...filtered[0], operator: undefined }
    }
    updateData('startStates', filtered)
  }

  // Update start state field
  const updateStartStateField = (startStateId: string, field: keyof StartStateConfig, value: string | undefined) => {
    updateData('startStates', startStates.map((ss, idx) => {
      if (ss.id !== startStateId) return ss
      const updated = { ...ss, [field]: value }
      // If changing to 'self', clear agent and legacy robotId
      if (field === 'quantifier' && value === 'self') {
        updated.agentId = undefined
        updated.robotId = undefined
      }
      // If changing to 'every' or 'any', set default agent and clear legacy robotId
      if (field === 'quantifier' && (value === 'every' || value === 'any')) {
        updated.robotId = undefined
        if (!ss.agentId && availableAgents.length > 0) {
          updated.agentId = availableAgents[0].id
        }
      }
      // If changing to 'specific', set default agent and clear legacy robotId
      if (field === 'quantifier' && value === 'specific') {
        updated.robotId = undefined
        if (!ss.agentId && availableAgents.length > 0) {
          updated.agentId = availableAgents[0].id
        }
      }
      // First item should not have operator
      if (idx === 0) {
        updated.operator = undefined
      }
      return updated
    }))
  }

  // Add new end state
  const addEndState = () => {
    const newEndState: EndStateConfig = {
      id: `end-${Date.now()}`,
      state: states[0] || 'idle',
      label: `State ${endStates.length + 1}`,
      outcome: 'success',
      color: OUTCOME_COLORS.success,
    }
    updateData('endStates', [...endStates, newEndState])
  }

  // Remove end state
  const removeEndState = (endStateId: string) => {
    if (endStates.length > 1) {
      updateData('endStates', endStates.filter(es => es.id !== endStateId))
    }
  }

  // Update end state
  const updateEndState = (endStateId: string, field: string, value: string) => {
    updateData('endStates', endStates.map(es => {
      if (es.id !== endStateId) return es
      const updated = { ...es, [field]: value }
      if (field === 'state' || field === 'label' || field === 'outcome') {
        updated.color = getEndStateColor(updated)
      }
      return updated
    }))
  }

  // Add during state
  const addDuringState = () => {
    if (states.length === 0) {
      return
    }
    updateData('duringStateTargets', [
      ...duringStateTargets,
      { state: states[0], target_type: 'self' },
    ])
  }

  // Calculate handle positions for end states (positioned in End States section area)
  const getEndStateHandlePosition = (index: number, total: number) => {
    const startY = 70 // Start at 70% (End States section starts around here)
    const endY = 92   // End at 92% (just above footer)
    if (total === 1) return 80
    return startY + (index * (endY - startY) / (total - 1))
  }

  const hasStartConditions = startStates.length > 0

  return (
    <div
      className={`
        min-w-[260px] max-w-[320px] rounded-lg overflow-hidden
        bg-[#1e1e2e] border-2
        shadow-lg
        ${selected ? 'border-white/60 shadow-xl shadow-blue-500/20' : 'border-[#2a2a4a]'}
        transition-all duration-150
      `}
    >
      {/* Input Handle - Left side, aligned with Start States section */}
      <Handle
        type="target"
        position={Position.Left}
        id="in"
        className="!w-3 !h-3 !bg-cyan-500 !border-2 !border-cyan-300 !rounded-full"
        style={{ left: -6, top: '18%' }}
        title="Input (from previous action)"
      />

      {/* Header - Action Server & Job Name */}
      <div style={{ backgroundColor: `${color}25` }}>
        {/* Action Server Name (primary) & Action Type (secondary) */}
        <div className="px-3 pt-2 pb-1 flex items-center gap-2">
          <div
            className="w-2 h-2 rounded-sm flex-shrink-0"
            style={{ backgroundColor: color }}
          />
          <div className="flex flex-col min-w-0 flex-1">
            {/* Action Server (primary identifier) */}
            <span
              className="text-[10px] font-mono text-white truncate"
              title={data.server || data.subtype}
            >
              {(data.server || data.subtype).replace(/^\//, '')}
            </span>
            {/* Action Type (secondary info) */}
            {data.actionType && (
              <span
                className="text-[8px] px-1 py-0.5 rounded truncate w-fit"
                style={{ backgroundColor: `${color}30`, color: color }}
                title={data.actionType}
              >
                {data.actionType.split('/').pop()}
              </span>
            )}
          </div>
          {hasStartConditions && (
            <div className="flex items-center gap-1 px-1.5 py-0.5 bg-cyan-500/20 rounded flex-shrink-0">
              <Filter className="w-2.5 h-2.5 text-cyan-400" />
              <span className="text-[9px] text-cyan-400">{startStates.length}</span>
            </div>
          )}
        </div>
        {/* Editable Job Name */}
        <div className="px-3 pb-2">
          <input
            type="text"
            value={jobName}
            onChange={(e) => {
              e.stopPropagation()
              updateData('jobName', e.target.value)
            }}
            onClick={(e) => e.stopPropagation()}
            className="w-full text-sm font-semibold text-white bg-transparent border-b border-transparent hover:border-gray-600 focus:border-white focus:outline-none"
            placeholder="Enter job name..."
          />
        </div>
      </div>

      {/* Job Parameters Section - from ROS2 Action interface */}
      {(goalFields.length > 0 || isLoadingFields) && (
        <div className="border-b border-[#2a2a4a]">
          <button
            onClick={(e) => { e.stopPropagation(); setExpandedSection(expandedSection === 'params' ? null : 'params') }}
            className="w-full px-3 py-1.5 flex items-center justify-between hover:bg-[#2a2a4a]/50 transition-colors"
          >
            <div className="flex items-center gap-2">
              <Settings className="w-3 h-3 text-amber-500" />
              <span className="text-[10px] text-amber-400 uppercase tracking-wider font-medium">Parameters</span>
              {isLoadingFields ? (
                <Loader2 className="w-3 h-3 text-amber-500 animate-spin" />
              ) : (
                <span className="text-[9px] text-gray-600">({goalFields.length})</span>
              )}
            </div>
            {expandedSection === 'params' ? (
              <ChevronUp className="w-3 h-3 text-gray-500" />
            ) : (
              <ChevronDown className="w-3 h-3 text-gray-500" />
            )}
          </button>
          {expandedSection === 'params' && (
            <div className="px-3 pb-2 space-y-2">
              {isLoadingFields ? (
                <div className="flex items-center gap-2 py-2">
                  <Loader2 className="w-3 h-3 text-amber-500 animate-spin" />
                  <span className="text-[9px] text-gray-500">Loading action interface...</span>
                </div>
              ) : goalFields.length === 0 ? (
                <p className="text-[9px] text-gray-600 italic">No parameters required</p>
              ) : (
                goalFields.map((field) => {
                  const inputType = getInputType(field.type, field.is_array)
                  const useWaypoint = canUseWaypoint(field.type)

                  return (
                    <div key={field.name} className="space-y-1">
                      {/* Field header */}
                      <div className="flex items-center gap-2">
                        <label className="text-[9px] text-gray-400 font-medium">{field.name}</label>
                        <span className="text-[8px] text-gray-600 font-mono px-1 py-0.5 bg-gray-800 rounded">
                          {field.type}{field.is_array ? '[]' : ''}
                        </span>
                      </div>

                      {/* Input based on type */}
                      {inputType === 'checkbox' ? (
                        <div className="flex items-center gap-2">
                          <input
                            type="checkbox"
                            checked={Boolean(params[field.name])}
                            onChange={(e) => {
                              e.stopPropagation()
                              updateParam(field.name, e.target.checked)
                            }}
                            onClick={(e) => e.stopPropagation()}
                            className="w-4 h-4 accent-amber-500"
                          />
                          <span className="text-[9px] text-gray-500">{params[field.name] ? 'true' : 'false'}</span>
                        </div>
                      ) : inputType === 'number' ? (
                        <input
                          type="number"
                          value={(params[field.name] as number) ?? ''}
                          onChange={(e) => {
                            e.stopPropagation()
                            updateParam(field.name, e.target.value === '' ? undefined : parseFloat(e.target.value))
                          }}
                          onClick={(e) => e.stopPropagation()}
                          className="w-full px-2 py-1.5 bg-[#16162a] border border-amber-500/30 rounded text-[10px] text-white focus:outline-none focus:border-amber-500"
                          placeholder={field.default || '0'}
                          step="any"
                        />
                      ) : inputType === 'pose' ? (
                        <div className="space-y-1">
                          {/* Waypoint selection or manual input */}
                          {useWaypoint && availableWaypoints.length > 0 ? (
                            <select
                              value={(params[`${field.name}_waypoint`] as string) || ''}
                              onChange={(e) => {
                                e.stopPropagation()
                                updateParam(`${field.name}_waypoint`, e.target.value)
                                // Clear manual values when waypoint is selected
                                if (e.target.value) {
                                  updateParam(field.name, undefined)
                                }
                              }}
                              onClick={(e) => e.stopPropagation()}
                              className="w-full px-2 py-1.5 bg-[#16162a] border border-green-500/30 rounded text-[10px] text-green-400 focus:outline-none"
                            >
                              <option value="" className="bg-[#1a1a2e]">Select waypoint...</option>
                              {availableWaypoints.map(wp => (
                                <option key={wp.id} value={wp.id} className="bg-[#1a1a2e]">{wp.name}</option>
                              ))}
                            </select>
                          ) : (
                            <div className="text-[8px] text-gray-500 italic">No waypoints available</div>
                          )}
                          {/* Manual pose input (x, y, theta) */}
                          <div className="flex gap-1">
                            <div className="flex-1">
                              <label className="text-[8px] text-gray-600">x</label>
                              <input
                                type="number"
                                value={((params[field.name] as Record<string, number>)?.x) ?? ''}
                                onChange={(e) => {
                                  e.stopPropagation()
                                  const current = (params[field.name] as Record<string, number>) || {}
                                  updateParam(field.name, { ...current, x: parseFloat(e.target.value) || 0 })
                                  updateParam(`${field.name}_waypoint`, '') // Clear waypoint
                                }}
                                onClick={(e) => e.stopPropagation()}
                                className="w-full px-1.5 py-1 bg-[#16162a] border border-gray-700 rounded text-[9px] text-white focus:outline-none"
                                placeholder="0"
                                step="0.01"
                              />
                            </div>
                            <div className="flex-1">
                              <label className="text-[8px] text-gray-600">y</label>
                              <input
                                type="number"
                                value={((params[field.name] as Record<string, number>)?.y) ?? ''}
                                onChange={(e) => {
                                  e.stopPropagation()
                                  const current = (params[field.name] as Record<string, number>) || {}
                                  updateParam(field.name, { ...current, y: parseFloat(e.target.value) || 0 })
                                  updateParam(`${field.name}_waypoint`, '')
                                }}
                                onClick={(e) => e.stopPropagation()}
                                className="w-full px-1.5 py-1 bg-[#16162a] border border-gray-700 rounded text-[9px] text-white focus:outline-none"
                                placeholder="0"
                                step="0.01"
                              />
                            </div>
                            <div className="flex-1">
                              <label className="text-[8px] text-gray-600">θ</label>
                              <input
                                type="number"
                                value={((params[field.name] as Record<string, number>)?.theta) ?? ''}
                                onChange={(e) => {
                                  e.stopPropagation()
                                  const current = (params[field.name] as Record<string, number>) || {}
                                  updateParam(field.name, { ...current, theta: parseFloat(e.target.value) || 0 })
                                  updateParam(`${field.name}_waypoint`, '')
                                }}
                                onClick={(e) => e.stopPropagation()}
                                className="w-full px-1.5 py-1 bg-[#16162a] border border-gray-700 rounded text-[9px] text-white focus:outline-none"
                                placeholder="0"
                                step="0.01"
                              />
                            </div>
                          </div>
                        </div>
                      ) : inputType === 'trajectory' ? (
                        <div className="space-y-1">
                          {/* Trajectory needs waypoint sequence */}
                          <select
                            value={(params[`${field.name}_waypoint`] as string) || ''}
                            onChange={(e) => {
                              e.stopPropagation()
                              updateParam(`${field.name}_waypoint`, e.target.value)
                            }}
                            onClick={(e) => e.stopPropagation()}
                            className="w-full px-2 py-1.5 bg-[#16162a] border border-purple-500/30 rounded text-[10px] text-purple-400 focus:outline-none"
                          >
                            <option value="" className="bg-[#1a1a2e]">Select joint waypoint...</option>
                            {availableWaypoints.filter(wp => wp.name.toLowerCase().includes('joint')).map(wp => (
                              <option key={wp.id} value={wp.id} className="bg-[#1a1a2e]">{wp.name}</option>
                            ))}
                          </select>
                          <div className="text-[8px] text-gray-500">
                            Trajectory from waypoint or teach mode
                          </div>
                        </div>
                      ) : inputType === 'complex' ? (
                        <div className="space-y-1">
                          <textarea
                            value={typeof params[field.name] === 'object'
                              ? JSON.stringify(params[field.name], null, 2)
                              : (params[field.name] as string) || ''}
                            onChange={(e) => {
                              e.stopPropagation()
                              try {
                                const parsed = JSON.parse(e.target.value)
                                updateParam(field.name, parsed)
                              } catch {
                                // Keep as string if not valid JSON
                                updateParam(field.name, e.target.value)
                              }
                            }}
                            onClick={(e) => e.stopPropagation()}
                            className="w-full px-2 py-1.5 bg-[#16162a] border border-gray-600 rounded text-[9px] text-gray-300 font-mono focus:outline-none resize-none"
                            placeholder="{}"
                            rows={3}
                          />
                          <div className="text-[8px] text-gray-500">JSON format</div>
                        </div>
                      ) : (
                        <input
                          type="text"
                          value={(params[field.name] as string) ?? ''}
                          onChange={(e) => {
                            e.stopPropagation()
                            updateParam(field.name, e.target.value)
                          }}
                          onClick={(e) => e.stopPropagation()}
                          className="w-full px-2 py-1.5 bg-[#16162a] border border-amber-500/30 rounded text-[10px] text-white focus:outline-none focus:border-amber-500"
                          placeholder={field.default || ''}
                        />
                      )}
                    </div>
                  )
                })
              )}
            </div>
          )}
        </div>
      )}

      {/* Start States Section */}
      <div className="border-b border-[#2a2a4a]">
        <button
          onClick={(e) => { e.stopPropagation(); setExpandedSection(expandedSection === 'start' ? null : 'start') }}
          className="w-full px-3 py-1.5 flex items-center justify-between hover:bg-[#2a2a4a]/50 transition-colors"
        >
          <div className="flex items-center gap-2">
            <div className="w-2 h-2 rounded-full bg-cyan-500" />
            <span className="text-[10px] text-cyan-400 uppercase tracking-wider font-medium">Start States</span>
            <span className="text-[9px] text-gray-600">({startStates.length})</span>
          </div>
          {expandedSection === 'start' ? (
            <ChevronUp className="w-3 h-3 text-gray-500" />
          ) : (
            <ChevronDown className="w-3 h-3 text-gray-500" />
          )}
        </button>
        {expandedSection === 'start' && (
          <div className="px-3 pb-2 space-y-1.5">
            {startStates.length === 0 ? (
              <p className="text-[9px] text-gray-600 italic">No preconditions</p>
            ) : (
              startStates.map((ss, idx) => (
                <div key={ss.id} className="flex flex-wrap items-center gap-1 text-[9px] bg-cyan-500/10 px-2 py-1.5 rounded group">
                  {/* Operator (and/or) - only for 2nd+ conditions */}
                  {idx > 0 && (
                    <select
                      value={ss.operator || 'and'}
                      onChange={(e) => {
                        e.stopPropagation()
                        updateStartStateField(ss.id, 'operator', e.target.value as 'and' | 'or')
                      }}
                      onClick={(e) => e.stopPropagation()}
                      className="bg-[#16162a] text-[9px] text-orange-400 font-bold uppercase focus:outline-none cursor-pointer rounded px-1.5 py-0.5 border border-orange-500/30"
                    >
                      <option value="and" className="bg-[#1a1a2e]">AND</option>
                      <option value="or" className="bg-[#1a1a2e]">OR</option>
                    </select>
                  )}

                  {/* Quantifier (Self/Every/Any/Specific) */}
                  <select
                    value={ss.quantifier || 'self'}
                    onChange={(e) => {
                      e.stopPropagation()
                      updateStartStateField(ss.id, 'quantifier', e.target.value as 'self' | 'every' | 'any' | 'specific')
                    }}
                    onClick={(e) => e.stopPropagation()}
                    className="bg-[#16162a] text-[9px] text-purple-400 font-medium focus:outline-none cursor-pointer rounded px-1.5 py-0.5 border border-purple-500/30"
                  >
                    <option value="self" className="bg-[#1a1a2e]">Self</option>
                    <option value="every" className="bg-[#1a1a2e]">Every</option>
                    <option value="any" className="bg-[#1a1a2e]">Any</option>
                    <option value="specific" className="bg-[#1a1a2e]">Agent</option>
                  </select>

                  {/* Agent (only for every/any) */}
                  {(ss.quantifier === 'every' || ss.quantifier === 'any') && (
                    <select
                      value={ss.agentId || availableAgents[0]?.id || ''}
                      onChange={(e) => {
                        e.stopPropagation()
                        updateStartStateField(ss.id, 'agentId', e.target.value)
                      }}
                      onClick={(e) => e.stopPropagation()}
                      className="bg-[#16162a] text-[9px] text-blue-400 focus:outline-none cursor-pointer rounded px-1.5 py-0.5 border border-blue-500/30"
                    >
                      {availableAgents.length > 0 ? (
                        availableAgents.map(agent => (
                          <option key={agent.id} value={agent.id} className="bg-[#1a1a2e]">
                            {agent.name}
                          </option>
                        ))
                      ) : (
                        <option value="" className="bg-[#1a1a2e]">No agents</option>
                      )}
                    </select>
                  )}

                  {/* Specific Agent (only for specific) */}
                  {ss.quantifier === 'specific' && (
                    <div className="flex-1">
                      <input
                        type="text"
                        list={`start-agent-options-${ss.id}`}
                        value={(() => {
                          if (ss.agentId) {
                            const match = availableAgents.find(agent => agent.id === ss.agentId)
                            return match ? match.name : ss.agentId
                          }
                          return ''
                        })()}
                        onChange={(e) => {
                          e.stopPropagation()
                          const value = e.target.value
                          const matchById = availableAgents.find(agent => agent.id === value)
                          if (matchById) {
                            updateStartStateField(ss.id, 'agentId', matchById.id)
                            return
                          }
                          const matchByName = availableAgents.find(agent => agent.name === value)
                          if (matchByName) {
                            updateStartStateField(ss.id, 'agentId', matchByName.id)
                            return
                          }
                          updateStartStateField(ss.id, 'agentId', value)
                        }}
                        onClick={(e) => e.stopPropagation()}
                        placeholder="Select or enter agent"
                        className="w-full bg-[#16162a] text-[9px] text-green-400 focus:outline-none cursor-pointer rounded px-1.5 py-0.5 border border-green-500/30"
                      />
                      <datalist id={`start-agent-options-${ss.id}`}>
                        {availableAgents.map(agent => (
                          <option key={agent.id} value={agent.name} />
                        ))}
                      </datalist>
                    </div>
                  )}

                  <span className="text-gray-500">is</span>

                  {/* State */}
                  <select
                    value={ss.state}
                    onChange={(e) => {
                      e.stopPropagation()
                      updateStartStateField(ss.id, 'state', e.target.value)
                    }}
                    onClick={(e) => e.stopPropagation()}
                    className="bg-[#16162a] text-[9px] text-cyan-400 font-mono focus:outline-none cursor-pointer rounded px-1.5 py-0.5 border border-cyan-500/30"
                  >
                    {states.map(s => <option key={s} value={s} className="bg-[#1a1a2e]">{s}</option>)}
                  </select>

                  {/* Delete button */}
                  <button
                    onClick={(e) => {
                      e.stopPropagation()
                      removeStartState(ss.id)
                    }}
                    className="ml-auto opacity-0 group-hover:opacity-100 transition-opacity"
                  >
                    <X className="w-2.5 h-2.5 text-cyan-500/70 hover:text-red-400" />
                  </button>
                </div>
              ))
            )}
            <button
              onClick={(e) => { e.stopPropagation(); addStartState() }}
              className="text-[9px] text-cyan-400 hover:text-cyan-300"
            >
              + Add start condition
            </button>
          </div>
        )}
      </div>

      {/* During States Section */}
      <div className="px-3 py-2 border-b border-[#2a2a4a]">
        <div className="flex items-center justify-between mb-1.5">
          <div className="flex items-center gap-2">
            <div className="w-2 h-2 rounded-full bg-yellow-500 animate-pulse" />
            <span className="text-[10px] text-yellow-400 uppercase tracking-wider font-medium">During States</span>
          </div>
          <button
            onClick={(e) => { e.stopPropagation(); addDuringState() }}
            className="p-0.5 hover:bg-yellow-500/20 rounded transition-colors"
          >
            <Plus className="w-2.5 h-2.5 text-yellow-500" />
          </button>
        </div>
        <div className="flex flex-wrap gap-1">
          {duringStateTargets.length === 0 ? (
            <span className="text-[9px] text-gray-600 italic">No states during execution</span>
          ) : (
            duringStateTargets.map((target, i) => (
              <div key={i} className="flex items-center gap-1 px-2 py-0.5 bg-yellow-500/10 border border-yellow-500/30 rounded-full group">
                <select
                  value={target.state}
                  onChange={(e) => {
                    e.stopPropagation()
                    const next = [...duringStateTargets]
                    next[i] = { ...next[i], state: e.target.value }
                    updateData('duringStateTargets', next)
                  }}
                  onClick={(e) => e.stopPropagation()}
                  className="bg-[#1a1a2e] text-[9px] text-yellow-400 font-medium focus:outline-none cursor-pointer rounded px-1"
                >
                  {states.map(s => <option key={s} value={s} className="bg-[#1a1a2e]">{s}</option>)}
                </select>
                <select
                  value={target.target_type || 'self'}
                  onChange={(e) => {
                    e.stopPropagation()
                    const next = [...duringStateTargets]
                    const nextTarget: NormalizedDuringStateTarget = {
                      ...next[i],
                      target_type: e.target.value as NormalizedDuringStateTarget['target_type'],
                    }
                    if (nextTarget.target_type !== 'agent') {
                      nextTarget.agent_id = undefined
                    } else if (!nextTarget.agent_id && availableAgents.length > 0) {
                      nextTarget.agent_id = availableAgents[0].id
                    }
                    next[i] = nextTarget
                    updateData('duringStateTargets', next)
                  }}
                  onClick={(e) => e.stopPropagation()}
                  className="bg-[#1a1a2e] text-[9px] text-yellow-300 font-medium focus:outline-none cursor-pointer rounded px-1"
                >
                  {DURING_TARGET_OPTIONS.map(option => (
                    <option key={option.value} value={option.value} className="bg-[#1a1a2e]">
                      {option.label}
                    </option>
                  ))}
                </select>
                {target.target_type === 'agent' && (
                  <select
                    value={target.agent_id || ''}
                    onChange={(e) => {
                      e.stopPropagation()
                      const next = [...duringStateTargets]
                      next[i] = { ...next[i], agent_id: e.target.value }
                      updateData('duringStateTargets', next)
                    }}
                    onClick={(e) => e.stopPropagation()}
                    className="bg-[#1a1a2e] text-[9px] text-yellow-300 font-medium focus:outline-none cursor-pointer rounded px-1"
                  >
                    {availableAgents.length === 0 && (
                      <option value="" className="bg-[#1a1a2e]">No agents</option>
                    )}
                    {availableAgents.map(agent => (
                      <option key={agent.id} value={agent.id} className="bg-[#1a1a2e]">
                        {agent.name}
                      </option>
                    ))}
                  </select>
                )}
                <button
                  onClick={(e) => {
                    e.stopPropagation()
                    updateData('duringStateTargets', duringStateTargets.filter((_, idx) => idx !== i))
                  }}
                  className="opacity-0 group-hover:opacity-100 transition-opacity"
                >
                  <X className="w-2.5 h-2.5 text-yellow-500/70 hover:text-yellow-500" />
                </button>
              </div>
            ))
          )}
        </div>
      </div>

      {/* End States Section */}
      <div className="px-3 py-2">
        <div className="flex items-center justify-between mb-1.5">
          <div className="flex items-center gap-2">
            <div className="w-2 h-2 rounded-sm bg-gradient-to-r from-green-500 to-red-500" />
            <span className="text-[10px] text-gray-300 uppercase tracking-wider font-medium">End States</span>
            {resultFields.length > 0 && (
              <span className="text-[8px] text-gray-600 font-mono">({resultFields.length} results)</span>
            )}
          </div>
          <button
            onClick={(e) => { e.stopPropagation(); addEndState() }}
            className="p-0.5 hover:bg-gray-500/20 rounded transition-colors"
          >
            <Plus className="w-2.5 h-2.5 text-gray-400" />
          </button>
        </div>
        {/* Show result fields info */}
        {resultFields.length > 0 && (
          <div className="mb-2 px-2 py-1 bg-gray-800/50 rounded text-[8px]">
            <span className="text-gray-500">Result: </span>
            <span className="text-gray-400 font-mono">
              {resultFields.map(f => `${f.name}: ${f.type}${f.is_array ? '[]' : ''}`).join(', ')}
            </span>
          </div>
        )}
        <div className="space-y-1">
          {endStates.map((endState) => (
            <div
              key={endState.id}
              className="flex items-center gap-2 pr-4"
            >
              <div
                className="w-2 h-2 rounded-full flex-shrink-0"
                style={{ backgroundColor: endState.color || getEndStateColor(endState) }}
              />
              <select
                value={endState.outcome || inferOutcomeFromEndState(endState)}
                onChange={(e) => {
                  e.stopPropagation()
                  updateEndState(endState.id, 'outcome', e.target.value)
                }}
                onClick={(e) => e.stopPropagation()}
                className="w-20 px-1 py-0.5 bg-[#16162a] border border-gray-700 rounded text-[9px] text-gray-300 focus:outline-none cursor-pointer"
              >
                {OUTCOME_OPTIONS.map(option => (
                  <option key={option.value} value={option.value} className="bg-[#16162a]">
                    {option.label}
                  </option>
                ))}
              </select>
              <input
                type="text"
                value={endState.label || ''}
                onChange={(e) => {
                  e.stopPropagation()
                  updateEndState(endState.id, 'label', e.target.value)
                }}
                onClick={(e) => e.stopPropagation()}
                placeholder="Label"
                className="w-16 px-1 py-0.5 bg-transparent border-b border-gray-700 text-[9px] text-gray-300 focus:outline-none focus:border-gray-500"
              />
              <span className="text-[9px] text-gray-600">→</span>
              <select
                value={endState.state}
                onChange={(e) => {
                  e.stopPropagation()
                  updateEndState(endState.id, 'state', e.target.value)
                }}
                onClick={(e) => e.stopPropagation()}
                className="flex-1 px-1 py-0.5 bg-[#16162a] border border-gray-700 rounded text-[9px] focus:outline-none cursor-pointer"
                style={{ color: endState.color || getEndStateColor(endState) }}
              >
                {states.map(s => <option key={s} value={s} className="bg-[#16162a]">{s}</option>)}
              </select>
              <input
                type="text"
                value={endState.condition || ''}
                onChange={(e) => {
                  e.stopPropagation()
                  updateEndState(endState.id, 'condition', e.target.value)
                }}
                onClick={(e) => e.stopPropagation()}
                placeholder="Condition (optional)"
                className="w-20 px-1 py-0.5 bg-transparent border-b border-gray-700 text-[9px] text-gray-400 focus:outline-none focus:border-gray-500"
              />
              {endStates.length > 1 && (
                <button
                  onClick={(e) => { e.stopPropagation(); removeEndState(endState.id) }}
                  className="text-gray-600 hover:text-red-400 transition-colors"
                >
                  <X className="w-2.5 h-2.5" />
                </button>
              )}
            </div>
          ))}
        </div>
      </div>

      {/* Footer */}
      <div
        className="px-3 py-1 border-t border-[#2a2a4a] flex items-center justify-between"
        style={{ backgroundColor: '#16162a' }}
      >
        <span className="text-[9px] text-gray-500 font-mono truncate max-w-[180px]">
          {data.server || data.subtype}
        </span>
        <div className="w-1.5 h-1.5 rounded-full bg-green-500" />
      </div>

      {/* Dynamic End State Handles */}
      {endStates.map((endState, index) => (
        <Handle
          key={endState.id}
          type="source"
          position={Position.Right}
          id={endState.id}
          className="!w-3 !h-3 !border-2 !rounded-full"
          style={{
            right: -6,
            top: `${getEndStateHandlePosition(index, endStates.length)}%`,
            backgroundColor: endState.color || getEndStateColor(endState),
            borderColor: `${endState.color || getEndStateColor(endState)}99`,
          }}
        />
      ))}
    </div>
  )
})

StateActionNode.displayName = 'StateActionNode'

export default StateActionNode
