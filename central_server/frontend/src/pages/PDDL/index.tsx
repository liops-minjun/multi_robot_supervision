import { useState, useEffect, useCallback, useMemo } from 'react'
import {
  RefreshCw, Play, Eye, Square, ChevronDown, ChevronRight, Info, Target,
  Database, Workflow, AlertTriangle, Link2, Plus, Trash2, Check, X, Layers,
  Circle, Edit, Gem, ToggleLeft, Bot,
} from 'lucide-react'
import { useTranslation } from '../../i18n'
import { behaviorTreeApi, agentApi, capabilityApi, pddlApi, taskDistributorApi } from '../../api/client'
import type {
  BehaviorTree, Agent, PlanResult, PlanExecution, ResourceAllocation,
  TaskDistributor, GraphListItem, TaskDistributorState, TaskDistributorResource, GraphStep,
} from '../../types'
import GoalEditor from './components/GoalEditor'
import PlanVisualization from './components/PlanVisualization'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface AgentWithCaps {
  agent: Agent
  capabilities: string[]
  isOnline: boolean
}


// ---------------------------------------------------------------------------
// Section colour themes
// ---------------------------------------------------------------------------

const SECTION_THEME = {
  resource: {
    accent: 'text-amber-400',
    accentBg: 'bg-amber-500/10',
    accentBorder: 'border-amber-500/20',
    iconBg: 'bg-amber-500/10 text-amber-400',
    pillBg: 'bg-amber-500/10 text-amber-400',
    barLeft: 'border-l-amber-500',
  },
  state: {
    accent: 'text-violet-400',
    accentBg: 'bg-violet-500/10',
    accentBorder: 'border-violet-500/20',
    iconBg: 'bg-violet-500/10 text-violet-400',
    pillBg: 'bg-violet-500/10 text-violet-400',
    barLeft: 'border-l-violet-500',
  },
  agent: {
    accent: 'text-emerald-400',
    accentBg: 'bg-emerald-500/10',
    accentBorder: 'border-emerald-500/20',
    iconBg: 'bg-emerald-500/10 text-emerald-400',
    pillBg: 'bg-emerald-500/10 text-emerald-400',
    barLeft: 'border-l-emerald-500',
  },
  goal: {
    accent: 'text-sky-400',
    accentBg: 'bg-sky-500/10',
    accentBorder: 'border-sky-500/20',
    iconBg: 'bg-sky-500/10 text-sky-400',
    pillBg: 'bg-sky-500/10 text-sky-400',
    barLeft: 'border-l-sky-500',
  },
  plan: {
    accent: 'text-rose-400',
    accentBg: 'bg-rose-500/10',
    accentBorder: 'border-rose-500/20',
    iconBg: 'bg-rose-500/10 text-rose-400',
    pillBg: 'bg-rose-500/10 text-rose-400',
    barLeft: 'border-l-rose-500',
  },
} as const

const TYPE_BADGE: Record<string, { bg: string; text: string }> = {
  bool: { bg: 'bg-green-500/15', text: 'text-green-400' },
  int: { bg: 'bg-blue-500/15', text: 'text-blue-400' },
  string: { bg: 'bg-orange-500/15', text: 'text-orange-400' },
}

function buildInstanceNames(typeName: string, count: number) {
  const normalized = typeName.trim().replace(/\s+/g, ' ')
  if (!normalized) return []

  return Array.from({ length: Math.max(1, count) }, (_, index) => {
    const suffix = String(index + 1)
    return /\s/.test(normalized) ? `${normalized} ${suffix}` : `${normalized}${suffix}`
  })
}

function inferResourceType(name: string) {
  const match = name.match(/^(.*?)(?:\s?)(\d+)$/)
  if (!match) return name
  return match[1].trim() || name
}

