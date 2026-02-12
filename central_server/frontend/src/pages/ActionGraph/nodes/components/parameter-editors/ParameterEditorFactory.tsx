import { memo, useCallback, useEffect } from 'react'
import { Code, Radio, Crosshair, Plus, Trash2 } from 'lucide-react'
import PoseEditor from './PoseEditor'
import JointArrayEditor from './JointArrayEditor'
import TelemetryPreview from './TelemetryPreview'
import { type RobotTelemetryData, getEditorType, getStdPrimitiveWrapperType } from './types'
import ParameterSourceSelector from '../../../../../components/ParameterSourceSelector'
import type { ParameterFieldSource } from '../../../../../types'
import type { AvailableStep } from '../../types'

interface ParameterEditorFactoryProps {
  fieldName: string
  fieldType: string
  isArray: boolean
  actionType?: string
  value: unknown
  onChange: (value: unknown) => void
  // Telemetry
  robotTelemetry?: RobotTelemetryData | null
  selectedToolFrame?: string | null
  // Binding
  fieldSource?: ParameterFieldSource
  availableSteps?: AvailableStep[]
  onFieldSourceChange?: (source: ParameterFieldSource | undefined) => void
}

const ParameterEditorFactory = memo(({
  fieldName,
  fieldType,
  isArray,
  actionType,
  value,
  onChange,
  robotTelemetry,
  selectedToolFrame,
  fieldSource,
  availableSteps,
  onFieldSourceChange,
}: ParameterEditorFactoryProps) => {
  const editorType = getEditorType(fieldType, isArray)
  const isToolFrameTarget = !isArray && isToolFrameField(fieldName)

  // Tool frame is dropdown-only field (no fixed text input, no step-result binding).
  if (isToolFrameTarget) {
    return (
      <ToolFrameFieldEditor
        fieldName={fieldName}
        fieldType={fieldType}
        value={value}
        onChange={onChange}
        robotTelemetry={robotTelemetry}
        fieldSource={fieldSource}
        onFieldSourceChange={onFieldSourceChange}
      />
    )
  }

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
      <PoseEditorWithBinding
        fieldName={fieldName}
        fieldType={fieldType}
        value={value}
        onChange={onChange}
        robotTelemetry={robotTelemetry}
        isStamped={isStamped}
        isPoint={false}
        preferredFrameId={selectedToolFrame || undefined}
        fieldSource={fieldSource}
        availableSteps={availableSteps}
        onFieldSourceChange={onFieldSourceChange}
      />
    )
  }

  // Point types (Point, PointStamped, Vector3)
  if (editorType === 'point') {
    const isStamped = fieldType.toLowerCase().includes('stamped')
    return (
      <PoseEditorWithBinding
        fieldName={fieldName}
        fieldType={fieldType}
        value={value}
        onChange={onChange}
        robotTelemetry={robotTelemetry}
        isStamped={isStamped}
        isPoint={true}
        preferredFrameId={selectedToolFrame || undefined}
        fieldSource={fieldSource}
        availableSteps={availableSteps}
        onFieldSourceChange={onFieldSourceChange}
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
        actionType={actionType}
        value={value}
        onChange={onChange}
        robotTelemetry={robotTelemetry}
        fieldSource={fieldSource}
        availableSteps={availableSteps}
        onFieldSourceChange={onFieldSourceChange}
      />
    )
  }

  // std_msgs primitive wrappers (std_msgs/msg/String, std_msgs/msg/Bool, ...)
  if (editorType === 'std_primitive_msg') {
    return (
      <StdPrimitiveMessageEditor
        fieldName={fieldName}
        fieldType={fieldType}
        value={value}
        onChange={onChange}
        robotTelemetry={robotTelemetry}
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

  // JointState type (sensor_msgs/msg/JointState)
  if (editorType === 'joint_state') {
    return (
      <JointStateEditor
        value={value}
        onChange={onChange}
        robotTelemetry={robotTelemetry}
        fieldSource={fieldSource}
        availableSteps={availableSteps}
        onFieldSourceChange={onFieldSourceChange}
        fieldType={fieldType}
        fieldName={fieldName}
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

function isManipulatorActionType(actionType?: string): boolean {
  const normalized = (actionType || '').toLowerCase()
  const compact = normalized.replace(/[^a-z0-9]/g, '')
  return (
    normalized.includes('joint_manipulation') ||
    normalized.includes('cartesian_manipulation') ||
    compact.includes('jointmanipulation') ||
    compact.includes('cartesianmanipulation')
  )
}

function isSpeedFactorField(fieldName: string): boolean {
  const normalized = fieldName.toLowerCase()
  return normalized === 'speed_factor' || normalized.endsWith('/speed_factor') || normalized.includes('speed_factor')
}

// Simple Primitive Editor with source selector
const PrimitiveEditor = memo(({
  fieldName,
  fieldType,
  actionType,
  value,
  onChange,
  robotTelemetry,
  fieldSource,
  availableSteps,
  onFieldSourceChange,
}: {
  fieldName: string
  fieldType: string
  actionType?: string
  value: unknown
  onChange: (value: unknown) => void
  robotTelemetry?: RobotTelemetryData | null
  fieldSource?: ParameterFieldSource
  availableSteps?: AvailableStep[]
  onFieldSourceChange?: (source: ParameterFieldSource | undefined) => void
}) => {
  const lower = fieldType.toLowerCase()
  const isBool = lower === 'bool' || lower === 'boolean'
  const isNumeric = ['int8', 'int16', 'int32', 'int64', 'uint8', 'uint16', 'uint32', 'uint64', 'float32', 'float64', 'double', 'float'].some(t => lower.includes(t))
  const isString = lower === 'string'
  const isBinding = fieldSource?.source === 'step_result'
  const supportsToolDropdown = isString && isToolFrameField(fieldName)
  const currentToolFrame = String(value ?? '').trim()
  const discoveredToolFrames = collectToolFramesFromTransforms(robotTelemetry)
  const toolFrameOptions = currentToolFrame && !discoveredToolFrames.includes(currentToolFrame)
    ? [...discoveredToolFrames, currentToolFrame]
    : discoveredToolFrames
  const shouldUseSpeedSlider = isNumeric && isManipulatorActionType(actionType) && isSpeedFactorField(fieldName)
  const normalizedSpeedFactorValue = (() => {
    if (!shouldUseSpeedSlider) return value
    const parsed = typeof value === 'number' ? value : parseFloat(String(value))
    if (!Number.isFinite(parsed)) return 1.0
    return Math.min(1.0, Math.max(0.1, parsed))
  })()

  useEffect(() => {
    if (!shouldUseSpeedSlider) return
    if (typeof normalizedSpeedFactorValue !== 'number') return
    if (value === normalizedSpeedFactorValue) return
    onChange(normalizedSpeedFactorValue)
  }, [shouldUseSpeedSlider, normalizedSpeedFactorValue, onChange, value])

  const inputType = isBool
    ? 'checkbox'
    : shouldUseSpeedSlider
      ? 'slider'
      : isNumeric
        ? 'number'
        : 'text'

  return (
    <div className="space-y-1.5">
      <ParameterSourceSelector
        fieldSource={fieldSource}
        availableSteps={availableSteps || []}
        onChange={onFieldSourceChange || (() => {})}
        targetFieldType={fieldType}
        targetFieldName={fieldName}
        constantValue={normalizedSpeedFactorValue}
        onConstantChange={onChange}
        inputType={inputType}
      />
      {!isBinding && supportsToolDropdown && (
        <div className="space-y-1">
          {toolFrameOptions.length > 0 ? (
            <>
              <div className="text-[9px] text-cyan-300">base_link 하위 tool0 및 하위 프레임</div>
              <select
                value={currentToolFrame}
                onChange={(e) => onChange(e.target.value)}
                onClick={(e) => e.stopPropagation()}
                className="w-full px-2 py-1.5 bg-sunken border border-cyan-500/30 rounded text-[11px] text-primary focus:outline-none focus:border-cyan-400"
              >
                <option value="">도구 프레임 선택...</option>
                {toolFrameOptions.map((frame) => (
                  <option key={frame} value={frame}>{frame}</option>
                ))}
              </select>
            </>
          ) : (
            <div className="text-[9px] text-muted">
              base_link 하위의 tool0 프레임이 아직 감지되지 않았습니다.
            </div>
          )}
        </div>
      )}
    </div>
  )
})

const ToolFrameFieldEditor = memo(({
  fieldName: _fieldName,
  fieldType,
  value,
  onChange,
  robotTelemetry,
  fieldSource,
  onFieldSourceChange,
}: {
  fieldName: string
  fieldType: string
  value: unknown
  onChange: (value: unknown) => void
  robotTelemetry?: RobotTelemetryData | null
  fieldSource?: ParameterFieldSource
  onFieldSourceChange?: (source: ParameterFieldSource | undefined) => void
}) => {
  const normalizedType = (fieldType || '').toLowerCase()
  const stdWrapperType = getStdPrimitiveWrapperType(fieldType)
  const isStdStringWrapper = stdWrapperType === 'string' ||
    normalizedType.includes('std_msgs/msg/string') ||
    normalizedType.includes('std_msgs::msg/string')

  const currentToolFrame = extractWrappedString(value).trim()
  const discoveredToolFrames = collectToolFramesFromTransforms(robotTelemetry)
  const toolFrameOptions = currentToolFrame && !discoveredToolFrames.includes(currentToolFrame)
    ? [...discoveredToolFrames, currentToolFrame]
    : discoveredToolFrames

  // Force-disable binding/fixed-source for tool_frame fields.
  useEffect(() => {
    if (fieldSource?.source && onFieldSourceChange) {
      onFieldSourceChange(undefined)
    }
  }, [fieldSource?.source, onFieldSourceChange])

  const handleToolFrameChange = useCallback((nextValue: string) => {
    if (
      isStdStringWrapper ||
      (value && typeof value === 'object' && !Array.isArray(value) && 'data' in (value as Record<string, unknown>))
    ) {
      onChange({ data: nextValue })
      return
    }
    onChange(nextValue)
  }, [isStdStringWrapper, onChange, value])

  return (
    <div className="space-y-1.5">
      <div className="space-y-1">
        <div className="text-[9px] text-cyan-300">base_link 하위 tool0 및 하위 프레임</div>
        <select
          value={currentToolFrame}
          onChange={(e) => handleToolFrameChange(e.target.value)}
          onClick={(e) => e.stopPropagation()}
          className="w-full px-2 py-1.5 bg-sunken border border-cyan-500/30 rounded text-[11px] text-primary focus:outline-none focus:border-cyan-400"
        >
          <option value="">
            {toolFrameOptions.length > 0 ? '도구 프레임 선택...' : '감지된 도구 프레임 없음'}
          </option>
          {toolFrameOptions.map((frame) => (
            <option key={frame} value={frame}>{frame}</option>
          ))}
        </select>
        {toolFrameOptions.length === 0 && (
          <div className="text-[9px] text-muted">
            base_link 하위의 tool0 프레임이 아직 감지되지 않았습니다.
          </div>
        )}
      </div>
    </div>
  )
})

function normalizeFrameName(frame: string): string {
  return frame.trim().replace(/^\/+/, '').replace(/\/+$/, '')
}

function getFrameBaseName(frame: string): string {
  const normalized = normalizeFrameName(frame)
  const parts = normalized.split('/')
  return (parts[parts.length - 1] || '').toLowerCase()
}

function hasAncestorFrame(
  startFrame: string,
  targetAncestorFrame: string,
  childToParents: Map<string, Set<string>>
): boolean {
  const normalizedStart = normalizeFrameName(startFrame)
  const normalizedTarget = normalizeFrameName(targetAncestorFrame)
  if (!normalizedStart || !normalizedTarget) return false
  if (normalizedStart === normalizedTarget) return true

  const visited = new Set<string>()
  const queue: string[] = [normalizedStart]

  while (queue.length > 0) {
    const current = queue.shift()!
    if (visited.has(current)) continue
    visited.add(current)

    const parents = childToParents.get(current)
    if (!parents) continue
    for (const parent of parents) {
      if (parent === normalizedTarget) return true
      if (!visited.has(parent)) queue.push(parent)
    }
  }

  return false
}

function collectToolFramesFromTransforms(robotTelemetry?: RobotTelemetryData | null): string[] {
  const transforms = robotTelemetry?.transforms || []
  if (transforms.length === 0) return []

  const parentToChildren = new Map<string, Set<string>>()
  const childToParents = new Map<string, Set<string>>()
  const originalFrameName = new Map<string, string>()

  transforms.forEach((transform) => {
    const parent = normalizeFrameName(transform.frame_id)
    const child = normalizeFrameName(transform.child_frame_id)
    if (!parent || !child) return
    if (!parentToChildren.has(parent)) {
      parentToChildren.set(parent, new Set())
    }
    parentToChildren.get(parent)!.add(child)
    if (!childToParents.has(child)) {
      childToParents.set(child, new Set())
    }
    childToParents.get(child)!.add(parent)
    if (!originalFrameName.has(parent)) {
      originalFrameName.set(parent, transform.frame_id)
    }
    if (!originalFrameName.has(child)) {
      originalFrameName.set(child, transform.child_frame_id)
    }
  })

  const tool0Candidates: string[] = []
  for (const frame of originalFrameName.keys()) {
    if (getFrameBaseName(frame) === 'tool0') {
      tool0Candidates.push(frame)
    }
  }

  if (tool0Candidates.length === 0) {
    const hasRobotBase = Array.from(originalFrameName.keys()).some((frame) => {
      const base = getFrameBaseName(frame)
      return base === 'base_link' || base === 'base'
    })
    return hasRobotBase ? ['tool0'] : []
  }

  // Prefer tool0 frames that are descendants of base_link.
  const roots = tool0Candidates.filter((candidate) =>
    hasAncestorFrame(candidate, 'base_link', childToParents)
  )
  const effectiveRoots = roots.length > 0 ? roots : tool0Candidates

  const visited = new Set<string>()
  const queue: string[] = [...effectiveRoots]
  const collected = new Set<string>()

  // Always include tool0 itself in the selectable tool frames.
  effectiveRoots.forEach((root) => {
    collected.add(originalFrameName.get(root) || root)
  })

  while (queue.length > 0) {
    const current = queue.shift()!
    if (visited.has(current)) continue
    visited.add(current)

    const children = parentToChildren.get(current)
    if (!children) continue

    children.forEach((child) => {
      if (!visited.has(child)) {
        queue.push(child)
      }
      collected.add(originalFrameName.get(child) || child)
    })
  }

  return Array.from(collected).sort((a, b) => a.localeCompare(b))
}

function isToolFrameField(fieldName: string): boolean {
  const normalized = fieldName.toLowerCase()
  return (
    normalized === 'tool' ||
    normalized === 'toolframe' ||
    normalized === 'tool_frame' ||
    normalized.includes('toolframe') ||
    normalized.includes('tool_frame') ||
    normalized.endsWith('_tool') ||
    normalized.includes('end_effector') ||
    normalized.includes('eef')
  )
}

function extractWrappedString(value: unknown): string {
  if (typeof value === 'string') return value
  if (value && typeof value === 'object' && !Array.isArray(value)) {
    const wrapped = value as Record<string, unknown>
    if (typeof wrapped.data === 'string') {
      return wrapped.data
    }
  }
  return ''
}

// std_msgs primitive wrapper editor (e.g. std_msgs/msg/String -> { data: "..." })
const StdPrimitiveMessageEditor = memo(({
  fieldName,
  fieldType,
  value,
  onChange,
  robotTelemetry,
  fieldSource,
  availableSteps,
  onFieldSourceChange,
}: {
  fieldName: string
  fieldType: string
  value: unknown
  onChange: (value: unknown) => void
  robotTelemetry?: RobotTelemetryData | null
  fieldSource?: ParameterFieldSource
  availableSteps?: AvailableStep[]
  onFieldSourceChange?: (source: ParameterFieldSource | undefined) => void
}) => {
  const wrapperType = getStdPrimitiveWrapperType(fieldType)

  const normalizeWrappedValue = useCallback((nextValue: unknown): { data: unknown } => {
    if (nextValue && typeof nextValue === 'object' && !Array.isArray(nextValue) && 'data' in (nextValue as Record<string, unknown>)) {
      return { data: (nextValue as Record<string, unknown>).data }
    }

    return { data: nextValue }
  }, [])

  const getDefaultValue = useCallback((): unknown => {
    if (wrapperType === 'boolean') return false
    if (wrapperType === 'number') return 0
    return ''
  }, [wrapperType])

  const wrapped = normalizeWrappedValue(value)
  const constantValue = wrapped.data ?? getDefaultValue()
  const inputType = wrapperType === 'boolean' ? 'checkbox' : wrapperType === 'number' ? 'number' : 'text'
  const isBinding = fieldSource?.source === 'step_result'
  const supportsToolDropdown = wrapperType === 'string' && isToolFrameField(fieldName)
  const currentToolFrame = extractWrappedString(value).trim()
  const discoveredToolFrames = collectToolFramesFromTransforms(robotTelemetry)
  const toolFrameOptions = currentToolFrame && !discoveredToolFrames.includes(currentToolFrame)
    ? [...discoveredToolFrames, currentToolFrame]
    : discoveredToolFrames

  const handleConstantChange = useCallback((nextValue: unknown) => {
    if (wrapperType === 'boolean') {
      onChange({ data: Boolean(nextValue) })
      return
    }

    if (wrapperType === 'number') {
      const normalizedNumber = typeof nextValue === 'number' ? nextValue : parseFloat(String(nextValue))
      onChange({ data: Number.isFinite(normalizedNumber) ? normalizedNumber : 0 })
      return
    }

    onChange({ data: String(nextValue ?? '') })
  }, [onChange, wrapperType])

  return (
    <div className="space-y-1.5">
      <ParameterSourceSelector
        fieldSource={fieldSource}
        availableSteps={availableSteps || []}
        onChange={onFieldSourceChange || (() => {})}
        targetFieldType={fieldType}
        targetFieldName={fieldName}
        constantValue={constantValue}
        onConstantChange={handleConstantChange}
        inputType={inputType}
      />
      {!isBinding && supportsToolDropdown && (
        <div className="space-y-1">
          {toolFrameOptions.length > 0 ? (
            <>
              <div className="text-[9px] text-cyan-300">base_link 하위 tool0 및 하위 프레임</div>
              <select
                value={currentToolFrame}
                onChange={(e) => handleConstantChange(e.target.value)}
                onClick={(e) => e.stopPropagation()}
                className="w-full px-2 py-1.5 bg-sunken border border-cyan-500/30 rounded text-[11px] text-primary focus:outline-none focus:border-cyan-400"
              >
                <option value="">도구 프레임 선택...</option>
                {toolFrameOptions.map((frame) => (
                  <option key={frame} value={frame}>{frame}</option>
                ))}
              </select>
            </>
          ) : (
            <div className="text-[9px] text-muted">
              base_link 하위의 tool0 프레임이 아직 감지되지 않았습니다.
            </div>
          )}
        </div>
      )}
      <div className="text-[9px] text-muted">
        저장 형식: <code className="text-[9px] text-purple-300">{'{'}"data": ...{'}'}</code>
      </div>
    </div>
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
        <div className="p-2 bg-elevated rounded border border-primary text-center">
          <p className="text-[10px] text-muted mb-2">배열이 비어있습니다</p>
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
                <span className="text-[9px] text-muted w-6">[{i}]</span>
                <input
                  type="number"
                  value={v}
                  onChange={(e) => { e.stopPropagation(); updateElement(i, parseFloat(e.target.value) || 0) }}
                  onClick={(e) => e.stopPropagation()}
                  className="flex-1 px-2 py-1 bg-elevated border border-primary rounded text-[10px] text-primary font-mono focus:outline-none focus:border-amber-500"
                  step="0.001"
                />
                <button
                  onClick={(e) => { e.stopPropagation(); removeElement(i) }}
                  className="p-1 text-muted hover:text-red-400 hover:bg-red-500/10 rounded opacity-0 group-hover:opacity-100 transition-all"
                >
                  ×
                </button>
              </div>
            ))}
          </div>
          <button
            onClick={(e) => { e.stopPropagation(); addElement() }}
            className="w-full py-1 bg-elevated hover:bg-surface border border-dashed border-gray-700 rounded text-[10px] text-muted hover:text-secondary"
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
        <div className="p-2 bg-elevated rounded border border-primary text-center">
          <p className="text-[10px] text-muted mb-2">배열이 비어있습니다</p>
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
                <span className="text-[9px] text-muted w-6">[{i}]</span>
                <input
                  type="text"
                  value={v}
                  onChange={(e) => { e.stopPropagation(); updateElement(i, e.target.value) }}
                  onClick={(e) => e.stopPropagation()}
                  className="flex-1 px-2 py-1 bg-elevated border border-primary rounded text-[10px] text-primary focus:outline-none focus:border-amber-500"
                />
                <button
                  onClick={(e) => { e.stopPropagation(); removeElement(i) }}
                  className="p-1 text-muted hover:text-red-400 hover:bg-red-500/10 rounded opacity-0 group-hover:opacity-100 transition-all"
                >
                  ×
                </button>
              </div>
            ))}
          </div>
          <button
            onClick={(e) => { e.stopPropagation(); addElement() }}
            className="w-full py-1 bg-elevated hover:bg-surface border border-dashed border-gray-700 rounded text-[10px] text-muted hover:text-secondary"
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
              : 'bg-gray-800/30 border-gray-700 text-muted cursor-not-allowed'
          }`}
        >
          <Code size={10} />
          이전 Step 결과 사용
          {!hasBindableSteps && <span className="text-[9px] text-muted ml-1">(사용 가능한 Step 없음)</span>}
        </button>
      )}
    </div>
  )
})

// Pose Editor with Binding support (includes telemetry capture)
const PoseEditorWithBinding = memo(({
  value,
  onChange,
  fieldSource,
  availableSteps,
  onFieldSourceChange,
  fieldType,
  fieldName,
  robotTelemetry,
  isStamped,
  isPoint,
  preferredFrameId,
}: {
  value: unknown
  onChange: (value: unknown) => void
  fieldSource?: ParameterFieldSource
  availableSteps?: AvailableStep[]
  onFieldSourceChange?: (source: ParameterFieldSource | undefined) => void
  fieldType: string
  fieldName: string
  robotTelemetry?: RobotTelemetryData | null
  isStamped: boolean
  isPoint: boolean
  preferredFrameId?: string
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
      {/* PoseEditor with telemetry support */}
      <PoseEditor
        fieldName={fieldName}
        fieldType={fieldType}
        value={value}
        onChange={onChange}
        robotTelemetry={robotTelemetry}
        isStamped={isStamped}
        isPoint={isPoint}
        preferredFrameId={preferredFrameId}
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
              : 'bg-gray-800/30 border-gray-700 text-muted cursor-not-allowed'
          }`}
        >
          <Code size={10} />
          이전 Step 결과 사용
          {!hasBindableSteps && <span className="text-[9px] text-muted ml-1">(사용 가능한 Step 없음)</span>}
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
              : 'bg-gray-800/30 border-gray-700 text-muted cursor-not-allowed'
          }`}
        >
          <Code size={10} />
          이전 Step 결과 사용
          {!hasBindableSteps && <span className="text-[9px] text-muted ml-1">(사용 가능한 Step 없음)</span>}
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
          <div className="text-[9px] text-muted mb-1">Linear</div>
          <div className="grid grid-cols-3 gap-1">
            {(['x', 'y', 'z'] as const).map((axis) => (
              <input
                key={axis}
                type="number"
                value={twistValue?.linear?.[axis] ?? 0}
                onChange={(e) => { e.stopPropagation(); updateField('linear', axis, parseFloat(e.target.value) || 0) }}
                onClick={(e) => e.stopPropagation()}
                placeholder={axis}
                className="px-2 py-1 bg-sunken border border-primary rounded text-[10px] text-primary font-mono"
                step="0.01"
              />
            ))}
          </div>
        </div>
        <div>
          <div className="text-[9px] text-muted mb-1">Angular</div>
          <div className="grid grid-cols-3 gap-1">
            {(['x', 'y', 'z'] as const).map((axis) => (
              <input
                key={axis}
                type="number"
                value={twistValue?.angular?.[axis] ?? 0}
                onChange={(e) => { e.stopPropagation(); updateField('angular', axis, parseFloat(e.target.value) || 0) }}
                onClick={(e) => e.stopPropagation()}
                placeholder={axis}
                className="px-2 py-1 bg-sunken border border-primary rounded text-[10px] text-primary font-mono"
                step="0.01"
              />
            ))}
          </div>
        </div>
      </div>
    </div>
  )
})

// JointState Editor for sensor_msgs/msg/JointState
const JointStateEditor = memo(({
  value,
  onChange,
  robotTelemetry,
  fieldSource,
  availableSteps,
  onFieldSourceChange,
  fieldType,
  fieldName,
}: {
  value: unknown
  onChange: (value: unknown) => void
  robotTelemetry?: RobotTelemetryData | null
  fieldSource?: ParameterFieldSource
  availableSteps?: AvailableStep[]
  onFieldSourceChange?: (source: ParameterFieldSource | undefined) => void
  fieldType: string
  fieldName: string
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

  // Get live joint state from telemetry
  const liveJointState = robotTelemetry?.joint_state
  const hasLiveTelemetry = !!(liveJointState?.name && liveJointState.name.length > 0)

  // Current value as JointState
  const jointStateValue = value as {
    name?: string[]
    position?: number[]
    velocity?: number[]
    effort?: number[]
  } | null

  const jointNames = jointStateValue?.name ?? []
  const jointPositions = jointStateValue?.position ?? []
  const hasValue = jointNames.length > 0

  const normalizeCurrentJointState = useCallback(() => {
    const names = [...jointNames]
    const positions = [...jointPositions]
    const maxLen = Math.max(names.length, positions.length)

    while (names.length < maxLen) names.push(`joint_${names.length + 1}`)
    while (positions.length < maxLen) positions.push(0)

    return { name: names, position: positions }
  }, [jointNames, jointPositions])

  // Capture current telemetry (only name and position - what planners actually use)
  const handleCapture = useCallback(() => {
    if (!liveJointState) return
    // Only capture name and position - velocity/effort are rarely needed for goal
    onChange({
      name: [...liveJointState.name],
      position: [...liveJointState.position],
    })
  }, [liveJointState, onChange])

  const startManualInput = useCallback(() => {
    if (hasValue) return
    onChange({
      name: ['joint_1'],
      position: [0],
    })
  }, [hasValue, onChange])

  const addManualJoint = useCallback(() => {
    const current = normalizeCurrentJointState()
    const nextIndex = current.name.length + 1
    onChange({
      ...current,
      name: [...current.name, `joint_${nextIndex}`],
      position: [...current.position, 0],
    })
  }, [normalizeCurrentJointState, onChange])

  const removeManualJoint = useCallback((index: number) => {
    const current = normalizeCurrentJointState()
    onChange({
      ...current,
      name: current.name.filter((_, i) => i !== index),
      position: current.position.filter((_, i) => i !== index),
    })
  }, [normalizeCurrentJointState, onChange])

  const updateManualJointName = useCallback((index: number, nextName: string) => {
    const current = normalizeCurrentJointState()
    const next = [...current.name]
    next[index] = nextName
    onChange({
      ...current,
      name: next,
    })
  }, [normalizeCurrentJointState, onChange])

  const updateManualJointPosition = useCallback((index: number, nextPosition: number) => {
    const current = normalizeCurrentJointState()
    const next = [...current.position]
    next[index] = nextPosition
    onChange({
      ...current,
      position: next,
    })
  }, [normalizeCurrentJointState, onChange])

  // Check if there are any steps with result fields (for enabling binding)
  const hasBindableSteps = availableSteps?.some(s => s.resultFields && s.resultFields.length > 0) || false

  return (
    <div className="space-y-2">
      {/* Live Telemetry Preview */}
      {hasLiveTelemetry ? (
        <div className="p-2 bg-green-500/10 rounded border border-green-500/30">
          <div className="flex items-center justify-between mb-2">
            <div className="flex items-center gap-2">
              <Radio className="w-3 h-3 text-green-400 animate-pulse" />
              <span className="text-[9px] text-green-400 font-medium">LIVE JointState</span>
              <span className="text-[8px] text-muted">({liveJointState!.name.length}개 관절)</span>
            </div>
          </div>
          {/* Simplified view - only name and position (what planners need) */}
          <div className="max-h-28 overflow-y-auto mb-2">
            <div className="grid grid-cols-2 gap-2 text-[8px] text-muted font-semibold mb-1 px-1">
              <span>Joint Name</span>
              <span>Position (rad)</span>
            </div>
            {liveJointState!.name.map((name, idx) => (
              <div key={name} className="grid grid-cols-2 gap-2 text-[9px] font-mono py-0.5 px-1 hover:bg-green-500/10 rounded">
                <span className="text-primary truncate" title={name}>{name}</span>
                <span className="text-cyan-400">{liveJointState!.position[idx]?.toFixed(4)}</span>
              </div>
            ))}
          </div>
          <button
            onClick={(e) => { e.stopPropagation(); handleCapture() }}
            className="w-full py-2.5 bg-green-500/30 hover:bg-green-500/50 text-green-300 rounded text-[11px] font-bold flex items-center justify-center gap-2 transition-all"
          >
            <Crosshair size={14} />
            현재 자세 캡처 ({liveJointState!.name.length}개 관절)
          </button>
        </div>
      ) : (
        <div className="p-3 bg-gray-800/50 rounded border border-gray-700/50">
          <div className="flex items-center gap-2">
            <Radio className="w-3 h-3 text-muted" />
            <span className="text-[10px] text-muted">JointState Telemetry 없음</span>
          </div>
          <p className="text-[9px] text-muted mt-1.5">로봇을 선택하고 텔레메트리가 수신되면 현재 자세를 캡처할 수 있습니다.</p>
        </div>
      )}

      {/* Saved/Manual Value */}
      {hasValue ? (
        <div className="p-2 bg-amber-500/10 rounded border border-amber-500/30">
          <div className="flex items-center justify-between mb-2">
            <span className="text-[10px] text-amber-400 font-medium">저장된 값 (수동 수정 가능)</span>
            <span className="text-[9px] text-amber-300">{jointNames.length}개 관절</span>
          </div>
          <div className="max-h-40 overflow-y-auto bg-sunken rounded p-1.5">
            <div className="grid grid-cols-[1fr_1fr_auto] gap-2 text-[8px] text-muted font-semibold mb-1">
              <span>Joint</span>
              <span>Position (rad)</span>
              <span />
            </div>
            {jointNames.map((name, idx) => (
              <div key={`${name}-${idx}`} className="grid grid-cols-[1fr_1fr_auto] gap-2 items-center py-0.5">
                <input
                  type="text"
                  value={name}
                  onChange={(e) => { e.stopPropagation(); updateManualJointName(idx, e.target.value) }}
                  onClick={(e) => e.stopPropagation()}
                  className="px-1.5 py-1 bg-elevated border border-primary rounded text-[9px] text-primary font-mono focus:outline-none focus:border-amber-500"
                />
                <input
                  type="number"
                  value={jointPositions[idx] ?? 0}
                  onChange={(e) => { e.stopPropagation(); updateManualJointPosition(idx, parseFloat(e.target.value) || 0) }}
                  onClick={(e) => e.stopPropagation()}
                  className="px-1.5 py-1 bg-elevated border border-primary rounded text-[9px] text-amber-300 font-mono focus:outline-none focus:border-amber-500"
                  step="0.001"
                />
                <button
                  onClick={(e) => { e.stopPropagation(); removeManualJoint(idx) }}
                  className="p-1 text-muted hover:text-red-400 hover:bg-red-500/10 rounded transition-colors"
                  title="관절 삭제"
                >
                  <Trash2 size={11} />
                </button>
              </div>
            ))}
          </div>
          <div className="flex items-center gap-2 mt-2">
            <button
              onClick={(e) => { e.stopPropagation(); addManualJoint() }}
              className="flex-1 py-1.5 bg-amber-500/20 hover:bg-amber-500/30 border border-amber-500/30 rounded text-[9px] text-amber-300 flex items-center justify-center gap-1"
            >
              <Plus size={11} />
              관절 추가
            </button>
            <button
              onClick={(e) => { e.stopPropagation(); onChange(null) }}
              className="flex-1 py-1.5 text-[9px] text-muted hover:text-red-400 hover:bg-red-500/10 rounded transition-colors"
            >
              값 초기화
            </button>
          </div>
        </div>
      ) : (
        <button
          onClick={(e) => { e.stopPropagation(); startManualInput() }}
          className="w-full py-1.5 bg-amber-500/10 hover:bg-amber-500/20 border border-amber-500/30 rounded text-[10px] text-amber-300 flex items-center justify-center gap-1"
        >
          <Plus size={12} />
          수동 입력 시작
        </button>
      )}

      {/* Binding option */}
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
              : 'bg-gray-800/30 border-gray-700 text-muted cursor-not-allowed'
          }`}
        >
          <Code size={10} />
          이전 Step 결과 사용
          {!hasBindableSteps && <span className="text-[9px] text-muted ml-1">(사용 가능한 Step 없음)</span>}
        </button>
      )}
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
      <div className="flex items-center gap-1 text-[9px] text-muted">
        <Code size={10} />
        <span>JSON ({fieldType})</span>
      </div>
      <textarea
        value={jsonString}
        onChange={(e) => { e.stopPropagation(); handleChange(e.target.value) }}
        onClick={(e) => e.stopPropagation()}
        className="w-full px-2 py-1.5 bg-sunken border border-primary rounded text-[9px] text-secondary font-mono focus:outline-none focus:border-amber-500 resize-none"
        rows={5}
      />
    </div>
  )
})

ParameterEditorFactory.displayName = 'ParameterEditorFactory'
PrimitiveEditor.displayName = 'PrimitiveEditor'
StdPrimitiveMessageEditor.displayName = 'StdPrimitiveMessageEditor'
JointStateEditor.displayName = 'JointStateEditor'
NumericArrayEditor.displayName = 'NumericArrayEditor'
StringArrayEditor.displayName = 'StringArrayEditor'
ArrayEditorWithBinding.displayName = 'ArrayEditorWithBinding'
PoseEditorWithBinding.displayName = 'PoseEditorWithBinding'
JointArrayEditorWithBinding.displayName = 'JointArrayEditorWithBinding'
TwistEditor.displayName = 'TwistEditor'
JsonEditor.displayName = 'JsonEditor'

export default ParameterEditorFactory
