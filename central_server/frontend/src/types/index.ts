// Agent Types
export interface Agent {
  id: string
  name: string
  status: 'online' | 'offline' | 'warning'
  ip_address: string | null
  last_seen: string | null
  robot_count: number
  created_at: string
}

// Robot Types
export interface Robot {
  id: string
  name: string
  agent_id: string | null
  ip_address: string | null
  current_state: string
  is_online: boolean
  staleness_sec: number
  last_seen: string | null
  created_at: string
}

// ============================================
// State ActionGraph Configuration Types
// ============================================

// Logical operator for combining conditions
export type LogicalOperator = 'AND' | 'OR'

// Single condition item
export interface StateCondition {
  id: string
  type: 'self_state' | 'agent_state'
  negated?: boolean  // NOT operator
  // Self state condition
  state?: string
  stateOperator?: '==' | '!='
  // Other agent condition
  agentId?: string
  agentType?: string  // Deprecated: use agentId
  agentQuantifier?: 'all' | 'any' | 'specific'
  agentIds?: string[]
  agentState?: string
}

// Group of conditions with logical operator
export interface ConditionGroup {
  id: string
  operator: LogicalOperator
  conditions: (StateCondition | ConditionGroup)[]
  negated?: boolean  // NOT operator for entire group
}

// Start Condition - pre-condition before action executes
// Reads like: "[Every/Any/Self/Specific] [AgentType/RobotId] is [State] [and/or]"
export interface StartStateConfig {
  id: string
  quantifier: 'self' | 'every' | 'any' | 'specific'  // Who does this apply to?
  agentId?: string  // Agent ID (for 'every', 'any', or 'specific')
  agentType?: string  // Deprecated: use agentId
  robotId?: string  // Deprecated: robot-specific start states are no longer used
  state: string  // Required state
  operator?: 'and' | 'or'  // Logical operator to combine with next condition
  conditionGroup?: ConditionGroup  // Optional additional conditions (advanced)
}

// Server-side start condition schema
export interface StartCondition {
  id: string
  operator?: 'and' | 'or'
  quantifier?: 'self' | 'all' | 'any' | 'none' | 'specific'
  target_type?: 'self' | 'robot' | 'agent' | 'all'
  robot_id?: string
  agent_id?: string
  state?: string
  state_operator?: '==' | '!=' | 'in' | 'not_in'
  allowed_states?: string[]
  max_staleness_sec?: number
  require_online?: boolean
  message?: string
}

export type ActionOutcome = 'success' | 'failed' | 'aborted' | 'cancelled' | 'timeout' | 'rejected'

export interface DuringStateTarget {
  state: string
  target_type?: 'self' | 'all' | 'agent'
  agent_id?: string
}

// End State - possible outcome that can connect to next action
export interface EndStateConfig {
  id: string
  state: string  // State to set on this outcome
  label?: string  // Display label (e.g., "Success", "Timeout", "Error")
  color?: string  // Handle color
  outcome?: ActionOutcome  // Action outcome this state represents
  condition?: string  // Optional edge condition for this outcome
}

// Legacy StateConfig for backward compatibility
export interface StateConfig {
  state: string
  conditions?: StateCondition[]
}

// State Definition Types
export interface ActionMapping {
  action_type: string
  server: string
  during_state?: string  // Deprecated: use during_states
  during_states?: string[]  // Multiple states during execution
}

export interface StateDefinition {
  id: string
  name: string
  description: string | null
  states: string[]
  default_state: string
  action_mappings: ActionMapping[]
  teachable_waypoints: string[] | null
  version: number
  created_at: string
  updated_at: string
}

// ActionGraph Types
export interface GraphStep {
  id: string
  name?: string
  type?: 'fallback' | 'terminal' | null
  terminal_type?: 'success' | 'failure'
  // Legacy preconditions (simple string-based)
  preconditions?: Precondition[]
  // New State ActionGraph Configuration
  startStates?: StartStateConfig[]  // Pre-conditions with agent conditions
  start_conditions?: StartCondition[]
  duringStates?: string[]           // States during execution
  during_states?: string[]
  duringStateTargets?: DuringStateTarget[]
  during_state_targets?: DuringStateTarget[]
  endStates?: EndStateConfig[]      // Possible outcomes, each can connect to different action
  end_states?: EndStateConfig[]
  // Legacy multi-state (for backward compatibility)
  preStates?: StateConfig[]
  successStates?: string[]
  failureStates?: string[]
  pre_states?: string[]
  success_states?: string[]
  failure_states?: string[]
  // Action and transition
  action?: StepAction
  wait_for?: WaitFor
  transition?: Transition
}

export interface Precondition {
  condition: string
  message?: string
}

