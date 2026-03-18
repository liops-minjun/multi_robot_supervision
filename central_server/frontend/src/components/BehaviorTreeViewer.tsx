import { useMemo, useCallback } from 'react'
import ReactFlow, {
  Node,
  Edge,
  Controls,
  Background,
  MiniMap,
  BackgroundVariant,
  MarkerType,
  ReactFlowProvider,
} from 'reactflow'
import 'reactflow/dist/style.css'
import { memo } from 'react'
import { Handle, Position, NodeProps } from 'reactflow'
import type { ActionGraph, StateDefinition } from '../types'

// Color palette for different action types
const ACTION_COLORS: Record<string, string> = {
  'nav2_msgs/NavigateToPose': '#fb7185',
  'control_msgs/FollowJointTrajectory': '#f97316',
  'control_msgs/GripperCommand': '#ef4444',
  'std_srvs/Trigger': '#0ea5e9',
}

const START_NODE_ID = '__behavior_tree_start__'
const START_NODE_COLOR = '#22c55e'

const getActionColor = (actionType: string): string => {
  return ACTION_COLORS[actionType] || '#f87171'
}

const inferCapabilityKindFromActionType = (actionType?: string): 'action' | 'service' => {
  const normalizedType = (actionType || '').toLowerCase()
  return normalizedType.includes('/srv/') ? 'service' : 'action'
}

const normalizeCapabilityKind = (kind?: string, actionType?: string): 'action' | 'service' => {
  const normalizedKind = (kind || '').toLowerCase()
  if (normalizedKind === 'service') return 'service'
  if (normalizedKind === 'action') return 'action'
  return inferCapabilityKindFromActionType(actionType)
}

const clamp = (value: number, min: number, max: number): number => {
  return Math.min(max, Math.max(min, value))
}

const median = (values: number[]): number => {
  if (values.length === 0) return 0
  const sorted = [...values].sort((a, b) => a - b)
  const middle = Math.floor(sorted.length / 2)
  return sorted.length % 2 === 0
    ? (sorted[middle - 1] + sorted[middle]) / 2
    : sorted[middle]
}

const positiveAxisDiffs = (values: number[]): number[] => {
  if (values.length < 2) return []
  const sorted = Array.from(new Set(values)).sort((a, b) => a - b)
  const diffs: number[] = []
  for (let i = 1; i < sorted.length; i += 1) {
    const diff = sorted[i] - sorted[i - 1]
    if (Number.isFinite(diff) && diff > 1) {
      diffs.push(diff)
    }
  }
  return diffs
}

const normalizeOutcome = (outcome?: string): string => {
  const normalized = (outcome || '').toLowerCase()
  if (!normalized) return ''
  if (normalized === 'success' || normalized === 'succeeded' || normalized === 'done') {
    return 'success'
  }
  if (normalized === 'failed' || normalized === 'failure' || normalized === 'error' || normalized === 'abort') {
    return 'failed'
  }
  return normalized
}

