import { useMemo } from 'react'
import { Link2, Hash, Check, AlertTriangle, X } from 'lucide-react'
import type {
  ParameterFieldSource,
  DataTypeInfo,
  TypeCompatibilityResult
} from '../types'
import {
  rosTypeToCanonical,
  checkTypeCompatibility
} from '../types'

interface StepInfo {
  id: string
  name: string
  resultFields?: Array<{ name: string; type: string }>
}

interface ParameterSourceSelectorProps {
  fieldSource?: ParameterFieldSource
  availableSteps: StepInfo[]
  onChange: (source: ParameterFieldSource | undefined) => void
  targetFieldType?: string  // Expected type of the target parameter field
  targetFieldName?: string  // Name of the target field (for display)
  // For constant value input
  constantValue?: unknown
  onConstantChange?: (value: unknown) => void
  inputType?: 'text' | 'number' | 'checkbox'
}

// Type compatibility indicator
function getCompatibilityIndicator(result: TypeCompatibilityResult | null): {
  color: string
  icon: React.ReactNode
  tooltip: string
} {
  if (!result) {
    return { color: '#22c55e', icon: <Check className="w-3 h-3" />, tooltip: '호환됨' }
  }

  if (!result.compatible) {
    return {
      color: '#ef4444',
      icon: <X className="w-3 h-3" />,
      tooltip: result.warningMessage || '타입이 호환되지 않음'
    }
  }

  if (result.conversionType === 'lossy') {
    return {
      color: '#f59e0b',
      icon: <AlertTriangle className="w-3 h-3" />,
      tooltip: result.warningMessage || '정밀도 손실 가능'
    }
  }

  return {
    color: '#22c55e',
    icon: <Check className="w-3 h-3" />,
    tooltip: '호환됨'
  }
}

