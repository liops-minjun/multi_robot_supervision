import { memo } from 'react'
import { Radio, ArrowUp, ArrowDown, RotateCw } from 'lucide-react'
import { formatNumber, calcDiff, quaternionToEuler } from './types'
import type { PoseValue, TwistValue } from './types'

interface TelemetryPreviewProps {
  type: 'pose' | 'point' | 'twist' | 'joint_array' | 'quaternion'
  liveValue: unknown
  savedValue?: unknown
  compact?: boolean
}

const TelemetryPreview = memo(({ type, liveValue, savedValue, compact = false }: TelemetryPreviewProps) => {
  if (!liveValue) {
    return (
      <div className="flex items-center gap-2 px-2 py-1.5 bg-gray-800/50 rounded border border-gray-700/50">
        <Radio className="w-3 h-3 text-muted" />
        <span className="text-[9px] text-muted">Telemetry 없음</span>
      </div>
    )
  }

  if (type === 'pose' || type === 'point') {
    return <PosePreview liveValue={liveValue as PoseValue} savedValue={savedValue as PoseValue | undefined} compact={compact} isPoint={type === 'point'} />
  }

  if (type === 'twist') {
    return <TwistPreview liveValue={liveValue as TwistValue} compact={compact} />
  }

  if (type === 'joint_array') {
    return <JointArrayPreview liveValue={liveValue as number[]} savedValue={savedValue as number[] | undefined} compact={compact} />
  }

  if (type === 'quaternion') {
    const q = liveValue as { x: number; y: number; z: number; w: number }
    const euler = quaternionToEuler(q)
    return (
      <div className="px-2 py-1.5 bg-purple-500/10 rounded border border-purple-500/30">
        <div className="flex items-center gap-2 mb-1">
          <Radio className="w-3 h-3 text-purple-400 animate-pulse" />
          <span className="text-[9px] text-purple-400 font-medium">LIVE</span>
        </div>
        <div className="flex gap-3 text-[10px] font-mono">
          <span className="text-secondary">R: <span className="text-primary">{formatNumber(euler.roll, 1)}°</span></span>
          <span className="text-secondary">P: <span className="text-primary">{formatNumber(euler.pitch, 1)}°</span></span>
          <span className="text-secondary">Y: <span className="text-primary">{formatNumber(euler.yaw, 1)}°</span></span>
        </div>
      </div>
    )
  }

  return null
})

// Position type (can be either from Pose or direct Point)
type PositionLike = { x: number; y: number; z: number }
type PoseLike = PoseValue | PositionLike

