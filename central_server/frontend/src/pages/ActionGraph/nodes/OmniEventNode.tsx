import { memo } from 'react'
import { Handle, Position, NodeProps } from 'reactflow'

interface OmniEventNodeData {
  label: string
  subtype: string
  color: string
}

const OmniEventNode = memo(({ data, selected }: NodeProps<OmniEventNodeData>) => {
  const color = data.color || '#22c55e'
  const isInput = data.subtype === 'OnStart'
  const isOutput = data.subtype === 'OnComplete' || data.subtype === 'OnError'

  return (
    <div
      className={`
        min-w-[140px] rounded-md overflow-hidden
        bg-surface border
        shadow-lg
        ${selected ? 'border-white/50 shadow-xl' : 'border-primary'}
        transition-all duration-150
      `}
      style={{
        borderLeftColor: color,
        borderLeftWidth: '3px',
      }}
    >
      {/* Header */}
      <div
        className="px-3 py-2 flex items-center gap-2"
        style={{ backgroundColor: `${color}15` }}
      >
        {/* Event icon */}
        <svg
          className="w-3.5 h-3.5"
          style={{ color }}
          fill="currentColor"
          viewBox="0 0 24 24"
        >
          {isInput ? (
            <path d="M13 10V3L4 14h7v7l9-11h-7z" />
          ) : isOutput ? (
            <path d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
          ) : (
            <path d="M8.228 9c.549-1.165 2.03-2 3.772-2 2.21 0 4 1.343 4 3 0 1.4-1.278 2.575-3.006 2.907-.542.104-.994.54-.994 1.093m0 3h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          )}
        </svg>
        <span className="text-xs font-medium text-primary truncate">
          {data.label}
        </span>
      </div>

      {/* Body */}
      <div className="relative px-3 py-2">
        <span className="text-[10px] text-secondary font-mono">{data.subtype}</span>

        {/* Output port for events */}
        {(isInput || data.subtype === 'Branch') && (
          <div className="mt-2 flex items-center justify-end gap-2">
            <span className="text-[10px] text-muted">Trigger</span>
            <Handle
              type="source"
              position={Position.Right}
              id="exec-out"
              className="!w-2.5 !h-2.5 !bg-white !border-2 !rounded-sm !-right-1"
              style={{ borderColor: color }}
            />
          </div>
        )}

        {/* Input port for terminal events */}
        {isOutput && (
          <div className="mt-2 flex items-center gap-2">
            <Handle
              type="target"
              position={Position.Left}
              id="exec-in"
              className="!w-2.5 !h-2.5 !bg-white !border-2 !rounded-sm !-left-1"
              style={{ borderColor: color }}
            />
            <span className="text-[10px] text-muted">Complete</span>
          </div>
        )}

        {/* Branch has both true/false outputs */}
        {data.subtype === 'Branch' && (
          <>
            <div className="mt-2 flex items-center justify-end gap-2">
              <span className="text-[10px] text-green-400">True</span>
              <Handle
                type="source"
                position={Position.Right}
                id="true-out"
                className="!w-2.5 !h-2.5 !bg-white !border-2 !rounded-sm !-right-1"
                style={{ borderColor: '#22c55e', top: '60%' }}
              />
            </div>
            <div className="mt-1 flex items-center justify-end gap-2">
              <span className="text-[10px] text-red-400">False</span>
              <Handle
                type="source"
                position={Position.Right}
                id="false-out"
                className="!w-2.5 !h-2.5 !bg-white !border-2 !rounded-sm !-right-1"
                style={{ borderColor: '#ef4444', top: '80%' }}
              />
            </div>
            <div className="mt-2 flex items-center gap-2">
              <Handle
                type="target"
                position={Position.Left}
                id="exec-in"
                className="!w-2.5 !h-2.5 !bg-white !border-2 !rounded-sm !-left-1"
                style={{ borderColor: '#f97316' }}
              />
              <span className="text-[10px] text-muted">Exec In</span>
            </div>
          </>
        )}
      </div>

      {/* Footer */}
      <div className="px-3 py-1 bg-elevated border-t border-primary">
        <span className="text-[9px] text-muted uppercase tracking-wider">Event</span>
      </div>
    </div>
  )
})

OmniEventNode.displayName = 'OmniEventNode'

export default OmniEventNode