function escapeRegExp(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

function buildNextInstanceName(typeName: string, instances: TaskDistributorResource[]) {
  const normalized = typeName.trim().replace(/\s+/g, ' ')
  if (!normalized) return ''

  const pattern = new RegExp(`^${escapeRegExp(normalized)}(?:\\s?)(\\d+)$`)
  let maxSuffix = 0

  for (const resource of instances) {
    const match = resource.name.match(pattern)
    if (!match) continue
    const numeric = Number(match[1])
    if (!Number.isNaN(numeric)) {
      maxSuffix = Math.max(maxSuffix, numeric)
    }
  }

  const nextSuffix = maxSuffix + 1
  return /\s/.test(normalized) ? `${normalized} ${nextSuffix}` : `${normalized}${nextSuffix}`
}

function isResourceType(resource: TaskDistributorResource) {
  return resource.kind === 'type'
}

function isResourceInstance(resource: TaskDistributorResource) {
  return !resource.kind || resource.kind === 'instance'
}

// ---------------------------------------------------------------------------
// Shared small components
// ---------------------------------------------------------------------------

function ActionButton({ onClick, disabled, tooltip, children, className }: {
  onClick: () => void
  disabled: boolean
  tooltip?: string
  children: React.ReactNode
  className: string
}) {
  return (
    <div className="relative group">
      <button onClick={onClick} disabled={disabled} className={className}>
        {children}
      </button>
      {disabled && tooltip && (
        <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-1 px-2 py-1
          bg-gray-900 text-white text-[10px] rounded whitespace-nowrap
          opacity-0 group-hover:opacity-100 transition-opacity pointer-events-none z-10">
          {tooltip}
        </div>
      )}
    </div>
  )
}

function SidebarSection({
  icon: Icon,
  title,
  count,
  defaultOpen = true,
  children,
}: {
  icon: React.ElementType
  title: string
  count?: number | string
  defaultOpen?: boolean
  children: React.ReactNode
}) {
  const [open, setOpen] = useState(defaultOpen)
  return (
    <div className="border-b border-border last:border-b-0">
      <button
        onClick={() => setOpen(v => !v)}
        className="flex w-full items-center gap-2 px-4 py-3 text-left text-sm font-semibold text-primary hover:bg-base/60 transition"
      >
        <Icon size={16} className="shrink-0 text-muted" />
        <span className="flex-1 truncate">{title}</span>
        {count != null && (
          <span className="rounded-full bg-surface px-2 py-0.5 text-[10px] font-medium text-secondary">
            {count}
          </span>
        )}
        {open ? <ChevronDown size={14} className="shrink-0 text-muted" /> : <ChevronRight size={14} className="shrink-0 text-muted" />}
      </button>
      {open && <div className="px-3 pb-3">{children}</div>}
    </div>
  )
}

function ThemedSection({
  icon: Icon,
  title,
  count,
  theme,
  defaultOpen = true,
  compact = false,
  children,
}: {
  icon: React.ElementType
  title: string
  count?: number | string
  theme: typeof SECTION_THEME[keyof typeof SECTION_THEME]
  defaultOpen?: boolean
  compact?: boolean
  children: React.ReactNode
}) {
  const [open, setOpen] = useState(defaultOpen)
  return (
    <section className={`rounded-2xl border border-border bg-surface shadow-sm shadow-slate-950/5 border-l-[3px] ${theme.barLeft}`}>
      <button
        onClick={() => setOpen(v => !v)}
        className={`flex w-full items-center gap-2 text-left transition hover:bg-base/40 ${compact ? 'px-3 py-2' : 'px-5 py-4 gap-3'}`}
      >
        <span className={`flex items-center justify-center rounded-lg ${compact ? 'h-6 w-6' : 'h-8 w-8 rounded-xl'} ${theme.iconBg}`}>
          <Icon size={compact ? 13 : 16} />
        </span>
        <span className={`flex-1 font-semibold text-primary ${compact ? 'text-xs' : 'text-sm'}`}>{title}</span>
        {count != null && (
          <span className={`rounded-full px-2 py-0.5 text-[10px] font-medium ${theme.pillBg}`}>
            {count}
          </span>
        )}
        {open ? <ChevronDown size={compact ? 12 : 14} className="text-muted" /> : <ChevronRight size={compact ? 12 : 14} className="text-muted" />}
      </button>
      {open && <div className={compact ? 'px-3 pb-3' : 'px-5 pb-5'}>{children}</div>}
    </section>
  )
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

export default function PDDL() {
  const { t } = useTranslation()
  const translateStatus = (status?: string) => {
    switch (status) {
      case 'pending': return t('status.pending')
      case 'running': return t('status.running')
      case 'completed': return t('status.completed')
      case 'failed': return t('status.failed')
      case 'cancelled': return t('status.cancelled')
      default: return t('status.pending')
    }
  }

  // -----------------------------------------------------------------------
  // Core state
  // -----------------------------------------------------------------------

  const [treeList, setTreeList] = useState<GraphListItem[]>([])
  const [selectedBTIds, setSelectedBTIds] = useState<string[]>([])
  const [btCache, setBtCache] = useState<Map<string, BehaviorTree>>(new Map())
  const [agents, setAgents] = useState<AgentWithCaps[]>([])
  const [selectedAgentIds, setSelectedAgentIds] = useState<string[]>([])
  const [goalState, setGoalState] = useState<Record<string, string>>({})
  const [initialState, setInitialState] = useState<Record<string, string>>({})
  const [plan, setPlan] = useState<PlanResult | null>(null)
  const [isLoading, setIsLoading] = useState(false)
  const [isSolving, setIsSolving] = useState(false)

  const [distributors, setDistributors] = useState<TaskDistributor[]>([])
  const [selectedDistributorId, setSelectedDistributorId] = useState<string | null>(null)

  const [autoLinkNotice, setAutoLinkNotice] = useState<string | null>(null)
  const [assignmentNotice, setAssignmentNotice] = useState<string | null>(null)
  const [showInitialState, setShowInitialState] = useState(false)

  const [executionId, setExecutionId] = useState<string | null>(null)
  const [execution, setExecution] = useState<PlanExecution | null>(null)
  const [resourceAllocations, setResourceAllocations] = useState<ResourceAllocation[]>([])

  // -----------------------------------------------------------------------
  // Inline TD creation state
  // -----------------------------------------------------------------------

  const [newTdName, setNewTdName] = useState('')
  const [editingTdId, setEditingTdId] = useState<string | null>(null)
  const [editTdName, setEditTdName] = useState('')

  // -----------------------------------------------------------------------
  // Inline Resource CRUD state
  // -----------------------------------------------------------------------

  const [newResourceName, setNewResourceName] = useState('')
  const [resourceTypeName, setResourceTypeName] = useState('')
  const [resourceTypeCount, setResourceTypeCount] = useState(2)
  const [resourceTypeDescription, setResourceTypeDescription] = useState('')
  const [resourceBuilderMessage, setResourceBuilderMessage] = useState<string | null>(null)
  const [isGeneratingResourceType, setIsGeneratingResourceType] = useState(false)
  const [typeInstanceDrafts, setTypeInstanceDrafts] = useState<Record<string, string>>({})

  // -----------------------------------------------------------------------
  // Inline State CRUD state
  // -----------------------------------------------------------------------

  const [newStateName, setNewStateName] = useState('')
  const [newStateType, setNewStateType] = useState<'bool' | 'int' | 'string'>('string')
  const [newStateInitialValue, setNewStateInitialValue] = useState('')
  const [editingStateId, setEditingStateId] = useState<string | null>(null)
  const [editStateInitialValue, setEditStateInitialValue] = useState('')

  // -----------------------------------------------------------------------
  // Derived values
  // -----------------------------------------------------------------------

  const selectedBTs = useMemo(
    () => selectedBTIds.map(id => btCache.get(id)).filter((bt): bt is BehaviorTree => bt != null),
    [selectedBTIds, btCache]
  )
  const selectedBT = selectedBTs[0] || null

  const selectedDistributor = distributors.find(d => d.id === selectedDistributorId) || null

  const stateVars = useMemo(
    () => [...(selectedDistributor?.states || [])].sort((a, b) => a.name.localeCompare(b.name)),
    [selectedDistributor]
  )

  const aggregatedResources = useMemo(
    () => [...(selectedDistributor?.resources || [])]
      .filter(resource => (resource.kind || 'instance') !== 'type')
      .sort((a, b) => a.name.localeCompare(b.name)),
    [selectedDistributor]
  )

  const resourceTypeGroups = useMemo(() => {
    if (!selectedDistributor) return []
    const groups = new Map<string, { typeResource: TaskDistributorResource | null; items: TaskDistributorResource[] }>()
    const resourceById = new Map((selectedDistributor.resources || []).map(resource => [resource.id, resource]))

    for (const resource of selectedDistributor.resources || []) {
      if (isResourceType(resource)) {
        const existing = groups.get(resource.name) || { typeResource: null, items: [] }
        existing.typeResource = resource
        groups.set(resource.name, existing)
        continue
      }

      const parentType = resource.parent_resource_id ? resourceById.get(resource.parent_resource_id) : null
      const typeName = parentType?.name || inferResourceType(resource.name)
      const existing = groups.get(typeName) || { typeResource: null, items: [] }
      existing.items.push(resource)
      groups.set(typeName, existing)
    }

    return Array.from(groups.entries())
      .map(([typeName, value]) => ({
        typeName,
        typeResource: value.typeResource,
        items: [...value.items].sort((a, b) => a.name.localeCompare(b.name)),
      }))
      .sort((a, b) => a.typeName.localeCompare(b.typeName))
  }, [selectedDistributor])

  const generatedResourceNames = useMemo(
    () => buildInstanceNames(resourceTypeName, resourceTypeCount),
    [resourceTypeName, resourceTypeCount]
  )

  const allSteps = useMemo<GraphStep[]>(
    () => selectedBT?.steps || [],
    [selectedBT]
  )

  const requiredActionTypes = useMemo(() => {
    const types = new Set<string>()
    for (const step of selectedBT?.steps || []) {
      if (step.action?.type) types.add(step.action.type)
    }
    return Array.from(types)
  }, [selectedBT])


  // -----------------------------------------------------------------------
  // Data loading
  // -----------------------------------------------------------------------

  const loadDistributors = useCallback(async () => {
    try {
      const list = await taskDistributorApi.list()
      const fullList = await Promise.all(list.map(d => taskDistributorApi.getFull(d.id)))
      setDistributors(fullList)
    } catch (err) {
      console.error('Failed to load distributors:', err)
    }
  }, [])

  const loadData = useCallback(async () => {
    setIsLoading(true)
    try {
      const [btList, agentList, capData] = await Promise.all([
        behaviorTreeApi.list(),
        agentApi.list(),
        capabilityApi.listAll(),
      ])
      setTreeList(btList)
      const agentsWithCaps: AgentWithCaps[] = agentList.map((agent: Agent) => {
        const agentCaps = capData.action_types
          .filter(c => c.agent_ids.includes(agent.id))
          .map(c => c.action_type)
        return { agent, capabilities: agentCaps, isOnline: agent.status === 'online' }
      })
      setAgents(agentsWithCaps)
    } catch (err) {
      console.error('Failed to load data:', err)
    } finally {
      setIsLoading(false)
    }
  }, [])

  useEffect(() => { loadData(); loadDistributors() }, [loadData, loadDistributors])

  // -----------------------------------------------------------------------
  // Effects
  // -----------------------------------------------------------------------

  useEffect(() => {
    if (!executionId) return
    const interval = setInterval(async () => {
      try {
        const [exec, agentList] = await Promise.all([
          pddlApi.getExecution(executionId),
          agentApi.list(),
        ])
        setExecution(exec)
        setResourceAllocations(exec.resources || [])
        setAgents(prev =>
          agentList.map((agent: Agent) => {
            const existing = prev.find(item => item.agent.id === agent.id)
            return {
              agent,
              capabilities: existing?.capabilities || [],
              isOnline: agent.status === 'online',
            }
          })
        )
        if (exec.status === 'completed' || exec.status === 'failed' || exec.status === 'cancelled') {
          clearInterval(interval)
        }
      } catch (err) {
        console.error('Failed to poll execution:', err)
        clearInterval(interval)
      }
    }, 1000)
    return () => clearInterval(interval)
  }, [executionId])

  const selectionKey = useMemo(
    () => `${[...selectedBTIds].sort().join(',')}|${selectedDistributorId || ''}`,
    [selectedBTIds, selectedDistributorId]
  )
  useEffect(() => {
    setGoalState({})
    setInitialState({})
    setPlan(null)
    setExecutionId(null)
    setExecution(null)
    setResourceAllocations([])
    setResourceBuilderMessage(null)
    setTypeInstanceDrafts({})
  }, [selectionKey])

  // -----------------------------------------------------------------------
  // BT / Agent handlers
  // -----------------------------------------------------------------------

  const handleToggleBT = useCallback(async (id: string) => {
    if (selectedBTIds.includes(id)) {
      setSelectedBTIds([])
      return
    }
    let bt = btCache.get(id)
    if (!bt) {
      try {
        bt = await behaviorTreeApi.get(id)
        setBtCache(prev => new Map(prev).set(id, bt!))
      } catch (err) {
        console.error('Failed to load BT:', err)
        return
      }
    }
    setSelectedBTIds([id])
    if (bt.task_distributor_id) {
      setSelectedDistributorId(bt.task_distributor_id)
      const td = distributors.find(d => d.id === bt!.task_distributor_id)
      if (td) {
        setAutoLinkNotice(td.name)
        setTimeout(() => setAutoLinkNotice(null), 4000)
      }
    } else if (selectedDistributorId) {
      try {
        const updated = await behaviorTreeApi.update(id, {
          task_distributor_id: selectedDistributorId,
        })
        setBtCache(prev => new Map(prev).set(updated.id, updated))
        setAssignmentNotice(
          t('pddl.distributorAssignedToTask', {
            distributor: distributors.find(d => d.id === selectedDistributorId)?.name || selectedDistributorId,
            task: updated.name,
          })
        )
        setTimeout(() => setAssignmentNotice(null), 4000)
      } catch (err) {
        console.error('Failed to assign selected distributor to task:', err)
        setAssignmentNotice(t('pddl.distributorAssignError'))
        setTimeout(() => setAssignmentNotice(null), 4000)
      }
    }
  }, [selectedBTIds, btCache, distributors, selectedDistributorId, t])

  const toggleAgent = (id: string) => {
    setSelectedAgentIds(prev =>
      prev.includes(id) ? prev.filter(a => a !== id) : [...prev, id]
    )
  }

  const handleSelectDistributor = useCallback(async (distributorId: string) => {
    setSelectedDistributorId(distributorId)

    if (!selectedBT || selectedBT.task_distributor_id === distributorId) {
      return
    }

    try {
      const updated = await behaviorTreeApi.update(selectedBT.id, {
        task_distributor_id: distributorId,
      })
      setBtCache(prev => new Map(prev).set(updated.id, updated))
      setAssignmentNotice(
        t('pddl.distributorAssignedToTask', {
          distributor: distributors.find(d => d.id === distributorId)?.name || distributorId,
          task: updated.name,
        })
      )
      setTimeout(() => setAssignmentNotice(null), 4000)
    } catch (err) {
      console.error('Failed to assign task distributor:', err)
      setAssignmentNotice(t('pddl.distributorAssignError'))
      setTimeout(() => setAssignmentNotice(null), 4000)
    }
  }, [selectedBT, distributors, t])

  // -----------------------------------------------------------------------
  // TD CRUD handlers
  // -----------------------------------------------------------------------

  const handleCreateTd = useCallback(async () => {
    if (!newTdName.trim()) return
    try {
      const created = await taskDistributorApi.create({ name: newTdName.trim() })
      setNewTdName('')
      await loadDistributors()
      setSelectedDistributorId(created.id)
    } catch (err) {
      console.error('Failed to create distributor:', err)
    }
  }, [newTdName, loadDistributors])

  const handleRenameTd = useCallback(async (id: string) => {
    if (!editTdName.trim()) return
    try {
      await taskDistributorApi.update(id, { name: editTdName.trim() })
      setEditingTdId(null)
      loadDistributors()
    } catch (err) {
      console.error('Failed to rename distributor:', err)
    }
  }, [editTdName, loadDistributors])

  const handleDeleteTd = useCallback(async (id: string) => {
    try {
      await taskDistributorApi.delete(id)
      if (selectedDistributorId === id) setSelectedDistributorId(null)
      loadDistributors()
    } catch (err) {
      console.error('Failed to delete distributor:', err)
    }
  }, [selectedDistributorId, loadDistributors])

  // -----------------------------------------------------------------------
  // Resource CRUD handlers
  // -----------------------------------------------------------------------

  const handleAddResource = useCallback(async () => {
    if (!selectedDistributor || !newResourceName.trim()) return
    try {
      await taskDistributorApi.createResource(selectedDistributor.id, {
        name: newResourceName.trim(),
        kind: 'instance',
      })
      setNewResourceName('')
      loadDistributors()
    } catch (err) {
      console.error('Failed to add resource:', err)
    }
  }, [selectedDistributor, newResourceName, loadDistributors])

  const handleDeleteResource = useCallback(async (resourceId: string) => {
    if (!selectedDistributor) return
    try {
      await taskDistributorApi.deleteResource(selectedDistributor.id, resourceId)
      loadDistributors()
    } catch (err) {
      console.error('Failed to delete resource:', err)
    }
  }, [selectedDistributor, loadDistributors])

  const handleGenerateResourceType = useCallback(async () => {
    if (!selectedDistributor || generatedResourceNames.length === 0) return

    const resourceList = selectedDistributor.resources || []
    const existingType = resourceList.find(resource => isResourceType(resource) && resource.name === resourceTypeName.trim())
    const existingResourceNames = new Set(resourceList.filter(isResourceInstance).map(resource => resource.name))

    const resourcesToCreate = generatedResourceNames.filter(name => !existingResourceNames.has(name))

    setIsGeneratingResourceType(true)
    setResourceBuilderMessage(null)

    try {
      const typeResource = existingType || await taskDistributorApi.createResource(selectedDistributor.id, {
        name: resourceTypeName.trim(),
        kind: 'type',
        description: resourceTypeDescription.trim() || undefined,
      })

      await Promise.all(resourcesToCreate.map(name =>
        taskDistributorApi.createResource(selectedDistributor.id, {
          name,
          kind: 'instance',
          parent_resource_id: typeResource.id,
          description: resourceTypeDescription.trim() || undefined,
        })
      ))

      const skippedCount = generatedResourceNames.length - resourcesToCreate.length
      setResourceBuilderMessage(
        t('pddl.generatorSummary', {
          resources: String(resourcesToCreate.length),
          skipped: String(skippedCount),
        })
      )
      setResourceTypeName('')
      setResourceTypeDescription('')
      setResourceTypeCount(2)
      await loadDistributors()
    } catch (err) {
      console.error('Failed to generate resource type:', err)
      setResourceBuilderMessage(t('pddl.generatorError'))
    } finally {
      setIsGeneratingResourceType(false)
    }
  }, [
    generatedResourceNames,
    loadDistributors,
    resourceTypeDescription,
    resourceTypeName,
    selectedDistributor,
    t,
  ])

  const handleAddInstanceToType = useCallback(async (
    group: { typeName: string; typeResource: TaskDistributorResource | null; items: TaskDistributorResource[] }
  ) => {
    if (!selectedDistributor) return

    const groupKey = group.typeResource?.id || group.typeName
    const suggestedName = buildNextInstanceName(group.typeName, group.items)
    const nextName = (typeInstanceDrafts[groupKey] || '').trim() || suggestedName
    if (!nextName) return

    const duplicate = (selectedDistributor.resources || []).some(resource => resource.name === nextName)
    if (duplicate) {
      setResourceBuilderMessage(t('pddl.resourceInstanceExists', { name: nextName }))
      return
    }

    try {
      const ensuredType = group.typeResource || await taskDistributorApi.createResource(selectedDistributor.id, {
        name: group.typeName,
        kind: 'type',
      })

      await taskDistributorApi.createResource(selectedDistributor.id, {
        name: nextName,
        kind: 'instance',
        parent_resource_id: ensuredType.id,
      })

      setTypeInstanceDrafts(prev => ({ ...prev, [groupKey]: '' }))
      setResourceBuilderMessage(t('pddl.instanceAddedToType', { instance: nextName, type: group.typeName }))
      await loadDistributors()
    } catch (err) {
      console.error('Failed to add typed resource instance:', err)
      setResourceBuilderMessage(t('pddl.generatorError'))
    }
  }, [loadDistributors, selectedDistributor, t, typeInstanceDrafts])

  // -----------------------------------------------------------------------
  // State CRUD handlers
  // -----------------------------------------------------------------------

  const handleAddState = useCallback(async () => {
    if (!selectedDistributor || !newStateName.trim()) return
    try {
      await taskDistributorApi.createState(selectedDistributor.id, {
        name: newStateName.trim(),
        type: newStateType,
        initial_value: newStateInitialValue || undefined,
      })
      setNewStateName('')
      setNewStateInitialValue('')
      loadDistributors()
    } catch (err) {
      console.error('Failed to add state:', err)
    }
  }, [selectedDistributor, newStateName, newStateType, newStateInitialValue, loadDistributors])

  const handleDeleteState = useCallback(async (stateId: string) => {
    if (!selectedDistributor) return
    try {
      await taskDistributorApi.deleteState(selectedDistributor.id, stateId)
      loadDistributors()
    } catch (err) {
      console.error('Failed to delete state:', err)
    }
  }, [selectedDistributor, loadDistributors])

  const handleUpdateStateInitialValue = useCallback(async (sv: TaskDistributorState) => {
    if (!selectedDistributor) return
    try {
      await taskDistributorApi.updateState(selectedDistributor.id, sv.id, {
        name: sv.name,
        type: sv.type,
        initial_value: editStateInitialValue,
        description: sv.description,
      })
      setEditingStateId(null)
      loadDistributors()
    } catch (err) {
      console.error('Failed to update state:', err)
    }
  }, [selectedDistributor, editStateInitialValue, loadDistributors])

  // -----------------------------------------------------------------------
  // Plan handlers
  // -----------------------------------------------------------------------

  const handlePreview = async () => {
    if (!selectedBT || !selectedDistributor || selectedAgentIds.length === 0 || Object.keys(goalState).length === 0) return
    setIsSolving(true); setPlan(null); setExecutionId(null); setExecution(null); setResourceAllocations([])
    try {
      const result = await pddlApi.preview({
        behavior_tree_id: selectedBT.id,
        task_distributor_id: selectedDistributor.id,
        initial_state: Object.keys(initialState).length > 0 ? initialState : undefined,
        goal_state: goalState,
        agent_ids: selectedAgentIds,
      })
      setPlan(result)
    } catch (err) {
      console.error('Preview failed:', err)
      setPlan({ assignments: [], is_valid: false, error_message: String(err), total_steps: 0, parallel_groups: 0 })
    } finally { setIsSolving(false) }
  }

  const handleSaveAndSolve = async () => {
    if (!selectedBT || !selectedDistributor || selectedAgentIds.length === 0 || Object.keys(goalState).length === 0) return
    setIsSolving(true); setPlan(null); setExecutionId(null); setExecution(null); setResourceAllocations([])
    try {
      const problem = await pddlApi.createProblem({
        name: `${selectedBT.name} - ${new Date().toLocaleString()}`,
        behavior_tree_id: selectedBT.id,
        task_distributor_id: selectedDistributor.id,
        initial_state: Object.keys(initialState).length > 0 ? initialState : undefined,
        goal_state: goalState,
        agent_ids: selectedAgentIds,
      })
      const solved = await pddlApi.solveProblem(problem.id)
      if (solved.plan_result) setPlan(solved.plan_result)
    } catch (err) {
      console.error('Solve failed:', err)
      setPlan({ assignments: [], is_valid: false, error_message: String(err), total_steps: 0, parallel_groups: 0 })
    } finally { setIsSolving(false) }
  }

  const handleExecute = async () => {
    if (!plan?.is_valid || !selectedBT || !selectedDistributor) return
    try {
      const problem = await pddlApi.createProblem({
        name: `${selectedBT.name} - Exec ${new Date().toLocaleString()}`,
        behavior_tree_id: selectedBT.id,
        task_distributor_id: selectedDistributor.id,
        initial_state: Object.keys(initialState).length > 0 ? initialState : undefined,
        goal_state: goalState,
        agent_ids: selectedAgentIds,
      })
      const solved = await pddlApi.solveProblem(problem.id)
      if (!solved.plan_result?.is_valid) { console.error('Plan invalid before execution'); return }
      const result = await pddlApi.executePlan(problem.id)
      setExecutionId(result.execution_id)
    } catch (err) { console.error('Execution failed:', err) }
  }

  const handleCancelExecution = async () => {
    if (!executionId) return
    try { await pddlApi.cancelExecution(executionId) } catch (err) { console.error('Cancel failed:', err) }
  }

  // -----------------------------------------------------------------------
  // Computed helpers
  // -----------------------------------------------------------------------

  const isExecuting = execution?.status === 'running' || execution?.status === 'pending'
  const goalCount = Object.keys(goalState).length
  const initialOverrideCount = Object.keys(initialState).length
  const recommendedAgents = useMemo(
    () => agents.filter(a =>
      a.isOnline &&
      requiredActionTypes.length > 0 &&
      requiredActionTypes.every(type => a.capabilities.includes(type))
    ),
    [agents, requiredActionTypes]
  )
  const recommendedAgentIds = recommendedAgents.map(({ agent }) => agent.id)
  const recommendedSet = useMemo(() => new Set(recommendedAgentIds), [recommendedAgentIds])
  const aggregatedResourceNames = useMemo(
    () => new Set(aggregatedResources.map(r => r.name)),
    [aggregatedResources]
  )
  const activeResourceAllocations = useMemo(
    () => resourceAllocations.filter(a => aggregatedResourceNames.has(a.resource)),
    [resourceAllocations, aggregatedResourceNames]
  )
  const allocationMap = useMemo(() => {
    const m = new Map<string, ResourceAllocation>()
    for (const a of activeResourceAllocations) m.set(a.resource, a)
    return m
  }, [activeResourceAllocations])
  const runtimePlanningState = useMemo(() => {
    const next: Record<string, string> = {}
    for (const stateVar of stateVars) {
      if (stateVar.initial_value != null && stateVar.initial_value !== '') {
        next[stateVar.name] = stateVar.initial_value
      }
    }
    for (const [key, value] of Object.entries(initialState)) {
      next[key] = value
    }
    for (const [key, value] of Object.entries(execution?.planning_state || {})) {
      next[key] = value
    }
    return next
  }, [stateVars, initialState, execution])
  const agentDispatchMap = useMemo(() => {
    const m = new Map<string, PlanExecution['steps']>()
    for (const dispatch of execution?.steps || []) {
      const existing = m.get(dispatch.agent_id) || []
      existing.push(dispatch)
      existing.sort((left, right) => {
        const rank = (status: string) => (
          status === 'running' ? 0 :
          status === 'pending' ? 1 :
          status === 'failed' ? 2 :
          status === 'completed' ? 3 : 4
        )
        const byStatus = rank(left.status) - rank(right.status)
        if (byStatus !== 0) return byStatus
        return left.order - right.order
      })
      m.set(dispatch.agent_id, existing)
    }
    return m
  }, [execution])
  const heldResourcesByAgent = useMemo(() => {
    const m = new Map<string, ResourceAllocation[]>()
    for (const allocation of activeResourceAllocations) {
      const keys = [
        allocation.holder_agent_id,
        allocation.holder_agent_name,
        allocation.holder_agent,
      ].filter(Boolean) as string[]
      for (const key of keys) {
        const existing = m.get(key) || []
        existing.push(allocation)
        m.set(key, existing)
      }
    }
    return m
  }, [activeResourceAllocations])
  const planStatus = execution?.status
    ? translateStatus(execution.status)
    : plan?.is_valid
      ? t('pddl.planReady')
      : plan
        ? t('status.failed')
        : t('pddl.planDraft')
  const pendingRequirements = useMemo(() => {
    const r: string[] = []
    if (!selectedDistributor) r.push(t('pddl.needDistributor'))
    if (selectedBTs.length === 0) r.push(t('pddl.needBT'))
    if (selectedAgentIds.length === 0) r.push(t('pddl.needAgents'))
    if (goalCount === 0) r.push(t('pddl.needGoal'))
    return r
  }, [selectedDistributor, selectedBTs.length, selectedAgentIds.length, goalCount, t])
  const solveTooltip = useMemo(() => {
    if (!selectedDistributor) return t('pddl.needDistributor')
    if (selectedBTs.length === 0) return t('pddl.needBT')
    if (selectedAgentIds.length === 0) return t('pddl.needAgents')
    if (Object.keys(goalState).length === 0) return t('pddl.needGoal')
    return undefined
  }, [selectedDistributor, selectedBTs.length, selectedAgentIds, goalState, t])
  const executeTooltip = useMemo(() => {
    if (!selectedDistributor) return t('pddl.needDistributor')
    if (!selectedBT) return t('pddl.needBT')
    if (isExecuting) return t('pddl.alreadyExecuting')
    if (!plan?.is_valid) return t('pddl.needValidPlan')
    return undefined
  }, [selectedDistributor, selectedBT, plan, isExecuting, t])
  const canSolve = !!selectedDistributor && !!selectedBT && selectedAgentIds.length > 0 && Object.keys(goalState).length > 0
  const canExecute = !!selectedDistributor && !!selectedBT && !!plan?.is_valid && !isExecuting

  const sortedAgents = useMemo(
    () => [...agents].sort((a, b) => {
      const aSelected = selectedAgentIds.includes(a.agent.id) ? 1 : 0
      const bSelected = selectedAgentIds.includes(b.agent.id) ? 1 : 0
      if (aSelected !== bSelected) return bSelected - aSelected
      const aRec = recommendedSet.has(a.agent.id) ? 1 : 0
      const bRec = recommendedSet.has(b.agent.id) ? 1 : 0
      if (aRec !== bRec) return bRec - aRec
      if (a.isOnline !== b.isOnline) return a.isOnline ? -1 : 1
      return a.agent.name.localeCompare(b.agent.name)
    }),
    [agents, selectedAgentIds, recommendedSet]
  )

  // -----------------------------------------------------------------------
  // Render
  // -----------------------------------------------------------------------

  return (
    <div className="flex h-[calc(100vh-48px)] bg-base overflow-hidden">
      {/* ================================================================ */}
      {/* SIDEBAR                                                         */}
      {/* ================================================================ */}
      <aside className="flex w-[340px] shrink-0 flex-col border-r border-border bg-surface overflow-hidden">
        {/* Header */}
        <div className="shrink-0 border-b border-border px-4 py-4">
          <div className="flex items-center justify-between gap-2">
            <div>
              <h1 className="text-base font-semibold tracking-tight text-primary">{t('pddl.title')}</h1>
              <p className="mt-1 text-xs text-secondary leading-5">{t('pddl.subtitle')}</p>
            </div>
            <button
              onClick={() => { loadData(); loadDistributors() }}
              className="rounded-xl border border-border p-2 text-muted transition hover:text-primary"
              title={t('common.refresh')}
            >
              <RefreshCw size={14} className={isLoading ? 'animate-spin' : ''} />
            </button>
          </div>
          {autoLinkNotice && (
            <div className="mt-3 inline-flex items-center gap-2 rounded-xl border border-accent/20 bg-accent/10 px-2.5 py-1.5 text-[11px] text-accent">
              <Info size={12} />
              {t('pddl.autoLinkedTD', { name: autoLinkNotice })}
            </div>
          )}
          {assignmentNotice && (
            <div className="mt-2 inline-flex items-center gap-2 rounded-xl border border-emerald-500/20 bg-emerald-500/10 px-2.5 py-1.5 text-[11px] text-emerald-300">
              <Info size={12} />
              {assignmentNotice}
            </div>
          )}
        </div>

        <div className="flex-1 overflow-y-auto">
          {/* ---- Distributors ---- */}
          <SidebarSection icon={Database} title={t('pddl.taskDistributor')} count={distributors.length}>
            {/* Inline create */}
            <div className="mb-2 flex gap-1.5">
              <input
                className="flex-1 rounded-lg border border-border bg-base px-2.5 py-2 text-sm text-primary placeholder:text-muted outline-none transition focus:border-accent focus:ring-1 focus:ring-accent/30"
                placeholder={t('pddl.distributorName')}
                value={newTdName}
                onChange={e => setNewTdName(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && handleCreateTd()}
              />
              <button
                onClick={handleCreateTd}
                disabled={!newTdName.trim()}
                className="rounded-lg bg-accent px-2.5 py-2 text-white transition hover:bg-accent/80 disabled:opacity-40"
              >
                <Plus size={14} />
              </button>
            </div>
            {distributors.length === 0 ? (
              <p className="rounded-xl border border-dashed border-border bg-base/40 px-3 py-6 text-center text-xs text-muted">
                {t('pddl.noDistributors')}
              </p>
            ) : (
              <div className="space-y-1">
                {distributors.map(d => (
                  <div
                    key={d.id}
                    className={`group flex items-center gap-2 rounded-xl px-3 py-2.5 transition cursor-pointer ${
                      selectedDistributorId === d.id
                        ? 'bg-accent/10 border border-accent/30'
                        : 'border border-transparent hover:bg-base/60'
                    }`}
                    onClick={() => handleSelectDistributor(d.id)}
                  >
                    {editingTdId === d.id ? (
                      <div className="flex flex-1 items-center gap-1.5" onClick={e => e.stopPropagation()}>
                        <input
                          className="flex-1 rounded-lg border border-border bg-base px-2 py-1.5 text-sm text-primary outline-none"
                          value={editTdName}
                          onChange={e => setEditTdName(e.target.value)}
                          onKeyDown={e => e.key === 'Enter' && handleRenameTd(d.id)}
                          autoFocus
                        />
                        <button onClick={() => handleRenameTd(d.id)} className="p-1 text-green-400"><Check size={13} /></button>
                        <button onClick={() => setEditingTdId(null)} className="p-1 text-muted"><X size={13} /></button>
                      </div>
                    ) : (
                      <>
                        <span className={`h-2 w-2 shrink-0 rounded-full ${
                          selectedDistributorId === d.id ? 'bg-accent' : 'bg-muted'
                        }`} />
                        <div className="min-w-0 flex-1">
                          <div className="truncate text-sm font-medium text-primary">{d.name}</div>
                          <div className="mt-0.5 text-[10px] text-secondary">
                            {d.resources?.length ?? 0} {t('pddl.resources')} · {d.states?.length ?? 0} {t('pddl.states')}
                          </div>
                        </div>
                        <div className="flex gap-0.5 opacity-0 group-hover:opacity-100 transition" onClick={e => e.stopPropagation()}>
                          <button
                            onClick={() => { setEditingTdId(d.id); setEditTdName(d.name) }}
                            className="rounded-lg p-1 text-muted hover:text-primary"
                          >
                            <Edit size={12} />
                          </button>
                          <button
                            onClick={() => handleDeleteTd(d.id)}
                            className="rounded-lg p-1 text-muted hover:text-red-400"
                          >
                            <Trash2 size={12} />
                          </button>
                        </div>
                      </>
                    )}
                  </div>
                ))}
              </div>
            )}
          </SidebarSection>

          {/* ---- Tasks (BT selection) ---- */}
          <SidebarSection
            icon={Workflow}
            title={t('pddl.selectBT')}
            count={selectedBT ? '1' : undefined}
          >
            {treeList.length === 0 ? (
              <p className="rounded-xl border border-dashed border-border bg-base/40 px-3 py-6 text-center text-xs text-muted">
                {t('actionGraph.noGraphs')}
              </p>
            ) : (
              <div className="max-h-[320px] space-y-1 overflow-y-auto">
                {treeList.map(item => {
                  const isSelected = selectedBTIds.includes(item.id)
                  const cached = btCache.get(item.id)
                  const linkedTdId = cached?.task_distributor_id
                  const linkedTd = linkedTdId ? distributors.find(d => d.id === linkedTdId) : null
                  return (
                    <label
                      key={item.id}
                      className={`flex cursor-pointer items-center gap-2.5 rounded-xl px-3 py-2.5 transition ${
                        isSelected ? 'bg-accent/5 border border-accent/20' : 'border border-transparent hover:bg-base/60'
                      }`}
                    >
                      <input
                        type="radio"
                        name="pddl-selected-task"
                        checked={isSelected}
                        onChange={() => handleToggleBT(item.id)}
                        className="h-3.5 w-3.5 shrink-0 border-border text-accent focus:ring-accent/30"
                      />
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-1.5">
                          <span className="truncate text-sm font-medium text-primary">{item.name}</span>
                          <span className="shrink-0 text-[10px] text-muted">v{item.version}</span>
                        </div>
                        <div className="mt-0.5 text-[10px] text-secondary">
                          {item.step_count} {t('pddl.steps')}
                        </div>
                      </div>
                      {linkedTd && (
                        <span className="inline-flex shrink-0 items-center gap-1 rounded-lg border border-accent/20 bg-accent/10 px-1.5 py-0.5 text-[9px] font-medium text-accent">
                          <Link2 size={9} />
                          {linkedTd.name}
                        </span>
                      )}
                    </label>
                  )
                })}
              </div>
            )}
            {selectedBT && (
              <div className="mt-2 rounded-xl border border-border bg-surface px-3 py-2 text-[11px] text-secondary">
                {t('pddl.sidebarTasksSummary', { steps: String(allSteps.length), actions: String(requiredActionTypes.length) })}
              </div>
            )}
          </SidebarSection>
        </div>
      </aside>

      {/* ================================================================ */}
      {/* MAIN CONTENT                                                    */}
      {/* ================================================================ */}
      <main className="flex flex-1 flex-col overflow-hidden">
        {/* ---- Action Bar ---- */}
        <div className="shrink-0 border-b border-border bg-surface px-5 py-3">
          <div className="flex items-center justify-between gap-4">
            <div className="flex items-center gap-3 min-w-0">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="truncate text-sm font-semibold text-primary">
                    {selectedDistributor?.name || t('pddl.noDistributorSelected')}
                  </span>
                  <span className={`rounded-full px-2 py-0.5 text-[10px] font-medium ${
                    plan?.is_valid ? 'bg-emerald-500/10 text-emerald-400'
                    : plan ? 'bg-red-500/10 text-red-400'
                    : 'bg-surface text-muted'
                  }`}>
                    {planStatus}
                  </span>
                </div>
                <div className="mt-0.5 flex items-center gap-2 text-[10px] text-secondary">
                  <span>{selectedBT ? selectedBT.name : `0 ${t('pddl.tasksSelectedShort')}`}</span>
                  <span>·</span>
                  <span>{selectedAgentIds.length} {t('pddl.agentsSelectedShort')}</span>
                  <span>·</span>
                  <span>{goalCount} {t('pddl.goalState')}</span>
                </div>
              </div>
            </div>
            <div className="flex items-center gap-2 shrink-0">
              <ActionButton
                onClick={handlePreview}
                disabled={!canSolve || isSolving}
                tooltip={solveTooltip}
                className="inline-flex items-center gap-1.5 rounded-xl border border-border bg-base/70 px-3 py-2 text-xs font-medium text-secondary transition hover:border-accent/20 hover:bg-accent/10 hover:text-accent disabled:cursor-not-allowed disabled:opacity-40"
              >
                <Eye size={14} />
                {t('pddl.preview')}
              </ActionButton>
              <ActionButton
                onClick={handleSaveAndSolve}
                disabled={!canSolve || isSolving}
                tooltip={solveTooltip}
                className="inline-flex items-center gap-1.5 rounded-xl bg-accent px-3 py-2 text-xs font-medium text-white transition hover:bg-accent/80 disabled:cursor-not-allowed disabled:opacity-40"
              >
                <Play size={14} />
                {t('pddl.solve')}
              </ActionButton>
              <ActionButton
                onClick={handleExecute}
                disabled={!canExecute}
                tooltip={executeTooltip}
                className="inline-flex items-center gap-1.5 rounded-xl bg-green-600 px-3 py-2 text-xs font-medium text-white transition hover:bg-green-500 disabled:cursor-not-allowed disabled:opacity-40"
              >
                <Play size={14} />
                {t('pddl.execute')}
              </ActionButton>
              {isExecuting && (
                <button
                  onClick={handleCancelExecution}
                  className="inline-flex items-center gap-1.5 rounded-xl bg-red-600 px-3 py-2 text-xs font-medium text-white transition hover:bg-red-500"
                >
                  <Square size={14} />
                  {t('pddl.cancelExecution')}
                </button>
              )}
            </div>
          </div>
        </div>

        {/* ---- Scrollable body ---- */}
        {!selectedDistributor ? (
          <div className="flex flex-1 items-center justify-center p-8">
            <div className="text-center max-w-sm">
              <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-2xl border border-border bg-base/70 text-muted">
                <Layers size={28} />
              </div>
              <h2 className="mt-5 text-lg font-semibold text-primary">{t('pddl.emptyMainTitle')}</h2>
              <p className="mt-2 text-sm leading-6 text-secondary">{t('pddl.emptyMainHint')}</p>
            </div>
          </div>
        ) : (
          <div className="flex-1 overflow-y-auto p-4 space-y-4">

            {/* ---- Top 4 sections: horizontal grid ---- */}
            <div className="grid grid-cols-4 gap-2 items-start">

            {/* =================== RESOURCE — amber =================== */}
            <ThemedSection icon={Gem} title="Resource" count={aggregatedResources.length} theme={SECTION_THEME.resource} compact>
              <div className="max-h-[280px] overflow-y-auto space-y-2">
                <div className="rounded border border-amber-500/20 bg-amber-500/5 p-2 space-y-1.5">
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-[10px] font-medium text-amber-300">{t('pddl.resourceTypeGeneratorTitle')}</span>
                    <span className="text-[9px] text-secondary">{t('pddl.instanceCount')}</span>
                  </div>
                  <div className="flex gap-1">
                    <input
                      className="flex-1 min-w-0 rounded border border-border bg-base px-2 py-1 text-[11px] text-primary placeholder:text-muted outline-none focus:border-amber-500/50"
                      placeholder={t('pddl.resourceTypePlaceholder')}
                      value={resourceTypeName}
                      onChange={e => setResourceTypeName(e.target.value)}
                    />
                    <input
                      type="number"
                      min={1}
                      max={32}
                      className="w-12 rounded border border-border bg-base px-1.5 py-1 text-[11px] text-primary outline-none focus:border-amber-500/50"
                      value={resourceTypeCount}
                      onChange={e => setResourceTypeCount(Math.max(1, Number(e.target.value) || 1))}
                    />
                    <button
                      onClick={handleGenerateResourceType}
                      disabled={!resourceTypeName.trim() || isGeneratingResourceType}
                      className="rounded bg-amber-500/20 px-1.5 text-amber-400 disabled:opacity-40"
                      title={t('pddl.generateResourceType')}
                    >
                      <Plus size={12} />
                    </button>
                  </div>
                  <input
                    className="w-full rounded border border-border bg-base px-2 py-1 text-[11px] text-primary placeholder:text-muted outline-none focus:border-amber-500/50"
                    placeholder={t('pddl.resourceDescriptionPlaceholder')}
                    value={resourceTypeDescription}
                    onChange={e => setResourceTypeDescription(e.target.value)}
                  />
                  {generatedResourceNames.length > 0 && (
                    <div className="flex flex-wrap gap-1">
                      {generatedResourceNames.map((name, index) => (
                        <span key={name} className="rounded-full bg-amber-500/10 px-2 py-0.5 text-[9px] text-amber-300">
                          {name}
                        </span>
                      ))}
                    </div>
                  )}
                  {resourceBuilderMessage && (
                    <div className="rounded border border-amber-500/20 bg-base/60 px-2 py-1 text-[9px] text-amber-200">
                      {resourceBuilderMessage}
                    </div>
                  )}
                </div>

                <div className="rounded border border-border bg-base/40 p-2">
                  <div className="mb-1.5 flex items-center justify-between gap-2">
                    <span className="text-[10px] font-medium text-amber-300">{t('pddl.currentResourceTypesTitle')}</span>
                    <span className="rounded-full bg-base px-1.5 py-0.5 text-[9px] text-secondary">{resourceTypeGroups.length}</span>
                  </div>
                  {resourceTypeGroups.length === 0 ? (
                    <p className="py-2 text-center text-[10px] text-muted">{t('pddl.currentResourceTypesEmpty')}</p>
                  ) : (
                    <div className="space-y-1.5">
                      {resourceTypeGroups.map(group => (
                        <div key={group.typeName} className="rounded border border-border bg-surface/60 p-2">
                          {(() => {
                            const groupKey = group.typeResource?.id || group.typeName
                            const suggestedName = buildNextInstanceName(group.typeName, group.items)
                            return (
                              <>
                          <div className="flex items-center justify-between gap-2">
                            <div className="min-w-0">
                              <div className="flex items-center gap-1.5">
                                <span className="rounded-full bg-amber-500/10 px-1.5 py-0.5 text-[8px] font-semibold text-amber-300">TYPE</span>
                                <span className="truncate text-[11px] font-medium text-primary">{group.typeName}</span>
                              </div>
                              {group.typeResource?.description && (
                                <div className="truncate text-[9px] text-secondary">{group.typeResource.description}</div>
                              )}
                            </div>
                            <div className="flex items-center gap-1">
                              <span className="rounded-full bg-base px-1.5 py-0.5 text-[9px] text-secondary">{group.items.length}</span>
                              {group.typeResource && (
                                <button
                                  onClick={() => handleDeleteResource(group.typeResource!.id)}
                                  className="text-muted hover:text-red-400"
                                  title={t('common.delete')}
                                >
                                  <Trash2 size={11} />
                                </button>
                              )}
                            </div>
                          </div>
                          <div className="mt-1.5 flex gap-1">
                            <input
                              className="flex-1 min-w-0 rounded border border-border bg-base px-2 py-1 text-[10px] text-primary placeholder:text-muted outline-none focus:border-amber-500/50"
                              placeholder={suggestedName || t('pddl.instanceLabel')}
                              value={typeInstanceDrafts[groupKey] || ''}
                              onChange={e => setTypeInstanceDrafts(prev => ({ ...prev, [groupKey]: e.target.value }))}
                              onKeyDown={e => e.key === 'Enter' && handleAddInstanceToType(group)}
                            />
                            <button
                              onClick={() => handleAddInstanceToType(group)}
                              className="rounded bg-amber-500/20 px-1.5 text-amber-400 disabled:opacity-40"
                              title={t('common.create')}
                            >
                              <Plus size={11} />
                            </button>
                          </div>
                          {suggestedName && (
                            <div className="mt-1 text-[8px] text-secondary">
                              {t('pddl.autoInstanceNameHint', { name: suggestedName })}
                            </div>
                          )}
                          <div className="mt-1.5 flex flex-wrap gap-1">
                            {group.items.length === 0 ? (
                              <span className="text-[9px] text-muted">{t('pddl.noResourceBoardTitle')}</span>
                            ) : group.items.map(item => {
                              const alloc = allocationMap.get(item.name)
                              const isHeld = !!alloc
                              const holderLabel = alloc?.holder_agent_name || alloc?.holder_agent || alloc?.holder_agent_id
                              return (
                                <span
                                  key={item.id}
                                  className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[9px] ${
                                    isHeld ? 'bg-amber-500/15 text-amber-300' : 'bg-base text-secondary'
                                  }`}
                                >
                                  <Circle size={6} className={isHeld ? 'fill-amber-400 text-amber-400' : 'fill-emerald-400 text-emerald-400'} />
                                  <span className="max-w-[110px] truncate">{item.name}</span>
                                  {holderLabel && (
                                    <span className="max-w-[72px] truncate text-amber-200/70">{holderLabel}</span>
                                  )}
                                  <button
                                    onClick={() => handleDeleteResource(item.id)}
                                    className="text-muted hover:text-red-400"
                                    title={t('common.delete')}
                                  >
                                    <X size={9} />
                                  </button>
                                </span>
                              )
                            })}
                          </div>
                              </>
                            )
                          })()}
                        </div>
                      ))}
                    </div>
                  )}
                </div>

                <div className="rounded border border-border bg-base/40 p-2 space-y-1.5">
                  <span className="text-[10px] font-medium text-amber-300">{t('pddl.resourceName')} · {t('pddl.standaloneResource')}</span>
                  <div className="flex gap-1">
                    <input
                      className="flex-1 min-w-0 rounded border border-border bg-base px-2 py-1 text-[11px] text-primary placeholder:text-muted outline-none focus:border-amber-500/50"
                      placeholder={t('pddl.resourceName')}
                      value={newResourceName}
                      onChange={e => setNewResourceName(e.target.value)}
                      onKeyDown={e => e.key === 'Enter' && handleAddResource()}
                    />
                    <button onClick={handleAddResource} disabled={!newResourceName.trim()} className="rounded bg-amber-500/20 px-1.5 text-amber-400 disabled:opacity-40"><Plus size={12} /></button>
                  </div>
                </div>
              </div>
            </ThemedSection>

            {/* =================== STATE — violet =================== */}
            <ThemedSection icon={ToggleLeft} title="State" count={stateVars.length} theme={SECTION_THEME.state} compact>
              <div className="max-h-[220px] overflow-y-auto space-y-1.5">
                <div className="flex gap-1">
                  <input
                    className="flex-1 min-w-0 rounded border border-border bg-base px-2 py-1 text-[11px] text-primary placeholder:text-muted outline-none focus:border-violet-500/50"
                    placeholder={t('pddl.stateName')}
                    value={newStateName}
                    onChange={e => setNewStateName(e.target.value)}
                    onKeyDown={e => e.key === 'Enter' && handleAddState()}
                  />
                  <select
                    className="rounded border border-border bg-base px-1 py-1 text-[10px] text-primary outline-none"
                    value={newStateType}
                    onChange={e => setNewStateType(e.target.value as 'bool' | 'int' | 'string')}
                  >
                    <option value="bool">bool</option>
                    <option value="int">int</option>
                    <option value="string">str</option>
                  </select>
                  <button onClick={handleAddState} disabled={!newStateName.trim()} className="rounded bg-violet-500/20 px-1.5 text-violet-400 disabled:opacity-40"><Plus size={12} /></button>
                </div>
                {stateVars.length === 0 ? (
                  <p className="py-3 text-center text-[10px] text-muted">{t('pddl.noPlanningStates')}</p>
                ) : stateVars.map(sv => {
                  const badge = TYPE_BADGE[sv.type || 'string'] || TYPE_BADGE.string
                  const isEditing = editingStateId === sv.id
                  const currentValue = runtimePlanningState[sv.name] ?? sv.initial_value ?? '-'
                  return (
                    <div key={sv.id} className="group flex items-center gap-2 rounded border border-border bg-base/50 px-2 py-1.5">
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-1.5">
                          <span className="truncate font-mono text-[11px] font-medium text-violet-400">{sv.name}</span>
                          <span className={`shrink-0 rounded px-1 py-px text-[9px] font-medium ${badge.bg} ${badge.text}`}>{sv.type}</span>
                        </div>
                        <div className="mt-0.5 text-[9px] text-secondary">
                          {t('pddl.defaultValue')}: {sv.initial_value || '-'}
                        </div>
                      </div>
                      <span className="shrink-0 rounded bg-violet-500/10 px-1.5 py-1 font-mono text-[10px] text-violet-300">
                        {currentValue}
                      </span>
                      {isEditing ? (
                        <div className="flex items-center gap-0.5 shrink-0">
                          <input
                            className="w-14 rounded border border-border bg-surface px-1 py-0.5 text-[10px] text-primary outline-none"
                            value={editStateInitialValue}
                            onChange={e => setEditStateInitialValue(e.target.value)}
                            onKeyDown={e => e.key === 'Enter' && handleUpdateStateInitialValue(sv)}
                            autoFocus
                          />
                          <button onClick={() => handleUpdateStateInitialValue(sv)} className="text-green-400"><Check size={10} /></button>
                          <button onClick={() => setEditingStateId(null)} className="text-muted"><X size={10} /></button>
                        </div>
                      ) : (
                        <button
                          onClick={() => { setEditingStateId(sv.id); setEditStateInitialValue(sv.initial_value || '') }}
                          className="shrink-0 text-[10px] text-secondary hover:text-primary"
                        >{t('pddl.editInitialValue')}</button>
                      )}
                      <button onClick={() => handleDeleteState(sv.id)} className="shrink-0 text-muted opacity-0 group-hover:opacity-100 hover:text-red-400"><Trash2 size={11} /></button>
                    </div>
                  )
                })}
              </div>
            </ThemedSection>

            {/* =================== AGENT — emerald =================== */}
            <ThemedSection icon={Bot} title="Agent" count={`${selectedAgentIds.length}/${agents.length}`} theme={SECTION_THEME.agent} compact>
              <div className="max-h-[220px] overflow-y-auto space-y-1">
                {recommendedAgentIds.length > 0 && selectedAgentIds.length === 0 && (
                  <button onClick={() => setSelectedAgentIds(recommendedAgentIds)} className="w-full rounded border border-emerald-500/20 bg-emerald-500/10 py-1 text-[10px] font-medium text-emerald-400 hover:bg-emerald-500/20">
                    {t('pddl.selectRecommended')} ({recommendedAgentIds.length})
                  </button>
                )}
                {sortedAgents.length === 0 ? (
                  <p className="py-3 text-center text-[10px] text-muted">{t('pddl.noAgentsAvailable')}</p>
                ) : sortedAgents.map(({ agent, isOnline }) => {
                  const selected = selectedAgentIds.includes(agent.id)
                  const activeDispatch = (agentDispatchMap.get(agent.id) || [])[0]
                  const heldResources = heldResourcesByAgent.get(agent.id) || heldResourcesByAgent.get(agent.name) || []
                  const agentStateLabel = agent.current_state_code || agent.current_state || (isOnline ? t('pddl.agentReadyState') : t('pddl.agentOfflineState'))
                  return (
                    <button
                      key={agent.id}
                      onClick={() => toggleAgent(agent.id)}
                      className={`w-full rounded border px-2 py-1.5 text-left transition ${
                        selected ? 'border-emerald-500/40 bg-emerald-500/5' : 'border-border bg-base/50 hover:border-emerald-500/20'
                      }`}
                    >
                      <div className="flex items-center gap-1.5">
                        <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${isOnline ? 'bg-emerald-400' : 'bg-red-400'}`} />
                        <span className="flex-1 truncate text-[11px] font-medium text-primary">{agent.name}</span>
                        {selected && <Check size={10} className="shrink-0 text-emerald-400" />}
                      </div>
                      <div className="mt-1 flex flex-wrap items-center gap-1.5 text-[9px] text-secondary">
                        <span className="rounded bg-emerald-500/10 px-1.5 py-0.5 text-emerald-300">{agentStateLabel}</span>
                        {agent.current_graph_id && (
                          <span className="rounded bg-surface px-1.5 py-0.5 text-secondary">{agent.current_graph_id}</span>
                        )}
                        {activeDispatch && (
                          <span className="rounded bg-surface px-1.5 py-0.5 text-secondary">
                            {translateStatus(activeDispatch.status)} · {activeDispatch.step_name || activeDispatch.step_id}
                          </span>
                        )}
                      </div>
                      <div className="mt-1 flex flex-wrap gap-1">
                        {heldResources.length > 0 ? heldResources.map(allocation => (
                          <span key={`${agent.id}:${allocation.resource}`} className="rounded-full bg-amber-500/10 px-2 py-0.5 text-[9px] text-amber-400">
                            {allocation.resource}
                          </span>
                        )) : (
                          <span className="text-[9px] text-muted">{t('pddl.noHeldResources')}</span>
                        )}
                      </div>
                    </button>
                  )
                })}
              </div>
            </ThemedSection>

            {/* =================== GOAL — sky =================== */}
            <ThemedSection icon={Target} title="Goal" count={`${goalCount}/${stateVars.length || 0}`} theme={SECTION_THEME.goal} compact>
              <div className="max-h-[220px] overflow-y-auto">
                <GoalEditor label="" stateVars={stateVars} values={goalState} onChange={setGoalState} />
                {showInitialState && (
                  <div className="mt-2 border-t border-border pt-2">
                    <GoalEditor label="" stateVars={stateVars} values={initialState} onChange={setInitialState} />
                  </div>
                )}
                <button
                  onClick={() => setShowInitialState(!showInitialState)}
                  className="mt-2 flex w-full items-center gap-1 text-[10px] text-secondary hover:text-primary"
                >
                  {showInitialState ? <ChevronDown size={10} /> : <ChevronRight size={10} />}
                  {t('pddl.initialStateOverride')} {initialOverrideCount > 0 && <span className="text-sky-400">({initialOverrideCount})</span>}
                </button>
              </div>
            </ThemedSection>

            </div>{/* end grid top 4 */}

            {/* ========================================================== */}
            {/* PLAN & EXECUTION — rose (prominent)                        */}
            {/* ========================================================== */}
            <ThemedSection
              icon={Layers}
              title={t('pddl.operationsBoardTitle')}
              count={plan ? (plan.is_valid ? `${plan.total_steps} ${t('pddl.steps')}` : t('status.failed')) : undefined}
              theme={SECTION_THEME.plan}
            >
              {pendingRequirements.length > 0 && (
                <div className="mb-4 flex items-center gap-2 rounded-xl border border-rose-500/20 bg-rose-500/5 px-4 py-3 text-xs text-rose-400">
                  <AlertTriangle size={14} />
                  {pendingRequirements[0]}
                </div>
              )}
              <PlanVisualization
                plan={plan}
                isLoading={isSolving}
                steps={allSteps.length > 0 ? allSteps : undefined}
                execution={execution}
                resources={selectedDistributor?.resources || []}
              />
            </ThemedSection>

          </div>
        )}
      </main>
    </div>
  )
}
