import { memo, useCallback } from 'react'
import { Handle, Position, NodeProps, useReactFlow } from 'reactflow'
import { X } from 'lucide-react'

interface StateEventNodeData {
  label: string
  subtype: 'Start' | 'End' | 'Error' | 'Warning'
  color: string
  initialState?: string
  finalState?: string
  debugMessage?: string
  isEditing?: boolean
}

const StateEventNode = memo(({ id, data, selected }: NodeProps<StateEventNodeData>) => {
  const { setNodes, setEdges } = useReactFlow()
  const isStart = data.subtype === 'Start'
  const isEnd = data.subtype === 'End'
  const isError = data.subtype === 'Error'
  const isWarning = data.subtype === 'Warning'
  const isEditing = data.isEditing ?? true

  const bgColor = isStart ? '#22c55e' : isError ? '#ef4444' : isWarning ? '#f59e0b' : '#3b82f6'
  const label = isStart ? 'START' : isError ? 'ERROR' : isWarning ? 'WARNING' : 'END'

  const updateDebugMessage = useCallback((value: string) => {
    setNodes((nds) => nds.map((node) => (
      node.id === id
        ? { ...node, data: { ...node.data, debugMessage: value } }
        : node
    )))
  }, [id, setNodes])

  const removeNode = useCallback(() => {
    if (!isEditing) return
    setNodes((nds) => nds.filter((node) => node.id !== id))
    setEdges((eds) => eds.filter((edge) => edge.source !== id && edge.target !== id))
  }, [id, isEditing, setEdges, setNodes])

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
        {isWarning && (
          <svg className="w-4 h-4 text-primary" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10.29 3.86l-8.13 14.1A1.5 1.5 0 003.46 20h17.08a1.5 1.5 0 001.3-2.24l-8.13-14.1a1.5 1.5 0 00-2.6 0z" />
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v4m0 4h.01" />
          </svg>
        )}
        <span className="text-xs font-bold text-primary tracking-wider">{label}</span>
        {isEditing && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation()
              removeNode()
            }}
            className="ml-1 rounded p-0.5 text-primary/90 transition hover:bg-black/20 hover:text-primary"
            title="노드 삭제"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        )}
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
                  : isWarning
                    ? '!bg-yellow-500 !border-yellow-300 hover:!bg-yellow-400'
                    : '!bg-blue-500 !border-blue-300 hover:!bg-blue-400'
              }`}
              style={{ position: 'absolute', left: -8, zIndex: 50, pointerEvents: 'all' }}
            />
            <div className="ml-3 flex-1">
              <div className="text-[9px] text-muted uppercase tracking-wider">Final State</div>
              <div className={`text-xs font-medium ${isError ? 'text-red-400' : isWarning ? 'text-yellow-300' : 'text-blue-400'}`}>
                {data.finalState || (isError ? 'error' : isWarning ? 'warning' : 'idle')}
              </div>
            </div>
          </div>
        )}

        {!isStart && (
          <div className="mt-3 border-t border-border/60 pt-2">
            <div className="mb-1 text-[9px] text-muted uppercase tracking-wider">
              Debug Message
            </div>
            {isEditing ? (
              <textarea
                value={data.debugMessage || ''}
                onChange={(e) => updateDebugMessage(e.target.value)}
                onClick={(e) => e.stopPropagation()}
                placeholder={isError ? '에러 디버깅 메시지' : isWarning ? '경고 디버깅 메시지' : '종료 메시지'}
                className="h-16 w-full resize-none rounded border border-primary/60 bg-base px-2 py-1 text-xs text-primary outline-none focus:border-white"
              />
            ) : (
              <div className="min-h-[2.5rem] rounded border border-primary/40 bg-base px-2 py-1 text-[11px] text-secondary">
                {(data.debugMessage || '').trim() || '-'}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
})

StateEventNode.displayName = 'StateEventNode'

export default StateEventNode
