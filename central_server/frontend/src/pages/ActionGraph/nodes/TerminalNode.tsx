import { memo } from 'react'
import { Handle, Position, NodeProps } from 'reactflow'

interface TerminalNodeData {
  label: string
  terminalType?: 'success' | 'failure'
}

const TerminalNode = memo(({ data, selected }: NodeProps<TerminalNodeData>) => {
  const isSuccess = data.terminalType === 'success'

  return (
    <div
      className={`
        relative group cursor-pointer
        ${selected ? 'scale-105' : ''}
        transition-transform duration-200
      `}
    >
      {/* Glow effect */}
      <div className={`
        absolute -inset-1 rounded-lg blur opacity-30 group-hover:opacity-50 transition-opacity
        ${isSuccess ? 'bg-gradient-to-r from-emerald-500 to-green-500' : 'bg-gradient-to-r from-red-500 to-rose-500'}
      `} />

      {/* Main node */}
      <div
        className={`
          relative px-6 py-3
          ${isSuccess
            ? 'bg-gradient-to-br from-emerald-600 via-green-700 to-emerald-800 border-emerald-400/30 shadow-emerald-500/20'
            : 'bg-gradient-to-br from-red-600 via-rose-700 to-red-800 border-red-400/30 shadow-red-500/20'
          }
          rounded-lg border shadow-xl
          ${selected ? 'ring-2 ring-cyan-400 ring-offset-2 ring-offset-slate-900' : ''}
        `}
      >
        {/* Input Handle */}
        <Handle
          type="target"
          position={Position.Top}
          className={`!w-3 !h-3 !border-2 !border-white shadow-lg ${
            isSuccess ? '!bg-emerald-400' : '!bg-red-400'
          }`}
        />

        <div className="flex items-center gap-3">
          {/* Terminal icon */}
          <div className={`w-8 h-8 rounded flex items-center justify-center backdrop-blur-sm ${
            isSuccess ? 'bg-white/10' : 'bg-white/10'
          }`}>
            {isSuccess ? (
              <svg className="w-5 h-5 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
              </svg>
            ) : (
              <svg className="w-5 h-5 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M6 18L18 6M6 6l12 12" />
              </svg>
            )}
          </div>
          <div>
            <span className="text-white font-bold tracking-wide">{data.label}</span>
            <p className={`text-[10px] font-medium uppercase tracking-wider ${
              isSuccess ? 'text-emerald-200/60' : 'text-red-200/60'
            }`}>
              {isSuccess ? 'Behavior Tree Complete' : 'Behavior Tree Failed'}
            </p>
          </div>
        </div>
      </div>
    </div>
  )
})

TerminalNode.displayName = 'TerminalNode'

export default TerminalNode
