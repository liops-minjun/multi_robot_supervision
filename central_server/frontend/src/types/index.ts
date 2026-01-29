// ============================================
// Lifecycle State Types (ROS2 Lifecycle Node)
// ============================================

export type LifecycleState = 'unknown' | 'unconfigured' | 'inactive' | 'active' | 'finalized'

// Helper to check if lifecycle state indicates availability
export function isLifecycleAvailable(state: LifecycleState): boolean {
  return state === 'active'
}

// Helper to get lifecycle state display info
export function getLifecycleStateInfo(state: LifecycleState): {
  label: string
  color: string
  description: string
} {
  switch (state) {
    case 'active':
      return { label: 'ACTIVE', color: 'green', description: 'Action server is running and accepting goals' }
    case 'inactive':
      return { label: 'INACTIVE', color: 'yellow', description: 'Configured but not accepting goals' }
    case 'unconfigured':
      return { label: 'UNCONFIGURED', color: 'gray', description: 'Node created but not configured' }
    case 'finalized':
      return { label: 'FINALIZED', color: 'red', description: 'Node is shutting down' }
    default:
      return { label: 'UNKNOWN', color: 'gray', description: 'Lifecycle state not available (non-lifecycle node)' }
  }
}

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
// Reads like: "[Every/Any/Self/Specific] [AgentId] is [State] [and/or]"
export interface StartStateConfig {
  id: string
  quantifier: 'self' | 'every' | 'any' | 'specific'  // Who does this apply to?
  agentId?: string  // Agent ID (for 'every', 'any', or 'specific')
  agentType?: string  // Deprecated: use agentId
  state: string  // Required state
  operator?: 'and' | 'or'  // Logical operator to combine with next condition
  conditionGroup?: ConditionGroup  // Optional additional conditions (advanced)
}

