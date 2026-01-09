# Test Action Server

ROS2 패키지로 Action Graph 테스트용 Action Server들을 제공합니다.

## Action Servers

| Action Server | Topic | 설명 |
|--------------|-------|-----|
| test_A_action | `/test_A_action` | Test Action A |
| test_B_action | `/test_B_action` | Test Action B |
| test_C_action | `/test_C_action` | Test Action C |

## 동작

각 Action Server는 다음과 같이 동작합니다:
- **실행 시간**: 5-10초 (랜덤)
- **성공률**: 90%
- **실패율**: 10%

## Action Interface

```
# Goal
string task_name          # 태스크 이름
float32 timeout_sec 30.0  # 타임아웃 (초)

# Result
bool success              # 성공 여부
string message            # 결과 메시지
float32 execution_time    # 실행 시간

# Feedback
float32 progress          # 진행률 (0.0 ~ 1.0)
string status             # 현재 상태
float32 elapsed_time      # 경과 시간
```

## 빌드

```bash
# ROS2 워크스페이스로 이동 (또는 새로 생성)
cd ~/ros2_ws/src

# 심볼릭 링크 생성 또는 패키지 복사
ln -s /path/to/test_action_server .

# 빌드
cd ~/ros2_ws
colcon build --packages-select test_action_server

# 환경 설정
source install/setup.bash
```

## 실행

### 모든 서버 실행
```bash
ros2 launch test_action_server test_servers.launch.py
```

### 네임스페이스 지정
```bash
ros2 launch test_action_server test_servers.launch.py namespace:=/robot_001
```

### 개별 서버 실행
```bash
ros2 run test_action_server test_a_server.py
ros2 run test_action_server test_b_server.py
ros2 run test_action_server test_c_server.py
```

## 테스트

### Action 목록 확인
```bash
ros2 action list
# 출력:
# /test_A_action
# /test_B_action
# /test_C_action
```

### Action 호출 테스트
```bash
ros2 action send_goal /test_A_action test_action_server/action/TestAction "{task_name: 'test_task'}"
```

### 피드백 포함 호출
```bash
ros2 action send_goal /test_A_action test_action_server/action/TestAction "{task_name: 'test_task'}" --feedback
```

## Fleet Agent와 함께 사용

Fleet Agent가 이 Action Server들을 자동으로 발견하려면:

1. 같은 ROS_DOMAIN_ID를 사용해야 합니다
2. Fleet Agent 설정에 로봇을 추가합니다:

```yaml
# fleet_agent_cpp/config/agent.yaml
robots:
  - id: "test_robot"
    namespace: ""  # 또는 "/robot_001"
    name: "Test Robot"
    tags: ["test"]
```

3. Fleet Agent를 실행하면 자동으로 Action Server들이 발견됩니다.
