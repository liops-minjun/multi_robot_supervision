import axios from 'axios'
import type {
  Agent,
  Robot,
  StateDefinition,
  ActionGraph,
  GraphListItem,
  GraphCreateRequest,
  Task,
  Waypoint,
  ActionDefinition,
  ActionDetail,
  FleetStateSnapshot,
  RobotStateSnapshot,
  EnhancedStartStateCondition,
  StartStateGroup,
  StartStateValidationResult,
  TemplateListItem,
  AssignmentInfo,
  AgentOverviewInfo,
  AgentCapabilitiesResponse,
  AgentActionGraphInfo,
  ActionTypeStats,
  ActionServerInfo,
  CompatibleAgentsResponse,
  AgentCompatibleTemplatesResponse,
  AgentConnectionStatus,
  SystemStatesResponse,
  TaskLogEntry,
  TaskLogStats,
  MultiAgentExecuteResponse,
  RobotTelemetry,
  JointStateData,
  OdometryData,
  TransformData
} from '../types'

const api = axios.create({
  baseURL: '/api',
  headers: {
    'Content-Type': 'application/json',
  },
})

// Agent APIs
export const agentApi = {
  list: async (): Promise<Agent[]> => {
    const { data } = await api.get('/agents')
    return data
  },

  get: async (id: string): Promise<Agent> => {
    const { data } = await api.get(`/agents/${id}`)
    return data
  },

  // Update agent (primarily for renaming)
  update: async (id: string, updates: { name?: string }): Promise<Agent> => {
    const { data } = await api.patch(`/agents/${id}`, updates)
    return data
  },

  // Delete agent
  delete: async (id: string): Promise<void> => {
    await api.delete(`/agents/${id}`)
  },

  // Get aggregated capabilities for all robots of an agent
  getCapabilities: async (agentId: string): Promise<AgentCapabilitiesResponse> => {
    const { data } = await api.get(`/agents/${agentId}/capabilities`)
    return data
  },

  // Get compatible templates for an agent
  getCompatibleTemplates: async (agentId: string): Promise<AgentCompatibleTemplatesResponse> => {
    const { data } = await api.get(`/agents/${agentId}/compatible-templates`)
    return data
  },

  // Get connection status for all agents (heartbeat monitoring)
  getConnectionStatus: async (): Promise<AgentConnectionStatus[]> => {
    const { data } = await api.get('/agents/connection-status')
    return data
  },

  // Get connection status for a specific agent
  getSingleConnectionStatus: async (agentId: string): Promise<AgentConnectionStatus> => {
    const { data } = await api.get(`/agents/${agentId}/connection-status`)
    return data
  },

  // Reset agent state to idle
  resetState: async (agentId: string): Promise<{
    success: boolean
    agent_id: string
    state: string
    message: string
  }> => {
    const { data } = await api.post(`/agents/${agentId}/reset-state`)
    return data
  },

  // Get behavior trees assigned to an agent
  getAssignedBehaviorTrees: async (agentId: string): Promise<AgentActionGraphInfo[]> => {
    const { data } = await api.get(`/agents/${agentId}/behavior-trees`)
    return data
  },

  // Backward compatibility alias
  getAssignedActionGraphs: async (agentId: string): Promise<AgentActionGraphInfo[]> => {
    const { data } = await api.get(`/agents/${agentId}/behavior-trees`)
    return data
  },

  // Deploy a behavior tree to an agent via QUIC
  deployBehaviorTree: async (graphId: string, agentId: string): Promise<{
    success: boolean
    behavior_tree_id: string
    agent_id: string
    version: number
    checksum: string
    error: string
    deployment_status: string
  }> => {
    const { data } = await api.post(`/behavior-trees/${graphId}/deploy/${agentId}`)
    return data
  },

  // Backward compatibility alias
  deployActionGraph: async (graphId: string, agentId: string): Promise<{
    success: boolean
    behavior_tree_id: string
    agent_id: string
    version: number
    checksum: string
    error: string
    deployment_status: string
  }> => {
    const { data } = await api.post(`/behavior-trees/${graphId}/deploy/${agentId}`)
    return data
  },
}

