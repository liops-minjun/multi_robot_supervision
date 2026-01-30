// Telemetry type mapping for value capture (robot arm teaching support)

// Map telemetry paths to compatible ROS2 message types
export const TELEMETRY_TO_GOAL_MAPPING: Record<string, string[]> = {
  // Odometry/Pose types
  'odometry.pose': [
    'geometry_msgs/msg/Pose',
    'geometry_msgs/msg/PoseStamped',
    'geometry_msgs/msg/PoseWithCovariance',
    'geometry_msgs/msg/PoseWithCovarianceStamped',
  ],
  'odometry.pose.position': [
    'geometry_msgs/msg/Point',
    'geometry_msgs/msg/PointStamped',
    'geometry_msgs/msg/Vector3',
  ],
  'odometry.pose.orientation': [
    'geometry_msgs/msg/Quaternion',
  ],
  'odometry.twist': [
    'geometry_msgs/msg/Twist',
    'geometry_msgs/msg/TwistStamped',
    'geometry_msgs/msg/TwistWithCovariance',
  ],
  // JointState - full message
  'joint_state': [
    'sensor_msgs/msg/JointState',
    'trajectory_msgs/msg/JointTrajectoryPoint',
  ],
  // JointState - individual arrays for direct field mapping
  'joint_state.position': [
    'array',           // Generic array type shown in UI
    'float64[]',
    'double[]',
    'sequence<double>',
    'sequence<float64>',
  ],
  'joint_state.velocity': [
    'array',
    'float64[]',
    'double[]',
    'sequence<double>',
    'sequence<float64>',
  ],
  'joint_state.effort': [
    'array',
    'float64[]',
    'double[]',
    'sequence<double>',
    'sequence<float64>',
  ],
  'joint_state.name': [
    'array',           // string array
    'string[]',
    'sequence<string>',
  ],
}

// Reverse mapping: ROS2 type -> telemetry path
export const GOAL_TO_TELEMETRY_MAPPING: Record<string, string> = {}
for (const [path, types] of Object.entries(TELEMETRY_TO_GOAL_MAPPING)) {
  for (const type of types) {
    GOAL_TO_TELEMETRY_MAPPING[type] = path
    // Also map the lowercase version
    GOAL_TO_TELEMETRY_MAPPING[type.toLowerCase()] = path
  }
}

// Check if a goal parameter type is compatible with telemetry
export function findCompatibleTelemetryPath(goalType: string): string | null {
  return GOAL_TO_TELEMETRY_MAPPING[goalType] || GOAL_TO_TELEMETRY_MAPPING[goalType.toLowerCase()] || null
}

// Check if a captured telemetry value is compatible with a goal field type
export function isTelemetryCompatible(telemetryPath: string, goalType: string): boolean {
  const compatibleTypes = TELEMETRY_TO_GOAL_MAPPING[telemetryPath] || []
  const normalizedGoalType = goalType.toLowerCase()
  return compatibleTypes.some(t => t.toLowerCase() === normalizedGoalType)
}

// Convert captured telemetry value to goal parameter format
export function convertTelemetryToGoalParam(
  telemetryPath: string,
  telemetryValue: unknown,
  targetType: string
): unknown {
  if (!telemetryValue) return null

  const lowerType = targetType.toLowerCase()

  // Handle PoseStamped - wrap pose in stamped container
  if (lowerType.includes('posestamped') && telemetryPath === 'odometry.pose') {
    return {
      header: {
        stamp: { sec: 0, nanosec: 0 },
        frame_id: 'map',
      },
      pose: telemetryValue,
    }
  }

  // Handle TwistStamped
  if (lowerType.includes('twiststamped') && telemetryPath === 'odometry.twist') {
    return {
      header: {
        stamp: { sec: 0, nanosec: 0 },
        frame_id: 'base_link',
      },
      twist: telemetryValue,
    }
  }

  // Handle PointStamped
  if (lowerType.includes('pointstamped') && telemetryPath === 'odometry.pose.position') {
    return {
      header: {
        stamp: { sec: 0, nanosec: 0 },
        frame_id: 'map',
      },
      point: telemetryValue,
    }
  }

  // Handle JointTrajectoryPoint from JointState
  if (lowerType.includes('jointtrajectorypoint') && telemetryPath === 'joint_state') {
    const js = telemetryValue as { position?: number[]; velocity?: number[]; effort?: number[] }
    return {
      positions: js.position || [],
      velocities: js.velocity || [],
      accelerations: [],
      effort: js.effort || [],
      time_from_start: { sec: 0, nanosec: 0 },
    }
  }

  // Direct value (no conversion needed)
  return telemetryValue
}

// Human-readable name for telemetry path
export function getTelemetryPathDisplayName(path: string): string {
  const names: Record<string, string> = {
    'joint_state': 'JointState (전체)',
    'joint_state.position': 'Joint Positions',
    'joint_state.velocity': 'Joint Velocities',
    'joint_state.effort': 'Joint Efforts',
    'joint_state.name': 'Joint Names',
    'odometry.pose': 'Odometry Pose',
    'odometry.pose.position': 'Position',
    'odometry.pose.orientation': 'Orientation',
    'odometry.twist': 'Velocity (Twist)',
  }
  return names[path] || path
}