export default function ParameterSourceSelector({
  fieldSource,
  availableSteps,
  onChange,
  targetFieldType,
  constantValue,
  onConstantChange,
  inputType = 'text',
}: ParameterSourceSelectorProps) {
  // Determine current mode: 'constant' or 'binding'
  const isBinding = fieldSource?.source === 'step_result'

  // Parse target type for compatibility checking
  const targetTypeInfo = useMemo<DataTypeInfo | null>(() => {
    if (!targetFieldType) return null
    return rosTypeToCanonical(targetFieldType)
  }, [targetFieldType])

  // Get compatibility for a field
  const getFieldCompatibility = (fieldType: string): TypeCompatibilityResult | null => {
    if (!targetTypeInfo) return null
    const sourceTypeInfo = rosTypeToCanonical(fieldType)
    return checkTypeCompatibility(sourceTypeInfo, targetTypeInfo)
  }

  // Get selected binding info
  const selectedStep = availableSteps.find(s => s.id === fieldSource?.step_id)
  const selectedField = selectedStep?.resultFields?.find(f => f.name === fieldSource?.result_field)

  // Switch to constant mode
  const switchToConstant = () => {
    onChange(undefined)
  }

  // Switch to binding mode
  const switchToBinding = () => {
    if (availableSteps.length > 0) {
      const firstStep = availableSteps[0]
      const firstField = firstStep.resultFields?.[0]
      onChange({
        source: 'step_result',
        step_id: firstStep.id,
        result_field: firstField?.name || '',
      })
    }
  }

  // Select a specific field from a step
  const selectField = (stepId: string, fieldName: string) => {
    onChange({
      source: 'step_result',
      step_id: stepId,
      result_field: fieldName,
    })
  }

  // Check if there are any bindable steps
  const hasBindableSteps = availableSteps.some(s => s.resultFields && s.resultFields.length > 0)

  return (
    <div className="space-y-1">
      {/* Mode Selection - Compact Radio style */}
      <div className="space-y-1">
        {/* Option 1: Fixed Value */}
        <div
          className={`rounded border transition-all cursor-pointer ${
            !isBinding
              ? 'border-amber-500/40 bg-amber-500/10'
              : 'border-gray-700 bg-surface hover:border-gray-600'
          }`}
          onClick={(e) => {
            e.stopPropagation()
            switchToConstant()
          }}
        >
          <div className="flex items-center gap-1.5 px-2 py-1.5">
            <div className={`w-3 h-3 rounded-full border-[1.5px] flex items-center justify-center ${
              !isBinding ? 'border-amber-500 bg-amber-500' : 'border-gray-500'
            }`}>
              {!isBinding && <div className="w-1.5 h-1.5 rounded-full bg-white" />}
            </div>
            <Hash className={`w-3 h-3 ${!isBinding ? 'text-amber-400' : 'text-gray-500'}`} />
            <span className={`text-[11px] font-medium ${!isBinding ? 'text-amber-400' : 'text-gray-400'}`}>
              고정값 입력
            </span>
          </div>

          {/* Constant value input - only when selected */}
          {!isBinding && onConstantChange && (
            <div className="px-2 pb-2">
              {inputType === 'checkbox' ? (
                <label className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={Boolean(constantValue)}
                    onChange={(e) => {
                      e.stopPropagation()
                      onConstantChange(e.target.checked)
                    }}
                    onClick={(e) => e.stopPropagation()}
                    className="w-4 h-4 rounded border-gray-600 bg-sunken text-amber-500 focus:ring-amber-500"
                  />
                  <span className="text-[11px] text-gray-300">{constantValue ? 'true' : 'false'}</span>
                </label>
              ) : inputType === 'number' ? (
                <input
                  type="number"
                  value={constantValue as number ?? ''}
                  onChange={(e) => {
                    e.stopPropagation()
                    onConstantChange(e.target.value === '' ? undefined : parseFloat(e.target.value))
                  }}
                  onClick={(e) => e.stopPropagation()}
                  className="w-full px-2 py-1.5 bg-sunken border border-amber-500/30 rounded text-[11px] text-white focus:outline-none focus:border-amber-500"
                  placeholder="숫자 입력..."
                />
              ) : (
                <input
                  type="text"
                  value={String(constantValue ?? '')}
                  onChange={(e) => {
                    e.stopPropagation()
                    onConstantChange(e.target.value)
                  }}
                  onClick={(e) => e.stopPropagation()}
                  className="w-full px-2 py-1.5 bg-sunken border border-amber-500/30 rounded text-[11px] text-white focus:outline-none focus:border-amber-500"
                  placeholder="값 입력..."
                />
              )}
            </div>
          )}
        </div>

        {/* Option 2: Bind from Previous Step */}
        <div
          className={`rounded border transition-all ${
            isBinding
              ? 'border-purple-500/40 bg-purple-500/10'
              : hasBindableSteps
                ? 'border-gray-700 bg-surface hover:border-gray-600 cursor-pointer'
                : 'border-gray-800 bg-sunken opacity-50 cursor-not-allowed'
          }`}
          onClick={(e) => {
            e.stopPropagation()
            if (hasBindableSteps && !isBinding) {
              switchToBinding()
            }
          }}
        >
          <div className="flex items-center gap-1.5 px-2 py-1.5">
            <div className={`w-3 h-3 rounded-full border-[1.5px] flex items-center justify-center ${
              isBinding ? 'border-purple-500 bg-purple-500' : 'border-gray-500'
            }`}>
              {isBinding && <div className="w-1.5 h-1.5 rounded-full bg-white" />}
            </div>
            <Link2 className={`w-3 h-3 ${isBinding ? 'text-purple-400' : 'text-gray-500'}`} />
            <span className={`text-[11px] font-medium ${isBinding ? 'text-purple-400' : 'text-gray-400'}`}>
              이전 Step 결과 사용
            </span>
            {!hasBindableSteps && (
              <span className="text-[9px] text-gray-600 ml-auto">사용 가능한 Step 없음</span>
            )}
          </div>

          {/* Binding selection - only when selected */}
          {isBinding && (
            <div className="px-2 pb-2 space-y-1.5">
              {/* Current binding preview */}
              {selectedStep && selectedField && (
                <div className="flex items-center gap-1.5 px-2 py-1.5 bg-purple-900/30 rounded border border-purple-500/30">
                  <span className="text-[9px] text-gray-400">바인딩:</span>
                  <code className="text-[11px] text-purple-300 font-mono">
                    {selectedStep.name || selectedStep.id}.{selectedField.name}
                  </code>
                  <span className="text-[9px] text-gray-500 ml-auto">
                    {selectedField.type}
                  </span>
                  {targetTypeInfo && (
                    <span
                      style={{ color: getCompatibilityIndicator(getFieldCompatibility(selectedField.type)).color }}
                      title={getCompatibilityIndicator(getFieldCompatibility(selectedField.type)).tooltip}
                    >
                      {getCompatibilityIndicator(getFieldCompatibility(selectedField.type)).icon}
                    </span>
                  )}
                </div>
              )}

              {/* Step and field selection */}
              <div className="space-y-1">
                <div className="text-[9px] text-gray-500">사용할 결과값 선택:</div>
                <div className="max-h-[140px] overflow-y-auto space-y-1">
                  {availableSteps.map(step => {
                    const hasFields = step.resultFields && step.resultFields.length > 0
                    if (!hasFields) return null

                    return (
                      <div
                        key={step.id}
                        className="rounded border border-gray-700 bg-sunken overflow-hidden"
                      >
                        {/* Step header */}
                        <div className="flex items-center gap-1.5 px-2 py-1 bg-gray-800/50 border-b border-gray-700">
                          <div className="w-1.5 h-1.5 rounded-full bg-green-500" />
                          <span className="text-[10px] font-medium text-gray-300">
                            {step.name || step.id}
                          </span>
                          <span className="text-[9px] text-gray-600 ml-auto">
                            {step.resultFields!.length}개
                          </span>
                        </div>

                        {/* Fields list */}
                        <div className="p-0.5">
                          {step.resultFields!.map(field => {
                            const isSelected = fieldSource?.step_id === step.id &&
                                             fieldSource?.result_field === field.name
                            const compat = getFieldCompatibility(field.type)
                            const compatIndicator = getCompatibilityIndicator(compat)
                            const isCompatible = !compat || compat.compatible

                            return (
                              <button
                                key={field.name}
                                onClick={(e) => {
                                  e.stopPropagation()
                                  selectField(step.id, field.name)
                                }}
                                disabled={!isCompatible}
                                className={`w-full flex items-center gap-1.5 px-2 py-1 rounded text-left transition-all ${
                                  isSelected
                                    ? 'bg-purple-500/30 border border-purple-500/50'
                                    : isCompatible
                                      ? 'hover:bg-white/5 border border-transparent'
                                      : 'opacity-40 cursor-not-allowed border border-transparent'
                                }`}
                                title={!isCompatible ? compatIndicator.tooltip : undefined}
                              >
                                {/* Compatibility indicator */}
                                <span style={{ color: compatIndicator.color }}>
                                  {isSelected ? (
                                    <Check className="w-3 h-3" />
                                  ) : (
                                    <div
                                      className="w-3 h-3 rounded-full border-[1.5px] flex items-center justify-center"
                                      style={{ borderColor: compatIndicator.color }}
                                    >
                                      {!isCompatible && <X className="w-2 h-2" />}
                                    </div>
                                  )}
                                </span>

                                {/* Field name */}
                                <span className={`text-[11px] font-mono ${
                                  isSelected ? 'text-purple-300' : 'text-gray-300'
                                }`}>
                                  {field.name}
                                </span>

                                {/* Field type */}
                                <span className="text-[9px] text-gray-600 ml-auto font-mono">
                                  {field.type}
                                </span>
                              </button>
                            )
                          })}
                        </div>
                      </div>
                    )
                  })}
                </div>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
