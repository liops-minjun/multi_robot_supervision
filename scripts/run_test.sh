#!/bin/bash
#
# Multi-Robot Supervision System - Integration Test Script
#
# This script:
# 1. Builds the test ROS2 action server package
# 2. Starts mock action servers for multiple simulated robots
# 3. Starts Robot Agents that discover and register capabilities
# 4. Verifies the complete flow with the central server
#
# Prerequisites:
# - ROS2 Humble or later installed
# - Central server running (docker-compose up)
# - Python venv for robot_agent
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
TEST_WS="${PROJECT_DIR}/test_ros2_ws"
ROBOT_AGENT_DIR="${PROJECT_DIR}/robot_agent"
CENTRAL_SERVER_URL="${CENTRAL_SERVER_URL:-http://localhost:8081}"
QUIC_SERVER="${QUIC_SERVER:-localhost}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

cleanup() {
    log_info "Cleaning up..."
    # Kill all background processes
    if [ -n "$ACTION_SERVER_PIDS" ]; then
        for pid in $ACTION_SERVER_PIDS; do
            kill $pid 2>/dev/null || true
        done
    fi
    if [ -n "$ROBOT_AGENT_PIDS" ]; then
        for pid in $ROBOT_AGENT_PIDS; do
            kill $pid 2>/dev/null || true
        done
    fi
    log_info "Cleanup complete"
}

trap cleanup EXIT

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."

    # Check ROS2
    if ! command -v ros2 &> /dev/null; then
        log_error "ROS2 not found. Please install ROS2 Humble or later."
        exit 1
    fi
    log_success "ROS2 found: $(ros2 --version 2>&1 | head -1)"

    # Check central server
    if ! curl -s "${CENTRAL_SERVER_URL}/health" > /dev/null 2>&1; then
        log_error "Central server not reachable at ${CENTRAL_SERVER_URL}"
        log_info "Please start the central server: docker-compose up -d"
        exit 1
    fi
    log_success "Central server reachable at ${CENTRAL_SERVER_URL}"

    # Check QUIC server (UDP port)
    log_info "QUIC server configured at ${QUIC_SERVER}:9443"
}

# Build test ROS2 package
build_test_package() {
    log_info "Building test ROS2 action server package..."

    cd "${TEST_WS}"

    # Source ROS2
    source /opt/ros/${ROS_DISTRO:-humble}/setup.bash

    # Build
    colcon build --packages-select test_action_servers 2>&1 | tail -20

    if [ $? -eq 0 ]; then
        log_success "Test package built successfully"
    else
        log_error "Failed to build test package"
        exit 1
    fi

    # Source the workspace
    source "${TEST_WS}/install/setup.bash"
}

# Start action servers for a robot
start_action_servers() {
    local robot_namespace=$1
    log_info "Starting action servers for ${robot_namespace}..."

    source /opt/ros/${ROS_DISTRO:-humble}/setup.bash
    source "${TEST_WS}/install/setup.bash"

    # Run all action servers in background
    ros2 run test_action_servers run_all_servers.py "${robot_namespace}" &
    local pid=$!
    ACTION_SERVER_PIDS="${ACTION_SERVER_PIDS} ${pid}"

    sleep 2  # Wait for servers to start

    # Verify action servers are running
    if ros2 action list 2>/dev/null | grep -q "${robot_namespace}"; then
        log_success "Action servers started for ${robot_namespace}"
    else
        log_warn "Could not verify action servers for ${robot_namespace}"
    fi
}

# Start robot agent
start_robot_agent() {
    local robot_id=$1
    local robot_name=$2
    local agent_id=$3
    local robot_namespace=$4

    log_info "Starting robot agent: ${robot_id} (${robot_name}) under agent ${agent_id}..."

    cd "${ROBOT_AGENT_DIR}"

    # Activate virtual environment if exists
    if [ -f "venv/bin/activate" ]; then
        source venv/bin/activate
    fi

    # Create temporary config
    local config_file="/tmp/robot_agent_${robot_id}.yaml"
    cat > "${config_file}" << EOF
agent:
  id: "${agent_id}"
  name: "${agent_id}"

robots:
  - id: "${robot_id}"
    name: "${robot_name}"
    namespace: "${robot_namespace}"

server:
  url: "${CENTRAL_SERVER_URL}"
  quic:
    server_address: "${QUIC_SERVER}"
    server_port: 9443
EOF

    # Run agent in background
    python3 -m fleet_agent --config "${config_file}" &
    local pid=$!
    ROBOT_AGENT_PIDS="${ROBOT_AGENT_PIDS} ${pid}"

    sleep 3  # Wait for agent to register

    log_success "Robot agent ${robot_id} started (PID: ${pid})"
}

# Verify registration
verify_registration() {
    log_info "Verifying robot registrations..."

    # Check agents
    local agents=$(curl -s "${CENTRAL_SERVER_URL}/api/agents" 2>/dev/null)
    echo "Registered agents:"
    echo "${agents}" | python3 -m json.tool 2>/dev/null || echo "${agents}"

    # Check robots
    local robots=$(curl -s "${CENTRAL_SERVER_URL}/api/robots" 2>/dev/null)
    echo ""
    echo "Registered robots:"
    echo "${robots}" | python3 -m json.tool 2>/dev/null || echo "${robots}"

    # Check capabilities
    echo ""
    echo "Fleet capabilities:"
    local capabilities=$(curl -s "${CENTRAL_SERVER_URL}/api/capabilities" 2>/dev/null)
    echo "${capabilities}" | python3 -m json.tool 2>/dev/null || echo "${capabilities}"
}

# Main test flow
main() {
    echo ""
    echo "=========================================="
    echo "  Multi-Robot Supervision System Test"
    echo "=========================================="
    echo ""

    check_prerequisites
    echo ""

    # Build test package
    build_test_package
    echo ""

    # Start action servers for 2 test robots
    start_action_servers "robot1"
    start_action_servers "robot2"
    echo ""

    # Wait for action servers to be ready
    log_info "Waiting for action servers to be ready..."
    sleep 3

    # List available actions
    log_info "Available actions:"
    ros2 action list 2>/dev/null || log_warn "Could not list actions"
    echo ""

    # Start robot agents
    start_robot_agent "test_robot_01" "Test Robot 1" "test_agent_01" "robot1"
    start_robot_agent "test_robot_02" "Test Robot 2" "test_agent_01" "robot2"
    echo ""

    # Wait for registration
    log_info "Waiting for robots to register..."
    sleep 5
    echo ""

    # Verify
    verify_registration
    echo ""

    log_success "Test environment is ready!"
    echo ""
    echo "You can now:"
    echo "  1. Open http://localhost:3000 to access the UI"
    echo "  2. Create templates and assign them to test_agent_01"
    echo "  3. Execute action graphs on the test robots"
    echo ""
    echo "Press Ctrl+C to stop all servers and agents"
    echo ""

    # Keep running
    wait
}

# Parse arguments
case "${1:-}" in
    --build-only)
        build_test_package
        ;;
    --servers-only)
        check_prerequisites
        build_test_package
        start_action_servers "robot1"
        start_action_servers "robot2"
        wait
        ;;
    --help|-h)
        echo "Usage: $0 [options]"
        echo ""
        echo "Options:"
        echo "  --build-only    Only build the test package"
        echo "  --servers-only  Start action servers without robot agents"
        echo "  --help          Show this help"
        echo ""
        echo "Environment variables:"
        echo "  CENTRAL_SERVER_URL  Central server URL (default: http://localhost:8081)"
        echo "  QUIC_SERVER         QUIC server host (default: localhost)"
        ;;
    *)
        main
        ;;
esac
