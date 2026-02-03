#!/bin/bash

# Multi-Robot Supervision System - Robot Agent Start Script
# 실제 Robot Agent를 서버에 연결합니다.

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
cd "$PROJECT_DIR"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

# Default values
SERVER_IP="${1:-localhost}"
AGENT_ID=""
CONFIG_FILE=""

# Parse arguments
show_help() {
    echo -e "${CYAN}Multi-Robot Supervision - Robot Agent${NC}"
    echo ""
    echo "Usage: ./run_client.sh [SERVER_IP] [OPTIONS]"
    echo ""
    echo "Arguments:"
    echo "  SERVER_IP          Server IP address (default: localhost)"
    echo ""
    echo "Options:"
    echo "  --agent-id=ID      Set agent ID (default: auto-generated)"
    echo "  --config=FILE      Use existing config file"
    echo "  --help, -h         Show this help"
    echo ""
    echo "Examples:"
    echo "  ./run_client.sh                        # Connect to localhost"
    echo "  ./run_client.sh 192.168.0.10           # Connect to remote server"
    echo "  ./run_client.sh 192.168.0.10 --agent-id=factory-agent-01"
    exit 0
}

for arg in "$@"; do
    case $arg in
        --agent-id=*)
            AGENT_ID="${arg#*=}"
            ;;
        --config=*)
            CONFIG_FILE="${arg#*=}"
            ;;
        --help|-h)
            show_help
            ;;
        -*)
            echo -e "${RED}Unknown option: $arg${NC}"
            show_help
            ;;
    esac
done

# Get SERVER_IP from first positional arg
for arg in "$@"; do
    case $arg in
        --*) ;;
        *)
            SERVER_IP="$arg"
            break
            ;;
    esac
done

# Auto-generate agent ID if not provided
if [ -z "$AGENT_ID" ]; then
    AGENT_ID="agent-$(hostname | tr '[:upper:]' '[:lower:]' | tr -cd '[:alnum:]-')"
fi

echo -e "${CYAN}=============================================="
echo -e "  Multi-Robot Supervision - Robot Agent"
echo -e "==============================================${NC}"
echo ""

# Check if ROS2 is available
if ! command -v ros2 &> /dev/null; then
    echo -e "${RED}Error: ROS2 is not installed or not sourced${NC}"
    echo ""
    echo -e "${YELLOW}Please source your ROS2 installation:${NC}"
    echo -e "  source /opt/ros/humble/setup.bash"
    echo ""
    echo -e "${YELLOW}Or install ROS2:${NC}"
    echo -e "  https://docs.ros.org/en/humble/Installation.html"
    exit 1
fi

echo -e "${GREEN}✓${NC} ROS2 detected: $(ros2 --version 2>/dev/null | head -1 || echo 'unknown')"

# Check server connectivity
SERVER_URL="http://${SERVER_IP}:8080"
QUIC_SERVER="${SERVER_IP}"

echo -e "\n${CYAN}Configuration:${NC}"
echo -e "  Server URL:   ${GREEN}${SERVER_URL}${NC}"
echo -e "  QUIC Server:  ${GREEN}${QUIC_SERVER}:9443${NC}"
echo -e "  Agent ID:     ${GREEN}${AGENT_ID}${NC}"
echo ""

echo -e "${YELLOW}Checking server connection...${NC}"
if curl -s --connect-timeout 5 "${SERVER_URL}/health" > /dev/null 2>&1; then
    echo -e "  ${GREEN}✓${NC} Server is reachable"
else
    echo -e "  ${RED}✗${NC} Cannot connect to server at ${SERVER_URL}"
    echo ""
    echo -e "${YELLOW}Make sure the server is running:${NC}"
    echo -e "  $SCRIPT_DIR/run_server.sh"
    exit 1
fi

# Config file handling
CONFIG_DIR="${PROJECT_DIR}/robot_agent/config"
if [ -z "$CONFIG_FILE" ]; then
    CONFIG_FILE="${CONFIG_DIR}/agent.yaml"
fi

# Create config if not exists
if [ ! -f "$CONFIG_FILE" ]; then
    echo -e "\n${YELLOW}Creating agent config...${NC}"
    mkdir -p "$(dirname "$CONFIG_FILE")"

    cat > "$CONFIG_FILE" << EOF
# Robot Agent Configuration
# Generated: $(date)

agent:
  id: "${AGENT_ID}"
  name: "Robot Agent ${AGENT_ID}"

server:
  url: "${SERVER_URL}"
  timeout_sec: 5.0
  quic:
    server_address: "${QUIC_SERVER}"
    server_port: 9443

robots:
  # Add your robots here
  - id: "robot-001"
    type: "gocart250"
    name: "Robot 001"
    ros_namespace: "/robot_001"
    enabled: true

communication:
  telemetry_rate_hz: 1.0

paths:
  definitions: "${PROJECT_DIR}/definitions"
  action_graphs: "/tmp/robot_agent/action_graphs"

timeouts:
  action_default_sec: 120.0
EOF

    echo -e "  ${GREEN}✓${NC} Config created: ${CONFIG_FILE}"
    echo ""
    echo -e "${YELLOW}Please edit the config file to add your robots:${NC}"
    echo -e "  ${CONFIG_FILE}"
    echo ""
    read -p "Press Enter to continue or Ctrl+C to exit and edit config first..."
fi

echo -e "\n${GREEN}Starting Robot Agent...${NC}"
echo -e "  Config: ${CONFIG_FILE}"
echo ""

# Run the agent
cd "${PROJECT_DIR}/robot_agent"
python3 -m robot_agent --config "$CONFIG_FILE"
