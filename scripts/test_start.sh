#!/bin/bash

# Fleet Management System - Mobile Manipulator 테스트
# Mobile Manipulator 전용 테스트 환경을 구성합니다.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
LOG_DIR="$PROJECT_DIR/logs"
PID_DIR="$PROJECT_DIR/.pids"
API_URL="http://localhost:8080/api"

# 색상 정의
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

mkdir -p "$LOG_DIR" "$PID_DIR"

echo -e "${CYAN}=============================================="
echo -e "  Mobile Manipulator 테스트 환경"
echo -e "==============================================${NC}"

# Backend 확인
check_backend() {
    if ! curl -s http://localhost:8080/health > /dev/null 2>&1; then
        echo -e "${RED}✗ Backend가 실행 중이 아닙니다!${NC}"
        echo -e "${YELLOW}  먼저 $SCRIPT_DIR/start.sh 를 실행하세요${NC}"
        exit 1
    fi
    echo -e "${GREEN}✓ Backend 연결 확인됨${NC}"
}

# 기존 데이터 정리 (선택적)
cleanup_data() {
    echo -e "\n${YELLOW}기존 데이터 정리 중...${NC}"
    # State definitions, flows, waypoints는 중복 생성 시 에러가 나므로 그냥 진행
}

# Mobile Manipulator State Definition 생성
create_state_definition() {
    echo -e "\n${BLUE}▶ Mobile Manipulator State Definition 생성${NC}"

    curl -s -X POST "$API_URL/state-definitions" \
        -H "Content-Type: application/json" \
        -d '{
            "id": "manipulator",
            "name": "Mobile Manipulator",
            "description": "6축 로봇팔이 장착된 모바일 플랫폼",
            "states": ["idle", "navigating", "moving_arm", "gripping", "releasing", "inspecting", "error"],
            "default_state": "idle",
            "action_mappings": [
                {
                    "action_type": "nav2_msgs/NavigateToPose",
                    "server": "/navigate_to_pose",
                    "during_state": "navigating"
                },
                {
                    "action_type": "control_msgs/FollowJointTrajectory",
                    "server": "/arm_controller/follow_joint_trajectory",
                    "during_state": "moving_arm"
                },
                {
                    "action_type": "control_msgs/GripperCommand",
                    "server": "/gripper_controller/gripper_cmd",
                    "during_state": "gripping"
                }
            ],
            "telemetry_topics": {
                "pose": "/amcl_pose",
                "battery": "/battery_state",
                "joint_states": "/joint_states",
                "gripper": "/gripper_state"
            }
        }' > /dev/null 2>&1

    echo -e "  ${GREEN}✓${NC} Mobile Manipulator 정의 생성됨"
}

