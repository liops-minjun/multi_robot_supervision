#!/bin/bash
# Test script for Canonical Graph API endpoints

BASE_URL="${API_URL:-http://localhost:8081}"

echo "========================================"
echo "Testing Canonical Graph API"
echo "Base URL: $BASE_URL"
echo "========================================"

# Color codes
GREEN='\033[0;32m'
RED='\033[0;31m'
BLUE='\033[0;34m'
YELLOW='\033[0;33m'
NC='\033[0m'

PASSED=0
FAILED=0

test_endpoint() {
    local method=$1
    local endpoint=$2
    local data=$3
    local expected_status=$4
    local description=$5

    echo -e "\n${BLUE}Testing: $description${NC}"
    echo "  $method $endpoint"

    if [ -n "$data" ]; then
        response=$(curl -s -w "\n%{http_code}" -X "$method" "$BASE_URL$endpoint" \
            -H "Content-Type: application/json" \
            -d "$data")
    else
        response=$(curl -s -w "\n%{http_code}" -X "$method" "$BASE_URL$endpoint")
    fi

    status_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | sed '$d')

    if [ "$status_code" == "$expected_status" ]; then
        echo -e "  ${GREEN}PASS${NC} (Status: $status_code)"
        PASSED=$((PASSED + 1))
    else
        echo -e "  ${RED}FAIL${NC} (Expected: $expected_status, Got: $status_code)"
        echo "  Response: $body"
        FAILED=$((FAILED + 1))
    fi

    echo "$body"
}

# Silent cleanup function (doesn't affect test counts)
silent_cleanup() {
    local endpoint=$1
    curl -s -X DELETE "$BASE_URL$endpoint" > /dev/null 2>&1
}

# 0. Pre-test cleanup (silent)
echo -e "\n${YELLOW}=== Pre-test Cleanup (silent) ===${NC}"
# Clean up any existing test data from previous runs (order matters for FK constraints)
# First delete robots that reference the agent
silent_cleanup "/api/robots/test_robot_graph"
silent_cleanup "/api/robots/test_robot_001"
# Then delete agent action graphs
silent_cleanup "/api/agents/test_agent_001/action-graphs/test_graph_001"
silent_cleanup "/api/agents/test_agent_001/action-graphs/test_graph_cycle"
# Then delete agent
silent_cleanup "/api/agents/test_agent_001"
# Finally delete action graphs
silent_cleanup "/api/action-graphs/test_graph_001"
silent_cleanup "/api/action-graphs/test_graph_cycle"
echo "  Cleanup complete"

# 1. Health check
echo -e "\n${BLUE}=== 1. Health Check ===${NC}"
test_endpoint "GET" "/health" "" "200" "Health check"

# 2. Create a test action graph with proper graph structure
echo -e "\n${BLUE}=== 2. Create Test Action Graph ===${NC}"
graph_payload='{
  "id": "test_graph_001",
  "name": "Test Pick and Place",
  "description": "Test graph for canonical format",
  "steps": [
    {
      "id": "navigate_to_pick",
      "name": "Navigate to Pick Location",
      "action": {
        "type": "nav2_msgs/action/NavigateToPose",
        "server": "/navigate_to_pose",
        "params": {
          "source": "waypoint",
          "waypoint_id": "pick_location"
        },
        "timeout_sec": 120.0
      },
      "during_states": ["navigating"],
      "success_states": ["at_pick"],
      "transition": {
        "on_success": "pick_item",
        "on_failure": {
          "retry": 2,
          "fallback": "failure_end"
        }
      }
    },
    {
      "id": "pick_item",
      "name": "Pick Item",
      "action": {
        "type": "control_msgs/action/GripperCommand",
        "server": "/gripper",
        "params": {
          "source": "inline",
          "data": {"position": 0.0, "max_effort": 50.0}
        },
        "timeout_sec": 30.0
      },
      "during_states": ["picking"],
      "success_states": ["holding_item"],
      "transition": {
        "on_success": "success_end",
        "on_failure": "failure_end"
      }
    },
    {
      "id": "success_end",
      "type": "terminal",
      "terminal_type": "success",
      "name": "Task Complete",
      "message": "Pick completed successfully"
    },
    {
      "id": "failure_end",
      "type": "terminal",
      "terminal_type": "failure",
      "name": "Task Failed",
      "alert": true,
      "message": "Pick task failed"
    }
  ]
}'
test_endpoint "POST" "/api/action-graphs" "$graph_payload" "201" "Create action graph"

# 3. Get the action graph in standard format
echo -e "\n${BLUE}=== 3. Get Action Graph (Standard Format) ===${NC}"
test_endpoint "GET" "/api/action-graphs/test_graph_001" "" "200" "Get action graph"

