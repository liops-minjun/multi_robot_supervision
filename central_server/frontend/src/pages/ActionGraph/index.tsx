import React, { useState, useCallback, useMemo, useEffect, useRef } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import ReactFlow, {
  Node,
  Edge,
  Controls,
  Background,
  useNodesState,
  useEdgesState,
  addEdge,
  Connection,
  BackgroundVariant,
  Panel,
  useReactFlow,
  ReactFlowProvider,
  ConnectionLineType,
  MarkerType,
  OnConnectStart,
  OnConnectEnd,
} from 'reactflow'
import 'reactflow/dist/style.css'
import {
  Trash2, Zap, ChevronDown, ChevronRight, Server, Activity, Plus, PlusCircle, X,
  Cpu, FileCode, Users, Link2, Unlink, Check, AlertCircle, Clock, Layout, Save, Radio,
  Edit, Lock, Unlock, Eye, EyeOff, Search
} from 'lucide-react'
import { templateApi, stateDefinitionApi, agentApi, capabilityApi, behaviorTreeLockApi } from '../../api/client'
import { useWebSocket, BehaviorTreeLockMessage, GraphSyncMessage } from '../../contexts/WebSocketContext'
import { useUserStore } from '../../stores/userStore'
import type {
  ActionGraph, StateDefinition, ActionMapping,
  AssignmentInfo, TemplateListItem,
  StartCondition, StartStateConfig, EndStateConfig, ActionOutcome, OutcomeTransition, DuringStateTarget, LifecycleState
} from '../../types'

// State-based Node Components
import StateActionNode from './nodes/StateActionNode'
import StateEventNode from './nodes/StateEventNode'
import StateTransitionNode from './nodes/StateTransitionNode'
import DeletableEdge from './edges/DeletableEdge'
import { TelemetryPanel } from '../../components/TelemetryPanel'
import { TelemetryProvider } from '../../contexts/TelemetryContext'

const nodeTypes = {
  action: StateActionNode,
  event: StateEventNode,
  transition: StateTransitionNode,
}

const edgeTypes = {
  deletable: DeletableEdge,
}

const START_NODE_ID = '__behavior_tree_start__'
const START_NODE_COLOR = '#22c55e'
const ACTION_NODE_DRAG_HANDLE_SELECTOR = '.action-node-drag-handle'

// Color palette for different action types
const ACTION_COLORS: Record<string, string> = {
  'nav2_msgs/NavigateToPose': '#fb7185',
  'control_msgs/FollowJointTrajectory': '#f97316',
  'control_msgs/GripperCommand': '#ef4444',
  'std_srvs/Trigger': '#0ea5e9',
}

// State color categories
const STATE_COLOR_OPTIONS = {
  success: { color: '#22c55e', label: 'Success', description: 'Completed, idle, ready' },
  error: { color: '#ef4444', label: 'Error', description: 'Failed, error states' },
  neutral: { color: '#6b7280', label: 'Neutral', description: 'In progress, waiting' },
} as const

type StateColorType = keyof typeof STATE_COLOR_OPTIONS

const DEFAULT_STATE_COLORS: Record<string, StateColorType> = {
  idle: 'success',
  completed: 'success',
  ready: 'success',
  done: 'success',
  error: 'error',
  failed: 'error',
  fault: 'error',
  cancelled: 'error',
}

const OUTCOME_EDGE_COLORS: Record<ActionOutcome, string> = {
  success: '#22c55e',
  failed: '#ef4444',
  aborted: '#ef4444',
  cancelled: '#6b7280',
  timeout: '#f59e0b',
  rejected: '#ef4444',
}

const getActionColor = (actionType: string): string => {
  return ACTION_COLORS[actionType] || '#f87171'
}

const HIDDEN_DISCOVERED_ACTIONS_STORAGE_KEY = 'action-graph.hidden-discovered-actions.v1'
const HIDDEN_DISCOVERED_SERVICES_STORAGE_KEY = 'action-graph.hidden-discovered-services.v1'
const SHOWN_DEFAULT_HIDDEN_DISCOVERED_SERVICES_STORAGE_KEY = 'action-graph.shown-default-hidden-discovered-services.v1'
const LAST_OPENED_TASK_STORAGE_KEY = 'action-graph.last-opened-task.v1'

type DiscoveryTab = 'visible' | 'hidden'

type PaletteItem = {
  type: string
  subtype: string
  label: string
  color: string
  actionType?: string
  server?: string
  duringState?: string
  robotCount?: number
  agentName?: string
  isAvailable?: boolean
  capabilityKind?: 'action' | 'service'
  providerNode?: string
  isLifecycleNode?: boolean
  lifecycleState?: LifecycleState
  hideKey?: string
  isHidden?: boolean
  isDefaultHidden?: boolean
  isDraggable?: boolean
}

type PaletteCategory = {
  category: string
  icon: React.ReactNode
  items: PaletteItem[]
}

const inferCapabilityKindFromActionType = (actionType?: string): 'action' | 'service' => {
  const normalizedType = (actionType || '').toLowerCase()
  return normalizedType.includes('/srv/') ? 'service' : 'action'
}

const normalizeCapabilityKind = (kind?: string, actionType?: string): 'action' | 'service' => {
  const normalizedKind = kind?.toLowerCase().trim()
  if (normalizedKind === 'service') return 'service'
  if (normalizedKind === 'action') return 'action'
  return inferCapabilityKindFromActionType(actionType)
}

const getServerLeafName = (serverName?: string): string => {
  if (!serverName) return ''
  const parts = serverName.split('/').filter(Boolean)
  return parts[parts.length - 1] || serverName
}

const normalizeLegacyNamespaceServer = (serverName?: string): string => {
  if (!serverName) return ''

  const trimmed = serverName.trim()
  if (!trimmed) return ''

  if (!trimmed.includes('{namespace}')) {
    return trimmed
  }

  const withoutToken = trimmed.replace(/\{namespace\}/g, '')
  const collapsed = withoutToken.replace(/\/{2,}/g, '/')
  return collapsed || ''
}

const formatTaskManagerName = (value?: string | null): string => {
  const raw = (value || '').trim()
  if (!raw) return ''
  return raw.replace(/^agent(?=[\s_-]|$)/i, 'Task Manager')
}

const getDefaultJobNameTemplate = (serverName?: string, fallbackLabel?: string): string => {
  const base = (getServerLeafName(serverName) || fallbackLabel || 'action').trim()
  return base ? `${base}/` : 'action/'
}

const getDiscoveredActionHideKey = (actionType: string, actionServerName: string): string => {
  return `${actionType}|${actionServerName}`
}

const getDiscoveredServiceHideKey = (serviceType: string, serviceName: string): string => {
  return `${serviceType}|${serviceName}`
}

const DEFAULT_HIDDEN_SERVICE_NAME_SUFFIXES = [
  '/change_state',
  '/get_state',
  '/get_available_states',
  '/get_available_transitions',
  '/describe_parameters',
  '/get_parameter_types',
  '/get_parameters',
  '/list_parameters',
  '/set_parameters',
  '/set_parameters_atomically',
]

const DEFAULT_HIDDEN_SERVICE_TYPE_NAMES = new Set([
  'getstate',
  'changestate',
  'getavailablestates',
  'getavailabletransitions',
  'describeparameters',
  'getparametertypes',
  'getparameters',
  'listparameters',
  'setparameters',
  'setparametersatomically',
])

const shouldDefaultHideDiscoveredService = (serviceType: string, serviceName: string): boolean => {
  const normalizedServiceName = (serviceName || '').toLowerCase()
  if (DEFAULT_HIDDEN_SERVICE_NAME_SUFFIXES.some((suffix) => normalizedServiceName.endsWith(suffix))) {
    return true
  }

  const normalizedTypeLeaf = (serviceType || '')
    .split('/')
    .pop()
    ?.toLowerCase()
    .replace(/_/g, '') || ''

  return DEFAULT_HIDDEN_SERVICE_TYPE_NAMES.has(normalizedTypeLeaf)
}

const loadStoredStringArray = (storageKey: string): string[] => {
  if (typeof window === 'undefined') return []

  try {
    const raw = window.localStorage.getItem(storageKey)
    if (!raw) return []

    const parsed = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []

    return parsed.filter((key): key is string => typeof key === 'string')
  } catch {
    return []
  }
}

const loadHiddenDiscoveredActionKeys = (): string[] => {
  return loadStoredStringArray(HIDDEN_DISCOVERED_ACTIONS_STORAGE_KEY)
}

const loadHiddenDiscoveredServiceKeys = (): string[] => {
  return loadStoredStringArray(HIDDEN_DISCOVERED_SERVICES_STORAGE_KEY)
}

const loadShownDefaultHiddenDiscoveredServiceKeys = (): string[] => {
  return loadStoredStringArray(SHOWN_DEFAULT_HIDDEN_DISCOVERED_SERVICES_STORAGE_KEY)
}

const loadLastOpenedTaskId = (): string | null => {
  if (typeof window === 'undefined') return null

  try {
    const value = window.localStorage.getItem(LAST_OPENED_TASK_STORAGE_KEY)
    if (!value) return null
    const trimmed = value.trim()
    return trimmed.length > 0 ? trimmed : null
  } catch {
    return null
  }
}

const extractApiErrorMessage = (error: any, fallback: string): string => {
  return (
    error?.response?.data?.error ||
    error?.response?.data?.detail ||
    error?.message ||
    fallback
  )
}

const matchesPaletteSearch = (item: PaletteItem, query: string): boolean => {
  if (!query) return true

  const haystacks = [
    item.label,
    item.subtype,
    item.type,
    item.actionType,
    item.server,
    item.agentName,
    item.providerNode,
    item.duringState,
    item.capabilityKind === 'service' ? 'service 서비스' : 'action 액션',
    item.isLifecycleNode ? 'lifecycle 라이프사이클' : 'non-lifecycle 비라이프사이클',
    item.lifecycleState,
  ]

  return haystacks.some((value) =>
    (value || '').toString().toLowerCase().includes(query)
  )
}

let stateColorsMap: Record<string, StateColorType> = {}

const getStateColor = (state: string): string => {
  const colorType = stateColorsMap[state] || DEFAULT_STATE_COLORS[state] || 'neutral'
  return STATE_COLOR_OPTIONS[colorType].color
}

const getStateColorType = (state: string): StateColorType => {
  return stateColorsMap[state] || DEFAULT_STATE_COLORS[state] || 'neutral'
}

const normalizeOutcome = (value?: string): ActionOutcome | undefined => {
  if (!value) return undefined
  const lower = value.toLowerCase()
  if (lower === 'success' || lower === 'succeeded') return 'success'
  if (lower === 'failed' || lower === 'failure' || lower === 'error') return 'failed'
  if (lower === 'aborted' || lower === 'abort') return 'aborted'
  if (lower === 'cancelled' || lower === 'canceled' || lower === 'cancel') return 'cancelled'
  if (lower === 'timeout' || lower === 'timed_out') return 'timeout'
  if (lower === 'rejected') return 'rejected'
  return undefined
}

const inferOutcome = (endState: EndStateConfig, index: number): ActionOutcome => {
  const normalized = normalizeOutcome(endState.outcome)
  if (normalized) return normalized

  const label = (endState.label || '').toLowerCase()
  const stateValue = (endState.state || '').toLowerCase()

  if (label.includes('timeout') || stateValue.includes('timeout')) return 'timeout'
  if (label.includes('cancel') || stateValue.includes('cancel')) return 'cancelled'
  if (label.includes('abort') || stateValue.includes('abort')) return 'aborted'
  if (label.includes('fail') || label.includes('error') || stateValue.includes('fail') || stateValue.includes('error')) return 'failed'
  if (label.includes('success') || label.includes('complete') || stateValue.includes('idle') || stateValue.includes('ready')) return 'success'

  return index === 0 ? 'success' : 'failed'
}

const normalizeDuringStateTargets = (
  targets?: DuringStateTarget[] | null,
  fallbackStates?: string[]
): DuringStateTarget[] => {
  if (targets && targets.length > 0) {
    return targets.map(target => ({
      ...target,
      target_type: target.target_type || 'self',
    }))
  }
  if (!fallbackStates || fallbackStates.length === 0) {
    return []
  }
  const fallbackState = fallbackStates.find(Boolean)
  if (!fallbackState) {
    return []
  }
  return [{ state: fallbackState, target_type: 'self' }]
}

const outcomeCategory = (outcome: ActionOutcome): 'success' | 'failure' => {
  return outcome === 'success' ? 'success' : 'failure'
}

// Generate unique node IDs to avoid collisions with existing nodes
const getNodeId = () => `node_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`

const mapStartStatesToConditions = (startStates: StartStateConfig[] = []): StartCondition[] => {
  return startStates
    .filter(state => state.state)
    .map((state) => {
      const quantifier = state.quantifier === 'every' ? 'all' : state.quantifier
      const condition: StartCondition = {
        id: state.id,
        operator: state.operator,
        quantifier,
        state: state.state,
        state_operator: '==',
        require_online: true,
      }

      if (quantifier === 'self') {
        condition.target_type = 'self'
      } else if (quantifier === 'specific') {
        if (state.agentId || state.agentType) {
          condition.target_type = 'agent'
          condition.agent_id = state.agentId || state.agentType
        } else {
          condition.target_type = 'agent'
        }
      } else if (state.agentId || state.agentType) {
        condition.target_type = 'agent'
        condition.agent_id = state.agentId || state.agentType
      } else {
        condition.target_type = 'all'
      }

      return condition
    })
}

const mapStartConditionsToStates = (conditions: StartCondition[] = []): StartStateConfig[] => {
  return conditions
    .filter(cond => cond.state)
    .map((cond, index) => {
      let rawQuantifier = cond.quantifier || (cond.target_type === 'self' ? 'self' : 'every')
      if (rawQuantifier === 'all') {
        rawQuantifier = 'every'
      }
      if (rawQuantifier !== 'self' && rawQuantifier !== 'every' && rawQuantifier !== 'any' && rawQuantifier !== 'specific') {
        rawQuantifier = 'self'
      }
      const quantifier = rawQuantifier as StartStateConfig['quantifier']

      const state: StartStateConfig = {
        id: cond.id || `start-${index}`,
        quantifier,
        state: cond.state || '',
        operator: index > 0 ? cond.operator : undefined,
      }

      if (quantifier === 'specific') {
        if (cond.agent_id) {
          state.agentId = cond.agent_id
        }
      } else if (quantifier === 'every' || quantifier === 'any') {
        state.agentId = cond.agent_id
      }

      return state
    })
}

const categorizeEndStates = (endStates: EndStateConfig[] = []) => {
  const successStates: string[] = []
  const failureStates: string[] = []
  const outcomes = new Map<string, ActionOutcome>()

  endStates.forEach((endState, index) => {
    const outcome = inferOutcome(endState, index)
    outcomes.set(endState.id, outcome)
    if (outcomeCategory(outcome) === 'success') {
      successStates.push(endState.state)
    } else {
      failureStates.push(endState.state)
    }
  })

  return {
    successStates: Array.from(new Set(successStates.filter(Boolean))),
    failureStates: Array.from(new Set(failureStates.filter(Boolean))),
    outcomes,
  }
}

const buildEndStates = (successStates: string[], failureStates: string[], defaultSuccess: string, defaultFailure: string): EndStateConfig[] => {
  const endStates: EndStateConfig[] = []
  const successList = successStates.length > 0 ? successStates : [defaultSuccess]
  const failureList = failureStates.length > 0 ? failureStates : [defaultFailure]

  successList.forEach((state, index) => {
    endStates.push({
      id: `end-success-${index}`,
      state,
      label: index === 0 ? 'Success' : `Success ${index + 1}`,
      outcome: 'success',
    })
  })
  failureList.forEach((state, index) => {
    endStates.push({
      id: `end-failure-${index}`,
      state,
      label: index === 0 ? 'Failure' : `Failure ${index + 1}`,
      outcome: 'failed',
    })
  })

  return endStates
}

const buildEndStatesFromOutcomes = (
  outcomes: OutcomeTransition[] = [],
  defaultSuccess: string,
  defaultFailure: string
): EndStateConfig[] => {
  if (outcomes.length === 0) {
    return []
  }

  return outcomes.map((outcome, index) => {
    const normalized = normalizeOutcome(outcome.outcome) || 'failed'
    return {
      id: `end-outcome-${index}`,
      state: outcome.state || (normalized === 'success' ? defaultSuccess : defaultFailure),
      label: outcome.outcome.charAt(0).toUpperCase() + outcome.outcome.slice(1),
      outcome: normalized,
    }
  })
}

// Deployment status badge component
function DeploymentBadge({ status }: { status: string }) {
  const config = {
    deployed: { color: 'bg-green-500/20 text-green-400 border-green-500/30', icon: Check },
    pending: { color: 'bg-yellow-500/20 text-yellow-400 border-yellow-500/30', icon: Clock },
    outdated: { color: 'bg-orange-500/20 text-orange-400 border-orange-500/30', icon: AlertCircle },
    failed: { color: 'bg-red-500/20 text-red-400 border-red-500/30', icon: AlertCircle },
    deploying: { color: 'bg-blue-500/20 text-blue-400 border-blue-500/30', icon: Clock },
  }[status] || { color: 'bg-gray-500/20 text-secondary border-gray-500/30', icon: Clock }

  const Icon = config.icon
  return (
    <span className={`inline-flex items-center gap-1 px-1.5 py-0.5 text-[9px] rounded border ${config.color}`}>
      <Icon size={10} />
      {status}
    </span>
  )
}

