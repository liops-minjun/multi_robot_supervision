import { memo, useCallback } from 'react'
import { Code } from 'lucide-react'
import PoseEditor from './PoseEditor'
import JointArrayEditor from './JointArrayEditor'
import TelemetryPreview from './TelemetryPreview'
import { type RobotTelemetryData, getEditorType } from './types'
import ParameterSourceSelector from '../../../../../components/ParameterSourceSelector'
import type { ParameterFieldSource } from '../../../../../types'
import type { AvailableStep } from '../../types'

interface ParameterEditorFactoryProps {
  fieldName: string
  fieldType: string
  isArray: boolean
  value: unknown
  onChange: (value: unknown) => void
  // Telemetry
  robotTelemetry?: RobotTelemetryData | null
  // Binding
  fieldSource?: ParameterFieldSource
  availableSteps?: AvailableStep[]
  onFieldSourceChange?: (source: ParameterFieldSource | undefined) => void
}

const ParameterEditorFactory = memo(({
  fieldName,
  fieldType,
  isArray,
  value,
  onChange,
  robotTelemetry,
  fieldSource,
  availableSteps,
  onFieldSourceChange,
}: ParameterEditorFactoryProps) => {
  const editorType = getEditorType(fieldType, isArray)

  // Check if field has a binding (step_result source)
  const hasBinding = fieldSource?.source === 'step_result'

  // If bound to step result, show binding info instead of editor
  if (hasBinding && onFieldSourceChange) {
    return (
      <div className="space-y-2">
        <ParameterSourceSelector
          fieldSource={fieldSource}
          availableSteps={availableSteps || []}
          onChange={onFieldSourceChange}
          targetFieldType={fieldType}
          targetFieldName={fieldName}
        />
      </div>
    )
  }

  // Pose types (Pose, PoseStamped, PoseWithCovariance, etc.)
  if (editorType === 'pose') {
    const isStamped = fieldType.toLowerCase().includes('stamped') || fieldType.toLowerCase().includes('covariance')
    return (
      <PoseEditor
        fieldName={fieldName}
        fieldType={fieldType}
        value={value}
        onChange={onChange}
        robotTelemetry={robotTelemetry}
        isStamped={isStamped}
        isPoint={false}
      />
    )
  }

  // Point types (Point, PointStamped, Vector3)
  if (editorType === 'point') {
    const isStamped = fieldType.toLowerCase().includes('stamped')
    return (
      <PoseEditor
        fieldName={fieldName}
        fieldType={fieldType}
        value={value}
        onChange={onChange}
        robotTelemetry={robotTelemetry}
        isStamped={isStamped}
        isPoint={true}
      />
    )
  }

  // Joint array (float64[] compatible with joint_state)
  if (editorType === 'joint_array') {
    return (
      <JointArrayEditorWithBinding
        value={value as number[] | null}
        onChange={onChange}
        fieldSource={fieldSource}
        availableSteps={availableSteps}
        onFieldSourceChange={onFieldSourceChange}
        fieldType={fieldType}
        fieldName={fieldName}
        robotTelemetry={robotTelemetry}
      />
    )
  }

  // Numeric array (not joint-compatible)
  if (editorType === 'numeric_array') {
    return (
      <ArrayEditorWithBinding
        value={value as number[] | null}
        onChange={onChange}
        fieldSource={fieldSource}
        availableSteps={availableSteps}
        onFieldSourceChange={onFieldSourceChange}
        fieldType={fieldType}
        fieldName={fieldName}
        arrayType="numeric"
      />
    )
  }

  // String array
  if (editorType === 'string_array') {
    return (
      <ArrayEditorWithBinding
        value={value as string[] | null}
        onChange={onChange}
        fieldSource={fieldSource}
        availableSteps={availableSteps}
        onFieldSourceChange={onFieldSourceChange}
        fieldType={fieldType}
        fieldName={fieldName}
        arrayType="string"
      />
    )
  }

  // Primitive types (bool, number, string)
  if (editorType === 'primitive') {
    return (
      <PrimitiveEditor
        fieldName={fieldName}
        fieldType={fieldType}
        value={value}
        onChange={onChange}
        fieldSource={fieldSource}
        availableSteps={availableSteps}
        onFieldSourceChange={onFieldSourceChange}
      />
    )
  }

  // Twist type
  if (editorType === 'twist') {
    return (
      <TwistEditor
        value={value}
        onChange={onChange}
        robotTelemetry={robotTelemetry}
      />
    )
  }

  // Default: JSON editor for complex/unknown types
  return (
    <JsonEditor
      value={value}
      onChange={onChange}
      fieldType={fieldType}
    />
  )
})