// Server-side start condition schema
export interface StartCondition {
  id: string
  operator?: 'and' | 'or'
  quantifier?: 'self' | 'all' | 'any' | 'none' | 'specific'
  target_type?: 'self' | 'agent' | 'all'
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

// ============================================
// Graph State Types (Auto-generated and custom states)
// ============================================

export type StateType = 'system' | 'graph' | 'custom'
export type StatePhase = 'idle' | 'executing' | 'success' | 'failed'

// Graph state definition (auto-generated or custom)
export interface GraphState {
  code: string               // State code (e.g., "pick:executing")
  name: string               // Human-readable name
  type: StateType            // system, graph, or custom
  step_id?: string           // Associated step ID (for graph states)
  phase?: StatePhase         // Phase: idle, executing, success, failed
  color?: string             // UI color
  description?: string       // Description
  semantic_tags?: string[]   // Semantic tags for cross-agent queries
}

// System states (predefined)
export const SystemStates: GraphState[] = [
  { code: 'idle', name: 'Idle', type: 'system', phase: 'idle', color: '#6B7280', description: 'Agent is idle and ready', semantic_tags: ['ready', 'available'] },
  { code: 'executing', name: 'Executing', type: 'system', phase: 'executing', color: '#3B82F6', description: 'Agent is executing an action', semantic_tags: ['busy', 'working'] },
  { code: 'error', name: 'Error', type: 'system', phase: 'failed', color: '#EF4444', description: 'Agent encountered an error', semantic_tags: ['error', 'fault'] },
  { code: 'offline', name: 'Offline', type: 'system', phase: 'idle', color: '#9CA3AF', description: 'Agent is offline', semantic_tags: ['unavailable'] },
]

// ActionGraph Types
export interface GraphStep {
  id: string
  name?: string
  job_name?: string                // User-defined job name for this step
  auto_generate_states?: boolean   // Whether to auto-generate states from this step
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

// ============================================
// Canonical Data Type System (for parameter binding)
// ============================================

/**
 * Canonical data types shared across frontend, backend, and agent.
 * Maps to ROS2 types and supports cross-step parameter binding.
 */
export type CanonicalDataType =
  // Primitive types
  | 'bool'
  | 'int8' | 'int16' | 'int32' | 'int64'
  | 'uint8' | 'uint16' | 'uint32' | 'uint64'
  | 'float32' | 'float64'
  | 'string'
  // Complex types
  | 'object'
  | 'array'
  // Any type (for dynamic/expression sources)
  | 'any'

/**
 * Type category for compatibility grouping
 */
export type TypeCategory = 'boolean' | 'integer' | 'float' | 'string' | 'object' | 'array' | 'any'

/**
 * Type metadata including array info and nested structure
 */
export interface DataTypeInfo {
  type: CanonicalDataType
  category: TypeCategory
  isArray: boolean
  arrayElementType?: DataTypeInfo  // For arrays: type of elements
  objectFields?: Record<string, DataTypeInfo>  // For objects: field types
  rosType?: string  // Original ROS2 type (e.g., "geometry_msgs/msg/Pose")
}

/**
 * Type compatibility rules:
 * - Same type: always compatible
 * - Integer types: compatible with each other (implicit conversion)
 * - Float types: compatible with each other and integers (implicit conversion)
 * - String: only compatible with string
 * - Bool: only compatible with bool
 * - Object: compatible if fields match
 * - Array: compatible if element types match
 * - Any: compatible with everything
 */
export interface TypeCompatibilityResult {
  compatible: boolean
  requiresConversion: boolean
  conversionType?: 'implicit' | 'explicit' | 'lossy'
  warningMessage?: string
}

/**
 * Get category for a canonical type
 */
export function getTypeCategory(type: CanonicalDataType): TypeCategory {
  if (type === 'bool') return 'boolean'
  if (type.startsWith('int') || type.startsWith('uint')) return 'integer'
  if (type.startsWith('float')) return 'float'
  if (type === 'string') return 'string'
  if (type === 'object') return 'object'
  if (type === 'array') return 'array'
  return 'any'
}

/**
 * Check if two types are compatible for parameter binding
 */
export function checkTypeCompatibility(
  sourceType: DataTypeInfo,
  targetType: DataTypeInfo
): TypeCompatibilityResult {
  // Any type is always compatible
  if (sourceType.type === 'any' || targetType.type === 'any') {
    return { compatible: true, requiresConversion: false }
  }

  // Exact type match
  if (sourceType.type === targetType.type && sourceType.isArray === targetType.isArray) {
    return { compatible: true, requiresConversion: false }
  }

  // Array compatibility
  if (sourceType.isArray !== targetType.isArray) {
    // Array to non-array: need index access
    if (sourceType.isArray && !targetType.isArray && sourceType.arrayElementType) {
      // Check if element type is compatible
      const elementCompat = checkTypeCompatibility(sourceType.arrayElementType, targetType)
      if (elementCompat.compatible) {
        return {
          compatible: true,
          requiresConversion: true,
          conversionType: 'explicit',
          warningMessage: 'Array access required - use [index] syntax'
        }
      }
    }
    return { compatible: false, requiresConversion: false }
  }

  // Same category compatibility with conversion
  const sourceCategory = getTypeCategory(sourceType.type)
  const targetCategory = getTypeCategory(targetType.type)

  // Boolean only with boolean
  if (sourceCategory === 'boolean' || targetCategory === 'boolean') {
    return { compatible: sourceCategory === targetCategory, requiresConversion: false }
  }

  // Integer to float (implicit, safe)
  if (sourceCategory === 'integer' && targetCategory === 'float') {
    return { compatible: true, requiresConversion: true, conversionType: 'implicit' }
  }

  // Float to integer (lossy, requires explicit)
  if (sourceCategory === 'float' && targetCategory === 'integer') {
    return {
      compatible: true,
      requiresConversion: true,
      conversionType: 'lossy',
      warningMessage: 'Float to integer conversion may lose precision'
    }
  }

  // Integer type widening/narrowing
  if (sourceCategory === 'integer' && targetCategory === 'integer') {
    const sourceWidth = getIntegerWidth(sourceType.type)
    const targetWidth = getIntegerWidth(targetType.type)
    if (sourceWidth <= targetWidth) {
      return { compatible: true, requiresConversion: true, conversionType: 'implicit' }
    } else {
      return {
        compatible: true,
        requiresConversion: true,
        conversionType: 'lossy',
        warningMessage: `Narrowing from ${sourceType.type} to ${targetType.type}`
      }
    }
  }

  // Float type precision
  if (sourceCategory === 'float' && targetCategory === 'float') {
    const isNarrowing = sourceType.type === 'float64' && targetType.type === 'float32'
    return {
      compatible: true,
      requiresConversion: true,
      conversionType: isNarrowing ? 'lossy' : 'implicit',
      warningMessage: isNarrowing ? 'float64 to float32 may lose precision' : undefined
    }
  }

  // String only with string
  if (sourceCategory === 'string' || targetCategory === 'string') {
    return { compatible: sourceCategory === targetCategory, requiresConversion: false }
  }

  // Object compatibility - check fields
  if (sourceCategory === 'object' && targetCategory === 'object') {
    // If both have field definitions, check them
    if (sourceType.objectFields && targetType.objectFields) {
      // Target's required fields must exist in source
      for (const [fieldName, fieldType] of Object.entries(targetType.objectFields)) {
        const sourceField = sourceType.objectFields[fieldName]
        if (!sourceField) {
          return {
            compatible: false,
            requiresConversion: false,
            warningMessage: `Missing field: ${fieldName}`
          }
        }
        const fieldCompat = checkTypeCompatibility(sourceField, fieldType)
        if (!fieldCompat.compatible) {
          return {
            compatible: false,
            requiresConversion: false,
            warningMessage: `Field '${fieldName}' type mismatch`
          }
        }
      }
      return { compatible: true, requiresConversion: false }
    }
    // If no field info, assume compatible (runtime check)
    return { compatible: true, requiresConversion: false }
  }

  return { compatible: false, requiresConversion: false }
}

/**
 * Get bit width of integer type
 */
function getIntegerWidth(type: CanonicalDataType): number {
  const match = type.match(/\d+/)
  return match ? parseInt(match[0], 10) : 32
}

/**
 * Parse a field path with array access (e.g., "poses[0].position.x")
 */
export interface FieldPathSegment {
  field: string
  arrayIndex?: number  // undefined if not array access
}

export function parseFieldPath(path: string): FieldPathSegment[] {
  const segments: FieldPathSegment[] = []
  const regex = /([a-zA-Z_][a-zA-Z0-9_]*)(?:\[(\d+)\])?/g
  let match

  while ((match = regex.exec(path)) !== null) {
    segments.push({
      field: match[1],
      arrayIndex: match[2] !== undefined ? parseInt(match[2], 10) : undefined
    })
  }

  return segments
}

/**
 * Resolve type from a field path (e.g., "pose.position.x" from Pose type)
 */
export function resolveFieldType(
  rootType: DataTypeInfo,
  fieldPath: string
): DataTypeInfo | null {
  const segments = parseFieldPath(fieldPath)
  let currentType = rootType

  for (const segment of segments) {
    // Handle array access
    if (segment.arrayIndex !== undefined) {
      if (!currentType.isArray || !currentType.arrayElementType) {
        return null  // Not an array
      }
      currentType = currentType.arrayElementType
    }

    // Handle field access
    if (segment.field && currentType.objectFields) {
      const fieldType = currentType.objectFields[segment.field]
      if (!fieldType) {
        return null  // Field not found
      }
      currentType = fieldType
    } else if (segment.field && currentType.type !== 'any') {
      // Can't access field on non-object
      if (segment.field !== segments[0].field) {
        return null
      }
    }
  }

  return currentType
}

/**
 * Map ROS2 type string to canonical type
 */
export function rosTypeToCanonical(rosType: string): DataTypeInfo {
  // Handle array types
  const isArray = rosType.includes('[]') || rosType.includes('[')
  const baseType = rosType.replace(/\[\d*\]/, '').trim()

  // Primitive mappings
  const primitiveMap: Record<string, CanonicalDataType> = {
    'bool': 'bool',
    'boolean': 'bool',
    'int8': 'int8',
    'int16': 'int16',
    'int32': 'int32',
    'int': 'int32',
    'int64': 'int64',
    'uint8': 'uint8',
    'byte': 'uint8',
    'uint16': 'uint16',
    'uint32': 'uint32',
    'uint64': 'uint64',
    'float32': 'float32',
    'float': 'float32',
    'float64': 'float64',
    'double': 'float64',
    'string': 'string',
  }

  const canonicalType = primitiveMap[baseType.toLowerCase()] || 'object'

  const typeInfo: DataTypeInfo = {
    type: canonicalType,
    category: getTypeCategory(canonicalType),
    isArray,
    rosType
  }

  if (isArray) {
    typeInfo.arrayElementType = {
      type: canonicalType,
      category: getTypeCategory(canonicalType),
      isArray: false,
      rosType: baseType
    }
  }

  return typeInfo
}

/**
 * Common ROS2 message type schemas
 */
export const ROS2_COMMON_TYPES: Record<string, DataTypeInfo> = {
  'geometry_msgs/msg/Vector3': {
    type: 'object',
    category: 'object',
    isArray: false,
    rosType: 'geometry_msgs/msg/Vector3',
    objectFields: {
      'x': { type: 'float64', category: 'float', isArray: false },
      'y': { type: 'float64', category: 'float', isArray: false },
      'z': { type: 'float64', category: 'float', isArray: false },
    }
  },
  'geometry_msgs/msg/Point': {
    type: 'object',
    category: 'object',
    isArray: false,
    rosType: 'geometry_msgs/msg/Point',
    objectFields: {
      'x': { type: 'float64', category: 'float', isArray: false },
      'y': { type: 'float64', category: 'float', isArray: false },
      'z': { type: 'float64', category: 'float', isArray: false },
    }
  },
  'geometry_msgs/msg/Quaternion': {
    type: 'object',
    category: 'object',
    isArray: false,
    rosType: 'geometry_msgs/msg/Quaternion',
    objectFields: {
      'x': { type: 'float64', category: 'float', isArray: false },
      'y': { type: 'float64', category: 'float', isArray: false },
      'z': { type: 'float64', category: 'float', isArray: false },
      'w': { type: 'float64', category: 'float', isArray: false },
    }
  },
  'geometry_msgs/msg/Pose': {
    type: 'object',
    category: 'object',
    isArray: false,
    rosType: 'geometry_msgs/msg/Pose',
    objectFields: {
      'position': {
        type: 'object',
        category: 'object',
        isArray: false,
        objectFields: {
          'x': { type: 'float64', category: 'float', isArray: false },
          'y': { type: 'float64', category: 'float', isArray: false },
          'z': { type: 'float64', category: 'float', isArray: false },
        }
      },
      'orientation': {
        type: 'object',
        category: 'object',
        isArray: false,
        objectFields: {
          'x': { type: 'float64', category: 'float', isArray: false },
          'y': { type: 'float64', category: 'float', isArray: false },
          'z': { type: 'float64', category: 'float', isArray: false },
          'w': { type: 'float64', category: 'float', isArray: false },
        }
      }
    }
  },
}

// ============================================
// Parameter Source Types (for step_result referencing)
// ============================================

export type ParameterSourceType = 'constant' | 'step_result' | 'dynamic' | 'expression'

// Defines how a single parameter field gets its value
export interface ParameterFieldSource {
  source: ParameterSourceType

