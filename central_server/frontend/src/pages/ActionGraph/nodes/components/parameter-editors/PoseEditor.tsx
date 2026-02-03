import { memo, useState, useCallback, useMemo } from 'react'
import { Crosshair, Edit3, Code } from 'lucide-react'
import TelemetryPreview from './TelemetryPreview'
import {
  type BaseEditorProps,
  type EditorMode,
  type PoseValue,
  type PoseStampedValue,
  quaternionToEuler,
  eulerToQuaternion,
  formatNumber,
} from './types'

interface PoseEditorProps extends BaseEditorProps {
  isStamped?: boolean  // PoseStamped vs Pose
  isPoint?: boolean    // Point/PointStamped (no orientation)
}

const PoseEditor = memo(({
  fieldName: _fieldName,
  fieldType: _fieldType,
  value,
  onChange,
  robotTelemetry,
  isStamped = false,
  isPoint = false,
}: PoseEditorProps) => {
  // _fieldName and _fieldType kept for interface consistency
  const [mode, setMode] = useState<EditorMode>('telemetry')

  // Extract pose value from stamped or regular pose
  const poseValue = useMemo((): PoseValue | null => {
    if (!value) return null
    if (isStamped) {
      const stamped = value as PoseStampedValue
      return stamped.pose || null
    }
    return value as PoseValue
  }, [value, isStamped])

  // Get live telemetry pose
  const livePose = useMemo((): PoseValue | null => {
    if (!robotTelemetry?.odometry?.pose) return null
    return robotTelemetry.odometry.pose
  }, [robotTelemetry])

  // Get frame_id for stamped types
  const frameId = useMemo(() => {
    if (!isStamped) return null
    const stamped = value as PoseStampedValue | null
    return stamped?.header?.frame_id || 'map'
  }, [value, isStamped])

  // Capture current telemetry value
  const handleCapture = useCallback(() => {
    if (!livePose) return

    if (isStamped) {
      const newValue: PoseStampedValue = {
        header: {
          stamp: { sec: 0, nanosec: 0 },
          frame_id: frameId || 'map',
        },
        pose: isPoint
          ? { position: livePose.position, orientation: { x: 0, y: 0, z: 0, w: 1 } }
          : livePose,
      }
      onChange(newValue)
    } else {
      onChange(isPoint
        ? { position: livePose.position, orientation: { x: 0, y: 0, z: 0, w: 1 } }
        : livePose
      )
    }
  }, [livePose, isStamped, isPoint, frameId, onChange])

  // Update position field
  const updatePosition = useCallback((axis: 'x' | 'y' | 'z', val: number) => {
    const currentPose = poseValue || {
      position: { x: 0, y: 0, z: 0 },
      orientation: { x: 0, y: 0, z: 0, w: 1 },
    }

    const newPose: PoseValue = {
      ...currentPose,
      position: { ...currentPose.position, [axis]: val },
    }

    if (isStamped) {
      const stamped = value as PoseStampedValue | null
      onChange({
        header: stamped?.header || { frame_id: 'map' },
        pose: newPose,
      })
    } else {
      onChange(newPose)
    }
  }, [poseValue, value, isStamped, onChange])

  // Update orientation (from euler angles in degrees)
  const updateOrientation = useCallback((eulerAxis: 'roll' | 'pitch' | 'yaw', degrees: number) => {
    const currentPose = poseValue || {
      position: { x: 0, y: 0, z: 0 },
      orientation: { x: 0, y: 0, z: 0, w: 1 },
    }

    const currentEuler = quaternionToEuler(currentPose.orientation)
    const newEuler = { ...currentEuler, [eulerAxis]: degrees }
    const newQuaternion = eulerToQuaternion(newEuler)

    const newPose: PoseValue = {
      ...currentPose,
      orientation: newQuaternion,
    }

    if (isStamped) {
      const stamped = value as PoseStampedValue | null
      onChange({
        header: stamped?.header || { frame_id: 'map' },
        pose: newPose,
      })
    } else {
      onChange(newPose)
    }
  }, [poseValue, value, isStamped, onChange])

  // Update frame_id
  const updateFrameId = useCallback((newFrameId: string) => {
    if (!isStamped) return
    const stamped = value as PoseStampedValue | null
    onChange({
      header: { ...stamped?.header, frame_id: newFrameId },
      pose: stamped?.pose || { position: { x: 0, y: 0, z: 0 }, orientation: { x: 0, y: 0, z: 0, w: 1 } },
    })
  }, [value, isStamped, onChange])

  // Update from JSON
  const handleJsonChange = useCallback((jsonStr: string) => {
    try {
      const parsed = JSON.parse(jsonStr)
      onChange(parsed)
    } catch {
      // Invalid JSON, ignore
    }
  }, [onChange])

  const currentEuler = poseValue?.orientation ? quaternionToEuler(poseValue.orientation) : { roll: 0, pitch: 0, yaw: 0 }

  return (
    <div className="space-y-2">
      {/* Mode tabs */}
      <div className="flex gap-1 p-0.5 bg-gray-800/50 rounded">
        <ModeButton icon={Crosshair} label="Telemetry" mode="telemetry" currentMode={mode} onClick={setMode} />
        <ModeButton icon={Edit3} label="Manual" mode="manual" currentMode={mode} onClick={setMode} />
        <ModeButton icon={Code} label="JSON" mode="json" currentMode={mode} onClick={setMode} />
      </div>

      {/* Telemetry mode */}
      {mode === 'telemetry' && (
        <div className="space-y-2">
          {/* Live preview */}
          <TelemetryPreview
            type={isPoint ? 'point' : 'pose'}
            liveValue={livePose}
            savedValue={poseValue || undefined}
          />

          {/* Capture button */}
          {livePose && (
            <button
              onClick={(e) => { e.stopPropagation(); handleCapture() }}
              className="w-full py-2 bg-purple-500/20 hover:bg-purple-500/30 text-purple-400 rounded border border-purple-500/30 text-[11px] font-medium flex items-center justify-center gap-2 transition-colors"
            >
              <Crosshair size={14} />
              현재 위치 캡처
            </button>
          )}

          {/* Saved value display */}
          {poseValue && (
            <div className="p-2 bg-gray-800/50 rounded border border-primary/50">
              <div className="text-[8px] text-muted mb-1">저장된 값</div>
              <div className="grid grid-cols-4 gap-2 text-[10px] font-mono">
                <div>
                  <span className="text-muted">x: </span>
                  <span className="text-amber-400">{formatNumber(poseValue.position.x)}</span>
                </div>
                <div>
                  <span className="text-muted">y: </span>
                  <span className="text-amber-400">{formatNumber(poseValue.position.y)}</span>
                </div>
                <div>
                  <span className="text-muted">z: </span>
                  <span className="text-amber-400">{formatNumber(poseValue.position.z)}</span>
                </div>
                {!isPoint && (
                  <div>
                    <span className="text-muted">yaw: </span>
                    <span className="text-amber-400">{formatNumber(currentEuler.yaw, 1)}°</span>
                  </div>
                )}
              </div>
            </div>
          )}
        </div>
      )}

      {/* Manual mode */}
      {mode === 'manual' && (
        <div className="space-y-2">
          {/* Frame ID for stamped types */}
          {isStamped && (
            <div className="flex items-center gap-2">
              <span className="text-[9px] text-muted w-16">frame_id</span>
              <input
                type="text"
                value={frameId || ''}
                onChange={(e) => { e.stopPropagation(); updateFrameId(e.target.value) }}
                onClick={(e) => e.stopPropagation()}
                className="flex-1 px-2 py-1 bg-sunken border border-primary rounded text-[10px] text-primary focus:outline-none focus:border-amber-500"
                placeholder="map"
              />
            </div>
          )}

          {/* Position inputs */}
          <div>
            <div className="text-[9px] text-muted mb-1">Position</div>
            <div className="grid grid-cols-3 gap-2">
              {(['x', 'y', 'z'] as const).map((axis) => (
                <div key={axis}>
                  <label className="text-[8px] text-muted uppercase">{axis}</label>
                  <input
                    type="number"
                    value={poseValue?.position[axis] ?? 0}
                    onChange={(e) => { e.stopPropagation(); updatePosition(axis, parseFloat(e.target.value) || 0) }}
                    onClick={(e) => e.stopPropagation()}
                    className="w-full px-2 py-1 bg-sunken border border-primary rounded text-[10px] text-primary focus:outline-none focus:border-amber-500 font-mono"
                    step="0.01"
                  />
                </div>
              ))}
            </div>
          </div>

          {/* Orientation inputs (not for point types) */}
          {!isPoint && (
            <div>
              <div className="text-[9px] text-muted mb-1">Orientation (degrees)</div>
              <div className="grid grid-cols-3 gap-2">
                {(['roll', 'pitch', 'yaw'] as const).map((axis) => (
                  <div key={axis}>
                    <label className="text-[8px] text-muted uppercase">{axis}</label>
                    <input
                      type="number"
                      value={formatNumber(currentEuler[axis], 1)}
                      onChange={(e) => { e.stopPropagation(); updateOrientation(axis, parseFloat(e.target.value) || 0) }}
                      onClick={(e) => e.stopPropagation()}
                      className="w-full px-2 py-1 bg-sunken border border-primary rounded text-[10px] text-primary focus:outline-none focus:border-amber-500 font-mono"
                      step="1"
                    />
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      {/* JSON mode */}
      {mode === 'json' && (
        <div>
          <textarea
            value={JSON.stringify(value || (isStamped
              ? { header: { frame_id: 'map' }, pose: { position: { x: 0, y: 0, z: 0 }, orientation: { x: 0, y: 0, z: 0, w: 1 } } }
              : { position: { x: 0, y: 0, z: 0 }, orientation: { x: 0, y: 0, z: 0, w: 1 } }
            ), null, 2)}
            onChange={(e) => { e.stopPropagation(); handleJsonChange(e.target.value) }}
            onClick={(e) => e.stopPropagation()}
            className="w-full px-2 py-1.5 bg-sunken border border-primary rounded text-[9px] text-secondary font-mono focus:outline-none focus:border-amber-500 resize-none"
            rows={isStamped ? 10 : 7}
          />
        </div>
      )}
    </div>
  )
})

// Mode button component
const ModeButton = memo(({ icon: Icon, label, mode, currentMode, onClick }: {
  icon: typeof Crosshair
  label: string
  mode: EditorMode
  currentMode: EditorMode
  onClick: (mode: EditorMode) => void
}) => (
  <button
    onClick={(e) => { e.stopPropagation(); onClick(mode) }}
    className={`flex-1 py-1 px-2 rounded text-[9px] flex items-center justify-center gap-1 transition-colors ${
      currentMode === mode
        ? 'bg-purple-500/30 text-purple-400'
        : 'text-muted hover:text-secondary hover:bg-gray-700/50'
    }`}
  >
    <Icon size={10} />
    {label}
  </button>
))

PoseEditor.displayName = 'PoseEditor'
ModeButton.displayName = 'ModeButton'

export default PoseEditor
