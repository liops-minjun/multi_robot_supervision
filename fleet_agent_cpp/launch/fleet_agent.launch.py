"""
Fleet Agent C++ Launch File

Launches the fleet agent node with configurable parameters.

Usage:
  ros2 launch fleet_agent_cpp fleet_agent.launch.py
  ros2 launch fleet_agent_cpp fleet_agent.launch.py config:=/path/to/agent.yaml
"""

import os
from launch import LaunchDescription
from launch.actions import DeclareLaunchArgument
from launch.substitutions import LaunchConfiguration, EnvironmentVariable
from launch_ros.actions import Node


def generate_launch_description():
    # Declare arguments
    config_arg = DeclareLaunchArgument(
        'config',
        default_value=[
            EnvironmentVariable('FLEET_AGENT_CONFIG', default_value='/etc/fleet_agent/agent.yaml')
        ],
        description='Path to agent configuration file'
    )

    log_level_arg = DeclareLaunchArgument(
        'log_level',
        default_value='info',
        description='Logging level (debug, info, warn, error)'
    )

    # ROS_DOMAIN_ID argument - defaults to 0 (standard ROS2 default)
    # Use domain_id:=X to isolate from other ROS2 networks if needed
    domain_id_arg = DeclareLaunchArgument(
        'domain_id',
        default_value='0',
        description='ROS_DOMAIN_ID (0 = default, use different value for isolation)'
    )

    # Fleet agent node
    fleet_agent_node = Node(
        package='fleet_agent_cpp',
        executable='fleet_agent_node',
        name='fleet_agent',
        output='screen',
        emulate_tty=True,
        arguments=[
            '--config', LaunchConfiguration('config'),
        ],
        parameters=[{
            'use_sim_time': False,
        }],
        # Set ROS_DOMAIN_ID to avoid DDS discovery blocking
        additional_env={
            'ROS_DOMAIN_ID': LaunchConfiguration('domain_id'),
        },
        # Remap topics if needed
        # remappings=[
        #     ('/robot_001/odom', '/odom'),
        # ],
    )

    return LaunchDescription([
        config_arg,
        log_level_arg,
        domain_id_arg,
        fleet_agent_node,
    ])
