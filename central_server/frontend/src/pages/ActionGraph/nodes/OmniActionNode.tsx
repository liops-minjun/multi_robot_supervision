import { memo } from 'react'
import { Handle, Position, NodeProps } from 'reactflow'

interface OmniActionNodeData {
  label: string
  subtype: string
  color: string
  params?: Record<string, any>
}

const OmniActionNode = memo(({ data, selected }: NodeProps<OmniActionNodeData>) => {
  const color = data.color || '#3b82f6'

  return (
    <div
      className={`
        min-w-[180px] rounded-md overflow-hidden
        bg-[#1e1e2e] border
        shadow-lg
        ${selected ? 'border-white/50 shadow-xl' : 'border-[#2a2a4a]'}
        transition-all duration-150
      `}
      style={{
        borderTopColor: selected ? color : undefined,
        borderTopWidth: selected ? '2px' : undefined,
      }}
    >
      {/* Header */}
      <div
        className="px-3 py-2 flex items-center gap-2"
        style={{ backgroundColor: `${color}20` }}
      >
        <div
          className="w-2.5 h-2.5 rounded-sm"
          style={{ backgroundColor: color }}
        />
        <span className="text-xs font-medium text-white truncate flex-1">
          {data.label}
        </span>
      </div>

      {/* Body with ports */}
      <div className="relative px-3 py-3">
        {/* Input Ports (Left side) */}
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <Handle
              type="target"
              position={Position.Left}
              id="exec-in"
              className="!w-2.5 !h-2.5 !bg-white !border-2 !rounded-sm !-left-1"
              style={{ borderColor: '#22c55e' }}
            />
            <span className="text-[10px] text-gray-500">Exec In</span>
          </div>
          {data.subtype === 'Branch' && (
            <div className="flex items-center gap-2">
              <Handle
                type="target"
                position={Position.Left}
                id="condition"
                className="!w-2.5 !h-2.5 !bg-white !border-2 !rounded-sm !-left-1"
                style={{ borderColor: '#f59e0b', top: '70%' }}
              />
              <span className="text-[10px] text-gray-500">Condition</span>
            </div>
          )}
        </div>

        {/* Subtype label */}
        <div className="mt-2 mb-2">
          <span className="text-[10px] text-gray-400 font-mono">{data.subtype}</span>
        </div>

        {/* Output Ports (Right side) */}
        <div className="space-y-2">
          <div className="flex items-center justify-end gap-2">
            <span className="text-[10px] text-gray-500">Exec Out</span>
            <Handle
              type="source"
              position={Position.Right}
              id="exec-out"
              className="!w-2.5 !h-2.5 !bg-white !border-2 !rounded-sm !-right-1"
              style={{ borderColor: '#22c55e' }}
            />
          </div>
          <div className="flex items-center justify-end gap-2">
            <span className="text-[10px] text-gray-500">On Error</span>
            <Handle
              type="source"
              position={Position.Right}
              id="error-out"
              className="!w-2.5 !h-2.5 !bg-white !border-2 !rounded-sm !-right-1"
              style={{ borderColor: '#ef4444', top: '70%' }}
            />
          </div>
        </div>
      </div>

      {/* Footer with status indicator */}
      <div className="px-3 py-1.5 bg-[#16162a] border-t border-[#2a2a4a] flex items-center justify-between">
        <span className="text-[9px] text-gray-600 uppercase tracking-wider">Action</span>
        <div className="w-1.5 h-1.5 rounded-full bg-green-500" />
      </div>
    </div>
  )
})

OmniActionNode.displayName = 'OmniActionNode'

export default OmniActionNode
