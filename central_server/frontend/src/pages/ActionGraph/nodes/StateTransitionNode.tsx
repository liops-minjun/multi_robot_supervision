import { memo } from 'react'
import { Handle, Position, NodeProps, useReactFlow } from 'reactflow'
import type { Condition } from '../../../components/ConditionBuilder'
import { ConditionPreview } from '../../../components/ConditionBuilder'
import { Filter } from 'lucide-react'

interface StateTransitionNodeData {
  label: string
  fromState: string
  toState: string
  color: string
  preconditions?: Condition[]
  availableStates?: string[]
}

const StateTransitionNode = memo(({ id, data, selected }: NodeProps<StateTransitionNodeData>) => {
  const { setNodes } = useReactFlow()
  const states = data.availableStates || []
  const preconditions = data.preconditions || []

  const updateData = (field: string, value: any) => {
    setNodes((nds) => nds.map((n) =>
      n.id === id ? { ...n, data: { ...n.data, [field]: value } } : n
    ))
  }

  // Default gray color for states
  const fromColor = '#6b7280'
  const toColor = '#6b7280'

  return (
    <div
      draggable={false}
      className={`
        min-w-[180px] rounded-lg overflow-hidden
        bg-surface border-2
        shadow-lg
        ${selected ? 'border-white/60 shadow-xl' : 'border-primary'}
        transition-all duration-150
      `}
    >
      {/* Header */}
      <div className="px-3 py-2 bg-gradient-to-r from-surface to-elevated flex items-center justify-between">
        <span className="text-xs font-semibold text-primary">{data.label}</span>
        {preconditions.length > 0 && (
          <div className="flex items-center gap-1 px-1.5 py-0.5 bg-purple-500/20 rounded">
            <Filter className="w-2.5 h-2.5 text-purple-400" />
            <span className="text-[9px] text-purple-400">{preconditions.length}</span>
          </div>
        )}
      </div>

      {/* Preconditions Preview */}
      {preconditions.length > 0 && (
        <div className="px-3 py-1.5 border-b border-primary bg-surface">
          <ConditionPreview conditions={preconditions} />
        </div>
      )}

      {/* State Transition */}
      <div className="px-3 py-3">
        <div className="flex items-center gap-2">
          {/* From State */}
          <div className="flex-1">
            <div className="text-[9px] text-muted uppercase tracking-wider mb-1">From</div>
            <select
              value={data.fromState || states[0]}
              onChange={(e) => updateData('fromState', e.target.value)}
              onClick={(e) => e.stopPropagation()}
              className="w-full px-2 py-1 rounded text-[11px] focus:outline-none cursor-pointer"
              style={{
                backgroundColor: `${fromColor}20`,
                borderColor: `${fromColor}50`,
                color: fromColor,
                borderWidth: '1px',
              }}
            >
              {states.map(s => <option key={s} value={s}>{s}</option>)}
            </select>
          </div>

          {/* Arrow */}
          <div className="flex items-center justify-center w-8 pt-4">
            <svg className="w-5 h-5 text-muted" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 7l5 5m0 0l-5 5m5-5H6" />
            </svg>
          </div>

          {/* To State */}
          <div className="flex-1">
            <div className="text-[9px] text-muted uppercase tracking-wider mb-1">To</div>
            <select
              value={data.toState || states[0]}
              onChange={(e) => updateData('toState', e.target.value)}
              onClick={(e) => e.stopPropagation()}
              className="w-full px-2 py-1 rounded text-[11px] focus:outline-none cursor-pointer"
              style={{
                backgroundColor: `${toColor}20`,
                borderColor: `${toColor}50`,
                color: toColor,
                borderWidth: '1px',
              }}
            >
              {states.map(s => <option key={s} value={s}>{s}</option>)}
            </select>
          </div>
        </div>
      </div>

      {/* Footer */}
      <div className="px-3 py-1 border-t border-primary bg-surface">
        <span className="text-[9px] text-muted uppercase tracking-wider">State Transition</span>
      </div>

      {/* Handles */}
      <Handle
        type="target"
        position={Position.Left}
        id="in"
        className="!w-3 !h-3 !bg-blue-500 !border-2 !border-blue-300 !rounded-full !-left-1.5"
        style={{ top: '50%' }}
      />
      <Handle
        type="source"
        position={Position.Right}
        id="out"
        className="!w-3 !h-3 !bg-green-500 !border-2 !border-green-300 !rounded-full !-right-1.5"
        style={{ top: '50%' }}
      />
    </div>
  )
})

StateTransitionNode.displayName = 'StateTransitionNode'

export default StateTransitionNode