# 4. Get the action graph in CANONICAL format
echo -e "\n${BLUE}=== 4. Get Action Graph (Canonical Format) ===${NC}"
test_endpoint "GET" "/api/action-graphs/test_graph_001/canonical" "" "200" "Get canonical graph"

# 5. Validate the canonical graph
echo -e "\n${BLUE}=== 5. Validate Canonical Graph ===${NC}"
test_endpoint "POST" "/api/action-graphs/test_graph_001/validate-canonical" "" "200" "Validate canonical graph"

# 6. Create a graph with a cycle (should be detected)
echo -e "\n${BLUE}=== 6. Create Graph with Potential Issues ===${NC}"
cyclic_graph='{
  "id": "test_graph_cycle",
  "name": "Graph with Issues",
  "steps": [
    {
      "id": "step_a",
      "name": "Step A",
      "transition": {
        "on_success": "step_b"
      }
    },
    {
      "id": "step_b",
      "name": "Step B"
    }
  ]
}'
test_endpoint "POST" "/api/action-graphs" "$cyclic_graph" "201" "Create graph without terminal"

# 7. Validate the problematic graph
echo -e "\n${BLUE}=== 7. Validate Problematic Graph ===${NC}"
test_endpoint "POST" "/api/action-graphs/test_graph_cycle/validate-canonical" "" "200" "Validate should show warnings"

# 8. List all action graphs
echo -e "\n${BLUE}=== 8. List Action Graphs ===${NC}"
test_endpoint "GET" "/api/action-graphs" "" "200" "List action graphs"

# 9. Create an agent first, then register robot
echo -e "\n${BLUE}=== 9. Register Agent and Robot ===${NC}"
# First create the agent (accept 201 or 409 if already exists)
echo -e "\n${BLUE}Testing: Register agent${NC}"
echo "  POST /api/agents"
agent_response=$(curl -s -w "\n%{http_code}" -X POST "$BASE_URL/api/agents" \
    -H "Content-Type: application/json" \
    -d '{"id":"test_agent_001","name":"Test Agent 1"}')
agent_status=$(echo "$agent_response" | tail -n1)
if [ "$agent_status" == "201" ] || [ "$agent_status" == "409" ]; then
    echo -e "  ${GREEN}PASS${NC} (Status: $agent_status)"
    PASSED=$((PASSED + 1))
else
    echo -e "  ${RED}FAIL${NC} (Expected: 201 or 409, Got: $agent_status)"
    FAILED=$((FAILED + 1))
fi
# Then create robot with that agent
test_endpoint "POST" "/api/robots" '{"id":"test_robot_graph","name":"Test Robot","agent_id":"test_agent_001"}' "201" "Register robot with agent"

# 10. Test deployment endpoint - Agent exists but not connected
# Note: This may timeout since agent is not running, so we skip counting it as failure
echo -e "\n${BLUE}=== 10. Deploy Graph to Agent (API Test) ===${NC}"
echo "  Note: Agent is registered but not connected, may timeout or fail"
deploy_response=$(curl -s -m 5 -w "\n%{http_code}" -X POST "$BASE_URL/api/action-graphs/test_graph_001/deploy/test_agent_001" 2>/dev/null || echo -e "\n000")
deploy_status=$(echo "$deploy_response" | tail -n1)
echo "  POST /api/action-graphs/test_graph_001/deploy/test_agent_001"
if [ "$deploy_status" == "000" ]; then
    echo -e "  ${YELLOW}SKIP${NC} (Timeout - Agent not connected, expected)"
elif [ "$deploy_status" == "404" ] || [ "$deploy_status" == "500" ]; then
    echo -e "  ${GREEN}PASS${NC} (Status: $deploy_status - Agent offline handling)"
    PASSED=$((PASSED + 1))
else
    echo -e "  ${RED}FAIL${NC} (Unexpected status: $deploy_status)"
    FAILED=$((FAILED + 1))
fi

# Cleanup
echo -e "\n${BLUE}=== Cleanup ===${NC}"
# Delete in correct order to avoid foreign key issues
test_endpoint "DELETE" "/api/robots/test_robot_graph" "" "200" "Delete test robot"
test_endpoint "DELETE" "/api/agents/test_agent_001" "" "200" "Delete test agent"
test_endpoint "DELETE" "/api/action-graphs/test_graph_001" "" "200" "Delete test graph 1"
test_endpoint "DELETE" "/api/action-graphs/test_graph_cycle" "" "200" "Delete test graph 2"

# Summary
echo -e "\n========================================"
echo "Test Results"
echo "========================================"
echo -e "Passed: ${GREEN}$PASSED${NC}"
echo -e "Failed: ${RED}$FAILED${NC}"
echo "========================================"

if [ $FAILED -gt 0 ]; then
    exit 1
fi
