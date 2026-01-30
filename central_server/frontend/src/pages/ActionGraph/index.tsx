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
  Trash2, Search, Zap, ChevronDown, ChevronRight, Server, Activity, Plus, PlusCircle, X,
  Cpu, FileCode, Users, Link2, Unlink, Check, AlertCircle, Clock, Layout, Save
} from 'lucide-react'
import { templateApi, stateDefinitionApi, agentApi, capabilityApi } from '../../api/client'
import type {
  ActionGraph, StateDefinition, ActionMapping,
  AssignmentInfo, AgentOverviewInfo, Agent, TemplateListItem,
  StartCondition, StartStateConfig, EndStateConfig, ActionOutcome, OutcomeTransition, DuringStateTarget
} from '../../types'

// State-based Node Components
import StateActionNode from './nodes/StateActionNode'
import StateEventNode from './nodes/StateEventNode'
import StateTransitionNode from './nodes/StateTransitionNode'
import DeletableEdge from './edges/DeletableEdge'

const nodeTypes = {
  action: StateActionNode,
  event: StateEventNode,
  transition: StateTransitionNode,
}

const edgeTypes = {
  deletable: DeletableEdge,
}

const START_NODE_ID = '__action_graph_start__'
const START_NODE_COLOR = '#22c55e'

