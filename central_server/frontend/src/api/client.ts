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
  ActionTypeStats,
  ActionServerInfo,
  CompatibleAgentsResponse,
  AgentCompatibleTemplatesResponse,
  AgentConnectionStatus
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
    }>
    total: number
  }> => {
    const { data } = await api.get(`/capabilities/${actionType}`)
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

// Action Graph APIs
export const actionGraphApi = {
  list: async (params?: {
    agentId?: string
    includeTemplates?: boolean
  }): Promise<GraphListItem[]> => {
    const queryParams: Record<string, string | boolean> = {}
    if (params?.agentId) queryParams.agent_id = params.agentId
    if (params?.includeTemplates !== undefined) queryParams.include_templates = params.includeTemplates
    const { data } = await api.get('/action-graphs', { params: queryParams })
    return data
  },

  get: async (id: string): Promise<ActionGraph> => {
    const { data } = await api.get(`/action-graphs/${id}`)
    return data
  },

  create: async (actionGraph: GraphCreateRequest): Promise<ActionGraph> => {
    const { data } = await api.post('/action-graphs', actionGraph)
    return data
  },

  update: async (id: string, actionGraph: Partial<ActionGraph>): Promise<ActionGraph> => {
    const { data } = await api.put(`/action-graphs/${id}`, actionGraph)
    return data
  },

  delete: async (id: string): Promise<void> => {
    await api.delete(`/action-graphs/${id}`)
  },

  execute: async (id: string, agentId: string, params?: Record<string, unknown>): Promise<unknown> => {
    const { data } = await api.post(`/action-graphs/${id}/execute`, null, {
      params: { agent_id: agentId, ...params }
    })
    return data
  },

  validate: async (id: string): Promise<{ valid: boolean; errors: string[]; warnings: string[] }> => {
    const { data } = await api.post(`/action-graphs/${id}/validate`)
    return data
  },
}

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

  confirm: async (id: string, confirmed: boolean = true): Promise<void> => {
    await api.post(`/tasks/${id}/confirm`, null, { params: { confirmed } })
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

  teach: async (agentId: string, request: { waypoint_type: string; name: string; description?: string; tags?: string[] }): Promise<Waypoint> => {
    const { data } = await api.post(`/agents/${agentId}/teach`, request)
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

// Action Graph Template APIs
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

  // Get single agent state (1:1 model: agent = robot)
  getAgentRobotState: async (agentId: string): Promise<RobotStateSnapshot> => {
    const { data } = await api.get(`/fleet/state/agent/${agentId}`)
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
    const { data } = await api.get(`/fleet/state/agent/${agentId}`)
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

export default api
