import { memo, useEffect } from 'react'
import { ChevronDown, ChevronUp, Upload, Loader2, Radio } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import type { ActionField, ParameterFieldSource, RobotTelemetry } from '../../../../types'
import type { AvailableStep } from '../types'
import { ParameterEditorFactory, type RobotTelemetryData } from './parameter-editors'
import { useTelemetryOptional } from '../../../../contexts/TelemetryContext'
import { robotApi, telemetryApi } from '../../../../api/client'

interface GoalParametersSectionProps {
  isExpanded: boolean
  onToggle: () => void
  isLoadingFields: boolean
  goalFields: ActionField[]
  params: Record<string, unknown>
  fieldSources: Record<string, ParameterFieldSource>
  availableSteps: AvailableStep[]
  hasActionType: boolean
  onUpdateParam: (key: string, value: unknown) => void
  onUpdateFieldSource: (key: string, source: ParameterFieldSource | undefined) => void
}

// Convert RobotTelemetry to RobotTelemetryData format for editors
function toEditorTelemetry(telemetry: RobotTelemetry | null | undefined): RobotTelemetryData | null {
  if (!telemetry) return null
  return {
    joint_state: telemetry.joint_state ? {
      name: telemetry.joint_state.name,
      position: telemetry.joint_state.position,
      velocity: telemetry.joint_state.velocity,
      effort: telemetry.joint_state.effort,
    } : undefined,
    odometry: telemetry.odometry ? {
      pose: {
        position: telemetry.odometry.pose.position,
        orientation: telemetry.odometry.pose.orientation,
      },
      twist: {
        linear: telemetry.odometry.twist.linear,
        angular: telemetry.odometry.twist.angular,
      },
    } : undefined,
  }
}

