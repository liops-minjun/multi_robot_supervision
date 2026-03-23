import { memo, useCallback } from 'react'
import { Handle, Position, NodeProps, useReactFlow } from 'reactflow'
import { RotateCcw, X } from 'lucide-react'

interface StateRetryNodeData {
  label: string
  subtype?: string
  color?: string
  maxRetries?: number
  backoffMs?: number
  isEditing?: boolean
}

const PORT_TOP = {
  out: '68%',
  input: '68%',
  failed: '84%',
} as const

const StateRetryNode = memo(({ id, data, selected }: NodeProps<StateRetryNodeData>) => {
  const { setNodes, setEdges } = useReactFlow()
  const isEditing = data.isEditing ?? true
  const color = data.color || '#f59e0b'
  const maxRetries = Number.isFinite(data.maxRetries) ? Number(data.maxRetries) : 3
  const backoffMs = Number.isFinite(data.backoffMs) ? Number(data.backoffMs) : 0

  const updateData = useCallback((field: keyof StateRetryNodeData, value: unknown) => {
    setNodes((nds) => nds.map((node) =>
      node.id === id ? { ...node, data: { ...node.data, [field]: value } } : node
    ))
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
        relative w-[180px] min-h-[160px] rounded-lg overflow-visible
        bg-surface border-2 shadow-lg
        ${selected ? 'border-white/60 shadow-xl' : 'border-primary'}
        transition-all duration-150
      `}
      style={{ borderColor: selected ? 'white' : color }}
    >
      <div
        className="px-3 py-2 border-b border-primary/60 flex items-center justify-between gap-2"
        style={{ backgroundColor: `${color}25` }}
      >
        <div className="flex items-center gap-2">
          <RotateCcw className="w-3.5 h-3.5" style={{ color }} />
          <span className="text-xs font-semibold text-primary">{data.label || 'Retry Block'}</span>
        </div>
        {isEditing && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation()
              removeNode()
            }}
            className="rounded p-0.5 text-muted transition hover:bg-red-500/20 hover:text-red-400"
            title="노드 삭제"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        )}
      </div>

      <div className="px-2 py-2">
        <div className="grid grid-cols-2 gap-2">
          <label className="block text-[10px] text-muted uppercase tracking-wider">
            max_retries
            <input
              type="number"
              min={0}
              step={1}
              value={maxRetries}
              disabled={!isEditing}
              onChange={(e) => {
                e.stopPropagation()
                const next = Math.max(0, Number(e.target.value || 0))
                updateData('maxRetries', next)
              }}
              onClick={(e) => e.stopPropagation()}
              className="mt-1 w-full rounded border border-primary/60 bg-base px-1.5 py-1 text-[11px] text-primary outline-none focus:border-white disabled:opacity-70"
            />
          </label>

          <label className="block text-[10px] text-muted uppercase tracking-wider">
            backoff_ms
            <input
              type="number"
              min={0}
              step={100}
              value={backoffMs}
              disabled={!isEditing}
              onChange={(e) => {
                e.stopPropagation()
                const next = Math.max(0, Number(e.target.value || 0))
                updateData('backoffMs', next)
              }}
              onClick={(e) => e.stopPropagation()}
              className="mt-1 w-full rounded border border-primary/60 bg-base px-1.5 py-1 text-[11px] text-primary outline-none focus:border-white disabled:opacity-70"
            />
          </label>
        </div>
      </div>

      <div className="pointer-events-none absolute left-2 -translate-y-1/2 rounded bg-base/90 px-1.5 py-0.5 text-[10px] font-medium text-amber-300" style={{ top: PORT_TOP.out }}>
        out
      </div>
      <div className="pointer-events-none absolute right-2 -translate-y-1/2 rounded bg-base/90 px-1.5 py-0.5 text-[10px] font-medium text-cyan-300" style={{ top: PORT_TOP.input }}>
        in
      </div>
      <div className="pointer-events-none absolute right-2 -translate-y-1/2 rounded bg-base/90 px-1.5 py-0.5 text-[10px] font-medium text-red-300" style={{ top: PORT_TOP.failed }}>
        failed
      </div>

      <Handle
        type="source"
        position={Position.Left}
        id="retry-out"
        className="!w-4 !h-4 !border-2 !rounded-full hover:!w-5 hover:!h-5 transition-all cursor-crosshair"
        style={{ backgroundColor: color, borderColor: `${color}99`, left: -8, top: PORT_TOP.out, pointerEvents: 'all' }}
      />

      <Handle
        type="target"
        position={Position.Right}
        id="retry-in"
        className="!w-4 !h-4 !border-2 !rounded-full hover:!w-5 hover:!h-5 transition-all cursor-crosshair"
        style={{ backgroundColor: '#22d3ee', borderColor: '#67e8f9', right: -8, top: PORT_TOP.input, pointerEvents: 'all' }}
      />

      <Handle
        type="source"
        position={Position.Right}
        id="retry-failed"
        className="!w-4 !h-4 !border-2 !rounded-full hover:!w-5 hover:!h-5 transition-all cursor-crosshair"
        style={{ backgroundColor: '#ef4444', borderColor: '#fca5a5', right: -8, top: PORT_TOP.failed, pointerEvents: 'all' }}
      />
    </div>
  )
})

StateRetryNode.displayName = 'StateRetryNode'

export default StateRetryNode
