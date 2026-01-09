"""
Fleet Agent C++ Launch File

Launches the fleet agent node with configurable parameters.

Usage:
  # 기본 실행 (localhost)
  ros2 launch fleet_agent_cpp fleet_agent.launch.py

  # 외부 서버 연결
  ros2 launch fleet_agent_cpp fleet_agent.launch.py server_ip:=192.168.0.100

  # 커스텀 설정 파일 사용
  ros2 launch fleet_agent_cpp fleet_agent.launch.py config:=/path/to/agent.yaml
"""

import os
from ament_index_python.packages import get_package_share_directory
from launch import LaunchDescription
from launch.actions import DeclareLaunchArgument
from launch.substitutions import LaunchConfiguration, PythonExpression
from launch_ros.actions import Node


def generate_launch_description():
    # Get package directory for default paths
    pkg_dir = get_package_share_directory('fleet_agent_cpp')
    default_config = os.path.join(pkg_dir, 'config', 'agent.yaml')

    # ============================================================
    # Launch Arguments
    # ============================================================

    # Server IP - 가장 자주 변경되는 설정
    server_ip_arg = DeclareLaunchArgument(
        'server_ip',
        default_value='localhost',
        description='Central server IP address (e.g., 192.168.0.100)'
    )

    # Server Port
    server_port_arg = DeclareLaunchArgument(
        'server_port',
        default_value='9444',
        description='Central server QUIC port'
    )

    # Agent ID
    agent_id_arg = DeclareLaunchArgument(
        'agent_id',
        default_value='agent_01',
        description='Unique agent identifier'
    )

    # Config file path
    config_arg = DeclareLaunchArgument(
        'config',
        default_value=default_config,
        description='Path to agent configuration file'
    )

    # Log level
    log_level_arg = DeclareLaunchArgument(
        'log_level',
        default_value='info',
        description='Logging level (debug, info, warn, error)'
    )

    # ROS Domain ID
    domain_id_arg = DeclareLaunchArgument(
        'domain_id',
        default_value='0',
        description='ROS_DOMAIN_ID for network isolation'
    )

    # ============================================================
    # Fleet Agent Node
    # ============================================================
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
        # Environment variables - config에서 ${VAR} 문법으로 사용 가능
        additional_env={
            'ROS_DOMAIN_ID': LaunchConfiguration('domain_id'),
            'FLEET_SERVER_IP': LaunchConfiguration('server_ip'),
            'FLEET_SERVER_PORT': LaunchConfiguration('server_port'),
            'FLEET_AGENT_ID': LaunchConfiguration('agent_id'),
            'FLEET_AGENT_PKG_PATH': pkg_dir,
        },
    )

    return LaunchDescription([
        server_ip_arg,
        server_port_arg,
        agent_id_arg,
        config_arg,
        log_level_arg,
        domain_id_arg,
        fleet_agent_node,
    ])
