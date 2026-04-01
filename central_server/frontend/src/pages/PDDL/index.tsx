import { useState, useEffect, useCallback, useMemo, useRef } from 'react'
import {
  RefreshCw, Play, Eye, Square, ChevronDown, ChevronRight, Info, Target,
  Database, Workflow, AlertTriangle, Link2, Plus, Trash2, Check, X, Layers,
  Circle, Edit, Gem, ToggleLeft, Bot, Download, Upload, ArrowRight, Activity, CheckCircle2, Clock3, RotateCcw,
  SlidersHorizontal,
} from 'lucide-react'
import { useTranslation } from '../../i18n'
import { behaviorTreeApi, agentApi, capabilityApi, pddlApi, taskDistributorApi, fleetApi, saveFilesApi } from '../../api/client'
import type {
  BehaviorTree, Agent, PlanResult, PlanExecution, ResourceAllocation,
  TaskDistributor, GraphListItem, TaskDistributorState, TaskDistributorResource, RobotStateSnapshot, PlanningTaskSpec,
  RealtimeGoalRule, RealtimeSession, TaskDistributorStateMergePolicy,
} from '../../types'
import GoalEditor from './components/GoalEditor'
import PlanVisualization from './components/PlanVisualization'
import RealtimeGoalEditor from './components/RealtimeGoalEditor'
import { ActionGraphViewer } from '../../components/BehaviorTreeViewer'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface AgentWithCaps {
  agent: Agent
  capabilities: string[]
  isOnline: boolean
}

interface DistributorProfileState {
  name: string
  type?: string
  initial_value?: string
  description?: string
}

interface DistributorProfileResource {
  id?: string
  name: string
  kind?: 'type' | 'instance' | string
  parent_name?: string
  description?: string
}

interface DistributorProfileTaskRef {
  id?: string
  name?: string
}

interface DistributorProfileAgentRef {
  id?: string
  name?: string
}

interface TaskDistributorProfile {
  version: number
  exported_at?: string
  distributor: {
    id?: string
    name: string
    description?: string
  }
  states: DistributorProfileState[]
  resources: DistributorProfileResource[]
  state_merge_policies?: TaskDistributorStateMergePolicy[]
  selected_tasks?: DistributorProfileTaskRef[]
  selected_agents?: DistributorProfileAgentRef[]
  initial_state?: Record<string, string>
  goal_state?: Record<string, string>
  realtime?: {
    tick_interval_sec?: number
    goals?: RealtimeGoalRule[]
  }
}

type StateMergePriority = 'live' | 'planner'

interface StateMergePolicyPreset {
  pattern: string
  defaultPriority: StateMergePriority
  titleKey: string
  descriptionKey: string
}

const STATE_MERGE_POLICY_PRESETS: StateMergePolicyPreset[] = [
  {
    pattern: '{{agent.name}}_status',
    defaultPriority: 'live',
    titleKey: 'pddl.mergePolicyAgentStatusTitle',
    descriptionKey: 'pddl.mergePolicyAgentStatusDesc',
  },
  {
    pattern: '{{agent.name}}_mode',
    defaultPriority: 'live',
    titleKey: 'pddl.mergePolicyAgentModeTitle',
    descriptionKey: 'pddl.mergePolicyAgentModeDesc',
  },
  {
    pattern: '{{agent.name}}_location',
    defaultPriority: 'live',
    titleKey: 'pddl.mergePolicyAgentLocationTitle',
    descriptionKey: 'pddl.mergePolicyAgentLocationDesc',
  },
  {
    pattern: '{{agent.name}}_battery_*',
    defaultPriority: 'live',
    titleKey: 'pddl.mergePolicyAgentBatteryTitle',
    descriptionKey: 'pddl.mergePolicyAgentBatteryDesc',
  },
  {
    pattern: '{{agent.name}}_*msg*',
    defaultPriority: 'planner',
    titleKey: 'pddl.mergePolicyAgentMsgTitle',
    descriptionKey: 'pddl.mergePolicyAgentMsgDesc',
  },
  {
    pattern: '{{resource.name}}_status',
    defaultPriority: 'live',
    titleKey: 'pddl.mergePolicyResourceStatusTitle',
    descriptionKey: 'pddl.mergePolicyResourceStatusDesc',
  },
  {
    pattern: '{{resource.name}}_location',
    defaultPriority: 'live',
    titleKey: 'pddl.mergePolicyResourceLocationTitle',
    descriptionKey: 'pddl.mergePolicyResourceLocationDesc',
  },
  {
    pattern: '{{resource.name}}_reserved_by',
    defaultPriority: 'planner',
    titleKey: 'pddl.mergePolicyResourceReservedTitle',
    descriptionKey: 'pddl.mergePolicyResourceReservedDesc',
  },
  {
    pattern: '{{resource.name}}_*msg*',
    defaultPriority: 'planner',
    titleKey: 'pddl.mergePolicyResourceMsgTitle',
    descriptionKey: 'pddl.mergePolicyResourceMsgDesc',
  },
]


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

const PDDL_DRAFT_STORAGE_KEY = 'mcs.pddl.draft.v2'
const PDDL_SCOPED_DRAFT_STORAGE_PREFIX = 'mcs.pddl.draft.v3:'
const PDDL_LAST_DISTRIBUTOR_STORAGE_KEY = 'mcs.pddl.lastDistributorId.v1'
const PDDL_NO_DISTRIBUTOR_SCOPE = '__none__'

interface PDDLDraftState {
  selectedBTIds: string[]
  selectedDistributorId: string | null
  selectedAgentIds: string[]
  goalState: Record<string, string>
  initialState: Record<string, string>
  showInitialState: boolean
  plan: PlanResult | null
  executionId: string | null
  realtimeGoals: RealtimeGoalRule[]
  realtimeTickIntervalSec: number
  realtimeSessionId: string | null
}

function getPddlDraftScopeKey(distributorId?: string | null) {
  return `${PDDL_SCOPED_DRAFT_STORAGE_PREFIX}${distributorId || PDDL_NO_DISTRIBUTOR_SCOPE}`
}

function normalizePddlDraft(raw?: Partial<PDDLDraftState> | null): PDDLDraftState {
  return {
    selectedBTIds: Array.isArray(raw?.selectedBTIds) ? raw!.selectedBTIds : [],
    selectedDistributorId: raw?.selectedDistributorId || null,
    selectedAgentIds: Array.isArray(raw?.selectedAgentIds) ? raw!.selectedAgentIds : [],
    goalState: raw?.goalState || {},
    initialState: raw?.initialState || {},
    showInitialState: Boolean(raw?.showInitialState),
    plan: raw?.plan || null,
    executionId: raw?.executionId || null,
    realtimeGoals: Array.isArray(raw?.realtimeGoals) ? raw!.realtimeGoals : [],
    realtimeTickIntervalSec: typeof raw?.realtimeTickIntervalSec === 'number' ? raw.realtimeTickIntervalSec : 2,
    realtimeSessionId: raw?.realtimeSessionId || null,
  }
}

function slugifyFileName(value: string) {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9가-힣]+/gi, '_')
    .replace(/^_+|_+$/g, '') || 'task_distributor_profile'
}

function toStringValue(value: unknown): string {
  if (value == null) return ''
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return JSON.stringify(value)
}

function toStringRecord(value?: Record<string, unknown> | null): Record<string, string> {
  if (!value) return {}
  return Object.entries(value).reduce<Record<string, string>>((acc, [key, raw]) => {
    acc[key] = toStringValue(raw)
    return acc
  }, {})
}

