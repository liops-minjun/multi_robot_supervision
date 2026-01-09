import { memo } from 'react'
import { Handle, Position, NodeProps } from 'reactflow'

interface StartNodeData {
  label: string
}

const StartNode = memo(({ data, selected }: NodeProps<StartNodeData>) => {
  return (
    <div
      className={`
        relative group cursor-pointer
        ${selected ? 'scale-105' : ''}
        transition-transform duration-200
      `}
    >
      {/* Glow effect */}
      <div className="absolute -inset-1 bg-gradient-to-r from-emerald-500 to-cyan-500 rounded-lg blur opacity-30 group-hover:opacity-50 transition-opacity" />

      {/* Main node */}
      <div
        className={`
          relative px-6 py-3
          bg-gradient-to-br from-emerald-600 via-emerald-700 to-cyan-700
          rounded-lg
          border border-emerald-400/30
          shadow-xl shadow-emerald-500/20
          ${selected ? 'ring-2 ring-cyan-400 ring-offset-2 ring-offset-slate-900' : ''}
        `}
      >
        <div className="flex items-center gap-3">
          {/* Start icon - play triangle */}
          <div className="w-8 h-8 bg-white/10 rounded flex items-center justify-center backdrop-blur-sm">
            <svg className="w-4 h-4 text-white ml-0.5" fill="currentColor" viewBox="0 0 24 24">
              <path d="M8 5v14l11-7z" />
            </svg>
          </div>
          <div>
            <span className="text-white font-bold tracking-wide">{data.label}</span>
            <p className="text-emerald-200/60 text-[10px] font-medium uppercase tracking-wider">Entry Point</p>
          </div>
        </div>
      </div>

      {/* Output Handle */}
      <Handle
        type="source"
        position={Position.Bottom}
        className="!w-3 !h-3 !bg-emerald-400 !border-2 !border-white shadow-lg"
      />
    </div>
  )
})

StartNode.displayName = 'StartNode'

export default StartNode
