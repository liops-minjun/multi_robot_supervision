"""
Launch file for Test Action Servers (Lifecycle Nodes)

Launches all three test action servers as lifecycle nodes:
- test_A_action
- test_B_action
- test_C_action

Each server waits 5-10 seconds and has 90% success rate.

Lifecycle states:
- UNCONFIGURED: Node created, action server not running
- INACTIVE: Configured, action server not accepting goals
- ACTIVE: Action server running and accepting goals

Usage:
  # Launch with auto-activation to ACTIVE state (default)
  ros2 launch test_action_server test_servers.launch.py

  # Launch in UNCONFIGURED state (manual activation required)
  ros2 launch test_action_server test_servers.launch.py auto_activate:=false

  # Launch with namespace
  ros2 launch test_action_server test_servers.launch.py namespace:=/robot_001

Manual lifecycle control:
  ros2 lifecycle set /test_a_action_server configure
  ros2 lifecycle set /test_a_action_server activate
  ros2 lifecycle get /test_a_action_server
"""

from launch import LaunchDescription
from launch.actions import DeclareLaunchArgument, ExecuteProcess, TimerAction
from launch.substitutions import LaunchConfiguration, PythonExpression
from launch.conditions import IfCondition
from launch_ros.actions import LifecycleNode
from launch_ros.events.lifecycle import ChangeState
from lifecycle_msgs.msg import Transition


def generate_launch_description():
    # Declare arguments
    namespace_arg = DeclareLaunchArgument(
        'namespace',
        default_value='',
        description='Namespace for the action servers'
    )

    auto_activate_arg = DeclareLaunchArgument(
        'auto_activate',
        default_value='true',
        description='Automatically activate lifecycle nodes to ACTIVE state'
    )

    # Test A Action Server (Lifecycle Node)
    test_a_server = LifecycleNode(
        package='test_action_server',
        executable='test_a_server.py',
        name='test_a_action_server',
        namespace=LaunchConfiguration('namespace'),
        output='screen',
        emulate_tty=True,
    )

    # Test B Action Server (Lifecycle Node)
    test_b_server = LifecycleNode(
        package='test_action_server',
        executable='test_b_server.py',
        name='test_b_action_server',
        namespace=LaunchConfiguration('namespace'),
        output='screen',
        emulate_tty=True,
    )

    # Test C Action Server (Lifecycle Node)
    test_c_server = LifecycleNode(
        package='test_action_server',
        executable='test_c_server.py',
        name='test_c_action_server',
        namespace=LaunchConfiguration('namespace'),
        output='screen',
        emulate_tty=True,
    )

    # Auto-activation using ros2 lifecycle commands (executed after nodes start)
    auto_activate_cmd = TimerAction(
        period=3.0,  # Wait 3 seconds for nodes to start
        actions=[
            ExecuteProcess(
                cmd=['ros2', 'run', 'test_action_server', 'auto_activate.py', '--all', '--quiet'],
                output='screen',
            )
        ],
        condition=IfCondition(LaunchConfiguration('auto_activate'))
    )

    return LaunchDescription([
        namespace_arg,
        auto_activate_arg,
        test_a_server,
        test_b_server,
        test_c_server,
        auto_activate_cmd,
    ])
