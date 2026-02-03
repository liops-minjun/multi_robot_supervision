#!/bin/bash
# Reset Fleet Agent to initial state
# This script clears all persistent state including:
# - Action graphs
# - State definitions
# - State persistence cache
# - Message queue
# - QUIC resumption ticket

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}Fleet Agent Reset Script${NC}"
echo "========================="

# Default paths (development mode uses /tmp)
DEV_BASE="/tmp/robot_agent"
PROD_BASE="/var/lib/robot_agent"

# Check which base to use
if [ -d "$DEV_BASE" ]; then
    BASE="$DEV_BASE"
    echo -e "Using development paths: ${GREEN}$DEV_BASE${NC}"
elif [ -d "$PROD_BASE" ]; then
    BASE="$PROD_BASE"
    echo -e "Using production paths: ${GREEN}$PROD_BASE${NC}"
else
    echo -e "${YELLOW}No agent state directories found. Agent may already be clean.${NC}"
    exit 0
fi

# Stop the agent if running
echo -e "\n${YELLOW}Step 1: Stopping fleet agent...${NC}"
if pkill -f robot_agent_node 2>/dev/null; then
    echo -e "${GREEN}Agent stopped${NC}"
    sleep 1
else
    echo -e "${YELLOW}Agent was not running${NC}"
fi

# Clear state directories
echo -e "\n${YELLOW}Step 2: Clearing state directories...${NC}"

clear_dir() {
    local dir=$1
    local name=$2
    if [ -d "$dir" ]; then
        local count=$(find "$dir" -type f 2>/dev/null | wc -l)
        rm -rf "$dir"/*
        echo -e "  ${GREEN}Cleared${NC} $name ($count files)"
    else
        echo -e "  ${YELLOW}Skipped${NC} $name (not found)"
    fi
}

clear_dir "$BASE/graphs" "Action Graphs"
clear_dir "$BASE/state_definitions" "State Definitions"
clear_dir "$BASE/state" "State Persistence"
clear_dir "$BASE/queue" "Message Queue"

# Clear QUIC resumption ticket
QUIC_TICKET="$BASE/quic_ticket"
if [ -f "$QUIC_TICKET" ]; then
    rm -f "$QUIC_TICKET"
    echo -e "  ${GREEN}Cleared${NC} QUIC resumption ticket"
else
    echo -e "  ${YELLOW}Skipped${NC} QUIC ticket (not found)"
fi

echo -e "\n${GREEN}Agent state cleared successfully!${NC}"
echo ""
echo "To restart the agent:"
echo "  cd ros2_ws"
echo "  source /opt/ros/humble/setup.bash"
echo "  source install/setup.bash"
echo "  ros2 run ros2_robot_agent robot_agent_node"