# Waypoints 생성
create_waypoints() {
    echo -e "\n${BLUE}▶ Waypoints 생성${NC}"

    # 픽업 위치들
    curl -s -X POST "$API_URL/waypoints" -H "Content-Type: application/json" -d '{
        "id": "pickup_station_a",
        "name": "픽업 스테이션 A",
        "waypoint_type": "pose_2d",
        "data": {"x": 2.0, "y": 3.0, "theta": 0.0},
        "tags": ["pickup", "station_a"],
        "description": "부품 픽업 위치 A"
    }' > /dev/null 2>&1

    curl -s -X POST "$API_URL/waypoints" -H "Content-Type: application/json" -d '{
        "id": "pickup_station_b",
        "name": "픽업 스테이션 B",
        "waypoint_type": "pose_2d",
        "data": {"x": 5.0, "y": 3.0, "theta": 0.0},
        "tags": ["pickup", "station_b"],
        "description": "부품 픽업 위치 B"
    }' > /dev/null 2>&1

    # 드롭 위치들
    curl -s -X POST "$API_URL/waypoints" -H "Content-Type: application/json" -d '{
        "id": "drop_station_1",
        "name": "드롭 스테이션 1",
        "waypoint_type": "pose_2d",
        "data": {"x": 10.0, "y": 5.0, "theta": 1.57},
        "tags": ["drop", "assembly"],
        "description": "조립 라인 드롭 위치"
    }' > /dev/null 2>&1

    curl -s -X POST "$API_URL/waypoints" -H "Content-Type: application/json" -d '{
        "id": "drop_station_2",
        "name": "드롭 스테이션 2",
        "waypoint_type": "pose_2d",
        "data": {"x": 12.0, "y": 5.0, "theta": 1.57},
        "tags": ["drop", "packaging"],
        "description": "포장 라인 드롭 위치"
    }' > /dev/null 2>&1

    # 검사 위치
    curl -s -X POST "$API_URL/waypoints" -H "Content-Type: application/json" -d '{
        "id": "inspection_point",
        "name": "검사 포인트",
        "waypoint_type": "pose_2d",
        "data": {"x": 8.0, "y": 8.0, "theta": 3.14},
        "tags": ["inspection", "qc"],
        "description": "품질 검사 위치"
    }' > /dev/null 2>&1

    # 홈 위치
    curl -s -X POST "$API_URL/waypoints" -H "Content-Type: application/json" -d '{
        "id": "home_position",
        "name": "홈 포지션",
        "waypoint_type": "pose_2d",
        "data": {"x": 0.0, "y": 0.0, "theta": 0.0},
        "tags": ["home", "charging"],
        "description": "충전 및 대기 위치"
    }' > /dev/null 2>&1

    # 로봇팔 포즈들
    curl -s -X POST "$API_URL/waypoints" -H "Content-Type: application/json" -d '{
        "id": "arm_home",
        "name": "팔 홈 포즈",
        "waypoint_type": "joint_state",
        "data": {"positions": [0.0, -1.57, 1.57, 0.0, 0.0, 0.0]},
        "tags": ["arm", "home"],
        "description": "로봇팔 기본 자세"
    }' > /dev/null 2>&1

    curl -s -X POST "$API_URL/waypoints" -H "Content-Type: application/json" -d '{
        "id": "arm_pickup_ready",
        "name": "픽업 준비 포즈",
        "waypoint_type": "joint_state",
        "data": {"positions": [0.0, -0.5, 1.0, 0.0, 0.5, 0.0]},
        "tags": ["arm", "pickup"],
        "description": "물체 픽업 준비 자세"
    }' > /dev/null 2>&1

    curl -s -X POST "$API_URL/waypoints" -H "Content-Type: application/json" -d '{
        "id": "arm_drop_ready",
        "name": "드롭 준비 포즈",
        "waypoint_type": "joint_state",
        "data": {"positions": [0.0, -0.3, 0.8, 0.0, 0.3, 0.0]},
        "tags": ["arm", "drop"],
        "description": "물체 내려놓기 자세"
    }' > /dev/null 2>&1

    echo -e "  ${GREEN}✓${NC} Waypoints 9개 생성됨"
}

