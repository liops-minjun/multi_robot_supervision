import { memo } from 'react'
import { Handle, Position, NodeProps } from 'reactflow'
import type { GraphStep } from '../../../types'

interface ActionNodeData {
  label: string
  step: GraphStep
  status?: 'idle' | 'running' | 'success' | 'failed'
}

const ActionNode = memo(({ data, selected }: NodeProps<ActionNodeData>) => {
  const status = data.status || 'idle'
  const actionType = data.step?.action?.type || 'Unknown'
  const server = data.step?.action?.server || ''

  // Get icon based on action type
  const getIcon = () => {
    if (actionType.includes('Navigate')) {
      return (
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M17.657 16.657L13.414 20.9a1.998 1.998 0 01-2.827 0l-4.244-4.243a8 8 0 1111.314 0z" />
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 11a3 3 0 11-6 0 3 3 0 016 0z" />
        </svg>
      )
    }
    if (actionType.includes('Joint') || actionType.includes('Arm') || actionType.includes('Trajectory')) {
      return (
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
        </svg>
      )
    }
    if (actionType.includes('Gripper')) {
      return (
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M7 11.5V14m0-2.5v-6a1.5 1.5 0 113 0m-3 6a1.5 1.5 0 00-3 0v2a7.5 7.5 0 0015 0v-5a1.5 1.5 0 00-3 0m-6-3V11m0-5.5v-1a1.5 1.5 0 013 0v1m0 0V11m0-5.5a1.5 1.5 0 013 0v3m0 0V11" />
        </svg>
      )
    }
    return (
      <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 10V3L4 14h7v7l9-11h-7z" />
      </svg>
    )
  }

  const statusConfig = {
    idle: {
      border: 'border-slate-500/30',
      bg: 'from-slate-800 via-slate-850 to-slate-900',
      glow: 'from-blue-500/20 to-cyan-500/20',
      iconBg: 'bg-blue-500/20',
      iconColor: 'text-blue-400',
      indicator: null,
    },
    running: {
      border: 'border-blue-500/50',
      bg: 'from-slate-800 via-blue-900/20 to-slate-900',
      glow: 'from-blue-500/40 to-cyan-500/40',
      iconBg: 'bg-blue-500/30',
      iconColor: 'text-blue-300',
      indicator: 'bg-blue-500',
    },
    success: {
      border: 'border-emerald-500/50',
      bg: 'from-slate-800 via-emerald-900/20 to-slate-900',
      glow: 'from-emerald-500/30 to-green-500/30',
      iconBg: 'bg-emerald-500/20',
      iconColor: 'text-emerald-400',
      indicator: 'bg-emerald-500',
    },
    failed: {
      border: 'border-red-500/50',
      bg: 'from-slate-800 via-red-900/20 to-slate-900',
      glow: 'from-red-500/30 to-rose-500/30',
      iconBg: 'bg-red-500/20',
      iconColor: 'text-red-400',
      indicator: 'bg-red-500',
    },
  }

  const config = statusConfig[status]

  return (
    <div
      className={`
        relative group cursor-pointer
        ${selected ? 'scale-[1.02]' : ''}
        transition-transform duration-200
      `}
    >
      {/* Glow effect */}
      <div className={`
        absolute -inset-1 bg-gradient-to-r ${config.glow} rounded-xl blur opacity-0
        group-hover:opacity-100 transition-opacity duration-300
      `} />

      {/* Running animation */}
      {status === 'running' && (
        <div className="absolute -inset-0.5 bg-gradient-to-r from-blue-500 via-cyan-500 to-blue-500 rounded-xl opacity-50 blur-sm animate-pulse" />
      )}

      {/* Main node */}
      <div
        className={`
          relative min-w-[240px]
          bg-gradient-to-br ${config.bg}
          rounded-xl border ${config.border}
          shadow-xl shadow-black/30
          ${selected ? 'ring-2 ring-cyan-400 ring-offset-2 ring-offset-slate-900' : ''}
        `}
      >
        {/* Input Handle */}
        <Handle
          type="target"
          position={Position.Top}
          className="!w-3 !h-3 !bg-cyan-400 !border-2 !border-white shadow-lg"
        />

        {/* Header */}
        <div className="px-4 py-3 border-b border-slate-700/50 flex items-center gap-3">
          <div className={`w-8 h-8 ${config.iconBg} rounded-lg flex items-center justify-center backdrop-blur-sm`}>
            <span className={config.iconColor}>{getIcon()}</span>
          </div>
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2">
              <span className="text-white font-semibold text-sm truncate">{data.label}</span>
              {config.indicator && (
                <span className="relative flex h-2 w-2">
                  {status === 'running' && (
                    <span className={`animate-ping absolute inline-flex h-full w-full rounded-full ${config.indicator} opacity-75`}></span>
                  )}
                  <span className={`relative inline-flex rounded-full h-2 w-2 ${config.indicator}`}></span>
                </span>
              )}
            </div>
            {/* Show server as primary, action type as secondary */}
            <p className="text-slate-400 text-[10px] font-mono truncate" title={server}>{server.replace(/^\//, '') || 'No server'}</p>
            <p className="text-slate-600 text-[9px] truncate">{actionType.split('/').pop()}</p>
          </div>
        </div>

        {/* Body */}
        <div className="px-4 py-3 space-y-2">
          {server && (
            <div className="flex items-center gap-2">
              <svg className="w-3 h-3 text-slate-500 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2m-2-4h.01M17 16h.01" />
              </svg>
              <span className="text-[10px] font-mono text-slate-400 truncate">{server}</span>
            </div>
          )}

          {/* Status badge */}
          {status !== 'idle' && (
            <div className="flex items-center gap-2">
              <span className={`
                px-2 py-0.5 rounded text-[10px] font-semibold uppercase tracking-wider
                ${status === 'running' ? 'bg-blue-500/20 text-blue-300' : ''}
                ${status === 'success' ? 'bg-emerald-500/20 text-emerald-300' : ''}
                ${status === 'failed' ? 'bg-red-500/20 text-red-300' : ''}
              `}>
                {status}
              </span>
            </div>
          )}
        </div>

        {/* Output Handles */}
        <div className="absolute -bottom-1 left-0 right-0 flex justify-center gap-20 text-[9px]">
          <div className="flex flex-col items-center">
            <Handle
              type="source"
              position={Position.Bottom}
              id="success"
              className="!relative !transform-none !w-3 !h-3 !bg-emerald-500 !border-2 !border-white shadow-lg"
            />
            <span className="text-emerald-400/70 mt-1 font-medium">SUCCESS</span>
          </div>
          <div className="flex flex-col items-center">
            <Handle
              type="source"
              position={Position.Bottom}
              id="failure"
              className="!relative !transform-none !w-3 !h-3 !bg-red-500 !border-2 !border-white shadow-lg"
            />
            <span className="text-red-400/70 mt-1 font-medium">FAILURE</span>
          </div>
        </div>

        {/* Fallback Handle on Right */}
        <div className="absolute -right-1 top-1/2 -translate-y-1/2 flex items-center">
          <Handle
            type="source"
            position={Position.Right}
            id="fallback"
            className="!relative !transform-none !w-3 !h-3 !bg-amber-500 !border-2 !border-white shadow-lg"
          />
        </div>

        {/* Spacer for bottom handles */}
        <div className="h-5" />
      </div>
    </div>
  )
})

ActionNode.displayName = 'ActionNode'

export default ActionNode
