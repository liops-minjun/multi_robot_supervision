import { memo, useCallback, useMemo } from 'react'
import { Crosshair, Plus, Trash2, Copy, Check } from 'lucide-react'
import { type BaseEditorProps, formatNumber } from './types'

interface JointArrayEditorProps extends BaseEditorProps {
  jointNames?: string[]  // Optional joint names for display
}

const JointArrayEditor = memo(({
  value,
  onChange,
  robotTelemetry,
  jointNames,
}: JointArrayEditorProps) => {
  // Current array value
  const arrayValue = useMemo((): number[] => {
    if (Array.isArray(value)) {
      return value.map(v => typeof v === 'number' ? v : parseFloat(v) || 0)
    }
    return []
  }, [value])

  // Get live joint positions from telemetry
  const liveJoints = useMemo((): number[] | null => {
    if (!robotTelemetry?.joint_state?.position) return null
    return robotTelemetry.joint_state.position
  }, [robotTelemetry])

  // Get joint names from telemetry or props
  const displayNames = useMemo((): string[] => {
    if (jointNames && jointNames.length > 0) return jointNames
    if (robotTelemetry?.joint_state?.name) return robotTelemetry.joint_state.name
    return []
  }, [jointNames, robotTelemetry])

  // Check if element counts match for one-click copy
  const countsMatch = useMemo(() => {
    if (!liveJoints || arrayValue.length === 0) return false
    return arrayValue.length === liveJoints.length
  }, [arrayValue.length, liveJoints])

  // Capture current telemetry value
  const handleCapture = useCallback(() => {
    if (!liveJoints) return
    onChange([...liveJoints])
  }, [liveJoints, onChange])

  // Update single element
  const updateElement = useCallback((index: number, newValue: number) => {
    const newArray = [...arrayValue]
    newArray[index] = newValue
    onChange(newArray)
  }, [arrayValue, onChange])

  // Add element
  const addElement = useCallback(() => {
    onChange([...arrayValue, 0])
  }, [arrayValue, onChange])

  // Remove element
  const removeElement = useCallback((index: number) => {
    const newArray = arrayValue.filter((_, i) => i !== index)
    onChange(newArray)
  }, [arrayValue, onChange])

  // Initialize from telemetry if empty
  const initFromTelemetry = useCallback(() => {
    if (liveJoints) {
      onChange([...liveJoints])
    }
  }, [liveJoints, onChange])

  return (
    <div className="space-y-2">
      {/* Live Telemetry Bar - only when available and array has elements */}
      {liveJoints && arrayValue.length > 0 && (
        <div className={`p-2 rounded border ${countsMatch ? 'bg-green-500/10 border-green-500/50' : 'bg-purple-500/10 border-purple-500/30'}`}>
          <div className="flex items-center gap-2">
            <Crosshair className={`w-3 h-3 ${countsMatch ? 'text-green-400' : 'text-purple-400'} animate-pulse flex-shrink-0`} />
            <span className="text-[9px] text-purple-300 font-mono truncate flex-1">
              [{liveJoints.slice(0, 4).map(v => v.toFixed(2)).join(', ')}
              {liveJoints.length > 4 ? `, ... (${liveJoints.length})` : ''}]
            </span>
            {countsMatch ? (
              <button
                onClick={(e) => { e.stopPropagation(); handleCapture() }}
                className="px-3 py-1.5 bg-green-500/40 hover:bg-green-500/60 text-green-200 rounded text-[10px] font-bold whitespace-nowrap flex items-center gap-1.5 shadow-lg shadow-green-500/20 animate-pulse"
              >
                <Copy size={12} />
                현재 값 복사
              </button>
            ) : (
              <button
                onClick={(e) => { e.stopPropagation(); handleCapture() }}
                className="px-2 py-1 bg-purple-500/30 hover:bg-purple-500/50 text-purple-300 rounded text-[9px] font-medium whitespace-nowrap"
              >
                캡처
              </button>
            )}
          </div>
          {countsMatch && (
            <div className="mt-1.5 pt-1.5 border-t border-green-500/20 flex items-center gap-1.5">
              <Check size={10} className="text-green-400" />
              <span className="text-[8px] text-green-400">
                배열 크기 일치 ({arrayValue.length}개) - 버튼 클릭으로 모든 값 복사 가능
              </span>
            </div>
          )}
        </div>
      )}

      {/* Array Elements - Always Visible */}
      <div className="space-y-1">
        {arrayValue.length === 0 ? (
          <div className="p-3 bg-[#1a1a2e] rounded border border-gray-700">
            {liveJoints ? (
              // 텔레메트리 있을 때: 큰 초기화 버튼
              <div className="space-y-2">
                <button
                  onClick={(e) => { e.stopPropagation(); initFromTelemetry() }}
                  className="w-full py-3 bg-green-500/30 hover:bg-green-500/50 border border-green-500/50 text-green-300 rounded text-[11px] font-bold flex items-center justify-center gap-2 shadow-lg shadow-green-500/10 transition-all"
                >
                  <Crosshair size={16} className="animate-pulse" />
                  현재 로봇 자세로 초기화 ({liveJoints.length}개 관절)
                </button>
                <p className="text-[9px] text-gray-500 text-center">
                  또는 <button
                    onClick={(e) => { e.stopPropagation(); addElement() }}
                    className="text-blue-400 hover:underline"
                  >수동으로 추가</button>
                </p>
              </div>
            ) : (
              // 텔레메트리 없을 때: 수동 추가만
              <div className="text-center">
                <p className="text-[10px] text-gray-500 mb-2">배열이 비어있습니다</p>
                <button
                  onClick={(e) => { e.stopPropagation(); addElement() }}
                  className="px-3 py-1.5 bg-blue-500/20 hover:bg-blue-500/30 text-blue-400 rounded text-[10px] flex items-center gap-1 mx-auto"
                >
                  <Plus size={12} />
                  수동 추가
                </button>
              </div>
            )}
          </div>
        ) : (
          <>
            {/* Element list */}
            <div className="space-y-1 max-h-48 overflow-y-auto">
              {arrayValue.map((val, idx) => {
                const name = displayNames[idx] || `[${idx}]`
                const liveVal = liveJoints?.[idx]
                const hasDiff = liveVal !== undefined && Math.abs(liveVal - val) > 0.001

                return (
                  <div key={idx} className="flex items-center gap-2 group">
                    {/* Index/Name */}
                    <span className="text-[9px] text-gray-500 w-20 truncate" title={name}>
                      {name}
                    </span>

                    {/* Input */}
                    <input
                      type="number"
                      value={val}
                      onChange={(e) => { e.stopPropagation(); updateElement(idx, parseFloat(e.target.value) || 0) }}
                      onClick={(e) => e.stopPropagation()}
                      className="flex-1 px-2 py-1 bg-[#1a1a2e] border border-gray-700 rounded text-[10px] text-white font-mono focus:outline-none focus:border-amber-500"
                      step="0.001"
                    />

                    {/* Degree conversion */}
                    <span className="text-[8px] text-gray-600 w-12 text-right">
                      {formatNumber(val * 180 / Math.PI, 1)}°
                    </span>

                    {/* Live diff indicator */}
                    {hasDiff && (
                      <span className="text-[8px] text-purple-400 w-12 text-right" title="Live 값과 차이">
                        Δ{formatNumber(liveVal! - val, 2)}
                      </span>
                    )}

                    {/* Delete button */}
                    <button
                      onClick={(e) => { e.stopPropagation(); removeElement(idx) }}
                      className="p-1 text-gray-600 hover:text-red-400 hover:bg-red-500/10 rounded opacity-0 group-hover:opacity-100 transition-all"
                    >
                      <Trash2 size={12} />
                    </button>
                  </div>
                )
              })}
            </div>

            {/* Add button */}
            <button
              onClick={(e) => { e.stopPropagation(); addElement() }}
              className="w-full py-1.5 bg-[#1a1a2e] hover:bg-[#2a2a4a] border border-dashed border-gray-700 hover:border-gray-600 rounded text-[10px] text-gray-500 hover:text-gray-400 flex items-center justify-center gap-1 transition-colors"
            >
              <Plus size={12} />
              요소 추가
            </button>
          </>
        )}
      </div>
    </div>
  )
})

JointArrayEditor.displayName = 'JointArrayEditor'

export default JointArrayEditor
