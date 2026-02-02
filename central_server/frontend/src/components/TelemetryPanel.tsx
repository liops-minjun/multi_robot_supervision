import { useState, useEffect, useCallback } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Radio, Copy, Check, RefreshCw, X, Crosshair
} from 'lucide-react'
import { telemetryApi, robotApi } from '../api/client'
import type { RobotTelemetry, JointStateData, OdometryData } from '../types'
import { useTelemetry } from '../contexts/TelemetryContext'

interface TelemetryPanelProps {
  isOpen: boolean
  onClose: () => void
  selectedRobotId?: string | null
  embedded?: boolean  // When true, don't render outer container (used in tabbed panels)
  horizontal?: boolean  // When true, display in horizontal layout for bottom panel
}

// Format number to 4 decimal places
const formatNumber = (num: number): string => {
  return num.toFixed(4)
}

// Telemetry type mapping for value capture
export const TELEMETRY_GOAL_MAPPING: Record<string, string[]> = {
  'odometry.pose': [
    'geometry_msgs/msg/Pose',
    'geometry_msgs/msg/PoseStamped',
  ],
  'odometry.pose.position': [
    'geometry_msgs/msg/Point',
  ],
  'odometry.twist': [
    'geometry_msgs/msg/Twist',
  ],
  'joint_state': [
    'sensor_msgs/msg/JointState',
    'trajectory_msgs/msg/JointTrajectoryPoint',
  ],
}

// Extract telemetry value and convert to goal parameter format
export function extractTelemetryValue(
  telemetry: RobotTelemetry,
  targetPath: string
): unknown {
  switch (targetPath) {
    case 'odometry.pose':
      return telemetry.odometry?.pose
    case 'odometry.pose.position':
      return telemetry.odometry?.pose?.position
    case 'odometry.twist':
      return telemetry.odometry?.twist
    case 'joint_state':
      if (telemetry.joint_state) {
        return {
          name: telemetry.joint_state.name,
          position: telemetry.joint_state.position,
          velocity: telemetry.joint_state.velocity,
          effort: telemetry.joint_state.effort,
        }
      }
      return null
    default:
      return null
  }
}

// Check if a goal parameter type is compatible with telemetry
export function findCompatibleTelemetryPath(goalType: string): string | null {
  for (const [path, types] of Object.entries(TELEMETRY_GOAL_MAPPING)) {
    if (types.includes(goalType)) {
      return path
    }
  }
  return null
}

