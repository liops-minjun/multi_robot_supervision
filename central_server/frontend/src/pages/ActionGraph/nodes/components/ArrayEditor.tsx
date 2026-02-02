import { memo, useCallback } from 'react'
import { Plus, Trash2, Crosshair } from 'lucide-react'
import type { CapturedTelemetry } from '../../../../contexts/TelemetryContext'
import { TELEMETRY_TO_GOAL_MAPPING } from '../../../../utils/telemetryMapping'

// Get compatible telemetry path for a field type
const getTelemetryPathForType = (fieldType: string): string | null => {
  for (const [telemetryPath, compatibleTypes] of Object.entries(TELEMETRY_TO_GOAL_MAPPING)) {
    if (compatibleTypes.some(t => t.toLowerCase() === fieldType.toLowerCase())) {
      return telemetryPath
    }
  }
  return null
}

interface ArrayEditorProps {
  value: number[] | string[] | null | undefined
  onChange: (value: number[] | string[]) => void
  fieldType: string
  capturedTelemetry?: CapturedTelemetry | null
  isNumeric: boolean
}

const ArrayEditor = memo(({ value, onChange, fieldType, capturedTelemetry, isNumeric }: ArrayEditorProps) => {
  const arrayValue: (number | string)[] = Array.isArray(value) ? value : []

  // Get compatible telemetry path and data
  const telemetryPath = getTelemetryPathForType(fieldType)
  const telemetryArray = capturedTelemetry?.type === telemetryPath ? capturedTelemetry.value as number[] | string[] : null
  const telemetrySize = Array.isArray(telemetryArray) ? telemetryArray.length : 0

  const addElement = useCallback(() => {
    if (isNumeric) {
      onChange([...arrayValue, 0] as number[])
    } else {
      onChange([...arrayValue, ''] as string[])
    }
  }, [arrayValue, onChange, isNumeric])

  const removeElement = useCallback((index: number) => {
    const newArray = arrayValue.filter((_, i) => i !== index)
    if (isNumeric) {
      onChange(newArray as number[])
    } else {
      onChange(newArray as string[])
    }
  }, [arrayValue, onChange, isNumeric])

  const updateElement = useCallback((index: number, newValue: number | string) => {
    const newArray = [...arrayValue]
    newArray[index] = newValue
    if (isNumeric) {
      onChange(newArray as number[])
    } else {
      onChange(newArray as string[])
    }
  }, [arrayValue, onChange, isNumeric])

  const copyFromTelemetry = useCallback(() => {
    if (telemetryArray && Array.isArray(telemetryArray)) {
      onChange(telemetryArray)
    }
  }, [telemetryArray, onChange])

  const canCopyFromTelemetry = telemetryArray && telemetryArray.length > 0
  const sizesMatch = telemetrySize > 0 && arrayValue.length === telemetrySize

  return (
    <div className="space-y-1.5">
      {/* Header with size info and add button */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span className="text-[9px] text-gray-500">요소: {arrayValue.length}개</span>
          {telemetrySize > 0 && (
            <span className={`text-[9px] px-1.5 py-0.5 rounded ${
              sizesMatch ? 'bg-green-500/20 text-green-400' : 'bg-yellow-500/20 text-yellow-400'
            }`}>
              Telemetry: {telemetrySize}개
            </span>
          )}
        </div>
        <button
          onClick={(e) => { e.stopPropagation(); addElement() }}
          className="px-2 py-0.5 bg-blue-500/20 text-blue-400 rounded hover:bg-blue-500/30 flex items-center gap-1 text-[9px]"
        >
          <Plus size={10} /> 추가
        </button>
      </div>

      {/* Telemetry copy button */}
      {canCopyFromTelemetry && (
        <button
          onClick={(e) => { e.stopPropagation(); copyFromTelemetry() }}
          className={`w-full py-1.5 rounded text-[10px] flex items-center justify-center gap-2 transition-colors ${
            sizesMatch
              ? 'bg-green-500/20 text-green-400 hover:bg-green-500/30 border border-green-500/30'
              : 'bg-purple-500/20 text-purple-400 hover:bg-purple-500/30 border border-purple-500/30 animate-pulse'
          }`}
        >
          <Crosshair size={12} />
          {sizesMatch
            ? `현재 Telemetry 값 복사 (${telemetrySize}개)`
            : `Telemetry에서 가져오기 (${telemetrySize}개로 조정)`
          }
        </button>
      )}

      {/* Array elements */}
      <div className="max-h-40 overflow-y-auto space-y-1 pr-1">
        {arrayValue.length === 0 ? (
          <div className="text-[9px] text-gray-500 italic py-3 text-center bg-gray-800/30 rounded">
            빈 배열 - "추가" 버튼으로 요소 추가
          </div>
        ) : (
          arrayValue.map((element, idx) => (
            <div key={idx} className="flex items-center gap-1.5 group">
              <span className="text-[8px] text-gray-600 w-6 text-right font-mono">[{idx}]</span>
              {isNumeric ? (
                <input
                  type="number"
                  value={element as number}
                  onChange={(e) => {
                    e.stopPropagation()
                    updateElement(idx, parseFloat(e.target.value) || 0)
                  }}
                  onClick={(e) => e.stopPropagation()}
                  className="flex-1 px-2 py-1 bg-surface border border-gray-700 rounded text-[10px] text-white focus:outline-none focus:border-amber-500 font-mono"
                  step="0.0001"
                />
              ) : (
                <input
                  type="text"
                  value={element as string}
                  onChange={(e) => {
                    e.stopPropagation()
                    updateElement(idx, e.target.value)
                  }}
                  onClick={(e) => e.stopPropagation()}
                  className="flex-1 px-2 py-1 bg-surface border border-gray-700 rounded text-[10px] text-white focus:outline-none focus:border-amber-500"
                />
              )}
              <button
                onClick={(e) => { e.stopPropagation(); removeElement(idx) }}
                className="opacity-0 group-hover:opacity-100 p-1 text-red-400 hover:text-red-300 hover:bg-red-500/20 rounded transition-all"
              >
                <Trash2 size={12} />
              </button>
            </div>
          ))
        )}
      </div>

      {/* Telemetry preview */}
      {telemetryArray && telemetryArray.length > 0 && (
        <div className="p-2 bg-gray-800/50 rounded border border-gray-700/50">
          <div className="text-[8px] text-gray-500 mb-1">Telemetry 미리보기:</div>
          <div className="text-[9px] text-gray-400 font-mono truncate">
            [{telemetryArray.slice(0, 4).map(v =>
              typeof v === 'number' ? v.toFixed(3) : `"${v}"`
            ).join(', ')}{telemetryArray.length > 4 ? `, ... +${telemetryArray.length - 4}` : ''}]
          </div>
        </div>
      )}
    </div>
  )
})

ArrayEditor.displayName = 'ArrayEditor'

export default ArrayEditor