// Pose/Point Preview
const PosePreview = memo(({ liveValue, savedValue, compact, isPoint }: {
  liveValue: PoseLike
  savedValue?: PoseLike
  compact: boolean
  isPoint: boolean
}) => {
  // Handle both Pose (with position property) and direct Point
  const pos: PositionLike = 'position' in liveValue ? liveValue.position : liveValue
  const savedPos: PositionLike | undefined = savedValue
    ? ('position' in savedValue ? savedValue.position : savedValue)
    : undefined

  // Calculate diffs
  const xDiff = savedPos ? calcDiff(pos.x, savedPos.x) : null
  const yDiff = savedPos ? calcDiff(pos.y, savedPos.y) : null
  const zDiff = savedPos ? calcDiff(pos.z, savedPos.z) : null

  // Orientation (for pose, not point)
  let rollDiff: ReturnType<typeof calcDiff> | null = null
  let pitchDiff: ReturnType<typeof calcDiff> | null = null
  let yawDiff: ReturnType<typeof calcDiff> | null = null
  let liveEuler = { roll: 0, pitch: 0, yaw: 0 }
  const liveOrientation = 'orientation' in liveValue ? liveValue.orientation : undefined
  const savedOrientation = savedValue && 'orientation' in savedValue ? savedValue.orientation : undefined
  if (!isPoint && liveOrientation) {
    liveEuler = quaternionToEuler(liveOrientation)
    if (savedOrientation) {
      const savedEuler = quaternionToEuler(savedOrientation)
      rollDiff = calcDiff(liveEuler.roll, savedEuler.roll)
      pitchDiff = calcDiff(liveEuler.pitch, savedEuler.pitch)
      yawDiff = calcDiff(liveEuler.yaw, savedEuler.yaw)
    }
  }

  if (compact) {
    return (
      <div className="flex items-start gap-2 px-2 py-1 bg-purple-500/10 rounded border border-purple-500/30">
        <Radio className="w-3 h-3 text-purple-400 animate-pulse" />
        <div className="text-[9px] text-secondary font-mono leading-4">
          <div>x={formatNumber(pos.x)} y={formatNumber(pos.y)} z={formatNumber(pos.z)}</div>
          {!isPoint && (
            <div>r={formatNumber(liveEuler.roll, 1)}° p={formatNumber(liveEuler.pitch, 1)}° y={formatNumber(liveEuler.yaw, 1)}°</div>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className="px-2 py-1.5 bg-purple-500/10 rounded border border-purple-500/30">
      <div className="flex items-center gap-2 mb-1.5">
        <Radio className="w-3 h-3 text-purple-400 animate-pulse" />
        <span className="text-[9px] text-purple-400 font-medium">LIVE</span>
        {savedValue && <span className="text-[8px] text-muted ml-auto">차이 표시됨</span>}
      </div>

      <div className="space-y-1.5 text-[10px] font-mono">
        <div className="grid grid-cols-3 gap-2">
          <div>
            <div className="text-muted text-[8px]">X</div>
            <div className="text-primary">{formatNumber(pos.x)}</div>
            {xDiff && xDiff.direction !== 'same' && (
              <DiffIndicator diff={xDiff} />
            )}
          </div>
          <div>
            <div className="text-muted text-[8px]">Y</div>
            <div className="text-primary">{formatNumber(pos.y)}</div>
            {yDiff && yDiff.direction !== 'same' && (
              <DiffIndicator diff={yDiff} />
            )}
          </div>
          <div>
            <div className="text-muted text-[8px]">Z</div>
            <div className="text-primary">{formatNumber(pos.z)}</div>
            {zDiff && zDiff.direction !== 'same' && (
              <DiffIndicator diff={zDiff} />
            )}
          </div>
        </div>

        {!isPoint && (
          <div className="grid grid-cols-3 gap-2">
            <div>
              <div className="text-muted text-[8px]">Roll</div>
              <div className="text-primary">{formatNumber(liveEuler.roll, 1)}°</div>
              {rollDiff && rollDiff.direction !== 'same' && (
                <DiffIndicator diff={rollDiff} isAngle />
              )}
            </div>
            <div>
              <div className="text-muted text-[8px]">Pitch</div>
              <div className="text-primary">{formatNumber(liveEuler.pitch, 1)}°</div>
              {pitchDiff && pitchDiff.direction !== 'same' && (
                <DiffIndicator diff={pitchDiff} isAngle />
              )}
            </div>
            <div>
              <div className="text-muted text-[8px]">Yaw</div>
              <div className="text-primary">{formatNumber(liveEuler.yaw, 1)}°</div>
              {yawDiff && yawDiff.direction !== 'same' && (
                <DiffIndicator diff={yawDiff} isAngle />
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  )
})

// Twist Preview
const TwistPreview = memo(({ liveValue, compact }: {
  liveValue: TwistValue
  compact: boolean
}) => {
  if (compact) {
    return (
      <div className="flex items-center gap-2 px-2 py-1 bg-purple-500/10 rounded border border-purple-500/30">
        <Radio className="w-3 h-3 text-purple-400 animate-pulse" />
        <span className="text-[9px] text-secondary font-mono">
          lin={formatNumber(liveValue.linear.x)} ang={formatNumber(liveValue.angular.z)}
        </span>
      </div>
    )
  }

  return (
    <div className="px-2 py-1.5 bg-purple-500/10 rounded border border-purple-500/30">
      <div className="flex items-center gap-2 mb-1.5">
        <Radio className="w-3 h-3 text-purple-400 animate-pulse" />
        <span className="text-[9px] text-purple-400 font-medium">LIVE</span>
      </div>

      <div className="space-y-1 text-[10px] font-mono">
        <div className="flex gap-3">
          <span className="text-muted w-12">Linear</span>
          <span className="text-secondary">x: <span className="text-primary">{formatNumber(liveValue.linear.x)}</span></span>
          <span className="text-secondary">y: <span className="text-primary">{formatNumber(liveValue.linear.y)}</span></span>
          <span className="text-secondary">z: <span className="text-primary">{formatNumber(liveValue.linear.z)}</span></span>
        </div>
        <div className="flex gap-3">
          <span className="text-muted w-12">Angular</span>
          <span className="text-secondary">x: <span className="text-primary">{formatNumber(liveValue.angular.x)}</span></span>
          <span className="text-secondary">y: <span className="text-primary">{formatNumber(liveValue.angular.y)}</span></span>
          <span className="text-secondary">z: <span className="text-primary">{formatNumber(liveValue.angular.z)}</span></span>
        </div>
      </div>
    </div>
  )
})

// Joint Array Preview with bar visualization
const JointArrayPreview = memo(({ liveValue, savedValue, compact }: {
  liveValue: number[]
  savedValue?: number[]
  compact: boolean
}) => {
  if (compact) {
    return (
      <div className="flex items-center gap-2 px-2 py-1 bg-purple-500/10 rounded border border-purple-500/30">
        <Radio className="w-3 h-3 text-purple-400 animate-pulse" />
        <span className="text-[9px] text-secondary font-mono">
          [{liveValue.slice(0, 3).map(v => formatNumber(v, 2)).join(', ')}{liveValue.length > 3 ? '...' : ''}]
        </span>
      </div>
    )
  }

  // For joint visualization, normalize values to -π to π range
  const normalizeJoint = (value: number) => {
    // Assume joint values are in radians, normalize to -1 to 1 for display
    const normalized = value / Math.PI
    return Math.max(-1, Math.min(1, normalized))
  }

  return (
    <div className="px-2 py-1.5 bg-purple-500/10 rounded border border-purple-500/30">
      <div className="flex items-center gap-2 mb-1.5">
        <Radio className="w-3 h-3 text-purple-400 animate-pulse" />
        <span className="text-[9px] text-purple-400 font-medium">LIVE</span>
        <span className="text-[8px] text-muted ml-auto">{liveValue.length} joints</span>
      </div>

      <div className="space-y-1 max-h-32 overflow-y-auto">
        {liveValue.map((value, idx) => {
          const normalized = normalizeJoint(value)
          const savedVal = savedValue?.[idx]
          const diff = savedVal !== undefined ? calcDiff(value, savedVal) : null
          const degrees = (value * 180 / Math.PI)

          return (
            <div key={idx} className="flex items-center gap-2">
              <span className="text-[8px] text-muted w-4 text-right">{idx + 1}</span>

              {/* Bar visualization */}
              <div className="flex-1 h-3 bg-gray-800 rounded-sm overflow-hidden relative">
                <div className="absolute inset-y-0 left-1/2 w-px bg-gray-600" />
                <div
                  className={`absolute inset-y-0 ${normalized >= 0 ? 'left-1/2' : 'right-1/2'} bg-purple-500/70`}
                  style={{ width: `${Math.abs(normalized) * 50}%` }}
                />
              </div>

              <span className="text-[9px] text-primary font-mono w-16 text-right">
                {formatNumber(degrees, 1)}°
              </span>

              {diff && diff.direction !== 'same' && (
                <div className="w-8">
                  <DiffIndicator diff={diff} small />
                </div>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
})

// Diff indicator component
const DiffIndicator = memo(({ diff, isAngle = false, small = false }: {
  diff: { diff: number; direction: 'up' | 'down' | 'same' }
  isAngle?: boolean
  small?: boolean
}) => {
  const Icon = isAngle ? RotateCw : (diff.direction === 'up' ? ArrowUp : ArrowDown)
  const color = diff.direction === 'up' ? 'text-green-400' : 'text-red-400'
  const size = small ? 8 : 10

  return (
    <div className={`flex items-center gap-0.5 ${color}`}>
      <Icon size={size} />
      <span className={`${small ? 'text-[7px]' : 'text-[8px]'} font-mono`}>
        {diff.direction === 'up' ? '+' : ''}{formatNumber(diff.diff, isAngle ? 1 : 2)}{isAngle ? '°' : ''}
      </span>
    </div>
  )
})

TelemetryPreview.displayName = 'TelemetryPreview'
PosePreview.displayName = 'PosePreview'
TwistPreview.displayName = 'TwistPreview'
JointArrayPreview.displayName = 'JointArrayPreview'
DiffIndicator.displayName = 'DiffIndicator'

export default TelemetryPreview
