"""
Launch file for test telemetry publisher

Usage:
  # Single robot
  ros2 launch test_telemetry_node telemetry_test.launch.py robot_namespace:=/robot_001

  # Multiple robots
  ros2 launch test_telemetry_node telemetry_test.launch.py robot_namespace:=/robot_001 &
  ros2 launch test_telemetry_node telemetry_test.launch.py robot_namespace:=/robot_002 &
"""

from launch import LaunchDescription
from launch.actions import DeclareLaunchArgument
from launch.substitutions import LaunchConfiguration
from launch_ros.actions import Node


def generate_launch_description():
    # Declare arguments
    robot_namespace_arg = DeclareLaunchArgument(
        'robot_namespace',
        default_value='/robot_001',
        description='Robot namespace (e.g., /robot_001)'
    )

    publish_rate_arg = DeclareLaunchArgument(
        'publish_rate',
        default_value='10.0',
        description='Telemetry publish rate in Hz'
    )

    joint_names_arg = DeclareLaunchArgument(
        'joint_names',
        default_value="['joint_1', 'joint_2', 'joint_3', 'joint_4', 'joint_5', 'joint_6']",
        description='List of joint names'
    )

    # Telemetry publisher node
    telemetry_node = Node(
        package='test_telemetry_node',
        executable='telemetry_publisher.py',
        name='telemetry_publisher',
        parameters=[{
            'robot_namespace': LaunchConfiguration('robot_namespace'),
            'publish_rate': LaunchConfiguration('publish_rate'),
            'joint_names': LaunchConfiguration('joint_names'),
        }],
        output='screen',
    )

    return LaunchDescription([
        robot_namespace_arg,
        publish_rate_arg,
        joint_names_arg,
        telemetry_node,
    ])
