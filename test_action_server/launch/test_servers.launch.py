"""
Launch file for Test Action Servers

Launches all three test action servers:
- test_A_action
- test_B_action
- test_C_action

Each server waits 5-10 seconds and has 90% success rate.

Usage:
  ros2 launch test_action_server test_servers.launch.py
  ros2 launch test_action_server test_servers.launch.py namespace:=/robot_001
"""

from launch import LaunchDescription
from launch.actions import DeclareLaunchArgument
from launch.substitutions import LaunchConfiguration
from launch_ros.actions import Node


def generate_launch_description():
    # Declare arguments
    namespace_arg = DeclareLaunchArgument(
        'namespace',
        default_value='',
        description='Namespace for the action servers'
    )

    # Test A Action Server
    test_a_server = Node(
        package='test_action_server',
        executable='test_a_server.py',
        name='test_a_action_server',
        namespace=LaunchConfiguration('namespace'),
        output='screen',
        emulate_tty=True,
    )

    # Test B Action Server
    test_b_server = Node(
        package='test_action_server',
        executable='test_b_server.py',
        name='test_b_action_server',
        namespace=LaunchConfiguration('namespace'),
        output='screen',
        emulate_tty=True,
    )

    # Test C Action Server
    test_c_server = Node(
        package='test_action_server',
        executable='test_c_server.py',
        name='test_c_action_server',
        namespace=LaunchConfiguration('namespace'),
        output='screen',
        emulate_tty=True,
    )

    return LaunchDescription([
        namespace_arg,
        test_a_server,
        test_b_server,
        test_c_server,
    ])
