import type { StartStateConfig, EndStateConfig, GraphState, ParameterFieldSource, DuringStateTarget } from '../../../types'

export interface StateActionNodeData {
  label: string
  subtype: string
  color: string
  actionType?: string
  server?: string
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