// Compact Action Node for Viewer
const ViewerActionNode = memo(({ data, selected }: NodeProps<any>) => {
  const color = data.color || '#3b82f6'
  const isActive = data.isActive
  const isCompleted = data.isCompleted
  const isFailed = data.isFailed

  let borderColor = '#2a2a4a'
  let glowClass = ''
  let glowStyle: React.CSSProperties = {}
  if (isActive) {
    borderColor = '#22d3ee'  // Cyan-400 for blue fluorescent
    glowClass = 'shadow-[0_0_15px_3px_rgba(34,211,238,0.6)]'  // Stronger cyan glow
    glowStyle = {
      boxShadow: '0 0 15px 3px rgba(34, 211, 238, 0.6), 0 0 30px 6px rgba(34, 211, 238, 0.3)',
      animation: 'pulse-glow 1.5s ease-in-out infinite'
    }
  } else if (isCompleted) {
    borderColor = '#22c55e'
  } else if (isFailed) {
    borderColor = '#ef4444'
  } else if (selected) {
    borderColor = 'white'
  }

  return (
    <div
      className={`
        min-w-[140px] rounded-lg overflow-hidden
        bg-surface border-2
        transition-all duration-300
        ${glowClass}
      `}
      style={{ borderColor, ...glowStyle }}
    >
      {/* Header */}
      <div
        className="px-2 py-1.5 flex items-center gap-2"
        style={{ backgroundColor: `${color}25` }}
      >
        <div
          className="w-2 h-2 rounded-sm flex-shrink-0"
          style={{ backgroundColor: color }}
        />
        <span className="text-[10px] font-semibold text-primary truncate flex-1">
          {data.label}
        </span>
        {isActive && (
          <div className="w-2 h-2 rounded-full bg-cyan-400 animate-pulse" />
        )}
        {isCompleted && (
          <svg className="w-3 h-3 text-green-400" fill="currentColor" viewBox="0 0 20 20">
            <path fillRule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clipRule="evenodd" />
          </svg>
        )}
        {isFailed && (
          <svg className="w-3 h-3 text-red-400" fill="currentColor" viewBox="0 0 20 20">
            <path fillRule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clipRule="evenodd" />
          </svg>
        )}
      </div>

      {/* State Info */}
      <div className="px-2 py-1.5 space-y-0.5">
        {data.duringState && (
          <div className="flex items-center gap-1">
            <span className="text-[9px] text-muted">state:</span>
            <span className="text-[9px] text-yellow-400 font-mono">{data.duringState}</span>
          </div>
        )}
        <div className="text-[9px] text-muted font-mono truncate">
          {data.server || data.subtype}
        </div>
      </div>

      {/* Handles */}
      <Handle
        type="target"
        position={Position.Left}
        id="state-in"
        className="!w-2 !h-2 !bg-blue-500 !border !border-blue-300 !rounded-full !-left-1"
      />
      <Handle
        type="source"
        position={Position.Right}
        id="success-out"
        className="!w-2 !h-2 !bg-green-500 !border !border-green-300 !rounded-full !-right-1"
        style={{ top: '40%' }}
      />
      <Handle
        type="source"
        position={Position.Right}
        id="failure-out"
        className="!w-2 !h-2 !bg-red-500 !border !border-red-300 !rounded-full !-right-1"
        style={{ top: '70%' }}
      />
    </div>
  )
})

ViewerActionNode.displayName = 'ViewerActionNode'

// Compact Event Node for Viewer
const ViewerEventNode = memo(({ data, selected }: NodeProps<any>) => {
  const isStart = data.subtype === 'Start'
  const isEnd = data.subtype === 'End'
  const isError = data.subtype === 'Error'
  const isWarning = data.subtype === 'Warning'
  const isActive = data.isActive
  const isCompleted = data.isCompleted

  const bgColor = isStart ? '#22c55e' : isError ? '#ef4444' : isWarning ? '#f59e0b' : '#3b82f6'

  let borderColor = bgColor
  let glowStyle: React.CSSProperties = {}
  if (isActive) {
    borderColor = '#22d3ee'  // Cyan-400 for blue fluorescent
    glowStyle = {
      boxShadow: '0 0 15px 3px rgba(34, 211, 238, 0.6), 0 0 30px 6px rgba(34, 211, 238, 0.3)',
      animation: 'pulse-glow 1.5s ease-in-out infinite'
    }
  } else if (selected) {
    borderColor = 'white'
  }

  return (
    <div
      className={`
        min-w-[80px] rounded-lg overflow-hidden
        bg-surface border-2
        transition-all duration-300
        ${isActive ? 'shadow-[0_0_15px_3px_rgba(34,211,238,0.6)]' : ''}
      `}
      style={{ borderColor, ...glowStyle }}
    >
      <div
        className="px-3 py-2 flex items-center justify-center gap-1.5"
        style={{ backgroundColor: bgColor }}
      >
        {isActive && (
          <div className="w-2 h-2 rounded-full bg-white animate-pulse" />
        )}
        <span className="text-[10px] font-bold text-primary tracking-wider">
          {isStart ? 'START' : isError ? 'ERROR' : isWarning ? 'WARNING' : 'END'}
        </span>
        {isCompleted && (
          <svg className="w-3 h-3 text-primary" fill="currentColor" viewBox="0 0 20 20">
            <path fillRule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clipRule="evenodd" />
          </svg>
        )}
      </div>

      {isStart && (
        <Handle
          type="source"
          position={Position.Right}
          id="state-out"
          className="!w-2 !h-2 !bg-green-500 !border !border-green-300 !rounded-full !-right-1"
        />
      )}
      {(isEnd || isError || isWarning) && (
        <Handle
          type="target"
          position={Position.Left}
          id="state-in"
          className={`!w-2 !h-2 !border !rounded-full !-left-1 ${
            isError
              ? '!bg-red-500 !border-red-300'
              : isWarning
                ? '!bg-yellow-500 !border-yellow-300'
                : '!bg-blue-500 !border-blue-300'
          }`}
        />
      )}
    </div>
  )
})