// Simple Primitive Editor with source selector
const PrimitiveEditor = memo(({
  fieldName,
  fieldType,
  value,
  onChange,
  fieldSource,
  availableSteps,
  onFieldSourceChange,
}: {
  fieldName: string
  fieldType: string
  value: unknown
  onChange: (value: unknown) => void
  fieldSource?: ParameterFieldSource
  availableSteps?: AvailableStep[]
  onFieldSourceChange?: (source: ParameterFieldSource | undefined) => void
}) => {
  const lower = fieldType.toLowerCase()
  const isBool = lower === 'bool' || lower === 'boolean'
  const isNumeric = ['int8', 'int16', 'int32', 'int64', 'uint8', 'uint16', 'uint32', 'uint64', 'float32', 'float64', 'double', 'float'].some(t => lower.includes(t))

  const inputType = isBool ? 'checkbox' : isNumeric ? 'number' : 'text'

  return (
    <ParameterSourceSelector
      fieldSource={fieldSource}
      availableSteps={availableSteps || []}
      onChange={onFieldSourceChange || (() => {})}
      targetFieldType={fieldType}
      targetFieldName={fieldName}
      constantValue={value}
      onConstantChange={onChange}
      inputType={inputType}
    />
  )
})

// Numeric Array Editor (simplified)
const NumericArrayEditor = memo(({
  value,
  onChange,
}: {
  value: number[] | null
  onChange: (value: unknown) => void
}) => {
  const arrayValue = Array.isArray(value) ? value : []

  const addElement = useCallback(() => {
    onChange([...arrayValue, 0])
  }, [arrayValue, onChange])

  const removeElement = useCallback((index: number) => {
    onChange(arrayValue.filter((_, i) => i !== index))
  }, [arrayValue, onChange])

  const updateElement = useCallback((index: number, newValue: number) => {
    const newArray = [...arrayValue]
    newArray[index] = newValue
    onChange(newArray)
  }, [arrayValue, onChange])

  return (
    <div className="space-y-1">
      {arrayValue.length === 0 ? (
        <div className="p-2 bg-[#1a1a2e] rounded border border-gray-700 text-center">
          <p className="text-[10px] text-gray-500 mb-2">배열이 비어있습니다</p>
          <button
            onClick={(e) => { e.stopPropagation(); addElement() }}
            className="px-3 py-1.5 bg-blue-500/20 hover:bg-blue-500/30 text-blue-400 rounded text-[10px]"
          >
            + 추가
          </button>
        </div>
      ) : (
        <>
          <div className="space-y-1 max-h-40 overflow-y-auto">
            {arrayValue.map((v, i) => (
              <div key={i} className="flex items-center gap-1 group">
                <span className="text-[9px] text-gray-500 w-6">[{i}]</span>
                <input
                  type="number"
                  value={v}
                  onChange={(e) => { e.stopPropagation(); updateElement(i, parseFloat(e.target.value) || 0) }}
                  onClick={(e) => e.stopPropagation()}
                  className="flex-1 px-2 py-1 bg-[#1a1a2e] border border-gray-700 rounded text-[10px] text-white font-mono focus:outline-none focus:border-amber-500"
                  step="0.001"
                />
                <button
                  onClick={(e) => { e.stopPropagation(); removeElement(i) }}
                  className="p-1 text-gray-600 hover:text-red-400 hover:bg-red-500/10 rounded opacity-0 group-hover:opacity-100 transition-all"
                >
                  ×
                </button>
              </div>
            ))}
          </div>
          <button
            onClick={(e) => { e.stopPropagation(); addElement() }}
            className="w-full py-1 bg-[#1a1a2e] hover:bg-[#2a2a4a] border border-dashed border-gray-700 rounded text-[10px] text-gray-500 hover:text-gray-400"
          >
            + 추가
          </button>
        </>
      )}
    </div>
  )
})

