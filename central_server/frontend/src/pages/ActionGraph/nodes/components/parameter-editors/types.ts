import type { CapturedTelemetry } from '../../../../../contexts/TelemetryContext'
import type { ParameterFieldSource } from '../../../../../types'
import type { AvailableStep } from '../../types'

// Common props for all parameter editors
export interface BaseEditorProps {
  fieldName: string
  fieldType: string
  value: unknown
  onChange: (value: unknown) => void
  // Telemetry
  robotTelemetry?: RobotTelemetryData | null
  capturedTelemetry?: CapturedTelemetry | null
  onCaptureTelemetry?: () => void
  // Binding
  fieldSource?: ParameterFieldSource
  availableSteps?: AvailableStep[]
  onFieldSourceChange?: (source: ParameterFieldSource | undefined) => void
}

// Robot telemetry data structure (from WebSocket)
export interface RobotTelemetryData {
  joint_state?: {
    name: string[]
    position: number[]
    velocity: number[]
    effort: number[]
  }
  odometry?: {
    frame_id?: string
    child_frame_id?: string
    pose: {
      position: { x: number; y: number; z: number }
      orientation: { x: number; y: number; z: number; w: number }
    }
    twist: {
      linear: { x: number; y: number; z: number }
      angular: { x: number; y: number; z: number }
    }
  }
  transforms?: Array<{
    frame_id: string
    child_frame_id: string
    translation: { x: number; y: number; z: number }
    rotation: { x: number; y: number; z: number; w: number }
    timestamp_ns?: number
  }>
}

// Pose value structure
export interface PoseValue {
  position: { x: number; y: number; z: number }
  orientation: { x: number; y: number; z: number; w: number }
}

// PoseStamped value structure
export interface PoseStampedValue {
  header: {
    stamp?: { sec: number; nanosec: number }
    frame_id: string
  }
  pose: PoseValue
}

// Twist value structure
export interface TwistValue {
  linear: { x: number; y: number; z: number }
  angular: { x: number; y: number; z: number }
}

// Input mode for parameter editors
export type EditorMode = 'telemetry' | 'manual' | 'binding' | 'json'

// Editor type determined by field type
export type EditorType =
  | 'pose'           // Pose, PoseStamped, PoseWithCovariance
  | 'point'          // Point, PointStamped, Vector3
  | 'twist'          // Twist, TwistStamped
  | 'quaternion'     // Quaternion
  | 'joint_array'    // float64[] with joint_state compatibility
  | 'numeric_array'  // Other numeric arrays
  | 'string_array'   // string[]
  | 'joint_state'    // Full JointState message
  | 'std_primitive_msg' // std_msgs/{String,Bool,Int*,UInt*,Float*,Double}
  | 'header'         // std_msgs/Header
  | 'primitive'      // bool, number, string
  | 'json'           // Unknown complex types

// Quaternion to Euler conversion (returns degrees)
export function quaternionToEuler(q: { x: number; y: number; z: number; w: number }): { roll: number; pitch: number; yaw: number } {
  // Roll (x-axis rotation)
  const sinr_cosp = 2 * (q.w * q.x + q.y * q.z)
  const cosr_cosp = 1 - 2 * (q.x * q.x + q.y * q.y)
  const roll = Math.atan2(sinr_cosp, cosr_cosp)

  // Pitch (y-axis rotation)
  const sinp = 2 * (q.w * q.y - q.z * q.x)
  let pitch: number
  if (Math.abs(sinp) >= 1) {
    pitch = Math.sign(sinp) * Math.PI / 2 // Use 90 degrees if out of range
  } else {
    pitch = Math.asin(sinp)
  }

  // Yaw (z-axis rotation)
  const siny_cosp = 2 * (q.w * q.z + q.x * q.y)
  const cosy_cosp = 1 - 2 * (q.y * q.y + q.z * q.z)
  const yaw = Math.atan2(siny_cosp, cosy_cosp)

  return {
    roll: roll * 180 / Math.PI,
    pitch: pitch * 180 / Math.PI,
    yaw: yaw * 180 / Math.PI,
  }
}

// Euler to Quaternion conversion (input in degrees)
export function eulerToQuaternion(euler: { roll: number; pitch: number; yaw: number }): { x: number; y: number; z: number; w: number } {
  const rollRad = euler.roll * Math.PI / 180
  const pitchRad = euler.pitch * Math.PI / 180
  const yawRad = euler.yaw * Math.PI / 180

  const cy = Math.cos(yawRad * 0.5)
  const sy = Math.sin(yawRad * 0.5)
  const cp = Math.cos(pitchRad * 0.5)
  const sp = Math.sin(pitchRad * 0.5)
  const cr = Math.cos(rollRad * 0.5)
  const sr = Math.sin(rollRad * 0.5)

  return {
    w: cr * cp * cy + sr * sp * sy,
    x: sr * cp * cy - cr * sp * sy,
    y: cr * sp * cy + sr * cp * sy,
    z: cr * cp * sy - sr * sp * cy,
  }
}