function ActionGraphEditor() {
  // View state
  // Bottom panel state (replaces right side panel)
  const [bottomPanelTab, setBottomPanelTab] = useState<'telemetry' | 'assignments' | null>(null)

  // Agent filtering for capabilities
  const [selectedAgentFilter, setSelectedAgentFilter] = useState<string | null>(null)
  const [selectedTemplateId, setSelectedTemplateId] = useState<string | null>(() => loadLastOpenedTaskId())

  // Modal state
  const [showCreateModal, setShowCreateModal] = useState(false)
  const [showAssignModal, setShowAssignModal] = useState(false)
  const [showAddStateModal, setShowAddStateModal] = useState(false)

  const [expandedCategories, setExpandedCategories] = useState<string[]>([
    'Discovered Actions',
    'Discovered Services',
    'Configured Actions',
    'End Nodes',
  ])
  const [nodeSearchQuery, setNodeSearchQuery] = useState('')
  const [discoveredActionTab, setDiscoveredActionTab] = useState<DiscoveryTab>('visible')
  const [discoveredServiceTab, setDiscoveredServiceTab] = useState<DiscoveryTab>('visible')
  const [discoveredActionLifecycleOnly, setDiscoveredActionLifecycleOnly] = useState(false)
  const [discoveredServiceLifecycleOnly, setDiscoveredServiceLifecycleOnly] = useState(false)
  const [hiddenDiscoveredActionKeys, setHiddenDiscoveredActionKeys] = useState<string[]>(() => loadHiddenDiscoveredActionKeys())
  const [hiddenDiscoveredServiceKeys, setHiddenDiscoveredServiceKeys] = useState<string[]>(() => loadHiddenDiscoveredServiceKeys())
  const [shownDefaultHiddenDiscoveredServiceKeys, setShownDefaultHiddenDiscoveredServiceKeys] = useState<string[]>(() => loadShownDefaultHiddenDiscoveredServiceKeys())


  // Validation state
  const [validationErrors, setValidationErrors] = useState<Array<{ nodeId: string; nodeName: string; errors: string[] }>>([])

  // Save state
  const [saveStatus, setSaveStatus] = useState<'idle' | 'saving' | 'saved' | 'error'>('idle')
  const lastSavedStateRef = useRef<{ nodes: string; edges: string } | null>(null)
  const loadedTemplateIdRef = useRef<string | null>(null)  // Track which template's data is currently loaded on canvas

  // Edit lock state (persistent session from user store)
  const { username, sessionId: storeSessionId } = useUserStore()
  const sessionId = storeSessionId || ''
  const [isEditing, setIsEditing] = useState(false)
  const [lockStatus, setLockStatus] = useState<{
    isLocked: boolean
    lockedBy: string | null
    expiresAt: number | null
    isOwnLock: boolean
  }>({ isLocked: false, lockedBy: null, expiresAt: null, isOwnLock: false })
  const lockHeartbeatRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // WebSocket for real-time updates
  const { subscribeLockEvents, subscribeSyncEvents } = useWebSocket()

  // ReactFlow
  const reactFlowWrapper = useRef<HTMLDivElement>(null)
  const { screenToFlowPosition, fitView } = useReactFlow()
  const queryClient = useQueryClient()

  // Fetch all agents for capability-based workflow
  const { data: agents = [] } = useQuery({
    queryKey: ['agents-list', 'template-only'],
    queryFn: () => agentApi.list({
      offlineMode: 'template_only',
    }),
  })

  const availableAgents = useMemo(
    () => agents
      .map(agent => ({ id: agent.id, name: agent.name })),
    [agents]
  )
  const sortedAgentsForFilter = useMemo(
    () => [...agents].sort((a, b) => {
      if (a.status !== b.status) return a.status === 'online' ? -1 : 1
      return (a.name || '').localeCompare(b.name || '')
    }),
    [agents]
  )

  // Fetch all discovered capabilities across the fleet
  // Fetch capabilities - filtered by agent or all
  const { data: fleetCapabilities } = useQuery({
    queryKey: ['fleet-capabilities', selectedAgentFilter],
    queryFn: async () => {
      if (selectedAgentFilter) {
        // Fetch capabilities for specific agent and transform to match listAll() format
        const agentCaps = await agentApi.getCapabilities(selectedAgentFilter)
        const actionCaps = agentCaps.capabilities.filter(
          (cap) => normalizeCapabilityKind(cap.capability_kind, cap.action_type) === 'action'
        )
        const serviceCaps = agentCaps.capabilities.filter(
          (cap) => normalizeCapabilityKind(cap.capability_kind, cap.action_type) === 'service'
        )

        return {
          action_types: [],
          action_servers: actionCaps.map((cap) => ({
            action_type: cap.action_type,
            action_server: cap.action_server,
            agent_id: agentCaps.agent_id,
            agent_name: agentCaps.agent_name,
            node_name: cap.node_name,
            is_lifecycle_node: cap.is_lifecycle_node ?? false,
            is_available: cap.is_available,
            lifecycle_state: cap.lifecycle_state || 'unknown',
            status: cap.status,
          })),
          service_servers: serviceCaps.map((cap) => ({
            service_type: cap.action_type,
            service_name: cap.action_server,
            agent_id: agentCaps.agent_id,
            agent_name: agentCaps.agent_name,
            node_name: cap.node_name,
            is_lifecycle_node: cap.is_lifecycle_node ?? false,
            is_available: cap.is_available,
            lifecycle_state: cap.lifecycle_state || 'unknown',
            status: cap.status,
          })),
          total_agents: 1,
        }
      }
      const fleetCaps = await capabilityApi.listAll()
      const rawActionServers = fleetCaps.action_servers || []
      const rawServiceServers = fleetCaps.service_servers || []
      const normalizedActionServers = rawActionServers.filter((srv: any) =>
        normalizeCapabilityKind(srv?.capability_kind, srv?.action_type) === 'action'
      )
      const inferredServiceServers = rawActionServers
        .filter((srv: any) => normalizeCapabilityKind(srv?.capability_kind, srv?.action_type) === 'service')
        .map((srv: any) => ({
          service_type: srv.action_type,
          service_name: srv.action_server,
          agent_id: srv.agent_id,
          agent_name: srv.agent_name,
          node_name: srv.node_name,
          is_lifecycle_node: srv.is_lifecycle_node ?? false,
          is_available: srv.is_available ?? false,
          lifecycle_state: srv.lifecycle_state || 'unknown',
          status: srv.status || 'unknown',
        }))

      return {
        ...fleetCaps,
        action_servers: normalizedActionServers,
        service_servers: [...rawServiceServers, ...inferredServiceServers],
      }
    },
  })

  // Always fetch all RTM capability templates (online + offline snapshot).
  const { data: allFleetCapabilities } = useQuery({
    queryKey: ['fleet-capabilities-all'],
    queryFn: async () => {
      const fleetCaps = await capabilityApi.listAll()
      const rawActionServers = fleetCaps.action_servers || []
      const rawServiceServers = fleetCaps.service_servers || []
      const normalizedActionServers = rawActionServers.filter((srv: any) =>
        normalizeCapabilityKind(srv?.capability_kind, srv?.action_type) === 'action'
      )
      const inferredServiceServers = rawActionServers
        .filter((srv: any) => normalizeCapabilityKind(srv?.capability_kind, srv?.action_type) === 'service')
        .map((srv: any) => ({
          service_type: srv.action_type,
          service_name: srv.action_server,
          agent_id: srv.agent_id,
          agent_name: srv.agent_name,
          node_name: srv.node_name,
          is_lifecycle_node: srv.is_lifecycle_node ?? false,
          is_available: srv.is_available ?? false,
          lifecycle_state: srv.lifecycle_state || 'unknown',
          status: srv.status || 'unknown',
        }))

      return {
        ...fleetCaps,
        action_servers: normalizedActionServers,
        service_servers: [...rawServiceServers, ...inferredServiceServers],
      }
    },
  })

  useEffect(() => {
    if (typeof window === 'undefined') return

    window.localStorage.setItem(
      HIDDEN_DISCOVERED_ACTIONS_STORAGE_KEY,
      JSON.stringify(hiddenDiscoveredActionKeys)
    )
  }, [hiddenDiscoveredActionKeys])

  useEffect(() => {
    if (typeof window === 'undefined') return

    window.localStorage.setItem(
      HIDDEN_DISCOVERED_SERVICES_STORAGE_KEY,
      JSON.stringify(hiddenDiscoveredServiceKeys)
    )
  }, [hiddenDiscoveredServiceKeys])

  useEffect(() => {
    if (typeof window === 'undefined') return

    window.localStorage.setItem(
      SHOWN_DEFAULT_HIDDEN_DISCOVERED_SERVICES_STORAGE_KEY,
      JSON.stringify(shownDefaultHiddenDiscoveredServiceKeys)
    )
  }, [shownDefaultHiddenDiscoveredServiceKeys])

  useEffect(() => {
    if (typeof window === 'undefined') return

    if (selectedTemplateId) {
      window.localStorage.setItem(LAST_OPENED_TASK_STORAGE_KEY, selectedTemplateId)
    } else {
      window.localStorage.removeItem(LAST_OPENED_TASK_STORAGE_KEY)
    }
  }, [selectedTemplateId])

  // Fetch all templates (capability-based - no type filtering)
  const { data: allTemplates = [], isLoading: templatesLoading } = useQuery({
    queryKey: ['templates-all'],
    queryFn: () => templateApi.list(),
  })

  useEffect(() => {
    if (templatesLoading) return
    if (allTemplates.length === 0) {
      if (selectedTemplateId !== null) {
        setSelectedTemplateId(null)
      }
      return
    }

    const isCurrentSelectionValid = !!selectedTemplateId &&
      allTemplates.some(template => template.id === selectedTemplateId)

    if (!isCurrentSelectionValid) {
      setSelectedTemplateId(allTemplates[0].id)
    }
  }, [templatesLoading, allTemplates, selectedTemplateId])


  // Fetch selected template
  const {
    data: selectedTemplate,
    isLoading: selectedTemplateLoading,
    isError: selectedTemplateError,
    error: selectedTemplateQueryError,
    refetch: refetchSelectedTemplate,
  } = useQuery({
    queryKey: ['template', selectedTemplateId],
    queryFn: () => templateApi.get(selectedTemplateId!),
    enabled: !!selectedTemplateId,
  })

  // Debug: Log modal state changes
  useEffect(() => {
    console.log('[ActionGraphEditor] showAssignModal changed to:', showAssignModal)
    console.log('[ActionGraphEditor] selectedTemplate:', selectedTemplate?.id, selectedTemplate?.name)
    console.log('[ActionGraphEditor] selectedTemplateId:', selectedTemplateId)
  }, [showAssignModal, selectedTemplate, selectedTemplateId])

  // Fetch first state definition (for states reference - legacy support)
  const { data: stateDefinitions = [], refetch: refetchStateDefinitions } = useQuery({
    queryKey: ['state-definitions'],
    queryFn: () => stateDefinitionApi.list(),
  })
  const selectedStateDef = stateDefinitions.length > 0 ? stateDefinitions[0] : undefined
  const refetchStateDef = () => {
    refetchStateDefinitions()
  }

  // Fetch assignments for selected template
  const { data: templateAssignments = [] } = useQuery({
    queryKey: ['template-assignments', selectedTemplateId],
    queryFn: () => templateApi.getAssignments(selectedTemplateId!),
    enabled: !!selectedTemplateId,
  })

  // Mutations
  const deleteTemplate = useMutation({
    mutationFn: (id: string) => templateApi.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['templates-all'] })
      setSelectedTemplateId(null)
    },
    onError: (error: any) => {
      alert(extractApiErrorMessage(error, '태스크 삭제에 실패했습니다'))
    },
  })

  const assignTemplate = useMutation({
    mutationFn: ({ templateId, agentId }: { templateId: string; agentId: string }) =>
      templateApi.assign(templateId, agentId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['template-assignments'] })
      queryClient.invalidateQueries({ queryKey: ['agents-overview'] })
    },
  })

  const unassignTemplate = useMutation({
    mutationFn: ({ templateId, agentId }: { templateId: string; agentId: string }) =>
      templateApi.unassign(templateId, agentId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['template-assignments'] })
      queryClient.invalidateQueries({ queryKey: ['agents-overview'] })
    },
  })

  // Save template mutation
  const saveTemplate = useMutation({
    mutationFn: async ({
      templateId,
      steps,
      entryPoint,
      states,
      lockSessionId,
    }: {
      templateId: string
      steps: ActionGraph['steps']
      entryPoint?: string
      states?: ActionGraph['states']
      lockSessionId?: string
    }) => {
      const payload: Partial<ActionGraph> = { steps }
      if (entryPoint) {
        payload.entry_point = entryPoint
      }
      if (states && states.length > 0) {
        payload.states = states
      }
      return templateApi.update(templateId, payload, lockSessionId)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['template', selectedTemplateId] })
      queryClient.invalidateQueries({ queryKey: ['templates-all'] })
      setSaveStatus('saved')
    },
    onError: () => {
      setSaveStatus('error')
      // Reset to idle after 3 seconds
      setTimeout(() => setSaveStatus('idle'), 3000)
    },
  })

  // Available states from state definition (with default fallback)
  // Memoized to prevent infinite re-render loops
  const availableStates = useMemo(() => {
    const DEFAULT_STATES = ['idle', 'busy', 'error', 'completed', 'waiting']
    return selectedStateDef?.states?.length ? selectedStateDef.states : DEFAULT_STATES
  }, [selectedStateDef?.states])

  const hiddenDiscoveredActionKeySet = useMemo(
    () => new Set(hiddenDiscoveredActionKeys),
    [hiddenDiscoveredActionKeys]
  )
  const hiddenDiscoveredServiceKeySet = useMemo(
    () => new Set(hiddenDiscoveredServiceKeys),
    [hiddenDiscoveredServiceKeys]
  )
  const shownDefaultHiddenDiscoveredServiceKeySet = useMemo(
    () => new Set(shownDefaultHiddenDiscoveredServiceKeys),
    [shownDefaultHiddenDiscoveredServiceKeys]
  )

  // Build node palette
  const nodePalette = useMemo(() => {
    const palette: PaletteCategory[] = []

    // Use discovered action servers from the fleet and keep full server paths.
    if (fleetCapabilities && fleetCapabilities.action_servers && fleetCapabilities.action_servers.length > 0) {
      // Deduplicate by action type + full action server path.
      const serverMap = new Map<string, typeof fleetCapabilities.action_servers[0]>()
      for (const srv of fleetCapabilities.action_servers) {
        const dedupeKey = `${srv.agent_id}|${srv.action_type}|${srv.action_server}`
        const existing = serverMap.get(dedupeKey)
        if (!existing || (!existing.is_available && srv.is_available)) {
          serverMap.set(dedupeKey, srv)
        }
      }

      palette.push({
        category: 'Discovered Actions',
        icon: <Server className="w-3.5 h-3.5" />,
        items: Array.from(serverMap.values()).map((srv) => {
          const actionServerName = getServerLeafName(srv.action_server)
          const hideKey = getDiscoveredActionHideKey(srv.action_type, srv.action_server)

          return {
            type: 'action',
            subtype: srv.action_server, // Use full action_server path as subtype (unique identifier)
            label: actionServerName || srv.action_server, // Display action server name
            color: getActionColor(srv.action_type),
            actionType: srv.action_type,
            server: srv.action_server,
            agentName: srv.agent_name,
            isAvailable: srv.is_available,
            capabilityKind: 'action',
            providerNode: srv.node_name,
            isLifecycleNode: srv.is_lifecycle_node,
            lifecycleState: srv.lifecycle_state,
            hideKey,
            isHidden: hiddenDiscoveredActionKeySet.has(hideKey),
            isDraggable: true,
          }
        }),
      })
    }

    // Discovered service servers
    if (fleetCapabilities?.service_servers && fleetCapabilities.service_servers.length > 0) {
      const serviceMap = new Map<string, typeof fleetCapabilities.service_servers[0]>()
      for (const srv of fleetCapabilities.service_servers) {
        const dedupeKey = `${srv.agent_id}|${srv.service_type}|${srv.service_name}`
        const existing = serviceMap.get(dedupeKey)
        if (!existing || (!existing.is_available && srv.is_available)) {
          serviceMap.set(dedupeKey, srv)
        }
      }

      palette.push({
        category: 'Discovered Services',
        icon: <Cpu className="w-3.5 h-3.5" />,
        items: Array.from(serviceMap.values()).map((srv) => {
          const serviceServerName = getServerLeafName(srv.service_name)
          const hideKey = getDiscoveredServiceHideKey(srv.service_type, srv.service_name)
          const isDefaultHidden = shouldDefaultHideDiscoveredService(srv.service_type, srv.service_name)
          const isExplicitShown = shownDefaultHiddenDiscoveredServiceKeySet.has(hideKey)
          const isHidden = isExplicitShown
            ? false
            : (hiddenDiscoveredServiceKeySet.has(hideKey) || isDefaultHidden)

          return {
            type: 'service',
            subtype: srv.service_name,
            label: serviceServerName || srv.service_name,
            color: '#0ea5e9',
            actionType: srv.service_type,
            server: srv.service_name,
            agentName: srv.agent_name,
            isAvailable: srv.is_available,
            capabilityKind: 'service',
            providerNode: srv.node_name,
            isLifecycleNode: srv.is_lifecycle_node,
            lifecycleState: srv.lifecycle_state,
            hideKey,
            isHidden,
            isDefaultHidden,
            isDraggable: true,
          }
        }),
      })
    }

    // Also include state definition action mappings if available (legacy support)
    if (selectedStateDef?.action_mappings && selectedStateDef.action_mappings.length > 0) {
      palette.push({
        category: 'Configured Actions',
        icon: <Server className="w-3.5 h-3.5" />,
        items: selectedStateDef.action_mappings.map((mapping: ActionMapping) => ({
          type: 'action',
          subtype: mapping.server,
          label: mapping.server.replace(/^\//, ''),
          color: getActionColor(mapping.action_type),
          actionType: mapping.action_type,
          server: mapping.server,
          duringState: mapping.during_states?.[0] || mapping.during_state,
          capabilityKind: 'action',
          isDraggable: true,
        })),
      })
    }

    // End nodes (terminal nodes for behavior tree)
    palette.push({
      category: 'End Nodes',
      icon: <Activity className="w-3.5 h-3.5" />,
      items: [
        {
          type: 'event',
          subtype: 'End',
          label: 'End (Success)',
          color: '#22c55e',
          isDraggable: true,
        },
        {
          type: 'event',
          subtype: 'Error',
          label: 'End (Error)',
          color: '#ef4444',
          isDraggable: true,
        },
      ],
    })

    return palette
  }, [
    selectedStateDef,
    fleetCapabilities,
    hiddenDiscoveredActionKeySet,
    hiddenDiscoveredServiceKeySet,
    shownDefaultHiddenDiscoveredServiceKeySet,
  ])

  // Convert template to graph
  const { initialNodes, initialEdges } = useMemo(() => {
    if (selectedTemplate) {
      return convertActionGraphToGraph(selectedTemplate, selectedStateDef, availableStates, availableAgents)
    }
    return { initialNodes: [], initialEdges: [] }
  }, [selectedTemplate, selectedStateDef, availableStates, availableAgents])

  const [nodes, setNodes, onNodesChange] = useNodesState(initialNodes)
  const [edges, setEdges, defaultOnEdgesChange] = useEdgesState(initialEdges)

  // Track edges that user has explicitly requested to delete (via UI button)
  const pendingDeletionsRef = useRef<Set<string>>(new Set())

  // Delete edge function - called from custom edge component
  const deleteEdge = useCallback((edgeId: string) => {
    console.log('[deleteEdge] User requested deletion of edge:', edgeId)
    pendingDeletionsRef.current.add(edgeId)
    setEdges((eds) => eds.filter((e) => e.id !== edgeId))
  }, [setEdges])

  // Custom onEdgesChange that prevents automatic edge removal when nodes are re-rendering
  // ReactFlow can try to remove edges when handles temporarily unmount during re-render
  const onEdgesChange = useCallback((changes: import('reactflow').EdgeChange[]) => {
    // Filter out spurious 'remove' changes that happen due to handle re-registration
    // Allow: 1) User-initiated deletions (tracked in pendingDeletions)
    //        2) Deletions where source or target node no longer exists
    //        3) Selection changes, position changes, etc.
    const filteredChanges = changes.filter(change => {
      if (change.type === 'remove') {
        // Check if this was a user-initiated deletion
        if (pendingDeletionsRef.current.has(change.id)) {
          pendingDeletionsRef.current.delete(change.id)
          return true // Allow user-initiated deletion
        }
        // Check if the source and target nodes still exist
        const edge = edges.find(e => e.id === change.id)
        if (edge) {
          const sourceExists = nodes.some(n => n.id === edge.source)
          const targetExists = nodes.some(n => n.id === edge.target)
          if (sourceExists && targetExists) {
            // Both nodes exist - this is likely spurious removal from re-render
            console.log('[onEdgesChange] Blocking spurious edge removal:', change.id)
            return false
          }
        }
        // One of the nodes was deleted, allow edge removal
        return true
      }
      return true
    })

    if (filteredChanges.length > 0) {
      defaultOnEdgesChange(filteredChanges)
    }
  }, [defaultOnEdgesChange, edges, nodes])

  // Wrap edges with onDelete callback and set type to 'deletable'
  const edgesWithDelete = useMemo(() => {
    return edges.map(edge => ({
      ...edge,
      type: 'deletable',
      data: {
        ...edge.data,
        onDelete: deleteEdge,
      },
    }))
  }, [edges, deleteEdge])

  const requiredCapabilityKeys = useMemo(() => {
    const required = new Set<string>()
    for (const node of nodes) {
      if (node.type !== 'action') continue
      const actionType = (node.data.actionType || node.data.subtype || '').trim()
      if (!actionType) continue
      const capabilityKind = normalizeCapabilityKind(node.data.capabilityKind, actionType)
      required.add(`${capabilityKind}:${actionType}`)
    }
    return Array.from(required).sort()
  }, [nodes])

  const capabilityTemplateByAgent = useMemo(() => {
    const byAgent = new Map<string, Set<string>>()
    const ensureAgent = (agentID: string) => {
      if (!byAgent.has(agentID)) byAgent.set(agentID, new Set<string>())
      return byAgent.get(agentID)!
    }

    for (const srv of allFleetCapabilities?.action_servers || []) {
      if (!srv?.agent_id || !srv?.action_type) continue
      ensureAgent(srv.agent_id).add(`action:${srv.action_type}`)
    }
    for (const srv of allFleetCapabilities?.service_servers || []) {
      if (!srv?.agent_id || !srv?.service_type) continue
      ensureAgent(srv.agent_id).add(`service:${srv.service_type}`)
    }

    return byAgent
  }, [allFleetCapabilities])

  const compatibleRtmTemplates = useMemo(() => {
    const templates = agents
      .filter((agent) => capabilityTemplateByAgent.has(agent.id) || agent.has_capability_template)
      .map((agent) => {
        const providedCapabilities = capabilityTemplateByAgent.get(agent.id) || new Set<string>()
        const missing = requiredCapabilityKeys.filter((required) => !providedCapabilities.has(required))
        return {
          id: agent.id,
          name: formatTaskManagerName(agent.name) || formatTaskManagerName(agent.id) || agent.id,
          status: agent.status,
          hasAllCapabilities: missing.length === 0,
          missingCount: missing.length,
          totalCapabilities: providedCapabilities.size,
        }
      })
      .sort((a, b) => {
        if (a.hasAllCapabilities !== b.hasAllCapabilities) return a.hasAllCapabilities ? -1 : 1
        if (a.status !== b.status) return a.status === 'online' ? -1 : 1
        return a.name.localeCompare(b.name)
      })

    return templates
  }, [agents, capabilityTemplateByAgent, requiredCapabilityKeys])

  const compatibleRtmTemplateCount = useMemo(
    () => compatibleRtmTemplates.filter((agent) => agent.hasAllCapabilities).length,
    [compatibleRtmTemplates]
  )

  const deleteSelectedElements = useCallback(() => {
    if (!isEditing) return

    const selectedNodeIds = new Set(
      nodes
        .filter(node => node.selected && node.id !== START_NODE_ID)
        .map(node => node.id)
    )
    const selectedEdgeIds = new Set(
      edges
        .filter(edge => edge.selected)
        .map(edge => edge.id)
    )

    if (selectedNodeIds.size === 0 && selectedEdgeIds.size === 0) {
      return
    }

    setNodes((nds) => nds.filter((node) => !selectedNodeIds.has(node.id)))
    setEdges((eds) => eds.filter((edge) => {
      if (selectedEdgeIds.has(edge.id)) return false
      if (selectedNodeIds.has(edge.source) || selectedNodeIds.has(edge.target)) return false
      return true
    }))
  }, [edges, isEditing, nodes, setEdges, setNodes])

  // Allow Del/Backspace node deletion from keyboard.
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (!isEditing) return
      if (event.key !== 'Delete' && event.key !== 'Backspace') return

      const target = event.target as HTMLElement | null
      if (target) {
        const tag = target.tagName
        const isTextInput = tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || target.isContentEditable
        if (isTextInput) return
      }

      const hasSelection = nodes.some((node) => node.selected && node.id !== START_NODE_ID) || edges.some((edge) => edge.selected)
      if (!hasSelection) return

      event.preventDefault()
      deleteSelectedElements()
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [deleteSelectedElements, edges, isEditing, nodes])

  // Track the selected template ID independently from query loading state.
  // Using selectedTemplate?.id here can transiently become undefined during refetch/error
  // and inadvertently clear the canvas.
  const templateId = selectedTemplateId

  // Keep editability flag inside node data for node-local controls (e.g. delete button).
  useEffect(() => {
    setNodes((nds) => nds.map((node) => {
      if ((node.data as { isEditing?: boolean })?.isEditing === isEditing) {
        return node
      }
      return {
        ...node,
        data: {
          ...node.data,
          isEditing,
        },
      }
    }))
  }, [isEditing, setNodes])

  // Limit action node dragging to explicit drag-handle areas only.
  useEffect(() => {
    setNodes((nds) => {
      let changed = false
      const next = nds.map((node) => {
        if (node.type !== 'action') return node
        if (node.dragHandle === ACTION_NODE_DRAG_HANDLE_SELECTOR) return node
        changed = true
        return {
          ...node,
          dragHandle: ACTION_NODE_DRAG_HANDLE_SELECTOR,
        }
      })
      return changed ? next : nds
    })
  }, [setNodes])

  // Clear canvas immediately when switching to a different template
  // This prevents showing stale data from the previous template
  useEffect(() => {
    if (templateId !== loadedTemplateIdRef.current && loadedTemplateIdRef.current !== null) {
      // Template ID changed - clear canvas to prevent showing old data
      console.log('[Template] Switching from', loadedTemplateIdRef.current, 'to', templateId, '- clearing canvas')
      setNodes([])
      setEdges([])
      setSaveStatus('idle')
      lastSavedStateRef.current = null
    }
  }, [templateId, setNodes, setEdges])

  // Load template from server when template data is ready
  useEffect(() => {
    // IMPORTANT: Verify selectedTemplate.id matches selected template ID.
    if (selectedTemplate && templateId && selectedTemplate.id === templateId) {
      // Skip if we already loaded this template
      if (loadedTemplateIdRef.current === templateId) {
        return
      }

      // Load from server
      console.log('[Template] Loading template data:', templateId, 'steps:', selectedTemplate.steps?.length || 0)
      const { initialNodes, initialEdges } = convertActionGraphToGraph(selectedTemplate, selectedStateDef, availableStates, availableAgents)
      setNodes(initialNodes)
      setEdges(initialEdges)

      // Always reset viewport when opening a task so the full flow is visible.
      // Double RAF ensures ReactFlow store has applied new nodes/edges before fitting.
      requestAnimationFrame(() => {
        requestAnimationFrame(() => {
          fitView({
            padding: 0.2,
            duration: 250,
            maxZoom: 1.2,
          })
        })
      })

      loadedTemplateIdRef.current = templateId
      lastSavedStateRef.current = {
        nodes: JSON.stringify(initialNodes),
        edges: JSON.stringify(initialEdges),
      }
      setSaveStatus('idle')
    }
  }, [templateId, selectedTemplate, setNodes, setEdges, selectedStateDef, availableStates, availableAgents, fitView])

  // Track if there are unsaved changes (for UI indicator)
  const hasUnsavedChanges = useMemo(() => {
    if (!lastSavedStateRef.current || nodes.length === 0) return false
    const currentState = {
      nodes: JSON.stringify(nodes),
      edges: JSON.stringify(edges),
    }
    return currentState.nodes !== lastSavedStateRef.current.nodes ||
           currentState.edges !== lastSavedStateRef.current.edges
  }, [nodes, edges])

  // Clear validation errors when template changes
  useEffect(() => {
    setValidationErrors([])
  }, [templateId])

  // Track last applied states to prevent infinite loops
  const lastAppliedCombinedStatesRef = useRef<string>('')
  const lastAppliedAgentsRef = useRef<Array<{ id: string; name: string }>>([])

  // Collect all generated state codes from all nodes
  const allGeneratedStateCodes = useMemo(() => {
    const codes: string[] = []
    nodes.forEach((node) => {
      if (node.data.autoGenerateStates && node.data.generatedStates?.length > 0) {
        node.data.generatedStates.forEach((gs: { code: string }) => {
          if (gs.code && !codes.includes(gs.code)) {
            codes.push(gs.code)
          }
        })
      }
    })
    return codes.sort() // Sort for consistent comparison
  }, [nodes])

  // Combine base states with all generated states from all nodes
  const combinedAvailableStates = useMemo(() => {
    const combined = [...availableStates]
    allGeneratedStateCodes.forEach((code) => {
      if (!combined.includes(code)) {
        combined.push(code)
      }
    })
    return combined
  }, [availableStates, allGeneratedStateCodes])

  // Update existing nodes' availableStates when state definition loads OR when generated states change
  useEffect(() => {
    // Only update if states actually changed (by value, not reference)
    const combinedKey = JSON.stringify(combinedAvailableStates.slice().sort())
    const statesChanged = combinedKey !== lastAppliedCombinedStatesRef.current

    if (combinedAvailableStates.length > 0 && statesChanged) {
      lastAppliedCombinedStatesRef.current = combinedKey
      setNodes((nds) =>
        nds.map((node) => ({
          ...node,
          data: { ...node.data, availableStates: combinedAvailableStates },
        }))
      )
    }
  }, [combinedAvailableStates, setNodes])

  useEffect(() => {
    const agentsChanged = JSON.stringify(availableAgents) !== JSON.stringify(lastAppliedAgentsRef.current)
    if (agentsChanged) {
      lastAppliedAgentsRef.current = availableAgents
      setNodes((nds) =>
        nds.map((node) => ({
          ...node,
          data: { ...node.data, availableAgents },
        }))
      )
    }
  }, [availableAgents, setNodes])

  // Convert ReactFlow nodes/edges back to ActionGraph steps
  const convertGraphToSteps = useCallback((): {
    steps: ActionGraph['steps']
    entryPoint?: string
    generatedStates?: ActionGraph['states']
  } => {
    const steps: ActionGraph['steps'] = []
    const allGeneratedStates: NonNullable<ActionGraph['states']> = []

    nodes.forEach((node) => {
      // Collect auto-generated states from nodes
      if (node.data.autoGenerateStates && node.data.generatedStates?.length > 0) {
        allGeneratedStates.push(...node.data.generatedStates)
      }
      if (node.type === 'event') {
        // Event nodes (Start/End) are terminal steps
        if (node.data.subtype === 'End' || node.data.subtype === 'Error') {
          steps.push({
            id: node.id,
            name: node.data.label,
            type: 'terminal',
            terminal_type: node.data.subtype === 'Error' ? 'failure' : 'success',
            ui: {
              x: node.position.x,
              y: node.position.y,
            },
          })
        }
        // Start nodes don't need to be saved as steps
        return
      }

      const endStates: EndStateConfig[] = node.data.endStates || []
      const { successStates, failureStates, outcomes } = categorizeEndStates(endStates)
      const resolvedSuccessStates = successStates.length > 0 ? successStates : (node.data.successStates || [])
      const resolvedFailureStates = failureStates.length > 0 ? failureStates : (node.data.failureStates || [])

      const outgoingEdges = edges.filter(e => e.source === node.id)
      const outcomeTransitions: OutcomeTransition[] = []
      endStates.forEach((endState, index) => {
        const edge = outgoingEdges.find(e => e.sourceHandle === endState.id)
        if (!edge) return
        const normalizedOutcome = normalizeOutcome(endState.outcome) || outcomes.get(endState.id) || inferOutcome(endState, index)
        outcomeTransitions.push({
          outcome: normalizedOutcome,
          next: edge.target,
          state: endState.state,
        })
      })

      const fallbackSuccess = outcomeTransitions.find(t => t.outcome === 'success')
      const fallbackFailure = outcomeTransitions.find(t => t.outcome !== 'success')

      const rawPreStates = node.data.preStates || []
      const preStates = Array.isArray(rawPreStates)
        ? rawPreStates
          .map((state: any) => (typeof state === 'string' ? state : state?.state))
          .filter(Boolean)
        : []

      const normalizedDuringTargets = normalizeDuringStateTargets(
        node.data.duringStateTargets,
        node.data.duringStates
      )
      const normalizedNodeServer = normalizeLegacyNamespaceServer(node.data.server)
      const selfDuringTarget = normalizedDuringTargets.find(target =>
        (!target.target_type || target.target_type === 'self' || target.target_type === 'all') &&
        target.state
      )
      const selfDuringStates = selfDuringTarget?.state ? [selfDuringTarget.state] : []

      const step: ActionGraph['steps'][0] = {
        id: node.id,
        name: node.data.label,
        ui: {
          x: node.position.x,
          y: node.position.y,
        },
        // Job name for this step (user-defined name)
        job_name: node.data.jobName || undefined,
        auto_generate_states: node.data.autoGenerateStates || undefined,
        // Regular action steps don't need type set (only 'fallback' or 'terminal' use type)
        action: {
          type: node.data.actionType || node.data.subtype,
          server: normalizedNodeServer || node.data.server,
          capability_kind: normalizeCapabilityKind(
            node.data.capabilityKind,
            node.data.actionType || node.data.subtype
          ),
          params: node.data.fieldSources && Object.keys(node.data.fieldSources).length > 0
            ? {
                source: 'mapped' as const,
                data: node.data.params || {},
                field_sources: node.data.fieldSources,
              }
            : {
                source: 'inline' as const,
                data: node.data.params || {},
              },
        },
        start_conditions: mapStartStatesToConditions(node.data.startStates || []),
        pre_states: preStates,
        during_states: selfDuringStates,
        during_state_targets: normalizedDuringTargets.length > 0 ? normalizedDuringTargets : undefined,
        end_states: endStates,
        success_states: resolvedSuccessStates,
        failure_states: resolvedFailureStates,
        transition: {
          on_success: fallbackSuccess?.next,
          on_failure: fallbackFailure?.next,
          on_outcomes: outcomeTransitions.length > 0 ? outcomeTransitions : undefined,
        },
      }

      // Debug: Log transitions being saved
      console.log(`[SAVE] Step ${node.id} (${node.data.label}): on_success=${fallbackSuccess?.next}, on_failure=${fallbackFailure?.next}, outcomeTransitions=`, outcomeTransitions)

      // Add preconditions if any
      if (node.data.preconditions && node.data.preconditions.length > 0) {
        step.preconditions = node.data.preconditions
      }

      steps.push(step)
    })

    const stepIds = new Set(steps.map(step => step.id))
    const startEdge = edges.find(edge => edge.source === START_NODE_ID)
    let entryPoint: string | undefined
    if (startEdge?.target && stepIds.has(startEdge.target)) {
      entryPoint = startEdge.target
    } else if (steps.length > 0) {
      entryPoint = steps[0].id
    }

    return {
      steps,
      entryPoint,
      generatedStates: allGeneratedStates.length > 0 ? allGeneratedStates : undefined,
    }
  }, [nodes, edges])

  // Validate graph before saving
  const validateGraph = useCallback((): Array<{ nodeId: string; nodeName: string; errors: string[] }> => {
    const errors: Array<{ nodeId: string; nodeName: string; errors: string[] }> = []

    // Find action nodes (not event nodes like Start/End)
    const actionNodes = nodes.filter(node => node.type === 'action')

    // Check if there's a start node connection
    const startEdge = edges.find(e => e.source === START_NODE_ID)
    if (!startEdge) {
      errors.push({
        nodeId: START_NODE_ID,
        nodeName: 'Start',
        errors: ['시작 노드가 다른 노드에 연결되어 있지 않습니다']
      })
    }

    // Check if there are any action nodes
    if (actionNodes.length === 0) {
      errors.push({
        nodeId: '__graph__',
        nodeName: 'Graph',
        errors: ['그래프에 Action 노드가 없습니다']
      })
    }

    // Validate each action node
    actionNodes.forEach(node => {
      const nodeErrors: string[] = []

      // Check for action type (required)
      if (!node.data.actionType && !node.data.subtype) {
        nodeErrors.push('Action Type이 설정되지 않았습니다')
      }

      // Check for action server (required)
      if (!node.data.server) {
        nodeErrors.push('Action Server가 설정되지 않았습니다')
      }

      // Check for outgoing edges (at least one connection)
      const outgoingEdges = edges.filter(e => e.source === node.id)
      if (outgoingEdges.length === 0) {
        nodeErrors.push('다음 노드로의 연결(Edge)이 없습니다')
      }

      if (nodeErrors.length > 0) {
        errors.push({
          nodeId: node.id,
          nodeName: node.data.label || node.id,
          errors: nodeErrors
        })
      }
    })

    return errors
  }, [nodes, edges])

  // Handle save with validation
  const handleSave = useCallback(() => {
    if (!selectedTemplateId) return

    // Validate graph
    const errors = validateGraph()
    setValidationErrors(errors)

    if (errors.length > 0) {
      // Show warning but allow save
      const hasBlockingErrors = errors.some(e =>
        e.errors.some(err =>
          err.includes('Action Type') || err.includes('Action Server')
        )
      )

      if (hasBlockingErrors) {
        // Block save for missing required fields
        alert(`저장 불가: 필수 필드가 누락되었습니다.\n\n${errors.map(e => `• ${e.nodeName}: ${e.errors.join(', ')}`).join('\n')}`)
        return
      }

      // Warn but allow for non-blocking errors (like missing edges)
      const proceed = window.confirm(
        `경고: 일부 문제가 발견되었습니다.\n\n${errors.map(e => `• ${e.nodeName}: ${e.errors.join(', ')}`).join('\n')}\n\n그래도 저장하시겠습니까?`
      )
      if (!proceed) return
    }

    setSaveStatus('saving')

    // Capture current state before save
    const stateToSave = {
      nodes: JSON.stringify(nodes),
      edges: JSON.stringify(edges),
    }

    const { steps, entryPoint, generatedStates } = convertGraphToSteps()
    saveTemplate.mutate({ templateId: selectedTemplateId, steps, entryPoint, states: generatedStates, lockSessionId: sessionId }, {
      onSuccess: () => {
        // Update lastSavedStateRef only after successful save
        lastSavedStateRef.current = stateToSave
      }
    })
  }, [selectedTemplateId, convertGraphToSteps, saveTemplate, validateGraph, nodes, edges, sessionId])

  // Lock management functions
  const acquireLock = useCallback(async () => {
    if (!selectedTemplateId) return false

    try {
      const result = await behaviorTreeLockApi.acquire(selectedTemplateId, sessionId, username || 'User')
      if (result.success) {
        setIsEditing(true)
        setLockStatus({
          isLocked: true,
          lockedBy: result.locked_by,
          expiresAt: result.expires_at,
          isOwnLock: true,
        })

        // Start heartbeat to keep lock alive (every 2 minutes)
        if (lockHeartbeatRef.current) {
          clearInterval(lockHeartbeatRef.current)
        }
        lockHeartbeatRef.current = setInterval(async () => {
          if (!selectedTemplateId) return
          const heartbeatResult = await behaviorTreeLockApi.heartbeat(selectedTemplateId, sessionId)
          if (!heartbeatResult.success) {
            // Lock was lost
            console.warn('[Lock] Lock lost:', heartbeatResult.error)
            setIsEditing(false)
            setLockStatus({ isLocked: false, lockedBy: null, expiresAt: null, isOwnLock: false })
            if (lockHeartbeatRef.current) {
              clearInterval(lockHeartbeatRef.current)
              lockHeartbeatRef.current = null
            }
          } else if (heartbeatResult.expires_at) {
            setLockStatus(prev => ({ ...prev, expiresAt: heartbeatResult.expires_at! }))
          }
        }, 2 * 60 * 1000) // 2 minutes

        return true
      } else {
        // Check error type
        if (result.error === 'executing') {
          // Graph is being executed by agents
          const agents = (result as any).executing_agents || []
          alert(`이 태스크가 현재 실행 중입니다.\n\n실행 중인 RTM: ${agents.join(', ')}\n\nTask가 완료된 후 다시 시도해주세요.`)
          return false
        }
        // Lock is held by someone else
        setLockStatus({
          isLocked: true,
          lockedBy: result.locked_by || 'Another user',
          expiresAt: result.expires_at || null,
          isOwnLock: false,
        })
        alert(`This graph is currently being edited by ${result.locked_by || 'another user'}.`)
        return false
      }
    } catch (error) {
      console.error('[Lock] Failed to acquire lock:', error)
      return false
    }
  }, [selectedTemplateId, sessionId, username])

  // Force unlock - override another user's lock
  const forceUnlock = useCallback(async () => {
    if (!selectedTemplateId) return
    const proceed = window.confirm(
      `${lockStatus.lockedBy}님이 현재 편집 중입니다.\n강제 해제하면 상대방의 편집 세션이 중단됩니다.\n\n강제 해제하시겠습니까?`
    )
    if (!proceed) return
    try {
      await behaviorTreeLockApi.forceRelease(selectedTemplateId, sessionId, username || 'User')
      setLockStatus({ isLocked: false, lockedBy: null, expiresAt: null, isOwnLock: false })
    } catch (error) {
      console.error('[Lock] Failed to force unlock:', error)
    }
  }, [selectedTemplateId, sessionId, username, lockStatus.lockedBy])

  const releaseLock = useCallback(async () => {
    if (!selectedTemplateId || !isEditing) return

    // Only auto-save on release when there are real unsaved changes.
    // This prevents accidental overwrite with empty canvas during transient data unload.
    if (hasUnsavedChanges) {
      // Save changes before releasing lock
      const { steps, entryPoint, generatedStates } = convertGraphToSteps()

      try {
        // Save the current state
        const payload: Partial<ActionGraph> = { steps }
        if (entryPoint) {
          payload.entry_point = entryPoint
        }
        if (generatedStates && generatedStates.length > 0) {
          payload.states = generatedStates
        }
        await templateApi.update(selectedTemplateId, payload, sessionId)

        // Update saved state ref
        lastSavedStateRef.current = {
          nodes: JSON.stringify(nodes),
          edges: JSON.stringify(edges),
        }

        // Invalidate queries to refetch
        queryClient.invalidateQueries({ queryKey: ['template', selectedTemplateId] })
        queryClient.invalidateQueries({ queryKey: ['templates-all'] })
      } catch (error) {
        console.error('[Lock] Failed to save before releasing lock:', error)
        // Ask user if they want to release lock without saving
        const proceed = window.confirm('저장에 실패했습니다. 저장하지 않고 편집을 종료하시겠습니까?')
        if (!proceed) return
      }
    }

    // Stop heartbeat
    if (lockHeartbeatRef.current) {
      clearInterval(lockHeartbeatRef.current)
      lockHeartbeatRef.current = null
    }

    try {
      await behaviorTreeLockApi.release(selectedTemplateId, sessionId)
    } catch (error) {
      console.error('[Lock] Failed to release lock:', error)
    }

    setIsEditing(false)
    setLockStatus({ isLocked: false, lockedBy: null, expiresAt: null, isOwnLock: false })
  }, [selectedTemplateId, sessionId, isEditing, hasUnsavedChanges, convertGraphToSteps, nodes, edges, queryClient])

  // Fetch lock status when template changes
  useEffect(() => {
    if (!selectedTemplateId) {
      setLockStatus({ isLocked: false, lockedBy: null, expiresAt: null, isOwnLock: false })
      setIsEditing(false)
      return
    }

    const fetchLockStatus = async () => {
      try {
        const status = await behaviorTreeLockApi.getStatus(selectedTemplateId, sessionId)
        setLockStatus({
          isLocked: status.is_locked,
          lockedBy: status.locked_by || null,
          expiresAt: status.expires_at || null,
          isOwnLock: status.is_own_lock,
        })
        setIsEditing(status.is_own_lock)

        // Reclaim lock after refresh: restart heartbeat if we still own it
        if (status.is_own_lock) {
          if (lockHeartbeatRef.current) {
            clearInterval(lockHeartbeatRef.current)
          }
          lockHeartbeatRef.current = setInterval(async () => {
            if (!selectedTemplateId) return
            const heartbeatResult = await behaviorTreeLockApi.heartbeat(selectedTemplateId, sessionId)
            if (!heartbeatResult.success) {
              setIsEditing(false)
              setLockStatus({ isLocked: false, lockedBy: null, expiresAt: null, isOwnLock: false })
              if (lockHeartbeatRef.current) {
                clearInterval(lockHeartbeatRef.current)
                lockHeartbeatRef.current = null
              }
            } else if (heartbeatResult.expires_at) {
              setLockStatus(prev => ({ ...prev, expiresAt: heartbeatResult.expires_at! }))
            }
          }, 2 * 60 * 1000)
        }
      } catch (error) {
        console.error('[Lock] Failed to fetch lock status:', error)
      }
    }

    fetchLockStatus()
  }, [selectedTemplateId, sessionId])

  // Subscribe to WebSocket lock events for current template
  useEffect(() => {
    if (!selectedTemplateId) return

    const unsubscribeLock = subscribeLockEvents(selectedTemplateId, (msg: BehaviorTreeLockMessage) => {
      console.log('[WebSocket] Lock event:', msg)
      if (msg.action === 'acquired') {
        // Ignore our own lock events (identified by session_id)
        if (msg.session_id !== sessionId) {
          setLockStatus({
            isLocked: true,
            lockedBy: msg.locked_by || 'Another user',
            expiresAt: msg.expires_at || null,
            isOwnLock: false,
          })
        }
      } else if (msg.action === 'released' || msg.action === 'expired') {
        // Lock was released or expired - if we were force-unlocked, exit edit mode
        if (isEditing && msg.session_id !== sessionId) {
          // Our lock was force-released by someone else
          setIsEditing(false)
          if (lockHeartbeatRef.current) {
            clearInterval(lockHeartbeatRef.current)
            lockHeartbeatRef.current = null
          }
        }
        setLockStatus({ isLocked: false, lockedBy: null, expiresAt: null, isOwnLock: false })
      }
    })

    const unsubscribeSync = subscribeSyncEvents(selectedTemplateId, (msg: GraphSyncMessage) => {
      console.log('[WebSocket] Sync event:', msg)
      if (msg.action === 'updated') {
        // Graph was updated by someone else, refetch
        if (!isEditing) {
          queryClient.invalidateQueries({ queryKey: ['template', selectedTemplateId] })
        }
      }
    })

    return () => {
      unsubscribeLock()
      unsubscribeSync()
    }
  }, [selectedTemplateId, subscribeLockEvents, subscribeSyncEvents, isEditing, sessionId, queryClient])

  // Cleanup heartbeat on unmount (lock persists across refresh via persistent session)
  useEffect(() => {
    return () => {
      if (lockHeartbeatRef.current) {
        clearInterval(lockHeartbeatRef.current)
        lockHeartbeatRef.current = null
      }
    }
  }, [])

  const onConnect = useCallback(
    (params: Connection) => {
      console.log('[onConnect] params:', params)
      if (!params.source || !params.target) {
        console.log('[onConnect] Missing source or target, ignoring')
        return
      }
      if (params.target === START_NODE_ID) {
        console.log('[onConnect] Cannot connect TO start node')
        return
      }

      // Get target node to determine correct target handle
      const targetNode = nodes.find(node => node.id === params.target)
      const targetHandleId = params.targetHandle || (targetNode?.type === 'action' ? 'in' : 'state-in')

      if (params.source === START_NODE_ID) {
        const color = START_NODE_COLOR
        setEdges((eds) => {
          const withoutStart = eds.filter(edge => edge.source !== START_NODE_ID)
          const newEdge = {
            ...params,
            sourceHandle: params.sourceHandle || 'state-out',
            targetHandle: targetHandleId,
            type: 'smoothstep',
            animated: false,
            markerEnd: { type: MarkerType.ArrowClosed, color },
            style: {
              stroke: color,
              strokeWidth: 2,
            },
          }
          console.log('[onConnect] Creating START edge:', newEdge)
          return addEdge(newEdge, withoutStart)
        })
        return
      }

      const sourceNode = nodes.find(node => node.id === params.source)
      const endStates = sourceNode?.data?.endStates as EndStateConfig[] | undefined

      // If no sourceHandle specified, try to find a default (first success outcome)
      let sourceHandleId = params.sourceHandle
      if (!sourceHandleId && endStates && endStates.length > 0) {
        // Default to first end state (usually success)
        sourceHandleId = endStates[0].id
        console.log('[onConnect] No sourceHandle, defaulting to:', sourceHandleId)
      }

      const matchedEndState = endStates?.find(es => es.id === sourceHandleId)
      const outcome = matchedEndState
        ? normalizeOutcome(matchedEndState.outcome) || inferOutcome(matchedEndState, 0)
        : undefined

      const isSuccess = outcome === 'success'
      const color = outcome ? OUTCOME_EDGE_COLORS[outcome] : '#22c55e'

      const newEdge = {
        ...params,
        sourceHandle: sourceHandleId,
        targetHandle: targetHandleId,
        type: 'smoothstep',
        animated: false,
        data: {
          outcome,
        },
        markerEnd: { type: MarkerType.ArrowClosed, color },
        style: {
          stroke: color,
          strokeWidth: 2,
          strokeDasharray: isSuccess ? undefined : '5,5',
        },
      }
      console.log('[onConnect] Creating edge:', newEdge)
      setEdges((eds) => {
        const isSameConnection = (edge: Edge) =>
          edge.source === params.source &&
          edge.target === params.target &&
          (edge.sourceHandle ?? null) === (sourceHandleId ?? null) &&
          (edge.targetHandle ?? null) === (targetHandleId ?? null)

        // Toggle behavior: connecting the exact same handles again removes the edge.
        if (eds.some(isSameConnection)) {
          return eds.filter(edge => !isSameConnection(edge))
        }

        return addEdge(newEdge, eds)
      })
    },
    [nodes, setEdges]
  )

  // Debug: Track connection start/end
  const onConnectStart: OnConnectStart = useCallback((event, params) => {
    console.log('[onConnectStart] event:', event.type, 'params:', params)
  }, [])

  const onConnectEnd: OnConnectEnd = useCallback((event) => {
    console.log('[onConnectEnd] event:', event.type, (event.target as HTMLElement)?.className)
  }, [])

  // Use React.DragEvent for type compatibility with ReactFlow
  const onDragOver = useCallback((event: React.DragEvent) => {
    event.preventDefault()
    event.dataTransfer.dropEffect = 'move'
  }, [])

  const onDrop = useCallback(
    (event: React.DragEvent) => {
      event.preventDefault()
      console.log('[onDrop] Event fired')
      // Try both keys for compatibility
      let dataStr = event.dataTransfer.getData('application/reactflow')
      if (!dataStr) {
        dataStr = event.dataTransfer.getData('text/plain')
      }
      console.log('[onDrop] dataStr:', dataStr)
      if (!dataStr) {
        console.log('[onDrop] No data, returning early')
        return
      }

      let data
      try {
        data = JSON.parse(dataStr)
      } catch (e) {
        console.error('[onDrop] Failed to parse data:', e)
        return
      }
      console.log('[onDrop] Parsed data:', data)

      if (data.type !== 'action' && data.type !== 'service' && data.type !== 'event') {
        console.log('[onDrop] Unsupported palette item type:', data.type)
        return
      }

      const isActionLikeNode = data.type === 'action' || data.type === 'service'

      const position = screenToFlowPosition({
        x: event.clientX,
        y: event.clientY,
      })

      const defaultSuccessState = availableStates.includes('idle') ? 'idle' : availableStates[0]
      const defaultFailureState = availableStates.includes('error') ? 'error' : availableStates[availableStates.length - 1]

      // Generate default end states for the action node
      // Use stable IDs so ReactFlow can track handles properly
      const defaultEndStates = [
        { id: 'success', state: defaultSuccessState, label: 'Success', outcome: 'success' as const, color: '#22c55e' },
        { id: 'failed', state: defaultFailureState, label: 'Failed', outcome: 'failed' as const, color: '#ef4444' },
        { id: 'aborted', state: defaultFailureState, label: 'Aborted', outcome: 'aborted' as const, color: '#ef4444' },
        { id: 'cancelled', state: defaultFailureState, label: 'Cancelled', outcome: 'cancelled' as const, color: '#6b7280' },
      ]
      const defaultJobName = data.type === 'action'
        ? getDefaultJobNameTemplate(data.server || data.subtype, data.label)
        : (data.label || data.subtype || '')

      const newNode: Node = {
        id: getNodeId(),
        type: isActionLikeNode ? 'action' : 'event',
        position,
        dragHandle: isActionLikeNode ? ACTION_NODE_DRAG_HANDLE_SELECTOR : undefined,
        data: {
          label: data.label,
          subtype: data.subtype,
          color: data.color,
          actionType: data.actionType,
          server: data.server,
          capabilityKind: isActionLikeNode
            ? (data.capabilityKind || (data.type === 'service' ? 'service' : 'action'))
            : undefined,
          preStates: [],
          duringStateTargets: data.duringState
            ? [{ state: data.duringState, target_type: 'self' }]
            : [],
          successStates: [defaultSuccessState],
          failureStates: [defaultFailureState],
          duringState: data.duringState,
          successState: defaultSuccessState,
          failureState: defaultFailureState,
          initialState: defaultSuccessState,
          finalState: data.subtype === 'Error' ? defaultFailureState : defaultSuccessState,
          fromState: defaultSuccessState,
          toState: data.subtype,
          availableStates,
          availableAgents,
          preconditions: [],
          params: {},
          jobName: defaultJobName,
          // Auto-generate states feature (enabled by default)
          autoGenerateStates: true,
          generatedStates: [],
          // Default end states for handle rendering
          endStates: defaultEndStates,
          isEditing,
        },
      }

      console.log('[onDrop] Creating node:', newNode)
      console.log('[onDrop] Node type:', newNode.type, 'Position:', newNode.position)
      setNodes((nds) => {
        const newNodes = [...nds, newNode]
        console.log('[onDrop] Updated nodes count:', newNodes.length)
        return newNodes
      })
    },
    [screenToFlowPosition, setNodes, availableStates, availableAgents, isEditing]
  )

  const onDragStart = (event: React.DragEvent<HTMLDivElement>, item: PaletteItem) => {
    console.log('[DragStart] Item:', item)
    const itemData = JSON.stringify(item)
    // Set data with both keys for compatibility
    event.dataTransfer.setData('application/reactflow', itemData)
    event.dataTransfer.setData('text/plain', itemData)
    event.dataTransfer.effectAllowed = 'move'
  }

  const toggleCategory = (category: string) => {
    setExpandedCategories(prev =>
      prev.includes(category) ? prev.filter(c => c !== category) : [...prev, category]
    )
  }

  const toggleDiscoveredActionHidden = useCallback((hideKey: string, hide: boolean) => {
    setHiddenDiscoveredActionKeys(prev => {
      const next = new Set(prev)
      if (hide) {
        next.add(hideKey)
      } else {
        next.delete(hideKey)
      }
      return Array.from(next)
    })
  }, [])

  const toggleDiscoveredServiceHidden = useCallback(
    (hideKey: string, isDefaultHidden: boolean, hide: boolean) => {
      setHiddenDiscoveredServiceKeys((prev) => {
        const next = new Set(prev)
        if (hide) {
          next.add(hideKey)
        } else {
          next.delete(hideKey)
        }
        return Array.from(next)
      })

      setShownDefaultHiddenDiscoveredServiceKeys((prev) => {
        const next = new Set(prev)
        if (hide) {
          next.delete(hideKey)
        } else if (isDefaultHidden) {
          next.add(hideKey)
        } else {
          next.delete(hideKey)
        }
        return Array.from(next)
      })
    },
    []
  )

  const handleOpenTask = useCallback((taskId: string) => {
    setSelectedTemplateId(taskId)
    setBottomPanelTab(null)
  }, [])

  const selectedTaskSummary = useMemo(
    () => allTemplates.find(template => template.id === selectedTemplateId) || null,
    [allTemplates, selectedTemplateId]
  )
  const selectedTaskName = selectedTemplate?.name || selectedTaskSummary?.name || selectedTemplateId || ''
  const selectedTaskVersion = selectedTemplate?.version || selectedTaskSummary?.version || null

  return (
    <div className="h-screen flex bg-base">
      {/* Left Sidebar - Task definitions */}
      <div className="w-44 bg-surface border-r border-primary flex flex-col">
        {/* Task list */}
        <div className="flex-1 overflow-y-auto">
          <div className="py-2">
            <div className="px-3 py-2 flex items-center justify-between">
              <span className="text-xs font-semibold text-secondary uppercase tracking-wider">
                태스크
              </span>
              <button
                onClick={() => setShowCreateModal(true)}
                className="p-1 text-blue-400 hover:bg-blue-500/20 rounded"
                title="새 태스크 생성"
              >
                <PlusCircle size={14} />
              </button>
            </div>
            <div className="px-3 pb-2 text-[10px] text-muted">
              항목 클릭: 열기 · 상단 편집 버튼: 수정
            </div>

            {templatesLoading ? (
              <div className="px-3 py-4 text-center text-muted text-sm">로딩 중...</div>
            ) : allTemplates.length === 0 ? (
              <div className="px-3 py-4 text-center text-muted text-sm">
                태스크가 없습니다. 새로 생성하세요.
              </div>
            ) : (
              <div className="space-y-0.5">
                {allTemplates.map((template: TemplateListItem) => (
                  <div
                    key={template.id}
                    onClick={() => handleOpenTask(template.id)}
                    className={`w-full px-3 py-2 flex items-center gap-2 text-left transition-colors cursor-pointer group ${
                      selectedTemplateId === template.id
                        ? 'bg-blue-600/20 text-blue-400 border-l-2 border-blue-500'
                        : 'text-secondary hover:bg-elevated hover:text-primary'
                    }`}
                  >
                    <FileCode size={14} className="flex-shrink-0" />
                    <span className="flex-1 text-sm font-medium truncate">{template.name}</span>
                    {template.assignment_count > 0 && (
                      <span className="text-[10px] px-1.5 py-0.5 bg-green-500/20 text-green-400 rounded">
                        {template.assignment_count}
                      </span>
                    )}
                    <button
                      onClick={(e) => {
                        e.stopPropagation()
                        if (confirm(`태스크 "${template.name}"을(를) 삭제할까요?`)) {
                          deleteTemplate.mutate(template.id)
                        }
                      }}
                      className={`p-1 text-muted hover:text-red-400 hover:bg-red-500/20 rounded transition-opacity ${
                        selectedTemplateId === template.id ? 'opacity-100' : 'opacity-0 group-hover:opacity-100'
                      }`}
                      title="태스크 삭제"
                    >
                      <Trash2 size={12} />
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>

        {/* Selected task info */}
        {selectedTemplateId && (
          <div className="border-t border-primary p-3 bg-elevated/50">
            <div className="flex items-center justify-between mb-2">
              <span className="text-xs font-semibold text-secondary uppercase">선택된 태스크</span>
              <button
                onClick={() => {
                  if (!selectedTemplate) return
                  setShowAssignModal(true)
                }}
                disabled={!selectedTemplate}
                className="text-xs px-2 py-1 bg-blue-600/20 text-blue-400 rounded hover:bg-blue-600/30 flex items-center gap-1"
              >
                <Link2 size={12} />
                할당
              </button>
            </div>
            <div className="text-sm text-primary font-medium truncate">{selectedTaskName}</div>
            <div className="text-xs text-muted mt-1">
              {selectedTaskVersion ? `v${selectedTaskVersion}` : '상세 불러오는 중...'}
            </div>
            <div className="text-[10px] text-muted mt-1">
              열림 상태입니다. 수정은 상단 `편집` 버튼을 눌러 시작합니다.
            </div>
            {templateAssignments.length > 0 && (
              <div className="mt-2 pt-2 border-t border-primary/50">
                <div className="text-xs text-muted mb-1">
                  {templateAssignments.length}개 RTM에 할당됨
                </div>
                <div className="flex flex-wrap gap-1">
                  {templateAssignments.slice(0, 3).map(a => (
                    <span key={a.id} className="text-[10px] px-1.5 py-0.5 bg-green-500/20 text-green-400 rounded">
                      {formatTaskManagerName(a.agent_name) || formatTaskManagerName(a.agent_id) || a.agent_id}
                    </span>
                  ))}
                  {templateAssignments.length > 3 && (
                    <span className="text-[10px] px-1.5 py-0.5 bg-gray-500/20 text-secondary rounded">
                      +{templateAssignments.length - 3}개 더
                    </span>
                  )}
                </div>
              </div>
            )}

            {/* Validation Errors Panel */}
            {validationErrors.length > 0 && (
              <div className="mt-2 pt-2 border-t border-primary/50">
                <div className="flex items-center gap-1 text-xs text-yellow-400 mb-1">
                  <AlertCircle size={12} />
                  <span className="font-semibold">Validation Issues</span>
                </div>
                <div className="space-y-1 max-h-32 overflow-y-auto">
                  {validationErrors.map((error, idx) => (
                    <div key={idx} className="text-[10px] p-1.5 bg-yellow-500/10 rounded border border-yellow-500/20">
                      <div className="font-medium text-yellow-300">{error.nodeName}</div>
                      {error.errors.map((e, i) => (
                        <div key={i} className="text-yellow-400/80 ml-2">• {e}</div>
                      ))}
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        )}
      </div>

      {/* Middle: Node Palette (when task loaded) */}
      {selectedTemplate && (
        <div className="w-56 bg-surface border-r border-primary flex flex-col">
          {/* States Management */}
          <div className="px-3 py-3 border-b border-primary">
            {selectedStateDef ? (
              <>
                <button
                  onClick={() => setShowAddStateModal(true)}
                  className="w-full flex items-center justify-center gap-2 px-3 py-2 bg-gradient-to-r from-green-600/20 to-green-500/10 border border-green-500/40 rounded-lg text-green-400 hover:from-green-600/30 hover:to-green-500/20 hover:border-green-500/60 transition-all"
                >
                  <Plus size={14} />
                  <span className="text-xs font-medium">Manage States</span>
                </button>
                <div className="mt-2 flex flex-wrap gap-1">
                  {availableStates.slice(0, 5).map(state => (
                    <span
                      key={state}
                      className="px-2 py-0.5 rounded text-[10px]"
                      style={{
                        backgroundColor: `${getStateColor(state)}20`,
                        color: getStateColor(state),
                      }}
                    >
                      {state}
                    </span>
                  ))}
                  {availableStates.length > 5 && (
                    <span className="px-2 py-0.5 rounded text-[10px] text-muted">
                      +{availableStates.length - 5}
                    </span>
                  )}
                </div>
              </>
            ) : (
              <div className="text-[10px] text-muted italic">
                상태 정의 없음
              </div>
            )}
          </div>

          {/* RTM Filter */}
          <div className="px-3 py-2 border-b border-primary">
            <div className="flex items-center gap-2 mb-1.5">
              <Cpu size={12} className="text-secondary" />
              <span className="text-[10px] font-semibold text-secondary uppercase tracking-wider">RTM</span>
            </div>
            <select
              value={selectedAgentFilter || ''}
              onChange={(e) => setSelectedAgentFilter(e.target.value || null)}
              className="w-full px-2 py-1.5 bg-elevated border border-primary rounded-lg text-xs text-primary focus:outline-none focus:border-blue-500 cursor-pointer"
            >
              <option value="">All RTMs</option>
              {sortedAgentsForFilter.map((agent) => (
                <option key={agent.id} value={agent.id}>
                  {(formatTaskManagerName(agent.name) || formatTaskManagerName(agent.id) || agent.id) +
                    (agent.status === 'online' ? '' : ' (offline template)')}
                </option>
              ))}
            </select>
          </div>

          {/* Compatible RTM Section - Prominent placement */}
          {selectedTemplate && (
            <div className="mx-3 my-2 p-2.5 bg-gradient-to-r from-green-500/10 to-emerald-500/5 border border-green-500/30 rounded-lg">
              <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-2">
                  <Users size={14} className="text-green-400" />
                  <span className="text-xs font-semibold text-green-400">
                    호환 RTM 템플릿
                  </span>
                </div>
                <span className="text-xs text-green-300 font-bold">
                  {compatibleRtmTemplateCount}/{compatibleRtmTemplates.length}
                </span>
              </div>

              {requiredCapabilityKeys.length === 0 ? (
                <div className="text-[10px] text-muted bg-elevated/70 px-2 py-1 rounded">
                  액션/서비스 노드를 추가하면 호환 RTM 템플릿을 계산합니다.
                </div>
              ) : compatibleRtmTemplates.length === 0 ? (
                <div className="text-[10px] text-yellow-400 bg-yellow-500/10 px-2 py-1 rounded">
                  RTM capability 템플릿이 없습니다. RTM이 한 번 이상 capability를 등록해야 합니다.
                </div>
              ) : compatibleRtmTemplateCount > 0 ? (
                <div className="flex flex-wrap gap-1.5">
                  {compatibleRtmTemplates
                    .filter((agent) => agent.hasAllCapabilities)
                    .map((agent) => (
                      <div
                        key={agent.id}
                        className="flex items-center gap-1.5 px-2 py-1 rounded-full text-[10px] font-medium bg-green-500/20 text-green-300 border border-green-500/40"
                      >
                        <div className={`w-1.5 h-1.5 rounded-full ${agent.status === 'online' ? 'bg-green-400' : 'bg-gray-400'}`} />
                        {agent.name}
                      </div>
                    ))}
                </div>
              ) : (
                <div className="text-[10px] text-yellow-400 bg-yellow-500/10 px-2 py-1 rounded">
                  현재 태스크의 action/service 조합을 모두 만족하는 RTM 템플릿이 없습니다.
                </div>
              )}

              {requiredCapabilityKeys.length > 0 && compatibleRtmTemplates.length > compatibleRtmTemplateCount && (
                <div className="text-[9px] text-muted mt-1.5">
                  부분 호환 {compatibleRtmTemplates.length - compatibleRtmTemplateCount}개
                </div>
              )}
            </div>
          )}

          {/* Node Search */}
          <div className="px-3 py-2 border-b border-primary">
            <div className="flex items-center gap-2 px-2 py-1.5 bg-elevated border border-primary rounded-lg">
              <Search size={12} className="text-secondary flex-shrink-0" />
              <input
                type="text"
                value={nodeSearchQuery}
                onChange={(e) => setNodeSearchQuery(e.target.value)}
                placeholder="Action / Service / Node 검색"
                className="flex-1 bg-transparent text-xs text-primary placeholder:text-muted focus:outline-none"
              />
              {nodeSearchQuery && (
                <button
                  type="button"
                  onClick={() => setNodeSearchQuery('')}
                  className="p-0.5 text-muted hover:text-primary hover:bg-surface rounded"
                  title="검색어 지우기"
                >
                  <X size={10} />
                </button>
              )}
            </div>
            <div className="mt-1 text-[9px] text-muted">
              서비스와 lifecycle 노드도 함께 검색됩니다.
            </div>
          </div>

          {/* Node Palette */}
          <div className="flex-1 overflow-y-auto">
            {nodePalette.length === 0 ? (
              <div className="p-4 text-center text-muted text-xs">No nodes found</div>
            ) : (
              nodePalette.map(category => {
                const normalizedSearchQuery = nodeSearchQuery.trim().toLowerCase()
                const isDiscoveredActionsCategory = category.category === 'Discovered Actions'
                const isDiscoveredServicesCategory = category.category === 'Discovered Services'
                const isDiscoveredCategory = isDiscoveredActionsCategory || isDiscoveredServicesCategory
                const discoveryTab = isDiscoveredActionsCategory ? discoveredActionTab : discoveredServiceTab
                const lifecycleOnly = isDiscoveredActionsCategory
                  ? discoveredActionLifecycleOnly
                  : discoveredServiceLifecycleOnly
                const visibleDiscoveredCount = category.items.filter((item) => !item.isHidden).length
                const hiddenDiscoveredCount = category.items.filter((item) => item.isHidden).length
                const lifecycleDiscoveredCount = category.items.filter((item) => item.isLifecycleNode === true).length
                const baseCategoryItems = isDiscoveredCategory
                  ? category.items.filter((item) => discoveryTab === 'hidden' ? item.isHidden : !item.isHidden)
                  : category.items
                const lifecycleFilteredItems = isDiscoveredCategory && lifecycleOnly
                  ? baseCategoryItems.filter((item) => item.isLifecycleNode === true)
                  : baseCategoryItems
                const categoryItems = normalizedSearchQuery
                  ? lifecycleFilteredItems.filter((item) => matchesPaletteSearch(item, normalizedSearchQuery))
                  : lifecycleFilteredItems

                return (
                  <div key={category.category}>
                    <button
                      onClick={() => toggleCategory(category.category)}
                      className="w-full px-3 py-2 flex items-center justify-between text-[10px] font-semibold text-secondary uppercase tracking-wider hover:bg-elevated"
                    >
                      <div className="flex items-center gap-1.5">
                        {category.icon}
                        <span>{category.category}</span>
                      </div>
                      <div className="flex items-center gap-2">
                        <span className="text-[9px] text-muted normal-case">
                          {categoryItems.length}
                          {normalizedSearchQuery ? `/${baseCategoryItems.length}` : ''}
                        </span>
                        {expandedCategories.includes(category.category) ? (
                          <ChevronDown className="w-3.5 h-3.5" />
                        ) : (
                          <ChevronRight className="w-3.5 h-3.5" />
                        )}
                      </div>
                    </button>
                    {expandedCategories.includes(category.category) && (
                      <div className="px-2 pb-2 space-y-0.5">
                        {isDiscoveredCategory && (
                          <div className="px-1 pt-1 pb-1.5">
                            <div className="flex items-center justify-between gap-2">
                              <div className="inline-flex rounded-md border border-primary overflow-hidden">
                                <button
                                  onClick={() => (
                                    isDiscoveredActionsCategory
                                      ? setDiscoveredActionTab('visible')
                                      : setDiscoveredServiceTab('visible')
                                  )}
                                  className={`px-2 py-1 text-[10px] transition-colors ${
                                    discoveryTab === 'visible'
                                      ? 'bg-blue-600/25 text-blue-300'
                                      : 'bg-elevated text-secondary hover:text-primary'
                                  }`}
                                >
                                  Visible ({visibleDiscoveredCount})
                                </button>
                                <button
                                  onClick={() => (
                                    isDiscoveredActionsCategory
                                      ? setDiscoveredActionTab('hidden')
                                      : setDiscoveredServiceTab('hidden')
                                  )}
                                  className={`px-2 py-1 text-[10px] transition-colors border-l border-primary ${
                                    discoveryTab === 'hidden'
                                      ? 'bg-yellow-600/25 text-yellow-300'
                                      : 'bg-elevated text-secondary hover:text-primary'
                                  }`}
                                >
                                  Hidden ({hiddenDiscoveredCount})
                                </button>
                              </div>

                              <button
                                onClick={() => (
                                  isDiscoveredActionsCategory
                                    ? setDiscoveredActionLifecycleOnly((prev) => !prev)
                                    : setDiscoveredServiceLifecycleOnly((prev) => !prev)
                                )}
                                className={`px-2 py-1 rounded-md border text-[10px] transition-colors ${
                                  lifecycleOnly
                                    ? 'border-amber-500/50 bg-amber-600/25 text-amber-200'
                                    : 'border-primary bg-elevated text-secondary hover:text-primary'
                                }`}
                                title="Lifecycle 노드가 제공한 항목만 표시"
                              >
                                Lifecycle Only ({lifecycleDiscoveredCount})
                              </button>
                            </div>
                          </div>
                        )}

                        {categoryItems.length === 0 ? (
                          <div className="px-2 py-2 text-[10px] text-muted italic">
                            {normalizedSearchQuery
                              ? '검색 결과가 없습니다.'
                              : isDiscoveredCategory && discoveryTab === 'hidden'
                              ? (isDiscoveredServicesCategory ? '숨겨진 service가 없습니다.' : '숨겨진 action이 없습니다.')
                              : '표시할 항목이 없습니다.'}
                          </div>
                        ) : categoryItems.map((item, idx) => {
                          const canDrag = isEditing && item.isDraggable !== false
                          const isDiscoveredItem = isDiscoveredCategory && !!item.hideKey

                          return (
                            <div
                              key={`${item.subtype}-${idx}`}
                              draggable={canDrag}
                              onDragStart={canDrag ? (e) => {
                                e.stopPropagation()
                                onDragStart(e, item)
                              } : undefined}
                              className={`flex items-center gap-2 px-2 py-1.5 rounded transition-colors border border-transparent ${
                                canDrag
                                  ? 'cursor-grab active:cursor-grabbing hover:bg-elevated hover:border-secondary'
                                  : item.isDraggable === false
                                    ? 'cursor-default hover:bg-elevated/70'
                                    : 'cursor-default opacity-60'
                              } ${
                                item.isAvailable === false ? 'opacity-50' : ''
                              }`}
                            >
                              <div
                                className="w-2.5 h-2.5 rounded-sm flex-shrink-0"
                                style={{ backgroundColor: item.color }}
                              />
                              <div className="flex-1 min-w-0">
                                <span className="text-xs text-primary block truncate">{item.label}</span>
                                {item.duringState && (
                                  <span className="text-[9px] text-yellow-500 block">{item.duringState}</span>
                                )}
                                {item.robotCount !== undefined && (
                                  <span className="text-[9px] text-blue-400 block">
                                    {item.robotCount} robot{item.robotCount !== 1 ? 's' : ''}
                                  </span>
                                )}
                                {item.agentName && (
                                  <span className="text-[9px] text-cyan-400 block truncate">{item.agentName}</span>
                                )}
                                {item.providerNode && (
                                  <span className="text-[9px] text-emerald-400 block truncate">
                                    node: {item.providerNode}
                                  </span>
                                )}
                                {item.isLifecycleNode !== undefined && (
                                  <span className={`inline-flex items-center gap-1 mt-0.5 px-1.5 py-0.5 rounded border text-[9px] ${
                                    item.isLifecycleNode
                                      ? 'bg-amber-500/15 text-amber-300 border-amber-500/30'
                                      : 'bg-gray-500/10 text-muted border-gray-500/20'
                                  }`}>
                                    <span className={`w-1.5 h-1.5 rounded-full ${
                                      item.isLifecycleNode ? 'bg-amber-400' : 'bg-gray-500'
                                    }`} />
                                    {item.isLifecycleNode
                                      ? `Lifecycle · ${item.lifecycleState || 'unknown'}`
                                      : 'Non-lifecycle'}
                                  </span>
                                )}
                                {item.actionType && !item.duringState && !item.robotCount && (
                                  <span className={`text-[9px] block ${
                                    item.capabilityKind === 'service' ? 'text-sky-400' : 'text-rose-400'
                                  }`}>
                                    {item.actionType.split('/').pop()}
                                  </span>
                                )}
                              </div>
                              <div className="flex flex-col items-end gap-1">
                                {item.isAvailable === false && (
                                  <span className="text-[8px] text-red-400">
                                    {item.capabilityKind === 'service' ? 'down' : 'busy'}
                                  </span>
                                )}
                                {isDiscoveredItem && item.hideKey && (
                                  <button
                                    type="button"
                                    onClick={(e) => {
                                      e.stopPropagation()
                                      if (isDiscoveredActionsCategory) {
                                        toggleDiscoveredActionHidden(item.hideKey!, !item.isHidden)
                                      } else {
                                        toggleDiscoveredServiceHidden(
                                          item.hideKey!,
                                          !!item.isDefaultHidden,
                                          !item.isHidden
                                        )
                                      }
                                    }}
                                    className="p-0.5 text-muted hover:text-primary hover:bg-elevated rounded"
                                    title={
                                      isDiscoveredServicesCategory
                                        ? (item.isHidden ? 'Show service' : 'Hide service')
                                        : (item.isHidden ? 'Show action' : 'Hide action')
                                    }
                                  >
                                    {item.isHidden ? <Eye size={10} /> : <EyeOff size={10} />}
                                  </button>
                                )}
                              </div>
                            </div>
                          )
                        })}
                      </div>
                    )}
                  </div>
                )
              })
            )}
          </div>

        </div>
      )}

      {/* Main Canvas */}
      <div className="flex-1 flex flex-col">
        {/* Toolbar */}
        <div className="h-12 bg-surface border-b border-primary flex items-center justify-between px-4">
          <div className="flex items-center gap-2">
            <Zap className="w-5 h-5 text-blue-400" />
            <span className="font-semibold text-primary">Task Definitions</span>
            {selectedTemplateId && (
              <>
                <span className="text-muted">/</span>
                <FileCode className="w-4 h-4 text-blue-400" />
                <span className="text-blue-400 text-sm">{selectedTaskName}</span>
                {selectedTaskVersion && (
                  <span className="text-muted text-xs ml-2">v{selectedTaskVersion}</span>
                )}
              </>
            )}
          </div>
          {selectedTemplateId && (
            <div className="flex items-center gap-2">
              {/* Lock Status / Edit Button */}
              {!isEditing ? (
                // Not editing - show Edit button or lock status
                lockStatus.isLocked && !lockStatus.isOwnLock ? (
                  <div className="flex items-center gap-2">
                    <div className="flex items-center gap-1.5 px-3 py-1.5 bg-yellow-600/20 text-yellow-400 rounded-lg text-sm border border-yellow-500/30">
                      <Lock size={14} />
                      <span>{lockStatus.lockedBy}님이 편집 중</span>
                    </div>
                    <button
                      onClick={forceUnlock}
                      className="flex items-center gap-1 px-2 py-1.5 text-xs text-red-400 hover:bg-red-600/20 rounded border border-red-500/30 transition-colors"
                      title={`강제 해제 (만료: ${lockStatus.expiresAt ? new Date(lockStatus.expiresAt).toLocaleTimeString() : '알 수 없음'})`}
                    >
                      <Unlock size={12} />
                      강제 해제
                    </button>
                  </div>
                ) : (
                  <button
                    onClick={acquireLock}
                    className="flex items-center gap-1.5 px-3 py-1.5 bg-blue-600 hover:bg-blue-700 text-white rounded-lg text-sm transition-colors"
                  >
                    <Edit size={14} />
                    <span>편집</span>
                  </button>
                )
              ) : (
                // Editing - show editing status and finish button
                <>
                  <div className="flex items-center gap-1.5 px-3 py-1.5 bg-green-600/20 text-green-400 rounded-lg text-sm border border-green-500/30">
                    <Unlock size={14} />
                    <span>편집 중</span>
                  </div>
                  <button
                    onClick={releaseLock}
                    className="flex items-center gap-1.5 px-3 py-1.5 bg-gray-600 hover:bg-gray-700 text-white rounded-lg text-sm transition-colors"
                  >
                    <Lock size={14} />
                    <span>편집 완료</span>
                  </button>
                </>
              )}

              {/* Validation Errors Indicator */}
              {validationErrors.length > 0 && (
                <div className="flex items-center gap-1.5 px-2 py-1 bg-yellow-600/20 text-yellow-400 rounded-lg text-xs">
                  <AlertCircle size={12} />
                  <span>{validationErrors.length}개 문제 발견</span>
                </div>
              )}

              {/* Save button (only show when editing) */}
              {isEditing && (
                <button
                  onClick={handleSave}
                  disabled={saveTemplate.isPending || saveStatus === 'saving'}
                  className={`flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium border transition-colors ${
                    saveStatus === 'saving' ? 'bg-blue-600/20 text-blue-400 border-blue-500/50 cursor-wait' :
                    saveStatus === 'saved' && !hasUnsavedChanges ? 'bg-green-600/20 text-green-400 border-green-500/50' :
                    saveStatus === 'error' ? 'bg-red-600/20 text-red-400 border-red-500/50' :
                    hasUnsavedChanges ? 'bg-yellow-600/20 text-yellow-400 border-yellow-500/50 hover:bg-yellow-600/30' :
                    'bg-elevated text-secondary border-primary hover:bg-surface hover:text-primary'
                  }`}
                >
                  {saveStatus === 'saving' ? (
                    <>
                      <div className="w-3 h-3 border-2 border-blue-400 border-t-transparent rounded-full animate-spin" />
                      <span>저장 중...</span>
                    </>
                  ) : saveStatus === 'error' ? (
                    <>
                      <AlertCircle size={14} />
                      <span>저장 오류</span>
                    </>
                  ) : hasUnsavedChanges ? (
                    <>
                      <Save size={14} />
                      <span>저장</span>
                    </>
                  ) : (
                    <>
                      <Check size={14} />
                      <span>저장됨</span>
                    </>
                  )}
                </button>
              )}
              <button
                onClick={() => {
                  if (!selectedTemplateId) return
                  if (confirm(`태스크 "${selectedTaskName}"을(를) 삭제할까요?`)) {
                    deleteTemplate.mutate(selectedTemplateId)
                  }
                }}
                className="p-1.5 text-red-400 hover:bg-red-500/20 rounded-md transition-colors"
              >
                <Trash2 size={16} />
              </button>
            </div>
          )}
        </div>

        {/* Canvas and Bottom Panel */}
        <div className="flex-1 flex flex-col">
          {/* Canvas Area */}
          <div ref={reactFlowWrapper} className="flex-1">
            {selectedTemplateId ? (
              selectedTemplateLoading ? (
                <div className="h-full flex items-center justify-center text-sm text-muted">
                  태스크를 불러오는 중...
                </div>
              ) : selectedTemplateError ? (
                <div className="h-full flex items-center justify-center">
                  <div className="text-center">
                    <AlertCircle className="w-10 h-10 mx-auto mb-3 text-yellow-400" />
                    <h3 className="text-base font-semibold text-secondary mb-2">태스크를 불러오지 못했습니다</h3>
                    <p className="text-xs text-muted mb-3">
                      {extractApiErrorMessage(selectedTemplateQueryError, '요청 처리 중 오류가 발생했습니다')}
                    </p>
                    <button
                      onClick={() => refetchSelectedTemplate()}
                      className="px-3 py-1.5 bg-blue-600 text-primary rounded hover:bg-blue-700 text-sm"
                    >
                      다시 시도
                    </button>
                  </div>
                </div>
              ) : selectedTemplate ? (
              <ReactFlow
                nodes={nodes}
                edges={edgesWithDelete}
                onNodesChange={isEditing ? onNodesChange : undefined}
                onEdgesChange={isEditing ? onEdgesChange : undefined}
                onConnect={isEditing ? onConnect : undefined}
                onConnectStart={isEditing ? onConnectStart : undefined}
                onConnectEnd={isEditing ? onConnectEnd : undefined}
                onDrop={isEditing ? onDrop : undefined}
                onDragOver={isEditing ? onDragOver : undefined}
                nodeTypes={nodeTypes}
                edgeTypes={edgeTypes}
                nodesDraggable={isEditing}
                nodesConnectable={isEditing}
                elementsSelectable={isEditing}
                fitView
                snapToGrid
                snapGrid={[16, 16]}
                defaultEdgeOptions={{
                  type: 'deletable',
                  animated: false,
                  markerEnd: { type: MarkerType.ArrowClosed, color: '#22c55e' },
                  style: { stroke: '#22c55e', strokeWidth: 2 },
                }}
                connectionLineStyle={{ stroke: '#64748b', strokeWidth: 2 }}
                connectionLineType={ConnectionLineType.SmoothStep}
              >
                <Background variant={BackgroundVariant.Dots} gap={16} size={1} color="#2a2a4a" />
                <Controls className="!bg-surface !border-primary !rounded-lg [&>button]:!bg-surface [&>button]:!border-primary [&>button]:!text-primary [&>button:hover]:!bg-elevated" />
                {!bottomPanelTab && (
                  <Panel position="bottom-center" className="mb-4">
                    <div className="bg-surface/90 backdrop-blur-sm px-4 py-2 rounded-lg border border-primary text-xs text-secondary">
                      {isEditing
                        ? 'Drag action servers to canvas to build task flow'
                        : '상단 "편집" 버튼을 눌러 태스크 수정을 시작하세요'}
                    </div>
                  </Panel>
                )}
              </ReactFlow>
              ) : (
                <div className="h-full flex items-center justify-center text-sm text-muted">
                  태스크 상세 데이터가 없습니다.
                </div>
              )
            ) : (
              <div className="h-full flex items-center justify-center">
                <div className="text-center">
                  <Layout className="w-16 h-16 mx-auto mb-4 text-muted" />
                  <h2 className="text-xl font-semibold text-secondary mb-2">태스크를 선택하세요</h2>
                  <p className="text-muted text-sm max-w-md">
                    왼쪽 패널에서 태스크를 열어 흐름을 확인/편집하거나,
                    새 태스크를 생성하세요.
                  </p>
                  <button
                    onClick={() => setShowCreateModal(true)}
                    className="mt-4 px-4 py-2 bg-blue-600 text-primary rounded-lg hover:bg-blue-700 inline-flex items-center gap-2"
                  >
                    <Plus size={16} />
                    새 태스크 생성
                  </button>
                </div>
              </div>
            )}
          </div>

          {/* Bottom Panel - Telemetry & Assignments */}
          {selectedTemplate && (
            <div className="border-t border-primary bg-surface">
              {/* Panel Toggle Bar */}
              <div className="flex items-center justify-between px-3 py-1.5 bg-elevated/50">
                <div className="flex items-center gap-1">
                  <button
                    onClick={() => setBottomPanelTab(bottomPanelTab === 'telemetry' ? null : 'telemetry')}
                    className={`flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium transition-colors ${
                      bottomPanelTab === 'telemetry'
                        ? 'bg-green-500/20 text-green-400'
                        : 'text-secondary hover:text-green-400 hover:bg-green-500/10'
                    }`}
                  >
                    <Radio size={12} className={bottomPanelTab === 'telemetry' ? 'animate-pulse' : ''} />
                    Telemetry
                  </button>
                  <button
                    onClick={() => setBottomPanelTab(bottomPanelTab === 'assignments' ? null : 'assignments')}
                    className={`flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium transition-colors ${
                      bottomPanelTab === 'assignments'
                        ? 'bg-blue-500/20 text-blue-400'
                        : 'text-secondary hover:text-blue-400 hover:bg-blue-500/10'
                    }`}
                  >
                    <Link2 size={12} />
                    Assignments
                    {templateAssignments.length > 0 && (
                      <span className="px-1.5 py-0.5 bg-blue-500/30 rounded text-[10px]">
                        {templateAssignments.length}
                      </span>
                    )}
                  </button>
                </div>
                {bottomPanelTab && (
                  <button
                    onClick={() => setBottomPanelTab(null)}
                    className="p-1 text-muted hover:text-primary hover:bg-elevated rounded transition-colors"
                  >
                    <ChevronDown size={14} />
                  </button>
                )}
              </div>

              {/* Panel Content */}
              {bottomPanelTab && (
                <div className="h-48 overflow-hidden">
                  {bottomPanelTab === 'telemetry' ? (
                    <TelemetryPanel
                      isOpen={true}
                      onClose={() => setBottomPanelTab(null)}
                      embedded={true}
                      horizontal={true}
                    />
                  ) : bottomPanelTab === 'assignments' ? (
                    <div className="h-full flex gap-4 p-3 overflow-x-auto">
                      {/* Assigned RTMs */}
                      <div className="flex-shrink-0 w-72">
                        <div className="flex items-center justify-between mb-2">
                          <h3 className="text-xs font-semibold text-secondary uppercase">할당된 RTM</h3>
                          <button
                            onClick={() => setShowAssignModal(true)}
                            className="p-1 text-blue-400 hover:bg-blue-500/20 rounded"
                            title="RTM에 할당"
                          >
                            <Plus size={14} />
                          </button>
                        </div>
                        {templateAssignments.length === 0 ? (
                          <div className="text-xs text-muted p-3 bg-elevated rounded-lg">
                            할당된 RTM 없음
                          </div>
                        ) : (
                          <div className="space-y-1.5 max-h-32 overflow-y-auto">
                            {templateAssignments.map(a => (
                              <div key={a.id} className="flex items-center justify-between p-2 bg-elevated rounded-lg">
                                <div className="flex items-center gap-2">
                                  <Cpu size={12} className="text-green-400" />
                                  <span className="text-xs text-primary">
                                    {formatTaskManagerName(a.agent_name) || formatTaskManagerName(a.agent_id) || a.agent_id}
                                  </span>
                                </div>
                                <button
                                  onClick={() => unassignTemplate.mutate({ templateId: selectedTemplate.id, agentId: a.agent_id })}
                                  className="p-1 text-red-400 hover:bg-red-500/20 rounded"
                                  title="할당 해제"
                                >
                                  <Unlink size={10} />
                                </button>
                              </div>
                            ))}
                          </div>
                        )}
                      </div>

                      {/* Compatible RTM Templates (independent from RTM filter selection) */}
                      <div className="flex-shrink-0 w-72">
                        <h3 className="text-xs font-semibold text-secondary uppercase mb-2">호환 RTM 템플릿</h3>
                        <div className="space-y-1.5 max-h-32 overflow-y-auto">
                          {compatibleRtmTemplates
                            .filter((agent) =>
                              agent.hasAllCapabilities &&
                              !templateAssignments.some((assignment) => assignment.agent_id === agent.id)
                            )
                            .map((agent) => (
                              <div key={agent.id} className="flex items-center justify-between p-2 bg-elevated rounded-lg">
                                <div className="flex items-center gap-2">
                                  <Cpu size={12} className="text-purple-400" />
                                  <span className="text-xs text-primary">
                                    {agent.name}
                                  </span>
                                </div>
                                <button
                                  onClick={() => assignTemplate.mutate({ templateId: selectedTemplate.id, agentId: agent.id })}
                                  className="px-2 py-1 text-[10px] bg-blue-600/20 text-blue-400 rounded hover:bg-blue-600/30"
                                >
                                  할당
                                </button>
                              </div>
                            ))}
                          {compatibleRtmTemplates.filter((agent) =>
                            agent.hasAllCapabilities &&
                            !templateAssignments.some((assignment) => assignment.agent_id === agent.id)
                          ).length === 0 && (
                            <div className="text-xs text-muted p-3 bg-elevated rounded-lg">
                              추가 호환 RTM 없음
                            </div>
                          )}
                        </div>
                      </div>
                    </div>
                  ) : null}
                </div>
              )}
            </div>
          )}
        </div>
      </div>

      {/* Create Template Modal */}
      {showCreateModal && (
        <CreateTemplateModal
          onClose={() => setShowCreateModal(false)}
          onCreated={(id) => {
            setShowCreateModal(false)
            setSelectedTemplateId(id)
            queryClient.invalidateQueries({ queryKey: ['templates'] })
            queryClient.invalidateQueries({ queryKey: ['templates-all'] })
          }}
        />
      )}

      {/* Assign Template Modal */}
      {showAssignModal && (
        selectedTemplate ? (
          <AssignTemplateModal
            template={selectedTemplate}
            currentAssignments={templateAssignments}
            allAgents={agents}
            onAssign={(agentId) => assignTemplate.mutate({ templateId: selectedTemplate.id, agentId })}
            onUnassign={(agentId) => unassignTemplate.mutate({ templateId: selectedTemplate.id, agentId })}
            onClose={() => setShowAssignModal(false)}
          />
        ) : (
          // Debug: Modal was triggered but no template selected
          <div className="fixed inset-0 bg-black/70 flex items-center justify-center z-50">
            <div className="bg-surface rounded-xl shadow-2xl p-6 border border-primary">
              <p className="text-primary mb-4">오류: 선택된 태스크가 없습니다</p>
              <button
                onClick={() => setShowAssignModal(false)}
                className="px-4 py-2 bg-blue-600 text-primary rounded"
              >
                닫기
              </button>
            </div>
          </div>
        )
      )}

      {/* Add State Modal */}
      {showAddStateModal && selectedStateDef && (
        <AddStateModal
          stateDef={selectedStateDef}
          onClose={() => setShowAddStateModal(false)}
          onAdded={() => {
            setShowAddStateModal(false)
            refetchStateDef()
          }}
        />
      )}
    </div>
  )
}

function convertActionGraphToGraph(
  actionGraph: ActionGraph,
  stateDef: StateDefinition | undefined,
  availableStates: string[],
  availableAgents: Array<{ id: string; name: string }>
): { initialNodes: Node[]; initialEdges: Edge[] } {
  const nodes: Node[] = []
  const edges: Edge[] = []

  // Defensive: ensure steps is always an array
  const steps = actionGraph.steps || []

  const actionMappings = stateDef?.action_mappings || []
  const defaultState = availableStates.includes('idle') ? 'idle' : availableStates[0] || 'idle'
  const errorState = availableStates.includes('error') ? 'error' : availableStates[availableStates.length - 1] || 'error'
  const stepIds = new Set(steps.map(step => step.id))
  const preferredEntry = actionGraph.entry_point || steps[0]?.id

  nodes.push({
    id: START_NODE_ID,
    type: 'event',
    position: { x: 80, y: 100 },
    data: {
      label: 'Start',
      subtype: 'Start',
      color: START_NODE_COLOR,
      initialState: defaultState,
      availableStates,
      availableAgents,
      isEditing: false,
    },
    draggable: false,
    selectable: false,
    deletable: false,
  })

  steps.forEach((step, index) => {
    const storedX = step.ui?.x
    const storedY = step.ui?.y
    const hasStoredPosition =
      typeof storedX === 'number' &&
      typeof storedY === 'number' &&
      Number.isFinite(storedX) &&
      Number.isFinite(storedY)

    const x = hasStoredPosition ? storedX : 300 + (index % 3) * 300
    const y = hasStoredPosition ? storedY : 100 + Math.floor(index / 3) * 200

    const normalizedServer = normalizeLegacyNamespaceServer(step.action?.server)
    let subtype = normalizedServer || step.action?.type || 'Unknown'
    const capabilityKind = normalizeCapabilityKind(step.action?.capability_kind, step.action?.type)
    let color = capabilityKind === 'service' ? '#0ea5e9' : '#f87171'
    let actionType = step.action?.type
    let duringStates: string[] = []

    const mapping = actionMappings.find(m => m.action_type === step.action?.type) ||
      actionMappings.find(m => m.server === step.action?.server)
    if (mapping && capabilityKind !== 'service') {
      color = getActionColor(mapping.action_type)
      actionType = mapping.action_type
      duringStates = mapping.during_states || (mapping.during_state ? [mapping.during_state] : [])
    } else if (step.action?.type) {
      color = capabilityKind === 'service' ? '#0ea5e9' : getActionColor(step.action.type)
    }

    const stepStartStates = step.startStates || mapStartConditionsToStates(step.start_conditions || [])
    const stepDuringStates = step.duringStates || step.during_states || duringStates
    const stepDuringTargets = normalizeDuringStateTargets(
      step.duringStateTargets || step.during_state_targets,
      stepDuringStates
    )
    const stepSuccessStates = step.successStates || step.success_states || [defaultState]
    const stepFailureStates = step.failureStates || step.failure_states || [errorState]
    const stepPreStates = step.preStates || step.pre_states || []
    const outcomeTransitions = step.transition?.on_outcomes || []
    const outcomeEndStates = buildEndStatesFromOutcomes(outcomeTransitions, defaultState, errorState)
    let stepEndStates = step.endStates || step.end_states || (outcomeEndStates.length > 0
      ? outcomeEndStates
      : buildEndStates(stepSuccessStates, stepFailureStates, defaultState, errorState))

    if (outcomeTransitions.length > 0) {
      const enriched: EndStateConfig[] = [...stepEndStates]
      outcomeTransitions.forEach((transition, idx) => {
        const normalizedOutcome = normalizeOutcome(transition.outcome) || 'failed'
        const match = enriched.find(es => {
          const esOutcome = normalizeOutcome(es.outcome) || inferOutcome(es, idx)
          return esOutcome === normalizedOutcome &&
            (!transition.state || es.state === transition.state)
        })
        if (!match) {
          enriched.push({
            id: `end-outcome-${step.id}-${idx}`,
            state: transition.state || (normalizedOutcome === 'success' ? defaultState : errorState),
            label: transition.outcome.charAt(0).toUpperCase() + transition.outcome.slice(1),
            outcome: normalizedOutcome,
          })
        }
      })
      stepEndStates = enriched
    }

    const isTerminal = step.type === 'terminal'
    const nodeType = isTerminal ? 'event' : 'action'

    nodes.push({
      id: step.id,
      type: nodeType,
      position: { x, y },
      dragHandle: nodeType === 'action' ? ACTION_NODE_DRAG_HANDLE_SELECTOR : undefined,
      data: {
        label: step.name || step.id,
        subtype: isTerminal ? (step.terminal_type === 'success' ? 'End' : 'Error') : subtype,
        color,
        actionType,
        server: normalizedServer || step.action?.server,
        capabilityKind,
        startStates: stepStartStates,
        preStates: stepPreStates,
        duringStateTargets: stepDuringTargets,
        duringStates: stepDuringStates,
        successStates: stepSuccessStates,
        failureStates: stepFailureStates,
        endStates: stepEndStates,
        duringState: stepDuringStates[0],
        successState: stepSuccessStates[0] || defaultState,
        failureState: stepFailureStates[0] || errorState,
        params: step.action?.params?.data || {},
        fieldSources: step.action?.params?.field_sources,
        waypointId: step.action?.params?.waypoint_id,
        jobName: step.job_name || (isTerminal ? '' : getDefaultJobNameTemplate(normalizedServer || step.action?.server, step.name || step.id)),
        autoGenerateStates: step.auto_generate_states ?? true,
        finalState: isTerminal ? (step.terminal_type === 'success' ? defaultState : errorState) : undefined,
        preconditions: [],
        availableStates,
        availableAgents,
        isEditing: false,
      },
    })

    if (outcomeTransitions.length > 0) {
      outcomeTransitions.forEach((transition, idx) => {
        if (!transition.next) return
        const normalizedOutcome = normalizeOutcome(transition.outcome) || 'failed'
        const match = stepEndStates.find(es => {
          const esOutcome = normalizeOutcome(es.outcome) || inferOutcome(es, idx)
          return esOutcome === normalizedOutcome &&
            (!transition.state || es.state === transition.state)
        })
        const handleId = match ? match.id : `end-outcome-${step.id}-${idx}`
        const color = OUTCOME_EDGE_COLORS[normalizedOutcome]
        const isSuccess = normalizedOutcome === 'success'
        edges.push({
          id: `${step.id}-outcome-${idx}->${transition.next}`,
          source: step.id,
          target: transition.next,
          sourceHandle: handleId,
          targetHandle: 'state-in',
          type: 'smoothstep',
          markerEnd: { type: MarkerType.ArrowClosed, color },
          style: {
            stroke: color,
            strokeWidth: 2,
            strokeDasharray: isSuccess ? undefined : '5,5',
          },
        })
      })
    } else if (step.transition?.on_success || step.transition?.on_failure) {
      const findOutcomeHandle = (desired: 'success' | 'failure') => {
        const match = stepEndStates.find((endState, idx) => {
          const inferred = normalizeOutcome(endState.outcome) || inferOutcome(endState, idx)
          return outcomeCategory(inferred) === desired
        })
        return match?.id
      }

      const successHandle = findOutcomeHandle('success')
      const failureHandle = findOutcomeHandle('failure')

      if (step.transition?.on_success) {
        const target = typeof step.transition.on_success === 'string'
          ? step.transition.on_success
          : step.transition.on_success.next

        if (target) {
          edges.push({
            id: `${step.id}->${target}`,
            source: step.id,
            target,
            sourceHandle: successHandle,
            targetHandle: 'state-in',
            type: 'smoothstep',
            markerEnd: { type: MarkerType.ArrowClosed, color: '#22c55e' },
            style: { stroke: '#22c55e', strokeWidth: 2 },
          })
        }
      }

      if (step.transition?.on_failure) {
        const target = typeof step.transition.on_failure === 'string'
          ? step.transition.on_failure
          : step.transition.on_failure.fallback

        if (target) {
          edges.push({
            id: `${step.id}-fail->${target}`,
            source: step.id,
            target,
            sourceHandle: failureHandle,
            targetHandle: 'state-in',
            type: 'smoothstep',
            markerEnd: { type: MarkerType.ArrowClosed, color: '#ef4444' },
            style: { stroke: '#ef4444', strokeWidth: 2, strokeDasharray: '5,5' },
          })
        }
      }
    }
  })

  const entryPoint = preferredEntry && stepIds.has(preferredEntry)
    ? preferredEntry
    : steps[0]?.id
  if (entryPoint) {
    edges.push({
      id: `${START_NODE_ID}->${entryPoint}`,
      source: START_NODE_ID,
      target: entryPoint,
      sourceHandle: 'state-out',
      targetHandle: 'state-in',
      type: 'smoothstep',
      markerEnd: { type: MarkerType.ArrowClosed, color: START_NODE_COLOR },
      style: { stroke: START_NODE_COLOR, strokeWidth: 2 },
    })
  }

  return { initialNodes: nodes, initialEdges: edges }
}

function CreateTemplateModal({
  onClose,
  onCreated,
}: {
  onClose: () => void
  onCreated: (id: string) => void
}) {
  const [formData, setFormData] = useState({
    id: '',
    name: '',
    description: '',
  })
  const [error, setError] = useState('')

  const createTemplate = useMutation({
    mutationFn: (data: typeof formData) => templateApi.create({
      id: data.id,
      name: data.name,
      description: data.description || undefined,
      steps: [],
    }),
    onSuccess: () => onCreated(formData.id),
    onError: (err: any) => setError(err.response?.data?.detail || '태스크 생성에 실패했습니다'),
  })

  return (
    <div className="fixed inset-0 bg-black/70 flex items-center justify-center z-50">
      <div className="bg-surface rounded-xl shadow-2xl w-full max-w-md border border-primary">
        <div className="px-6 py-4 border-b border-primary flex items-center justify-between">
          <h2 className="text-lg font-semibold text-primary">새 태스크 생성</h2>
          <button onClick={onClose} className="text-muted hover:text-primary">
            <X size={20} />
          </button>
        </div>
        <form
          onSubmit={(e) => {
            e.preventDefault()
            if (!formData.id || !formData.name) {
              setError('ID and Name are required')
              return
            }
            createTemplate.mutate(formData)
          }}
          className="p-6 space-y-4"
        >
          {error && (
            <div className="p-3 bg-red-500/20 border border-red-500/50 rounded-lg text-red-400 text-sm">
              {error}
            </div>
          )}

          <div>
            <label className="block text-sm font-medium text-primary mb-1">태스크 ID</label>
            <input
              type="text"
              value={formData.id}
              onChange={e => setFormData(prev => ({ ...prev, id: e.target.value }))}
              placeholder="예: pick_and_place"
              className="w-full px-3 py-2 bg-elevated border border-primary rounded-lg text-primary placeholder-gray-600"
              required
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-primary mb-1">이름</label>
            <input
              type="text"
              value={formData.name}
              onChange={e => setFormData(prev => ({ ...prev, name: e.target.value }))}
              placeholder="예: Pick and Place"
              className="w-full px-3 py-2 bg-elevated border border-primary rounded-lg text-primary placeholder-gray-600"
              required
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-primary mb-1">설명</label>
            <textarea
              value={formData.description}
              onChange={e => setFormData(prev => ({ ...prev, description: e.target.value }))}
              placeholder="선택사항..."
              className="w-full px-3 py-2 bg-elevated border border-primary rounded-lg text-primary resize-none placeholder-gray-600"
              rows={2}
            />
          </div>

          <div className="flex justify-end gap-3 pt-4">
            <button type="button" onClick={onClose} className="px-4 py-2 text-secondary hover:text-primary">
              취소
            </button>
            <button
              type="submit"
              disabled={createTemplate.isPending}
              className="px-6 py-2 bg-blue-600 text-primary rounded-lg hover:bg-blue-700 disabled:opacity-50"
            >
              {createTemplate.isPending ? '생성 중...' : '태스크 생성'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

// Compatibility progress bar component
function CompatibilityBar({
  total,
  matched
}: {
  total: number
  matched: number
}) {
  const percentage = total > 0 ? (matched / total) * 100 : 100
  return (
    <div className="flex items-center gap-2">
      <div className="flex-1 h-1.5 bg-gray-700 rounded-full overflow-hidden">
        <div
          className={`h-full transition-all ${
            percentage === 100 ? 'bg-green-500' : percentage >= 50 ? 'bg-yellow-500' : 'bg-red-500'
          }`}
          style={{ width: `${percentage}%` }}
        />
      </div>
      <span className={`text-[10px] font-mono ${
        percentage === 100 ? 'text-green-400' : percentage >= 50 ? 'text-yellow-400' : 'text-red-400'
      }`}>
        {matched}/{total}
      </span>
    </div>
  )
}

function AssignTemplateModal({
  template,
  currentAssignments,
  allAgents,
  onAssign,
  onUnassign,
  onClose,
}: {
  template: ActionGraph
  currentAssignments: AssignmentInfo[]
  allAgents: Array<{ id: string; name: string; status: string }>
  onAssign: (agentId: string) => void
  onUnassign: (agentId: string) => void
  onClose: () => void
}) {
  console.log('[AssignTemplateModal] Rendering, template:', template?.id, template?.name)
  console.log('[AssignTemplateModal] currentAssignments:', currentAssignments)

  const assignedAgentIdSet = useMemo(
    () => new Set(currentAssignments.map((assignment) => assignment.agent_id)),
    [currentAssignments]
  )

  // Fetch compatible agents using capability-based API
  const { data: compatibleAgentsData, isLoading: compatibleLoading, error: compatibleError } = useQuery({
    queryKey: ['template-compatible-agents', template.id],
    queryFn: () => templateApi.getCompatibleAgents(template.id),
  })

  console.log('[AssignTemplateModal] compatibleAgentsData:', compatibleAgentsData)
  console.log('[AssignTemplateModal] compatibleLoading:', compatibleLoading)
  console.log('[AssignTemplateModal] compatibleError:', compatibleError)

  const requiredActionTypes = compatibleAgentsData?.required_action_types || []
  const compatibleAgents = compatibleAgentsData?.agents || []

  const mergedAgents = useMemo(() => {
    type ModalAgent = {
      agent_id: string
      agent_name: string
      status: string
      has_all_capabilities: boolean
      missing_capabilities: string[]
    }

    const byAgent = new Map<string, ModalAgent>()
    const compatibilityMap = new Map(compatibleAgents.map((agent) => [agent.agent_id, agent]))

    for (const agent of allAgents) {
      const compatibility = compatibilityMap.get(agent.id)
      const isAssigned = assignedAgentIdSet.has(agent.id)

      byAgent.set(agent.id, {
        agent_id: agent.id,
        agent_name: agent.name || agent.id,
        status: compatibility?.status || agent.status || 'offline',
        has_all_capabilities: compatibility?.has_all_capabilities ?? isAssigned,
        missing_capabilities: compatibility?.missing_capabilities || [],
      })
    }

    for (const compatibility of compatibleAgents) {
      if (byAgent.has(compatibility.agent_id)) continue
      byAgent.set(compatibility.agent_id, {
        agent_id: compatibility.agent_id,
        agent_name: compatibility.agent_name || compatibility.agent_id,
        status: compatibility.status || 'offline',
        has_all_capabilities: compatibility.has_all_capabilities,
        missing_capabilities: compatibility.missing_capabilities || [],
      })
    }

    for (const assignment of currentAssignments) {
      if (byAgent.has(assignment.agent_id)) continue
      byAgent.set(assignment.agent_id, {
        agent_id: assignment.agent_id,
        agent_name: assignment.agent_name || assignment.agent_id,
        status: 'offline',
        has_all_capabilities: true,
        missing_capabilities: [],
      })
    }

    return Array.from(byAgent.values())
  }, [allAgents, compatibleAgents, currentAssignments, assignedAgentIdSet])

  // Sort agents: assigned first, then compatible, then online, then by name
  const sortedAgents = [...mergedAgents].sort((a, b) => {
    const aAssigned = assignedAgentIdSet.has(a.agent_id)
    const bAssigned = assignedAgentIdSet.has(b.agent_id)
    if (aAssigned !== bAssigned) {
      return aAssigned ? -1 : 1
    }
    if (a.has_all_capabilities !== b.has_all_capabilities) {
      return a.has_all_capabilities ? -1 : 1
    }
    if (a.status !== b.status) {
      return a.status === 'online' ? -1 : 1
    }
    return formatTaskManagerName(a.agent_name).localeCompare(formatTaskManagerName(b.agent_name))
  })

  return (
    <div className="fixed inset-0 bg-black/70 flex items-center justify-center z-50">
      <div className="bg-surface rounded-xl shadow-2xl w-full max-w-lg border border-primary">
        <div className="px-6 py-4 border-b border-primary flex items-center justify-between">
          <div>
            <h2 className="text-lg font-semibold text-primary">태스크 할당</h2>
            <p className="text-sm text-muted mt-0.5">{template.name}</p>
          </div>
          <button onClick={onClose} className="text-muted hover:text-primary">
            <X size={20} />
          </button>
        </div>

        <div className="p-6">
          {/* Show required action types with checkmarks */}
          <div className="mb-5 p-4 bg-elevated rounded-lg border border-primary">
            <div className="text-xs text-secondary mb-3 font-medium uppercase tracking-wider">
              Required Action Types ({requiredActionTypes.length})
            </div>
            {requiredActionTypes.length > 0 ? (
              <div className="space-y-1.5">
                {requiredActionTypes.map(at => (
                  <div key={at} className="flex items-center gap-2 text-sm">
                    <Check size={14} className="text-purple-400" />
                    <span className="text-primary font-mono text-xs">{at}</span>
                  </div>
                ))}
              </div>
            ) : (
              <div className="flex items-center gap-2 text-sm text-muted">
                <AlertCircle size={14} />
                <span className="italic">이 태스크에 action이 없습니다. 할당하려면 action을 추가하세요.</span>
              </div>
            )}
          </div>

          {/* RTM list */}
          <div className="text-xs text-secondary mb-2 font-medium uppercase tracking-wider">
            RTMs ({sortedAgents.filter(a => a.has_all_capabilities).length} compatible)
          </div>

          <div className="space-y-2 max-h-72 overflow-y-auto">
            {compatibleLoading ? (
              <div className="text-center py-8 text-muted">Loading RTMs...</div>
            ) : sortedAgents.length === 0 ? (
              <div className="text-center py-8 text-muted">
                {requiredActionTypes.length === 0
                  ? '먼저 태스크에 action을 추가하세요'
                  : 'No RTMs registered yet'}
              </div>
            ) : (
              sortedAgents.map(agent => {
                const isAssigned = assignedAgentIdSet.has(agent.agent_id)
                const isCompatible = agent.has_all_capabilities
                const matchedCount = requiredActionTypes.length - (agent.missing_capabilities?.length || 0)

                return (
                  <div
                    key={agent.agent_id}
                    className={`p-3 rounded-lg border transition-colors ${
                      isAssigned
                        ? 'bg-green-500/10 border-green-500/30'
                        : isCompatible
                          ? 'bg-elevated border-primary hover:border-secondary'
                          : 'bg-elevated border-yellow-500/30'
                    }`}
                  >
                    <div className="flex items-center justify-between mb-2">
                      <div className="flex items-center gap-3">
                        <div className={`w-2 h-2 rounded-full ${
                          agent.status === 'online' ? 'bg-green-500' :
                          agent.status === 'warning' ? 'bg-yellow-500' : 'bg-gray-500'
                        }`} />
                        <div>
                          <div className="flex items-center gap-2">
                            <span className="text-sm text-primary font-medium">
                              {formatTaskManagerName(agent.agent_name) || formatTaskManagerName(agent.agent_id) || agent.agent_id}
                            </span>
                            {isCompatible ? (
                              <span className="text-[9px] px-1.5 py-0.5 bg-green-500/20 text-green-400 rounded flex items-center gap-1">
                                <Check size={10} />
                                Compatible
                              </span>
                            ) : (
                              <span className="text-[9px] px-1.5 py-0.5 bg-yellow-500/20 text-yellow-400 rounded flex items-center gap-1">
                                <AlertCircle size={10} />
                                Partial
                              </span>
                            )}
                          </div>
                        </div>
                      </div>
                      <button
                        onClick={() => isAssigned ? onUnassign(agent.agent_id) : onAssign(agent.agent_id)}
                        disabled={!isCompatible && !isAssigned}
                        className={`px-3 py-1.5 rounded-lg text-sm font-medium transition-colors flex items-center gap-1.5 ${
                          isAssigned
                            ? 'bg-red-500/20 text-red-400 hover:bg-red-500/30'
                            : isCompatible
                              ? 'bg-blue-600/20 text-blue-400 hover:bg-blue-600/30'
                              : 'bg-gray-500/10 text-muted cursor-not-allowed'
                        }`}
                      >
                        {isAssigned ? (
                          <>
                            <Unlink size={14} />
                            Unassign
                          </>
                        ) : (
                          <>
                            <Link2 size={14} />
                            Assign
                          </>
                        )}
                      </button>
                    </div>

                    {/* Compatibility progress bar */}
                    {requiredActionTypes.length > 0 && (
                      <div className="mb-2">
                        <CompatibilityBar total={requiredActionTypes.length} matched={matchedCount} />
                      </div>
                    )}

                    {/* Missing capabilities */}
                    {!isCompatible && (agent.missing_capabilities?.length || 0) > 0 && (
                      <div className="flex flex-wrap gap-1 mt-2">
                        {(agent.missing_capabilities || []).map(c => (
                          <span key={c} className="text-[9px] px-1.5 py-0.5 bg-red-500/20 text-red-400 rounded">
                            ✗ {c.split('/').pop()}
                          </span>
                        ))}
                      </div>
                    )}

                    {/* Assignment status */}
                    {isAssigned && (
                      <div className="mt-2 pt-2 border-t border-green-500/20">
                        {(() => {
                          const assignment = currentAssignments.find(a => a.agent_id === agent.agent_id)
                          return assignment && (
                            <div className="flex items-center gap-2 text-xs">
                              <DeploymentBadge status={assignment.deployment_status} />
                              <span className="text-muted">
                                v{assignment.server_version}
                                {assignment.deployed_version && ` (deployed: v${assignment.deployed_version})`}
                              </span>
                            </div>
                          )
                        })()}
                      </div>
                    )}
                  </div>
                )
              })
            )}
          </div>

          <div className="mt-4 pt-4 border-t border-primary flex justify-end">
            <button
              onClick={onClose}
              className="px-4 py-2 bg-surface text-primary rounded-lg hover:bg-elevated"
            >
              Done
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

function AddStateModal({
  stateDef,
  onClose,
  onAdded,
}: {
  stateDef: StateDefinition
  onClose: () => void
  onAdded: () => void
}) {
  const [newState, setNewState] = useState('')
  const [newStateColor, setNewStateColor] = useState<StateColorType>('neutral')
  const [error, setError] = useState('')
  const [localStates, setLocalStates] = useState<string[]>(stateDef.states)
  const [localColors, setLocalColors] = useState<Record<string, StateColorType>>(() => {
    const colors: Record<string, StateColorType> = {}
    stateDef.states.forEach(s => {
      colors[s] = getStateColorType(s)
    })
    return colors
  })
  const queryClient = useQueryClient()

  const updateStateDef = useMutation({
    mutationFn: async (updatedStates: string[]) => {
      return stateDefinitionApi.update(stateDef.id, {
        ...stateDef,
        states: updatedStates,
      })
    },
    onSuccess: () => {
      Object.assign(stateColorsMap, localColors)
      queryClient.invalidateQueries({ queryKey: ['state-definitions'] })
      onAdded()
    },
    onError: (err: any) => {
      setError(err.message || 'Failed to update states')
    },
  })

  const handleAddState = (e: React.FormEvent) => {
    e.preventDefault()
    const trimmed = newState.trim().toLowerCase().replace(/\s+/g, '_')

    if (!trimmed) {
      setError('State name is required')
      return
    }

    if (localStates.includes(trimmed)) {
      setError('State already exists')
      return
    }

    setLocalStates([...localStates, trimmed])
    setLocalColors({ ...localColors, [trimmed]: newStateColor })
    setNewState('')
    setNewStateColor('neutral')
    setError('')
  }

  const handleRemoveState = (stateToRemove: string) => {
    if (stateToRemove === stateDef.default_state) {
      setError(`Cannot remove default state "${stateToRemove}"`)
      return
    }
    setLocalStates(localStates.filter(s => s !== stateToRemove))
    const newColors = { ...localColors }
    delete newColors[stateToRemove]
    setLocalColors(newColors)
    setError('')
  }

  const handleColorChange = (state: string, colorType: StateColorType) => {
    setLocalColors({ ...localColors, [state]: colorType })
  }

  const handleSave = () => {
    if (localStates.length === 0) {
      setError('At least one state is required')
      return
    }
    updateStateDef.mutate(localStates)
  }

  const hasChanges = JSON.stringify(localStates) !== JSON.stringify(stateDef.states)

  return (
    <div className="fixed inset-0 bg-black/70 flex items-center justify-center z-50">
      <div className="bg-surface rounded-xl shadow-2xl w-full max-w-lg border border-primary">
        <div className="px-6 py-4 border-b border-primary flex items-center justify-between">
          <h2 className="text-lg font-semibold text-primary">Manage States</h2>
          <button onClick={onClose} className="text-muted hover:text-primary">
            <X size={20} />
          </button>
        </div>

        <div className="p-6 space-y-5">
          <div className="space-y-3">
            <label className="block text-xs text-secondary uppercase tracking-wider">Add New State</label>
            <form onSubmit={handleAddState} className="space-y-2">
              <div className="flex gap-2">
                <input
                  type="text"
                  value={newState}
                  onChange={e => {
                    setNewState(e.target.value)
                    setError('')
                  }}
                  placeholder="State name"
                  className="flex-1 px-3 py-2 bg-elevated border border-primary rounded-lg text-primary text-sm"
                  autoFocus
                />
                <button
                  type="submit"
                  className="px-4 py-2 bg-green-600 text-primary rounded-lg hover:bg-green-700 text-sm"
                >
                  Add
                </button>
              </div>
              <div className="flex items-center gap-3">
                <span className="text-xs text-muted">Color:</span>
                <div className="flex gap-2">
                  {(Object.keys(STATE_COLOR_OPTIONS) as StateColorType[]).map(colorType => (
                    <button
                      key={colorType}
                      type="button"
                      onClick={() => setNewStateColor(colorType)}
                      className={`flex items-center gap-1.5 px-2 py-1 rounded text-xs transition-all ${
                        newStateColor === colorType ? 'ring-2 ring-offset-1 ring-offset-[#16162a]' : 'opacity-60 hover:opacity-100'
                      }`}
                      style={{
                        backgroundColor: `${STATE_COLOR_OPTIONS[colorType].color}20`,
                        color: STATE_COLOR_OPTIONS[colorType].color,
                        boxShadow: newStateColor === colorType ? `0 0 0 2px ${STATE_COLOR_OPTIONS[colorType].color}` : 'none',
                      }}
                    >
                      <div className="w-2 h-2 rounded-full" style={{ backgroundColor: STATE_COLOR_OPTIONS[colorType].color }} />
                      {STATE_COLOR_OPTIONS[colorType].label}
                    </button>
                  ))}
                </div>
              </div>
            </form>
          </div>

          {error && <p className="text-red-400 text-sm">{error}</p>}

          <div>
            <label className="block text-xs text-secondary uppercase tracking-wider mb-3">
              States ({localStates.length})
            </label>
            <div className="space-y-2 max-h-48 overflow-y-auto">
              {localStates.map(state => (
                <div key={state} className="flex items-center justify-between px-3 py-2 bg-elevated rounded-lg">
                  <div className="flex items-center gap-3">
                    <div className="flex gap-1">
                      {(Object.keys(STATE_COLOR_OPTIONS) as StateColorType[]).map(colorType => (
                        <button
                          key={colorType}
                          onClick={() => handleColorChange(state, colorType)}
                          className={`w-4 h-4 rounded-full transition-all ${
                            localColors[state] === colorType ? 'scale-110' : 'opacity-40 hover:opacity-70'
                          }`}
                          style={{
                            backgroundColor: STATE_COLOR_OPTIONS[colorType].color,
                            boxShadow: localColors[state] === colorType
                              ? `0 0 0 2px #1a1a2e, 0 0 0 3px ${STATE_COLOR_OPTIONS[colorType].color}`
                              : 'none',
                          }}
                        />
                      ))}
                    </div>
                    <span className="text-sm text-primary">{state}</span>
                    {state === stateDef.default_state && (
                      <span className="text-[9px] px-1.5 py-0.5 bg-blue-500/20 text-blue-400 rounded">default</span>
                    )}
                  </div>
                  <button
                    onClick={() => handleRemoveState(state)}
                    className={`p-1 rounded ${
                      state === stateDef.default_state
                        ? 'text-muted cursor-not-allowed'
                        : 'text-muted hover:text-red-400 hover:bg-red-500/20'
                    }`}
                    disabled={state === stateDef.default_state}
                  >
                    <X size={14} />
                  </button>
                </div>
              ))}
            </div>
          </div>

          <div className="flex justify-between items-center pt-4 border-t border-primary">
            <span className={`text-sm ${hasChanges ? 'text-yellow-400' : 'text-muted'}`}>
              {hasChanges ? 'Unsaved changes' : 'No changes'}
            </span>
            <div className="flex gap-3">
              <button onClick={onClose} className="px-4 py-2 text-secondary hover:text-primary text-sm">
                Cancel
              </button>
              <button
                onClick={handleSave}
                disabled={updateStateDef.isPending || !hasChanges}
                className="px-4 py-2 bg-blue-600 text-primary rounded-lg hover:bg-blue-700 disabled:opacity-50 text-sm"
              >
                {updateStateDef.isPending ? 'Saving...' : 'Save'}
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

export default function ActionGraph() {
  return (
    <TelemetryProvider>
      <ReactFlowProvider>
        <ActionGraphEditor />
      </ReactFlowProvider>
    </TelemetryProvider>
  )
}