// Capability APIs (Fleet-wide capability queries)
export const capabilityApi = {
  // Get all unique action types with agent counts
  getAllActionTypes: async (): Promise<{ action_types: ActionTypeStats[]; total: number }> => {
    const { data } = await api.get('/capabilities/action-types')
    return data
  },

  // Get all capabilities across all agents
  listAll: async (): Promise<{
    action_types: Array<{
      action_type: string
      agent_ids: string[]
      available_count: number
      total_count: number
    }>
    action_servers: ActionServerInfo[]  // Individual action servers (not grouped)
    total_agents: number
  }> => {
    const { data } = await api.get('/capabilities')
    return data
  },

  // Get agents with a specific action type
  getByActionType: async (actionType: string): Promise<{
    action_type: string
    agents: Array<{
      agent_id: string
      agent_name: string
      action_server: string
      status: string
      is_available: boolean
      goal_schema: Record<string, unknown>
      result_schema: Record<string, unknown>
    }>
    total: number
  }> => {
    const { data } = await api.get(`/capabilities/action-type/${actionType}`)
    return data
  },
}

// Robot APIs
export const robotApi = {
  list: async (): Promise<Robot[]> => {
    const { data } = await api.get('/robots')
    return data
  },

  get: async (id: string): Promise<Robot> => {
    const { data } = await api.get(`/robots/${id}`)
    return data
  },

  delete: async (id: string): Promise<void> => {
    await api.delete(`/robots/${id}`)
  },
}

// State Definition APIs
export const stateDefinitionApi = {
  list: async (): Promise<StateDefinition[]> => {
    const { data } = await api.get('/state-definitions')
    return data
  },

  get: async (id: string): Promise<StateDefinition> => {
    const { data } = await api.get(`/state-definitions/${id}`)
    return data
  },

  create: async (stateDef: Partial<StateDefinition>): Promise<StateDefinition> => {
    const { data } = await api.post('/state-definitions', stateDef)
    return data
  },

  update: async (id: string, stateDef: Partial<StateDefinition>): Promise<StateDefinition> => {
    const { data } = await api.put(`/state-definitions/${id}`, stateDef)
    return data
  },

  delete: async (id: string): Promise<void> => {
    await api.delete(`/state-definitions/${id}`)
  },

  deploy: async (id: string, agentIds?: string[]): Promise<unknown> => {
    const { data } = await api.post(`/state-definitions/${id}/deploy`, agentIds)
    return data
  },
}

// Behavior Tree APIs
export const behaviorTreeApi = {
  list: async (params?: {
    agentId?: string
    includeTemplates?: boolean
  }): Promise<GraphListItem[]> => {
    const queryParams: Record<string, string | boolean> = {}
    if (params?.agentId) queryParams.agent_id = params.agentId
    if (params?.includeTemplates !== undefined) queryParams.include_templates = params.includeTemplates
    const { data } = await api.get('/behavior-trees', { params: queryParams })
    return data
  },

  get: async (id: string): Promise<ActionGraph> => {
    const { data } = await api.get(`/behavior-trees/${id}`)
    return data
  },

  create: async (behaviorTree: GraphCreateRequest): Promise<ActionGraph> => {
    const { data } = await api.post('/behavior-trees', behaviorTree)
    return data
  },

  update: async (id: string, behaviorTree: Partial<ActionGraph>): Promise<ActionGraph> => {
    const { data } = await api.put(`/behavior-trees/${id}`, behaviorTree)
    return data
  },

  delete: async (id: string): Promise<void> => {
    await api.delete(`/behavior-trees/${id}`)
  },

  execute: async (id: string, agentId: string, params?: Record<string, unknown>): Promise<unknown> => {
    const { data } = await api.post(`/behavior-trees/${id}/execute`, null, {
      params: { agent_id: agentId, ...params }
    })
    return data
  },

  // Check if behavior tree can be executed on an agent (safety check)
  checkExecutability: async (graphId: string, agentId: string): Promise<{
    behavior_tree_id: string
    agent_id: string
    can_execute: boolean
    capabilities_valid: boolean
    agent_online: boolean
    missing_capabilities: string[] | null
    unavailable_servers: string[] | null
    message: string
  }> => {
    const { data } = await api.get(`/behavior-trees/${graphId}/check-executability`, {
      params: { agent_id: agentId }
    })
    return data
  },

  // Multi-agent simultaneous execution
  executeMulti: async (
    graphId: string,
    agentIds: string[],
    options?: {
      commonParams?: Record<string, unknown>
      agentParams?: Record<string, Record<string, unknown>>
      syncMode?: 'barrier' | 'best_effort'
      timeoutSec?: number
    }
  ): Promise<MultiAgentExecuteResponse> => {
    const { data } = await api.post(`/behavior-trees/${graphId}/execute-multi`, {
      agent_ids: agentIds,
      params: options?.commonParams,
      agent_params: options?.agentParams,
      sync_mode: options?.syncMode || 'barrier',
      timeout_sec: options?.timeoutSec || 30,
    })
    return data
  },

  validate: async (id: string): Promise<{ valid: boolean; errors: string[]; warnings: string[] }> => {
    const { data } = await api.post(`/behavior-trees/${id}/validate`)
    return data
  },
}