# Flows 생성
create_flows() {
    echo -e "\n${BLUE}▶ Flow 템플릿 생성${NC}"

    # 1. 단순 픽앤플레이스 Flow
    curl -s -X POST "$API_URL/flows" \
        -H "Content-Type: application/json" \
        -d '{
            "id": "simple_pick_and_place",
            "name": "단순 픽앤플레이스",
            "description": "A에서 물체를 집어 B로 옮기는 기본 작업",
            "version": "1.0",
            "steps": [
                {
                    "id": "go_to_pickup",
                    "name": "픽업 위치로 이동",
                    "action": {
                        "type": "nav2_msgs/NavigateToPose",
                        "server": "/navigate_to_pose",
                        "params": {"waypoint_id": "pickup_station_a"}
                    },
                    "transition": {"on_success": "prepare_arm", "on_failure": {"fallback": "error_stop"}}
                },
                {
                    "id": "prepare_arm",
                    "name": "팔 픽업 준비",
                    "action": {
                        "type": "control_msgs/FollowJointTrajectory",
                        "server": "/arm_controller/follow_joint_trajectory",
                        "params": {"waypoint_id": "arm_pickup_ready"}
                    },
                    "transition": {"on_success": "grip_object", "on_failure": {"fallback": "error_stop"}}
                },
                {
                    "id": "grip_object",
                    "name": "물체 잡기",
                    "action": {
                        "type": "control_msgs/GripperCommand",
                        "server": "/gripper_controller/gripper_cmd",
                        "params": {"position": 0.0, "max_effort": 50.0}
                    },
                    "transition": {"on_success": "go_to_drop", "on_failure": {"fallback": "error_stop"}}
                },
                {
                    "id": "go_to_drop",
                    "name": "드롭 위치로 이동",
                    "action": {
                        "type": "nav2_msgs/NavigateToPose",
                        "server": "/navigate_to_pose",
                        "params": {"waypoint_id": "drop_station_1"}
                    },
                    "transition": {"on_success": "drop_arm_position", "on_failure": {"fallback": "error_stop"}}
                },
                {
                    "id": "drop_arm_position",
                    "name": "팔 드롭 준비",
                    "action": {
                        "type": "control_msgs/FollowJointTrajectory",
                        "server": "/arm_controller/follow_joint_trajectory",
                        "params": {"waypoint_id": "arm_drop_ready"}
                    },
                    "transition": {"on_success": "release_object", "on_failure": {"fallback": "error_stop"}}
                },
                {
                    "id": "release_object",
                    "name": "물체 놓기",
                    "action": {
                        "type": "control_msgs/GripperCommand",
                        "server": "/gripper_controller/gripper_cmd",
                        "params": {"position": 0.08, "max_effort": 10.0}
                    },
                    "transition": {"on_success": "return_home", "on_failure": {"fallback": "error_stop"}}
                },
                {
                    "id": "return_home",
                    "name": "홈으로 복귀",
                    "action": {
                        "type": "nav2_msgs/NavigateToPose",
                        "server": "/navigate_to_pose",
                        "params": {"waypoint_id": "home_position"}
                    },
                    "transition": {"on_success": "done"}
                },
                {
                    "id": "done",
                    "name": "완료",
                    "type": "terminal",
                    "terminal_type": "success"
                },
                {
                    "id": "error_stop",
                    "name": "에러 정지",
                    "type": "terminal",
                    "terminal_type": "failure"
                }
            ]
        }' > /dev/null 2>&1
    echo -e "  ${GREEN}✓${NC} 단순 픽앤플레이스 Flow"

    # 2. 검사 포함 Flow
    curl -s -X POST "$API_URL/flows" \
        -H "Content-Type: application/json" \
        -d '{
            "id": "pick_inspect_place",
            "name": "픽업-검사-배치",
            "description": "물체를 픽업하고 검사 후 배치하는 작업",
            "version": "1.0",
            "steps": [
                {
                    "id": "start",
                    "name": "시작",
                    "transition": {"on_success": "navigate_pickup"}
                },
                {
                    "id": "navigate_pickup",
                    "name": "픽업 위치 이동",
                    "action": {
                        "type": "nav2_msgs/NavigateToPose",
                        "server": "/navigate_to_pose",
                        "params": {"waypoint_id": "pickup_station_b"}
                    },
                    "transition": {"on_success": "pickup_object", "on_failure": {"fallback": "fail"}}
                },
                {
                    "id": "pickup_object",
                    "name": "물체 픽업",
                    "action": {
                        "type": "control_msgs/GripperCommand",
                        "server": "/gripper_controller/gripper_cmd",
                        "params": {"position": 0.0}
                    },
                    "transition": {"on_success": "navigate_inspection"}
                },
                {
                    "id": "navigate_inspection",
                    "name": "검사 위치 이동",
                    "action": {
                        "type": "nav2_msgs/NavigateToPose",
                        "server": "/navigate_to_pose",
                        "params": {"waypoint_id": "inspection_point"}
                    },
                    "transition": {"on_success": "wait_inspection"}
                },
                {
                    "id": "wait_inspection",
                    "name": "검사 대기 (수동 확인)",
                    "manual_confirm": true,
                    "transition": {"on_success": "navigate_drop", "on_failure": {"fallback": "reject_item"}}
                },
                {
                    "id": "navigate_drop",
                    "name": "드롭 위치 이동",
                    "action": {
                        "type": "nav2_msgs/NavigateToPose",
                        "server": "/navigate_to_pose",
                        "params": {"waypoint_id": "drop_station_2"}
                    },
                    "transition": {"on_success": "drop_object"}
                },
                {
                    "id": "drop_object",
                    "name": "물체 배치",
                    "action": {
                        "type": "control_msgs/GripperCommand",
                        "server": "/gripper_controller/gripper_cmd",
                        "params": {"position": 0.08}
                    },
                    "transition": {"on_success": "success"}
                },
                {
                    "id": "reject_item",
                    "name": "불량품 처리",
                    "type": "fallback",
                    "action": {
                        "type": "nav2_msgs/NavigateToPose",
                        "server": "/navigate_to_pose",
                        "params": {"waypoint_id": "home_position"}
                    },
                    "transition": {"on_success": "fail"}
                },
                {
                    "id": "success",
                    "name": "성공",
                    "type": "terminal",
                    "terminal_type": "success"
                },
                {
                    "id": "fail",
                    "name": "실패",
                    "type": "terminal",
                    "terminal_type": "failure"
                }
            ]
        }' > /dev/null 2>&1
    echo -e "  ${GREEN}✓${NC} 픽업-검사-배치 Flow"

    # 3. 멀티 픽업 Flow
    curl -s -X POST "$API_URL/flows" \
        -H "Content-Type: application/json" \
        -d '{
            "id": "multi_station_delivery",
            "name": "다중 스테이션 배송",
            "description": "여러 스테이션을 순회하며 물품 배송",
            "version": "1.0",
            "steps": [
                {
                    "id": "station_a",
                    "name": "스테이션 A 방문",
                    "action": {
                        "type": "nav2_msgs/NavigateToPose",
                        "server": "/navigate_to_pose",
                        "params": {"waypoint_id": "pickup_station_a"}
                    },
                    "transition": {"on_success": "work_a"}
                },
                {
                    "id": "work_a",
                    "name": "A 작업",
                    "action": {
                        "type": "control_msgs/FollowJointTrajectory",
                        "server": "/arm_controller/follow_joint_trajectory",
                        "params": {"waypoint_id": "arm_pickup_ready"}
                    },
                    "transition": {"on_success": "station_b"}
                },
                {
                    "id": "station_b",
                    "name": "스테이션 B 방문",
                    "action": {
                        "type": "nav2_msgs/NavigateToPose",
                        "server": "/navigate_to_pose",
                        "params": {"waypoint_id": "pickup_station_b"}
                    },
                    "transition": {"on_success": "work_b"}
                },
                {
                    "id": "work_b",
                    "name": "B 작업",
                    "action": {
                        "type": "control_msgs/FollowJointTrajectory",
                        "server": "/arm_controller/follow_joint_trajectory",
                        "params": {"waypoint_id": "arm_drop_ready"}
                    },
                    "transition": {"on_success": "final_drop"}
                },
                {
                    "id": "final_drop",
                    "name": "최종 배송",
                    "action": {
                        "type": "nav2_msgs/NavigateToPose",
                        "server": "/navigate_to_pose",
                        "params": {"waypoint_id": "drop_station_1"}
                    },
                    "transition": {"on_success": "complete"}
                },
                {
                    "id": "complete",
                    "name": "완료",
                    "type": "terminal",
                    "terminal_type": "success"
                }
            ]
        }' > /dev/null 2>&1
    echo -e "  ${GREEN}✓${NC} 다중 스테이션 배송 Flow"
}