// String Array Editor (simplified)
const StringArrayEditor = memo(({
  value,
  onChange,
}: {
  value: string[] | null
  onChange: (value: unknown) => void
}) => {
  const arrayValue = Array.isArray(value) ? value : []

  const addElement = useCallback(() => {
    onChange([...arrayValue, ''])
  }, [arrayValue, onChange])

  const removeElement = useCallback((index: number) => {
    onChange(arrayValue.filter((_, i) => i !== index))
  }, [arrayValue, onChange])

  const updateElement = useCallback((index: number, newValue: string) => {
    const newArray = [...arrayValue]
    newArray[index] = newValue
    onChange(newArray)
  }, [arrayValue, onChange])

  return (
    <div className="space-y-1">
      {arrayValue.length === 0 ? (
        <div className="p-2 bg-[#1a1a2e] rounded border border-gray-700 text-center">
          <p className="text-[10px] text-gray-500 mb-2">배열이 비어있습니다</p>
          <button
            onClick={(e) => { e.stopPropagation(); addElement() }}
            className="px-3 py-1.5 bg-blue-500/20 hover:bg-blue-500/30 text-blue-400 rounded text-[10px]"
          >
            + 추가
          </button>
        </div>
      ) : (
        <>
          <div className="space-y-1 max-h-40 overflow-y-auto">
            {arrayValue.map((v, i) => (
              <div key={i} className="flex items-center gap-1 group">
                <span className="text-[9px] text-gray-500 w-6">[{i}]</span>
                <input
                  type="text"
                  value={v}
                  onChange={(e) => { e.stopPropagation(); updateElement(i, e.target.value) }}
                  onClick={(e) => e.stopPropagation()}
                  className="flex-1 px-2 py-1 bg-[#1a1a2e] border border-gray-700 rounded text-[10px] text-white focus:outline-none focus:border-amber-500"
                />
                <button
                  onClick={(e) => { e.stopPropagation(); removeElement(i) }}
                  className="p-1 text-gray-600 hover:text-red-400 hover:bg-red-500/10 rounded opacity-0 group-hover:opacity-100 transition-all"
                >
                  ×
                </button>
              </div>
            ))}
          </div>
          <button
            onClick={(e) => { e.stopPropagation(); addElement() }}
            className="w-full py-1 bg-[#1a1a2e] hover:bg-[#2a2a4a] border border-dashed border-gray-700 rounded text-[10px] text-gray-500 hover:text-gray-400"
          >
            + 추가
          </button>
        </>
      )}
    </div>
  )
})

// Array Editor with Binding support
const ArrayEditorWithBinding = memo(({
  value,
  onChange,
  fieldSource,
  availableSteps,
  onFieldSourceChange,
  fieldType,
  fieldName,
  arrayType,
}: {
  value: unknown[] | null
  onChange: (value: unknown) => void
  fieldSource?: ParameterFieldSource
  availableSteps?: AvailableStep[]
  onFieldSourceChange?: (source: ParameterFieldSource | undefined) => void
  fieldType: string
  fieldName: string
  arrayType: 'numeric' | 'string'
}) => {
  // Check if field has a binding (step_result source)
  const hasBinding = fieldSource?.source === 'step_result'

  // If bound to step result, show binding selector
  if (hasBinding && onFieldSourceChange) {
    return (
      <ParameterSourceSelector
        fieldSource={fieldSource}
        availableSteps={availableSteps || []}
        onChange={onFieldSourceChange}
        targetFieldType={fieldType}
        targetFieldName={fieldName}
      />
    )
  }

  // Check if there are any steps with result fields (for enabling binding)
  const hasBindableSteps = availableSteps?.some(s => s.resultFields && s.resultFields.length > 0) || false

  return (
    <div className="space-y-2">
      {/* Array Editor */}
      {arrayType === 'string' ? (
        <StringArrayEditor
          value={value as string[] | null}
          onChange={onChange}
        />
      ) : (
        <NumericArrayEditor
          value={value as number[] | null}
          onChange={onChange}
        />
      )}

      {/* Binding option - always show, disabled when no bindable steps */}
      {onFieldSourceChange && (
        <button
          onClick={(e) => {
            e.stopPropagation()
            if (!hasBindableSteps) return
            const firstStep = availableSteps?.find(s => s.resultFields && s.resultFields.length > 0)
            if (firstStep) {
              onFieldSourceChange({
                source: 'step_result',
                step_id: firstStep.id,
                result_field: firstStep.resultFields?.[0]?.name || '',
              })
            }
          }}
          disabled={!hasBindableSteps}
          className={`w-full py-1.5 border rounded text-[10px] flex items-center justify-center gap-1 transition-all ${
            hasBindableSteps
              ? 'bg-purple-500/10 hover:bg-purple-500/20 border-purple-500/30 text-purple-400 cursor-pointer'
              : 'bg-gray-800/30 border-gray-700 text-gray-600 cursor-not-allowed'
          }`}
        >
          <Code size={10} />
          이전 Step 결과 사용
          {!hasBindableSteps && <span className="text-[9px] text-gray-600 ml-1">(사용 가능한 Step 없음)</span>}
        </button>
      )}
    </div>
  )
})

