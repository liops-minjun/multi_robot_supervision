import { memo } from 'react'
import { Handle, Position } from 'reactflow'
import { Plus, X } from 'lucide-react'
import type { EndStateConfig } from '../../../../types'
import { OUTCOME_OPTIONS } from '../constants'
import { getEndStateColor, inferOutcomeFromEndState } from '../utils'

interface EndStatesSectionProps {
  endStates: EndStateConfig[]
  states: string[]
  onAddEndState: () => void
  onRemoveEndState: (id: string) => void
  onUpdateEndState: (id: string, field: string, value: string) => void
}

const EndStatesSection = memo(({
  endStates,
  states,
  onAddEndState,
  onRemoveEndState,
  onUpdateEndState,
}: EndStatesSectionProps) => {
  return (
    <div className="px-3 py-2 relative">
      <div className="flex items-center justify-between mb-1.5">
        <div className="flex items-center gap-2">
          <div className="w-2 h-2 rounded-sm bg-gradient-to-r from-green-500 to-red-500" />
          <span className="text-[10px] text-gray-300 uppercase tracking-wider font-medium">종료 상태</span>
        </div>
        <button
          onClick={(e) => { e.stopPropagation(); onAddEndState() }}
          className="p-0.5 hover:bg-gray-500/20 rounded transition-colors"
        >
          <Plus className="w-2.5 h-2.5 text-gray-400" />
        </button>
      </div>

      <div className="space-y-1.5">
        {endStates.map((endState) => {
          const outcome = endState.outcome || inferOutcomeFromEndState(endState)
          const color = endState.color || getEndStateColor(endState)

          return (
            <div
              key={endState.id}
              className="flex items-center gap-1.5 pr-6 relative group"
            >
              {/* Color indicator */}
              <div
                className="w-2.5 h-2.5 rounded-full flex-shrink-0 border-2"
                style={{ backgroundColor: color, borderColor: `${color}99` }}
              />

              {/* Outcome selector */}
              <select
                value={outcome}
                onChange={(e) => {
                  e.stopPropagation()
                  onUpdateEndState(endState.id, 'outcome', e.target.value)
                }}
                onClick={(e) => e.stopPropagation()}
                className="w-16 px-1 py-0.5 bg-surface border border-gray-700 rounded text-[9px] text-gray-300 focus:outline-none cursor-pointer"
              >
                {OUTCOME_OPTIONS.map(option => (
                  <option key={option.value} value={option.value} className="bg-surface">
                    {option.label}
                  </option>
                ))}
              </select>

              {/* Label input */}
              <input
                type="text"
                value={endState.label || ''}
                onChange={(e) => {
                  e.stopPropagation()
                  onUpdateEndState(endState.id, 'label', e.target.value)
                }}
                onClick={(e) => e.stopPropagation()}
                placeholder="Label"
                className="w-14 px-1 py-0.5 bg-transparent border-b border-gray-700 text-[9px] text-gray-300 focus:outline-none focus:border-gray-500"
              />

              <span className="text-[9px] text-gray-600">→</span>

              {/* State selector */}
              <select
                value={endState.state}
                onChange={(e) => {
                  e.stopPropagation()
                  onUpdateEndState(endState.id, 'state', e.target.value)
                }}
                onClick={(e) => e.stopPropagation()}
                className="flex-1 px-1 py-0.5 bg-surface border border-gray-700 rounded text-[9px] focus:outline-none cursor-pointer"
                style={{ color }}
              >
                {states.map(s => <option key={s} value={s} className="bg-surface">{s}</option>)}
              </select>

              {/* Delete button */}
              {endStates.length > 1 && (
                <button
                  onClick={(e) => { e.stopPropagation(); onRemoveEndState(endState.id) }}
                  className="absolute right-0 text-gray-600 hover:text-red-400 transition-colors opacity-0 group-hover:opacity-100"
                >
                  <X className="w-3 h-3" />
                </button>
              )}
            </div>
          )
        })}
      </div>

      {/* Output Handles - positioned inline with content */}
      <div className="absolute right-0 top-0 bottom-0 w-4 flex flex-col justify-center gap-1 py-8">
        {/* Success Handle */}
        <div className="relative flex items-center justify-end h-4" title="성공: 드래그하여 연결">
          <span className="text-[7px] text-green-400 mr-1.5 opacity-60">S</span>
          <Handle
            type="source"
            position={Position.Right}
            id="success"
            className="!w-3.5 !h-3.5 !border-2 !rounded-full hover:!w-4 hover:!h-4 transition-all cursor-crosshair !pointer-events-auto !relative !transform-none !top-auto !right-auto"
            style={{ backgroundColor: '#22c55e', borderColor: '#22c55e99' }}
          />
        </div>

        {/* Failed Handle */}
        <div className="relative flex items-center justify-end h-4" title="실패: 드래그하여 연결">
          <span className="text-[7px] text-red-400 mr-1.5 opacity-60">F</span>
          <Handle
            type="source"
            position={Position.Right}
            id="failed"
            className="!w-3.5 !h-3.5 !border-2 !rounded-full hover:!w-4 hover:!h-4 transition-all cursor-crosshair !pointer-events-auto !relative !transform-none !top-auto !right-auto"
            style={{ backgroundColor: '#ef4444', borderColor: '#ef444499' }}
          />
        </div>

        {/* Aborted Handle */}
        <div className="relative flex items-center justify-end h-4" title="중단: 드래그하여 연결">
          <span className="text-[7px] text-red-400 mr-1.5 opacity-60">A</span>
          <Handle
            type="source"
            position={Position.Right}
            id="aborted"
            className="!w-3.5 !h-3.5 !border-2 !rounded-full hover:!w-4 hover:!h-4 transition-all cursor-crosshair !pointer-events-auto !relative !transform-none !top-auto !right-auto"
            style={{ backgroundColor: '#ef4444', borderColor: '#ef444499' }}
          />
        </div>

        {/* Cancelled Handle */}
        <div className="relative flex items-center justify-end h-4" title="취소됨: 드래그하여 연결">
          <span className="text-[7px] text-gray-400 mr-1.5 opacity-60">C</span>
          <Handle
            type="source"
            position={Position.Right}
            id="cancelled"
            className="!w-3.5 !h-3.5 !border-2 !rounded-full hover:!w-4 hover:!h-4 transition-all cursor-crosshair !pointer-events-auto !relative !transform-none !top-auto !right-auto"
            style={{ backgroundColor: '#6b7280', borderColor: '#6b728099' }}
          />
        </div>
      </div>
    </div>
  )
})

EndStatesSection.displayName = 'EndStatesSection'

export default EndStatesSection