# Mock Robot Simulator 시작
start_simulator() {
    echo -e "\n${BLUE}▶ Mock Robot Simulator 시작${NC}"

    # 기존 프로세스 종료
    if [ -f "$PID_DIR/mock_robots.pid" ]; then
        kill $(cat "$PID_DIR/mock_robots.pid") 2>/dev/null
        rm -f "$PID_DIR/mock_robots.pid"
        sleep 1
    fi

    # unbuffered 모드로 실행
    PYTHONUNBUFFERED=1 nohup python3 "$PROJECT_DIR/mock_robot_simulator.py" > "$LOG_DIR/mock_robots.log" 2>&1 &
    echo $! > "$PID_DIR/mock_robots.pid"

    sleep 2

    if ps -p $(cat "$PID_DIR/mock_robots.pid") > /dev/null 2>&1; then
        echo -e "  ${GREEN}✓${NC} Simulator 시작됨 (PID: $(cat $PID_DIR/mock_robots.pid))"
    else
        echo -e "  ${RED}✗${NC} Simulator 시작 실패"
        cat "$LOG_DIR/mock_robots.log"
    fi
}

# 결과 출력
print_summary() {
    echo -e "\n${CYAN}=============================================="
    echo -e "  테스트 환경 준비 완료!"
    echo -e "==============================================${NC}"

    echo -e "\n${YELLOW}📦 생성된 데이터:${NC}"
    echo -e "  • State Definition: Mobile Manipulator"
    echo -e "  • Waypoints: 9개 (픽업/드롭/검사/홈/팔 포즈)"
    echo -e "  • Flows: 3개"
    echo -e "    - 단순 픽앤플레이스"
    echo -e "    - 픽업-검사-배치 (수동 확인 포함)"
    echo -e "    - 다중 스테이션 배송"
    echo -e "  • Mock Robots: 3대 (MANIP-001~003)"

    echo -e "\n${YELLOW}🌐 접속 주소:${NC}"
    echo -e "  • UI: ${GREEN}http://localhost:3000${NC}"
    echo -e "  • API: ${GREEN}http://localhost:8080/docs${NC}"

    echo -e "\n${YELLOW}📋 Flow 테스트 방법:${NC}"
    echo -e "  1. UI에서 'Flow Editor' 메뉴 접속"
    echo -e "  2. Flow 선택 후 'Execute' 버튼 클릭"
    echo -e "  3. 로봇 선택 후 실행"
    echo -e "  4. 'Task History'에서 진행 상황 확인"

    echo -e "\n${YELLOW}📊 로그 확인:${NC}"
    echo -e "  tail -f $LOG_DIR/mock_robots.log"

    echo -e "\n${YELLOW}🛑 시뮬레이터 종료:${NC}"
    echo -e "  kill \$(cat $PID_DIR/mock_robots.pid)"
}

# 메인 실행
main() {
    check_backend
    create_state_definition
    create_waypoints
    create_flows
    start_simulator
    print_summary
}

main