// Joint Array Editor with Binding support (includes telemetry capture)
const JointArrayEditorWithBinding = memo(({
  value,
  onChange,
  fieldSource,
  availableSteps,
  onFieldSourceChange,
  fieldType,
  fieldName,
  robotTelemetry,
}: {
  value: number[] | null
  onChange: (value: unknown) => void
  fieldSource?: ParameterFieldSource
  availableSteps?: AvailableStep[]
  onFieldSourceChange?: (source: ParameterFieldSource | undefined) => void
  fieldType: string
  fieldName: string
  robotTelemetry?: RobotTelemetryData | null
}) => {
  // Check if field has a binding (step_result source)
  const hasBinding = fieldSource?.source === 'step_result'

  // If bound to step result, show binding selector
  if (hasBinding && onFieldSourceChange) {
    return (
      <ParameterSourceSelector
        fieldSource={fieldSource}
        availableSteps={availableSteps || []}
        onChange={onFieldSourceChange}
        targetFieldType={fieldType}
        targetFieldName={fieldName}
      />
    )
  }

  // Check if there are any steps with result fields (for enabling binding)
  const hasBindableSteps = availableSteps?.some(s => s.resultFields && s.resultFields.length > 0) || false

  return (
    <div className="space-y-2">
      {/* JointArrayEditor with telemetry support */}
      <JointArrayEditor
        fieldName={fieldName}
        fieldType={fieldType}
        value={value}
        onChange={onChange}
        robotTelemetry={robotTelemetry}
        jointNames={robotTelemetry?.joint_state?.name}
      />

      {/* Binding option - always show, disabled when no bindable steps */}
      {onFieldSourceChange && (
        <button
          onClick={(e) => {
            e.stopPropagation()
            if (!hasBindableSteps) return
            const firstStep = availableSteps?.find(s => s.resultFields && s.resultFields.length > 0)
            if (firstStep) {
              onFieldSourceChange({
                source: 'step_result',
                step_id: firstStep.id,
                result_field: firstStep.resultFields?.[0]?.name || '',
              })
            }
          }}
          disabled={!hasBindableSteps}
          className={`w-full py-1.5 border rounded text-[10px] flex items-center justify-center gap-1 transition-all ${
            hasBindableSteps
              ? 'bg-purple-500/10 hover:bg-purple-500/20 border-purple-500/30 text-purple-400 cursor-pointer'
              : 'bg-gray-800/30 border-gray-700 text-gray-600 cursor-not-allowed'
          }`}
        >
          <Code size={10} />
          이전 Step 결과 사용
          {!hasBindableSteps && <span className="text-[9px] text-gray-600 ml-1">(사용 가능한 Step 없음)</span>}
        </button>
      )}
    </div>
  )
})