// Determine editor type from ROS2 type string
export function getEditorType(rosType: string, isArray: boolean): EditorType {
  const lower = rosType.toLowerCase()
  const stdPrimitive = getStdPrimitiveWrapperType(rosType)

  // IMPORTANT: Check for arrays FIRST before primitives!
  // Otherwise "string" with isArray=true would match primitive check
  if (isArray || lower.includes('[]') || lower.includes('sequence<')) {
    // Extract base type from array notation
    const baseType = lower.replace('[]', '').replace(/sequence<(.+)>/, '$1').trim()

    // Check if it's a joint-compatible array (float64[], number[], etc)
    if (baseType.includes('float') || baseType.includes('double') || baseType === 'number') {
      return 'joint_array' // Can be used with joint_state telemetry
    }
    if (baseType.includes('string')) {
      return 'string_array'
    }
    // Any other numeric type
    const numericTypes = ['int8', 'int16', 'int32', 'int64', 'uint8', 'uint16', 'uint32', 'uint64']
    if (numericTypes.some(t => baseType === t || baseType.startsWith(t))) {
      return 'numeric_array'
    }
    return 'numeric_array' // Default for arrays
  }

  // Check for known ROS2 message types
  if (lower.includes('/')) {
    // std_msgs primitive wrappers (String/Bool/Int*/Float*)
    if (stdPrimitive) {
      return 'std_primitive_msg'
    }

    // Pose types
    if (lower.includes('posestamped') || lower.includes('posewithcovariance')) {
      return 'pose'
    }
    if (lower.includes('pose') && !lower.includes('array')) {
      return 'pose'
    }

    // Point/Vector types
    if (lower.includes('point') || lower.includes('vector3')) {
      return 'point'
    }

    // Twist types
    if (lower.includes('twist')) {
      return 'twist'
    }

    // Quaternion
    if (lower.includes('quaternion')) {
      return 'quaternion'
    }

    // JointState
    if (lower.includes('jointstate')) {
      return 'joint_state'
    }

    // Header
    if (lower.includes('header')) {
      return 'header'
    }

    // Path or PoseArray (array of poses)
    if (lower.includes('path') || lower.includes('posearray')) {
      return 'json' // For now, complex arrays go to JSON
    }

    // Unknown complex type
    return 'json'
  }

  // Primitive types (only for non-arrays)
  if (lower === 'bool' || lower === 'boolean') {
    return 'primitive'
  }

  // Numeric types
  const numericTypes = ['int8', 'int16', 'int32', 'int64', 'uint8', 'uint16', 'uint32', 'uint64', 'float32', 'float64', 'double', 'float']
  if (numericTypes.some(t => lower === t || lower.startsWith(t))) {
    return 'primitive'
  }

  if (lower === 'string') {
    return 'primitive'
  }

  return 'primitive'
}

export type StdPrimitiveWrapperType = 'string' | 'boolean' | 'number'

export function getStdPrimitiveWrapperType(rosType: string): StdPrimitiveWrapperType | null {
  const lower = (rosType || '').toLowerCase().trim()
  const normalized = lower
    .replace(/::/g, '/')
    .replace(/__/g, '/')
    .replace(/^std_msgs\//, 'std_msgs/msg/')
    .replace(/\/+/g, '/')

  if (normalized === 'std_msgs/msg/string') return 'string'
  if (normalized === 'std_msgs/msg/bool') return 'boolean'

  const numericNames = [
    'int8', 'int16', 'int32', 'int64',
    'uint8', 'uint16', 'uint32', 'uint64',
    'float32', 'float64', 'float', 'double',
  ]

  if (numericNames.some((name) => normalized === `std_msgs/msg/${name}`)) {
    return 'number'
  }

  return null
}

// Format number for display
export function formatNumber(value: number, precision: number = 3): string {
  return value.toFixed(precision)
}

// Calculate difference between two values
export function calcDiff(current: number, saved: number): { diff: number; direction: 'up' | 'down' | 'same' } {
  const diff = current - saved
  if (Math.abs(diff) < 0.001) {
    return { diff: 0, direction: 'same' }
  }
  return { diff, direction: diff > 0 ? 'up' : 'down' }
}