ViewerEventNode.displayName = 'ViewerEventNode'

// Compact Retry Node for Viewer
const VIEWER_RETRY_PORT_TOP = {
  out: '68%',
  input: '68%',
  failed: '84%',
} as const

const ViewerRetryNode = memo(({ data, selected }: NodeProps<any>) => {
  const isActive = data.isActive
  const borderColor = isActive ? '#22d3ee' : (selected ? 'white' : '#f59e0b')
  const maxRetries = Number.isFinite(data.maxRetries) ? Number(data.maxRetries) : 0
  const backoffMs = Number.isFinite(data.backoffMs) ? Number(data.backoffMs) : 0

  return (
    <div
      className={`
        w-[170px] min-h-[144px] rounded-lg overflow-visible
        bg-surface border-2 transition-all duration-300
        ${isActive ? 'shadow-[0_0_15px_3px_rgba(34,211,238,0.45)]' : ''}
      `}
      style={{ borderColor }}
    >
      <div className="px-2.5 py-1.5 border-b border-primary/40 bg-amber-500/15 flex items-center justify-between">
        <span className="text-[10px] font-semibold text-amber-300">Retry</span>
        <span className="text-[9px] text-muted">x{maxRetries}</span>
      </div>
      <div className="px-2 py-2">
        <div className="grid grid-cols-2 gap-2">
          <div className="rounded border border-primary/30 bg-base/50 px-1.5 py-1">
            <div className="text-[8px] uppercase tracking-wider text-muted">max_retries</div>
            <div className="text-[10px] font-mono text-primary">{maxRetries}</div>
          </div>
          <div className="rounded border border-primary/30 bg-base/50 px-1.5 py-1">
            <div className="text-[8px] uppercase tracking-wider text-muted">backoff_ms</div>
            <div className="text-[10px] font-mono text-primary">{backoffMs}</div>
          </div>
        </div>
      </div>

      <div className="pointer-events-none absolute left-2 -translate-y-1/2 rounded bg-base/90 px-1 py-0.5 text-[9px] font-medium text-amber-300" style={{ top: VIEWER_RETRY_PORT_TOP.out }}>out</div>
      <div className="pointer-events-none absolute right-2 -translate-y-1/2 rounded bg-base/90 px-1 py-0.5 text-[9px] font-medium text-cyan-300" style={{ top: VIEWER_RETRY_PORT_TOP.input }}>in</div>
      <div className="pointer-events-none absolute right-2 -translate-y-1/2 rounded bg-base/90 px-1 py-0.5 text-[9px] font-medium text-red-300" style={{ top: VIEWER_RETRY_PORT_TOP.failed }}>failed</div>

      <Handle
        type="source"
        position={Position.Left}
        id="retry-out"
        className="!w-2.5 !h-2.5 !bg-amber-500 !border !border-amber-300 !rounded-full !-left-1"
        style={{ top: VIEWER_RETRY_PORT_TOP.out }}
      />
      <Handle
        type="target"
        position={Position.Right}
        id="retry-in"
        className="!w-2.5 !h-2.5 !bg-cyan-400 !border !border-cyan-200 !rounded-full !-right-1"
        style={{ top: VIEWER_RETRY_PORT_TOP.input }}
      />
      <Handle
        type="source"
        position={Position.Right}
        id="retry-failed"
        className="!w-2.5 !h-2.5 !bg-red-500 !border !border-red-300 !rounded-full !-right-1"
        style={{ top: VIEWER_RETRY_PORT_TOP.failed }}
      />
    </div>
  )
})