// Sub-component: JointState display
function JointStateView({
  jointState,
  onCapture,
  onCapturePosition,
  onCaptureVelocity,
  onCaptureName,
}: {
  jointState: JointStateData
  onCapture?: () => void
  onCapturePosition?: () => void
  onCaptureVelocity?: () => void
  onCaptureName?: () => void
}) {
  const [copied, setCopied] = useState(false)

  const handleCopy = () => {
    const data = {
      name: jointState.name,
      position: jointState.position,
      velocity: jointState.velocity,
      effort: jointState.effort,
    }
    navigator.clipboard.writeText(JSON.stringify(data, null, 2))
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className="bg-[#16162a] rounded-lg p-3 border border-[#2a2a4a]">
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <span className="text-xs font-semibold text-cyan-400">JointState</span>
          <span className="text-[10px] text-gray-500">({jointState.name.length} joints)</span>
        </div>
        <div className="flex items-center gap-1">
          {onCapture && (
            <button
              onClick={onCapture}
              className="px-2 py-1 text-[10px] bg-purple-500/20 text-purple-400 rounded hover:bg-purple-500/30 transition-colors flex items-center gap-1"
              title="전체 JointState 캡처"
            >
              <Crosshair size={10} />
              전체
            </button>
          )}
          <button
            onClick={handleCopy}
            className="p-1 text-gray-500 hover:text-white rounded hover:bg-[#2a2a4a] transition-colors"
            title="JSON 복사"
          >
            {copied ? <Check size={12} className="text-green-400" /> : <Copy size={12} />}
          </button>
        </div>
      </div>
      {jointState.topic_name && (
        <div className="flex items-center gap-2 px-2 py-1 bg-[#0d0d1a] rounded mb-2">
          <span className="text-[10px] text-gray-500">TOPIC</span>
          <span className="text-xs text-cyan-400 font-mono">{jointState.topic_name}</span>
        </div>
      )}
      <div className="overflow-x-auto">
        {/* Column headers with capture buttons */}
        <div className="grid grid-cols-4 gap-2 text-[9px] text-gray-500 font-semibold uppercase mb-1 px-1">
          <div className="flex items-center gap-1">
            <span>Joint</span>
            {onCaptureName && (
              <button
                onClick={onCaptureName}
                className="p-0.5 text-purple-400 hover:bg-purple-500/20 rounded"
                title="joint names 배열 캡처"
              >
                <Crosshair size={8} />
              </button>
            )}
          </div>
          <div className="flex items-center gap-1">
            <span>Position</span>
            {onCapturePosition && (
              <button
                onClick={onCapturePosition}
                className="p-0.5 text-cyan-400 hover:bg-cyan-500/20 rounded"
                title="positions 배열 캡처"
              >
                <Crosshair size={8} />
              </button>
            )}
          </div>
          <div className="flex items-center gap-1">
            <span>Velocity</span>
            {onCaptureVelocity && (
              <button
                onClick={onCaptureVelocity}
                className="p-0.5 text-yellow-400 hover:bg-yellow-500/20 rounded"
                title="velocities 배열 캡처"
              >
                <Crosshair size={8} />
              </button>
            )}
          </div>
          <span>Effort</span>
        </div>
        <div className="space-y-0.5 max-h-48 overflow-y-auto">
          {jointState.name.map((name, idx) => (
            <div key={name} className="grid grid-cols-4 gap-2 text-xs font-mono px-1 py-0.5 hover:bg-[#1a1a2e] rounded">
              <span className="text-gray-300 truncate" title={name}>{name}</span>
              <span className="text-cyan-400">{formatNumber(jointState.position?.[idx] ?? 0)}</span>
              <span className="text-yellow-400">{formatNumber(jointState.velocity?.[idx] ?? 0)}</span>
              <span className="text-purple-400">{formatNumber(jointState.effort?.[idx] ?? 0)}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

// Sub-component: Odometry display
function OdometryView({
  odometry,
  onCapturePose,
  onCaptureTwist,
}: {
  odometry: OdometryData
  onCapturePose?: () => void
  onCaptureTwist?: () => void
}) {
  const [copied, setCopied] = useState(false)

  const handleCopy = () => {
    navigator.clipboard.writeText(JSON.stringify(odometry, null, 2))
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className="bg-[#16162a] rounded-lg p-3 border border-[#2a2a4a]">
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <span className="text-xs font-semibold text-green-400">Odometry</span>
          <span className="text-[10px] text-gray-500">{odometry.frame_id}</span>
        </div>
        <button
          onClick={handleCopy}
          className="p-1 text-gray-500 hover:text-white rounded hover:bg-[#2a2a4a] transition-colors"
          title="JSON 복사"
        >
          {copied ? <Check size={12} className="text-green-400" /> : <Copy size={12} />}
        </button>
      </div>
      {odometry.topic_name && (
        <div className="flex items-center gap-2 px-2 py-1 bg-[#0d0d1a] rounded mb-2">
          <span className="text-[10px] text-gray-500">TOPIC</span>
          <span className="text-xs text-green-400 font-mono">{odometry.topic_name}</span>
        </div>
      )}
      <div className="space-y-3">
        {/* Pose */}
        <div>
          <div className="flex items-center justify-between mb-1">
            <span className="text-[10px] text-gray-400 uppercase font-semibold">Pose</span>
            {onCapturePose && (
              <button
                onClick={onCapturePose}
                className="px-2 py-0.5 text-[9px] bg-purple-500/20 text-purple-400 rounded hover:bg-purple-500/30 transition-colors flex items-center gap-1"
              >
                <Crosshair size={8} />
                캡처
              </button>
            )}
          </div>
          <div className="grid grid-cols-3 gap-2 text-xs font-mono bg-[#0d0d1a] rounded p-2">
            <div>
              <span className="text-[9px] text-gray-500">X</span>
              <div className="text-cyan-400">{formatNumber(odometry.pose?.position?.x ?? 0)}</div>
            </div>
            <div>
              <span className="text-[9px] text-gray-500">Y</span>
              <div className="text-cyan-400">{formatNumber(odometry.pose?.position?.y ?? 0)}</div>
            </div>
            <div>
              <span className="text-[9px] text-gray-500">Z</span>
              <div className="text-cyan-400">{formatNumber(odometry.pose?.position?.z ?? 0)}</div>
            </div>
          </div>
          <div className="grid grid-cols-4 gap-2 text-xs font-mono bg-[#0d0d1a] rounded p-2 mt-1">
            <div>
              <span className="text-[9px] text-gray-500">QX</span>
              <div className="text-yellow-400">{formatNumber(odometry.pose?.orientation?.x ?? 0)}</div>
            </div>
            <div>
              <span className="text-[9px] text-gray-500">QY</span>
              <div className="text-yellow-400">{formatNumber(odometry.pose?.orientation?.y ?? 0)}</div>
            </div>
            <div>
              <span className="text-[9px] text-gray-500">QZ</span>
              <div className="text-yellow-400">{formatNumber(odometry.pose?.orientation?.z ?? 0)}</div>
            </div>
            <div>
              <span className="text-[9px] text-gray-500">QW</span>
              <div className="text-yellow-400">{formatNumber(odometry.pose?.orientation?.w ?? 0)}</div>
            </div>
          </div>
        </div>
        {/* Twist */}
        <div>
          <div className="flex items-center justify-between mb-1">
            <span className="text-[10px] text-gray-400 uppercase font-semibold">Twist</span>
            {onCaptureTwist && (
              <button
                onClick={onCaptureTwist}
                className="px-2 py-0.5 text-[9px] bg-purple-500/20 text-purple-400 rounded hover:bg-purple-500/30 transition-colors flex items-center gap-1"
              >
                <Crosshair size={8} />
                캡처
              </button>
            )}
          </div>
          <div className="grid grid-cols-2 gap-2 text-xs font-mono bg-[#0d0d1a] rounded p-2">
            <div>
              <span className="text-[9px] text-gray-500">Linear</span>
              <div className="text-green-400">
                {formatNumber(odometry.twist?.linear?.x ?? 0)}, {formatNumber(odometry.twist?.linear?.y ?? 0)}, {formatNumber(odometry.twist?.linear?.z ?? 0)}
              </div>
            </div>
            <div>
              <span className="text-[9px] text-gray-500">Angular</span>
              <div className="text-purple-400">
                {formatNumber(odometry.twist?.angular?.x ?? 0)}, {formatNumber(odometry.twist?.angular?.y ?? 0)}, {formatNumber(odometry.twist?.angular?.z ?? 0)}
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

export function TelemetryPanel({
  isOpen,
  onClose,
  selectedRobotId: externalRobotId,
  embedded = false,
  horizontal = false,
}: TelemetryPanelProps) {
  // Use telemetry context for capturing values and live telemetry sync
  const {
    setCapturedValue,
    capturedValue,
    selectedRobotId: contextRobotId,
    setSelectedRobotId: setContextRobotId,
    setLiveTelemetry,
  } = useTelemetry()

  // Use context robot ID, or external if provided
  const selectedRobotId = externalRobotId || contextRobotId

  const setSelectedRobotId = useCallback((robotId: string | null) => {
    setContextRobotId(robotId)
  }, [setContextRobotId])

  // Sync external robot selection
  useEffect(() => {
    if (externalRobotId && externalRobotId !== contextRobotId) {
      setContextRobotId(externalRobotId)
    }
  }, [externalRobotId, contextRobotId, setContextRobotId])

  // Fetch robots list
  const { data: robots = [] } = useQuery({
    queryKey: ['robots'],
    queryFn: () => robotApi.list(),
    enabled: isOpen,
  })

  // Fetch telemetry for selected robot
  const { data: telemetry, isLoading, refetch } = useQuery({
    queryKey: ['robot-telemetry', selectedRobotId],
    queryFn: () => telemetryApi.getRobotTelemetry(selectedRobotId!),
    enabled: isOpen && !!selectedRobotId,
    refetchInterval: isOpen && selectedRobotId ? 500 : false, // 500ms refresh
    refetchIntervalInBackground: false,
  })

  // Sync live telemetry to context for use in parameter editors
  useEffect(() => {
    if (telemetry) {
      setLiveTelemetry(telemetry)
    } else if (!selectedRobotId) {
      setLiveTelemetry(null)
    }
  }, [telemetry, selectedRobotId, setLiveTelemetry])

  const handleCaptureJointState = useCallback(() => {
    if (telemetry?.joint_state) {
      setCapturedValue({
        type: 'joint_state',
        value: extractTelemetryValue(telemetry, 'joint_state'),
        capturedAt: new Date(),
        robotId: selectedRobotId || undefined,
      })
    }
  }, [setCapturedValue, telemetry, selectedRobotId])

  // Individual array captures from JointState
  const handleCapturePosition = useCallback(() => {
    if (telemetry?.joint_state?.position) {
      setCapturedValue({
        type: 'joint_state.position',
        value: telemetry.joint_state.position,
        capturedAt: new Date(),
        robotId: selectedRobotId || undefined,
      })
    }
  }, [setCapturedValue, telemetry, selectedRobotId])

  const handleCaptureVelocity = useCallback(() => {
    if (telemetry?.joint_state?.velocity) {
      setCapturedValue({
        type: 'joint_state.velocity',
        value: telemetry.joint_state.velocity,
        capturedAt: new Date(),
        robotId: selectedRobotId || undefined,
      })
    }
  }, [setCapturedValue, telemetry, selectedRobotId])

  const handleCaptureJointNames = useCallback(() => {
    if (telemetry?.joint_state?.name) {
      setCapturedValue({
        type: 'joint_state.name',
        value: telemetry.joint_state.name,
        capturedAt: new Date(),
        robotId: selectedRobotId || undefined,
      })
    }
  }, [setCapturedValue, telemetry, selectedRobotId])

  const handleCapturePose = useCallback(() => {
    if (telemetry?.odometry?.pose) {
      setCapturedValue({
        type: 'odometry.pose',
        value: extractTelemetryValue(telemetry, 'odometry.pose'),
        capturedAt: new Date(),
        robotId: selectedRobotId || undefined,
      })
    }
  }, [setCapturedValue, telemetry, selectedRobotId])

  const handleCaptureTwist = useCallback(() => {
    if (telemetry?.odometry?.twist) {
      setCapturedValue({
        type: 'odometry.twist',
        value: extractTelemetryValue(telemetry, 'odometry.twist'),
        capturedAt: new Date(),
        robotId: selectedRobotId || undefined,
      })
    }
  }, [setCapturedValue, telemetry, selectedRobotId])

  if (!isOpen) return null

  const hasData = telemetry && (telemetry.joint_state || telemetry.odometry || (telemetry.transforms && telemetry.transforms.length > 0))

  // Horizontal layout content for bottom panel
  if (horizontal) {
    return (
      <div className="flex h-full">
        {/* Robot Selector - Left */}
        <div className="w-40 flex-shrink-0 border-r border-[#2a2a4a] p-2">
          <div className="text-[10px] text-gray-500 uppercase mb-1.5">로봇</div>
          <select
            value={selectedRobotId || ''}
            onChange={(e) => setSelectedRobotId(e.target.value || null)}
            className="w-full px-2 py-1 bg-[#1a1a2e] border border-[#2a2a4a] rounded text-xs text-white focus:outline-none focus:border-green-500 cursor-pointer"
          >
            <option value="">선택...</option>
            {robots.map((robot) => (
              <option key={robot.id} value={robot.id}>
                {robot.name || robot.id}
              </option>
            ))}
          </select>
          {/* Capture Status */}
          {capturedValue && (
            <div className="mt-2 p-1.5 bg-green-500/10 rounded">
              <div className="flex items-center gap-1 text-[9px] text-green-400">
                <Check size={10} />
                <span className="truncate">{capturedValue.type}</span>
              </div>
              <button
                onClick={() => setCapturedValue(null)}
                className="text-[8px] text-gray-500 hover:text-gray-300 mt-0.5"
              >
                취소
              </button>
            </div>
          )}
        </div>

        {/* Telemetry Content - Center (scrollable horizontally) */}
        <div className="flex-1 flex gap-3 p-2 overflow-x-auto">
          {!selectedRobotId ? (
            <div className="flex items-center justify-center w-full text-gray-500 text-xs">
              <Radio size={16} className="mr-2" />
              로봇을 선택하세요
            </div>
          ) : isLoading ? (
            <div className="flex items-center justify-center w-full">
              <RefreshCw size={16} className="text-gray-500 animate-spin" />
            </div>
          ) : !hasData ? (
            <div className="flex items-center justify-center w-full text-gray-500 text-xs">
              <Radio size={16} className="mr-2" />
              텔레메트리 없음
            </div>
          ) : (
            <>
              {/* Joint State - Compact horizontal */}
              {telemetry.joint_state && (
                <div className="flex-shrink-0 w-80 bg-[#1a1a2e] rounded border border-[#2a2a4a] p-2">
                  <div className="flex items-center justify-between mb-1.5">
                    <span className="text-[10px] font-semibold text-cyan-400">JointState</span>
                    <div className="flex items-center gap-1">
                      <button
                        onClick={handleCaptureJointState}
                        className="px-1.5 py-0.5 text-[9px] bg-purple-500/20 text-purple-400 rounded hover:bg-purple-500/30 flex items-center gap-0.5"
                      >
                        <Crosshair size={8} />
                        캡처
                      </button>
                    </div>
                  </div>
                  <div className="max-h-28 overflow-y-auto">
                    <div className="grid grid-cols-4 gap-1 text-[8px] text-gray-500 font-semibold px-1 mb-0.5">
                      <span>Joint</span>
                      <span>Pos</span>
                      <span>Vel</span>
                      <span>Eff</span>
                    </div>
                    {telemetry.joint_state.name.slice(0, 6).map((name, idx) => (
                      <div key={name} className="grid grid-cols-4 gap-1 text-[9px] font-mono px-1 py-0.5 hover:bg-[#0d0d1a] rounded">
                        <span className="text-gray-300 truncate" title={name}>{name}</span>
                        <span className="text-cyan-400">{formatNumber(telemetry.joint_state!.position?.[idx] ?? 0)}</span>
                        <span className="text-yellow-400">{formatNumber(telemetry.joint_state!.velocity?.[idx] ?? 0)}</span>
                        <span className="text-purple-400">{formatNumber(telemetry.joint_state!.effort?.[idx] ?? 0)}</span>
                      </div>
                    ))}
                    {telemetry.joint_state.name.length > 6 && (
                      <div className="text-[8px] text-gray-500 px-1">+{telemetry.joint_state.name.length - 6} more</div>
                    )}
                  </div>
                </div>
              )}

              {/* Odometry - Compact horizontal */}
              {telemetry.odometry && (
                <div className="flex-shrink-0 w-56 bg-[#1a1a2e] rounded border border-[#2a2a4a] p-2">
                  <div className="flex items-center justify-between mb-1.5">
                    <span className="text-[10px] font-semibold text-green-400">Odometry</span>
                    <button
                      onClick={handleCapturePose}
                      className="px-1.5 py-0.5 text-[9px] bg-purple-500/20 text-purple-400 rounded hover:bg-purple-500/30 flex items-center gap-0.5"
                    >
                      <Crosshair size={8} />
                      Pose
                    </button>
                  </div>
                  <div className="space-y-1.5">
                    <div>
                      <div className="text-[8px] text-gray-500 mb-0.5">Position</div>
                      <div className="grid grid-cols-3 gap-1 text-[9px] font-mono bg-[#0d0d1a] rounded p-1">
                        <div>
                          <span className="text-[7px] text-gray-500">X</span>
                          <div className="text-cyan-400">{formatNumber(telemetry.odometry.pose?.position?.x ?? 0)}</div>
                        </div>
                        <div>
                          <span className="text-[7px] text-gray-500">Y</span>
                          <div className="text-cyan-400">{formatNumber(telemetry.odometry.pose?.position?.y ?? 0)}</div>
                        </div>
                        <div>
                          <span className="text-[7px] text-gray-500">Z</span>
                          <div className="text-cyan-400">{formatNumber(telemetry.odometry.pose?.position?.z ?? 0)}</div>
                        </div>
                      </div>
                    </div>
                    <div>
                      <div className="text-[8px] text-gray-500 mb-0.5">Orientation</div>
                      <div className="grid grid-cols-4 gap-0.5 text-[8px] font-mono bg-[#0d0d1a] rounded p-1">
                        <div className="text-yellow-400">{formatNumber(telemetry.odometry.pose?.orientation?.x ?? 0)}</div>
                        <div className="text-yellow-400">{formatNumber(telemetry.odometry.pose?.orientation?.y ?? 0)}</div>
                        <div className="text-yellow-400">{formatNumber(telemetry.odometry.pose?.orientation?.z ?? 0)}</div>
                        <div className="text-yellow-400">{formatNumber(telemetry.odometry.pose?.orientation?.w ?? 0)}</div>
                      </div>
                    </div>
                  </div>
                </div>
              )}

              {/* Transforms - Compact horizontal */}
              {telemetry.transforms && telemetry.transforms.length > 0 && (
                <div className="flex-shrink-0 w-48 bg-[#1a1a2e] rounded border border-[#2a2a4a] p-2">
                  <div className="flex items-center gap-1 mb-1.5">
                    <span className="text-[10px] font-semibold text-orange-400">TF</span>
                    <span className="text-[8px] text-gray-500">({telemetry.transforms.length})</span>
                  </div>
                  <div className="max-h-28 overflow-y-auto space-y-0.5">
                    {telemetry.transforms.slice(0, 4).map((tf, idx) => (
                      <div key={idx} className="text-[8px] font-mono p-1 bg-[#0d0d1a] rounded truncate">
                        <span className="text-orange-400">{tf.frame_id}</span>
                        <span className="text-gray-500"> → </span>
                        <span className="text-yellow-400">{tf.child_frame_id}</span>
                      </div>
                    ))}
                    {telemetry.transforms.length > 4 && (
                      <div className="text-[8px] text-gray-500 px-1">+{telemetry.transforms.length - 4} more</div>
                    )}
                  </div>
                </div>
              )}
            </>
          )}
        </div>

        {/* Status - Right */}
        <div className="w-32 flex-shrink-0 border-l border-[#2a2a4a] p-2 flex flex-col justify-between">
          {hasData && telemetry && (
            <div className="text-[9px]">
              <div className="text-gray-500 mb-0.5">업데이트</div>
              <div className={telemetry.is_stale ? 'text-yellow-500' : 'text-green-400'}>
                {new Date(telemetry.updated_at).toLocaleTimeString()}
              </div>
            </div>
          )}
          <div className="text-[8px] text-gray-600">
            <Crosshair size={10} className="inline mr-1 text-purple-400" />
            캡처 후 파라미터 적용
          </div>
        </div>
      </div>
    )
  }

  // Vertical layout content (default)
  const content = (
    <>
      {/* Robot Selector */}
      <div className="px-3 py-2 border-b border-[#2a2a4a]">
        <select
          value={selectedRobotId || ''}
          onChange={(e) => setSelectedRobotId(e.target.value || null)}
          className="w-full px-2 py-1.5 bg-[#1a1a2e] border border-[#2a2a4a] rounded-lg text-xs text-white focus:outline-none focus:border-green-500 cursor-pointer"
        >
          <option value="">로봇 선택...</option>
          {robots.map((robot) => (
            <option key={robot.id} value={robot.id}>
              {robot.name || robot.id}
            </option>
          ))}
        </select>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto p-3 space-y-3">
        {!selectedRobotId ? (
          <div className="text-center py-8">
            <Radio size={32} className="mx-auto text-gray-600 mb-3" />
            <p className="text-xs text-gray-500">모니터링할 로봇을 선택하세요</p>
          </div>
        ) : isLoading ? (
          <div className="text-center py-8">
            <RefreshCw size={24} className="mx-auto text-gray-500 animate-spin mb-3" />
            <p className="text-xs text-gray-500">데이터 로딩 중...</p>
          </div>
        ) : !hasData ? (
          <div className="text-center py-8">
            <Radio size={32} className="mx-auto text-gray-600 mb-3" />
            <p className="text-xs text-gray-500">수신된 텔레메트리 없음</p>
            <p className="text-[10px] text-gray-600 mt-1">로봇이 데이터를 전송하는지 확인하세요</p>
          </div>
        ) : (
          <>
            {/* Updated timestamp */}
            <div className="flex items-center justify-between text-[10px] text-gray-500">
              <span>최근 업데이트</span>
              <span className={telemetry.is_stale ? 'text-yellow-500' : 'text-green-400'}>
                {new Date(telemetry.updated_at).toLocaleTimeString()}
                {telemetry.is_stale && ' (stale)'}
              </span>
            </div>

            {/* Joint State */}
            {telemetry.joint_state && (
              <JointStateView
                jointState={telemetry.joint_state}
                onCapture={handleCaptureJointState}
                onCapturePosition={handleCapturePosition}
                onCaptureVelocity={handleCaptureVelocity}
                onCaptureName={handleCaptureJointNames}
              />
            )}

            {/* Odometry */}
            {telemetry.odometry && (
              <OdometryView
                odometry={telemetry.odometry}
                onCapturePose={handleCapturePose}
                onCaptureTwist={handleCaptureTwist}
              />
            )}

            {/* Transforms */}
            {telemetry.transforms && telemetry.transforms.length > 0 && (
              <div className="bg-[#16162a] rounded-lg p-3 border border-[#2a2a4a]">
                <div className="flex items-center gap-2 mb-2">
                  <span className="text-xs font-semibold text-orange-400">Transforms</span>
                  <span className="text-[10px] text-gray-500">({telemetry.transforms.length})</span>
                </div>
                <div className="space-y-1 max-h-32 overflow-y-auto">
                  {telemetry.transforms.map((tf, idx) => (
                    <div key={idx} className="text-[10px] font-mono p-1.5 bg-[#0d0d1a] rounded">
                      <span className="text-orange-400">{tf.frame_id}</span>
                      <span className="text-gray-500"> → </span>
                      <span className="text-yellow-400">{tf.child_frame_id}</span>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </>
        )}
      </div>

      {/* Capture Mode Info */}
      <div className="px-3 py-2 border-t border-[#2a2a4a] bg-purple-500/5">
        {capturedValue ? (
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2 text-[10px] text-green-400">
              <Check size={12} />
              <span>
                {capturedValue.type} 캡처됨 -
                <span className="text-gray-400 ml-1">
                  {capturedValue.capturedAt.toLocaleTimeString()}
                </span>
              </span>
            </div>
            <button
              onClick={() => setCapturedValue(null)}
              className="text-[9px] text-gray-500 hover:text-gray-300"
            >
              취소
            </button>
          </div>
        ) : (
          <div className="flex items-center gap-2 text-[10px] text-purple-400">
            <Crosshair size={12} />
            <span>캡처 버튼을 눌러 파라미터에 적용할 값을 저장하세요</span>
          </div>
        )}
      </div>

      {/* Type Compatibility Info */}
      <div className="px-3 py-2 border-t border-[#2a2a4a] bg-[#0d0d1a]">
        <div className="text-[9px] text-gray-500 space-y-0.5">
          <div className="font-semibold text-gray-400 mb-1">호환 타입:</div>
          <div>• <span className="text-cyan-400">Position</span> → array, float64[]</div>
          <div>• <span className="text-yellow-400">Velocity</span> → array, float64[]</div>
          <div>• <span className="text-purple-400">JointState</span> → JointState, JointTrajectoryPoint</div>
          <div>• <span className="text-green-400">Pose</span> → Pose, PoseStamped</div>
        </div>
      </div>
    </>
  )

  // In embedded mode, just return the content without the outer container
  if (embedded) {
    return <div className="flex flex-col h-full">{content}</div>
  }

  // Standalone mode with full container
  return (
    <div className="w-80 bg-[#16162a] border-l border-[#2a2a4a] flex flex-col h-full">
      {/* Header */}
      <div className="px-3 py-2 border-b border-[#2a2a4a] flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Radio size={14} className="text-green-400 animate-pulse" />
          <span className="text-xs font-semibold text-white">Telemetry Monitor</span>
        </div>
        <div className="flex items-center gap-1">
          <button
            onClick={() => refetch()}
            className="p-1 text-gray-500 hover:text-white rounded hover:bg-[#2a2a4a] transition-colors"
            title="새로고침"
          >
            <RefreshCw size={12} />
          </button>
          <button
            onClick={onClose}
            className="p-1 text-gray-500 hover:text-white rounded hover:bg-[#2a2a4a] transition-colors"
          >
            <X size={14} />
          </button>
        </div>
      </div>
      {content}
    </div>
  )
}

export default TelemetryPanel
