#!/bin/bash
# Setup test ROS2 workspace with symbolic links
# Usage: ./setup_test.sh

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WS_DIR="${SCRIPT_DIR}/ros2_ws"

echo "Creating ROS2 workspace at ${WS_DIR}..."

# Create workspace directory
mkdir -p "${WS_DIR}/src"

# Create symbolic links
cd "${WS_DIR}/src"

if [ ! -L "ros2_robot_agent" ]; then
    ln -s "${SCRIPT_DIR}/ros2_robot_agent" ros2_robot_agent
    echo "Created symlink: ros2_robot_agent"
else
    echo "Symlink already exists: ros2_robot_agent"
fi

if [ ! -L "test_action_server" ]; then
    ln -s "${SCRIPT_DIR}/test_action_server" test_action_server
    echo "Created symlink: test_action_server"
else
    echo "Symlink already exists: test_action_server"
fi

echo ""
echo "Setup complete! To build:"
echo "  cd ${WS_DIR}"
echo "  source /opt/ros/humble/setup.bash"
echo "  colcon build"