ViewerRetryNode.displayName = 'ViewerRetryNode'

const nodeTypes = {
  action: ViewerActionNode,
  event: ViewerEventNode,
  retry: ViewerRetryNode,
}

interface BehaviorTreeViewerProps {
  behaviorTree: ActionGraph | null
  stateDef?: StateDefinition | null
  currentStepId?: string | null
  completedSteps?: string[]
  failedSteps?: string[]
  className?: string
  compact?: boolean
  showControls?: boolean
  showMiniMap?: boolean
}

// Backward compatibility alias - allows either behaviorTree or actionGraph prop
interface ActionGraphViewerProps {
  behaviorTree?: ActionGraph | null
  actionGraph?: ActionGraph | null
  stateDef?: StateDefinition | null
  currentStepId?: string | null
  completedSteps?: string[]
  failedSteps?: string[]
  className?: string
  compact?: boolean
  showControls?: boolean
  showMiniMap?: boolean
}

function BehaviorTreeViewerInner({
  behaviorTree,
  stateDef,
  currentStepId,
  completedSteps = [],
  failedSteps = [],
  className = '',
  compact = false,
  showControls = true,
  showMiniMap = false,
}: BehaviorTreeViewerProps) {
  const convertBehaviorTreeToGraph = useCallback((): { nodes: Node[]; edges: Edge[] } => {
    if (!behaviorTree) return { nodes: [], edges: [] }

    const nodes: Node[] = []
    const edges: Edge[] = []
    const actionMappings = stateDef?.action_mappings || []
    const entryPoint = behaviorTree.entry_point || behaviorTree.steps[0]?.id
    const fallbackCols = compact ? 2 : 3
    const fallbackBaseX = compact ? 50 : 300
    const fallbackBaseY = compact ? 50 : 100
    const fallbackSpacingX = compact ? 200 : 300
    const fallbackSpacingY = compact ? 110 : 200
    const stepPositions = new Map<string, { x: number; y: number }>()

    behaviorTree.steps.forEach((step, index) => {
      const storedX = step.ui?.x
      const storedY = step.ui?.y
      const hasStoredPosition =
        typeof storedX === 'number' &&
        typeof storedY === 'number' &&
        Number.isFinite(storedX) &&
        Number.isFinite(storedY)

      const x = hasStoredPosition ? storedX : fallbackBaseX + (index % fallbackCols) * fallbackSpacingX
      const y = hasStoredPosition ? storedY : fallbackBaseY + Math.floor(index / fallbackCols) * fallbackSpacingY
      stepPositions.set(step.id, { x, y })
    })

    // Preview cards are intentionally smaller than editor cards.
    // Compact the coordinate space so ordering stays the same while spacing fits viewer scale.
    const displayPositions = new Map<string, { x: number; y: number }>()
    if (stepPositions.size > 0) {
      const positions = Array.from(stepPositions.values())
      const minX = Math.min(...positions.map((position) => position.x))
      const minY = Math.min(...positions.map((position) => position.y))

      const xDiffs = positiveAxisDiffs(positions.map((position) => position.x))
      const yDiffs = positiveAxisDiffs(positions.map((position) => position.y))
      const sourceSpacingX = median(xDiffs) || fallbackSpacingX
      const sourceSpacingY = median(yDiffs) || fallbackSpacingY

      const targetSpacingX = compact ? 170 : 235
      const targetSpacingY = compact ? 95 : 145

      // Keep relative layout but shrink oversized editor spacing for preview readability.
      const scaleX = clamp(targetSpacingX / sourceSpacingX, compact ? 0.7 : 0.5, 1)
      const scaleY = clamp(targetSpacingY / sourceSpacingY, compact ? 0.72 : 0.5, 1)

      const previewBaseX = compact ? 50 : 100
      const previewBaseY = compact ? 50 : 100

      stepPositions.forEach((position, stepId) => {
        displayPositions.set(stepId, {
          x: previewBaseX + (position.x - minX) * scaleX,
          y: previewBaseY + (position.y - minY) * scaleY,
        })
      })
    }

    let startX = compact ? 20 : 80
    let startY = compact ? 20 : 100
    const entryPosition = entryPoint ? displayPositions.get(entryPoint) : null
    const startOffsetX = compact ? 130 : 190
    if (entryPosition) {
      startX = entryPosition.x - startOffsetX
      startY = entryPosition.y
    } else if (displayPositions.size > 0) {
      const positions = Array.from(displayPositions.values())
      const minX = Math.min(...positions.map((position) => position.x))
      const sumY = positions.reduce((acc, position) => acc + position.y, 0)
      startX = minX - startOffsetX
      startY = sumY / positions.length
    }

    nodes.push({
      id: START_NODE_ID,
      type: 'event',
      position: { x: startX, y: startY },
      data: {
        label: 'Start',
        subtype: 'Start',
        color: START_NODE_COLOR,
      },
    })

    behaviorTree.steps.forEach((step, index) => {
      const fallbackX = fallbackBaseX + (index % fallbackCols) * fallbackSpacingX
      const fallbackY = fallbackBaseY + Math.floor(index / fallbackCols) * fallbackSpacingY
      const position = displayPositions.get(step.id) || { x: fallbackX, y: fallbackY }

      let subtype = step.action?.server || step.action?.type || 'Unknown'
      const capabilityKind = normalizeCapabilityKind(step.action?.capability_kind, step.action?.type)
      let color = capabilityKind === 'service' ? '#0ea5e9' : '#f87171'
      let duringState: string | undefined

      const mapping = actionMappings.find(m => m.action_type === step.action?.type) ||
        actionMappings.find(m => m.server === step.action?.server)
      if (mapping && capabilityKind !== 'service') {
        color = getActionColor(mapping.action_type)
        duringState = mapping.during_states?.[0] || mapping.during_state
      } else if (step.action?.type) {
        color = capabilityKind === 'service' ? '#0ea5e9' : getActionColor(step.action.type)
      }
      const stepDuringTargets = step.duringStateTargets || step.during_state_targets
      const stepDuringStates = step.duringStates || step.during_states
      if (stepDuringTargets && stepDuringTargets.length > 0) {
        const selfTarget = stepDuringTargets.find(target => !target.target_type || target.target_type === 'self')
        duringState = (selfTarget || stepDuringTargets[0]).state
      } else if (stepDuringStates && stepDuringStates.length > 0) {
        duringState = stepDuringStates[0]
      }

      const isTerminal = step.type === 'terminal'
      const nodeType = isTerminal ? 'event' : 'action'
      const isActive = currentStepId === step.id
      const isCompleted = completedSteps.includes(step.id)
      const isFailed = failedSteps.includes(step.id)

      nodes.push({
        id: step.id,
        type: nodeType,
        position,
        data: {
          label: step.job_name || step.name || step.id,
          subtype: isTerminal
            ? ((step.terminal_type === 'failure'
                ? 'Error'
                : (step.alert ? 'Warning' : 'End')))
            : subtype,
          color,
          server: step.action?.server,
          duringState,
          isActive,
          isCompleted,
          isFailed,
        },
      })

      const failureTransitionRaw = step.transition?.on_failure as unknown
      const failureTransitionObj =
        failureTransitionRaw && typeof failureTransitionRaw === 'object'
          ? failureTransitionRaw as Record<string, unknown>
          : undefined
      const retryCount = Math.max(0, Number((failureTransitionObj?.retry as number | undefined) ?? 0))
      const retryBackoffMs = Math.max(
        0,
        Number(
          (failureTransitionObj?.backoff_ms as number | undefined) ??
          (failureTransitionObj?.backoffMs as number | undefined) ??
          0
        )
      )
      const retryFallbackTarget =
        typeof failureTransitionObj?.fallback === 'string'
          ? failureTransitionObj.fallback
          : (
            typeof failureTransitionObj?.next === 'string'
              ? failureTransitionObj.next
              : (typeof failureTransitionRaw === 'string' ? failureTransitionRaw : '')
          )
      const retryUi = failureTransitionObj?.ui && typeof failureTransitionObj.ui === 'object'
        ? failureTransitionObj.ui as Record<string, unknown>
        : undefined
      const retryStoredX = typeof retryUi?.x === 'number' && Number.isFinite(retryUi.x) ? retryUi.x : undefined
      const retryStoredY = typeof retryUi?.y === 'number' && Number.isFinite(retryUi.y) ? retryUi.y : undefined
      const hasRetryBlock = retryCount > 0 || retryBackoffMs > 0
      const retryNodeId = `${step.id}__retry_view`
      let retryInputEdgeAdded = false
      let retryLoopEdgeAdded = false
      let retryFallbackEdgeAdded = false

      const ensureRetryNode = () => {
        if (!hasRetryBlock) return
        if (nodes.some(node => node.id === retryNodeId)) return
        nodes.push({
          id: retryNodeId,
          type: 'retry',
          position: { x: retryStoredX ?? (position.x + 180), y: retryStoredY ?? (position.y + 76) },
          data: {
            label: 'Retry',
            maxRetries: retryCount,
            backoffMs: retryBackoffMs,
            isActive,
          },
        })
      }

      const addRetryInputEdge = (sourceHandle = 'failure-out') => {
        if (!hasRetryBlock || retryInputEdgeAdded) return
        ensureRetryNode()
        edges.push({
          id: `${step.id}-fail->${retryNodeId}`,
          source: step.id,
          target: retryNodeId,
          sourceHandle,
          targetHandle: 'retry-in',
          type: 'smoothstep',
          markerEnd: { type: MarkerType.ArrowClosed, color: '#f59e0b' },
          style: { stroke: '#f59e0b', strokeWidth: 1.8, strokeDasharray: '4,4' },
        })
        retryInputEdgeAdded = true
      }

      const addRetryLoopEdge = () => {
        if (!hasRetryBlock || retryLoopEdgeAdded) return
        ensureRetryNode()
        edges.push({
          id: `${retryNodeId}->${step.id}::retry`,
          source: retryNodeId,
          target: step.id,
          sourceHandle: 'retry-out',
          targetHandle: 'state-in',
          type: 'smoothstep',
          markerEnd: { type: MarkerType.ArrowClosed, color: '#f59e0b' },
          style: { stroke: '#f59e0b', strokeWidth: 1.6, strokeDasharray: '5,5' },
        })
        retryLoopEdgeAdded = true
      }

      const addRetryFallbackEdge = (target?: string) => {
        const fallbackTarget = target || retryFallbackTarget
        if (!hasRetryBlock || retryFallbackEdgeAdded || !fallbackTarget) return
        ensureRetryNode()
        const fallbackStep = behaviorTree.steps.find(s => s.id === fallbackTarget)
        const fallbackSubtype = fallbackStep
          ? (fallbackStep.terminal_type === 'failure' ? 'Error' : (fallbackStep.alert ? 'Warning' : 'End'))
          : ''
        const edgeColor = fallbackSubtype === 'Warning' ? '#f59e0b' : '#ef4444'
        const sourceHandle = 'retry-failed'
        edges.push({
          id: `${retryNodeId}->${fallbackTarget}::fallback`,
          source: retryNodeId,
          target: fallbackTarget,
          sourceHandle,
          targetHandle: 'state-in',
          type: 'smoothstep',
          markerEnd: { type: MarkerType.ArrowClosed, color: edgeColor },
          style: { stroke: edgeColor, strokeWidth: 1.6, strokeDasharray: '4,4' },
        })
        retryFallbackEdgeAdded = true
      }

      if (hasRetryBlock) {
        addRetryLoopEdge()
        addRetryFallbackEdge()
      }

      const outcomeTransitions = step.transition?.on_outcomes || []
      if (outcomeTransitions.length > 0) {
        outcomeTransitions.forEach((transition, outcomeIndex) => {
          if (!transition.next) return
          const normalizedOutcome = normalizeOutcome(transition.outcome)
          const isSuccessOutcome = normalizedOutcome === 'success'
          if (hasRetryBlock && !isSuccessOutcome) {
            addRetryInputEdge('failure-out')
            addRetryFallbackEdge(transition.next)
            return
          }
          const sourceHandle = isSuccessOutcome ? 'success-out' : 'failure-out'
          const edgeColor = isSuccessOutcome
            ? (isActive ? '#22d3ee' : '#22c55e')
            : '#ef4444'
          edges.push({
            id: `${step.id}-outcome-${outcomeIndex}->${transition.next}`,
            source: step.id,
            target: transition.next,
            sourceHandle,
            targetHandle: 'state-in',
            type: 'smoothstep',
            markerEnd: { type: MarkerType.ArrowClosed, color: edgeColor },
            style: {
              stroke: edgeColor,
              strokeWidth: isSuccessOutcome && isActive ? 2.5 : 1.5,
              strokeDasharray: isSuccessOutcome ? undefined : '4,4',
            },
            animated: isSuccessOutcome && isActive,
          })
        })
        if (hasRetryBlock && !retryInputEdgeAdded) {
          addRetryInputEdge('failure-out')
        }
        if (hasRetryBlock && !retryLoopEdgeAdded) {
          addRetryLoopEdge()
        }
        if (hasRetryBlock && !retryFallbackEdgeAdded) {
          addRetryFallbackEdge()
        }
      } else {
        if (step.transition?.on_success) {
          const target = typeof step.transition.on_success === 'string'
            ? step.transition.on_success
            : step.transition.on_success.next

          if (target) {
            // Use cyan color for active edges, green for inactive
            const edgeColor = isActive ? '#22d3ee' : '#22c55e'
            edges.push({
              id: `${step.id}->${target}`,
              source: step.id,
              target,
              sourceHandle: 'success-out',
              targetHandle: 'state-in',
              type: 'smoothstep',
              markerEnd: { type: MarkerType.ArrowClosed, color: edgeColor },
              style: { stroke: edgeColor, strokeWidth: isActive ? 2.5 : 1.5 },
              animated: isActive,
            })
          }
        }

        if (step.transition?.on_failure) {
          const failureTransition = step.transition.on_failure
          const target = typeof failureTransition === 'string'
            ? failureTransition
            : (failureTransition.fallback || (failureTransition as { next?: string }).next)

          if (hasRetryBlock) {
            addRetryInputEdge('failure-out')
            addRetryFallbackEdge(target)
          } else if (target) {
            edges.push({
              id: `${step.id}-fail->${target}`,
              source: step.id,
              target,
              sourceHandle: 'failure-out',
              targetHandle: 'state-in',
              type: 'smoothstep',
              markerEnd: { type: MarkerType.ArrowClosed, color: '#ef4444' },
              style: { stroke: '#ef4444', strokeWidth: 1.5, strokeDasharray: '4,4' },
            })
          }
        }
      }
    })

    if (entryPoint) {
      edges.push({
        id: `${START_NODE_ID}->${entryPoint}`,
        source: START_NODE_ID,
        target: entryPoint,
        sourceHandle: 'state-out',
        targetHandle: 'state-in',
        type: 'smoothstep',
        markerEnd: { type: MarkerType.ArrowClosed, color: START_NODE_COLOR },
        style: { stroke: START_NODE_COLOR, strokeWidth: 1.5 },
      })
    }

    return { nodes, edges }
  }, [behaviorTree, stateDef, currentStepId, completedSteps, failedSteps, compact])

  // Use computed nodes/edges directly (no state hooks needed since nodes aren't draggable)
  // This ensures nodes update immediately when currentStepId changes
  const { nodes, edges } = useMemo(() => convertBehaviorTreeToGraph(), [convertBehaviorTreeToGraph])

  if (!behaviorTree) {
    return (
      <div className={`flex items-center justify-center bg-base text-muted ${className}`}>
        <p className="text-sm">No behavior tree selected</p>
      </div>
    )
  }

  return (
    <div className={`bg-base ${className}`}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        fitView
        fitViewOptions={{ padding: 0.2 }}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable={false}
        panOnDrag={!compact}
        zoomOnScroll={!compact}
        preventScrolling={compact}
        proOptions={{ hideAttribution: true }}
      >
        <Background
          variant={BackgroundVariant.Dots}
          gap={16}
          size={1}
          color="#2a2a4a"
        />
        {showControls && !compact && (
          <Controls
            className="!bg-surface !border-primary !rounded-lg [&>button]:!bg-surface [&>button]:!border-primary [&>button]:!text-primary [&>button:hover]:!bg-elevated"
          />
        )}
        {showMiniMap && !compact && (
          <MiniMap
            nodeColor={(node) => node.data.color || '#3b82f6'}
            maskColor="rgba(0,0,0,0.8)"
            className="!bg-surface !rounded-lg"
          />
        )}
      </ReactFlow>
    </div>
  )
}