export interface StepAction {
  type: string
  server?: string
  params: {
    source: 'waypoint' | 'inline' | 'dynamic'
    waypoint_id?: string
    data?: Record<string, unknown>
    fields?: Array<{ name: string; type: string; label: string }>
  }
  timeout_sec?: number
}

export interface WaitFor {
  type: 'manual_confirm'
  message?: string
  timeout_sec?: number
}

export interface Transition {
  on_success?: string | TransitionCondition
  on_failure?: string | { retry?: number; fallback?: string }
  on_confirm?: string
  on_cancel?: string
  on_timeout?: string
  on_outcomes?: OutcomeTransition[]
}

export interface TransitionCondition {
  condition?: string
  next?: string
  else?: string
  wait?: boolean
  timeout_sec?: number
}

export interface OutcomeTransition {
  outcome: ActionOutcome
  next?: string
  condition?: string
  state?: string
}

export interface ActionGraph {
  id: string
  name: string
  description: string | null
  agent_id: string | null       // Owner agent (null = template)
  agent_name: string | null     // Convenience field
  entry_point?: string | null
  preconditions: Precondition[] | null
  steps: GraphStep[]
  version: number
  is_template: boolean          // true if agent_id is null
  deployment_status: string | null  // From AgentActionGraph if exists
  created_at: string
  updated_at: string | null
}

export interface GraphListItem {
  id: string
  name: string
  description: string | null
  agent_id: string | null
  agent_name: string | null
  entry_point?: string | null
  step_count: number
  version: number
  is_template: boolean
  deployment_status: string | null
  created_at: string
  updated_at: string | null
}

export interface GraphCreateRequest {
  id: string
  name: string
  description?: string
  agent_id?: string             // Owner agent (null = template)
  entry_point?: string
  preconditions?: Precondition[]
  steps: GraphStep[]
}

// Task Types
export interface Task {
  id: string
  flow_id: string
  robot_id: string
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled' | 'paused' | 'waiting_confirm'
  current_step_id: string | null
  current_step_index: number
  step_results: Record<string, unknown> | null
  error_message: string | null
  created_at: string
  started_at: string | null
  completed_at: string | null
  flow_name?: string
  robot_name?: string
  total_steps?: number
  progress?: { current: number; total: number }
}

// Waypoint Types
export interface Waypoint {
  id: string
  name: string
  waypoint_type: 'pose_2d' | 'joint_state' | 'pose_3d' | 'gripper'
  data: Record<string, unknown>
  created_by: 'teach' | 'manual' | null
  description: string | null
  tags: string[] | null
  created_at: string
  updated_at: string
}

// Action Types
export interface ActionDefinition {
  full_name: string
  package: string
  name: string
  goal_field_count: number
  result_field_count: number
  feedback_field_count: number
}

export interface ActionDetail {
  package: string
  name: string
  full_name: string
  goal_fields: ActionField[]
  result_fields: ActionField[]
  feedback_fields: ActionField[]
}

export interface ActionField {
  name: string
  type: string
  is_array: boolean
  is_constant: boolean
  constant_value?: string
  default?: string
}

// ============================================
// Action Graph Template & Assignment Types
// ============================================

export interface TemplateListItem {
  id: string
  name: string
  description: string | null
  required_action_types?: string[]       // Capability-based matching
  step_count: number
  version: number
  assignment_count: number
  created_at: string
  updated_at: string | null
}

export interface AssignmentInfo {
  id: string
  agent_id: string
  agent_name: string
  action_graph_id: string
  action_graph_name: string
  robot_count: number
  server_version: number
  deployed_version: number | null
  deployment_status: string
  enabled: boolean
  deployed_at: string | null
}

// Action server in agent overview
export interface AgentOverviewActionServer {
  action_server: string  // e.g., "/test_A_action"
  action_type: string    // e.g., "test_msgs/TestAction"
  is_available: boolean
  status: string
}

export interface AgentOverviewInfo {
  agent_id: string
  agent_name: string
  status: string
  robot_count: number
  action_types: string[]  // backward compat
  action_servers?: AgentOverviewActionServer[]  // individual servers
  assigned_templates: {
    assignment_id: string
    template_id: string
    template_name: string
    version: number
    deployed_version: number | null
    status: string
    enabled: boolean
  }[]
}

// ============================================
// Fleet State Types (Real-time state with timestamps)
// ============================================

export type StateQuantifier = 'self' | 'all' | 'any' | 'none' | 'specific'
export type StateOperatorType = '==' | '!=' | 'in' | 'not_in'

