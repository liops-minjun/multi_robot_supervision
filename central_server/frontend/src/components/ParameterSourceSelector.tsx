import { useMemo } from 'react'
import { Link2, Hash, Check, AlertTriangle, X, Code } from 'lucide-react'
import type {
  ParameterFieldSource,
  DataTypeInfo,
  TypeCompatibilityResult,
  RuntimeBindingOption,
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
  runtimeBindings?: RuntimeBindingOption[]
  onChange: (source: ParameterFieldSource | undefined) => void
  targetFieldType?: string  // Expected type of the target parameter field
  targetFieldName?: string  // Name of the target field (for display)
  // For constant value input
  constantValue?: unknown
  onConstantChange?: (value: unknown) => void
  inputType?: 'text' | 'number' | 'checkbox' | 'slider'
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
  runtimeBindings = [],
  onChange,
  targetFieldType,
  constantValue,
  onConstantChange,
  inputType = 'text',
}: ParameterSourceSelectorProps) {
  const clampSpeedFactor = (raw: unknown): number => {
    const parsed = typeof raw === 'number' ? raw : parseFloat(String(raw))
    if (!Number.isFinite(parsed)) return 1.0
    return Math.min(1.0, Math.max(0.1, parsed))
  }

  const isBinding = fieldSource?.source === 'step_result'
  const isRuntimeBinding = fieldSource?.source === 'expression'
  const selectedRuntimeBinding = runtimeBindings.find(binding => binding.expression === fieldSource?.expression)

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

  const switchToRuntimeBinding = () => {
    if (runtimeBindings.length === 0) return
    onChange({
      source: 'expression',
      expression: runtimeBindings[0].expression,
    })
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
            !isBinding && !isRuntimeBinding
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
              !isBinding && !isRuntimeBinding ? 'border-amber-500 bg-amber-500' : 'border-gray-500'
            }`}>
              {!isBinding && !isRuntimeBinding && <div className="w-1.5 h-1.5 rounded-full bg-white" />}
            </div>
            <Hash className={`w-3 h-3 ${!isBinding && !isRuntimeBinding ? 'text-amber-400' : 'text-muted'}`} />
            <span className={`text-[11px] font-medium ${!isBinding && !isRuntimeBinding ? 'text-amber-400' : 'text-secondary'}`}>
              고정값 입력
            </span>
          </div>

          {/* Constant value input - only when selected */}
          {!isBinding && !isRuntimeBinding && onConstantChange && (
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
                  <span className="text-[11px] text-primary">{constantValue ? 'true' : 'false'}</span>
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
                  className="w-full px-2 py-1.5 bg-sunken border border-amber-500/30 rounded text-[11px] text-primary focus:outline-none focus:border-amber-500"
                  placeholder="숫자 입력..."
                />
              ) : inputType === 'slider' ? (
                <div className="space-y-1.5">
                  <input
                    type="range"
                    min={0.1}
                    max={1.0}
                    step={0.01}
                    value={clampSpeedFactor(constantValue)}
                    onChange={(e) => {
                      e.stopPropagation()
                      onConstantChange(clampSpeedFactor(e.target.value))
                    }}
                    onClick={(e) => e.stopPropagation()}
                    className="w-full accent-amber-500 cursor-pointer"
                  />
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-[9px] text-muted">0.1</span>
                    <input
                      type="number"
                      min={0.1}
                      max={1.0}
                      step={0.01}
                      value={clampSpeedFactor(constantValue)}
                      onChange={(e) => {
                        e.stopPropagation()
                        onConstantChange(clampSpeedFactor(e.target.value))
                      }}
                      onClick={(e) => e.stopPropagation()}
                      className="w-20 px-2 py-1 bg-sunken border border-amber-500/30 rounded text-[11px] text-primary focus:outline-none focus:border-amber-500 text-right font-mono"
                    />
                    <span className="text-[9px] text-muted">1.0</span>
                  </div>
                </div>
              ) : (
                <input
                  type="text"
                  value={String(constantValue ?? '')}
                  onChange={(e) => {
                    e.stopPropagation()
                    onConstantChange(e.target.value)
                  }}
                  onClick={(e) => e.stopPropagation()}
                  className="w-full px-2 py-1.5 bg-sunken border border-amber-500/30 rounded text-[11px] text-primary focus:outline-none focus:border-amber-500"
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
            <Link2 className={`w-3 h-3 ${isBinding ? 'text-purple-400' : 'text-muted'}`} />
            <span className={`text-[11px] font-medium ${isBinding ? 'text-purple-400' : 'text-secondary'}`}>
              이전 Step 결과 사용
            </span>
            {!hasBindableSteps && (
              <span className="text-[9px] text-muted ml-auto">사용 가능한 Step 없음</span>
            )}
          </div>

          {/* Binding selection - only when selected */}
          {isBinding && (
            <div className="px-2 pb-2 space-y-1.5">
              {/* Current binding preview */}
              {selectedStep && selectedField && (
                <div className="flex items-center gap-1.5 px-2 py-1.5 bg-purple-900/30 rounded border border-purple-500/30">
                  <span className="text-[9px] text-secondary">바인딩:</span>
                  <code className="text-[11px] text-purple-300 font-mono">
                    {selectedStep.name || selectedStep.id}.{selectedField.name}
                  </code>
                  <span className="text-[9px] text-muted ml-auto">
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
                <div className="text-[9px] text-muted">사용할 결과값 선택:</div>
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
                          <span className="text-[10px] font-medium text-primary">
                            {step.name || step.id}
                          </span>
                          <span className="text-[9px] text-muted ml-auto">
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
                                  isSelected ? 'text-purple-300' : 'text-primary'
                                }`}>
                                  {field.name}
                                </span>

                                {/* Field type */}
                                <span className="text-[9px] text-muted ml-auto font-mono">
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

        {/* Option 3: Bind from planner/runtime variable */}
        <div
          className={`rounded border transition-all ${
            isRuntimeBinding
              ? 'border-sky-500/40 bg-sky-500/10'
              : runtimeBindings.length > 0
                ? 'border-gray-700 bg-surface hover:border-gray-600 cursor-pointer'
                : 'border-gray-800 bg-sunken opacity-50 cursor-not-allowed'
          }`}
          onClick={(e) => {
            e.stopPropagation()
            if (runtimeBindings.length > 0 && !isRuntimeBinding) {
              switchToRuntimeBinding()
            }
          }}
        >
          <div className="flex items-center gap-1.5 px-2 py-1.5">
            <div className={`w-3 h-3 rounded-full border-[1.5px] flex items-center justify-center ${
              isRuntimeBinding ? 'border-sky-500 bg-sky-500' : 'border-gray-500'
            }`}>
              {isRuntimeBinding && <div className="w-1.5 h-1.5 rounded-full bg-white" />}
            </div>
            <Code className={`w-3 h-3 ${isRuntimeBinding ? 'text-sky-400' : 'text-muted'}`} />
            <span className={`text-[11px] font-medium ${isRuntimeBinding ? 'text-sky-400' : 'text-secondary'}`}>
              PDDL / 실행 변수 사용
            </span>
            {runtimeBindings.length === 0 && (
              <span className="text-[9px] text-muted ml-auto">사용 가능한 변수 없음</span>
            )}
          </div>

          {isRuntimeBinding && (
            <div className="px-2 pb-2 space-y-1.5">
              {fieldSource?.expression && (
                <div className="flex items-center gap-1.5 px-2 py-1.5 bg-sky-900/30 rounded border border-sky-500/30">
                  <span className="text-[9px] text-secondary">바인딩:</span>
                  <code className="text-[11px] text-sky-300 font-mono">
                    {fieldSource.expression}
                  </code>
                  {selectedRuntimeBinding?.label && (
                    <span className="text-[9px] text-muted ml-auto">
                      {selectedRuntimeBinding.label}
                    </span>
                  )}
                </div>
              )}

              <div className="space-y-1">
                <div className="text-[9px] text-muted">사용할 실행 변수 선택:</div>
                <div className="max-h-[140px] overflow-y-auto space-y-1">
                  {runtimeBindings.map((binding) => {
                    const isSelected = fieldSource?.expression === binding.expression
                    return (
                      <button
                        key={binding.key}
                        onClick={(e) => {
                          e.stopPropagation()
                          onChange({
                            source: 'expression',
                            expression: binding.expression,
                          })
                        }}
                        className={`w-full rounded border px-2 py-1.5 text-left transition-all ${
                          isSelected
                            ? 'border-sky-500/50 bg-sky-500/20'
                            : 'border-gray-700 bg-sunken hover:border-sky-500/30 hover:bg-sky-500/5'
                        }`}
                      >
                        <div className="flex items-center gap-2">
                          <code className={`text-[11px] font-mono ${isSelected ? 'text-sky-300' : 'text-primary'}`}>
                            {binding.expression}
                          </code>
                          <span className="text-[9px] text-muted">{binding.label}</span>
                        </div>
                        {binding.description && (
                          <div className="mt-1 text-[9px] text-muted">
                            {binding.description}
                          </div>
                        )}
                      </button>
                    )
                  })}
                </div>
              </div>

              <div className="space-y-1">
                <div className="text-[9px] text-muted">직접 입력</div>
                <input
                  type="text"
                  value={fieldSource?.expression || ''}
                  onChange={(e) => {
                    e.stopPropagation()
                    onChange({
                      source: 'expression',
                      expression: e.target.value,
                    })
                  }}
                  onClick={(e) => e.stopPropagation()}
                  placeholder="${resource_name}"
                  className="w-full rounded border border-sky-500/30 bg-sunken px-2 py-1.5 text-[11px] text-primary font-mono focus:outline-none focus:border-sky-400"
                />
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