export default function BehaviorTreeViewer(props: BehaviorTreeViewerProps) {
  return (
    <ReactFlowProvider>
      <BehaviorTreeViewerInner {...props} />
    </ReactFlowProvider>
  )
}

// Backward compatibility wrapper that accepts actionGraph prop
export function ActionGraphViewer(props: ActionGraphViewerProps) {
  const behaviorTree = props.behaviorTree || props.actionGraph || null
  return (
    <ReactFlowProvider>
      <BehaviorTreeViewerInner {...props} behaviorTree={behaviorTree} />
    </ReactFlowProvider>
  )
}

// Mini version for embedding in cards
export function BehaviorTreeMini({
  behaviorTree,
  currentStepId,
  completedSteps,
}: {
  behaviorTree: ActionGraph | null
  currentStepId?: string | null
  completedSteps?: string[]
}) {
  if (!behaviorTree) return null

  const totalSteps = behaviorTree.steps.length
  const currentIndex = behaviorTree.steps.findIndex(s => s.id === currentStepId)

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between text-xs">
        <span className="text-secondary">{behaviorTree.name}</span>
        <span className="text-blue-400">
          {currentIndex + 1} / {totalSteps}
        </span>
      </div>
      <div className="flex gap-1">
        {behaviorTree.steps.map((step) => {
          const isCurrent = step.id === currentStepId
          const isCompleted = completedSteps?.includes(step.id)

          let bgColor = 'bg-gray-700'
          if (isCurrent) bgColor = 'bg-blue-500 animate-pulse'
          else if (isCompleted) bgColor = 'bg-green-500'

          return (
            <div
              key={step.id}
              className={`flex-1 h-2 rounded-full ${bgColor} transition-colors`}
              title={step.job_name || step.name || step.id}
            />
          )
        })}
      </div>
      {currentStepId && (
        <p className="text-[10px] text-muted truncate">
          Current: {behaviorTree.steps.find(s => s.id === currentStepId)?.job_name || behaviorTree.steps.find(s => s.id === currentStepId)?.name || currentStepId}
        </p>
      )}
    </div>
  )
}

// Backward compatibility alias
export function ActionGraphMini({
  actionGraph,
  currentStepId,
  completedSteps,
}: {
  actionGraph: ActionGraph | null
  currentStepId?: string | null
  completedSteps?: string[]
}) {
  return (
    <BehaviorTreeMini
      behaviorTree={actionGraph}
      currentStepId={currentStepId}
      completedSteps={completedSteps}
    />
  )
}
