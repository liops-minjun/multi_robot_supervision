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

if [ ! -L "fleet_agent_cpp" ]; then
    ln -s "${SCRIPT_DIR}/fleet_agent_cpp" fleet_agent_cpp
    echo "Created symlink: fleet_agent_cpp"
else
    echo "Symlink already exists: fleet_agent_cpp"
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
