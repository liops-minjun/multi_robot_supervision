import { memo } from 'react'
import { Handle, Position, NodeProps } from 'reactflow'

interface ManualNodeData {
  label: string
  message?: string
  status?: 'waiting' | 'confirmed' | 'rejected'
}

const ManualNode = memo(({ data, selected }: NodeProps<ManualNodeData>) => {
  const status = data.status || 'waiting'

  return (
    <div
      className={`
        relative group cursor-pointer
        ${selected ? 'scale-[1.02]' : ''}
        transition-transform duration-200
      `}
    >
      {/* Animated border for waiting state */}
      {status === 'waiting' && (
        <div className="absolute -inset-0.5 bg-gradient-to-r from-amber-500 via-orange-500 to-amber-500 rounded-xl opacity-75 blur-sm animate-pulse" />
      )}

      {/* Glow effect */}
      <div className="absolute -inset-1 bg-gradient-to-r from-amber-500 to-orange-500 rounded-xl blur opacity-20 group-hover:opacity-40 transition-opacity" />

      {/* Main node */}
      <div
        className={`
          relative min-w-[220px]
          bg-gradient-to-br from-slate-800 via-slate-850 to-slate-900
          rounded-xl border
          shadow-xl
          ${status === 'waiting' ? 'border-amber-500/50' : 'border-slate-600/50'}
          ${selected ? 'ring-2 ring-cyan-400 ring-offset-2 ring-offset-slate-900' : ''}
        `}
      >
        {/* Input Handle */}
        <Handle
          type="target"
          position={Position.Top}
          className="!w-3 !h-3 !bg-amber-400 !border-2 !border-white shadow-lg"
        />

        {/* Header with warning pattern */}
        <div className="px-4 py-2.5 border-b border-amber-500/20 bg-gradient-to-r from-amber-500/10 to-orange-500/10 rounded-t-xl">
          <div className="flex items-center gap-2">
            {/* Warning/Manual icon */}
            <div className="w-7 h-7 bg-amber-500/20 rounded-lg flex items-center justify-center">
              <svg className="w-4 h-4 text-amber-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9" />
              </svg>
            </div>
            <div className="flex-1">
              <span className="text-white font-semibold text-sm">{data.label}</span>
            </div>
            {status === 'waiting' && (
              <span className="relative flex h-2 w-2">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-amber-400 opacity-75"></span>
                <span className="relative inline-flex rounded-full h-2 w-2 bg-amber-500"></span>
              </span>
            )}
          </div>
        </div>

        {/* Body */}
        <div className="px-4 py-3">
          <div className="flex items-center gap-2 text-xs">
            <span className={`
              px-2 py-1 rounded-md font-medium
              ${status === 'waiting' ? 'bg-amber-500/20 text-amber-300' : ''}
              ${status === 'confirmed' ? 'bg-emerald-500/20 text-emerald-300' : ''}
              ${status === 'rejected' ? 'bg-red-500/20 text-red-300' : ''}
            `}>
              {status === 'waiting' ? 'AWAITING APPROVAL' : status.toUpperCase()}
            </span>
          </div>
          {data.message && (
            <p className="text-[11px] text-slate-400 mt-2 leading-relaxed">{data.message}</p>
          )}
        </div>

        {/* Output Handles with labels */}
        <div className="absolute -bottom-1 left-0 right-0 flex justify-center gap-16 text-[9px]">
          <div className="flex flex-col items-center">
            <Handle
              type="source"
              position={Position.Bottom}
              id="confirm"
              className="!relative !transform-none !w-3 !h-3 !bg-emerald-500 !border-2 !border-white shadow-lg"
            />
            <span className="text-emerald-400 mt-1 font-medium">CONFIRM</span>
          </div>
          <div className="flex flex-col items-center">
            <Handle
              type="source"
              position={Position.Bottom}
              id="reject"
              className="!relative !transform-none !w-3 !h-3 !bg-red-500 !border-2 !border-white shadow-lg"
            />
            <span className="text-red-400 mt-1 font-medium">REJECT</span>
          </div>
        </div>

        {/* Spacer for handles */}
        <div className="h-6" />
      </div>
    </div>
  )
})

ManualNode.displayName = 'ManualNode'

export default ManualNode