const GoalParametersSection = memo(({
  isExpanded,
  onToggle,
  isLoadingFields,
  goalFields,
  params,
  fieldSources,
  availableSteps,
  hasActionType,
  onUpdateParam,
  onUpdateFieldSource,
}: GoalParametersSectionProps) => {
  // Use telemetry from context (shared with TelemetryPanel)
  const telemetryContext = useTelemetryOptional()
  const liveTelemetry = telemetryContext?.liveTelemetry
  const selectedRobotId = telemetryContext?.selectedRobotId
  const setSelectedRobotId = telemetryContext?.setSelectedRobotId
  const setLiveTelemetry = telemetryContext?.setLiveTelemetry

  // Fetch robots list when expanded
  const { data: robots = [] } = useQuery({
    queryKey: ['robots'],
    queryFn: () => robotApi.list(),
    enabled: isExpanded,
    staleTime: 30000,
  })

  // Auto-select first robot if none selected
  useEffect(() => {
    if (robots.length > 0 && !selectedRobotId && setSelectedRobotId) {
      setSelectedRobotId(robots[0].id)
    }
  }, [robots, selectedRobotId, setSelectedRobotId])

  // Fetch telemetry for selected robot
  const { data: telemetry } = useQuery({
    queryKey: ['robot-telemetry', selectedRobotId],
    queryFn: () => telemetryApi.getRobotTelemetry(selectedRobotId!),
    enabled: isExpanded && !!selectedRobotId,
    refetchInterval: isExpanded && selectedRobotId ? 500 : false,
  })

  // Sync telemetry to context
  useEffect(() => {
    if (telemetry && setLiveTelemetry) {
      setLiveTelemetry(telemetry)
    }
  }, [telemetry, setLiveTelemetry])

  const mergedTelemetry: RobotTelemetry | null = (liveTelemetry || telemetry)
    ? {
        ...(telemetry || {}),
        ...(liveTelemetry || {}),
        joint_state: liveTelemetry?.joint_state || telemetry?.joint_state,
        odometry: liveTelemetry?.odometry || telemetry?.odometry,
        updated_at: liveTelemetry?.updated_at || telemetry?.updated_at || new Date().toISOString(),
      }
    : null

  const editorTelemetry = toEditorTelemetry(mergedTelemetry)
  const hasTelemetry = !!(mergedTelemetry?.joint_state || mergedTelemetry?.odometry)

  return (
    <div className="border-b border-primary">
      <button
        onClick={(e) => { e.stopPropagation(); onToggle() }}
        className="w-full px-3 py-1.5 flex items-center justify-between hover:bg-elevated/50 transition-colors"
      >
        <div className="flex items-center gap-2">
          <Upload className="w-3 h-3 text-amber-500" />
          <span className="text-[10px] text-amber-400 uppercase tracking-wider font-medium">Goal 파라미터</span>
          {isLoadingFields ? (
            <Loader2 className="w-3 h-3 text-amber-500 animate-spin" />
          ) : goalFields.length > 0 ? (
            <span className="text-[9px] text-muted">({goalFields.length})</span>
          ) : Object.keys(fieldSources).length > 0 ? (
            <span className="text-[9px] text-purple-500">({Object.keys(fieldSources).length}개 바인딩)</span>
          ) : Object.keys(params).length > 0 ? (
            <span className="text-[9px] text-amber-500">({Object.keys(params).length})</span>
          ) : hasActionType ? (
            <span className="text-[9px] text-yellow-500 bg-yellow-500/10 px-1.5 py-0.5 rounded">클릭하여 설정</span>
          ) : null}
        </div>
        {isExpanded ? (
          <ChevronUp className="w-3 h-3 text-muted" />
        ) : (
          <ChevronDown className="w-3 h-3 text-muted" />
        )}
      </button>

      {isExpanded && (
        <div className="px-3 pb-2 space-y-2">
          {/* Robot Selector + Telemetry Status */}
          <div className={`flex items-center gap-2 p-2 rounded border ${hasTelemetry ? 'bg-green-500/10 border-green-500/30' : 'bg-purple-500/10 border-purple-500/30'}`}>
            <Radio className={`w-3 h-3 ${hasTelemetry ? 'text-green-400 animate-pulse' : 'text-muted'}`} />
            <select
              value={selectedRobotId || ''}
              onChange={(e) => {
                e.stopPropagation()
                if (setSelectedRobotId) {
                  setSelectedRobotId(e.target.value || null)
                }
              }}
              onClick={(e) => e.stopPropagation()}
              className="flex-1 px-2 py-1 bg-elevated border border-primary rounded text-[10px] text-primary focus:outline-none focus:border-purple-500 cursor-pointer"
            >
              <option value="">로봇 선택...</option>
              {robots.map((robot) => (
                <option key={robot.id} value={robot.id}>
                  {robot.name || robot.id}
                </option>
              ))}
            </select>
            {hasTelemetry && (
              <span className="text-[8px] text-green-400 bg-green-500/20 px-1.5 py-0.5 rounded">LIVE</span>
            )}
            {mergedTelemetry?.joint_state && (
              <span className="text-[8px] text-yellow-400 bg-yellow-500/10 px-1.5 py-0.5 rounded">
                {mergedTelemetry.joint_state.name.length}개 관절
              </span>
            )}
          </div>

          {/* Field editors */}
          {isLoadingFields ? (
            <div className="flex items-center gap-2 py-2">
              <Loader2 className="w-3 h-3 text-amber-500 animate-spin" />
              <span className="text-[9px] text-muted">액션 인터페이스 로딩 중...</span>
            </div>
          ) : goalFields.length === 0 ? (
            <div className="p-2 bg-elevated rounded border border-primary">
              <p className="text-[9px] text-muted">
                스키마 없음. Action Type을 선택하면 파라미터가 자동으로 표시됩니다.
              </p>
            </div>
          ) : (
            <div className="space-y-2">
              {goalFields.map((field) => (
                <div key={field.name} className="p-2 bg-surface rounded border border-primary/50">
                  {/* Field header */}
                  <div className="flex items-center gap-2 pb-1 mb-2 border-b border-gray-700/30">
                    <span className="text-[11px] text-primary font-medium">{field.name}</span>
                    <span className="text-[9px] text-muted font-mono px-1.5 py-0.5 bg-gray-800 rounded">
                      {field.type}{field.is_array ? '[]' : ''}
                    </span>
                  </div>

                  {/* Parameter Editor */}
                  <ParameterEditorFactory
                    fieldName={field.name}
                    fieldType={field.type}
                    isArray={field.is_array}
                    value={params[field.name]}
                    onChange={(value) => onUpdateParam(field.name, value)}
                    robotTelemetry={editorTelemetry}
                    fieldSource={fieldSources[field.name]}
                    availableSteps={availableSteps}
                    onFieldSourceChange={(source) => onUpdateFieldSource(field.name, source)}
                  />
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
})

GoalParametersSection.displayName = 'GoalParametersSection'

export default GoalParametersSection
