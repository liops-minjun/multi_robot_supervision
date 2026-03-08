import { memo, useState, useEffect, useMemo, useCallback, useRef } from 'react'
import { Handle, Position, NodeProps, useReactFlow, useUpdateNodeInternals, useNodes } from 'reactflow'
import { ChevronDown, ChevronUp, X, Loader2, Download } from 'lucide-react'
import type { EndStateConfig, ActionField, ParameterFieldSource } from '../../../types'
import { capabilityApi } from '../../../api/client'

// Local imports
import type { StateActionNodeData, AvailableStep } from './types'
import { OUTCOME_COLORS } from './constants'
import {
  schemaToFields,
  inferOutcomeFromEndState,
  getEndStateColor,
} from './utils'
import { GoalParametersSection } from './components'

// Fixed end states (5 outcomes, always present)
const DEFAULT_END_STATES: EndStateConfig[] = [
  { id: 'end-success', state: 'idle', label: 'Success', outcome: 'success', color: OUTCOME_COLORS.success },
  { id: 'end-failed', state: 'error', label: 'Failed', outcome: 'failed', color: OUTCOME_COLORS.failed },
  { id: 'end-aborted', state: 'error', label: 'Aborted', outcome: 'aborted', color: OUTCOME_COLORS.aborted },
  { id: 'end-cancelled', state: 'idle', label: 'Cancelled', outcome: 'cancelled', color: OUTCOME_COLORS.cancelled },
  { id: 'end-timeout', state: 'error', label: 'Timeout', outcome: 'timeout', color: OUTCOME_COLORS.timeout },
]