// Twist Editor
const TwistEditor = memo(({
  value,
  onChange,
  robotTelemetry,
}: {
  value: unknown
  onChange: (value: unknown) => void
  robotTelemetry?: RobotTelemetryData | null
}) => {
  const twistValue = value as { linear?: { x: number; y: number; z: number }; angular?: { x: number; y: number; z: number } } | null
  const liveTwist = robotTelemetry?.odometry?.twist

  const updateField = useCallback((group: 'linear' | 'angular', axis: 'x' | 'y' | 'z', val: number) => {
    const current = twistValue || { linear: { x: 0, y: 0, z: 0 }, angular: { x: 0, y: 0, z: 0 } }
    onChange({
      ...current,
      [group]: { ...current[group], [axis]: val },
    })
  }, [twistValue, onChange])

  const handleCapture = useCallback(() => {
    if (liveTwist) {
      onChange(liveTwist)
    }
  }, [liveTwist, onChange])

  return (
    <div className="space-y-2">
      {liveTwist && (
        <>
          <TelemetryPreview type="twist" liveValue={liveTwist} savedValue={twistValue || undefined} compact />
          <button
            onClick={(e) => { e.stopPropagation(); handleCapture() }}
            className="w-full py-1.5 bg-purple-500/20 hover:bg-purple-500/30 text-purple-400 rounded border border-purple-500/30 text-[10px] flex items-center justify-center gap-1"
          >
            현재 값 캡처
          </button>
        </>
      )}

      <div className="space-y-2">
        <div>
          <div className="text-[9px] text-gray-500 mb-1">Linear</div>
          <div className="grid grid-cols-3 gap-1">
            {(['x', 'y', 'z'] as const).map((axis) => (
              <input
                key={axis}
                type="number"
                value={twistValue?.linear?.[axis] ?? 0}
                onChange={(e) => { e.stopPropagation(); updateField('linear', axis, parseFloat(e.target.value) || 0) }}
                onClick={(e) => e.stopPropagation()}
                placeholder={axis}
                className="px-2 py-1 bg-[#16162a] border border-gray-700 rounded text-[10px] text-white font-mono"
                step="0.01"
              />
            ))}
          </div>
        </div>
        <div>
          <div className="text-[9px] text-gray-500 mb-1">Angular</div>
          <div className="grid grid-cols-3 gap-1">
            {(['x', 'y', 'z'] as const).map((axis) => (
              <input
                key={axis}
                type="number"
                value={twistValue?.angular?.[axis] ?? 0}
                onChange={(e) => { e.stopPropagation(); updateField('angular', axis, parseFloat(e.target.value) || 0) }}
                onClick={(e) => e.stopPropagation()}
                placeholder={axis}
                className="px-2 py-1 bg-[#16162a] border border-gray-700 rounded text-[10px] text-white font-mono"
                step="0.01"
              />
            ))}
          </div>
        </div>
      </div>
    </div>
  )
})

// JSON Editor for complex types
const JsonEditor = memo(({
  value,
  onChange,
  fieldType,
}: {
  value: unknown
  onChange: (value: unknown) => void
  fieldType: string
}) => {
  const handleChange = useCallback((jsonStr: string) => {
    try {
      const parsed = JSON.parse(jsonStr)
      onChange(parsed)
    } catch {
      // Keep raw string if invalid JSON
      onChange(jsonStr)
    }
  }, [onChange])

  const jsonString = typeof value === 'object' && value !== null
    ? JSON.stringify(value, null, 2)
    : String(value || '{}')

  return (
    <div className="space-y-1">
      <div className="flex items-center gap-1 text-[9px] text-gray-500">
        <Code size={10} />
        <span>JSON ({fieldType})</span>
      </div>
      <textarea
        value={jsonString}
        onChange={(e) => { e.stopPropagation(); handleChange(e.target.value) }}
        onClick={(e) => e.stopPropagation()}
        className="w-full px-2 py-1.5 bg-[#16162a] border border-gray-700 rounded text-[9px] text-gray-300 font-mono focus:outline-none focus:border-amber-500 resize-none"
        rows={5}
      />
    </div>
  )
})

ParameterEditorFactory.displayName = 'ParameterEditorFactory'
PrimitiveEditor.displayName = 'PrimitiveEditor'
NumericArrayEditor.displayName = 'NumericArrayEditor'
StringArrayEditor.displayName = 'StringArrayEditor'
ArrayEditorWithBinding.displayName = 'ArrayEditorWithBinding'
JointArrayEditorWithBinding.displayName = 'JointArrayEditorWithBinding'
TwistEditor.displayName = 'TwistEditor'
JsonEditor.displayName = 'JsonEditor'

export default ParameterEditorFactory