// Backward compatibility alias
export const actionGraphApi = behaviorTreeApi

// Task APIs
export const taskApi = {
  list: async (status?: string, agentId?: string): Promise<Task[]> => {
    const params: Record<string, string> = {}
    if (status) params.status_filter = status
    if (agentId) params.agent_id = agentId
    const { data } = await api.get('/tasks', { params })
    return data
  },

  get: async (id: string): Promise<Task> => {
    const { data } = await api.get(`/tasks/${id}`)
    return data
  },

  cancel: async (id: string, reason?: string): Promise<void> => {
    await api.post(`/tasks/${id}/cancel`, null, { params: { reason } })
  },

  pause: async (id: string): Promise<void> => {
    await api.post(`/tasks/${id}/pause`)
  },

  resume: async (id: string): Promise<void> => {
    await api.post(`/tasks/${id}/resume`)
  },

  // Get execution logs for a task
  getLogs: async (taskId: string, limit?: number): Promise<TaskLogEntry[]> => {
    const params: Record<string, number> = {}
    if (limit) params.limit = limit
    const { data } = await api.get(`/tasks/${taskId}/logs`, { params })
    return data
  },
}

// Task Execution Logs APIs
export const logsApi = {
  // Get recent logs across all agents
  getRecent: async (limit?: number): Promise<TaskLogEntry[]> => {
    const params: Record<string, number> = {}
    if (limit) params.limit = limit
    const { data } = await api.get('/logs', { params })
    return data
  },

  // Get logs for a specific agent
  getAgentLogs: async (agentId: string, limit?: number): Promise<TaskLogEntry[]> => {
    const params: Record<string, number> = {}
    if (limit) params.limit = limit
    const { data } = await api.get(`/agents/${agentId}/logs`, { params })
    return data
  },

  // Get log statistics
  getStats: async (): Promise<TaskLogStats> => {
    const { data } = await api.get('/logs/stats')
    return data
  },
}

// Waypoint APIs
export const waypointApi = {
  list: async (waypointType?: string): Promise<Waypoint[]> => {
    const params: Record<string, string> = {}
    if (waypointType) params.waypoint_type = waypointType
    const { data } = await api.get('/waypoints', { params })
    return data
  },

  get: async (id: string): Promise<Waypoint> => {
    const { data } = await api.get(`/waypoints/${id}`)
    return data
  },

  create: async (waypoint: Partial<Waypoint>): Promise<Waypoint> => {
    const { data } = await api.post('/waypoints', waypoint)
    return data
  },

  update: async (id: string, waypoint: Partial<Waypoint>): Promise<Waypoint> => {
    const { data } = await api.put(`/waypoints/${id}`, waypoint)
    return data
  },

  delete: async (id: string): Promise<void> => {
    await api.delete(`/waypoints/${id}`)
  },

  teach: async (robotId: string, request: { waypoint_type: string; name: string; description?: string; tags?: string[] }): Promise<Waypoint> => {
    const { data } = await api.post(`/robots/${robotId}/teach`, request)
    return data
  },
}

// Action APIs
export const actionApi = {
  list: async (): Promise<ActionDefinition[]> => {
    const { data } = await api.get('/actions')
    return data
  },

  get: async (actionType: string): Promise<ActionDetail> => {
    const { data } = await api.get(`/actions/${actionType}`)
    return data
  },
}

// Behavior Tree Template APIs
export const templateApi = {
  // List all templates
  list: async (): Promise<TemplateListItem[]> => {
    const { data } = await api.get('/templates')
    return data
  },

  // Get a specific template
  get: async (id: string): Promise<ActionGraph> => {
    const { data } = await api.get(`/templates/${id}`)
    return data
  },

  // Create a template
  create: async (template: GraphCreateRequest): Promise<ActionGraph> => {
    const { data } = await api.post('/templates', template)
    return data
  },

  // Update a template
  update: async (id: string, template: Partial<ActionGraph>): Promise<ActionGraph> => {
    const { data } = await api.put(`/templates/${id}`, template)
    return data
  },

  // Delete a template
  delete: async (id: string): Promise<void> => {
    await api.delete(`/templates/${id}`)
  },

  // Get assignments for a template
  getAssignments: async (templateId: string): Promise<AssignmentInfo[]> => {
    const { data } = await api.get(`/templates/${templateId}/assignments`)
    return data
  },

  // Assign template to an agent
  assign: async (templateId: string, agentId: string, enabled = true, priority = 0): Promise<AssignmentInfo> => {
    const { data } = await api.post(`/templates/${templateId}/assignments`, {
      agent_id: agentId,
      enabled,
      priority
    })
    return data
  },

  // Unassign template from an agent
  unassign: async (templateId: string, agentId: string): Promise<void> => {
    await api.delete(`/templates/${templateId}/assignments/${agentId}`)
  },

  // Get agents overview (with capabilities and assignments)
  getAgentsOverview: async (): Promise<AgentOverviewInfo[]> => {
    const { data } = await api.get('/templates/agents-overview')
    return data
  },

  // Get available templates for an agent
  getAvailableForAgent: async (agentId: string): Promise<TemplateListItem[]> => {
    const { data } = await api.get(`/templates/agents/${agentId}/available-templates`)
    return data
  },

  // Get compatible agents for a template (capability-based)
  getCompatibleAgents: async (templateId: string): Promise<CompatibleAgentsResponse> => {
    const { data } = await api.get(`/templates/${templateId}/compatible-agents`)
    return data
  },
}