const StateActionNode = memo(({ id, data, selected }: NodeProps<StateActionNodeData>) => {
  const PARAMETER_EDITING_Z_INDEX = 5000
  const color = data.color || '#f87171'
  const capabilityKind = data.capabilityKind === 'service' ? 'service' : 'action'
  const accentClasses = capabilityKind === 'service'
    ? {
      text: 'text-cyan-400',
      textHover: 'group-hover:text-cyan-300',
      border: 'border-cyan-500/30',
      codeText: 'text-cyan-400',
      codeBg: 'bg-cyan-500/10',
    }
    : {
      text: 'text-rose-400',
      textHover: 'group-hover:text-rose-300',
      border: 'border-rose-500/30',
      codeText: 'text-rose-400',
      codeBg: 'bg-rose-500/10',
    }
  const { setNodes, setEdges } = useReactFlow()
  const updateNodeInternals = useUpdateNodeInternals()
  const allNodes = useNodes()
  const [expandedSection, setExpandedSection] = useState<string | null>(null)
  const [isParameterEditing, setIsParameterEditing] = useState<boolean>(Boolean(data.isParameterEditing))
  const nodeRootRef = useRef<HTMLDivElement | null>(null)

  // Action fields from API
  const [goalFields, setGoalFields] = useState<ActionField[]>([])
  const [resultFields, setResultFields] = useState<ActionField[]>([])
  const [isLoadingFields, setIsLoadingFields] = useState(false)

  const jobName = data.jobName ?? ''
  const isEditing = data.isEditing ?? true

  // End states: use fixed 5 defaults, preserving user state selections if they exist
  const endStates = useMemo(() => {
    if (data.endStates && data.endStates.length > 0) {
      // Ensure all 5 outcomes exist
      const existing = new Map(data.endStates.map(es => [es.outcome || inferOutcomeFromEndState(es), es]))
      return DEFAULT_END_STATES.map(def => {
        const ex = existing.get(def.outcome!)
        return ex ? { ...ex, color: ex.color || def.color } : def
      })
    }
    return DEFAULT_END_STATES
  }, [data.endStates])

  // PDDL planning states/resources from TaskDistributor
  const pStates = data.taskDistributorStates || []
  const pResources = data.taskDistributorResources || []
  const typeResources = useMemo(
    () => pResources.filter(resource => resource.kind === 'type'),
    [pResources]
  )
  const instanceResources = useMemo(
    () => pResources.filter(resource => (resource.kind || 'instance') !== 'type'),
    [pResources]
  )

  const formatResourceToken = useCallback((token: string) => {
    if (token.startsWith('type:')) {
      const resource = typeResources.find(item => item.id === token.slice(5))
      return resource ? `TYPE ${resource.name}` : token
    }
    if (token.startsWith('instance:')) {
      const resource = instanceResources.find(item => item.id === token.slice(9))
      return resource ? resource.name : token
    }
    return token
  }, [instanceResources, typeResources])

  const updateData = useCallback((field: string, value: unknown) => {
    setNodes((nds) => nds.map((n) => n.id === id ? { ...n, data: { ...n.data, [field]: value } } : n))
  }, [id, setNodes])

  const updateParameterEditingMode = useCallback((active: boolean) => {
    setIsParameterEditing(active)
    setNodes((nds) => nds.map((n) => {
      if (n.id !== id) return n
      const nodeData = n.data as StateActionNodeData
      const currentlyActive = Boolean(nodeData.isParameterEditing)
      const currentZIndex = typeof n.zIndex === 'number' ? n.zIndex : undefined
      if (active) {
        if (currentlyActive && currentZIndex === PARAMETER_EDITING_Z_INDEX) return n
        return { ...n, zIndex: PARAMETER_EDITING_Z_INDEX, data: { ...n.data, isParameterEditing: true } }
      }
      if (!currentlyActive && currentZIndex === undefined) return n
      const { zIndex: _unused, ...restNode } = n
      return { ...restNode, data: { ...n.data, isParameterEditing: false } }
    }))
  }, [id, setNodes])

  // Fetch action schema
  useEffect(() => {
    if (!data.actionType) {
      setGoalFields([])
      setResultFields([])
      return
    }
    setIsLoadingFields(true)
    capabilityApi.getByActionType(data.actionType)
      .then((capData) => {
        if (capData.agents && capData.agents.length > 0) {
          const scoreAgent = (agent: typeof capData.agents[number]): number => {
            let score = 0
            if (data.subtype && agent.action_server === data.subtype) score += 1000
            if (agent.is_available) score += 100
            if (agent.goal_schema && Object.keys(agent.goal_schema).length > 0) score += 20
            if (agent.result_schema && Object.keys(agent.result_schema).length > 0) score += 10
            return score
          }
          const bestAgent = [...capData.agents].sort((a, b) => scoreAgent(b) - scoreAgent(a))[0]
          const goalFieldsParsed = schemaToFields(bestAgent.goal_schema)
          setGoalFields(goalFieldsParsed)
          const resultFieldsParsed = schemaToFields(bestAgent.result_schema)
          setResultFields(resultFieldsParsed)
          updateData('resultFields', resultFieldsParsed.map(f => ({ name: f.name, type: f.type + (f.is_array ? '[]' : '') })))
        } else {
          setGoalFields([])
          setResultFields([])
          updateData('resultFields', [])
        }
      })
      .catch(() => {
        setGoalFields([])
        setResultFields([])
        updateData('resultFields', [])
      })
      .finally(() => setIsLoadingFields(false))
  }, [data.actionType, data.subtype])

  useEffect(() => {
    if (selected && !isLoadingFields && goalFields.length === 0 && data.actionType) {
      setExpandedSection('params')
    }
  }, [selected, isLoadingFields, goalFields.length, data.actionType])

  useEffect(() => {
    const timer = setTimeout(() => updateNodeInternals(id), 0)
    return () => clearTimeout(timer)
  }, [id, jobName, updateNodeInternals])

  const params = data.params || {}
  const fieldSources = data.fieldSources || {}

  const availableSteps: AvailableStep[] = useMemo(() => {
    return allNodes
      .filter(node => node.type === 'action' && node.id !== id && (node.data as StateActionNodeData)?.jobName)
      .map(node => {
        const nodeData = node.data as StateActionNodeData
        return { id: node.id, name: nodeData?.jobName || node.id, resultFields: nodeData?.resultFields || [] }
      })
  }, [allNodes, id])

  const updateParam = useCallback((key: string, value: unknown) => updateData('params', { ...params, [key]: value }), [params, updateData])

  const updateFieldSource = useCallback((key: string, source: ParameterFieldSource | undefined) => {
    if (source === undefined) {
      const newFieldSources = { ...fieldSources }
      delete newFieldSources[key]
      updateData('fieldSources', Object.keys(newFieldSources).length > 0 ? newFieldSources : undefined)
    } else {
      updateData('fieldSources', { ...fieldSources, [key]: source })
    }
  }, [fieldSources, updateData])

  const removeNode = useCallback(() => {
    if (!isEditing) return
    setNodes((nds) => nds.filter((node) => node.id !== id))
    setEdges((eds) => eds.filter((edge) => edge.source !== id && edge.target !== id))
  }, [id, isEditing, setEdges, setNodes])

  const isActivelyEditingNode = isParameterEditing || expandedSection === 'params'

  const handleNodeFocusCapture = useCallback((event: React.FocusEvent<HTMLDivElement>) => {
    const target = event.target as HTMLElement | null
    if (!target) return
    if (target.matches('input, textarea, select, [contenteditable="true"]')) {
      updateParameterEditingMode(true)
    }
  }, [updateParameterEditingMode])

  const handleNodeBlurCapture = useCallback((event: React.FocusEvent<HTMLDivElement>) => {
    const nextTarget = event.relatedTarget as Node | null
    if (nextTarget && nodeRootRef.current?.contains(nextTarget)) return
    window.setTimeout(() => {
      const activeElement = document.activeElement
      if (activeElement && nodeRootRef.current?.contains(activeElement)) return
      if (expandedSection !== 'params') updateParameterEditingMode(false)
    }, 0)
  }, [expandedSection, updateParameterEditingMode])

  const handleGoalSectionToggle = useCallback(() => {
    const opening = expandedSection !== 'params'
    setExpandedSection(opening ? 'params' : null)
    if (opening) {
      updateParameterEditingMode(true)
      return
    }
    const activeElement = document.activeElement
    if (!activeElement || !nodeRootRef.current?.contains(activeElement)) {
      updateParameterEditingMode(false)
    }
  }, [expandedSection, updateParameterEditingMode])

  // PDDL helpers
  const hasPDDLData = pStates.length > 0 || pResources.length > 0
  const activePlanningDuring = (data.planningDuring ?? []).find(effect => effect.variable) || null

  const renderVarSelect = (value: string, onChange: (v: string) => void) => (
    <select className="w-20 px-0.5 py-0.5 bg-surface border border-primary rounded text-[9px] text-primary" value={value}
      onClick={(e) => e.stopPropagation()}
      onChange={(e) => { e.stopPropagation(); onChange(e.target.value) }}>
      <option value="">var...</option>
      {pStates.map(sv => <option key={sv.name} value={sv.name}>{sv.name}</option>)}
    </select>
  )

  const renderValueInput = (variable: string, value: string, onChange: (v: string) => void) => {
    if (!variable) {
      return (
        <input
          disabled
          className="w-16 px-1 py-0.5 bg-surface border border-primary rounded text-[9px] text-muted opacity-60"
          value=""
          placeholder="value"
          onClick={(e) => e.stopPropagation()}
        />
      )
    }
    const varDef = pStates.find(sv => sv.name === variable)
    if (varDef?.type === 'bool') {
      return (
        <select className="w-14 px-0.5 py-0.5 bg-surface border border-primary rounded text-[9px] text-primary" value={value}
          onClick={(e) => e.stopPropagation()}
          onChange={(e) => { e.stopPropagation(); onChange(e.target.value) }}>
          <option value="true">true</option>
          <option value="false">false</option>
        </select>
      )
    }
    if (varDef?.type === 'int') {
      return <input type="number" className="w-14 px-1 py-0.5 bg-surface border border-primary rounded text-[9px] text-primary" value={value} placeholder="0"
        onClick={(e) => e.stopPropagation()} onChange={(e) => { e.stopPropagation(); onChange(e.target.value) }} />
    }
    return <input className="w-16 px-1 py-0.5 bg-surface border border-primary rounded text-[9px] text-primary" value={value} placeholder="val"
      onClick={(e) => e.stopPropagation()} onChange={(e) => { e.stopPropagation(); onChange(e.target.value) }} />
  }

  const getPlanningDefaultValue = useCallback((variable: string) => {
    const varDef = pStates.find(sv => sv.name === variable)
    if (varDef?.type === 'bool') return 'true'
    if (varDef?.type === 'int') return '0'
    return ''
  }, [pStates])

  const updatePlanningDuring = useCallback((variable: string, value?: string) => {
    if (!variable) {
      updateData('planningDuring', [])
      return
    }

    const previous = activePlanningDuring
    const resolvedValue = value ?? (
      previous?.variable === variable && previous.value !== ''
        ? previous.value
        : getPlanningDefaultValue(variable)
    )

    updateData('planningDuring', [{ variable, value: resolvedValue }])
  }, [activePlanningDuring, getPlanningDefaultValue, updateData])

  const renderResourceTokenSelect = useCallback((onPick: (token: string) => void) => (
    <select
      className="w-24 px-1 py-0.5 bg-surface border border-primary rounded text-[9px] text-primary"
      onClick={(e) => e.stopPropagation()}
      onChange={(e) => {
        e.stopPropagation()
        if (!e.target.value) return
        onPick(e.target.value)
        e.target.value = ''
      }}
    >
      <option value="">+ add</option>
      {typeResources.length > 0 && (
        <optgroup label="Types">
          {typeResources.map(resource => <option key={resource.id} value={`type:${resource.id}`}>{resource.name}</option>)}
        </optgroup>
      )}
      {instanceResources.length > 0 && (
        <optgroup label="Instances">
          {instanceResources.map(resource => <option key={resource.id} value={`instance:${resource.id}`}>{resource.name}</option>)}
        </optgroup>
      )}
    </select>
  ), [instanceResources, typeResources])

  return (
    <div
      ref={nodeRootRef}
      draggable={false}
      onFocusCapture={handleNodeFocusCapture}
      onBlurCapture={handleNodeBlurCapture}
      className={`
        relative min-w-[280px] max-w-[340px] rounded-lg overflow-visible
        bg-surface border-2 shadow-lg
        ${isActivelyEditingNode
          ? (capabilityKind === 'service'
            ? 'border-cyan-300 shadow-2xl shadow-cyan-500/30 ring-2 ring-cyan-400/35'
            : 'border-rose-300 shadow-2xl shadow-rose-500/30 ring-2 ring-rose-400/35')
          : (selected ? 'border-white/60 shadow-xl shadow-blue-500/20' : 'border-primary')}
        transition-all duration-150
      `}
    >
      {/* Drag handle */}
      <div
        className="action-node-drag-handle h-2 cursor-grab active:cursor-grabbing border-b border-primary/60 bg-elevated/40"
        title="드래그하여 노드 이동"
      />

      {/* Input Handle */}
      <Handle
        type="target"
        position={Position.Left}
        id="in"
        className="!w-4 !h-4 !bg-cyan-500 !border-2 !border-cyan-300 !rounded-full hover:!w-5 hover:!h-5 hover:!bg-cyan-400 transition-all cursor-crosshair !pointer-events-auto"
        style={{ position: 'absolute', left: -8, top: '50px', zIndex: 1000, pointerEvents: 'auto' }}
        title="입력 (이전 액션에서)"
      />

      {/* Header */}
      <div style={{ backgroundColor: `${color}25` }}>
        <div className="px-3 pt-2 pb-1 flex items-center gap-2">
          <div className="w-2 h-2 rounded-sm flex-shrink-0" style={{ backgroundColor: color }} />
          <div className="flex-1 min-w-0">
            <input
              type="text"
              value={jobName}
              onChange={(e) => { e.stopPropagation(); updateData('jobName', e.target.value) }}
              onClick={(e) => e.stopPropagation()}
              className="w-full text-sm font-semibold text-primary bg-transparent border-b border-transparent hover:border-gray-600 focus:border-white focus:outline-none truncate"
              placeholder="action_name/세부작업"
            />
          </div>
          {isEditing && (
            <button
              onClick={(e) => { e.stopPropagation(); removeNode() }}
              className="p-1 text-muted hover:text-red-400 hover:bg-red-500/20 rounded transition-colors"
              title="노드 삭제"
            >
              <X className="w-3 h-3" />
            </button>
          )}
        </div>

        <div className="px-3 pb-2 flex items-center gap-2">
          <input
            type="text"
            value={data.server || data.subtype || ''}
            onChange={(e) => { e.stopPropagation(); updateData('server', e.target.value) }}
            onClick={(e) => e.stopPropagation()}
            className="flex-1 min-w-0 text-[9px] font-mono text-secondary bg-transparent border-b border-transparent hover:border-gray-600 focus:border-white focus:outline-none truncate"
            placeholder={capabilityKind === 'service' ? '/service_server' : '/action_server'}
            title={`${capabilityKind === 'service' ? 'Service' : 'Action'} Server full path`}
          />
          {data.actionType && (
            <span className="text-[8px] px-1 py-0.5 rounded truncate flex-shrink-0" style={{ backgroundColor: `${color}30`, color }}>
              {data.actionType.split('/').pop()}
            </span>
          )}
        </div>
      </div>

      {/* Goal Parameters Section */}
      <GoalParametersSection
        isExpanded={expandedSection === 'params'}
        onToggle={handleGoalSectionToggle}
        isLoadingFields={isLoadingFields}
        goalFields={goalFields}
        actionType={data.actionType}
        params={params}
        fieldSources={fieldSources}
        availableSteps={availableSteps}
        hasActionType={!!data.actionType}
        onUpdateParam={updateParam}
        onUpdateFieldSource={updateFieldSource}
      />

      {/* Result Schema Section */}
      <div className="border-b border-primary">
        <button
          onClick={(e) => { e.stopPropagation(); setExpandedSection(expandedSection === 'result' ? null : 'result') }}
          className="w-full px-3 py-1.5 flex items-center justify-between hover:bg-elevated/50 transition-colors"
        >
          <div className="flex items-center gap-2">
            <Download className="w-3 h-3 text-green-500" />
            <span className="text-[10px] text-green-400 uppercase tracking-wider font-medium">Result 스키마</span>
            {isLoadingFields ? <Loader2 className="w-3 h-3 text-green-500 animate-spin" /> : <span className="text-[9px] text-muted">({resultFields.length})</span>}
          </div>
          {expandedSection === 'result' ? <ChevronUp className="w-3 h-3 text-muted" /> : <ChevronDown className="w-3 h-3 text-muted" />}
        </button>
        {expandedSection === 'result' && (
          <div className="px-3 pb-2 space-y-2">
            {resultFields.length === 0 ? (
              <div className="p-2 bg-elevated rounded border border-primary">
                <p className="text-[9px] text-muted">Result 스키마 없음</p>
              </div>
            ) : (
              <div className="space-y-1">
                {resultFields.map((field) => (
                  <div key={field.name} className="flex items-center justify-between px-2 py-1.5 bg-surface rounded border border-green-500/20">
                    <span className="text-[9px] text-green-400 font-mono">{field.name}</span>
                    <code className={`text-[8px] ${accentClasses.codeText} ${accentClasses.codeBg} px-1.5 py-0.5 rounded`}>${'{'}${jobName || id}.{field.name}{'}'}</code>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
      </div>

      {/* PDDL Settings Section — always visible */}
      <div className="border-b border-primary">
        <div className="px-3 py-2">
          <div className="flex items-center justify-between mb-2">
            <div className="flex items-center gap-1.5">
              <div className="w-2 h-2 rounded-sm bg-violet-500" />
              <span className="text-[10px] text-primary uppercase tracking-wider font-medium">PDDL 설정</span>
            </div>
            <span className="text-[8px] text-muted">State / Resources</span>
          </div>

          {!hasPDDLData ? (
            <p className="text-[9px] text-yellow-400 italic">Task Distributor를 먼저 설정하세요</p>
          ) : (
            <div className="space-y-2">
              <div className="rounded border border-violet-500/20 bg-violet-500/5 p-2">
                <div className="mb-1 flex items-center justify-between">
                  <span className="text-[9px] font-medium uppercase tracking-wider text-violet-300">State</span>
                  {activePlanningDuring && (
                    <button
                      onClick={(e) => { e.stopPropagation(); updateData('planningDuring', []) }}
                      className="text-[8px] text-muted hover:text-red-400"
                    >
                      clear
                    </button>
                  )}
                </div>
                {pStates.length === 0 ? (
                  <p className="text-[9px] text-muted italic">사용 가능한 state 없음</p>
                ) : (
                  <>
                    <div className="flex items-center gap-1">
                      {renderVarSelect(activePlanningDuring?.variable || '', (variable) => updatePlanningDuring(variable))}
                      <span className="text-[9px] text-muted">=</span>
                      {renderValueInput(
                        activePlanningDuring?.variable || '',
                        activePlanningDuring?.value || '',
                        (value) => updatePlanningDuring(activePlanningDuring?.variable || '', value)
                      )}
                    </div>
                    <div className="mt-1 text-[8px] text-secondary">
                      during state는 1개만 저장됩니다.
                    </div>
                  </>
                )}
              </div>

              <div className="rounded border border-amber-500/20 bg-amber-500/5 p-2">
                <div className="mb-1 flex items-center justify-between">
                  <span className="text-[9px] font-medium uppercase tracking-wider text-amber-300">Resources</span>
                  <span className="text-[8px] text-muted">type / instance</span>
                </div>
                {pResources.length === 0 ? (
                  <p className="text-[9px] text-muted italic">사용 가능한 resource 없음</p>
                ) : (
                  <div className="space-y-1.5">
                    <div>
                      <div className="mb-1 flex items-center justify-between gap-2">
                        <span className="text-[8px] font-medium text-yellow-400">Acquire</span>
                        {renderResourceTokenSelect((token) => updateData('resourceAcquire', [...(data.resourceAcquire ?? []), token]))}
                      </div>
                      <div className="flex flex-wrap gap-1">
                        {(data.resourceAcquire ?? []).length === 0 ? (
                          <span className="text-[8px] text-muted">없음</span>
                        ) : (data.resourceAcquire ?? []).map((resource, index) => (
                          <span key={`${resource}-${index}`} className="inline-flex items-center gap-1 rounded-full bg-yellow-500/10 px-1.5 py-0.5 text-[9px] text-yellow-400">
                            {formatResourceToken(resource)}
                            <button
                              onClick={(e) => {
                                e.stopPropagation()
                                const next = [...(data.resourceAcquire ?? [])]
                                next.splice(index, 1)
                                updateData('resourceAcquire', next)
                              }}
                            >
                              <X className="w-2 h-2" />
                            </button>
                          </span>
                        ))}
                      </div>
                    </div>

                    <div>
                      <div className="mb-1 flex items-center justify-between gap-2">
                        <span className="text-[8px] font-medium text-green-400">Release</span>
                        {renderResourceTokenSelect((token) => updateData('resourceRelease', [...(data.resourceRelease ?? []), token]))}
                      </div>
                      <div className="flex flex-wrap gap-1">
                        {(data.resourceRelease ?? []).length === 0 ? (
                          <span className="text-[8px] text-muted">없음</span>
                        ) : (data.resourceRelease ?? []).map((resource, index) => (
                          <span key={`${resource}-${index}`} className="inline-flex items-center gap-1 rounded-full bg-green-500/10 px-1.5 py-0.5 text-[9px] text-green-400">
                            {formatResourceToken(resource)}
                            <button
                              onClick={(e) => {
                                e.stopPropagation()
                                const next = [...(data.resourceRelease ?? [])]
                                next.splice(index, 1)
                                updateData('resourceRelease', next)
                              }}
                            >
                              <X className="w-2 h-2" />
                            </button>
                          </span>
                        ))}
                      </div>
                    </div>
                  </div>
                )}
              </div>
            </div>
          )}
        </div>
      </div>

      {/* End States Section (Fixed 5 outcomes) */}
      <div className="px-3 py-2 relative">
        <div className="flex items-center gap-2 mb-1.5">
          <div className="w-2 h-2 rounded-sm bg-gradient-to-r from-green-500 to-red-500" />
          <span className="text-[10px] text-primary uppercase tracking-wider font-medium">종료 상태</span>
        </div>

        <div className="space-y-1.5">
          {endStates.map((endState) => {
            const stateColor = endState.color || getEndStateColor(endState)
            return (
              <div key={endState.id} className="flex items-center gap-1.5 pr-5 relative">
                <div className="w-2.5 h-2.5 rounded-full flex-shrink-0 border" style={{ backgroundColor: stateColor, borderColor: `${stateColor}99` }} />
                <span className="text-[9px] text-primary w-14">{endState.label}</span>
                <span className="text-[9px] text-muted">→</span>
                <span className="text-[9px] font-mono" style={{ color: stateColor }}>{endState.state}</span>
              </div>
            )
          })}
        </div>

        {/* Output Handles */}
        <div className="absolute right-[-12px] top-0 bottom-0 flex flex-col justify-center gap-0.5 py-6">
          <div className="flex items-center h-5" title="성공">
            <span className="text-[7px] text-green-400 mr-1">S</span>
            <Handle type="source" position={Position.Right} id="success" className="!w-3 !h-3 !border-2 !rounded-full hover:!w-4 hover:!h-4 transition-all cursor-crosshair !pointer-events-auto !relative !transform-none !top-auto !right-auto" style={{ backgroundColor: '#22c55e', borderColor: '#22c55e99' }} />
          </div>
          <div className="flex items-center h-5" title="실패">
            <span className="text-[7px] text-red-400 mr-1">F</span>
            <Handle type="source" position={Position.Right} id="failed" className="!w-3 !h-3 !border-2 !rounded-full hover:!w-4 hover:!h-4 transition-all cursor-crosshair !pointer-events-auto !relative !transform-none !top-auto !right-auto" style={{ backgroundColor: '#ef4444', borderColor: '#ef444499' }} />
          </div>
          <div className="flex items-center h-5" title="중단">
            <span className="text-[7px] text-red-400 mr-1">A</span>
            <Handle type="source" position={Position.Right} id="aborted" className="!w-3 !h-3 !border-2 !rounded-full hover:!w-4 hover:!h-4 transition-all cursor-crosshair !pointer-events-auto !relative !transform-none !top-auto !right-auto" style={{ backgroundColor: '#ef4444', borderColor: '#ef444499' }} />
          </div>
          <div className="flex items-center h-5" title="취소">
            <span className="text-[7px] text-secondary mr-1">C</span>
            <Handle type="source" position={Position.Right} id="cancelled" className="!w-3 !h-3 !border-2 !rounded-full hover:!w-4 hover:!h-4 transition-all cursor-crosshair !pointer-events-auto !relative !transform-none !top-auto !right-auto" style={{ backgroundColor: '#6b7280', borderColor: '#6b728099' }} />
          </div>
          <div className="flex items-center h-5" title="타임아웃">
            <span className="text-[7px] text-yellow-400 mr-1">T</span>
            <Handle type="source" position={Position.Right} id="timeout" className="!w-3 !h-3 !border-2 !rounded-full hover:!w-4 hover:!h-4 transition-all cursor-crosshair !pointer-events-auto !relative !transform-none !top-auto !right-auto" style={{ backgroundColor: '#f59e0b', borderColor: '#f59e0b99' }} />
          </div>
        </div>
      </div>

      {/* Footer */}
      <div className="px-3 py-1 border-t border-primary flex items-center justify-between" style={{ backgroundColor: '#16162a' }}>
        <span className="text-[9px] text-muted font-mono truncate max-w-[180px]">{data.server || data.subtype}</span>
        <div className="w-1.5 h-1.5 rounded-full bg-green-500" />
      </div>
    </div>
  )
})

StateActionNode.displayName = 'StateActionNode'

export default StateActionNode
