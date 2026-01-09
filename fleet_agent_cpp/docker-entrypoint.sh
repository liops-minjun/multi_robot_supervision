#!/bin/bash
set -e

# Source ROS2 environments
source /opt/ros/humble/setup.bash
source /opt/ros2_ws/install/setup.bash

# Check if config exists
if [ ! -f "$FLEET_AGENT_CONFIG" ]; then
    echo "Warning: Config file not found at $FLEET_AGENT_CONFIG"
    echo "Using default configuration"
fi

# Execute command
exec "$@"
