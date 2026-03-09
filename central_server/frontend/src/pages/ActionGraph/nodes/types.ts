import type {
  StartStateConfig,
  EndStateConfig,
  GraphState,
  ParameterFieldSource,
  DuringStateTarget,
  PlanningCondition,
  PlanningEffect,
  PlanningTaskSpec,
  TaskDistributorState,
  TaskDistributorResource,
} from '../../../types'

export interface StateActionNodeData {
  label: string
  subtype: string
  color: string
  actionType?: string
  server?: string
  capabilityKind?: 'action' | 'service'
  // Job configuration
  jobName?: string
  params?: Record<string, unknown>
  fieldSources?: Record<string, ParameterFieldSource>
  // Result fields from this action (for other nodes to reference)
  resultFields?: Array<{ name: string; type: string }>
  // Auto-generate states toggle
  autoGenerateStates?: boolean
  generatedStates?: GraphState[]
  // State Configuration
  startStates?: StartStateConfig[]
  duringStates?: string[]
  duringStateTargets?: DuringStateTarget[]
  endStates?: EndStateConfig[]
  // Available data
  availableStates?: string[]
  availableAgents?: Array<{ id: string; name: string }>
  availableWaypoints?: Array<{ id: string; name: string }>
  isEditing?: boolean
  isParameterEditing?: boolean
  // PDDL Planning fields
  resourceAcquire?: string[]
  resourceRelease?: string[]
  planningPreconditions?: PlanningCondition[]
  planningEffects?: PlanningEffect[]
  planningDuring?: PlanningEffect[]
  hasPlanningStates?: boolean  // True if parent BT has planning_states defined
  taskDistributorId?: string
  taskDistributorStates?: TaskDistributorState[]
  taskDistributorResources?: TaskDistributorResource[]
  planningTask?: PlanningTaskSpec
  taskTemplateName?: string
  onTaskPlanningDuringChange?: (variable: string, value?: string) => void
  onTaskPlanningResultUpsert?: (effect: PlanningEffect) => void
  onTaskPlanningResultDelete?: (variable: string) => void
}

export type NormalizedDuringStateTarget = {
  state: string
  target_type: NonNullable<DuringStateTarget['target_type']>
  agent_id?: string
}

export type InputType = 'number' | 'text' | 'checkbox' | 'pose' | 'trajectory' | 'complex'

export interface AvailableStep {
  id: string
  name: string
  resultFields: Array<{ name: string; type: string }>
}