  // source='constant': Fixed value
  value?: unknown

  // source='step_result': Reference to previous step's result
  step_id?: string           // Step ID to reference
  result_field?: string      // Field path in result (e.g., "pose.position.x", "poses[0].position")

  // source='expression': Variable expression (e.g., "${pick_step.result.pose}")
  expression?: string

  // Type information (for validation and UI)
  source_type?: DataTypeInfo   // Type of the source value
  target_type?: DataTypeInfo   // Expected type of target parameter
  conversion?: TypeConversionConfig  // Optional conversion config
}

// Type conversion configuration
export interface TypeConversionConfig {
  enabled: boolean
  mode: 'implicit' | 'explicit' | 'custom'
  customExpression?: string  // For custom conversion (e.g., "Math.round(value)")
}

// Result schema for a step (used by other steps to reference)
export interface StepResultSchema {
  fields: Array<{
    name: string
    type: string
    description?: string
    typeInfo?: DataTypeInfo  // Parsed type information for validation
  }>
}

export interface StepAction {
  type: string
  server?: string
  params: {
    source: 'waypoint' | 'inline' | 'dynamic' | 'mapped'
    waypoint_id?: string
    data?: Record<string, unknown>
    fields?: Array<{ name: string; type: string; label: string }>
    // NEW: Per-field source mapping (when source='mapped')
    field_sources?: Record<string, ParameterFieldSource>
  }
  timeout_sec?: number
  // NEW: Expected result schema (for other steps to reference)
  result_schema?: StepResultSchema
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
  states?: GraphState[]         // Auto-generated and custom states
  auto_generate_states?: boolean // Whether to auto-generate states from steps
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
  state_count?: number          // Number of states in the graph
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
  states?: GraphState[]         // Custom states (optional)
  auto_generate_states?: boolean // Whether to auto-generate states (default: true)
}

// Task Types
export interface Task {
  id: string
  flow_id: string
  agent_id: string
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled' | 'paused' | 'waiting_confirm'
  current_step_id: string | null
  current_step_index: number
  step_results: Record<string, unknown> | null
  error_message: string | null
  created_at: string
  started_at: string | null
  completed_at: string | null
  flow_name?: string
  agent_name?: string
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

// Agent action graph assignment info (from GET /agents/{id}/action-graphs)
export interface AgentActionGraphInfo {
  id: string
  agent_id: string
  action_graph_id: string
  action_graph_name?: string
  server_version: number
  deployed_version: number
  deployment_status: string
  deployment_error?: string
  deployed_at?: string
  enabled: boolean
  priority: number
  created_at: string
  updated_at: string
}

// Action server in agent overview
export interface AgentOverviewActionServer {
  action_server: string  // e.g., "/test_A_action"
  action_type: string    // e.g., "test_msgs/TestAction"
  is_available: boolean
  lifecycle_state?: LifecycleState  // ROS2 lifecycle state
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

export type ExecutionPhase = 'idle' | 'offline' | 'starting' | 'executing' | 'completing' | 'waiting_for_precondition'

export interface RobotStateSnapshot {
  id?: string
  name?: string
  agent_id?: string | null
  current_state?: string
  state_code?: string               // Enhanced state code (e.g., "pick:executing")
  current_graph_id?: string         // Currently executing graph ID
  execution_phase?: ExecutionPhase  // Explicit phase: idle, offline, starting, executing, waiting_for_precondition
  semantic_tags?: string[]          // State semantic tags
  is_online?: boolean
  is_executing?: boolean
  current_task_id?: string
  current_step_id?: string
  staleness_sec?: number
  // Precondition waiting status
  is_waiting_for_precondition?: boolean
  waiting_for_precondition_since?: string  // ISO timestamp
  blocking_conditions?: BlockingConditionInfo[]
  precondition_timeout_sec?: number
  // Legacy fields (for backward compatibility)
  agent_name?: string | null
  state?: string
  state_updated_at?: string  // ISO timestamp
}

// Blocking condition information for UI display
export interface BlockingConditionInfo {
  condition_id: string
  description: string              // Human-readable description
  target_agent_id?: string         // Target agent (if cross-agent condition)
  target_agent_name?: string       // Target agent name
  required_state: string           // Required state
  current_state?: string           // Current state of target
  reason: string                   // Why it's blocking (e.g., "state mismatch", "agent offline", "state too old")
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
  target_type: 'self' | 'agent' | 'all'
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
  agent_id: string
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
  lifecycle_state?: LifecycleState  // ROS2 lifecycle state
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
  lifecycle_state: LifecycleState  // ROS2 lifecycle state
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
}

// ============================================
// Enhanced Precondition Types (Cross-Agent State Checking)
// ============================================

export type EnhancedPreconditionType = 'self_state' | 'agent_state' | 'semantic_tag' | 'any_agent_state'
export type PreconditionOperator = 'equals' | 'not_equals' | 'in' | 'not_in' | 'has_tag' | 'not_has_tag'

// Filter for matching agents in precondition queries
export interface PreconditionFilter {
  graph_id?: string           // Filter by action graph ID
  capability?: string         // Filter by capability
  tags?: string[]             // Filter by semantic tags
  online_only?: boolean       // Only check online agents
  executing_only?: boolean    // Only check executing agents
  include_self?: boolean      // Include self in query
}

// Enhanced precondition for cross-agent state checking
export interface EnhancedPrecondition {
  id: string
  type: EnhancedPreconditionType
  target_agent_id?: string    // For agent_state type
  expected_state?: string     // Expected state code
  expected_states?: string[]  // Multiple expected states (for 'in' operator)
  operator?: PreconditionOperator
  filter?: PreconditionFilter // For semantic_tag and any_agent_state types
  message?: string            // Error message if not satisfied
}

// Result of precondition evaluation
export interface PreconditionResult {
  satisfied: boolean
  reason?: string             // Reason if not satisfied
  matched_agents?: string[]   // Agents that matched the filter
}

// Agent state entry for cross-agent state tracking
export interface AgentStateEntry {
  agent_id: string
  state_code: string
  semantic_tags: string[]
  current_graph_id?: string
  is_online: boolean
  is_executing: boolean
  updated_at: string
}

// System states response from GET /api/system/states
export interface SystemStatesResponse {
  system_states: GraphState[]
  count: number
}

// ============================================
// Task Execution Log Types
// ============================================

export type TaskLogLevel = 'DEBUG' | 'INFO' | 'WARN' | 'ERROR' | 'UNKNOWN'

export interface TaskLogEntry {
  agent_id: string
  task_id: string
  step_id: string
  command_id: string
  level: number
  level_str: TaskLogLevel
  message: string
  component: string
  timestamp_ms: number
  timestamp: string
  metadata?: Record<string, string>
}

export interface TaskLogStats {
  total_logs: number
  buffer_size: number
  max_logs: number
  tasks_tracked: number
  subscriber_count: number
}

// ============================================
// Multi-Agent Execution Types
// ============================================

// Request for multi-agent simultaneous execution
export interface MultiAgentExecuteRequest {
  agent_ids: string[]
  params?: Record<string, unknown>
  agent_params?: Record<string, Record<string, unknown>>
  sync_mode?: 'barrier' | 'best_effort'
  timeout_sec?: number
}

// Task info in multi-agent response
export interface MultiAgentTaskInfo {
  agent_id: string
  agent_name?: string
  task_id: string
  status: string
}

// Successful multi-agent execution response
export interface MultiAgentExecuteResponse {
  execution_group_id: string
  tasks: MultiAgentTaskInfo[]
  started_at: string
  sync_mode: string
  message: string
}

// Failed agent in multi-agent execution
export interface MultiAgentFailedAgent {
  agent_id: string
  reason: string
}

// Error response for multi-agent execution
export interface MultiAgentExecuteErrorResponse {
  error: string
  message: string
  failed_agents: MultiAgentFailedAgent[]
  passed_agents?: string[]
}

// ============================================
// Telemetry Types (for parameter loading)
// ============================================

export interface Vector3 {
  x: number
  y: number
  z: number
}

export interface Quaternion {
  x: number
  y: number
  z: number
  w: number
}

export interface Pose {
  position: Vector3
  orientation: Quaternion
}

export interface Twist {
  linear: Vector3
  angular: Vector3
}

// JointState telemetry data
export interface JointStateData {
  name: string[]
  position: number[]
  velocity: number[]
  effort: number[]
  topic_name?: string  // ROS2 topic name for visualization
  timestamp_ns?: number  // ROS2 message timestamp in nanoseconds
}

// Odometry telemetry data
export interface OdometryData {
  frame_id: string
  child_frame_id: string
  pose: Pose
  twist: Twist
  topic_name?: string  // ROS2 topic name for visualization
  timestamp_ns?: number  // ROS2 message timestamp in nanoseconds
}

// Transform telemetry data
export interface TransformData {
  frame_id: string
  child_frame_id: string
  translation: Vector3
  rotation: Quaternion
  timestamp_ns?: number  // ROS2 message timestamp in nanoseconds
}

// Combined robot telemetry
export interface RobotTelemetry {
  joint_state?: JointStateData
  odometry?: OdometryData
  transforms?: TransformData[]
  updated_at: string
  is_stale?: boolean  // True if telemetry data is older than staleness threshold
}
