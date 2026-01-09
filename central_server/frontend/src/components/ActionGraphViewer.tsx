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
  useNodesState,
  useEdgesState,
} from 'reactflow'
import 'reactflow/dist/style.css'
import { memo } from 'react'
import { Handle, Position, NodeProps } from 'reactflow'
import type { ActionGraph, StateDefinition } from '../types'

// Color palette for different action types
const ACTION_COLORS: Record<string, string> = {
  'nav2_msgs/NavigateToPose': '#3b82f6',
  'control_msgs/FollowJointTrajectory': '#8b5cf6',
  'control_msgs/GripperCommand': '#f59e0b',
  'std_srvs/Trigger': '#06b6d4',
}

const getActionColor = (actionType: string): string => {
  return ACTION_COLORS[actionType] || '#6b7280'
}

// Compact Action Node for Viewer
const ViewerActionNode = memo(({ data, selected }: NodeProps<any>) => {
  const color = data.color || '#3b82f6'
  const isActive = data.isActive
  const isCompleted = data.isCompleted
  const isFailed = data.isFailed

  let borderColor = '#2a2a4a'
  let glowClass = ''
  if (isActive) {
    borderColor = '#22c55e'
    glowClass = 'shadow-lg shadow-green-500/30'
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
        bg-[#1e1e2e] border-2
        transition-all duration-300
        ${glowClass}
      `}
      style={{ borderColor }}
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
        <span className="text-[10px] font-semibold text-white truncate flex-1">
          {data.label}
        </span>
        {isActive && (
          <div className="w-2 h-2 rounded-full bg-green-500 animate-pulse" />
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
            <span className="text-[9px] text-gray-500">state:</span>
            <span className="text-[9px] text-yellow-400 font-mono">{data.duringState}</span>
          </div>
        )}
        <div className="text-[9px] text-gray-600 font-mono truncate">
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
  const isActive = data.isActive
  const isCompleted = data.isCompleted

  const bgColor = isStart ? '#22c55e' : isError ? '#ef4444' : '#3b82f6'

  let borderColor = bgColor
  if (isActive) {
    borderColor = '#22c55e'
  } else if (selected) {
    borderColor = 'white'
  }

  return (
    <div
      className={`
        min-w-[80px] rounded-lg overflow-hidden
        bg-[#1e1e2e] border-2
        transition-all duration-300
        ${isActive ? 'shadow-lg shadow-green-500/30' : ''}
      `}
      style={{ borderColor }}
    >
      <div
        className="px-3 py-2 flex items-center justify-center gap-1.5"
        style={{ backgroundColor: bgColor }}
      >
        {isActive && (
          <div className="w-2 h-2 rounded-full bg-white animate-pulse" />
        )}
        <span className="text-[10px] font-bold text-white tracking-wider">
          {isStart ? 'START' : isError ? 'ERROR' : 'END'}
        </span>
        {isCompleted && (
          <svg className="w-3 h-3 text-white" fill="currentColor" viewBox="0 0 20 20">
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
      {(isEnd || isError) && (
        <Handle
          type="target"
          position={Position.Left}
          id="state-in"
          className={`!w-2 !h-2 !border !rounded-full !-left-1 ${
            isError ? '!bg-red-500 !border-red-300' : '!bg-blue-500 !border-blue-300'
          }`}
        />
      )}
    </div>
  )
})

ViewerEventNode.displayName = 'ViewerEventNode'

const nodeTypes = {
  action: ViewerActionNode,
  event: ViewerEventNode,
}

interface ActionGraphViewerProps {
  actionGraph: ActionGraph | null
  stateDef?: StateDefinition | null
  currentStepId?: string | null
  completedSteps?: string[]
  failedSteps?: string[]
  className?: string
  compact?: boolean
  showControls?: boolean
  showMiniMap?: boolean
}

function ActionGraphViewerInner({
  actionGraph,
  stateDef,
  currentStepId,
  completedSteps = [],
  failedSteps = [],
  className = '',
  compact = false,
  showControls = true,
  showMiniMap = false,
}: ActionGraphViewerProps) {
  const convertActionGraphToGraph = useCallback((): { nodes: Node[]; edges: Edge[] } => {
    if (!actionGraph) return { nodes: [], edges: [] }

    const nodes: Node[] = []
    const edges: Edge[] = []
    const actionMappings = stateDef?.action_mappings || []

    actionGraph.steps.forEach((step, index) => {
      const cols = compact ? 2 : 3
      const x = 50 + (index % cols) * (compact ? 200 : 280)
      const y = 50 + Math.floor(index / cols) * (compact ? 100 : 150)

      let subtype = step.action?.server || step.action?.type || 'Unknown'
      let color = '#6b7280'
      let duringState: string | undefined

      const mapping = actionMappings.find(m => m.action_type === step.action?.type) ||
        actionMappings.find(m => m.server === step.action?.server)
      if (mapping) {
        color = getActionColor(mapping.action_type)
        duringState = mapping.during_states?.[0] || mapping.during_state
      } else if (step.action?.type) {
        color = getActionColor(step.action.type)
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
        position: { x, y },
        data: {
          label: step.name || step.id,
          subtype: isTerminal ? (step.terminal_type === 'success' ? 'End' : 'Error') : subtype,
          color,
          server: step.action?.server,
          duringState,
          isActive,
          isCompleted,
          isFailed,
        },
      })

      if (step.transition?.on_success) {
        const target = typeof step.transition.on_success === 'string'
          ? step.transition.on_success
          : step.transition.on_success.next

        if (target) {
          edges.push({
            id: `${step.id}->${target}`,
            source: step.id,
            target,
            sourceHandle: 'success-out',
            targetHandle: 'state-in',
            type: 'smoothstep',
            markerEnd: { type: MarkerType.ArrowClosed, color: '#22c55e' },
            style: { stroke: '#22c55e', strokeWidth: 1.5 },
            animated: isActive,
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
            sourceHandle: 'failure-out',
            targetHandle: 'state-in',
            type: 'smoothstep',
            markerEnd: { type: MarkerType.ArrowClosed, color: '#ef4444' },
            style: { stroke: '#ef4444', strokeWidth: 1.5, strokeDasharray: '4,4' },
          })
        }
      }
    })

    return { nodes, edges }
  }, [actionGraph, stateDef, currentStepId, completedSteps, failedSteps, compact])

  const { nodes: initialNodes, edges: initialEdges } = useMemo(() => convertActionGraphToGraph(), [convertActionGraphToGraph])
  const [nodes] = useNodesState(initialNodes)
  const [edges] = useEdgesState(initialEdges)

  if (!actionGraph) {
    return (
      <div className={`flex items-center justify-center bg-[#0f0f1a] text-gray-500 ${className}`}>
        <p className="text-sm">No action graph selected</p>
      </div>
    )
  }

  return (
    <div className={`bg-[#0f0f1a] ${className}`}>
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
            className="!bg-[#16162a] !border-[#2a2a4a] !rounded-lg [&>button]:!bg-[#16162a] [&>button]:!border-[#2a2a4a] [&>button]:!text-white [&>button:hover]:!bg-[#2a2a4a]"
          />
        )}
        {showMiniMap && !compact && (
          <MiniMap
            nodeColor={(node) => node.data.color || '#3b82f6'}
            maskColor="rgba(0,0,0,0.8)"
            className="!bg-[#16162a] !rounded-lg"
          />
        )}
      </ReactFlow>
    </div>
  )
}

export default function ActionGraphViewer(props: ActionGraphViewerProps) {
  return (
    <ReactFlowProvider>
      <ActionGraphViewerInner {...props} />
    </ReactFlowProvider>
  )
}

// Mini version for embedding in cards
export function ActionGraphMini({
  actionGraph,
  currentStepId,
  completedSteps,
}: {
  actionGraph: ActionGraph | null
  currentStepId?: string | null
  completedSteps?: string[]
}) {
  if (!actionGraph) return null

  const totalSteps = actionGraph.steps.length
  const currentIndex = actionGraph.steps.findIndex(s => s.id === currentStepId)

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between text-xs">
        <span className="text-gray-400">{actionGraph.name}</span>
        <span className="text-blue-400">
          {currentIndex + 1} / {totalSteps}
        </span>
      </div>
      <div className="flex gap-1">
        {actionGraph.steps.map((step) => {
          const isCurrent = step.id === currentStepId
          const isCompleted = completedSteps?.includes(step.id)

          let bgColor = 'bg-gray-700'
          if (isCurrent) bgColor = 'bg-blue-500 animate-pulse'
          else if (isCompleted) bgColor = 'bg-green-500'

          return (
            <div
              key={step.id}
              className={`flex-1 h-2 rounded-full ${bgColor} transition-colors`}
              title={step.name || step.id}
            />
          )
        })}
      </div>
      {currentStepId && (
        <p className="text-[10px] text-gray-500 truncate">
          Current: {actionGraph.steps.find(s => s.id === currentStepId)?.name || currentStepId}
        </p>
      )}
    </div>
  )
}
