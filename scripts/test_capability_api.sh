#!/bin/bash
# Test script for Capability API endpoints (Zero-Config Architecture)

BASE_URL="${API_URL:-http://localhost:8081}"

echo "========================================"
echo "Testing Capability API - Zero Config"
echo "Base URL: $BASE_URL"
echo "========================================"

# Color codes for output
GREEN='\033[0;32m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test result counter
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

    # Return the body for further processing
    echo "$body"
}

# 1. Health check
echo -e "\n${BLUE}=== 1. Health Check ===${NC}"
test_endpoint "GET" "/health" "" "200" "Health check"

# 2. Register a robot with capabilities (Zero-config style)
echo -e "\n${BLUE}=== 2. Register Robot with Capabilities ===${NC}"
robot_payload='{
  "id": "test_robot_001",
  "name": "Test Robot 1",
  "agent_id": "test_agent_001",
  "namespace": "/test_robot_001",
  "tags": ["mobile", "navigation"],
  "capabilities": [
    {
      "action_type": "nav2_msgs/action/NavigateToPose",
      "action_server": "/test_robot_001/navigate_to_pose",
      "goal_schema": {
        "pose": {"type": "geometry_msgs/PoseStamped"}
      },
      "result_schema": {
        "result": {"type": "int16"}
      },
      "success_criteria": {
        "field": "result",
        "operator": "equals",
        "value": 0
      }
    },
    {
      "action_type": "nav2_msgs/action/Spin",
      "action_server": "/test_robot_001/spin",
      "goal_schema": {
        "target_yaw": {"type": "float32"}
      }
    }
  ]
}'
test_endpoint "POST" "/api/robots" "$robot_payload" "201" "Register robot with capabilities"

# 3. Get robot details (should include capabilities)
echo -e "\n${BLUE}=== 3. Get Robot Details ===${NC}"
test_endpoint "GET" "/api/robots/test_robot_001" "" "200" "Get robot with capabilities"

# 4. Get robot capabilities
echo -e "\n${BLUE}=== 4. Get Robot Capabilities ===${NC}"
test_endpoint "GET" "/api/robots/test_robot_001/capabilities" "" "200" "Get robot capabilities"

# 5. Register additional capabilities
echo -e "\n${BLUE}=== 5. Register Additional Capabilities ===${NC}"
cap_payload='{
  "robot_id": "test_robot_001",
  "capabilities": [
    {
      "action_type": "nav2_msgs/action/NavigateToPose",
      "action_server": "/test_robot_001/navigate_to_pose",
      "goal_schema": {"pose": {"type": "geometry_msgs/PoseStamped"}}
    },
    {
      "action_type": "control_msgs/action/GripperCommand",
      "action_server": "/test_robot_001/gripper",
      "goal_schema": {"position": {"type": "float32"}, "max_effort": {"type": "float32"}}
    }
  ]
}'
test_endpoint "PUT" "/api/robots/test_robot_001/capabilities" "$cap_payload" "200" "Update capabilities"

# 6. Update capability status
echo -e "\n${BLUE}=== 6. Update Capability Status ===${NC}"
status_payload='{
  "robot_id": "test_robot_001",
  "status": {
    "nav2_msgs/action/NavigateToPose": {
      "available": true,
      "status": "executing"
    },
    "control_msgs/action/GripperCommand": {
      "available": true,
      "status": "idle"
    }
  }
}'
test_endpoint "PATCH" "/api/robots/test_robot_001/capabilities/status" "$status_payload" "200" "Update capability status"

# 7. List all capabilities (fleet-wide)
echo -e "\n${BLUE}=== 7. List All Capabilities (Fleet-wide) ===${NC}"
test_endpoint "GET" "/api/capabilities" "" "200" "List all capabilities"

# 8. Get capabilities by action type
echo -e "\n${BLUE}=== 8. Get Capabilities by Action Type ===${NC}"
test_endpoint "GET" "/api/capabilities/nav2_msgs/action/NavigateToPose" "" "200" "Get capabilities by action type"

# 9. Register second robot (to test aggregation)
echo -e "\n${BLUE}=== 9. Register Second Robot ===${NC}"
robot2_payload='{
  "id": "test_robot_002",
  "name": "Test Robot 2",
  "agent_id": "test_agent_001",
  "namespace": "/test_robot_002",
  "tags": ["mobile", "manipulation"],
  "capabilities": [
    {
      "action_type": "nav2_msgs/action/NavigateToPose",
      "action_server": "/test_robot_002/navigate_to_pose"
    },
    {
      "action_type": "moveit_msgs/action/MoveGroup",
      "action_server": "/test_robot_002/move_group"
    }
  ]
}'
test_endpoint "POST" "/api/robots" "$robot2_payload" "201" "Register second robot"

# 10. List all capabilities again (should show aggregation)
echo -e "\n${BLUE}=== 10. List All Capabilities (After Second Robot) ===${NC}"
test_endpoint "GET" "/api/capabilities" "" "200" "List aggregated capabilities"

# 11. Update robot metadata
echo -e "\n${BLUE}=== 11. Update Robot Metadata ===${NC}"
update_payload='{
  "name": "Updated Test Robot 1",
  "tags": ["mobile", "navigation", "updated"]
}'
test_endpoint "PATCH" "/api/robots/test_robot_001" "$update_payload" "200" "Update robot metadata"

# 12. List robots
echo -e "\n${BLUE}=== 12. List All Robots ===${NC}"
test_endpoint "GET" "/api/robots" "" "200" "List all robots"

# Cleanup (optional)
# echo -e "\n${BLUE}=== Cleanup ===${NC}"
# test_endpoint "DELETE" "/api/robots/test_robot_001" "" "200" "Delete test robot 1"
# test_endpoint "DELETE" "/api/robots/test_robot_002" "" "200" "Delete test robot 2"

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