function normalizePlanningStateInitialValue(value?: string | null): string {
  const raw = value ?? ''
  const trimmed = raw.trim()
  if (!trimmed) return ''
  if ((trimmed.startsWith('"') && trimmed.endsWith('"')) || trimmed === 'null') {
    try {
      const parsed = JSON.parse(trimmed)
      return parsed == null ? '' : String(parsed)
    } catch {
      return raw
    }
  }
  return raw
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

function normalizeStateMergePolicies(
  policies?: TaskDistributorStateMergePolicy[] | null
): TaskDistributorStateMergePolicy[] {
  const byPattern = new Map<string, TaskDistributorStateMergePolicy>()
  STATE_MERGE_POLICY_PRESETS.forEach(preset => {
    byPattern.set(preset.pattern, {
      pattern: preset.pattern,
      priority: preset.defaultPriority,
    })
  })
  ;(policies || []).forEach(policy => {
    const pattern = policy.pattern?.trim()
    const priority = policy.priority === 'planner' ? 'planner' : 'live'
    if (!pattern) return
    byPattern.set(pattern, { pattern, priority })
  })

  const orderedPatterns = [
    ...STATE_MERGE_POLICY_PRESETS.map(preset => preset.pattern),
    ...Array.from(byPattern.keys()).filter(pattern => !STATE_MERGE_POLICY_PRESETS.some(preset => preset.pattern === pattern)),
  ]

  return orderedPatterns
    .map(pattern => byPattern.get(pattern))
    .filter((policy): policy is TaskDistributorStateMergePolicy => Boolean(policy))
}

function collectStateMergeAgentTokens(agents: AgentWithCaps[]) {
  return Array.from(new Set(
    agents.flatMap(item => {
      const id = item.agent.id?.trim() || ''
      const name = (item.agent.name || id).trim()
      return [name, id, name.replace(/-/g, '_'), id.replace(/-/g, '_')].filter(Boolean)
    })
  ))
}

function collectStateMergeResourceTokens(resources: TaskDistributorResource[]) {
  return Array.from(new Set(
    resources.flatMap(resource => {
      const name = resource.name?.trim() || ''
      const id = resource.id?.trim() || ''
      return [name, id, name.replace(/-/g, '_'), id.replace(/-/g, '_')].filter(Boolean)
    })
  ))
}

function buildStateMergePatternVariants(
  pattern: string,
  resources: TaskDistributorResource[],
  agents: AgentWithCaps[],
) {
  let variants = [pattern.trim()]
  if (variants[0] === '') return []

  if (pattern.includes('{{agent.name}}') || pattern.includes('{{agent.id}}') || pattern.includes('{{agent}}')) {
    const tokens = collectStateMergeAgentTokens(agents)
    if (tokens.length === 0) return []
    variants = tokens.flatMap(token =>
      variants.map(variant => variant
        .split('{{agent.name}}').join(token)
        .split('{{agent.id}}').join(token)
        .split('{{agent}}').join(token))
    )
  }

  if (pattern.includes('{{resource.name}}') || pattern.includes('{{resource.id}}') || pattern.includes('{{resource}}')) {
    const tokens = collectStateMergeResourceTokens(resources.filter(isResourceInstance))
    if (tokens.length === 0) return []
    variants = tokens.flatMap(token =>
      variants.map(variant => variant
        .split('{{resource.name}}').join(token)
        .split('{{resource.id}}').join(token)
        .split('{{resource}}').join(token))
    )
  }

  return Array.from(new Set(variants.map(value => value.trim()).filter(Boolean)))
}

function patternVariantToRegex(variant: string) {
  return new RegExp(`^${escapeRegExp(variant).replace(/\\\*/g, '.*')}$`)
}

function matchStateMergePolicyStates(
  pattern: string,
  states: TaskDistributorState[],
  resources: TaskDistributorResource[],
  agents: AgentWithCaps[],
) {
  const variants = buildStateMergePatternVariants(pattern, resources, agents)
  if (variants.length === 0) return []
  const matchers = variants.map(patternVariantToRegex)
  return states.filter(state => matchers.some(regex => regex.test(state.name)))
}

function expandStateNameByTemplate(
  template: string,
  resources: TaskDistributorResource[],
  agents: Array<{ id: string; name: string }>
): string[] {
  const trimmed = template.trim()
  if (!trimmed) return []

  const hasResourcePlaceholder =
    trimmed.includes('{{resource.name}}')
    || trimmed.includes('{{resource.id}}')
    || trimmed.includes('{{resource}}')
  const hasAgentPlaceholder =
    trimmed.includes('{{agent.name}}')
    || trimmed.includes('{{agent.id}}')
    || trimmed.includes('{{agent}}')

  if (!hasResourcePlaceholder && !hasAgentPlaceholder) {
    return [trimmed]
  }

  let variants = [trimmed]

  if (hasResourcePlaceholder) {
    const instances = resources.filter(isResourceInstance)
    if (instances.length === 0) return []

    variants = instances
      .flatMap(resource => variants.map(variant => variant
        .split('{{resource.name}}').join(resource.name)
        .split('{{resource.id}}').join(resource.id)
        .split('{{resource}}').join(resource.name)))
  }

  if (hasAgentPlaceholder) {
    const uniqueAgents = Array.from(new Map(
      agents
        .map(agent => ({
          id: (agent.id || '').trim(),
          name: (agent.name || '').trim() || (agent.id || '').trim(),
        }))
        .filter(agent => agent.id || agent.name)
        .map(agent => [`${agent.id}::${agent.name}`, agent])
    ).values())
    if (uniqueAgents.length === 0) return []

    variants = uniqueAgents
      .flatMap(agent => variants.map(variant => variant
        .split('{{agent.name}}').join(agent.name)
        .split('{{agent.id}}').join(agent.id)
        .split('{{agent}}').join(agent.name)))
  }

  return variants
    .map(name => name.trim())
    .filter(Boolean)
    .filter((name, index, array) => array.indexOf(name) === index)
}

function resolveRuntimeStateLabel(runtime?: RobotStateSnapshot | null, fallback?: Agent | null) {
  return runtime?.current_state || runtime?.state_code || fallback?.current_state_code || fallback?.current_state || 'idle'
}

function resolveRuntimeStepId(graph: BehaviorTree | null, runtime?: RobotStateSnapshot | null) {
  if (!graph || !runtime?.current_step_id) return null
  if (runtime.current_graph_id && runtime.current_graph_id !== graph.id) return null
  return runtime.current_step_id
}

const EMPTY_PLANNING_TASK: PlanningTaskSpec = {
  preconditions: [],
  required_resources: [],
  during_state: [],
  result_states: [],
}

function mergePlanningTasks(tasks: Array<PlanningTaskSpec | null | undefined>): PlanningTaskSpec {
  const preconditions = new Map<string, { operator?: '==' | '!='; value: string }>()
  const requiredResources = new Set<string>()
  const duringStates = new Map<string, string>()
  const resultStates = new Map<string, string>()

  for (const task of tasks) {
    for (const condition of task?.preconditions || []) {
      if (condition?.variable) preconditions.set(condition.variable, { operator: condition.operator, value: condition.value })
    }
    for (const resource of task?.required_resources || []) {
      if (resource) requiredResources.add(resource)
    }
    for (const effect of task?.during_state || []) {
      if (effect?.variable) duringStates.set(effect.variable, effect.value)
    }
    for (const effect of task?.result_states || []) {
      if (effect?.variable) resultStates.set(effect.variable, effect.value)
    }
  }

  return {
    preconditions: Array.from(preconditions.entries()).map(([variable, data]) => ({ variable, operator: data.operator, value: data.value })),
    required_resources: Array.from(requiredResources),
    during_state: Array.from(duringStates.entries()).map(([variable, value]) => ({ variable, value })),
    result_states: Array.from(resultStates.entries()).map(([variable, value]) => ({ variable, value })),
  }
}

function getApiErrorMessage(error: unknown): string {
  const err = error as {
    response?: { data?: { error?: string; message?: string } }
    message?: string
  }
  return err?.response?.data?.error
    || err?.response?.data?.message
    || err?.message
    || String(error)
}

function isMissingRealtimeSessionError(error: unknown): boolean {
  const err = error as {
    response?: { status?: number; data?: { error?: string; message?: string } }
    message?: string
  }
  const status = err?.response?.status
  const detail = (
    err?.response?.data?.error
    || err?.response?.data?.message
    || err?.message
    || ''
  ).toLowerCase()
  if (status === 404) return true
  if (status === 400 && detail.includes('not found')) return true
  if (detail.includes('realtime session') && detail.includes('not found')) return true
  return false
}

function createRealtimeGoalTemplate(index: number): RealtimeGoalRule {
  return {
    id: `realtime_goal_${index + 1}`,
    name: `Realtime goal ${index + 1}`,
    priority: index + 1,
    enabled: true,
    activation_conditions: [],
    goal_state: {},
  }
}

type SequenceTaskStatus = 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'

interface SequenceTaskBlock {
  agentId: string
  taskId: string
  taskName: string
  behaviorTreeId?: string
  order: number
  status: SequenceTaskStatus
  runtimeTaskId?: string
  currentStepId?: string | null
  currentStepName?: string | null
  completedStepIds?: string[]
  failedStepIds?: string[]
  completedStepCount?: number
  totalStepCount?: number
  error?: string | null
}

const sequenceStatusRank: Record<SequenceTaskStatus, number> = {
  running: 5,
  failed: 4,
  cancelled: 3,
  pending: 2,
  completed: 1,
}

function sortStateEntries(state: Record<string, string> | undefined): Array<[string, string]> {
  return Object.entries(state || {}).sort(([left], [right]) => left.localeCompare(right))
}

function stateValueClass(value: string) {
  const normalized = String(value || '').trim().toLowerCase()
  if (normalized === 'true' || normalized === 'running' || normalized === 'busy' || normalized === 'charging') {
    return 'bg-emerald-500/10 text-emerald-300 border-emerald-500/20'
  }
  if (normalized === 'false' || normalized === 'idle' || normalized === 'none') {
    return 'bg-slate-500/10 text-slate-300 border-slate-500/20'
  }
  if (normalized === 'done' || normalized === 'warning') {
    return 'bg-amber-500/10 text-amber-300 border-amber-500/20'
  }
  if (normalized === 'error' || normalized === 'fault' || normalized === 'failed') {
    return 'bg-red-500/10 text-red-300 border-red-500/20'
  }
  return 'bg-sky-500/10 text-sky-300 border-sky-500/20'
}

interface RealtimeMonitoringCard {
  id: string
  label: string
  kind: 'agent' | 'resource'
  statusKey?: string
  statusValue: string
  msgKey?: string
  msgValue: string
  resetValues: Record<string, string>
  resetKeys: string[]
}

function normalizeSequenceStatus(status?: string): SequenceTaskStatus {
  const normalized = (status || '').toLowerCase()
  if (normalized === 'running') return 'running'
  if (normalized === 'failed') return 'failed'
  if (normalized === 'cancelled') return 'cancelled'
  if (normalized === 'completed') return 'completed'
  return 'pending'
}

function mergeSequenceStatus(current: SequenceTaskStatus, next: SequenceTaskStatus): SequenceTaskStatus {
  return sequenceStatusRank[next] > sequenceStatusRank[current] ? next : current
}

function statusBadgeClass(status: SequenceTaskStatus) {
  switch (status) {
    case 'running':
      return 'bg-cyan-500/15 text-cyan-300 border-cyan-400/20'
    case 'completed':
      return 'bg-emerald-500/15 text-emerald-300 border-emerald-400/20'
    case 'failed':
      return 'bg-red-500/15 text-red-300 border-red-400/20'
    case 'cancelled':
      return 'bg-amber-500/15 text-amber-300 border-amber-400/20'
    default:
      return 'bg-surface text-secondary border-border'
  }
}

function appendUnique(values: string[] | undefined, value?: string | null) {
  const next = values ? [...values] : []
  const normalized = (value || '').trim()
  if (!normalized) return next
  if (!next.includes(normalized)) next.push(normalized)
  return next
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
  actions,
  children,
}: {
  icon: React.ElementType
  title: string
  count?: number | string
  theme: typeof SECTION_THEME[keyof typeof SECTION_THEME]
  defaultOpen?: boolean
  compact?: boolean
  actions?: React.ReactNode
  children: React.ReactNode
}) {
  const [open, setOpen] = useState(defaultOpen)
  return (
    <section className={`rounded-2xl border border-border bg-surface shadow-sm shadow-slate-950/5 border-l-[3px] ${theme.barLeft}`}>
      <div className={`flex items-center gap-2 ${compact ? 'px-3 py-2' : 'px-5 py-4 gap-3'}`}>
        <button
          onClick={() => setOpen(v => !v)}
          className="flex min-w-0 flex-1 items-center gap-2 text-left transition hover:text-primary"
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
        {actions && <div className="shrink-0">{actions}</div>}
      </div>
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
  const [showStateMergePolicyModal, setShowStateMergePolicyModal] = useState(false)
  const [stateMergePolicyDraft, setStateMergePolicyDraft] = useState<TaskDistributorStateMergePolicy[]>([])
  const [isSavingStateMergePolicies, setIsSavingStateMergePolicies] = useState(false)

  const [autoLinkNotice, setAutoLinkNotice] = useState<string | null>(null)
  const [assignmentNotice, setAssignmentNotice] = useState<string | null>(null)
  const [showInitialState, setShowInitialState] = useState(false)

  const [executionId, setExecutionId] = useState<string | null>(null)
  const [execution, setExecution] = useState<PlanExecution | null>(null)
  const [realtimeExecutionMap, setRealtimeExecutionMap] = useState<Record<string, PlanExecution>>({})
  const [resourceAllocations, setResourceAllocations] = useState<ResourceAllocation[]>([])
  const [agentRuntimeMap, setAgentRuntimeMap] = useState<Record<string, RobotStateSnapshot>>({})
  const [realtimeGoals, setRealtimeGoals] = useState<RealtimeGoalRule[]>([])
  const [realtimeTickIntervalSec, setRealtimeTickIntervalSec] = useState(2)
  const [realtimeSessionId, setRealtimeSessionId] = useState<string | null>(null)
  const [realtimeSession, setRealtimeSession] = useState<RealtimeSession | null>(null)
  const [isStartingRealtime, setIsStartingRealtime] = useState(false)
  const [isStoppingRealtime, setIsStoppingRealtime] = useState(false)
  const [isResettingMonitorKey, setIsResettingMonitorKey] = useState<string | null>(null)
  const [realtimeNotice, setRealtimeNotice] = useState<string | null>(null)
  const [showPlanningConfig, setShowPlanningConfig] = useState(true)
  const [selectedFlowTree, setSelectedFlowTree] = useState<BehaviorTree | null>(null)
  const [selectedFlowLabel, setSelectedFlowLabel] = useState<string>('Task flow')
  const [selectedFlowAgentId, setSelectedFlowAgentId] = useState<string | null>(null)
  const [selectedFlowTaskId, setSelectedFlowTaskId] = useState<string | null>(null)
  const [isLoadingFlowTree, setIsLoadingFlowTree] = useState(false)
  const autoSelectedTaskRef = useRef(false)

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
  const [stateBuilderMessage, setStateBuilderMessage] = useState<string | null>(null)
  const [editingStateId, setEditingStateId] = useState<string | null>(null)
  const [editStateInitialValue, setEditStateInitialValue] = useState('')
  const [profileNotice, setProfileNotice] = useState<string | null>(null)
  const [isApplyingProfile, setIsApplyingProfile] = useState(false)
  const didRestoreDraftRef = useRef(false)
  const skipNextSelectionResetRef = useRef(false)
  const restoredSelectionKeyRef = useRef<string | null>(null)
  const isApplyingScopedDraftRef = useRef(false)
  const [savedProfileFiles, setSavedProfileFiles] = useState<Array<{
    name: string
    size_bytes: number
    updated_at: string
  }>>([])
  const [showProfileLoadModal, setShowProfileLoadModal] = useState(false)

  // -----------------------------------------------------------------------
  // Derived values
  // -----------------------------------------------------------------------

  const selectedBTs = useMemo(
    () => selectedBTIds.map(id => btCache.get(id)).filter((bt): bt is BehaviorTree => bt != null),
    [selectedBTIds, btCache]
  )
  const selectedTaskIdSet = useMemo(() => new Set(treeList.map(item => item.id)), [treeList])
  const singleSelectedBT = selectedBTs.length === 1 ? selectedBTs[0] : null

  const selectedDistributor = distributors.find(d => d.id === selectedDistributorId) || null
  const resolvedSelectedAgentIds = useMemo(() => {
    const knownIds = new Set(agents.map(({ agent }) => agent.id))
    const seen = new Set<string>()
    const resolved: string[] = []
    for (const id of selectedAgentIds) {
      if (!knownIds.has(id) || seen.has(id)) continue
      seen.add(id)
      resolved.push(id)
    }
    return resolved
  }, [agents, selectedAgentIds])

  const selectedStateMergePolicies = useMemo(
    () => normalizeStateMergePolicies(selectedDistributor?.state_merge_policies),
    [selectedDistributor]
  )

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

  const stateMergePolicyRows = useMemo(() => {
    if (!selectedDistributor) return []
    const activePolicies = normalizeStateMergePolicies(showStateMergePolicyModal ? stateMergePolicyDraft : selectedStateMergePolicies)
    return STATE_MERGE_POLICY_PRESETS.map(preset => {
      const policy = activePolicies.find(item => item.pattern === preset.pattern) || {
        pattern: preset.pattern,
        priority: preset.defaultPriority,
      }
      const matchedStates = matchStateMergePolicyStates(
        preset.pattern,
        stateVars,
        selectedDistributor.resources || [],
        agents
      )
      return {
        ...preset,
        priority: (policy.priority === 'planner' ? 'planner' : 'live') as StateMergePriority,
        matchedStates,
      }
    })
  }, [agents, selectedDistributor, selectedStateMergePolicies, showStateMergePolicyModal, stateMergePolicyDraft, stateVars])

  const realtimeGoalResourceTypeOptions = useMemo(() => (
    resourceTypeGroups
      .filter(group => group.typeResource?.id)
      .map(group => ({
        id: group.typeResource!.id,
        name: group.typeName,
        instanceCount: group.items.length,
      }))
  ), [resourceTypeGroups])

  const generatedResourceNames = useMemo(
    () => buildInstanceNames(resourceTypeName, resourceTypeCount),
    [resourceTypeName, resourceTypeCount]
  )

  const buildCurrentDraft = useCallback((distributorIdOverride?: string | null): PDDLDraftState => ({
    selectedBTIds,
    selectedDistributorId: distributorIdOverride ?? selectedDistributorId,
    selectedAgentIds: resolvedSelectedAgentIds,
    goalState,
    initialState,
    showInitialState,
    plan,
    executionId,
    realtimeGoals,
    realtimeTickIntervalSec,
    realtimeSessionId,
  }), [
    selectedBTIds,
    selectedDistributorId,
    selectedAgentIds,
    goalState,
    initialState,
    showInitialState,
    plan,
    executionId,
    realtimeGoals,
    realtimeTickIntervalSec,
    realtimeSessionId,
  ])

  const readStoredDraft = useCallback((distributorId?: string | null): PDDLDraftState | null => {
    if (typeof window === 'undefined') return null
    try {
      const scopedRaw = window.localStorage.getItem(getPddlDraftScopeKey(distributorId))
      if (scopedRaw) {
        return normalizePddlDraft(JSON.parse(scopedRaw) as Partial<PDDLDraftState>)
      }
    } catch (err) {
      console.error('Failed to read scoped PDDL draft:', err)
    }
    return null
  }, [])

  const writeStoredDraft = useCallback((distributorId: string | null, draft: PDDLDraftState) => {
    if (typeof window === 'undefined') return
    try {
      window.localStorage.setItem(getPddlDraftScopeKey(distributorId), JSON.stringify({
        ...draft,
        selectedDistributorId: distributorId,
      }))
      if (distributorId) {
        window.localStorage.setItem(PDDL_LAST_DISTRIBUTOR_STORAGE_KEY, distributorId)
      }
    } catch (err) {
      console.error('Failed to persist scoped PDDL draft:', err)
    }
  }, [])

  const applyStoredDraft = useCallback((distributorId: string | null, rawDraft?: Partial<PDDLDraftState> | null) => {
    const draft = normalizePddlDraft(rawDraft)
    skipNextSelectionResetRef.current = true
    restoredSelectionKeyRef.current = `${[...draft.selectedBTIds].sort().join(',')}|${distributorId || ''}`
    isApplyingScopedDraftRef.current = true
    setSelectedDistributorId(distributorId)
    setSelectedBTIds(draft.selectedBTIds)
    setSelectedAgentIds(draft.selectedAgentIds)
    setGoalState(draft.goalState)
    setInitialState(draft.initialState)
    setShowInitialState(draft.showInitialState)
    setPlan(draft.plan)
    setExecutionId(draft.executionId)
    setExecution(null)
    setRealtimeExecutionMap({})
    setResourceAllocations([])
    setRealtimeGoals(draft.realtimeGoals)
    setRealtimeTickIntervalSec(draft.realtimeTickIntervalSec)
    setRealtimeSessionId(draft.realtimeSessionId)
    setRealtimeSession(null)
    setRealtimeNotice(null)
    setResourceBuilderMessage(null)
    setStateBuilderMessage(null)
    setTypeInstanceDrafts({})
  }, [])

  const switchDistributorScope = useCallback((nextDistributorId: string | null) => {
    writeStoredDraft(selectedDistributorId, buildCurrentDraft())
    const nextDraft = readStoredDraft(nextDistributorId)
    if (nextDraft) {
      applyStoredDraft(nextDistributorId, nextDraft)
      return
    }
    applyStoredDraft(nextDistributorId, {
      selectedDistributorId: nextDistributorId,
    })
  }, [selectedDistributorId, buildCurrentDraft, writeStoredDraft, readStoredDraft, applyStoredDraft])

  const selectedTaskPlanningByTaskId = useMemo(
    () => Object.fromEntries(selectedBTs.map(bt => [bt.id, bt.planning_task || EMPTY_PLANNING_TASK])),
    [selectedBTs]
  )
  const selectedTaskPlanning = useMemo(
    () => mergePlanningTasks(Object.values(selectedTaskPlanningByTaskId)),
    [selectedTaskPlanningByTaskId]
  )

  const requiredActionTypesByTaskId = useMemo(() => {
    const entries: Record<string, string[]> = {}
    for (const bt of selectedBTs) {
      if (bt.required_action_types?.length) {
        entries[bt.id] = bt.required_action_types
        continue
      }
      const types = new Set<string>()
      for (const step of bt.steps || []) {
        if (step.action?.type) types.add(step.action.type)
      }
      entries[bt.id] = Array.from(types)
    }
    return entries
  }, [selectedBTs])

  const requiredActionTypes = useMemo(
    () => Array.from(new Set(Object.values(requiredActionTypesByTaskId).flat())),
    [requiredActionTypesByTaskId]
  )

  useEffect(() => {
    if (treeList.length === 0) return
    setSelectedBTIds(prev => {
      const filtered = prev.filter(id => selectedTaskIdSet.has(id))
      return filtered.length === prev.length ? prev : filtered
    })
  }, [treeList, selectedTaskIdSet])

  useEffect(() => {
    if (autoSelectedTaskRef.current) return
    if (selectedBTIds.length > 0) {
      autoSelectedTaskRef.current = true
      return
    }
    if (treeList.length === 0) return
    autoSelectedTaskRef.current = true
    setSelectedBTIds([treeList[0].id])
  }, [selectedBTIds.length, treeList])


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

  useEffect(() => {
    if (didRestoreDraftRef.current) return
    didRestoreDraftRef.current = true

    try {
      const legacyRaw = window.localStorage.getItem(PDDL_DRAFT_STORAGE_KEY)
      let legacyDraft: PDDLDraftState | null = null
      if (legacyRaw) {
        legacyDraft = normalizePddlDraft(JSON.parse(legacyRaw) as Partial<PDDLDraftState>)
        const legacyScopeId = legacyDraft.selectedDistributorId || null
        const legacyScopedKey = getPddlDraftScopeKey(legacyScopeId)
        if (!window.localStorage.getItem(legacyScopedKey)) {
          window.localStorage.setItem(legacyScopedKey, JSON.stringify(legacyDraft))
        }
        if (legacyScopeId && !window.localStorage.getItem(PDDL_LAST_DISTRIBUTOR_STORAGE_KEY)) {
          window.localStorage.setItem(PDDL_LAST_DISTRIBUTOR_STORAGE_KEY, legacyScopeId)
        }
      }

      const lastDistributorId = window.localStorage.getItem(PDDL_LAST_DISTRIBUTOR_STORAGE_KEY) || legacyDraft?.selectedDistributorId || null
      const draft = readStoredDraft(lastDistributorId) || legacyDraft
      if (draft) {
        applyStoredDraft(draft.selectedDistributorId || lastDistributorId || null, draft)
      } else if (lastDistributorId) {
        setSelectedDistributorId(lastDistributorId)
      }
    } catch (err) {
      console.error('Failed to restore PDDL draft:', err)
    }
  }, [readStoredDraft, applyStoredDraft])

  useEffect(() => {
    if (selectedBTIds.length === 0) return

    const missingIds = selectedBTIds.filter(id => !btCache.has(id))
    if (missingIds.length === 0) return

    let cancelled = false

    const loadMissingBTs = async () => {
      try {
        const trees = await Promise.all(missingIds.map(id => behaviorTreeApi.get(id)))
        if (cancelled) return
        setBtCache(prev => {
          const next = new Map(prev)
          for (const tree of trees) {
            next.set(tree.id, tree)
          }
          return next
        })
      } catch (err) {
        console.error('Failed to restore selected BT details:', err)
      }
    }

    void loadMissingBTs()
    return () => {
      cancelled = true
    }
  }, [selectedBTIds, btCache])

  useEffect(() => {
    if (isApplyingScopedDraftRef.current) {
      isApplyingScopedDraftRef.current = false
      return
    }
    try {
      const draft = buildCurrentDraft()
      window.localStorage.setItem(PDDL_DRAFT_STORAGE_KEY, JSON.stringify(draft))
      writeStoredDraft(selectedDistributorId, draft)
      if (selectedDistributorId) {
        window.localStorage.setItem(PDDL_LAST_DISTRIBUTOR_STORAGE_KEY, selectedDistributorId)
      }
    } catch (err) {
      console.error('Failed to persist PDDL draft:', err)
    }
  }, [selectedBTIds, selectedDistributorId, selectedAgentIds, goalState, initialState, showInitialState, plan, executionId, realtimeGoals, realtimeTickIntervalSec, realtimeSessionId, buildCurrentDraft, writeStoredDraft])

  // -----------------------------------------------------------------------
  // Effects
  // -----------------------------------------------------------------------

  useEffect(() => {
    if (!executionId) return
    let cancelled = false

    const pollExecution = async (): Promise<string | null> => {
      try {
        const [exec, agentList] = await Promise.all([
          pddlApi.getExecution(executionId),
          agentApi.list(),
        ])
        if (cancelled) return null
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
        return exec.status || null
      } catch (err) {
        console.error('Failed to poll execution:', err)
        return null
      }
    }

    void pollExecution()
    const interval = setInterval(async () => {
      const status = await pollExecution()
      if (cancelled) return
      if (status === 'completed' || status === 'failed' || status === 'cancelled') {
        clearInterval(interval)
      }
    }, 1000)
    return () => {
      cancelled = true
      clearInterval(interval)
    }
  }, [executionId])

  useEffect(() => {
    if (!realtimeSessionId) {
      setRealtimeSession(null)
      setRealtimeExecutionMap({})
      return
    }

    let cancelled = false

    const pollRealtime = async () => {
      try {
        const session = await pddlApi.getRealtimeSession(realtimeSessionId)
        if (cancelled) return
        setRealtimeSession(session)
      } catch (err) {
        if (cancelled) return
        console.error('Failed to poll realtime session:', err)
        if (isMissingRealtimeSessionError(err)) {
          setRealtimeSession(null)
          setRealtimeSessionId(null)
          setRealtimeExecutionMap({})
          setRealtimeNotice('Realtime 세션이 서버에 없어 로컬 상태를 자동 정리했습니다.')
        }
      }
    }

    void pollRealtime()
    const interval = setInterval(pollRealtime, 1000)
    return () => {
      cancelled = true
      clearInterval(interval)
    }
  }, [realtimeSessionId])

  const realtimeActiveExecutionIds = useMemo(() => {
    const ids: string[] = []
    for (const id of realtimeSession?.active_execution_ids || []) {
      if (id) ids.push(id)
    }
    if (realtimeSession?.active_execution_id) {
      ids.push(realtimeSession.active_execution_id)
    }
    return Array.from(new Set(ids))
  }, [realtimeSession?.active_execution_id, realtimeSession?.active_execution_ids])

  useEffect(() => {
    if (realtimeActiveExecutionIds.length === 0) {
      setRealtimeExecutionMap({})
      setResourceAllocations([])
      return
    }

    let cancelled = false

    const refreshRealtimeExecutions = async () => {
      const fetched = await Promise.all(
        realtimeActiveExecutionIds.map(async (executionID) => {
          try {
            const exec = await pddlApi.getExecution(executionID)
            return { executionID, exec }
          } catch (err) {
            console.error('Failed to refresh realtime execution:', executionID, err)
            return null
          }
        })
      )
      if (cancelled) return

      const nextMap: Record<string, PlanExecution> = {}
      const mergedResources: ResourceAllocation[] = []
      const seenResourceKeys = new Set<string>()
      for (const item of fetched) {
        if (!item) continue
        const executionStatus = (item.exec.status || '').toLowerCase()
        if (executionStatus === 'completed' || executionStatus === 'failed' || executionStatus === 'cancelled') {
          continue
        }
        nextMap[item.executionID] = item.exec
        for (const allocation of item.exec.resources || []) {
          const resourceKey = allocation.resource || `${allocation.holder_agent_id}:${allocation.task_id}:${allocation.step_id}`
          if (seenResourceKeys.has(resourceKey)) continue
          seenResourceKeys.add(resourceKey)
          mergedResources.push(allocation)
        }
      }

      setRealtimeExecutionMap(nextMap)
      setResourceAllocations(mergedResources)
    }

    void refreshRealtimeExecutions()
    const interval = setInterval(refreshRealtimeExecutions, 1000)
    return () => {
      cancelled = true
      clearInterval(interval)
    }
  }, [realtimeActiveExecutionIds])

  const realtimeExecutionSteps = useMemo(() => {
    const merged: PlanExecution['steps'] = []
    for (const executionID of realtimeActiveExecutionIds) {
      const exec = realtimeExecutionMap[executionID]
      if (!exec) continue
      for (const step of exec.steps || []) {
        merged.push(step)
      }
    }
    return merged
  }, [realtimeExecutionMap, realtimeActiveExecutionIds])

  const effectiveExecutionSteps = useMemo(
    () => (realtimeSessionId ? realtimeExecutionSteps : (execution?.steps || [])),
    [realtimeSessionId, realtimeExecutionSteps, execution?.steps]
  )

  const runtimeAgentIds = useMemo(() => {
    const ids = new Set<string>()
    for (const id of selectedAgentIds) ids.add(id)
    for (const assignment of plan?.assignments || []) {
      if (assignment.agent_id) ids.add(assignment.agent_id)
    }
    for (const step of effectiveExecutionSteps) {
      if (step.agent_id) ids.add(step.agent_id)
    }
    return Array.from(ids)
  }, [selectedAgentIds, plan?.assignments, effectiveExecutionSteps])

  useEffect(() => {
    if (runtimeAgentIds.length === 0) {
      setAgentRuntimeMap({})
      return
    }

    let cancelled = false

    const refreshRuntime = async () => {
      try {
        const snapshot = await fleetApi.getState({ agentIds: runtimeAgentIds })
        if (cancelled) return

        const nextMap: Record<string, RobotStateSnapshot> = {}
        for (const runtime of Object.values(snapshot.robots || {})) {
          const agentId = runtime.agent_id || runtime.id
          if (!agentId) continue
          const existing = nextMap[agentId]
          if (!existing) {
            nextMap[agentId] = runtime
            continue
          }
          const prefersCurrent = Boolean(runtime.is_executing) && !existing.is_executing
          const fresher = (runtime.staleness_sec ?? Number.POSITIVE_INFINITY) < (existing.staleness_sec ?? Number.POSITIVE_INFINITY)
          if (prefersCurrent || fresher) {
            nextMap[agentId] = runtime
          }
        }
        setAgentRuntimeMap(nextMap)
      } catch (err) {
        console.error('Failed to poll fleet runtime:', err)
      }
    }

    void refreshRuntime()
    const interval = setInterval(refreshRuntime, 1000)
    return () => {
      cancelled = true
      clearInterval(interval)
    }
  }, [runtimeAgentIds])

  const selectionKey = useMemo(
    () => `${[...selectedBTIds].sort().join(',')}|${selectedDistributorId || ''}`,
    [selectedBTIds, selectedDistributorId]
  )
  useEffect(() => {
    if (skipNextSelectionResetRef.current) {
      skipNextSelectionResetRef.current = false
      return
    }
    if (restoredSelectionKeyRef.current && restoredSelectionKeyRef.current === selectionKey) {
      restoredSelectionKeyRef.current = null
      return
    }
    setGoalState({})
    setInitialState({})
    setPlan(null)
    setExecutionId(null)
    setExecution(null)
    setRealtimeExecutionMap({})
    setResourceAllocations([])
    setResourceBuilderMessage(null)
    setStateBuilderMessage(null)
    setTypeInstanceDrafts({})
  }, [selectionKey])

  // -----------------------------------------------------------------------
  // BT / Agent handlers
  // -----------------------------------------------------------------------

  const handleToggleBT = useCallback(async (id: string) => {
    if (selectedBTIds.includes(id)) {
      setSelectedBTIds(prev => prev.filter(item => item !== id))
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
    setSelectedBTIds(prev => [...prev, id])
    if (bt.task_distributor_id && !selectedDistributorId) {
      setSelectedDistributorId(bt.task_distributor_id)
      const td = distributors.find(d => d.id === bt!.task_distributor_id)
      if (td) {
        setAutoLinkNotice(td.name)
        setTimeout(() => setAutoLinkNotice(null), 4000)
      }
    } else if (selectedDistributorId && !bt.task_distributor_id) {
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
    setSelectedAgentIds(prev => {
      const knownIds = new Set(agents.map(({ agent }) => agent.id))
      const sanitized = Array.from(new Set(prev.filter(agentId => knownIds.has(agentId))))
      return sanitized.includes(id) ? sanitized.filter(a => a !== id) : [...sanitized, id]
    })
  }

  const handleSelectDistributor = useCallback(async (distributorId: string) => {
    if (distributorId !== selectedDistributorId) {
      switchDistributorScope(distributorId)
    }

    const targets = selectedBTs.filter(bt => bt.task_distributor_id !== distributorId)
    if (targets.length === 0) {
      return
    }

    try {
      const updatedTrees = await Promise.all(targets.map(bt =>
        behaviorTreeApi.update(bt.id, {
          task_distributor_id: distributorId,
        })
      ))
      setBtCache(prev => {
        const next = new Map(prev)
        for (const updated of updatedTrees) {
          next.set(updated.id, updated)
        }
        return next
      })
      setAssignmentNotice(
        t('pddl.distributorAssignedToTask', {
          distributor: distributors.find(d => d.id === distributorId)?.name || distributorId,
          task: updatedTrees.length === 1 ? updatedTrees[0].name : `${updatedTrees.length} tasks`,
        })
      )
      setTimeout(() => setAssignmentNotice(null), 4000)
    } catch (err) {
      console.error('Failed to assign task distributor:', err)
      setAssignmentNotice(t('pddl.distributorAssignError'))
      setTimeout(() => setAssignmentNotice(null), 4000)
    }
  }, [selectedBTs, distributors, t, selectedDistributorId, switchDistributorScope])

  // -----------------------------------------------------------------------
  // TD CRUD handlers
  // -----------------------------------------------------------------------

  const handleCreateTd = useCallback(async () => {
    if (!newTdName.trim()) return
    try {
      const created = await taskDistributorApi.create({ name: newTdName.trim() })
      setNewTdName('')
      await loadDistributors()
      switchDistributorScope(created.id)
    } catch (err) {
      console.error('Failed to create distributor:', err)
    }
  }, [newTdName, loadDistributors, switchDistributorScope])

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
      if (selectedDistributorId === id) {
        switchDistributorScope(null)
      }
      loadDistributors()
    } catch (err) {
      console.error('Failed to delete distributor:', err)
    }
  }, [selectedDistributorId, loadDistributors, switchDistributorScope])

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

    const template = newStateName.trim()
    const expanded = expandStateNameByTemplate(
      template,
      selectedDistributor.resources || [],
      agents.map(item => ({ id: item.agent.id, name: item.agent.name || item.agent.id }))
    )
    if (expanded.length === 0) {
      const needsResource =
        template.includes('{{resource.name}}')
        || template.includes('{{resource.id}}')
        || template.includes('{{resource}}')
      const needsAgent =
        template.includes('{{agent.name}}')
        || template.includes('{{agent.id}}')
        || template.includes('{{agent}}')
      if (needsResource && (selectedDistributor.resources || []).filter(isResourceInstance).length === 0) {
        setStateBuilderMessage('생성할 resource instance가 없습니다. Resource를 먼저 추가하세요.')
      } else if (needsAgent && agents.length === 0) {
        setStateBuilderMessage('생성할 agent가 없습니다. Agent 연결 상태를 먼저 확인하세요.')
      } else {
        setStateBuilderMessage('생성할 상태가 없습니다. placeholder 입력값을 확인하세요.')
      }
      return
    }

    const existing = new Set((selectedDistributor.states || []).map(state => state.name))
    const uniqueNames = Array.from(new Set(expanded))
    const targetNames = uniqueNames.filter(name => !existing.has(name))
    const skipped = uniqueNames.length - targetNames.length
    const normalizedInitialValue = (() => {
      if (newStateType === 'bool') {
        return newStateInitialValue === '' ? 'false' : newStateInitialValue
      }
      return newStateInitialValue || undefined
    })()

    if (targetNames.length === 0) {
      setStateBuilderMessage('모든 상태가 이미 존재해서 추가할 항목이 없습니다.')
      return
    }

    try {
      await Promise.all(targetNames.map((name) =>
        taskDistributorApi.createState(selectedDistributor.id, {
          name,
          type: newStateType,
          initial_value: normalizedInitialValue,
        })
      ))
      setNewStateName('')
      setNewStateInitialValue('')
      setStateBuilderMessage(`상태 ${targetNames.length}개 생성, 중복 ${skipped}개 건너뜀`)
      await loadDistributors()
    } catch (err) {
      console.error('Failed to add state:', err)
      setStateBuilderMessage('상태 생성 중 오류가 발생했습니다.')
    }
  }, [selectedDistributor, newStateName, newStateType, newStateInitialValue, loadDistributors, agents])

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

  const handleOpenStateMergePolicyModal = useCallback(() => {
    setStateMergePolicyDraft(selectedStateMergePolicies)
    setShowStateMergePolicyModal(true)
  }, [selectedStateMergePolicies])

  const handleSetStateMergePriority = useCallback((pattern: string, priority: StateMergePriority) => {
    setStateMergePolicyDraft(prev => prev.map(policy =>
      policy.pattern === pattern ? { ...policy, priority } : policy
    ))
  }, [])

  const handleSaveStateMergePolicies = useCallback(async () => {
    if (!selectedDistributor) return
    setIsSavingStateMergePolicies(true)
    try {
      const normalized = normalizeStateMergePolicies(stateMergePolicyDraft).map(policy => ({
        pattern: policy.pattern,
        priority: (policy.priority === 'planner' ? 'planner' : 'live') as StateMergePriority,
      }))
      await taskDistributorApi.updateStateMergePolicies(selectedDistributor.id, normalized)
      await loadDistributors()
      setShowStateMergePolicyModal(false)
      setStateBuilderMessage(t('pddl.mergePolicySaved'))
      window.setTimeout(() => setStateBuilderMessage(null), 3000)
    } catch (err) {
      console.error('Failed to save state merge policies:', err)
      setStateBuilderMessage(t('pddl.mergePolicySaveError'))
      window.setTimeout(() => setStateBuilderMessage(null), 4000)
    } finally {
      setIsSavingStateMergePolicies(false)
    }
  }, [loadDistributors, selectedDistributor, stateMergePolicyDraft, t])

  // -----------------------------------------------------------------------
  // Task Distributor profile import/export (JSON)
  // -----------------------------------------------------------------------

  const handleExportDistributorProfile = useCallback(async () => {
    if (!selectedDistributor) return

    const resourceById = new Map((selectedDistributor.resources || []).map(resource => [resource.id, resource]))

    const profile: TaskDistributorProfile = {
      version: 1,
      exported_at: new Date().toISOString(),
      distributor: {
        id: selectedDistributor.id,
        name: selectedDistributor.name,
        description: selectedDistributor.description,
      },
      states: (selectedDistributor.states || []).map(state => ({
        name: state.name,
        type: state.type,
        initial_value: state.initial_value,
        description: state.description,
      })),
      resources: (selectedDistributor.resources || []).map(resource => ({
        name: resource.name,
        kind: resource.kind || 'instance',
        parent_name: resource.parent_resource_id
          ? resourceById.get(resource.parent_resource_id)?.name
          : undefined,
        description: resource.description,
      })),
      state_merge_policies: selectedStateMergePolicies,
      selected_tasks: selectedBTs.map(bt => ({ id: bt.id, name: bt.name })),
      selected_agents: resolvedSelectedAgentIds
        .map(agentId => agents.find(item => item.agent.id === agentId)?.agent)
        .filter((agent): agent is Agent => Boolean(agent))
        .map(agent => ({ id: agent.id, name: agent.name })),
      initial_state: { ...initialState },
      goal_state: { ...goalState },
      realtime: {
        tick_interval_sec: realtimeTickIntervalSec,
        goals: realtimeGoals,
      },
    }

    const safeName = slugifyFileName(selectedDistributor.name || 'task_distributor_profile')
    const fileName = `${safeName}.json`
    try {
      const saved = await saveFilesApi.savePddlProfile(fileName, profile)
      const files = await saveFilesApi.listPddlProfiles()
      setSavedProfileFiles(files)
      setProfileNotice(`Exported profile: ${saved.name}`)
      setTimeout(() => setProfileNotice(null), 4000)
    } catch (err) {
      console.error('Failed to export distributor profile:', err)
      setProfileNotice(`Export failed: ${getApiErrorMessage(err)}`)
      setTimeout(() => setProfileNotice(null), 5000)
    }
  }, [
    agents,
    goalState,
    initialState,
    realtimeGoals,
    realtimeTickIntervalSec,
    selectedAgentIds,
    selectedBTs,
    selectedDistributor,
    selectedStateMergePolicies,
  ])

  const handleOpenProfileImport = useCallback(async () => {
    setIsApplyingProfile(true)
    setProfileNotice(null)
    try {
      const files = await saveFilesApi.listPddlProfiles()
      setSavedProfileFiles(files)
      if (files.length === 0) {
        setProfileNotice('Save_files/pddl 폴더에 저장된 프로필 JSON이 없습니다.')
        setTimeout(() => setProfileNotice(null), 5000)
        return
      }
      setShowProfileLoadModal(true)
    } catch (err) {
      console.error('Failed to list profile save files:', err)
      setProfileNotice(`Import failed: ${getApiErrorMessage(err)}`)
      setTimeout(() => setProfileNotice(null), 5000)
    } finally {
      setIsApplyingProfile(false)
    }
  }, [])

  const handleImportDistributorProfile = useCallback(async (fileName: string) => {
    setShowProfileLoadModal(false)
    setIsApplyingProfile(true)
    setProfileNotice(null)

    try {
      const parsed = await saveFilesApi.loadPddlProfile<TaskDistributorProfile>(fileName)
      const distributorName = parsed?.distributor?.name?.trim()

      if (!distributorName) {
        throw new Error('Invalid profile: distributor.name is required')
      }

      let distributor =
        (parsed.distributor.id ? distributors.find(item => item.id === parsed.distributor.id) : undefined) ||
        distributors.find(item => item.name === distributorName)

      if (!distributor) {
        const created = await taskDistributorApi.create({
          name: distributorName,
          description: parsed.distributor.description?.trim() || undefined,
        })
        distributor = await taskDistributorApi.getFull(created.id)
      } else {
        await taskDistributorApi.update(distributor.id, {
          name: distributorName,
          description: parsed.distributor.description?.trim() || undefined,
        })
        distributor = await taskDistributorApi.getFull(distributor.id)
      }

      const distributorId = distributor.id

      // Reset current distributor states/resources, then recreate from profile
      await Promise.all((distributor.states || []).map(state =>
        taskDistributorApi.deleteState(distributorId, state.id)
      ))
      await Promise.all((distributor.resources || []).map(resource =>
        taskDistributorApi.deleteResource(distributorId, resource.id)
      ))

      for (const state of parsed.states || []) {
        if (!state?.name?.trim()) continue
        await taskDistributorApi.createState(distributorId, {
          name: state.name.trim(),
          type: (state.type || 'string').trim(),
          initial_value: state.initial_value != null ? toStringValue(state.initial_value) : undefined,
          description: state.description?.trim() || undefined,
        })
      }

      const profileResources = (parsed.resources || [])
        .filter(resource => resource?.name?.trim())
        .map(resource => ({
          id: resource.id?.trim(),
          name: resource.name.trim(),
          kind: (resource.kind || 'instance').trim().toLowerCase(),
          parent_name: resource.parent_name?.trim(),
          description: resource.description?.trim(),
        }))

      const typeResourceByName = new Map<string, string>()

      const typeResources = profileResources.filter(resource => resource.kind === 'type')
      for (const typeResource of typeResources) {
        const created = await taskDistributorApi.createResource(distributorId, {
          name: typeResource.name,
          kind: 'type',
          description: typeResource.description || undefined,
        })
        typeResourceByName.set(typeResource.name, created.id)
      }

      const instanceResources = profileResources.filter(resource => resource.kind !== 'type')
      for (const instanceResource of instanceResources) {
        let parentResourceId: string | undefined

        if (instanceResource.parent_name) {
          parentResourceId = typeResourceByName.get(instanceResource.parent_name)
          if (!parentResourceId) {
            const createdType = await taskDistributorApi.createResource(distributorId, {
              name: instanceResource.parent_name,
              kind: 'type',
            })
            typeResourceByName.set(instanceResource.parent_name, createdType.id)
            parentResourceId = createdType.id
          }
        }

        await taskDistributorApi.createResource(distributorId, {
          name: instanceResource.name,
          kind: 'instance',
          parent_resource_id: parentResourceId,
          description: instanceResource.description || undefined,
        })
      }

      await taskDistributorApi.updateStateMergePolicies(
        distributorId,
        normalizeStateMergePolicies(parsed.state_merge_policies || [])
      )

      const distributorFull = await taskDistributorApi.getFull(distributorId)
      const resourceTypeIdByAlias = new Map<string, string>()
      for (const resource of distributorFull.resources || []) {
        if (resource.kind !== 'type') continue
        resourceTypeIdByAlias.set(resource.id, resource.id)
        resourceTypeIdByAlias.set(resource.name, resource.id)
      }
      for (const resource of typeResources) {
        const createdID = typeResourceByName.get(resource.name)
        if (!createdID) continue
        resourceTypeIdByAlias.set(resource.name, createdID)
        if (resource.id) {
          resourceTypeIdByAlias.set(resource.id, createdID)
        }
      }

      await loadDistributors()

      const selectedTaskIds = Array.from(new Set((parsed.selected_tasks || [])
        .map(taskRef => {
          if (taskRef.id && treeList.some(tree => tree.id === taskRef.id)) return taskRef.id
          if (taskRef.name) {
            const matched = treeList.find(tree => tree.name === taskRef.name)
            if (matched) return matched.id
          }
          return null
        })
        .filter((value): value is string => Boolean(value))))

      const selectedImportedAgentIds = Array.from(new Set((parsed.selected_agents || [])
        .map(agentRef => {
          if (agentRef.id && agents.some(item => item.agent.id === agentRef.id)) return agentRef.id
          if (agentRef.name) {
            const matched = agents.find(item => item.agent.name === agentRef.name)
            if (matched) return matched.agent.id
          }
          return null
        })
        .filter((value): value is string => Boolean(value))))

      if (selectedTaskIds.length > 0) {
        try {
          const newResources = distributorFull.resources || []
          const newResourceTokenSet = new Set(
            newResources.map(resource => `${resource.kind === 'type' ? 'type' : 'instance'}:${resource.id}`)
          )
          const oldDistributorCache = new Map<string, TaskDistributor>()

          const updatedTrees = await Promise.all(selectedTaskIds.map(async (taskId) => {
            const currentTree = await behaviorTreeApi.get(taskId)
            const originalDistributorId = currentTree.task_distributor_id || ''

            let oldResources: TaskDistributorResource[] = []
            if (originalDistributorId && originalDistributorId !== distributorId) {
              let cached = oldDistributorCache.get(originalDistributorId)
              if (!cached) {
                cached = await taskDistributorApi.getFull(originalDistributorId)
                oldDistributorCache.set(originalDistributorId, cached)
              }
              oldResources = cached.resources || []
            }

            const legacyTokenToName = new Map<string, string>()
            for (const resource of oldResources) {
              legacyTokenToName.set(
                `${resource.kind === 'type' ? 'type' : 'instance'}:${resource.id}`,
                resource.name
              )
            }

            const nextPlanningTask = currentTree.planning_task
              ? {
                  ...currentTree.planning_task,
                  required_resources: Array.from(new Set((currentTree.planning_task.required_resources || [])
                    .map((token) => {
                      const trimmed = `${token || ''}`.trim()
                      if (!trimmed) return ''
                      if (newResourceTokenSet.has(trimmed)) return trimmed

                      const [prefix, value = ''] = trimmed.split(':', 2)
                      const resourceKind = prefix === 'instance' ? 'instance' : 'type'
                      const legacyName = legacyTokenToName.get(trimmed)
                      const fallbackName = value && !value.includes('-') ? value : ''
                      const targetName = legacyName || fallbackName
                      if (!targetName) return ''

                      const matched = newResources.find(resource =>
                        resource.kind === resourceKind && resource.name === targetName
                      )
                      if (!matched) return ''
                      return `${resourceKind}:${matched.id}`
                    })
                    .filter(Boolean))),
                }
              : undefined

            return behaviorTreeApi.update(taskId, {
              task_distributor_id: distributorId,
              planning_task: nextPlanningTask,
            })
          }))
          setBtCache(prev => {
            const next = new Map(prev)
            for (const tree of updatedTrees) {
              next.set(tree.id, tree)
            }
            return next
          })
        } catch (err) {
          console.error('Failed to bind imported tasks to distributor:', err)
        }
      }

      skipNextSelectionResetRef.current = true
      restoredSelectionKeyRef.current = `${[...selectedTaskIds].sort().join(',')}|${distributorId}`
      setSelectedDistributorId(distributorId)
      setSelectedBTIds(selectedTaskIds)
      setSelectedAgentIds(selectedImportedAgentIds)
      setInitialState(toStringRecord(parsed.initial_state))
      setGoalState(toStringRecord(parsed.goal_state))
      setShowInitialState(Object.keys(parsed.initial_state || {}).length > 0)
      setPlan(null)
      setExecutionId(null)
      setExecution(null)
      setRealtimeExecutionMap({})
      setResourceAllocations([])

      const unresolvedResourceScopes = new Set<string>()
      const importedGoals = (parsed.realtime?.goals || []).map((goal, index) => {
        const rawScopeValues = [
          ...(Array.isArray(goal.resource_type_ids) ? goal.resource_type_ids : []),
          ...(goal.resource_type_id ? [goal.resource_type_id] : []),
        ]
          .map(value => `${value || ''}`.trim())
          .filter(Boolean)

        const resolvedScopeValues = Array.from(new Set(rawScopeValues
          .map(value => {
            const resolved = resourceTypeIdByAlias.get(value)
            if (!resolved) unresolvedResourceScopes.add(value)
            return resolved || ''
          })
          .filter(Boolean)))

        return {
          ...goal,
          id: goal.id || `goal_${Date.now()}_${index}`,
          name: goal.name || `Goal ${index + 1}`,
          priority: Number.isFinite(goal.priority) ? goal.priority : (index + 1) * 10,
          enabled: goal.enabled !== false,
          resource_type_id: resolvedScopeValues[0] || '',
          resource_type_ids: resolvedScopeValues,
          goal_state: toStringRecord(goal.goal_state),
        }
      })
      setRealtimeGoals(importedGoals)
      if (typeof parsed.realtime?.tick_interval_sec === 'number' && parsed.realtime.tick_interval_sec > 0) {
        setRealtimeTickIntervalSec(parsed.realtime.tick_interval_sec)
      }
      setRealtimeSessionId(null)
      setRealtimeSession(null)
      setRealtimeExecutionMap({})

      const unresolvedSuffix = unresolvedResourceScopes.size > 0
        ? ` (resource scope unresolved: ${Array.from(unresolvedResourceScopes).join(', ')})`
        : ''
      setProfileNotice(`Imported profile: ${fileName}${unresolvedSuffix}`)
      setTimeout(() => setProfileNotice(null), 5000)
    } catch (err) {
      console.error('Failed to import distributor profile:', err)
      const message = err instanceof Error ? err.message : 'Unknown import error'
      setProfileNotice(`Import failed: ${message}`)
      setTimeout(() => setProfileNotice(null), 6000)
    } finally {
      setIsApplyingProfile(false)
    }
  }, [agents, distributors, loadDistributors, treeList])

  // -----------------------------------------------------------------------
  // Plan handlers
  // -----------------------------------------------------------------------

  const selectedBehaviorTreeIds = useMemo(() => {
    const fromLoaded = selectedBTs.map(bt => bt.id)
    if (fromLoaded.length > 0) return fromLoaded
    return selectedBTIds.filter(id => selectedTaskIdSet.has(id))
  }, [selectedBTIds, selectedBTs, selectedTaskIdSet])
  const selectedBehaviorTreeLabel = selectedBTs.length === 1
    ? selectedBTs[0].name
    : `${selectedBehaviorTreeIds.length} tasks`

  const handlePreview = async () => {
    if (selectedBehaviorTreeIds.length === 0 || !selectedDistributor || resolvedSelectedAgentIds.length === 0 || Object.keys(goalState).length === 0) return
    if (resolvedSelectedAgentIds.length !== selectedAgentIds.length) {
      setSelectedAgentIds(resolvedSelectedAgentIds)
    }
    setIsSolving(true); setPlan(null); setExecutionId(null); setExecution(null); setRealtimeExecutionMap({}); setResourceAllocations([])
    try {
      const result = await pddlApi.preview({
        behavior_tree_id: selectedBehaviorTreeIds[0],
        behavior_tree_ids: selectedBehaviorTreeIds,
        task_distributor_id: selectedDistributor.id,
        initial_state: Object.keys(initialState).length > 0 ? initialState : undefined,
        goal_state: goalState,
        agent_ids: resolvedSelectedAgentIds,
      })
      setPlan(result)
    } catch (err) {
      console.error('Preview failed:', err)
      setPlan({ assignments: [], is_valid: false, error_message: getApiErrorMessage(err), total_steps: 0, parallel_groups: 0 })
    } finally { setIsSolving(false) }
  }

  const handleSaveAndSolve = async () => {
    if (selectedBehaviorTreeIds.length === 0 || !selectedDistributor || resolvedSelectedAgentIds.length === 0 || Object.keys(goalState).length === 0) return
    if (resolvedSelectedAgentIds.length !== selectedAgentIds.length) {
      setSelectedAgentIds(resolvedSelectedAgentIds)
    }
    setIsSolving(true); setPlan(null); setExecutionId(null); setExecution(null); setRealtimeExecutionMap({}); setResourceAllocations([])
    try {
      const problem = await pddlApi.createProblem({
        name: `${selectedBehaviorTreeLabel} - ${new Date().toLocaleString()}`,
        behavior_tree_id: selectedBehaviorTreeIds[0],
        behavior_tree_ids: selectedBehaviorTreeIds,
        task_distributor_id: selectedDistributor.id,
        initial_state: Object.keys(initialState).length > 0 ? initialState : undefined,
        goal_state: goalState,
        agent_ids: resolvedSelectedAgentIds,
      })
      const solved = await pddlApi.solveProblem(problem.id)
      if (solved.plan_result) setPlan(solved.plan_result)
    } catch (err) {
      console.error('Solve failed:', err)
      setPlan({ assignments: [], is_valid: false, error_message: getApiErrorMessage(err), total_steps: 0, parallel_groups: 0 })
    } finally { setIsSolving(false) }
  }

  const handleExecute = async () => {
    if (!plan?.is_valid || selectedBehaviorTreeIds.length === 0 || !selectedDistributor) return
    if (resolvedSelectedAgentIds.length !== selectedAgentIds.length) {
      setSelectedAgentIds(resolvedSelectedAgentIds)
    }
    try {
      const problem = await pddlApi.createProblem({
        name: `${selectedBehaviorTreeLabel} - Exec ${new Date().toLocaleString()}`,
        behavior_tree_id: selectedBehaviorTreeIds[0],
        behavior_tree_ids: selectedBehaviorTreeIds,
        task_distributor_id: selectedDistributor.id,
        initial_state: Object.keys(initialState).length > 0 ? initialState : undefined,
        goal_state: goalState,
        agent_ids: resolvedSelectedAgentIds,
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

  const handleImportCurrentGoalToRealtime = useCallback(() => {
    const nextIndex = realtimeGoals.length
    setRealtimeGoals(prev => [
      ...prev,
      {
        ...createRealtimeGoalTemplate(nextIndex),
        goal_state: { ...goalState },
      },
    ])
  }, [goalState, realtimeGoals.length])

  const handleStartRealtime = async () => {
    if (selectedBehaviorTreeIds.length === 0 || !selectedDistributor || resolvedSelectedAgentIds.length === 0) return
    const validGoals = realtimeGoals
      .filter(goal => goal.enabled && Object.keys(goal.goal_state || {}).length > 0)
      .map((goal, index) => ({
        ...goal,
        priority: goal.priority || index + 1,
      }))
    if (validGoals.length === 0) return

    if (resolvedSelectedAgentIds.length !== selectedAgentIds.length) {
      setSelectedAgentIds(resolvedSelectedAgentIds)
    }
    setIsStartingRealtime(true)
    setRealtimeNotice(null)
    setRealtimeExecutionMap({})
    try {
      const session = await pddlApi.startRealtimeSession({
        name: `${selectedBehaviorTreeLabel} realtime`,
        behavior_tree_id: selectedBehaviorTreeIds[0],
        behavior_tree_ids: selectedBehaviorTreeIds,
        task_distributor_id: selectedDistributor.id,
        initial_state: Object.keys(initialState).length > 0 ? initialState : undefined,
        agent_ids: resolvedSelectedAgentIds,
        tick_interval_sec: realtimeTickIntervalSec,
        goals: validGoals,
      })
      setRealtimeSessionId(session.id)
      setRealtimeSession(session)
      setRealtimeNotice('Realtime 세션을 시작했습니다.')
    } catch (err) {
      console.error('Failed to start realtime PDDL session:', err)
      setRealtimeNotice(`Realtime 세션 시작 실패: ${getApiErrorMessage(err)}`)
    } finally {
      setIsStartingRealtime(false)
    }
  }

  const handleStopRealtime = async () => {
    if (!realtimeSessionId) return
    setIsStoppingRealtime(true)
    setRealtimeNotice(null)
    try {
      const session = await pddlApi.stopRealtimeSession(realtimeSessionId)
      if ('status' in session) {
        setRealtimeSession(session)
        if (session.status === 'stopped') {
          setRealtimeSessionId(null)
          setRealtimeExecutionMap({})
        }
      } else {
        setRealtimeSession(null)
        setRealtimeSessionId(null)
        setRealtimeExecutionMap({})
      }
      setRealtimeNotice('Realtime 세션 중지 요청을 처리했습니다.')
    } catch (err) {
      console.error('Failed to stop realtime PDDL session:', err)
      if (isMissingRealtimeSessionError(err)) {
        setRealtimeSession(null)
        setRealtimeSessionId(null)
        setRealtimeExecutionMap({})
        setRealtimeNotice('Realtime 세션이 이미 종료되어 로컬 상태를 정리했습니다.')
      } else {
        setRealtimeNotice('Realtime 세션 중지 실패: 서버 상태를 확인하고 다시 시도하세요.')
      }
    } finally {
      setIsStoppingRealtime(false)
    }
  }

  const handleResetRealtimeMonitorCard = useCallback(async (card: RealtimeMonitoringCard) => {
    if (!realtimeSessionId || !realtimeSession) return
    if (Object.keys(card.resetValues).length === 0) return
    setIsResettingMonitorKey(card.id)
    setRealtimeNotice(null)
    try {
      const session = await pddlApi.resetRealtimeSessionState(realtimeSessionId, {
        values: card.resetValues,
        clear_live_keys: card.resetKeys,
      })
      setRealtimeSession(session)
      setRealtimeNotice(`${card.label} 상태/msg를 기본값으로 되돌렸습니다.`)
    } catch (err) {
      setRealtimeNotice(`${card.label} 상태 초기화 실패: ${getApiErrorMessage(err)}`)
    } finally {
      setIsResettingMonitorKey(null)
    }
  }, [realtimeSession, realtimeSessionId])

  // -----------------------------------------------------------------------
  // Computed helpers
  // -----------------------------------------------------------------------

  const isExecuting = execution?.status === 'running' || execution?.status === 'pending'
  const goalCount = Object.keys(goalState).length
  const initialOverrideCount = Object.keys(initialState).length
  const recommendedAgents = useMemo(
    () => agents.filter(a =>
      a.isOnline &&
      selectedBTs.length > 0 &&
      selectedBTs.some(bt => {
        const taskRequirements = requiredActionTypesByTaskId[bt.id] || []
        return taskRequirements.length === 0 || taskRequirements.every(type => a.capabilities.includes(type))
      })
    ),
    [agents, selectedBTs, requiredActionTypesByTaskId]
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
    for (const dispatch of effectiveExecutionSteps) {
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
  }, [effectiveExecutionSteps])
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
  const runtimeViewAgents = useMemo(() => {
    return runtimeAgentIds
      .map(agentId => {
        const agentEntry = agents.find(item => item.agent.id === agentId) || null
        const runtime = agentRuntimeMap[agentId] || null
        const currentStepId = resolveRuntimeStepId(singleSelectedBT, runtime)
        const currentStepName = currentStepId
          ? singleSelectedBT?.steps.find(step => step.id === currentStepId)?.job_name || singleSelectedBT?.steps.find(step => step.id === currentStepId)?.name || currentStepId
          : null
        const dispatches = agentDispatchMap.get(agentId) || []
        const heldResources = heldResourcesByAgent.get(agentId)
          || (agentEntry ? heldResourcesByAgent.get(agentEntry.agent.name) : undefined)
          || []
        return {
          agentId,
          agent: agentEntry?.agent || null,
          isOnline: agentEntry?.isOnline ?? Boolean(runtime?.is_online),
          runtime,
          currentStepId,
          currentStepName,
          dispatches,
          heldResources,
          stateLabel: resolveRuntimeStateLabel(runtime, agentEntry?.agent || null),
        }
      })
      .sort((left, right) => left.agentId.localeCompare(right.agentId))
  }, [runtimeAgentIds, agents, agentRuntimeMap, singleSelectedBT, agentDispatchMap, heldResourcesByAgent])
  const planStatus = execution?.status
    ? translateStatus(execution.status)
    : plan?.is_valid
      ? t('pddl.planReady')
      : plan
        ? t('status.failed')
        : t('pddl.planDraft')
  const selectedTaskCount = selectedBehaviorTreeIds.length
  const pendingRequirements = useMemo(() => {
    const r: string[] = []
    if (!selectedDistributor) r.push(t('pddl.needDistributor'))
    if (selectedTaskCount === 0) r.push(t('pddl.needBT'))
    if (resolvedSelectedAgentIds.length === 0) r.push(t('pddl.needAgents'))
    if (goalCount === 0) r.push(t('pddl.needGoal'))
    return r
  }, [selectedDistributor, selectedTaskCount, resolvedSelectedAgentIds.length, goalCount, t])
  const solveTooltip = useMemo(() => {
    if (!selectedDistributor) return t('pddl.needDistributor')
    if (selectedTaskCount === 0) return t('pddl.needBT')
    if (resolvedSelectedAgentIds.length === 0) return t('pddl.needAgents')
    if (Object.keys(goalState).length === 0) return t('pddl.needGoal')
    return undefined
  }, [selectedDistributor, selectedTaskCount, resolvedSelectedAgentIds, goalState, t])
  const executeTooltip = useMemo(() => {
    if (!selectedDistributor) return t('pddl.needDistributor')
    if (selectedTaskCount === 0) return t('pddl.needBT')
    if (isExecuting) return t('pddl.alreadyExecuting')
    if (!plan?.is_valid) return t('pddl.needValidPlan')
    return undefined
  }, [selectedDistributor, selectedTaskCount, plan, isExecuting, t])
  const canSolve = !!selectedDistributor && selectedTaskCount > 0 && resolvedSelectedAgentIds.length > 0 && Object.keys(goalState).length > 0
  const canExecute = !!selectedDistributor && selectedTaskCount > 0 && !!plan?.is_valid && !isExecuting
  const validRealtimeGoals = useMemo(
    () => realtimeGoals.filter(goal => goal.enabled && Object.keys(goal.goal_state || {}).length > 0),
    [realtimeGoals]
  )
  const hasRealtimeSession = Boolean(realtimeSessionId)
  const isRealtimeActive = hasRealtimeSession && (realtimeSession ? realtimeSession.status !== 'stopped' : false)
  const canStartRealtime = !!selectedDistributor && selectedTaskCount > 0 && resolvedSelectedAgentIds.length > 0 && validRealtimeGoals.length > 0 && !isStartingRealtime && !isStoppingRealtime && !isRealtimeActive && !isExecuting
  const realtimeStartDisabledReason = useMemo(() => {
    if (canStartRealtime) return null
    if (!selectedDistributor) return t('pddl.needDistributor')
    if (selectedTaskCount === 0) return t('pddl.needBT')
    if (resolvedSelectedAgentIds.length === 0) return t('pddl.needAgents')
    if (validRealtimeGoals.length === 0) return '유효한 Realtime Goal이 필요합니다 (enabled + goal_state 설정)'
    if (isExecuting) return t('pddl.alreadyExecuting')
    if (isStartingRealtime) return 'Realtime 세션 시작 중입니다...'
    if (isStoppingRealtime) return 'Realtime 세션 중지 처리 중입니다...'
    if (isRealtimeActive) return '이미 Realtime 세션이 실행 중입니다. 먼저 Stop realtime을 눌러주세요.'
    return 'Realtime 시작 조건을 확인해주세요.'
  }, [
    canStartRealtime,
    isExecuting,
    isRealtimeActive,
    isStartingRealtime,
    isStoppingRealtime,
    resolvedSelectedAgentIds.length,
    selectedTaskCount,
    selectedDistributor,
    t,
    validRealtimeGoals.length,
  ])

  const sortedAgents = useMemo(
    () => [...agents].sort((a, b) => {
      const aSelected = resolvedSelectedAgentIds.includes(a.agent.id) ? 1 : 0
      const bSelected = resolvedSelectedAgentIds.includes(b.agent.id) ? 1 : 0
      if (aSelected !== bSelected) return bSelected - aSelected
      const aRec = recommendedSet.has(a.agent.id) ? 1 : 0
      const bRec = recommendedSet.has(b.agent.id) ? 1 : 0
      if (aRec !== bRec) return bRec - aRec
      if (a.isOnline !== b.isOnline) return a.isOnline ? -1 : 1
      return a.agent.name.localeCompare(b.agent.name)
    }),
    [agents, resolvedSelectedAgentIds, recommendedSet]
  )

  const realtimeMonitoringCards = useMemo<RealtimeMonitoringCard[]>(() => {
    const effectiveState = realtimeSession?.effective_state || realtimeSession?.live_state || realtimeSession?.current_state || {}
    const defaultsByKey = new Map(stateVars.map(stateVar => [
      stateVar.name,
      normalizePlanningStateInitialValue(stateVar.initial_value),
    ]))

    const cards: RealtimeMonitoringCard[] = []
    const agentRows = sortedAgents.filter(({ agent }) => realtimeSession?.agent_ids?.includes(agent.id) || selectedAgentIds.includes(agent.id))
    for (const row of agentRows) {
      const statusKey = `${row.agent.name}_status`
      const msgKey = `${row.agent.name}_msg`
      const resetKeys = [statusKey, msgKey].filter(key => defaultsByKey.has(key))
      cards.push({
        id: `monitor-agent:${row.agent.id}`,
        label: row.agent.name,
        kind: 'agent',
        statusKey: defaultsByKey.has(statusKey) || statusKey in effectiveState ? statusKey : undefined,
        statusValue: effectiveState[statusKey] ?? normalizePlanningStateInitialValue(defaultsByKey.get(statusKey)),
        msgKey: defaultsByKey.has(msgKey) || msgKey in effectiveState ? msgKey : undefined,
        msgValue: effectiveState[msgKey] ?? normalizePlanningStateInitialValue(defaultsByKey.get(msgKey)),
        resetValues: resetKeys.reduce<Record<string, string>>((acc, key) => {
          acc[key] = normalizePlanningStateInitialValue(defaultsByKey.get(key))
          return acc
        }, {}),
        resetKeys,
      })
    }

    for (const resource of aggregatedResources) {
      const statusKey = `${resource.name}_status`
      const msgKey = `${resource.name}_msg`
      const resetKeys = [statusKey, msgKey].filter(key => defaultsByKey.has(key))
      cards.push({
        id: `monitor-resource:${resource.id}`,
        label: resource.name,
        kind: 'resource',
        statusKey: defaultsByKey.has(statusKey) || statusKey in effectiveState ? statusKey : undefined,
        statusValue: effectiveState[statusKey] ?? normalizePlanningStateInitialValue(defaultsByKey.get(statusKey)),
        msgKey: defaultsByKey.has(msgKey) || msgKey in effectiveState ? msgKey : undefined,
        msgValue: effectiveState[msgKey] ?? normalizePlanningStateInitialValue(defaultsByKey.get(msgKey)),
        resetValues: resetKeys.reduce<Record<string, string>>((acc, key) => {
          acc[key] = normalizePlanningStateInitialValue(defaultsByKey.get(key))
          return acc
        }, {}),
        resetKeys,
      })
    }

    return cards
  }, [aggregatedResources, realtimeSession, resolvedSelectedAgentIds, sortedAgents, stateVars])

  const plannedTaskBlocksByAgent = useMemo(() => {
    const grouped = new Map<string, SequenceTaskBlock[]>()
    const assignments = realtimeSession?.last_plan?.assignments || plan?.assignments || []

    for (const assignment of assignments) {
      const agentId = assignment.agent_id
      if (!agentId) continue
      const list = grouped.get(agentId) || []
      const taskId = assignment.task_id || assignment.behavior_tree_id || assignment.task_name || assignment.step_id
      const taskName = assignment.task_name || assignment.step_name || taskId
      if (!taskId || !taskName) continue

      const prev = list[list.length - 1]
      if (prev && prev.taskId === taskId) {
        prev.order = Math.min(prev.order, assignment.order)
        prev.status = mergeSequenceStatus(prev.status, 'pending')
      } else {
        list.push({
          agentId,
          taskId,
          taskName,
          behaviorTreeId: assignment.behavior_tree_id,
          order: assignment.order,
          status: 'pending',
        })
      }
      grouped.set(agentId, list)
    }

    for (const value of grouped.values()) {
      value.sort((left, right) => left.order - right.order)
    }

    return grouped
  }, [plan?.assignments, realtimeSession?.last_plan?.assignments])

  const executionTaskBlocksByAgent = useMemo(() => {
    const grouped = new Map<string, SequenceTaskBlock[]>()

    for (const step of effectiveExecutionSteps) {
      const agentId = step.agent_id
      if (!agentId) continue
      const list = grouped.get(agentId) || []
      const taskId = step.task_id || step.behavior_tree_id || step.task_name || step.step_id
      const taskName = step.task_name || step.step_name || taskId
      if (!taskId || !taskName) continue
      const status = normalizeSequenceStatus(step.status)

      const prev = list[list.length - 1]
      if (prev && prev.taskId === taskId) {
        prev.order = Math.min(prev.order, step.order)
        prev.status = mergeSequenceStatus(prev.status, status)
        prev.runtimeTaskId = prev.runtimeTaskId || step.runtime_task_id
        prev.behaviorTreeId = prev.behaviorTreeId || step.behavior_tree_id
        prev.totalStepCount = (prev.totalStepCount || 0) + 1
        if (status === 'completed') {
          prev.completedStepIds = appendUnique(prev.completedStepIds, step.step_id)
          prev.completedStepCount = (prev.completedStepCount || 0) + 1
        }
        if (status === 'failed' || status === 'cancelled') {
          prev.failedStepIds = appendUnique(prev.failedStepIds, step.step_id)
          prev.error = prev.error || step.error || null
        }
        if (status === 'running') {
          prev.currentStepId = step.step_id
          prev.currentStepName = step.step_name || step.step_id
        }
      } else {
        list.push({
          agentId,
          taskId,
          taskName,
          behaviorTreeId: step.behavior_tree_id,
          order: step.order,
          status,
          runtimeTaskId: step.runtime_task_id,
          currentStepId: status === 'running' ? step.step_id : null,
          currentStepName: status === 'running' ? (step.step_name || step.step_id) : null,
          completedStepIds: status === 'completed' ? [step.step_id] : [],
          failedStepIds: status === 'failed' || status === 'cancelled' ? [step.step_id] : [],
          completedStepCount: status === 'completed' ? 1 : 0,
          totalStepCount: 1,
          error: (status === 'failed' || status === 'cancelled') ? (step.error || null) : null,
        })
      }
      grouped.set(agentId, list)
    }

    for (const value of grouped.values()) {
      value.sort((left, right) => left.order - right.order)
    }

    return grouped
  }, [effectiveExecutionSteps])

  const realtimeTaskRows = useMemo(() => {
    return runtimeViewAgents.map(entry => {
      const executionTasks = executionTaskBlocksByAgent.get(entry.agentId) || []
      const plannedTasks = plannedTaskBlocksByAgent.get(entry.agentId) || []
      const mergedTimeline = new Map<string, SequenceTaskBlock>()

      for (const task of plannedTasks) {
        mergedTimeline.set(task.taskId, { ...task })
      }
      for (const task of executionTasks) {
        const existing = mergedTimeline.get(task.taskId)
        if (!existing) {
          mergedTimeline.set(task.taskId, { ...task })
          continue
        }
        mergedTimeline.set(task.taskId, {
          ...existing,
          ...task,
          order: Math.min(existing.order, task.order),
          status: mergeSequenceStatus(existing.status, task.status),
          completedStepIds: task.completedStepIds || existing.completedStepIds,
          failedStepIds: task.failedStepIds || existing.failedStepIds,
          currentStepId: task.currentStepId || existing.currentStepId || null,
          currentStepName: task.currentStepName || existing.currentStepName || null,
          completedStepCount: task.completedStepCount ?? existing.completedStepCount,
          totalStepCount: task.totalStepCount ?? existing.totalStepCount,
          runtimeTaskId: task.runtimeTaskId || existing.runtimeTaskId,
          error: task.error || existing.error || null,
        })
      }

      const timeline = Array.from(mergedTimeline.values()).sort((left, right) => left.order - right.order)

      let currentIndex = timeline.findIndex(task => task.status === 'running')
      if (currentIndex < 0) currentIndex = timeline.findIndex(task => task.status === 'pending')

      let previous: SequenceTaskBlock | null = null
      let current: SequenceTaskBlock | null = null
      let next: SequenceTaskBlock | null = null
      if (currentIndex >= 0) {
        previous = currentIndex > 0 ? timeline[currentIndex - 1] : null
        current = timeline[currentIndex]
        next = currentIndex + 1 < timeline.length ? timeline[currentIndex + 1] : null
      } else if (timeline.length > 0) {
        // all completed/terminal: completed를 '지금'으로 보이지 않도록 마지막 항목을 이전으로만 표기
        previous = timeline[timeline.length - 1]
      }

      return {
        ...entry,
        timeline,
        previous,
        current,
        next,
      }
    })
  }, [executionTaskBlocksByAgent, plannedTaskBlocksByAgent, runtimeViewAgents])

  const selectedFlowBlock = useMemo(() => {
    if (!selectedFlowAgentId || !selectedFlowTaskId) return null
    const row = realtimeTaskRows.find(item => item.agentId === selectedFlowAgentId)
    if (!row) return null
    return row.timeline.find(item => item.taskId === selectedFlowTaskId) || null
  }, [realtimeTaskRows, selectedFlowAgentId, selectedFlowTaskId])

  const selectedFlowRuntime = useMemo(() => {
    if (!selectedFlowAgentId) return null
    return agentRuntimeMap[selectedFlowAgentId] || null
  }, [agentRuntimeMap, selectedFlowAgentId])

  const selectedFlowLiveStepId = useMemo(() => {
    if (!selectedFlowTree || !selectedFlowRuntime) return null
    if (
      selectedFlowBlock?.runtimeTaskId
      && selectedFlowRuntime.current_task_id
      && selectedFlowRuntime.current_task_id !== selectedFlowBlock.runtimeTaskId
    ) {
      return null
    }
    return resolveRuntimeStepId(selectedFlowTree, selectedFlowRuntime)
  }, [selectedFlowBlock?.runtimeTaskId, selectedFlowRuntime, selectedFlowTree])

  const selectedFlowLiveStepName = useMemo(() => {
    if (!selectedFlowTree || !selectedFlowLiveStepId) return null
    const step = selectedFlowTree.steps.find(item => item.id === selectedFlowLiveStepId)
    return step?.job_name || step?.name || selectedFlowLiveStepId
  }, [selectedFlowLiveStepId, selectedFlowTree])

  const selectedFlowStepIds = useMemo(() => new Set(selectedFlowTree?.steps.map(step => step.id) || []), [selectedFlowTree])

  const selectedFlowCompletedSteps = useMemo(
    () => (selectedFlowBlock?.completedStepIds || []).filter(stepId => selectedFlowStepIds.has(stepId)),
    [selectedFlowBlock?.completedStepIds, selectedFlowStepIds]
  )

  const selectedFlowFailedSteps = useMemo(
    () => (selectedFlowBlock?.failedStepIds || []).filter(stepId => selectedFlowStepIds.has(stepId)),
    [selectedFlowBlock?.failedStepIds, selectedFlowStepIds]
  )

  const handleOpenTaskFlow = useCallback(async (task?: SequenceTaskBlock | null) => {
    if (!task) return
    setSelectedFlowAgentId(task.agentId)
    setSelectedFlowTaskId(task.taskId)

    let resolvedTree: BehaviorTree | null = null
    const candidateIds: string[] = []
    if (task.behaviorTreeId) candidateIds.push(task.behaviorTreeId)
    if (task.taskId) candidateIds.push(task.taskId)

    for (const id of candidateIds) {
      const cached = btCache.get(id)
      if (cached) {
        resolvedTree = cached
        break
      }
    }

    if (!resolvedTree) {
      const byName = treeList.find(item => item.name === task.taskName)
      if (byName) {
        const cached = btCache.get(byName.id)
        if (cached) {
          resolvedTree = cached
        } else {
          candidateIds.push(byName.id)
        }
      }
    }

    if (!resolvedTree && candidateIds.length > 0) {
      setIsLoadingFlowTree(true)
      try {
        for (const id of candidateIds) {
          if (!id) continue
          try {
            const fetched = await behaviorTreeApi.get(id)
            setBtCache(prev => new Map(prev).set(fetched.id, fetched))
            resolvedTree = fetched
            break
          } catch {
            // try next candidate id
          }
        }
      } finally {
        setIsLoadingFlowTree(false)
      }
    }

    if (!resolvedTree) return
    setSelectedFlowLabel(task.taskName)
    setSelectedFlowTree(resolvedTree)
  }, [btCache, treeList])

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
            <div className="mb-2 grid grid-cols-2 gap-1.5">
              <button
                onClick={handleExportDistributorProfile}
                disabled={!selectedDistributor || isApplyingProfile}
                className="inline-flex items-center justify-center gap-1 rounded-lg border border-border bg-base/70 px-2 py-1.5 text-[11px] text-secondary transition hover:border-accent/30 hover:bg-accent/10 hover:text-accent disabled:cursor-not-allowed disabled:opacity-40"
                title={selectedDistributor ? 'Export selected distributor profile as JSON' : 'Select a distributor first'}
              >
                <Download size={12} />
                Export JSON
              </button>
              <button
                onClick={handleOpenProfileImport}
                disabled={isApplyingProfile}
                className="inline-flex items-center justify-center gap-1 rounded-lg border border-border bg-base/70 px-2 py-1.5 text-[11px] text-secondary transition hover:border-accent/30 hover:bg-accent/10 hover:text-accent disabled:cursor-not-allowed disabled:opacity-40"
                title="Import distributor profile JSON"
              >
                <Upload size={12} />
                {isApplyingProfile ? 'Importing...' : 'Import JSON'}
              </button>
            </div>
            {profileNotice && (
              <div className="mb-2 rounded-lg border border-accent/20 bg-accent/10 px-2.5 py-2 text-[10px] text-accent">
                {profileNotice}
              </div>
            )}

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
                          <div className="mt-0.5 truncate font-mono text-[10px] text-muted">
                            ID: {d.id}
                          </div>
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
            count={selectedTaskCount > 0 ? String(selectedTaskCount) : undefined}
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
                        type="checkbox"
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
                          {item.required_action_types?.length || 0} {t('pddl.requiredActions')}
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
            {selectedTaskCount > 0 && (
              <div className="mt-2 rounded-xl border border-border bg-surface px-3 py-2 text-[11px] text-secondary">
                Capability {requiredActionTypes.length} · Need {(selectedTaskPlanning.preconditions || []).length} · Resource {(selectedTaskPlanning.required_resources || []).length} · Result {(selectedTaskPlanning.result_states || []).length}
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
                  <span>{selectedTaskCount > 0 ? selectedBehaviorTreeLabel : `0 ${t('pddl.tasksSelectedShort')}`}</span>
                  <span>·</span>
                  <span>{resolvedSelectedAgentIds.length} {t('pddl.agentsSelectedShort')}</span>
                  <span>·</span>
                  <span>{goalCount} {t('pddl.goalState')}</span>
                </div>
              </div>
            </div>
            <div className="flex items-center gap-2 shrink-0">
              <button
                onClick={() => setShowPlanningConfig(prev => !prev)}
                className="inline-flex items-center gap-1.5 rounded-xl border border-border bg-base/70 px-3 py-2 text-xs font-medium text-secondary transition hover:border-accent/20 hover:bg-accent/10 hover:text-accent"
                title="Resource/State/Agent/Goal 설정 창 열기"
              >
                <Database size={14} />
                {showPlanningConfig ? 'PDDL Config 숨기기' : 'PDDL Config 열기'}
              </button>
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

            {/* ---- Planning config workspace ---- */}
            <div className="rounded-2xl border border-border bg-surface shadow-sm shadow-slate-950/5">
              <button
                onClick={() => setShowPlanningConfig(prev => !prev)}
                className="flex w-full items-center gap-2 border-b border-border px-4 py-3 text-left transition hover:bg-base/40"
              >
                <Database size={14} className="text-accent" />
                <span className="flex-1 text-sm font-semibold text-primary">Planning Setup Workspace</span>
                <span className="rounded-full bg-base px-2 py-0.5 text-[10px] text-secondary">
                  Resource {aggregatedResources.length} · State {stateVars.length} · Agent {resolvedSelectedAgentIds.length} · Goal {goalCount}
                </span>
                {showPlanningConfig ? <ChevronDown size={14} className="text-muted" /> : <ChevronRight size={14} className="text-muted" />}
              </button>
              {showPlanningConfig && (
                <div className="grid gap-2 p-3 md:grid-cols-2 2xl:grid-cols-4 items-start">

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
                      {generatedResourceNames.map((name) => (
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
            <ThemedSection
              icon={ToggleLeft}
              title="State"
              count={stateVars.length}
              theme={SECTION_THEME.state}
              compact
              actions={selectedDistributor ? (
                <button
                  onClick={handleOpenStateMergePolicyModal}
                  className="inline-flex items-center gap-1 rounded border border-violet-500/20 bg-violet-500/10 px-2 py-1 text-[10px] font-medium text-violet-200 hover:bg-violet-500/15"
                  title={t('pddl.mergePolicyButton')}
                >
                  <SlidersHorizontal size={11} />
                  {t('pddl.mergePolicyButton')}
                </button>
              ) : null}
            >
              <div className="max-h-[220px] overflow-y-auto space-y-1.5">
                <div className="flex gap-1">
                  <input
                    className="flex-1 min-w-0 rounded border border-border bg-base px-2 py-1 text-[11px] text-primary placeholder:text-muted outline-none focus:border-violet-500/50"
                    placeholder={`${t('pddl.stateName')} (예: at_{{resource.name}})`}
                    value={newStateName}
                    onChange={e => {
                      setNewStateName(e.target.value)
                      setStateBuilderMessage(null)
                    }}
                    onKeyDown={e => e.key === 'Enter' && handleAddState()}
                  />
                  <select
                    className="rounded border border-border bg-base px-1 py-1 text-[10px] text-primary outline-none"
                    value={newStateType}
                    onChange={e => {
                      setNewStateType(e.target.value as 'bool' | 'int' | 'string')
                      setStateBuilderMessage(null)
                    }}
                  >
                    <option value="bool">bool</option>
                    <option value="int">int</option>
                    <option value="string">str</option>
                  </select>
                  <button onClick={handleAddState} disabled={!newStateName.trim()} className="rounded bg-violet-500/20 px-1.5 text-violet-400 disabled:opacity-40"><Plus size={12} /></button>
                </div>
                <div className="flex gap-1">
                  {newStateType === 'bool' ? (
                    <select
                      className="w-full rounded border border-border bg-base px-2 py-1 text-[11px] text-primary outline-none focus:border-violet-500/50"
                      value={newStateInitialValue || 'false'}
                      onChange={e => {
                        setNewStateInitialValue(e.target.value)
                        setStateBuilderMessage(null)
                      }}
                    >
                      <option value="false">initial: false</option>
                      <option value="true">initial: true</option>
                    </select>
                  ) : (
                    <input
                      type={newStateType === 'int' ? 'number' : 'text'}
                      className="w-full rounded border border-border bg-base px-2 py-1 text-[11px] text-primary placeholder:text-muted outline-none focus:border-violet-500/50"
                      placeholder={newStateType === 'int' ? 'initial value (e.g. 0)' : 'initial value (optional)'}
                      value={newStateInitialValue}
                      onChange={e => {
                        setNewStateInitialValue(e.target.value)
                        setStateBuilderMessage(null)
                      }}
                    />
                  )}
                </div>
                <div className="rounded border border-violet-500/20 bg-violet-500/5 px-2 py-1 text-[10px] leading-4 text-secondary">
                  상태 이름에 <span className="font-mono text-violet-200">{'{{resource.name}}'}</span> 을 넣으면
                  resource instance별로 상태를 일괄 생성합니다. <span className="font-mono text-violet-200">{'{{agent.name}}'}</span> / <span className="font-mono text-violet-200">{'{{agent.id}}'}</span> 도 동일하게 확장됩니다.
                </div>
                {stateBuilderMessage && (
                  <div className="rounded border border-violet-500/20 bg-violet-500/10 px-2 py-1 text-[10px] text-violet-200">
                    {stateBuilderMessage}
                  </div>
                )}
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
            <ThemedSection icon={Bot} title="Agent" count={`${resolvedSelectedAgentIds.length}/${agents.length}`} theme={SECTION_THEME.agent} compact>
              <div className="max-h-[220px] overflow-y-auto space-y-1">
                {recommendedAgentIds.length > 0 && resolvedSelectedAgentIds.length === 0 && (
                  <button onClick={() => setSelectedAgentIds(Array.from(new Set(recommendedAgentIds)))} className="w-full rounded border border-emerald-500/20 bg-emerald-500/10 py-1 text-[10px] font-medium text-emerald-400 hover:bg-emerald-500/20">
                    {t('pddl.selectRecommended')} ({recommendedAgentIds.length})
                  </button>
                )}
                {sortedAgents.length === 0 ? (
                  <p className="py-3 text-center text-[10px] text-muted">{t('pddl.noAgentsAvailable')}</p>
                ) : sortedAgents.map(({ agent, isOnline }) => {
                  const selected = resolvedSelectedAgentIds.includes(agent.id)
                  const activeDispatch = (agentDispatchMap.get(agent.id) || [])[0]
                  const heldResources = heldResourcesByAgent.get(agent.id) || heldResourcesByAgent.get(agent.name) || []
                  const runtime = agentRuntimeMap[agent.id]
                  const agentStateLabel = resolveRuntimeStateLabel(runtime, agent) || (isOnline ? t('pddl.agentReadyState') : t('pddl.agentOfflineState'))
                  const currentStepId = resolveRuntimeStepId(singleSelectedBT, runtime)
                  const currentStepName = currentStepId
                    ? singleSelectedBT?.steps.find(step => step.id === currentStepId)?.job_name || singleSelectedBT?.steps.find(step => step.id === currentStepId)?.name || currentStepId
                    : null
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
                        {(runtime?.current_graph_id || agent.current_graph_id) && (
                          <span className="rounded bg-surface px-1.5 py-0.5 text-secondary">{runtime?.current_graph_id || agent.current_graph_id}</span>
                        )}
                        {activeDispatch && (
                          <span className="rounded bg-surface px-1.5 py-0.5 text-secondary">
                            {translateStatus(activeDispatch.status)} · {activeDispatch.task_name || activeDispatch.step_name || activeDispatch.task_id || activeDispatch.step_id}
                          </span>
                        )}
                        {currentStepName && (
                          <span className="rounded bg-blue-500/10 px-1.5 py-0.5 text-blue-300">
                            step · {currentStepName}
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

                </div>
              )}
            </div>

            <ThemedSection
              icon={Activity}
              title="Realtime Control Room"
              count={realtimeMonitoringCards.length}
              theme={SECTION_THEME.plan}
            >
              {!realtimeSession ? (
                <div className="rounded-xl border border-dashed border-border bg-base/30 px-4 py-8 text-center text-sm text-muted">
                  Realtime 세션을 시작하면 agent / resource 상태와 메시지를 여기서 바로 볼 수 있습니다.
                </div>
              ) : (
                <div className="space-y-4">
                  <div className="rounded-2xl border border-border bg-base/40 p-4">
                    <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
                      <div>
                        <div className="text-xs font-semibold uppercase tracking-wider text-secondary">
                          Monitoring overview
                        </div>
                        <div className="mt-1 text-[11px] text-muted">
                          Effective state 기준으로 agent / resource의 핵심 status, msg만 요약해서 보여줍니다.
                        </div>
                      </div>
                      <span className="rounded-full bg-surface px-3 py-1 text-[11px] text-secondary">
                        session · {realtimeSession.id}
                      </span>
                    </div>

                    {realtimeMonitoringCards.length === 0 ? (
                      <div className="rounded-xl border border-dashed border-border bg-base/30 px-4 py-8 text-center text-sm text-muted">
                        표시할 agent/resource 카드가 없습니다.
                      </div>
                    ) : (
                      <div className="grid gap-3 xl:grid-cols-2">
                        {realtimeMonitoringCards.map((card) => {
                          const msgDisplay = String(card.msgValue ?? '').trim()
                          const hasMsg = msgDisplay !== ''
                          return (
                            <div
                              key={card.id}
                              className="rounded-2xl border border-border bg-gradient-to-br from-base/90 to-surface/90 p-4 shadow-[0_8px_20px_rgba(0,0,0,0.10)]"
                            >
                              <div className="flex items-start justify-between gap-3">
                                <div className="min-w-0">
                                  <div className="flex items-center gap-2">
                                    <div className="truncate text-sm font-semibold text-primary">{card.label}</div>
                                    <span className={`rounded-full px-2 py-0.5 text-[10px] ${
                                      card.kind === 'agent'
                                        ? 'bg-emerald-500/10 text-emerald-300'
                                        : 'bg-amber-500/10 text-amber-300'
                                    }`}>
                                      {card.kind}
                                    </span>
                                  </div>
                                  <div className="mt-3 flex flex-wrap items-center gap-2">
                                    <span className="text-[10px] font-semibold uppercase tracking-wider text-secondary">
                                      status
                                    </span>
                                    <span className={`inline-flex items-center rounded-full border px-2.5 py-1 text-xs font-mono ${stateValueClass(card.statusValue)}`}>
                                      {card.statusValue || '-'}
                                    </span>
                                  </div>
                                </div>
                                <button
                                  onClick={() => handleResetRealtimeMonitorCard(card)}
                                  disabled={isResettingMonitorKey === card.id || Object.keys(card.resetValues).length === 0}
                                  className="inline-flex shrink-0 items-center gap-1.5 rounded-xl border border-border bg-surface px-3 py-2 text-[11px] font-medium text-secondary transition hover:border-accent/20 hover:text-primary disabled:cursor-not-allowed disabled:opacity-40"
                                  title={Object.keys(card.resetValues).length === 0 ? '초기값이 정의된 status/msg state가 없습니다.' : 'status와 msg를 distributor 기본값으로 되돌립니다.'}
                                >
                                  <RotateCcw size={12} />
                                  {isResettingMonitorKey === card.id ? 'Resetting...' : 'Reset default'}
                                </button>
                              </div>

                              <div className="mt-4 rounded-xl border border-border bg-base/60 px-3 py-3">
                                <div className="flex items-center gap-2">
                                  <span className="text-[10px] font-semibold uppercase tracking-wider text-secondary">
                                    msg
                                  </span>
                                  {card.msgKey && (
                                    <span className="rounded-full bg-base px-2 py-0.5 text-[10px] text-muted">
                                      {card.msgKey}
                                    </span>
                                  )}
                                </div>
                                <div className={`mt-2 rounded-xl border px-3 py-2 text-xs font-mono ${hasMsg ? 'border-amber-500/20 bg-amber-500/5 text-amber-100' : 'border-border bg-surface/60 text-muted'}`}>
                                  {hasMsg ? msgDisplay : '""'}
                                </div>
                              </div>
                            </div>
                          )
                        })}
                      </div>
                    )}
                  </div>

                  <details className="rounded-2xl border border-border bg-base/40 p-4">
                    <summary className="cursor-pointer list-none">
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <div>
                          <div className="text-xs font-semibold uppercase tracking-wider text-secondary">
                            Raw state details
                          </div>
                          <div className="mt-1 text-[11px] text-muted">
                            필요할 때만 펼쳐서 effective/current/live 원본 값을 확인합니다.
                          </div>
                        </div>
                        <span className="rounded-full bg-surface px-3 py-1 text-[11px] text-secondary">
                          effective {sortStateEntries(realtimeSession.effective_state || realtimeSession.live_state || realtimeSession.current_state).length}
                          {' · '}
                          current {sortStateEntries(realtimeSession.current_state).length}
                          {' · '}
                          live {sortStateEntries(realtimeSession.live_state).length}
                        </span>
                      </div>
                    </summary>

                    <div className="mt-4 grid gap-4 xl:grid-cols-3">
                      <div className="rounded-2xl border border-border bg-base/50 p-4">
                        <div className="mb-2 text-xs font-semibold uppercase tracking-wider text-secondary">
                          Effective state
                        </div>
                        <div className="space-y-2">
                          {sortStateEntries(realtimeSession.effective_state || realtimeSession.live_state || realtimeSession.current_state).length === 0 ? (
                            <div className="text-sm text-muted">표시할 merged state가 없습니다.</div>
                          ) : sortStateEntries(realtimeSession.effective_state || realtimeSession.live_state || realtimeSession.current_state).map(([key, value]) => (
                            <div key={`rt-effective:${key}`} className="flex items-center justify-between gap-3 rounded-xl border border-border bg-surface/60 px-3 py-2 text-sm">
                              <span className="font-mono text-primary">{key}</span>
                              <span className="font-mono text-secondary">{value}</span>
                            </div>
                          ))}
                        </div>
                      </div>

                      <div className="rounded-2xl border border-border bg-base/50 p-4">
                        <div className="mb-2 text-xs font-semibold uppercase tracking-wider text-secondary">
                          Current state
                        </div>
                        <div className="space-y-2">
                          {sortStateEntries(realtimeSession.current_state).length === 0 ? (
                            <div className="text-sm text-muted">저장된 planner state 없음</div>
                          ) : sortStateEntries(realtimeSession.current_state).map(([key, value]) => (
                            <div key={`rt-current:${key}`} className="flex items-center justify-between gap-3 rounded-xl border border-border bg-surface/60 px-3 py-2 text-sm">
                              <span className="font-mono text-primary">{key}</span>
                              <span className="font-mono text-secondary">{value}</span>
                            </div>
                          ))}
                        </div>
                      </div>

                      <div className="rounded-2xl border border-border bg-base/50 p-4">
                        <div className="mb-2 text-xs font-semibold uppercase tracking-wider text-secondary">
                          Live state
                        </div>
                        <div className="space-y-2">
                          {sortStateEntries(realtimeSession.live_state).length === 0 ? (
                            <div className="text-sm text-muted">실시간 overlay state 없음</div>
                          ) : sortStateEntries(realtimeSession.live_state).map(([key, value]) => (
                            <div key={`rt-live:${key}`} className="flex items-center justify-between gap-3 rounded-xl border border-border bg-surface/60 px-3 py-2 text-sm">
                              <span className="font-mono text-primary">{key}</span>
                              <span className="font-mono text-secondary">{value}</span>
                            </div>
                          ))}
                        </div>
                      </div>
                    </div>
                  </details>
                </div>
              )}
            </ThemedSection>

            <ThemedSection
              icon={Workflow}
              title="Realtime PDDL Loop"
              count={validRealtimeGoals.length}
              theme={SECTION_THEME.plan}
              defaultOpen={false}
            >
              <div className="space-y-4">
                <div className="flex flex-wrap items-center gap-3 rounded-2xl border border-border bg-base/40 p-4">
                  <div className="min-w-[160px]">
                    <div className="text-xs font-semibold text-primary">Loop tick (sec)</div>
                    <div className="mt-1 text-[11px] text-secondary">
                      우선순위 goal 후보를 주기적으로 다시 평가합니다.
                    </div>
                  </div>
                  <input
                    type="number"
                    min={0.5}
                    step={0.5}
                    value={realtimeTickIntervalSec}
                    onChange={(e) => setRealtimeTickIntervalSec(Math.max(0.5, Number(e.target.value) || 2))}
                    className="w-28 rounded-xl border border-border bg-surface px-3 py-2 text-sm text-primary outline-none"
                  />
                  <button
                    onClick={handleImportCurrentGoalToRealtime}
                    className="inline-flex items-center gap-1.5 rounded-xl border border-border bg-surface px-3 py-2 text-xs font-medium text-secondary transition hover:border-accent/20 hover:text-primary"
                  >
                    <Plus size={14} />
                    현재 Goal 복사
                  </button>
                  <button
                    onClick={handleStartRealtime}
                    disabled={!canStartRealtime}
                    title={realtimeStartDisabledReason || undefined}
                    className="inline-flex items-center gap-1.5 rounded-xl bg-accent px-3 py-2 text-xs font-medium text-white transition hover:bg-accent/80 disabled:cursor-not-allowed disabled:opacity-40"
                  >
                    <Play size={14} />
                    Start realtime
                  </button>
                  {hasRealtimeSession && realtimeSessionId && (
                    <button
                      onClick={handleStopRealtime}
                      disabled={isStoppingRealtime}
                      className="inline-flex items-center gap-1.5 rounded-xl bg-red-600 px-3 py-2 text-xs font-medium text-white transition hover:bg-red-500 disabled:cursor-not-allowed disabled:opacity-60"
                    >
                      <Square size={14} />
                      {isStoppingRealtime ? 'Stopping...' : 'Stop realtime'}
                    </button>
                  )}
                  {realtimeSession && (
                    <div className="ml-auto flex flex-wrap items-center gap-2 text-[11px] text-secondary">
                      <span className="rounded-full bg-surface px-3 py-1">
                        session · {realtimeSession.id}
                      </span>
                      <span className={`rounded-full px-3 py-1 ${
                        realtimeSession.status === 'running' ? 'bg-emerald-500/10 text-emerald-300'
                          : realtimeSession.status === 'error' ? 'bg-red-500/10 text-red-300'
                          : 'bg-surface text-secondary'
                      }`}>
                        {realtimeSession.status}
                      </span>
                      {realtimeSession.selected_goal_name && (
                        <span className="rounded-full bg-sky-500/10 px-3 py-1 text-sky-300">
                          goal · {realtimeSession.selected_goal_name}
                        </span>
                      )}
                      {realtimeSession.selected_agent_name && (
                        <span className="rounded-full bg-violet-500/10 px-3 py-1 text-violet-300">
                          agent · {realtimeSession.selected_agent_name}
                        </span>
                      )}
                      {realtimeSession.selected_resource_name && (
                        <span className="rounded-full bg-amber-500/10 px-3 py-1 text-amber-300">
                          resource · {realtimeSession.selected_resource_name}
                        </span>
                      )}
                    </div>
                  )}
                </div>
                {!canStartRealtime && !hasRealtimeSession && realtimeStartDisabledReason && (
                  <div className="rounded-xl border border-amber-500/20 bg-amber-500/10 px-3 py-2 text-[11px] text-amber-200">
                    Start realtime 비활성화 사유: {realtimeStartDisabledReason}
                  </div>
                )}

                {realtimeSession?.last_error && (
                  <div className="flex items-center gap-2 rounded-xl border border-red-500/20 bg-red-500/5 px-4 py-3 text-xs text-red-300">
                    <AlertTriangle size={14} />
                    {realtimeSession.last_error}
                  </div>
                )}
                {realtimeNotice && (
                  <div className="rounded-xl border border-sky-500/20 bg-sky-500/5 px-4 py-3 text-xs text-sky-200">
                    {realtimeNotice}
                  </div>
                )}

                <RealtimeGoalEditor
                  stateVars={stateVars}
                  resourceTypes={realtimeGoalResourceTypeOptions}
                  goals={realtimeGoals}
                  onChange={setRealtimeGoals}
                />

              </div>
            </ThemedSection>

            <ThemedSection
              icon={Bot}
              title="Realtime Agent Task Sequence"
              count={realtimeTaskRows.length}
              theme={SECTION_THEME.agent}
            >
              <div className="space-y-2.5">
                {realtimeTaskRows.length === 0 ? (
                  <div className="rounded-xl border border-dashed border-border bg-base/30 px-4 py-8 text-center text-sm text-muted">
                    실행 중인 agent/task 시퀀스가 없습니다.
                  </div>
                ) : (
                  <div className="max-h-[360px] space-y-2.5 overflow-y-auto pr-1">
                    {realtimeTaskRows.map((row) => {
                      return (
                        <div key={`realtime-seq-${row.agentId}`} className="rounded-2xl border border-border bg-gradient-to-br from-base/90 to-surface/90 p-3 shadow-[0_10px_24px_rgba(0,0,0,0.12)]">
                          <div className="flex flex-col gap-2 xl:flex-row xl:items-start xl:justify-between">
                            <div className="min-w-0 flex-1 space-y-2">
                              <div className="flex flex-wrap items-center gap-2">
                                <span className="truncate text-sm font-semibold text-primary">{row.agent?.name || row.agentId}</span>
                                <span className={`rounded-full px-2 py-0.5 text-[10px] ${
                                  row.isOnline ? 'bg-emerald-500/10 text-emerald-300' : 'bg-red-500/10 text-red-300'
                                }`}>
                                  {row.stateLabel}
                                </span>
                                <span className="rounded-full bg-surface px-2 py-0.5 text-[10px] text-secondary">
                                  queue {row.timeline.length}
                                </span>
                                {row.current?.status === 'running' && (
                                  <span className="inline-flex items-center gap-1 rounded-full bg-cyan-500/10 px-2 py-0.5 text-[10px] text-cyan-300">
                                    <Activity size={11} />
                                    live
                                  </span>
                                )}
                              </div>

                              <div className="flex flex-wrap items-center gap-1.5 text-[10px]">
                                <span className="rounded-full bg-base/70 px-2 py-0.5 text-secondary">
                                  이전 · {row.previous?.taskName || '없음'}
                                </span>
                                <span className="rounded-full bg-emerald-500/10 px-2 py-0.5 text-emerald-300">
                                  지금 · {row.current?.taskName || '없음'}
                                </span>
                                <span className="rounded-full bg-base/70 px-2 py-0.5 text-secondary">
                                  다음 · {row.next?.taskName || '없음'}
                                </span>
                              </div>
                            </div>

                            <div className="grid min-w-[220px] gap-1.5 text-[10px] xl:w-[240px]">
                              <div className="rounded-xl border border-border bg-base/50 px-3 py-2">
                                <div className="mb-0.5 uppercase tracking-wider text-muted">현재 액션</div>
                                <div className="truncate font-medium text-primary">
                                  {row.current?.currentStepName || row.currentStepName || row.current?.taskName || '대기 중'}
                                </div>
                              </div>
                              <div className="rounded-xl border border-border bg-base/50 px-3 py-2">
                                <div className="mb-0.5 uppercase tracking-wider text-muted">점유 Resource</div>
                                <div className="truncate font-medium text-primary">
                                  {row.heldResources.length > 0
                                    ? row.heldResources.slice(0, 3).map(item => item.resource).join(', ')
                                    : '없음'}
                                </div>
                              </div>
                            </div>
                          </div>

                          {row.timeline.length === 0 ? (
                            <div className="mt-2 rounded-xl border border-dashed border-border bg-base/20 px-4 py-5 text-center text-sm text-muted">
                              현재 큐에 표시할 task가 없습니다.
                            </div>
                          ) : (
                            <div className="mt-2 overflow-x-auto pb-1">
                              <div className="flex min-w-max items-stretch gap-3 pr-2">
                                {row.timeline.map((task, index) => {
                                  const isFocused = task.taskId === row.current?.taskId || task.status === 'running'
                                  const completedCount = task.completedStepCount || task.completedStepIds?.length || 0
                                  const totalCount = task.totalStepCount || 0
                                  return (
                                    <div key={`${row.agentId}:${task.taskId}:${index}`} className="flex items-stretch gap-3">
                                      <button
                                        onClick={() => handleOpenTaskFlow(task)}
                                        title={task.taskId}
                                        className={`group relative w-[210px] rounded-xl border px-3 py-2.5 text-left transition ${
                                          isFocused
                                            ? 'border-cyan-400/30 bg-cyan-500/8 shadow-[0_10px_24px_rgba(34,211,238,0.08)]'
                                            : statusBadgeClass(task.status)
                                        } ${task.status === 'completed' ? 'opacity-80' : ''}`}
                                      >
                                        <div className="mb-2 flex items-start justify-between gap-2">
                                          <div className="inline-flex items-center gap-2">
                                            <span className="rounded-lg border border-border bg-base/70 px-2 py-1 text-[10px] font-semibold text-secondary">
                                              #{task.order}
                                            </span>
                                            {isFocused && (
                                              <span className="inline-flex h-2.5 w-2.5 rounded-full bg-cyan-300 shadow-[0_0_14px_rgba(34,211,238,0.8)]" />
                                            )}
                                          </div>
                                            <span className={`rounded-full border px-2 py-0.5 text-[10px] uppercase ${statusBadgeClass(task.status)}`}>
                                              {task.status}
                                            </span>
                                        </div>

                                        <div className="line-clamp-2 min-h-[34px] text-sm font-semibold leading-5 text-primary">
                                          {task.taskName}
                                        </div>
                                        <div className="mt-2 space-y-1.5">
                                          <div className="rounded-lg border border-border bg-base/55 px-2.5 py-2">
                                            <div className="mb-0.5 flex items-center gap-1 text-[10px] uppercase tracking-wider text-muted">
                                              <Clock3 size={11} />
                                              step
                                            </div>
                                            <div className="truncate text-xs font-medium text-primary">
                                              {task.currentStepName || (task.status === 'completed' ? '모든 step 완료' : '대기 중')}
                                            </div>
                                          </div>

                                          {totalCount > 0 && (
                                            <div className="flex items-center justify-between gap-2 text-[11px] text-secondary">
                                              <span className="inline-flex items-center gap-1">
                                                <CheckCircle2 size={11} />
                                                progress
                                              </span>
                                              <span className="font-mono text-primary">
                                                {completedCount}/{totalCount}
                                              </span>
                                            </div>
                                          )}

                                          {task.error && (
                                            <div className="line-clamp-2 rounded-lg border border-red-500/20 bg-red-500/8 px-2.5 py-2 text-[11px] text-red-300">
                                              {task.error}
                                            </div>
                                          )}
                                        </div>
                                      </button>

                                      {index < row.timeline.length - 1 && (
                                        <div className="flex items-center justify-center text-secondary/70">
                                          <ArrowRight size={16} />
                                        </div>
                                      )}
                                    </div>
                                  )
                                })}
                              </div>
                            </div>
                          )}
                        </div>
                      )
                    })}
                  </div>
                )}
              </div>
            </ThemedSection>

            {/* ========================================================== */}
            {/* PLAN & EXECUTION — rose (prominent)                        */}
            {/* ========================================================== */}
            <ThemedSection
              icon={Layers}
              title={t('pddl.operationsBoardTitle')}
              count={plan ? (plan.is_valid ? `${plan.total_tasks ?? plan.total_steps} Task` : t('status.failed')) : undefined}
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
                taskPlanning={selectedTaskPlanning}
                taskPlanningByTaskId={selectedTaskPlanningByTaskId}
                taskName={selectedBTs.length === 1 ? selectedBTs[0].name : selectedBehaviorTreeLabel}
                taskNameByTaskId={Object.fromEntries(selectedBTs.map(bt => [bt.id, bt.name]))}
                requiredActionTypes={requiredActionTypes}
                requiredActionTypesByTaskId={requiredActionTypesByTaskId}
                execution={execution}
                resources={selectedDistributor?.resources || []}
              />

              {singleSelectedBT && runtimeViewAgents.length > 0 && (
                <div className="mt-4 space-y-3">
                  <div className="flex items-center gap-2 px-1">
                    <Bot size={14} className="text-emerald-400" />
                    <span className="text-xs font-semibold uppercase tracking-wider text-emerald-300">
                      RTM Live View
                    </span>
                    <span className="rounded-full bg-emerald-500/10 px-2 py-0.5 text-[10px] text-emerald-300">
                      {runtimeViewAgents.length}
                    </span>
                  </div>

                  <div className="grid gap-3 xl:grid-cols-2">
                    {runtimeViewAgents.map((entry) => {
                      const activeDispatch = entry.dispatches[0]
                      const graphMismatch = entry.runtime?.current_graph_id && entry.runtime.current_graph_id !== singleSelectedBT.id

                      return (
                        <div key={entry.agentId} className="rounded-2xl border border-border bg-base/60 p-3">
                          <div className="flex flex-wrap items-start justify-between gap-2">
                            <div>
                              <div className="text-sm font-semibold text-primary">
                                {entry.agent?.name || entry.agentId}
                              </div>
                              <div className="mt-1 flex flex-wrap items-center gap-1.5 text-[10px] text-secondary">
                                <span className={`rounded-full px-2 py-0.5 ${
                                  entry.isOnline ? 'bg-emerald-500/10 text-emerald-300' : 'bg-red-500/10 text-red-300'
                                }`}>
                                  {entry.stateLabel}
                                </span>
                                {entry.runtime?.semantic_tags?.slice(0, 3).map(tag => (
                                  <span key={`${entry.agentId}:${tag}`} className="rounded-full bg-surface px-2 py-0.5 text-[10px] text-secondary">
                                    {tag}
                                  </span>
                                ))}
                              </div>
                            </div>
                            {activeDispatch && (
                              <span className="rounded-full bg-surface px-2 py-1 text-[10px] text-secondary">
                                {translateStatus(activeDispatch.status)} · {activeDispatch.task_name || activeDispatch.step_name || activeDispatch.task_id}
                              </span>
                            )}
                          </div>

                          <div className="mt-3 grid gap-2 md:grid-cols-3">
                            <div className="rounded-xl border border-border bg-surface px-3 py-2">
                              <div className="text-[10px] uppercase tracking-wider text-muted">Graph</div>
                              <div className="mt-1 truncate text-xs text-primary">
                                {entry.runtime?.current_graph_id || entry.agent?.current_graph_id || singleSelectedBT.id}
                              </div>
                            </div>
                            <div className="rounded-xl border border-border bg-surface px-3 py-2">
                              <div className="text-[10px] uppercase tracking-wider text-muted">Current Step</div>
                              <div className="mt-1 truncate text-xs text-primary">
                                {entry.currentStepName || (graphMismatch ? '다른 BT 실행 중' : 'idle')}
                              </div>
                            </div>
                            <div className="rounded-xl border border-border bg-surface px-3 py-2">
                              <div className="text-[10px] uppercase tracking-wider text-muted">Held Resources</div>
                              <div className="mt-1 truncate text-xs text-primary">
                                {entry.heldResources.length > 0
                                  ? entry.heldResources.map(resource => resource.resource).join(', ')
                                  : 'none'}
                              </div>
                            </div>
                          </div>

                          <div className="mt-3 h-56 overflow-hidden rounded-2xl border border-border bg-base">
                            <ActionGraphViewer
                              actionGraph={singleSelectedBT}
                              currentStepId={entry.currentStepId}
                              className="h-full"
                              compact={true}
                              showControls={false}
                              showMiniMap={false}
                            />
                          </div>
                        </div>
                      )
                    })}
                  </div>
                </div>
              )}
            </ThemedSection>

          </div>
        )}
      </main>

      {showProfileLoadModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="w-full max-w-2xl rounded-2xl border border-border bg-surface shadow-2xl">
            <div className="flex items-center justify-between border-b border-border px-4 py-3">
              <div>
                <div className="text-sm font-semibold text-primary">Import JSON</div>
                <div className="text-xs text-secondary">Save_files/pddl 폴더에서 프로필 JSON을 선택하세요.</div>
              </div>
              <button
                onClick={() => setShowProfileLoadModal(false)}
                className="rounded-lg border border-border bg-base px-2 py-1 text-xs text-secondary transition hover:text-primary"
              >
                닫기
              </button>
            </div>
            <div className="max-h-[60vh] space-y-2 overflow-y-auto p-3">
              {savedProfileFiles.length === 0 ? (
                <div className="rounded-xl border border-border bg-base px-3 py-4 text-xs text-secondary">
                  저장된 프로필이 없습니다.
                </div>
              ) : (
                savedProfileFiles.map(file => (
                  <button
                    key={file.name}
                    onClick={() => handleImportDistributorProfile(file.name)}
                    className="w-full rounded-xl border border-border bg-base px-3 py-2 text-left transition hover:border-accent/30 hover:bg-accent/10"
                  >
                    <div className="truncate text-xs font-medium text-primary">{file.name}</div>
                    <div className="mt-1 text-[10px] text-secondary">
                      {new Date(file.updated_at).toLocaleString()} · {Math.max(1, Math.round(file.size_bytes / 1024))} KB
                    </div>
                  </button>
                ))
              )}
            </div>
          </div>
        </div>
      )}

      {showStateMergePolicyModal && selectedDistributor && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="flex w-full max-w-4xl flex-col rounded-2xl border border-border bg-surface shadow-2xl">
            <div className="flex items-start justify-between gap-4 border-b border-border px-5 py-4">
              <div>
                <div className="text-sm font-semibold text-primary">{t('pddl.mergePolicyModalTitle')}</div>
                <div className="mt-1 text-xs leading-5 text-secondary">
                  {t('pddl.mergePolicyModalDesc')}
                </div>
              </div>
              <button
                onClick={() => setShowStateMergePolicyModal(false)}
                className="rounded-lg border border-border bg-base px-2 py-1 text-xs text-secondary transition hover:text-primary"
              >
                {t('common.close')}
              </button>
            </div>

            <div className="max-h-[70vh] overflow-y-auto px-5 py-4">
              <div className="grid gap-3">
                {stateMergePolicyRows.map(row => {
                  const draftPolicy = stateMergePolicyDraft.find(policy => policy.pattern === row.pattern)
                  const priority = (draftPolicy?.priority === 'planner' ? 'planner' : 'live') as StateMergePriority
                  return (
                    <div key={row.pattern} className="rounded-2xl border border-border bg-base/60 p-4">
                      <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                        <div className="min-w-0 flex-1">
                          <div className="text-xs font-semibold text-primary">{t(row.titleKey as never)}</div>
                          <div className="mt-1 font-mono text-[11px] text-violet-300">{row.pattern}</div>
                          <div className="mt-2 text-[11px] leading-5 text-secondary">{t(row.descriptionKey as never)}</div>
                          <div className="mt-2 flex flex-wrap gap-1">
                            {row.matchedStates.length > 0 ? row.matchedStates.slice(0, 4).map(state => (
                              <span key={state.id} className="rounded-full bg-surface px-2 py-0.5 text-[10px] text-secondary">
                                {state.name}
                              </span>
                            )) : (
                              <span className="rounded-full bg-surface px-2 py-0.5 text-[10px] text-muted">
                                {t('pddl.mergePolicyNoMatches')}
                              </span>
                            )}
                            {row.matchedStates.length > 4 && (
                              <span className="rounded-full bg-surface px-2 py-0.5 text-[10px] text-secondary">
                                +{row.matchedStates.length - 4}
                              </span>
                            )}
                          </div>
                        </div>

                        <div className="shrink-0">
                          <div className="inline-flex rounded-xl border border-border bg-surface p-1">
                            <button
                              onClick={() => handleSetStateMergePriority(row.pattern, 'planner')}
                              className={`rounded-lg px-3 py-1.5 text-[11px] font-medium transition ${
                                priority === 'planner'
                                  ? 'bg-base text-primary shadow-sm'
                                  : 'text-secondary hover:text-primary'
                              }`}
                            >
                              {t('pddl.mergePolicyPlanner')}
                            </button>
                            <button
                              onClick={() => handleSetStateMergePriority(row.pattern, 'live')}
                              className={`rounded-lg px-3 py-1.5 text-[11px] font-medium transition ${
                                priority === 'live'
                                  ? 'bg-violet-500/15 text-violet-200 shadow-sm'
                                  : 'text-secondary hover:text-primary'
                              }`}
                            >
                              {t('pddl.mergePolicyLive')}
                            </button>
                          </div>
                        </div>
                      </div>
                    </div>
                  )
                })}
              </div>
            </div>

            <div className="flex items-center justify-between gap-3 border-t border-border px-5 py-4">
              <div className="text-[11px] text-secondary">
                {t('pddl.mergePolicyFooterHint')}
              </div>
              <div className="flex items-center gap-2">
                <button
                  onClick={() => {
                    setStateMergePolicyDraft(normalizeStateMergePolicies(undefined))
                  }}
                  className="rounded-xl border border-border bg-base px-3 py-2 text-xs text-secondary transition hover:text-primary"
                >
                  {t('pddl.mergePolicyResetDefaults')}
                </button>
                <button
                  onClick={() => setShowStateMergePolicyModal(false)}
                  className="rounded-xl border border-border bg-base px-3 py-2 text-xs text-secondary transition hover:text-primary"
                >
                  {t('common.cancel')}
                </button>
                <button
                  onClick={handleSaveStateMergePolicies}
                  disabled={isSavingStateMergePolicies}
                  className="rounded-xl bg-accent px-3 py-2 text-xs font-medium text-white transition hover:bg-accent/80 disabled:opacity-50"
                >
                  {isSavingStateMergePolicies ? t('common.saving') : t('common.save')}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {(selectedFlowTree || isLoadingFlowTree) && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="flex h-[80vh] w-full max-w-6xl flex-col rounded-2xl border border-border bg-surface shadow-2xl">
            <div className="flex items-center justify-between border-b border-border px-4 py-3">
              <div>
                <div className="text-sm font-semibold text-primary">Task BT Flow</div>
                <div className="text-xs text-secondary">{selectedFlowLabel}</div>
                {selectedFlowBlock && (
                  <div className="mt-2 flex flex-wrap items-center gap-2 text-[11px]">
                    <span className="rounded-full bg-surface px-2 py-1 text-secondary">
                      {(realtimeTaskRows.find(row => row.agentId === selectedFlowBlock.agentId)?.agent?.name) || selectedFlowBlock.agentId}
                    </span>
                    <span className={`rounded-full border px-2 py-1 ${statusBadgeClass(selectedFlowBlock.status)}`}>
                      {selectedFlowBlock.status}
                    </span>
                    {selectedFlowLiveStepName && (
                      <span className="rounded-full bg-cyan-500/10 px-2 py-1 text-cyan-300">
                        live BT step · {selectedFlowLiveStepName}
                      </span>
                    )}
                    {selectedFlowBlock.totalStepCount ? (
                      <span className="rounded-full bg-base px-2 py-1 text-secondary">
                        progress {selectedFlowBlock.completedStepCount || 0}/{selectedFlowBlock.totalStepCount}
                      </span>
                    ) : null}
                  </div>
                )}
              </div>
              <button
                onClick={() => {
                  setSelectedFlowTree(null)
                  setSelectedFlowLabel('Task flow')
                  setSelectedFlowAgentId(null)
                  setSelectedFlowTaskId(null)
                  setIsLoadingFlowTree(false)
                }}
                className="rounded-lg border border-border bg-base px-2 py-1 text-xs text-secondary transition hover:text-primary"
              >
                닫기
              </button>
            </div>
            <div className="flex-1 overflow-hidden p-3">
              {isLoadingFlowTree ? (
                <div className="flex h-full items-center justify-center text-sm text-secondary">
                  BT 흐름을 불러오는 중...
                </div>
              ) : selectedFlowTree ? (
                <div className="h-full overflow-hidden rounded-xl border border-border bg-base">
                  <ActionGraphViewer
                    actionGraph={selectedFlowTree}
                    currentStepId={selectedFlowLiveStepId}
                    completedSteps={selectedFlowCompletedSteps}
                    failedSteps={selectedFlowFailedSteps}
                    className="h-full"
                    showControls={true}
                    showMiniMap={true}
                  />
                </div>
              ) : (
                <div className="flex h-full items-center justify-center text-sm text-muted">
                  표시할 BT가 없습니다.
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