// Color palette for different action types
const ACTION_COLORS: Record<string, string> = {
  'nav2_msgs/NavigateToPose': '#3b82f6',
  'control_msgs/FollowJointTrajectory': '#8b5cf6',
  'control_msgs/GripperCommand': '#f59e0b',
  'std_srvs/Trigger': '#06b6d4',
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
  return ACTION_COLORS[actionType] || '#6b7280'
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

let nodeId = 0
const getNodeId = () => `node_${nodeId++}`

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
  }[status] || { color: 'bg-gray-500/20 text-gray-400 border-gray-500/30', icon: Clock }

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
  const [viewMode, setViewMode] = useState<'templates' | 'assignments'>('templates')
  const [selectedTemplateId, setSelectedTemplateId] = useState<string | null>(null)
  const [expandedAgents, setExpandedAgents] = useState<string[]>([])

  // Modal state
  const [showCreateModal, setShowCreateModal] = useState(false)
  const [showAssignModal, setShowAssignModal] = useState(false)
  const [showAddStateModal, setShowAddStateModal] = useState(false)
  const [showCreateStateDefModal, setShowCreateStateDefModal] = useState(false)

  // Search
  const [searchTerm, setSearchTerm] = useState('')
  const [expandedCategories, setExpandedCategories] = useState<string[]>(['Discovered Actions', 'Configured Actions', 'States'])

  // Validation state
  const [validationErrors, setValidationErrors] = useState<Array<{ nodeId: string; nodeName: string; errors: string[] }>>([])

  // ReactFlow
  const reactFlowWrapper = useRef<HTMLDivElement>(null)
  const { screenToFlowPosition } = useReactFlow()
  const queryClient = useQueryClient()

  // Fetch all agents for capability-based workflow
  const { data: agents = [] } = useQuery({
    queryKey: ['agents-list'],
    queryFn: () => agentApi.list(),
  })

  const availableAgents = useMemo(
    () => agents
      .filter(agent => agent.status !== 'offline')
      .map(agent => ({ id: agent.id, name: agent.name })),
    [agents]
  )

  // Fetch all discovered capabilities across the fleet
  const { data: fleetCapabilities } = useQuery({
    queryKey: ['fleet-capabilities'],
    queryFn: () => capabilityApi.listAll(),
  })

  // Fetch all templates (capability-based - no type filtering)
  const { data: allTemplates = [], isLoading: templatesLoading } = useQuery({
    queryKey: ['templates-all'],
    queryFn: () => templateApi.list(),
  })

  // Fetch agents overview (agents with assignments)
  const { data: agentsOverview = [], isLoading: agentsLoading } = useQuery({
    queryKey: ['agents-overview'],
    queryFn: () => templateApi.getAgentsOverview(),
  })

  // Fetch selected template
  const { data: selectedTemplate } = useQuery({
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
      queryClient.invalidateQueries({ queryKey: ['templates'] })
      setSelectedTemplateId(null)
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
    }: {
      templateId: string
      steps: ActionGraph['steps']
      entryPoint?: string
      states?: ActionGraph['states']
    }) => {
      const payload: Partial<ActionGraph> = { steps }
      if (entryPoint) {
        payload.entry_point = entryPoint
      }
      if (states && states.length > 0) {
        payload.states = states
      }
      return templateApi.update(templateId, payload)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['template', selectedTemplateId] })
      queryClient.invalidateQueries({ queryKey: ['templates-all'] })
    },
  })

  // Available states from state definition (with default fallback)
  // Memoized to prevent infinite re-render loops
  const availableStates = useMemo(() => {
    const DEFAULT_STATES = ['idle', 'busy', 'error', 'completed', 'waiting']
    return selectedStateDef?.states?.length ? selectedStateDef.states : DEFAULT_STATES
  }, [selectedStateDef?.states])

  // Build node palette
  const nodePalette = useMemo(() => {
    const palette: Array<{
      category: string
      icon: React.ReactNode
      items: Array<{
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
      }>
    }> = []

    // Use discovered action servers from the fleet if available (individual servers, not grouped by type)
    if (fleetCapabilities && fleetCapabilities.action_servers && fleetCapabilities.action_servers.length > 0) {
      palette.push({
        category: 'Discovered Actions',
        icon: <Server className="w-3.5 h-3.5" />,
        items: fleetCapabilities.action_servers.map((srv) => ({
          type: 'action',
          subtype: srv.action_server,  // Use action_server path as subtype (unique identifier)
          label: srv.action_server.replace(/^\//, ''),  // Display without leading slash
          color: getActionColor(srv.action_type),
          actionType: srv.action_type,
          server: srv.action_server,  // Store full server path
          agentName: srv.agent_name,
          isAvailable: srv.is_available,
        })),
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
        })),
      })
    }

    // States are always available (default fallback if no state definition)
    const stateItems = availableStates.map(state => ({
      type: 'transition',
      subtype: state,
      label: state,
      color: getStateColor(state),
    }))

    stateItems.push({
      type: 'event',
      subtype: 'End',
      label: 'End (Success)',
      color: '#3b82f6',
    })
    stateItems.push({
      type: 'event',
      subtype: 'Error',
      label: 'End (Error)',
      color: '#ef4444',
    })

    palette.push({
      category: 'States',
      icon: <Activity className="w-3.5 h-3.5" />,
      items: stateItems,
    })

    return palette
  }, [selectedStateDef, availableStates, fleetCapabilities])

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

  // Only reset graph when template changes (by ID), not when state definition loads
  // This prevents newly added nodes from being lost when async data loads
  const templateId = selectedTemplate?.id
  useEffect(() => {
    if (selectedTemplate && templateId) {
      const { initialNodes, initialEdges } = convertActionGraphToGraph(selectedTemplate, selectedStateDef, availableStates, availableAgents)
      setNodes(initialNodes)
      setEdges(initialEdges)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [templateId]) // Only depend on template ID change

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
      const selfDuringTarget = normalizedDuringTargets.find(target =>
        (!target.target_type || target.target_type === 'self' || target.target_type === 'all') &&
        target.state
      )
      const selfDuringStates = selfDuringTarget?.state ? [selfDuringTarget.state] : []

      const step: ActionGraph['steps'][0] = {
        id: node.id,
        name: node.data.label,
        // Job name for this step (user-defined name)
        job_name: node.data.jobName || undefined,
        auto_generate_states: node.data.autoGenerateStates || undefined,
        // Regular action steps don't need type set (only 'fallback' or 'terminal' use type)
        action: {
          type: node.data.actionType || node.data.subtype,
          server: node.data.server,
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

    const { steps, entryPoint, generatedStates } = convertGraphToSteps()
    saveTemplate.mutate({ templateId: selectedTemplateId, steps, entryPoint, states: generatedStates })
  }, [selectedTemplateId, convertGraphToSteps, saveTemplate, validateGraph])

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
      setEdges((eds) => addEdge(newEdge, eds))
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

      const newNode: Node = {
        id: getNodeId(),
        type: data.type,
        position,
        data: {
          label: data.label,
          subtype: data.subtype,
          color: data.color,
          actionType: data.actionType,
          server: data.server,
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
          // Auto-generate states feature (enabled by default)
          autoGenerateStates: true,
          generatedStates: [],
          // Default end states for handle rendering
          endStates: defaultEndStates,
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
    [screenToFlowPosition, setNodes, availableStates, availableAgents]
  )

  const onDragStart = (event: React.DragEvent<HTMLDivElement>, item: any) => {
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

  const toggleAgent = (agentId: string) => {
    setExpandedAgents(prev =>
      prev.includes(agentId) ? prev.filter(a => a !== agentId) : [...prev, agentId]
    )
  }

  const filteredPalette = nodePalette.map(cat => ({
    ...cat,
    items: cat.items.filter(item =>
      item.label.toLowerCase().includes(searchTerm.toLowerCase()) ||
      item.subtype.toLowerCase().includes(searchTerm.toLowerCase())
    )
  })).filter(cat => cat.items.length > 0)

  return (
    <div className="h-screen flex bg-[#0f0f1a]">
      {/* Left Sidebar - Templates/Assignments Navigation */}
      <div className="w-80 bg-[#16162a] border-r border-[#2a2a4a] flex flex-col">
        {/* View Mode Toggle */}
        <div className="p-3 border-b border-[#2a2a4a]">
          <div className="flex bg-[#1a1a2e] rounded-lg p-0.5">
            <button
              onClick={() => setViewMode('templates')}
              className={`flex-1 flex items-center justify-center gap-2 py-2 rounded-md text-sm font-medium transition-colors ${
                viewMode === 'templates'
                  ? 'bg-blue-600 text-white'
                  : 'text-gray-400 hover:text-white'
              }`}
            >
              <FileCode size={16} />
              Templates
            </button>
            <button
              onClick={() => setViewMode('assignments')}
              className={`flex-1 flex items-center justify-center gap-2 py-2 rounded-md text-sm font-medium transition-colors ${
                viewMode === 'assignments'
                  ? 'bg-blue-600 text-white'
                  : 'text-gray-400 hover:text-white'
              }`}
            >
              <Users size={16} />
              Assignments
            </button>
          </div>
        </div>

        {/* Content based on view mode */}
        <div className="flex-1 overflow-y-auto">
          {viewMode === 'templates' ? (
            // Templates View - Capability-based (flat list)
            <div className="py-2">
              <div className="px-3 py-2 flex items-center justify-between">
                <span className="text-xs font-semibold text-gray-400 uppercase tracking-wider">
                  템플릿
                </span>
                <button
                  onClick={() => setShowCreateModal(true)}
                  className="p-1 text-blue-400 hover:bg-blue-500/20 rounded"
                  title="새 템플릿 생성"
                >
                  <PlusCircle size={14} />
                </button>
              </div>

              {templatesLoading ? (
                <div className="px-3 py-4 text-center text-gray-500 text-sm">로딩 중...</div>
              ) : allTemplates.length === 0 ? (
                <div className="px-3 py-4 text-center text-gray-500 text-sm">
                  템플릿이 없습니다. 새로 생성하세요.
                </div>
              ) : (
                <div className="space-y-0.5">
                  {allTemplates.map((template: TemplateListItem) => (
                    <div
                      key={template.id}
                      onClick={() => setSelectedTemplateId(template.id)}
                      className={`w-full px-3 py-2.5 flex items-start gap-2 text-left transition-colors cursor-pointer group ${
                        selectedTemplateId === template.id
                          ? 'bg-blue-600/20 text-blue-400 border-l-2 border-blue-500'
                          : 'text-gray-400 hover:bg-[#1a1a2e] hover:text-white'
                      }`}
                    >
                      <FileCode size={16} className="mt-0.5 flex-shrink-0" />
                      <div className="flex-1 min-w-0">
                        <div className="text-sm font-medium truncate">{template.name}</div>
                        {/* Show required action types as capability tags */}
                        {template.required_action_types && template.required_action_types.length > 0 ? (
                          <div className="flex flex-wrap gap-1 mt-1">
                            {template.required_action_types.slice(0, 2).map(at => (
                              <span key={at} className="text-[9px] px-1.5 py-0.5 bg-purple-500/20 text-purple-400 rounded">
                                {at.split('/').pop()}
                              </span>
                            ))}
                            {template.required_action_types.length > 2 && (
                              <span className="text-[9px] px-1.5 py-0.5 bg-gray-500/20 text-gray-500 rounded">
                                +{template.required_action_types.length - 2}
                              </span>
                            )}
                          </div>
                        ) : (
                          <span className="text-[10px] text-gray-600 italic">액션 없음</span>
                        )}
                      </div>
                      <div className="flex items-center gap-1 flex-shrink-0">
                        <span className="text-[10px] text-gray-600">
                          {template.assignment_count} agent{template.assignment_count !== 1 ? 's' : ''}
                        </span>
                        <button
                          onClick={(e) => {
                            e.stopPropagation()
                            if (confirm(`Delete template "${template.name}"?`)) {
                              deleteTemplate.mutate(template.id)
                            }
                          }}
                          className="p-1 text-gray-600 hover:text-red-400 hover:bg-red-500/20 rounded opacity-0 group-hover:opacity-100 transition-opacity"
                          title="템플릿 삭제"
                        >
                          <Trash2 size={14} />
                        </button>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          ) : (
            // Assignments View - By Agent
            <div className="py-2">
              <div className="px-3 py-2">
                <span className="text-xs font-semibold text-gray-400 uppercase tracking-wider">
                  에이전트 & 할당
                </span>
              </div>

              {agentsLoading ? (
                <div className="px-3 py-4 text-center text-gray-500 text-sm">로딩 중...</div>
              ) : agentsOverview.length === 0 ? (
                <div className="px-3 py-4 text-center text-gray-500 text-sm">
                  연결된 에이전트 없음
                </div>
              ) : (
                agentsOverview.map((agent: AgentOverviewInfo) => {
                  const actionServers = agent.action_servers || []
                  const actionTypes = agent.action_types || []
                  const assignedTemplates = agent.assigned_templates || []

                  return (
                  <div key={agent.agent_id} className="border-b border-[#2a2a4a]/50">
                    <button
                      onClick={() => toggleAgent(agent.agent_id)}
                      className="w-full px-3 py-2.5 flex items-center gap-2 hover:bg-[#1a1a2e] transition-colors"
                    >
                      {expandedAgents.includes(agent.agent_id) ? (
                        <ChevronDown size={14} className="text-gray-500" />
                      ) : (
                        <ChevronRight size={14} className="text-gray-500" />
                      )}
                      <Cpu size={16} className="text-green-400" />
                      <span className="flex-1 text-left text-sm text-white">{agent.agent_name}</span>
                      <span className={`w-2 h-2 rounded-full ${
                        agent.status === 'online' ? 'bg-green-500' :
                        agent.status === 'warning' ? 'bg-yellow-500' : 'bg-red-500'
                      }`} />
                    </button>

                    {/* Expanded: Show capabilities and assigned templates */}
                    {expandedAgents.includes(agent.agent_id) && (
                      <div className="bg-[#0f0f1a]/50">
                        {/* Action Servers (individual) */}
                        {actionServers.length > 0 ? (
                          <div className="pl-8 pr-3 py-2 border-t border-[#2a2a4a]/30">
                            <div className="flex flex-wrap gap-1">
                              {actionServers.slice(0, 4).map(srv => (
                                <span
                                  key={srv.action_server}
                                  className={`text-[9px] px-1.5 py-0.5 rounded font-mono ${
                                    srv.is_available
                                      ? 'bg-purple-500/20 text-purple-400'
                                      : 'bg-gray-500/20 text-gray-500'
                                  }`}
                                  title={`${srv.action_type} - ${srv.status}`}
                                >
                                  {srv.action_server.replace(/^\//, '')}
                                </span>
                              ))}
                              {actionServers.length > 4 && (
                                <span className="text-[9px] px-1.5 py-0.5 bg-gray-500/20 text-gray-500 rounded">
                                  +{actionServers.length - 4}
                                </span>
                              )}
                            </div>
                          </div>
                        ) : actionTypes.length > 0 && (
                          /* Fallback to action_types if action_servers not available */
                          <div className="pl-8 pr-3 py-2 border-t border-[#2a2a4a]/30">
                            <div className="flex flex-wrap gap-1">
                              {actionTypes.slice(0, 4).map(at => (
                                <span key={at} className="text-[9px] px-1.5 py-0.5 bg-purple-500/20 text-purple-400 rounded">
                                  {at.split('/').pop()}
                                </span>
                              ))}
                              {actionTypes.length > 4 && (
                                <span className="text-[9px] px-1.5 py-0.5 bg-gray-500/20 text-gray-500 rounded">
                                  +{actionTypes.length - 4}
                                </span>
                              )}
                            </div>
                          </div>
                        )}
                        {/* Assigned templates */}
                        {assignedTemplates.length === 0 ? (
                          <div className="pl-10 pr-3 py-2 text-xs text-gray-600 italic border-t border-[#2a2a4a]/30">
                            No templates assigned
                          </div>
                        ) : (
                          assignedTemplates.map(at => (
                            <button
                              key={at.assignment_id}
                              onClick={() => setSelectedTemplateId(at.template_id)}
                              className={`w-full pl-10 pr-3 py-1.5 flex items-center gap-2 text-left transition-colors border-t border-[#2a2a4a]/30 ${
                                selectedTemplateId === at.template_id
                                  ? 'bg-blue-600/20 text-blue-400'
                                  : 'text-gray-400 hover:bg-[#1a1a2e] hover:text-white'
                              }`}
                            >
                              <Link2 size={12} className="text-gray-500" />
                              <span className="flex-1 text-xs truncate">{at.template_name}</span>
                              <DeploymentBadge status={at.status} />
                            </button>
                          ))
                        )}
                      </div>
                    )}
                  </div>
                )})
              )}
            </div>
          )}
        </div>

        {/* Selected Template Info */}
        {selectedTemplate && (
          <div className="border-t border-[#2a2a4a] p-3 bg-[#1a1a2e]/50">
            <div className="flex items-center justify-between mb-2">
              <span className="text-xs font-semibold text-gray-400 uppercase">선택된 템플릿</span>
              <button
                onClick={() => setShowAssignModal(true)}
                className="text-xs px-2 py-1 bg-blue-600/20 text-blue-400 rounded hover:bg-blue-600/30 flex items-center gap-1"
              >
                <Link2 size={12} />
                Assign
              </button>
            </div>
            <div className="text-sm text-white font-medium truncate">{selectedTemplate.name}</div>
            <div className="text-xs text-gray-500 mt-1">
              v{selectedTemplate.version}
            </div>
            {templateAssignments.length > 0 && (
              <div className="mt-2 pt-2 border-t border-[#2a2a4a]/50">
                <div className="text-xs text-gray-500 mb-1">
                  Assigned to {templateAssignments.length} agent(s)
                </div>
                <div className="flex flex-wrap gap-1">
                  {templateAssignments.slice(0, 3).map(a => (
                    <span key={a.id} className="text-[10px] px-1.5 py-0.5 bg-green-500/20 text-green-400 rounded">
                      {a.agent_name}
                    </span>
                  ))}
                  {templateAssignments.length > 3 && (
                    <span className="text-[10px] px-1.5 py-0.5 bg-gray-500/20 text-gray-400 rounded">
                      +{templateAssignments.length - 3} more
                    </span>
                  )}
                </div>
              </div>
            )}

            {/* Validation Errors Panel */}
            {validationErrors.length > 0 && (
              <div className="mt-2 pt-2 border-t border-[#2a2a4a]/50">
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

      {/* Middle: Node Palette (when template selected) */}
      {selectedTemplate && (
        <div className="w-56 bg-[#16162a] border-r border-[#2a2a4a] flex flex-col">
          {/* States Management */}
          <div className="px-3 py-3 border-b border-[#2a2a4a]">
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
                    <span className="px-2 py-0.5 rounded text-[10px] text-gray-500">
                      +{availableStates.length - 5}
                    </span>
                  )}
                </div>
              </>
            ) : (
              <>
                <button
                  onClick={() => setShowCreateStateDefModal(true)}
                  className="w-full flex items-center justify-center gap-2 px-3 py-2 bg-gradient-to-r from-blue-600/20 to-blue-500/10 border border-blue-500/40 rounded-lg text-blue-400 hover:from-blue-600/30 hover:to-blue-500/20 hover:border-blue-500/60 transition-all"
                >
                  <Plus size={14} />
                  <span className="text-xs font-medium">상태 정의 생성</span>
                </button>
                <div className="mt-2 text-[10px] text-gray-500">
                  상태 정의가 없습니다. 상태 관리를 위해 생성하세요.
                </div>
              </>
            )}
          </div>

          {/* Search */}
          <div className="p-3 border-b border-[#2a2a4a]">
            <div className="relative">
              <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-gray-500" />
              <input
                type="text"
                placeholder="검색..."
                value={searchTerm}
                onChange={(e) => setSearchTerm(e.target.value)}
                className="w-full pl-8 pr-3 py-1.5 bg-[#1a1a2e] border border-[#2a2a4a] rounded-lg text-xs text-white placeholder-gray-500 focus:outline-none focus:border-blue-500"
              />
            </div>
          </div>

          {/* Node Palette */}
          <div className="flex-1 overflow-y-auto">
            {filteredPalette.length === 0 ? (
              <div className="p-4 text-center text-gray-500 text-xs">No nodes found</div>
            ) : (
              filteredPalette.map(category => (
                <div key={category.category}>
                  <button
                    onClick={() => toggleCategory(category.category)}
                    className="w-full px-3 py-2 flex items-center justify-between text-[10px] font-semibold text-gray-400 uppercase tracking-wider hover:bg-[#1a1a2e]"
                  >
                    <div className="flex items-center gap-1.5">
                      {category.icon}
                      <span>{category.category}</span>
                    </div>
                    {expandedCategories.includes(category.category) ? (
                      <ChevronDown className="w-3.5 h-3.5" />
                    ) : (
                      <ChevronRight className="w-3.5 h-3.5" />
                    )}
                  </button>
                  {expandedCategories.includes(category.category) && (
                    <div className="px-2 pb-2 space-y-0.5">
                      {category.category === 'States' ? (
                        <>
                          <div className="px-2 py-1 text-[9px] text-gray-600 italic">Reference only</div>
                          {category.items.map((item, idx) => (
                            <div
                              key={`${item.subtype}-${idx}`}
                              className="flex items-center gap-2 px-2 py-1.5 rounded hover:bg-[#1a1a2e] transition-colors"
                            >
                              <div
                                className="w-2.5 h-2.5 rounded-full flex-shrink-0"
                                style={{ backgroundColor: item.color }}
                              />
                              <span className="text-xs text-gray-400">{item.label}</span>
                            </div>
                          ))}
                        </>
                      ) : (
                        category.items.map((item, idx) => (
                          <div
                            key={`${item.subtype}-${idx}`}
                            draggable={true}
                            onDragStart={(e) => {
                              e.stopPropagation()
                              onDragStart(e, item)
                            }}
                            className={`flex items-center gap-2 px-2 py-1.5 rounded cursor-grab active:cursor-grabbing hover:bg-[#2a2a4a] transition-colors border border-transparent hover:border-[#3a3a5a] ${
                              item.isAvailable === false ? 'opacity-50' : ''
                            }`}
                          >
                            <div
                              className="w-2.5 h-2.5 rounded-sm flex-shrink-0"
                              style={{ backgroundColor: item.color }}
                            />
                            <div className="flex-1 min-w-0">
                              <span className="text-xs text-gray-300 block truncate">{item.label}</span>
                              {item.duringState && (
                                <span className="text-[9px] text-yellow-500">{item.duringState}</span>
                              )}
                              {item.robotCount !== undefined && (
                                <span className="text-[9px] text-blue-400">{item.robotCount} robot{item.robotCount !== 1 ? 's' : ''}</span>
                              )}
                              {item.agentName && (
                                <span className="text-[9px] text-cyan-400">{item.agentName}</span>
                              )}
                              {item.actionType && !item.duringState && !item.robotCount && (
                                <span className="text-[9px] text-purple-400">{item.actionType.split('/').pop()}</span>
                              )}
                            </div>
                            {item.isAvailable === false && (
                              <span className="text-[8px] text-red-400">busy</span>
                            )}
                          </div>
                        ))
                      )}
                    </div>
                  )}
                </div>
              ))
            )}
          </div>
        </div>
      )}

      {/* Main Canvas */}
      <div className="flex-1 flex flex-col">
        {/* Toolbar */}
        <div className="h-12 bg-[#16162a] border-b border-[#2a2a4a] flex items-center justify-between px-4">
          <div className="flex items-center gap-2">
            <Zap className="w-5 h-5 text-blue-400" />
            <span className="font-semibold text-white">Action Graph Templates</span>
            {selectedTemplate && (
              <>
                <span className="text-gray-500">/</span>
                <FileCode className="w-4 h-4 text-blue-400" />
                <span className="text-blue-400 text-sm">{selectedTemplate.name}</span>
                <span className="text-gray-500 text-xs ml-2">v{selectedTemplate.version}</span>
              </>
            )}
          </div>
          {selectedTemplate && (
            <div className="flex items-center gap-2">
              {/* Validation Errors Indicator */}
              {validationErrors.length > 0 && (
                <div className="flex items-center gap-1.5 px-2 py-1 bg-yellow-600/20 text-yellow-400 rounded-lg text-xs">
                  <AlertCircle size={12} />
                  <span>{validationErrors.length}개 문제 발견</span>
                </div>
              )}
              <button
                onClick={handleSave}
                disabled={saveTemplate.isPending}
                className="flex items-center gap-1.5 px-3 py-1.5 bg-green-600/20 text-green-400 rounded-lg hover:bg-green-600/30 text-sm disabled:opacity-50"
              >
                <Save size={14} />
                {saveTemplate.isPending ? '저장 중...' : '저장'}
              </button>
              <button
                onClick={() => {
                  console.log('[Assign Button] Clicked, selectedTemplate:', selectedTemplate?.id, selectedTemplate?.name)
                  setShowAssignModal(true)
                }}
                className="flex items-center gap-1.5 px-3 py-1.5 bg-blue-600/20 text-blue-400 rounded-lg hover:bg-blue-600/30 text-sm"
              >
                <Link2 size={14} />
                Assign to Agent
              </button>
              <button
                onClick={() => {
                  if (confirm(`Delete template "${selectedTemplate.name}"?`)) {
                    deleteTemplate.mutate(selectedTemplate.id)
                  }
                }}
                className="p-1.5 text-red-400 hover:bg-red-500/20 rounded-md transition-colors"
              >
                <Trash2 size={16} />
              </button>
            </div>
          )}
        </div>

        {/* Canvas or Welcome */}
        <div ref={reactFlowWrapper} className="flex-1">
          {selectedTemplate ? (
            <ReactFlow
              nodes={nodes}
              edges={edgesWithDelete}
              onNodesChange={onNodesChange}
              onEdgesChange={onEdgesChange}
              onConnect={onConnect}
              onConnectStart={onConnectStart}
              onConnectEnd={onConnectEnd}
              onDrop={onDrop}
              onDragOver={onDragOver}
              nodeTypes={nodeTypes}
              edgeTypes={edgeTypes}
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
              <Controls className="!bg-[#16162a] !border-[#2a2a4a] !rounded-lg [&>button]:!bg-[#16162a] [&>button]:!border-[#2a2a4a] [&>button]:!text-white [&>button:hover]:!bg-[#2a2a4a]" />
              <Panel position="bottom-center" className="mb-4">
                <div className="bg-[#16162a]/90 backdrop-blur-sm px-4 py-2 rounded-lg border border-[#2a2a4a] text-xs text-gray-400">
                  Drag action servers to canvas to build workflow
                </div>
              </Panel>
            </ReactFlow>
          ) : (
            <div className="h-full flex items-center justify-center">
              <div className="text-center">
                <Layout className="w-16 h-16 mx-auto mb-4 text-gray-700" />
                <h2 className="text-xl font-semibold text-gray-400 mb-2">템플릿을 선택하세요</h2>
                <p className="text-gray-600 text-sm max-w-md">
                  왼쪽 패널에서 템플릿을 선택하여 워크플로우를 확인하고 편집하거나,
                  새 템플릿을 생성하세요.
                </p>
                <button
                  onClick={() => setShowCreateModal(true)}
                  className="mt-4 px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 inline-flex items-center gap-2"
                >
                  <Plus size={16} />
                  Create New Template
                </button>
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Create Template Modal */}
      {showCreateModal && (
        <CreateTemplateModal
          agents={agents}
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
            onAssign={(agentId) => assignTemplate.mutate({ templateId: selectedTemplate.id, agentId })}
            onUnassign={(agentId) => unassignTemplate.mutate({ templateId: selectedTemplate.id, agentId })}
            onClose={() => setShowAssignModal(false)}
          />
        ) : (
          // Debug: Modal was triggered but no template selected
          <div className="fixed inset-0 bg-black/70 flex items-center justify-center z-50">
            <div className="bg-[#16162a] rounded-xl shadow-2xl p-6 border border-[#2a2a4a]">
              <p className="text-white mb-4">Error: No template selected</p>
              <button
                onClick={() => setShowAssignModal(false)}
                className="px-4 py-2 bg-blue-600 text-white rounded"
              >
                Close
              </button>
            </div>
          </div>
        )
      )}

      {/* Create State Definition Modal */}
      {showCreateStateDefModal && (
        <CreateStateDefinitionModal
          onClose={() => setShowCreateStateDefModal(false)}
          onCreated={() => {
            setShowCreateStateDefModal(false)
            refetchStateDef()
          }}
        />
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

  const actionMappings = stateDef?.action_mappings || []
  const defaultState = availableStates.includes('idle') ? 'idle' : availableStates[0] || 'idle'
  const errorState = availableStates.includes('error') ? 'error' : availableStates[availableStates.length - 1] || 'error'
  const stepIds = new Set(actionGraph.steps.map(step => step.id))
  const preferredEntry = actionGraph.entry_point || actionGraph.steps[0]?.id

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
    },
    draggable: false,
    selectable: false,
    deletable: false,
  })

  actionGraph.steps.forEach((step, index) => {
    const x = 300 + (index % 3) * 300
    const y = 100 + Math.floor(index / 3) * 200

    let subtype = step.action?.server || step.action?.type || 'Unknown'
    let color = '#6b7280'
    let actionType = step.action?.type
    let duringStates: string[] = []

    const mapping = actionMappings.find(m => m.action_type === step.action?.type) ||
      actionMappings.find(m => m.server === step.action?.server)
    if (mapping) {
      color = getActionColor(mapping.action_type)
      actionType = mapping.action_type
      duringStates = mapping.during_states || (mapping.during_state ? [mapping.during_state] : [])
    } else if (step.action?.type) {
      color = getActionColor(step.action.type)
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
      data: {
        label: step.name || step.id,
        subtype: isTerminal ? (step.terminal_type === 'success' ? 'End' : 'Error') : subtype,
        color,
        actionType,
        server: step.action?.server,
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
        jobName: step.job_name || '',
        autoGenerateStates: step.auto_generate_states ?? true,
        finalState: isTerminal ? (step.terminal_type === 'success' ? defaultState : errorState) : undefined,
        preconditions: [],
        availableStates,
        availableAgents,
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
    : actionGraph.steps[0]?.id
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
  agents,
  onClose,
  onCreated,
}: {
  agents: Agent[]
  onClose: () => void
  onCreated: (id: string) => void
}) {
  const [formData, setFormData] = useState({
    id: '',
    name: '',
    description: '',
    baseAgentId: '', // Optional: base template on agent's capabilities
  })
  const [error, setError] = useState('')

  // Fetch capabilities for selected agent (if any)
  const { data: agentCapabilities } = useQuery({
    queryKey: ['agent-capabilities', formData.baseAgentId],
    queryFn: () => agentApi.getCapabilities(formData.baseAgentId),
    enabled: !!formData.baseAgentId,
  })

  const createTemplate = useMutation({
    mutationFn: (data: typeof formData) => templateApi.create({
      id: data.id,
      name: data.name,
      description: data.description || undefined,
      steps: [],
    }),
    onSuccess: () => onCreated(formData.id),
    onError: (err: any) => setError(err.response?.data?.detail || 'Failed to create template'),
  })

  return (
    <div className="fixed inset-0 bg-black/70 flex items-center justify-center z-50">
      <div className="bg-[#16162a] rounded-xl shadow-2xl w-full max-w-md border border-[#2a2a4a]">
        <div className="px-6 py-4 border-b border-[#2a2a4a] flex items-center justify-between">
          <h2 className="text-lg font-semibold text-white">Create New Template</h2>
          <button onClick={onClose} className="text-gray-500 hover:text-white">
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
            <label className="block text-sm font-medium text-gray-300 mb-1">템플릿 ID</label>
            <input
              type="text"
              value={formData.id}
              onChange={e => setFormData(prev => ({ ...prev, id: e.target.value }))}
              placeholder="예: pick_and_place"
              className="w-full px-3 py-2 bg-[#1a1a2e] border border-[#2a2a4a] rounded-lg text-white placeholder-gray-600"
              required
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-300 mb-1">이름</label>
            <input
              type="text"
              value={formData.name}
              onChange={e => setFormData(prev => ({ ...prev, name: e.target.value }))}
              placeholder="예: Pick and Place"
              className="w-full px-3 py-2 bg-[#1a1a2e] border border-[#2a2a4a] rounded-lg text-white placeholder-gray-600"
              required
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-300 mb-1">설명</label>
            <textarea
              value={formData.description}
              onChange={e => setFormData(prev => ({ ...prev, description: e.target.value }))}
              placeholder="선택사항..."
              className="w-full px-3 py-2 bg-[#1a1a2e] border border-[#2a2a4a] rounded-lg text-white resize-none placeholder-gray-600"
              rows={2}
            />
          </div>

          {/* Optional: Base on Agent for capability reference */}
          <div>
            <label className="block text-sm font-medium text-gray-300 mb-1">
              기준 에이전트 <span className="text-gray-500 font-normal">(선택사항)</span>
            </label>
            <select
              value={formData.baseAgentId}
              onChange={e => setFormData(prev => ({ ...prev, baseAgentId: e.target.value }))}
              className="w-full px-3 py-2 bg-[#1a1a2e] border border-[#2a2a4a] rounded-lg text-white"
            >
              <option value="">-- 사용 가능한 액션 확인을 위해 선택 --</option>
              {agents.map(agent => (
                <option key={agent.id} value={agent.id}>
                  {agent.name}
                </option>
              ))}
            </select>
            <p className="text-xs text-gray-500 mt-1">
              에이전트를 선택하면 템플릿에서 사용 가능한 액션 타입을 확인할 수 있습니다.
            </p>
          </div>

          {/* Show agent's action servers if selected */}
          {formData.baseAgentId && agentCapabilities && (
            <div className="p-3 bg-[#1a1a2e] rounded-lg border border-[#2a2a4a]">
              <div className="text-xs text-gray-400 mb-2">
                사용 가능한 액션 서버 ({agentCapabilities.total}개):
              </div>
              <div className="flex flex-wrap gap-1">
                {agentCapabilities.capabilities.map(cap => (
                  <span
                    key={cap.action_server}
                    className="text-[10px] px-2 py-1 bg-purple-500/20 text-purple-400 rounded font-mono"
                    title={cap.action_type}
                  >
                    {cap.action_server.replace(/^\//, '')}
                  </span>
                ))}
                {agentCapabilities.capabilities.length === 0 && (
                  <span className="text-xs text-gray-500 italic">감지된 액션 서버 없음</span>
                )}
              </div>
            </div>
          )}

          <div className="flex justify-end gap-3 pt-4">
            <button type="button" onClick={onClose} className="px-4 py-2 text-gray-400 hover:text-white">
              취소
            </button>
            <button
              type="submit"
              disabled={createTemplate.isPending}
              className="px-6 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
            >
              {createTemplate.isPending ? '생성 중...' : '템플릿 생성'}
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
  onAssign,
  onUnassign,
  onClose,
}: {
  template: ActionGraph
  currentAssignments: AssignmentInfo[]
  onAssign: (agentId: string) => void
  onUnassign: (agentId: string) => void
  onClose: () => void
}) {
  console.log('[AssignTemplateModal] Rendering, template:', template?.id, template?.name)
  console.log('[AssignTemplateModal] currentAssignments:', currentAssignments)

  const assignedAgentIds = currentAssignments.map(a => a.agent_id)

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

  // Sort agents: compatible first, then assigned, then by name
  const sortedAgents = [...compatibleAgents].sort((a, b) => {
    const aAssigned = assignedAgentIds.includes(a.agent_id)
    const bAssigned = assignedAgentIds.includes(b.agent_id)
    if (a.has_all_capabilities !== b.has_all_capabilities) {
      return a.has_all_capabilities ? -1 : 1
    }
    if (aAssigned !== bAssigned) {
      return aAssigned ? -1 : 1
    }
    return a.agent_name.localeCompare(b.agent_name)
  })

  return (
    <div className="fixed inset-0 bg-black/70 flex items-center justify-center z-50">
      <div className="bg-[#16162a] rounded-xl shadow-2xl w-full max-w-lg border border-[#2a2a4a]">
        <div className="px-6 py-4 border-b border-[#2a2a4a] flex items-center justify-between">
          <div>
            <h2 className="text-lg font-semibold text-white">Assign Template</h2>
            <p className="text-sm text-gray-500 mt-0.5">{template.name}</p>
          </div>
          <button onClick={onClose} className="text-gray-500 hover:text-white">
            <X size={20} />
          </button>
        </div>

        <div className="p-6">
          {/* Show required action types with checkmarks */}
          <div className="mb-5 p-4 bg-[#1a1a2e] rounded-lg border border-[#2a2a4a]">
            <div className="text-xs text-gray-400 mb-3 font-medium uppercase tracking-wider">
              Required Action Types ({requiredActionTypes.length})
            </div>
            {requiredActionTypes.length > 0 ? (
              <div className="space-y-1.5">
                {requiredActionTypes.map(at => (
                  <div key={at} className="flex items-center gap-2 text-sm">
                    <Check size={14} className="text-purple-400" />
                    <span className="text-gray-300 font-mono text-xs">{at}</span>
                  </div>
                ))}
              </div>
            ) : (
              <div className="flex items-center gap-2 text-sm text-gray-500">
                <AlertCircle size={14} />
                <span className="italic">No actions in this template. Add actions to enable assignment.</span>
              </div>
            )}
          </div>

          {/* Agent list */}
          <div className="text-xs text-gray-400 mb-2 font-medium uppercase tracking-wider">
            Agents ({sortedAgents.filter(a => a.has_all_capabilities).length} compatible)
          </div>

          <div className="space-y-2 max-h-72 overflow-y-auto">
            {compatibleLoading ? (
              <div className="text-center py-8 text-gray-500">Loading agents...</div>
            ) : sortedAgents.length === 0 ? (
              <div className="text-center py-8 text-gray-500">
                {requiredActionTypes.length === 0
                  ? 'Add actions to template first'
                  : 'No agents registered yet'}
              </div>
            ) : (
              sortedAgents.map(agent => {
                const isAssigned = assignedAgentIds.includes(agent.agent_id)
                const isCompatible = agent.has_all_capabilities
                const matchedCount = requiredActionTypes.length - (agent.missing_capabilities?.length || 0)

                return (
                  <div
                    key={agent.agent_id}
                    className={`p-3 rounded-lg border transition-colors ${
                      isAssigned
                        ? 'bg-green-500/10 border-green-500/30'
                        : isCompatible
                          ? 'bg-[#1a1a2e] border-[#2a2a4a] hover:border-[#3a3a5a]'
                          : 'bg-[#1a1a2e] border-yellow-500/30'
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
                            <span className="text-sm text-white font-medium">{agent.agent_name}</span>
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
                              : 'bg-gray-500/10 text-gray-600 cursor-not-allowed'
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
                              <span className="text-gray-500">
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

          <div className="mt-4 pt-4 border-t border-[#2a2a4a] flex justify-end">
            <button
              onClick={onClose}
              className="px-4 py-2 bg-[#2a2a4a] text-white rounded-lg hover:bg-[#3a3a5a]"
            >
              Done
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

function CreateStateDefinitionModal({
  onClose,
  onCreated,
}: {
  onClose: () => void
  onCreated: () => void
}) {
  const [formData, setFormData] = useState({
    id: '',
    name: '',
    description: '',
  })
  const [states, setStates] = useState<string[]>([])
  const [newState, setNewState] = useState('')
  const [defaultState, setDefaultState] = useState('')
  const [error, setError] = useState('')
  const queryClient = useQueryClient()

  useEffect(() => {
    if (states.length === 0) {
      if (defaultState !== '') {
        setDefaultState('')
      }
      return
    }
    if (!defaultState || !states.includes(defaultState)) {
      setDefaultState(states[0])
    }
  }, [states, defaultState])

  const createStateDef = useMutation({
    mutationFn: (payload: Partial<StateDefinition>) => stateDefinitionApi.create(payload),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['state-definitions'] })
      onCreated()
    },
    onError: (err: any) => setError(err.response?.data?.detail || 'Failed to create state definition'),
  })

  const handleAddState = () => {
    const trimmed = newState.trim().toLowerCase().replace(/\s+/g, '_')
    if (!trimmed) {
      setError('State name is required')
      return
    }
    if (states.includes(trimmed)) {
      setError('State already exists')
      return
    }
    setStates(prev => [...prev, trimmed])
    if (!defaultState) {
      setDefaultState(trimmed)
    }
    setNewState('')
    setError('')
  }

  const handleRemoveState = (stateToRemove: string) => {
    const nextStates = states.filter(state => state !== stateToRemove)
    setStates(nextStates)
    if (defaultState === stateToRemove) {
      setDefaultState(nextStates[0] || '')
    }
    setError('')
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    const id = formData.id.trim()
    const name = formData.name.trim()
    if (!id || !name) {
      setError('ID and Name are required')
      return
    }
    if (states.length === 0) {
      setError('At least one state is required')
      return
    }

    const payload: Partial<StateDefinition> = {
      id,
      name,
      states,
      default_state: defaultState || states[0],
    }
    const description = formData.description.trim()
    if (description) {
      payload.description = description
    }

    createStateDef.mutate(payload)
  }

  return (
    <div className="fixed inset-0 bg-black/70 flex items-center justify-center z-50">
      <div className="bg-[#16162a] rounded-xl shadow-2xl w-full max-w-lg border border-[#2a2a4a]">
        <div className="px-6 py-4 border-b border-[#2a2a4a] flex items-center justify-between">
          <h2 className="text-lg font-semibold text-white">Create State Definition</h2>
          <button onClick={onClose} className="text-gray-500 hover:text-white">
            <X size={20} />
          </button>
        </div>

        <form onSubmit={handleSubmit} className="p-6 space-y-4">
          {error && (
            <div className="p-3 bg-red-500/20 border border-red-500/50 rounded-lg text-red-400 text-sm">
              {error}
            </div>
          )}

          <div>
            <label className="block text-sm font-medium text-gray-300 mb-1">정의 ID</label>
            <input
              type="text"
              value={formData.id}
              onChange={e => setFormData(prev => ({ ...prev, id: e.target.value }))}
              placeholder="예: default_states"
              className="w-full px-3 py-2 bg-[#1a1a2e] border border-[#2a2a4a] rounded-lg text-white placeholder-gray-600"
              required
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-300 mb-1">이름</label>
            <input
              type="text"
              value={formData.name}
              onChange={e => setFormData(prev => ({ ...prev, name: e.target.value }))}
              placeholder="예: Fleet States"
              className="w-full px-3 py-2 bg-[#1a1a2e] border border-[#2a2a4a] rounded-lg text-white placeholder-gray-600"
              required
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-300 mb-1">설명</label>
            <textarea
              value={formData.description}
              onChange={e => setFormData(prev => ({ ...prev, description: e.target.value }))}
              placeholder="선택사항..."
              className="w-full px-3 py-2 bg-[#1a1a2e] border border-[#2a2a4a] rounded-lg text-white resize-none placeholder-gray-600"
              rows={2}
            />
          </div>

          <div className="space-y-2">
            <label className="block text-sm font-medium text-gray-300">상태 목록</label>
            <div className="flex gap-2">
              <input
                type="text"
                value={newState}
                onChange={e => {
                  setNewState(e.target.value)
                  setError('')
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault()
                    handleAddState()
                  }
                }}
                placeholder="상태 이름"
                className="flex-1 px-3 py-2 bg-[#1a1a2e] border border-[#2a2a4a] rounded-lg text-white text-sm"
              />
              <button
                type="button"
                onClick={handleAddState}
                className="px-4 py-2 bg-green-600 text-white rounded-lg hover:bg-green-700 text-sm"
              >
                추가
              </button>
            </div>

            {states.length === 0 ? (
              <div className="text-xs text-gray-500 italic">추가된 상태 없음</div>
            ) : (
              <div className="space-y-2 max-h-40 overflow-y-auto">
                {states.map(state => (
                  <div key={state} className="flex items-center justify-between px-3 py-2 bg-[#1a1a2e] rounded-lg">
                    <span className="text-sm text-white">{state}</span>
                    <button
                      type="button"
                      onClick={() => handleRemoveState(state)}
                      className="p-1 text-gray-500 hover:text-red-400 hover:bg-red-500/20 rounded"
                    >
                      <X size={14} />
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-300 mb-1">기본 상태</label>
            <select
              value={defaultState}
              onChange={e => setDefaultState(e.target.value)}
              disabled={states.length === 0}
              className="w-full px-3 py-2 bg-[#1a1a2e] border border-[#2a2a4a] rounded-lg text-white disabled:opacity-50"
            >
              <option value="">-- 기본 상태 선택 --</option>
              {states.map(state => (
                <option key={state} value={state}>
                  {state}
                </option>
              ))}
            </select>
          </div>

          <div className="flex justify-end gap-3 pt-4">
            <button type="button" onClick={onClose} className="px-4 py-2 text-gray-400 hover:text-white">
              취소
            </button>
            <button
              type="submit"
              disabled={createStateDef.isPending}
              className="px-6 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
            >
              {createStateDef.isPending ? '생성 중...' : '생성'}
            </button>
          </div>
        </form>
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
      <div className="bg-[#16162a] rounded-xl shadow-2xl w-full max-w-lg border border-[#2a2a4a]">
        <div className="px-6 py-4 border-b border-[#2a2a4a] flex items-center justify-between">
          <h2 className="text-lg font-semibold text-white">Manage States</h2>
          <button onClick={onClose} className="text-gray-500 hover:text-white">
            <X size={20} />
          </button>
        </div>

        <div className="p-6 space-y-5">
          <div className="space-y-3">
            <label className="block text-xs text-gray-400 uppercase tracking-wider">Add New State</label>
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
                  className="flex-1 px-3 py-2 bg-[#1a1a2e] border border-[#2a2a4a] rounded-lg text-white text-sm"
                  autoFocus
                />
                <button
                  type="submit"
                  className="px-4 py-2 bg-green-600 text-white rounded-lg hover:bg-green-700 text-sm"
                >
                  Add
                </button>
              </div>
              <div className="flex items-center gap-3">
                <span className="text-xs text-gray-500">Color:</span>
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
            <label className="block text-xs text-gray-400 uppercase tracking-wider mb-3">
              States ({localStates.length})
            </label>
            <div className="space-y-2 max-h-48 overflow-y-auto">
              {localStates.map(state => (
                <div key={state} className="flex items-center justify-between px-3 py-2 bg-[#1a1a2e] rounded-lg">
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
                    <span className="text-sm text-white">{state}</span>
                    {state === stateDef.default_state && (
                      <span className="text-[9px] px-1.5 py-0.5 bg-blue-500/20 text-blue-400 rounded">default</span>
                    )}
                  </div>
                  <button
                    onClick={() => handleRemoveState(state)}
                    className={`p-1 rounded ${
                      state === stateDef.default_state
                        ? 'text-gray-700 cursor-not-allowed'
                        : 'text-gray-500 hover:text-red-400 hover:bg-red-500/20'
                    }`}
                    disabled={state === stateDef.default_state}
                  >
                    <X size={14} />
                  </button>
                </div>
              ))}
            </div>
          </div>

          <div className="flex justify-between items-center pt-4 border-t border-[#2a2a4a]">
            <span className={`text-sm ${hasChanges ? 'text-yellow-400' : 'text-gray-600'}`}>
              {hasChanges ? 'Unsaved changes' : 'No changes'}
            </span>
            <div className="flex gap-3">
              <button onClick={onClose} className="px-4 py-2 text-gray-400 hover:text-white text-sm">
                Cancel
              </button>
              <button
                onClick={handleSave}
                disabled={updateStateDef.isPending || !hasChanges}
                className="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50 text-sm"
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
    <ReactFlowProvider>
      <ActionGraphEditor />
    </ReactFlowProvider>
  )
}