// Fleet State APIs
export const fleetApi = {
  // Get current fleet state snapshot
  getState: async (params?: {
    agentIds?: string[]
    maxStalenessSec?: number
  }): Promise<FleetStateSnapshot> => {
    const queryParams: Record<string, string | boolean | number> = {}
    if (params?.agentIds) queryParams.agent_ids = params.agentIds.join(',')
    if (params?.maxStalenessSec !== undefined) queryParams.max_staleness_sec = params.maxStalenessSec
    const { data } = await api.get('/fleet/state', { params: queryParams })
    return data
  },

  // Get single robot state
  getRobotState: async (robotId: string): Promise<RobotStateSnapshot> => {
    const { data } = await api.get(`/fleet/robots/${robotId}`)
    return data
  },

  // Get agent robots state
  getAgentState: async (agentId: string): Promise<{
    agent_id: string
    timestamp: string
    robots: RobotStateSnapshot[]
    total: number
    online: number
  }> => {
    const { data } = await api.get(`/fleet/agents/${agentId}/robots`)
    return data
  },

  // Validate start state conditions
  validate: async (
    executingAgentId: string,
    conditions: (EnhancedStartStateCondition | StartStateGroup)[]
  ): Promise<StartStateValidationResult> => {
    const { data } = await api.post('/fleet/validate', {
      executing_agent_id: executingAgentId,
      conditions
    })
    return data
  },

  // Get fleet summary
  getSummary: async (): Promise<{
    timestamp: string
    total_robots: number
    online_robots: number
    offline_robots: number
    fresh_robots: number
    stale_robots: number
    by_state: Record<string, number>
    by_agent: Record<string, number>
  }> => {
    const { data } = await api.get('/fleet/summary')
    return data
  },
}

// System APIs
export const systemApi = {
  // Get predefined system states
  getSystemStates: async (): Promise<SystemStatesResponse> => {
    const { data } = await api.get('/system/states')
    return data
  },

  // Get cache statistics
  getCacheStats: async (): Promise<{
    graph_cache: {
      total_entries: number
      template_count: number
      deployed_count: number
      total_hits: number
      total_misses: number
      hit_rate: number
    }
    timestamp: string
  }> => {
    const { data } = await api.get('/system/cache/stats')
    return data
  },

  // Evict stale cache entries
  evictStaleCache: async (maxAgeMinutes?: number): Promise<{
    evicted_count: number
    max_age_minutes: number
    timestamp: string
  }> => {
    const { data } = await api.post('/system/cache/evict', {
      max_age_minutes: maxAgeMinutes || 60
    })
    return data
  },
}

// Telemetry APIs (for parameter loading)
export const telemetryApi = {
  // Get full telemetry for a robot
  getRobotTelemetry: async (robotId: string): Promise<RobotTelemetry> => {
    const { data } = await api.get(`/fleet/robots/${robotId}/telemetry`)
    return data
  },

  // Get joint state only
  getJointState: async (robotId: string): Promise<JointStateData> => {
    const { data } = await api.get(`/fleet/robots/${robotId}/telemetry/joint-state`)
    return data
  },

  // Get odometry only
  getOdometry: async (robotId: string): Promise<OdometryData> => {
    const { data } = await api.get(`/fleet/robots/${robotId}/telemetry/odometry`)
    return data
  },

  // Get transforms only
  getTransforms: async (robotId: string): Promise<TransformData[]> => {
    const { data } = await api.get(`/fleet/robots/${robotId}/telemetry/transforms`)
    return data
  },
}

export default api
