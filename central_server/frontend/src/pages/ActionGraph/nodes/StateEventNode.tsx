import { memo } from 'react'
import { Handle, Position, NodeProps } from 'reactflow'

interface StateEventNodeData {
  label: string
  subtype: 'Start' | 'End' | 'Error'
  color: string
  initialState?: string
  finalState?: string
}

const StateEventNode = memo(({ data, selected }: NodeProps<StateEventNodeData>) => {
  const isStart = data.subtype === 'Start'
  const isEnd = data.subtype === 'End'
  const isError = data.subtype === 'Error'

  const bgColor = isStart ? '#22c55e' : isError ? '#ef4444' : '#3b82f6'
  const label = isStart ? 'START' : isError ? 'ERROR' : 'END'

  return (
    <div
      draggable={false}
      className={`
        relative min-w-[140px] rounded-lg overflow-visible
        bg-surface border-2
        shadow-lg
        ${selected ? 'border-white/60 shadow-xl' : 'border-primary'}
        transition-all duration-150
      `}
      style={{ borderColor: selected ? 'white' : bgColor }}
    >
      {/* Header */}
      <div
        className="px-4 py-2 flex items-center justify-center gap-2"
        style={{ backgroundColor: bgColor }}
      >
        {isStart && (
          <svg className="w-4 h-4 text-primary" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
        )}
        {isEnd && (
          <svg className="w-4 h-4 text-primary" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
        )}
        {isError && (
          <svg className="w-4 h-4 text-primary" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
        )}
        <span className="text-xs font-bold text-primary tracking-wider">{label}</span>
      </div>

      {/* State Section */}
      <div className="px-3 py-3">
        {isStart ? (
          <div className="flex items-center justify-end">
            <div className="mr-3 flex-1 text-right">
              <div className="text-[9px] text-muted uppercase tracking-wider">Initial State</div>
              <div className="text-xs text-green-400 font-medium">
                {data.initialState || 'idle'}
              </div>
            </div>
            <Handle
              type="source"
              position={Position.Right}
              id="state-out"
              className="!w-4 !h-4 !bg-green-500 !border-2 !border-green-300 !rounded-full hover:!w-5 hover:!h-5 hover:!bg-green-400 transition-all cursor-crosshair"
              style={{ position: 'absolute', right: -8, zIndex: 50, pointerEvents: 'all' }}
            />
          </div>
        ) : (
          <div className="flex items-center">
            <Handle
              type="target"
              position={Position.Left}
              id="state-in"
              className={`!w-4 !h-4 !border-2 !rounded-full hover:!w-5 hover:!h-5 transition-all cursor-crosshair ${
                isError
                  ? '!bg-red-500 !border-red-300 hover:!bg-red-400'
                  : '!bg-blue-500 !border-blue-300 hover:!bg-blue-400'
              }`}
              style={{ position: 'absolute', left: -8, zIndex: 50, pointerEvents: 'all' }}
            />
            <div className="ml-3 flex-1">
              <div className="text-[9px] text-muted uppercase tracking-wider">Final State</div>
              <div className={`text-xs font-medium ${isError ? 'text-red-400' : 'text-blue-400'}`}>
                {data.finalState || (isError ? 'error' : 'idle')}
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  )
})

StateEventNode.displayName = 'StateEventNode'

export default StateEventNode
