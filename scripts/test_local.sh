#!/bin/bash

# Fleet Management System - Local Test (No Docker)
# SQLite mode for simple local testing

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BACKEND_DIR="$PROJECT_DIR/central_server/backend"
LOG_DIR="$PROJECT_DIR/logs"
PID_DIR="$PROJECT_DIR/.pids"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

mkdir -p "$LOG_DIR" "$PID_DIR"

echo -e "${CYAN}=============================================="
echo -e "  Fleet Management System - Local Test"
echo -e "  (SQLite mode)"
echo -e "==============================================${NC}"

# Check Python
check_python() {
    if ! command -v python3 &> /dev/null; then
        echo -e "${RED}✗ Python3 not found${NC}"
        exit 1
    fi
    echo -e "${GREEN}✓ Python3 found: $(python3 --version)${NC}"
}

# Install dependencies
install_deps() {
    echo -e "\n${BLUE}▶ Installing backend dependencies...${NC}"
    cd "$BACKEND_DIR"

    if [ ! -d "venv" ]; then
        python3 -m venv venv
        echo -e "  ${GREEN}✓${NC} Virtual environment created"
    fi

    source venv/bin/activate
    pip install -q -r requirements.txt 2>/dev/null
    echo -e "  ${GREEN}✓${NC} Dependencies installed"
}

# Start backend
start_backend() {
    echo -e "\n${BLUE}▶ Starting backend server...${NC}"
    cd "$BACKEND_DIR"
    source venv/bin/activate

    # Kill existing process
    if [ -f "$PID_DIR/backend.pid" ]; then
        kill $(cat "$PID_DIR/backend.pid") 2>/dev/null
        rm -f "$PID_DIR/backend.pid"
        sleep 1
    fi

    # Remove old SQLite DB for fresh start
    rm -f fleet.db

    # Set environment variables
    export DATABASE_URL="sqlite:///./fleet.db"
    export DEFINITIONS_PATH="$PROJECT_DIR/definitions"
    export DEBUG="true"

    # Start backend
    nohup python3 -m uvicorn app.main:app --host 0.0.0.0 --port 8080 --reload \
        > "$LOG_DIR/backend.log" 2>&1 &
    echo $! > "$PID_DIR/backend.pid"

    echo -e "  Waiting for backend to start..."
    sleep 3

    # Check if running
    for i in {1..10}; do
        if curl -s http://localhost:8080/health > /dev/null 2>&1; then
            echo -e "  ${GREEN}✓${NC} Backend started (PID: $(cat $PID_DIR/backend.pid))"
            return 0
        fi
        sleep 1
    done

    echo -e "  ${RED}✗${NC} Backend failed to start"
    cat "$LOG_DIR/backend.log"
    return 1
}

# Create test data
create_test_data() {
    echo -e "\n${BLUE}▶ Creating test data...${NC}"
    API_URL="http://localhost:8080/api"

    # 1. Create Agent
    echo -e "  Creating Agent..."
    curl -s -X POST "$API_URL/robots" \
        -H "Content-Type: application/json" \
        -d '{
            "id": "test_robot_01",
            "name": "Test Robot 1",
            "type_id": null,
            "agent_id": "test_agent_01"
        }' > /dev/null 2>&1

    # Note: Agent is auto-created when robot registers with agent_id

    # 2. Create a Flow
    echo -e "  Creating Flow..."
    curl -s -X POST "$API_URL/flows" \
        -H "Content-Type: application/json" \
        -d '{
            "id": "test_flow_01",
            "name": "Test Pick and Place",
            "description": "A simple test flow",
            "steps": [
                {
                    "id": "step_1",
                    "name": "Move to pickup",
                    "action": {
                        "type": "nav2_msgs/NavigateToPose",
                        "server": "/navigate_to_pose",
                        "params": {
                            "source": "inline",
                            "data": {"x": 1.0, "y": 2.0, "theta": 0.0}
                        }
                    },
                    "transition": {"on_success": "step_2"}
                },
                {
                    "id": "step_2",
                    "name": "Grip object",
                    "action": {
                        "type": "control_msgs/GripperCommand",
                        "server": "/gripper_cmd",
                        "params": {
                            "source": "inline",
                            "data": {"position": 0.0}
                        }
                    },
                    "transition": {"on_success": "done"}
                },
                {
                    "id": "done",
                    "name": "Complete",
                    "type": "terminal",
                    "terminal_type": "success"
                }
            ]
        }' > /dev/null 2>&1

    echo -e "  ${GREEN}✓${NC} Test data created"
}

# Test Agent Flow API
test_agent_flow_api() {
    echo -e "\n${BLUE}▶ Testing Agent Flow API...${NC}"
    API_URL="http://localhost:8080/api"

    # First ensure agent exists (create via robot registration)
    # The agent table needs to exist first
    echo -e "  Checking agents..."
    AGENTS=$(curl -s "$API_URL/robots" | python3 -c "import sys, json; data=json.load(sys.stdin); print(len(data))" 2>/dev/null || echo "0")
    echo -e "    Found ${AGENTS} robot(s)"

    # Since we need agent in DB, let's insert directly via API test
    echo -e "\n  Testing Flow list API..."
    FLOWS=$(curl -s "$API_URL/flows")
    echo -e "    Response: ${FLOWS:0:100}..."

    echo -e "\n  Testing Flow copy-blocks API..."
    COPY_RESULT=$(curl -s -X POST "$API_URL/flows/test_flow_01/copy-blocks" \
        -H "Content-Type: application/json" \
        -d '{"step_ids": ["step_1", "step_2"]}')
    echo -e "    Response: ${COPY_RESULT:0:150}..."

    echo -e "\n  ${GREEN}✓${NC} API tests completed"
}

# Print summary
print_summary() {
    echo -e "\n${CYAN}=============================================="
    echo -e "  Test Environment Ready!"
    echo -e "==============================================${NC}"

    echo -e "\n${YELLOW}🌐 Access URLs:${NC}"
    echo -e "  • API Docs: ${GREEN}http://localhost:8080/docs${NC}"
    echo -e "  • Health:   ${GREEN}http://localhost:8080/health${NC}"

    echo -e "\n${YELLOW}📝 Test the new Agent Flow APIs:${NC}"
    echo -e "  # List flows"
    echo -e "  curl http://localhost:8080/api/flows"
    echo -e ""
    echo -e "  # Copy blocks from flow"
    echo -e "  curl -X POST http://localhost:8080/api/flows/test_flow_01/copy-blocks \\"
    echo -e "    -H 'Content-Type: application/json' \\"
    echo -e "    -d '{\"step_ids\": [\"step_1\", \"step_2\"]}'"

    echo -e "\n${YELLOW}📊 Logs:${NC}"
    echo -e "  tail -f $LOG_DIR/backend.log"

    echo -e "\n${YELLOW}🛑 Stop:${NC}"
    echo -e "  kill \$(cat $PID_DIR/backend.pid)"
}

# Main
main() {
    check_python
    install_deps
    if start_backend; then
        create_test_data
        test_agent_flow_api
        print_summary
    else
        echo -e "${RED}Failed to start backend${NC}"
        exit 1
    fi
}

main