export interface RobotStateSnapshot {
  id?: string
  name?: string
  agent_id?: string | null
  current_state?: string
  is_online?: boolean
  is_executing?: boolean
  current_task_id?: string
  current_step_id?: string
  staleness_sec?: number
  // Legacy fields (for backward compatibility)
  robot_id?: string
  robot_name?: string
  agent_name?: string | null
  state?: string
  state_updated_at?: string  // ISO timestamp
}

export interface FleetStateSnapshot {
  timestamp: string
  robots: Record<string, RobotStateSnapshot>
  total_robots: number
  online_robots: number
  by_agent: Record<string, string[]>
  by_state: Record<string, string[]>
}

export interface EnhancedStartStateCondition {
  id: string
  target_type: 'self' | 'robot' | 'agent' | 'all'
  robot_id?: string
  agent_id?: string
  quantifier: StateQuantifier
  state: string
  state_operator: StateOperatorType
  allowed_states?: string[]
  max_staleness_sec: number
  require_online: boolean
  error_message?: string
}

export interface StartStateGroup {
  id: string
  operator: 'and' | 'or'
  conditions: (EnhancedStartStateCondition | StartStateGroup)[]
  negated: boolean
}

export interface ConditionResult {
  condition_id: string
  passed: boolean
  target_robots: string[]
  matching_robots: string[]
  failed_robots: string[]
  error: string | null
  stale_robots: string[]
  offline_robots: string[]
}

export interface StartStateValidationResult {
  passed: boolean
  timestamp: string
  condition_results: ConditionResult[]
  total_conditions: number
  passed_conditions: number
  failed_conditions: number
  error_message: string | null
}

// WebSocket Types
export interface MonitorData {
  timestamp: string
  robots: RobotMonitorData[]
  tasks: TaskMonitorData[]
}

export interface RobotMonitorData {
  id: string
  name: string
  state: string
  is_online: boolean
  staleness_sec: number
  current_task: {
    id: string
    flow_name: string
    current_step: string
    step_index: number
    total_steps: number
  } | null
  action_servers: Record<string, string> | null
}

export interface TaskMonitorData {
  id: string
  flow_id: string
  flow_name: string
  robot_id: string
  status: string
  current_step_id: string | null
  started_at: string | null
  progress: { current: number; total: number }
}

// ============================================
// Capability-Based Template Types (NEW)
// ============================================

// Agent capability info (direct from agent_capabilities table)
export interface AgentCapabilityInfo {
  action_type: string
  action_server: string
  goal_schema?: Record<string, unknown>
  result_schema?: Record<string, unknown>
  feedback_schema?: Record<string, unknown>
  success_criteria?: Record<string, unknown>
  status: string
  is_available: boolean
  discovered_at: string
}

// Response from GET /api/agents/{agentID}/capabilities
export interface AgentCapabilitiesResponse {
  agent_id: string
  agent_name: string
  status: string
  capabilities: AgentCapabilityInfo[]
  total: number
}

// Action type with agent count (from GET /api/capabilities/action-types)
export interface ActionTypeStats {
  action_type: string
  agent_count: number
}

// Individual action server info (not grouped by type)
export interface ActionServerInfo {
  action_type: string    // e.g., "test_msgs/TestAction"
  action_server: string  // e.g., "/test_A_action"
  agent_id: string
  agent_name?: string
  is_available: boolean
  status: string
}

// Compatible agent info (from GET /api/templates/{id}/compatible-agents)
export interface CompatibleAgent {
  agent_id: string
  agent_name: string
  status: string
  has_all_capabilities: boolean
  missing_capabilities: string[]
}

// Response from GET /api/templates/{id}/compatible-agents
export interface CompatibleAgentsResponse {
  template_id: string
  required_action_types: string[]
  agents: CompatibleAgent[]
}

// Template compatibility info (from GET /api/agents/{id}/compatible-templates)
export interface TemplateCompatibilityInfo {
  template_id: string
  template_name: string
  description?: string
  required_action_types?: string[]
  is_fully_compatible: boolean
  missing_capabilities?: string[]
  already_assigned: boolean
}

// Response from GET /api/agents/{id}/compatible-templates
export interface AgentCompatibleTemplatesResponse {
  agent_id: string
  agent_name: string
  agent_action_types: string[]
  templates: TemplateCompatibilityInfo[]
}

// ============================================
// Agent Connection Status Types (Heartbeat)
// ============================================

// Connection status for an agent (heartbeat monitoring)
export interface AgentConnectionStatus {
  id: string
  name: string
  ip_address?: string
  status: 'online' | 'offline'
  is_online: boolean
  connected_at?: string
  last_heartbeat?: string
  heartbeat_age_ms: number
  heartbeat_health: 'healthy' | 'warning' | 'critical'
  last_ping?: string
  ping_latency_ms?: number
  ping_latency_us?: number
  robot_ids: string[]
}
