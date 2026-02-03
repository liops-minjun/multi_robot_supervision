# Fleet Management System 개발 명세서

## 1. 개요

### 1.1 시스템 목적
- 여러 로봇(AMR, 지게차, 모바일 매니퓰레이터 등)을 중앙에서 관리
- 웹 UI에서 Action Flow를 구성하고 로봇에 배포/실행
- 로봇 상태 실시간 모니터링 (staleness 포함)
- Teach 기능으로 로봇 상태를 Waypoint로 저장 및 재사용

### 1.2 핵심 설계 원칙

| 원칙 | 설명 |
|------|------|
| **Server-DDS 분리** | Central Server는 DDS 없음. 순수 HTTP/WebSocket만 사용 |
| **Agent 단순화** | Robot Agent는 상태 보고 + 명령 실행만. 최대한 단순하게 |
| **파일 vs DB 분리** | .action 파일만 파일로 관리, 나머지는 DB + UI |
| **State 자동 전환** | Action 실행 시 자동으로 해당 State로 전환 |
| **Fleet 상태 공유** | Server가 다른 로봇 상태를 각 Agent에게 전달 |

### 1.3 용어 정의

| 용어 | 설명 |
|------|------|
| State | 로봇의 현재 상태 (idle, navigating, lifting 등) |
| Action | ROS2 Action Server로 실행하는 작업 단위 |
| Action Mapping | Action Type과 State의 매핑 (이 Action 중 = 이 State) |
| Flow | 여러 Step으로 구성된 작업 시나리오 |
| Step | Flow 내 단일 작업 단위 (Action + Transition 조건) |
| Waypoint | 저장된 위치/자세 파라미터 (Teach로 생성 가능) |
| Telemetry | Robot Agent가 서버에 보고하는 상태 데이터 |

---

## 2. 아키텍처

### 2.1 전체 구조

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Web Clients                                     │
│                 (State Editor, Flow Editor, Monitoring, Teach UI)           │
└────────────────────────────────────┬────────────────────────────────────────┘
                                     │ REST API / WebSocket (1Hz)
                                     ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                      Central Server (192.168.0.200)                         │
│                              ❌ DDS 없음                                     │
│                                                                             │
│  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐ ┌───────────────────┐  │
│  │ Robot        │ │ State        │ │ Flow         │ │ Action            │  │
│  │ Registry     │ │ Manager      │ │ Executor     │ │ Definition Loader │  │
│  └──────────────┘ └──────────────┘ └──────────────┘ └───────────────────┘  │
│                                                                             │
│  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐                        │
│  │ Waypoint     │ │ Deploy       │ │ Condition    │                        │
│  │ Store        │ │ Service      │ │ Evaluator    │                        │
│  └──────────────┘ └──────────────┘ └──────────────┘                        │
│                                                                             │
│  Database: Neo4j                                                            │
└────────────────────────────────────┬────────────────────────────────────────┘
                                     │ HTTP (Telemetry 1Hz, Command Poll 2Hz)
          ┌──────────────────────────┼──────────────────────────┐
          ▼                          ▼                          ▼
┌──────────────────┐      ┌──────────────────┐      ┌──────────────────┐
│   Robot Agent    │      │   Robot Agent    │      │   Robot Agent    │
│   forklift_001   │      │   forklift_002   │      │   mm_001         │
│   192.168.1.101  │      │   192.168.1.102  │      │   192.168.1.103  │
│                  │      │                  │      │                  │
│ config.yaml:     │      │ config.yaml:     │      │ config.yaml:     │
│  robot_id        │      │  robot_id        │      │  robot_id        │
│  server_url      │      │  server_url      │      │  server_url      │
│                  │      │                  │      │                  │
│     ↕ DDS        │      │     ↕ DDS        │      │     ↕ DDS        │
│   (localhost)    │      │   (localhost)    │      │   (localhost)    │
│                  │      │                  │      │                  │
│  Nav2, Fork      │      │  Nav2, Fork      │      │  Nav2, MoveIt    │
│  Action Servers  │      │  Action Servers  │      │  Action Servers  │
└──────────────────┘      └──────────────────┘      └──────────────────┘
```

### 2.2 데이터 관리 구분

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  파일로 관리 (복사 붙여넣기)                                                  │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  definitions/actions/                                                       │
│  └── *.action 파일들 (ROS2 interface 그대로)                                 │
│                                                                             │
│  → 새 Action 추가: .action 파일 복사 후 서버 재시작                           │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│  DB + UI로 관리 (웹에서 CRUD, Agent에 배포)                                   │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  • State Definition (Robot Type별 상태 목록)                                 │
│  • Action Mapping (Action → State 매핑)                                     │
│  • Flow (작업 시나리오)                                                      │
│  • Waypoint (저장된 위치/자세)                                               │
│  • Robot 등록 정보                                                          │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 3. 폴더 구조

```
fleet_system/
│
├── central_server/
│   ├── backend/
│   │   ├── app/
│   │   │   ├── __init__.py
│   │   │   ├── main.py                     # FastAPI entry point
│   │   │   ├── config.py                   # 환경설정
│   │   │   │
│   │   │   ├── api/                        # REST API 라우터
│   │   │   │   ├── __init__.py
│   │   │   │   ├── robots.py               # 로봇 등록/상태
│   │   │   │   ├── state_definitions.py    # State 정의 CRUD
│   │   │   │   ├── graphs.py                # Flow CRUD
│   │   │   │   ├── tasks.py                # Task 실행/상태
│   │   │   │   ├── waypoints.py            # Waypoint CRUD
│   │   │   │   ├── teach.py                # Teach 기능
│   │   │   │   ├── actions.py              # Action 정의 조회
│   │   │   │   └── deploy.py               # Agent 배포
│   │   │   │
│   │   │   ├── models/                     # Pydantic 모델
│   │   │   │   ├── __init__.py
│   │   │   │   ├── robot.py
│   │   │   │   ├── state_definition.py
│   │   │   │   ├── graph.py
│   │   │   │   ├── task.py
│   │   │   │   ├── waypoint.py
│   │   │   │   └── command.py
│   │   │   │
│   │   │   ├── services/                   # 비즈니스 로직
│   │   │   │   ├── __init__.py
│   │   │   │   ├── action_loader.py        # .action 파일 파싱
│   │   │   │   ├── robot_registry.py       # 로봇 관리
│   │   │   │   ├── state_manager.py        # State 정의 관리
│   │   │   │   ├── graph_executor.py        # Flow 실행 엔진
│   │   │   │   ├── condition_evaluator.py  # 조건 평가
│   │   │   │   ├── waypoint_store.py       # Waypoint 관리
│   │   │   │   └── deploy_service.py       # Agent 배포
│   │   │   │
│   │   │   ├── db/                         # 데이터베이스
│   │   │   │   ├── __init__.py
│   │   │   │   ├── database.py             # DB 연결
│   │   │   │   ├── models.py               # SQLAlchemy 모델
│   │   │   │   └── repositories/
│   │   │   │       ├── robot_repo.py
│   │   │   │       ├── state_repo.py
│   │   │   │       ├── flow_repo.py
│   │   │   │       └── waypoint_repo.py
│   │   │   │
│   │   │   └── websocket/                  # 실시간 통신
│   │   │       ├── __init__.py
│   │   │       └── monitor.py              # 모니터링 WebSocket
│   │   │
│   │   ├── requirements.txt
│   │   ├── Dockerfile
│   │   └── alembic/                        # DB 마이그레이션
│   │
│   └── frontend/
│       ├── src/
│       │   ├── App.tsx
│       │   │
│       │   ├── pages/
│       │   │   ├── Dashboard/              # 대시보드
│       │   │   ├── StateDefinitions/       # State 정의 관리
│       │   │   ├── FlowEditor/             # Flow 편집기
│       │   │   ├── RobotMonitor/           # 로봇 모니터링
│       │   │   ├── Waypoints/              # Waypoint 관리
│       │   │   └── TaskHistory/            # 실행 이력
│       │   │
│       │   ├── components/
│       │   │   ├── StateEditor/            # State 편집 컴포넌트
│       │   │   ├── ActionMappingEditor/    # Action-State 매핑
│       │   │   ├── FlowCanvas/             # Flow 시각화
│       │   │   ├── TeachPanel/             # Teach 기능
│       │   │   ├── RobotCard/              # 로봇 상태 카드
│       │   │   └── ConditionBuilder/       # 조건 편집기
│       │   │
│       │   ├── hooks/
│       │   │   ├── useRobots.ts
│       │   │   ├── useWebSocket.ts
│       │   │   ├── useFlows.ts
│       │   │   └── useWaypoints.ts
│       │   │
│       │   ├── api/
│       │   │   └── client.ts               # API 클라이언트
│       │   │
│       │   └── types/
│       │       └── index.ts                # TypeScript 타입
│       │
│       ├── package.json
│       └── Dockerfile
│
├── robot_agent/
│   ├── robot_agent/
│   │   ├── __init__.py
│   │   ├── agent.py                        # 메인 Agent 노드
│   │   ├── config_loader.py                # config.yaml 로딩
│   │   │
│   │   ├── server_client/                  # 서버 통신
│   │   │   ├── __init__.py
│   │   │   ├── http_client.py              # HTTP 클라이언트
│   │   │   └── command_handler.py          # 명령 처리
│   │   │
│   │   ├── executor/                       # Action 실행
│   │   │   ├── __init__.py
│   │   │   ├── action_executor.py          # Action 실행기
│   │   │   └── state_tracker.py            # State 추적
│   │   │
│   │   └── telemetry/                      # 상태 수집
│   │       ├── __init__.py
│   │       └── collector.py                # Telemetry 수집
│   │
│   ├── config/
│   │   └── config.example.yaml             # 설정 예시
│   │
│   ├── setup.py
│   ├── package.xml
│   └── requirements.txt
│
├── definitions/                            # ⭐ .action 파일만 (복사 붙여넣기)
│   └── actions/
│       ├── nav2_msgs/
│       │   └── NavigateToPose.action
│       ├── control_msgs/
│       │   ├── FollowJointTrajectory.action
│       │   └── GripperCommand.action
│       ├── forklift_msgs/
│       │   ├── LiftFork.action
│       │   └── LowerFork.action
│       └── README.md                       # 등록 가이드
│
├── docker-compose.yaml
└── README.md
```

---

## 4. Action 정의 (파일 관리)

### 4.1 파일 구조

```
definitions/actions/
├── nav2_msgs/
│   └── NavigateToPose.action
├── control_msgs/
│   ├── FollowJointTrajectory.action
│   └── GripperCommand.action
└── forklift_msgs/
    ├── LiftFork.action
    └── LowerFork.action
```

### 4.2 .action 파일 형식 (ROS2 표준)

```
# definitions/actions/nav2_msgs/NavigateToPose.action

#goal definition
geometry_msgs/PoseStamped pose
string behavior_tree
---
#result definition
std_msgs/Empty result
---
#feedback definition
geometry_msgs/PoseStamped current_pose
builtin_interfaces/Duration navigation_time
builtin_interfaces/Duration estimated_time_remaining
int16 number_of_recoveries
float32 distance_remaining
```

```
# definitions/actions/control_msgs/FollowJointTrajectory.action

#goal definition
trajectory_msgs/JointTrajectory trajectory
trajectory_msgs/JointTolerance[] path_tolerance
trajectory_msgs/JointTolerance[] goal_tolerance
builtin_interfaces/Duration goal_time_tolerance
---
#result definition
int32 error_code
int32 SUCCESSFUL = 0
int32 INVALID_GOAL = -1
int32 INVALID_JOINTS = -2
int32 OLD_HEADER_TIMESTAMP = -3
int32 PATH_TOLERANCE_VIOLATED = -4
int32 GOAL_TOLERANCE_VIOLATED = -5
string error_string
---
#feedback definition
Header header
string[] joint_names
trajectory_msgs/JointTrajectoryPoint desired
trajectory_msgs/JointTrajectoryPoint actual
trajectory_msgs/JointTrajectoryPoint error
```

### 4.3 새 Action 등록 방법

```bash
# 1. ROS2 패키지에서 .action 파일 위치 찾기
ros2 pkg prefix nav2_msgs
# → /opt/ros/humble/share/nav2_msgs

# 2. 해당 패키지의 action 폴더에서 복사
cp /opt/ros/humble/share/nav2_msgs/action/NavigateToPose.action \
   definitions/actions/nav2_msgs/

# 3. 서버 재시작 (자동 로딩)

# 4. UI에서 Action Mapping 설정
```

### 4.4 Action Loader 동작

```python
# 서버 시작 시
# 1. definitions/actions/ 디렉토리 스캔
# 2. 모든 .action 파일 파싱
# 3. Goal/Result/Feedback 필드 구조 추출
# 4. 메모리에 캐시

# 결과 예시:
{
    "nav2_msgs/NavigateToPose": {
        "goal_fields": [
            {"name": "pose", "type": "geometry_msgs/PoseStamped"},
            {"name": "behavior_tree", "type": "string"}
        ],
        "result_fields": [...],
        "feedback_fields": [...]
    },
    "control_msgs/FollowJointTrajectory": {...}
}
```

---

## 5. State Definition (DB/UI 관리)

### 5.1 데이터 모델

```python
# State Definition (Robot Type)
{
    "id": "type_forklift",
    "name": "Forklift",
    "description": "지게차 로봇",
    
    # 이 로봇 타입이 가질 수 있는 State 목록
    "states": ["idle", "navigating", "lifting", "lowering", "error"],
    "default_state": "idle",
    
    # Action → State 매핑
    "action_mappings": [
        {
            "action_type": "nav2_msgs/NavigateToPose",
            "server": "/navigate_to_pose",
            "during_state": "navigating"
        },
        {
            "action_type": "forklift_msgs/LiftFork",
            "server": "/lift_fork",
            "during_state": "lifting"
        },
        {
            "action_type": "forklift_msgs/LowerFork",
            "server": "/lower_fork",
            "during_state": "lowering"
        }
    ],
    
    # Telemetry 수집 토픽
    "telemetry_topics": {
        "pose": "/amcl_pose",
        "battery": "/battery_state",
        "fork_height": "/fork_height"
    },
    
    # Teach 가능한 Waypoint 타입
    "teachable_waypoints": ["pose_2d"],
    
    # 버전 관리
    "version": 3,
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-15T10:00:00Z"
}
```

```python
# Mobile Manipulator 예시
{
    "id": "type_mobile_manipulator",
    "name": "Mobile Manipulator",
    "description": "AMR + 로봇팔",
    
    "states": ["idle", "navigating", "moving_arm", "gripping", "error"],
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
        "joint_states": "/joint_states",
        "eef_pose": "/eef_pose",
        "gripper_state": "/gripper_state",
        "battery": "/battery_state"
    },
    
    "teachable_waypoints": ["pose_2d", "joint_state", "pose_3d"],
    
    "version": 1
}
```

### 5.2 State 전환 규칙

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  State 전환 규칙                                                             │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  1. 기본 상태: default_state (보통 "idle")                                   │
│                                                                             │
│  2. Action 시작 → during_state로 전환                                        │
│     예: NavigateToPose 시작 → state = "navigating"                          │
│                                                                             │
│  3. Action 완료/실패 → "idle"로 복귀                                         │
│                                                                             │
│  4. 에러 발생 → "error" 상태                                                 │
│                                                                             │
│  전환 예시:                                                                  │
│  idle → (NavigateToPose 시작) → navigating → (완료) → idle                  │
│  idle → (LiftFork 시작) → lifting → (완료) → idle                           │
│  navigating → (에러 발생) → error                                           │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 6. Robot Agent

### 6.1 설정 파일

```yaml
# /etc/robot_agent/config.yaml (각 로봇에 배포)

robot_id: "forklift_001"
server_url: "http://192.168.0.200:8080"

# 통신 설정
telemetry_rate_hz: 1.0                    # 상태 보고 주기
command_poll_rate_hz: 2.0                 # 명령 폴링 주기

# 타임아웃
server_timeout_sec: 5.0
action_default_timeout_sec: 120.0
```

### 6.2 Agent 역할 (단순화)

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  Robot Agent의 역할                                                          │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ✅ 하는 것                                                                  │
│  ─────────────────────────────────────────────────                          │
│  • 자신의 상태(Telemetry) 수집 및 서버에 보고 (1Hz)                           │
│  • 서버로부터 명령 수신 (Command Poll, 2Hz)                                  │
│  • ROS2 Action 실행 (로컬 DDS)                                              │
│  • Action 실행 중 State 자동 전환                                           │
│  • Precondition 평가 (자신 상태 + 서버에서 받은 Fleet 상태)                   │
│  • 실행 결과 보고                                                            │
│                                                                             │
│  ❌ 안 하는 것                                                               │
│  ─────────────────────────────────────────────────                          │
│  • Flow 로직 관리 (서버 역할)                                                │
│  • 다음 Step 결정 (서버 역할)                                                │
│  • 복잡한 조건 평가 (서버 역할)                                              │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 6.3 동작 흐름

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  Robot Agent 동작 흐름                                                       │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  1. 시작                                                                    │
│     └─ config.yaml 로드                                                     │
│     └─ 서버에 연결 등록                                                      │
│        POST /api/robots/connect                                             │
│        {robot_id, agent_id, ip_address}                                     │
│     └─ State Definition 수신 (action_mappings 포함)                         │
│                                                                             │
│  2. 메인 루프                                                               │
│     ┌────────────────────────────────────────────────────────────────┐     │
│     │  Telemetry Thread (1Hz)                                        │     │
│     │  └─ ROS2 토픽에서 데이터 수집                                    │     │
│     │  └─ 현재 State 계산                                             │     │
│     │  └─ POST /api/robots/{id}/telemetry                            │     │
│     └────────────────────────────────────────────────────────────────┘     │
│     ┌────────────────────────────────────────────────────────────────┐     │
│     │  Command Poll Thread (2Hz)                                     │     │
│     │  └─ GET /api/robots/{id}/commands                              │     │
│     │  └─ 응답에 포함된 fleet_states 저장                              │     │
│     │  └─ 명령 있으면 처리                                            │     │
│     └────────────────────────────────────────────────────────────────┘     │
│                                                                             │
│  3. 명령 처리                                                               │
│     └─ EXECUTE_STEP: Precondition 확인 → Action 실행 → 결과 보고           │
│     └─ CANCEL: 현재 Action 취소                                            │
│     └─ UPDATE_CONFIG: State Definition 업데이트                            │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 6.4 Telemetry 구조

```json
{
    "robot_id": "forklift_001",
    "timestamp": "2024-01-15T10:30:00.500Z",
    
    "state": "navigating",
    
    "action": {
        "status": "executing",
        "action_type": "nav2_msgs/NavigateToPose",
        "started_at": "2024-01-15T10:29:55.000Z"
    },
    
    "telemetry": {
        "pose": {
            "x": 1.5,
            "y": 2.3,
            "theta": 0.5
        },
        "battery": {
            "percent": 85.0,
            "is_charging": false
        },
        "fork_height": 0.0
    },
    
    "action_servers": {
        "/navigate_to_pose": "busy",
        "/lift_fork": "available",
        "/lower_fork": "available"
    }
}
```

### 6.5 Fleet States (서버에서 수신)

```json
// GET /api/robots/{id}/commands 응답에 포함
{
    "commands": [...],
    
    "fleet_states": {
        "forklift_001": {
            "state": "navigating",
            "pose": {"x": 1.5, "y": 2.3, "theta": 0.5}
        },
        "forklift_002": {
            "state": "idle",
            "pose": {"x": 5.0, "y": 3.0, "theta": 1.57}
        },
        "mm_001": {
            "state": "gripping",
            "pose": {"x": 2.0, "y": 4.0, "theta": 0.0}
        }
    }
}
```

---

## 7. Action Flow

### 7.1 Flow 구조

```yaml
# Flow 예시: pick_and_place.yaml

id: "flow_001"
name: "Pick and Place"
description: "물체를 픽업하여 드롭 위치로 이동"
version: 2

# Flow 전체 Precondition
preconditions:
  - condition: "self.state == 'idle'"
    message: "로봇이 대기 상태여야 합니다"
  - condition: "self.battery > 20"
    message: "배터리가 20% 이상이어야 합니다"

steps:
  # === 정상 흐름 ===
  
  - id: "move_to_pick"
    name: "픽업 위치로 이동"
    
    action:
      type: "nav2_msgs/NavigateToPose"
      server: "/navigate_to_pose"
      params:
        source: "waypoint"
        waypoint_id: "wp_pickup_location"
      timeout_sec: 120
    
    transition:
      on_success: "approach_object"
      on_failure: "fallback_home"

  - id: "approach_object"
    name: "물체 접근 (팔 이동)"
    
    preconditions:
      - condition: "self.state == 'idle'"
    
    action:
      type: "control_msgs/FollowJointTrajectory"
      server: "/arm_controller/follow_joint_trajectory"
      params:
        source: "waypoint"
        waypoint_id: "wp_approach_pose"
      timeout_sec: 30
    
    transition:
      on_success: "grip_object"
      on_failure:
        retry: 2
        fallback: "fallback_arm_home"

  - id: "grip_object"
    name: "물체 파지"
    
    action:
      type: "control_msgs/GripperCommand"
      server: "/gripper_controller/gripper_cmd"
      params:
        source: "inline"
        data:
          position: 0.0
          max_effort: 50.0
      timeout_sec: 10
    
    transition:
      on_success:
        # 상태 기반 조건
        condition: "self.gripper_has_object == true"
        next: "confirm_grip"
        else: "grip_object"  # 재시도
        max_retry: 3
        fallback: "fallback_release"

  - id: "confirm_grip"
    name: "파지 확인 (수동)"
    
    # Action 없이 조건만 대기
    wait_for:
      type: "manual_confirm"
      message: "물체가 제대로 잡혔는지 확인하세요"
      timeout_sec: 60
    
    transition:
      on_confirm: "move_to_place"
      on_cancel: "release_and_retry"
      on_timeout: "fallback_release"

  - id: "move_to_place"
    name: "드롭 위치로 이동"
    
    preconditions:
      # 다른 로봇 상태 확인
      - condition: "fleet.forklift_001.state != 'navigating'"
        message: "지게차 1이 이동 중이 아니어야 합니다"
    
    action:
      type: "nav2_msgs/NavigateToPose"
      server: "/navigate_to_pose"
      params:
        source: "waypoint"
        waypoint_id: "wp_drop_location"
      timeout_sec: 120
    
    transition:
      on_success: "release_object"
      on_failure: "fallback_with_object"

  - id: "release_object"
    name: "물체 놓기"
    
    action:
      type: "control_msgs/GripperCommand"
      server: "/gripper_controller/gripper_cmd"
      params:
        source: "inline"
        data:
          position: 0.08
      timeout_sec: 10
    
    transition:
      on_success: "done"
      on_failure:
        retry: 2
        fallback: "error_stop"

  # === Fallback 흐름 ===

  - id: "fallback_home"
    name: "Fallback: 홈으로 복귀"
    type: "fallback"
    
    action:
      type: "nav2_msgs/NavigateToPose"
      params:
        source: "waypoint"
        waypoint_id: "wp_home"
    
    transition:
      on_success: "error_stop"
      on_failure: "error_stop"

  - id: "fallback_arm_home"
    name: "Fallback: 팔 홈 위치"
    type: "fallback"
    
    action:
      type: "control_msgs/FollowJointTrajectory"
      params:
        source: "waypoint"
        waypoint_id: "wp_arm_home"
    
    transition:
      on_success: "error_stop"
      on_failure: "error_stop"

  - id: "fallback_release"
    name: "Fallback: 그리퍼 열기"
    type: "fallback"
    
    action:
      type: "control_msgs/GripperCommand"
      params:
        source: "inline"
        data:
          position: 0.08
    
    transition:
      on_success: "fallback_arm_home"
      on_failure: "error_stop"

  - id: "fallback_with_object"
    name: "Fallback: 물체 들고 홈으로"
    type: "fallback"
    
    action:
      type: "nav2_msgs/NavigateToPose"
      params:
        source: "waypoint"
        waypoint_id: "wp_home"
    
    transition:
      on_success: "error_stop"
      on_failure: "error_stop"

  # === 종료 상태 ===

  - id: "done"
    type: "terminal"
    terminal_type: "success"

  - id: "error_stop"
    type: "terminal"
    terminal_type: "failure"
    alert: true
    message: "작업 실패 - 수동 개입 필요"

created_at: "2024-01-01T00:00:00Z"
updated_at: "2024-01-15T10:00:00Z"
```

### 7.2 Flow 상태 머신

```
                          ┌──────────────┐
                          │ move_to_pick │
                          └──────┬───────┘
                   success       │       failure
                ┌────────────────┴────────────────┐
                ▼                                 ▼
        ┌───────────────┐                 ┌──────────────┐
        │approach_object│                 │fallback_home │
        └───────┬───────┘                 └──────┬───────┘
                │                                │
                ▼                                ▼
        ┌───────────────┐                 ┌──────────────┐
        │  grip_object  │◄─────retry──────│  error_stop  │
        └───────┬───────┘                 └──────────────┘
                │ condition:                      ▲
                │ gripper_has_object              │
                ▼                                 │
        ┌───────────────┐                         │
        │ confirm_grip  │ ←── manual_confirm      │
        └───────┬───────┘                         │
                │                                 │
                ▼                                 │
        ┌───────────────┐                         │
        │ move_to_place │                         │
        │               │ precondition:           │
        │               │ fleet.forklift_001      │
        │               │ .state != navigating    │
        └───────┬───────┘                         │
                │                                 │
                ▼                                 │
        ┌───────────────┐                         │
        │release_object │─────────────────────────┘
        └───────┬───────┘
                │
                ▼
        ┌───────────────┐
        │     done      │ ✓
        └───────────────┘
```

### 7.3 Transition 조건 타입

```yaml
# 1. 단순 성공/실패
transition:
  on_success: "next_step"
  on_failure: "fallback_step"

# 2. 재시도
transition:
  on_failure:
    retry: 3                    # 3번까지 재시도
    fallback: "fallback_step"   # 재시도 초과 시

# 3. 상태 기반 조건
transition:
  on_success:
    condition: "self.gripper_has_object == true"
    next: "continue_step"
    else: "retry_step"

# 4. 다른 로봇 상태 조건
transition:
  on_success:
    condition: "fleet.forklift_001.state == 'idle'"
    next: "continue_step"
    wait: true                  # 조건 충족까지 대기
    timeout_sec: 30

# 5. 수동 확인
wait_for:
  type: "manual_confirm"
  message: "확인 버튼을 누르세요"
  timeout_sec: 60
transition:
  on_confirm: "next_step"
  on_cancel: "cancel_step"
  on_timeout: "timeout_step"
```

### 7.4 파라미터 소스

```yaml
# 1. Waypoint 참조
params:
  source: "waypoint"
  waypoint_id: "wp_pickup_location"

# 2. 직접 입력
params:
  source: "inline"
  data:
    position: 0.0
    max_effort: 50.0

# 3. 동적 입력 (실행 시 UI에서 입력)
params:
  source: "dynamic"
  fields:
    - name: "x"
      type: "float"
      label: "X 좌표"
    - name: "y"
      type: "float"
      label: "Y 좌표"
```

---

## 8. Waypoint / Teach

### 8.1 Waypoint 타입

| 타입 | 설명 | 데이터 구조 |
|------|------|-------------|
| pose_2d | AMR 2D 위치 | {x, y, theta, frame_id} |
| joint_state | 관절 각도 | {joint_1: val, joint_2: val, ...} |
| pose_3d | 3D 위치/자세 | {x, y, z, qx, qy, qz, qw, frame_id} |
| gripper | 그리퍼 위치 | {position, max_effort} |

### 8.2 Waypoint 데이터 모델

```json
{
    "id": "wp_001",
    "name": "픽업 위치",
    "waypoint_type": "pose_2d",
    
    "data": {
        "x": 3.5,
        "y": 2.0,
        "theta": 1.57,
        "frame_id": "map"
    },
    
    "created_by": "teach",
    "created_at": "2024-01-15T10:00:00Z",
    "description": "팔레트 픽업 위치",
    "tags": ["pickup", "zone_a"]
}
```

```json
{
    "id": "wp_002",
    "name": "픽업 자세",
    "waypoint_type": "joint_state",
    
    "data": {
        "joint_1": 0.5,
        "joint_2": -1.2,
        "joint_3": 0.8,
        "joint_4": -0.3,
        "joint_5": 0.0,
        "joint_6": 0.2,
        
        "_eef_pose": {
            "x": 0.4,
            "y": 0.0,
            "z": 0.3,
            "qx": 0.0,
            "qy": 0.707,
            "qz": 0.0,
            "qw": 0.707
        }
    },
    
    "created_by": "teach",
    "created_at": "2024-01-15T10:05:00Z"
}
```

### 8.3 Teach 흐름

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  Teach 흐름                                                                  │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  1. 로봇을 원하는 위치/자세로 수동 조작                                        │
│                                                                             │
│  2. Web UI에서 [현재 상태 저장] 클릭                                          │
│     - 로봇 선택                                                              │
│     - Waypoint 타입 선택 (pose_2d, joint_state 등)                          │
│     - 이름 입력                                                              │
│                                                                             │
│  3. POST /api/robots/{robot_id}/teach                                       │
│     {                                                                       │
│       "waypoint_type": "joint_state",                                       │
│       "name": "픽업 자세"                                                    │
│     }                                                                       │
│                                                                             │
│  4. Server → Agent: 현재 상태 요청                                           │
│     (Agent의 최신 Telemetry에서 해당 데이터 추출)                             │
│                                                                             │
│  5. Waypoint 생성 및 DB 저장                                                 │
│                                                                             │
│  6. Flow에서 waypoint_id로 참조 가능                                         │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 9. API 명세

### 9.1 Robot APIs

| Method | Endpoint | 설명 |
|--------|----------|------|
| POST | /api/robots/connect | Agent 연결 등록 |
| POST | /api/robots/{id}/telemetry | 상태 보고 (1Hz) |
| GET | /api/robots | 로봇 목록 (staleness 포함) |
| GET | /api/robots/{id} | 로봇 상세 |
| GET | /api/robots/{id}/commands | Agent가 명령 가져가기 (fleet_states 포함) |
| POST | /api/robots/{id}/commands/{cmd_id}/result | 명령 결과 보고 |

### 9.2 State Definition APIs

| Method | Endpoint | 설명 |
|--------|----------|------|
| GET | /api/state-definitions | State 정의 목록 |
| POST | /api/state-definitions | State 정의 생성 |
| GET | /api/state-definitions/{id} | State 정의 상세 |
| PUT | /api/state-definitions/{id} | State 정의 수정 |
| DELETE | /api/state-definitions/{id} | State 정의 삭제 |
| POST | /api/state-definitions/{id}/deploy | Agent에 배포 |

### 9.3 Action APIs

| Method | Endpoint | 설명 |
|--------|----------|------|
| GET | /api/actions | 사용 가능한 Action 목록 (.action 파일 기반) |
| GET | /api/actions/{type} | Action 상세 (Goal/Result/Feedback 구조) |

### 9.4 Flow APIs

| Method | Endpoint | 설명 |
|--------|----------|------|
| GET | /api/flows | Flow 목록 |
| POST | /api/flows | Flow 생성 |
| GET | /api/flows/{id} | Flow 상세 |
| PUT | /api/flows/{id} | Flow 수정 |
| DELETE | /api/flows/{id} | Flow 삭제 |
| POST | /api/flows/{id}/execute | Flow 실행 시작 |
| POST | /api/flows/{id}/validate | Flow 유효성 검사 |

### 9.5 Task APIs

| Method | Endpoint | 설명 |
|--------|----------|------|
| GET | /api/tasks | Task 목록 |
| GET | /api/tasks/{id} | Task 상태 조회 |
| POST | /api/tasks/{id}/cancel | Task 취소 |
| POST | /api/tasks/{id}/pause | Task 일시정지 |
| POST | /api/tasks/{id}/resume | Task 재개 |
| POST | /api/tasks/{id}/confirm | 수동 확인 응답 |

### 9.6 Waypoint APIs

| Method | Endpoint | 설명 |
|--------|----------|------|
| GET | /api/waypoints | Waypoint 목록 |
| POST | /api/waypoints | Waypoint 생성 (수동) |
| GET | /api/waypoints/{id} | Waypoint 상세 |
| PUT | /api/waypoints/{id} | Waypoint 수정 |
| DELETE | /api/waypoints/{id} | Waypoint 삭제 |
| POST | /api/robots/{id}/teach | Teach로 Waypoint 생성 |

### 9.7 WebSocket

| Endpoint | 설명 |
|----------|------|
| /ws/monitor | 실시간 모니터링 (1Hz 브로드캐스트) |
| /ws/task/{id} | 특정 Task 실시간 상태 |

---

## 10. 모니터링

### 10.1 WebSocket 메시지 (1Hz)

```json
{
    "timestamp": "2024-01-15T10:30:00.500Z",
    
    "robots": [
        {
            "id": "forklift_001",
            "name": "지게차 1호",
            "type": "forklift",
            
            "state": "navigating",
            "is_online": true,
            "staleness_sec": 0.5,
            
            "telemetry": {
                "pose": {"x": 1.5, "y": 2.3, "theta": 0.5},
                "battery": {"percent": 85.0},
                "fork_height": 0.0
            },
            
            "current_task": {
                "id": "task_001",
                "flow_name": "Transport Pallet",
                "current_step": "move_to_pick",
                "step_index": 1,
                "total_steps": 5
            },
            
            "action_servers": {
                "/navigate_to_pose": "busy",
                "/lift_fork": "available"
            }
        },
        {
            "id": "mm_001",
            "name": "모바일 매니퓰레이터",
            "type": "mobile_manipulator",
            
            "state": "idle",
            "is_online": true,
            "staleness_sec": 0.3,
            
            "telemetry": {
                "pose": {"x": 2.0, "y": 4.0, "theta": 0.0},
                "battery": {"percent": 72.0}
            },
            
            "current_task": null
        }
    ],
    
    "tasks": [
        {
            "id": "task_001",
            "flow_id": "flow_001",
            "flow_name": "Transport Pallet",
            "robot_id": "forklift_001",
            "status": "running",
            "current_step_id": "move_to_pick",
            "started_at": "2024-01-15T10:28:00Z",
            "progress": {
                "current": 1,
                "total": 5
            }
        }
    ]
}
```

### 10.2 Staleness 계산

```python
staleness_sec = current_time - robot.last_seen

# 상태 판정
if staleness_sec < 3:
    status = "online"       # 정상
elif staleness_sec < 10:
    status = "warning"      # 경고 (연결 불안정)
else:
    status = "offline"      # 오프라인
```

---

## 11. 배포 흐름

### 11.1 State Definition 배포

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  State Definition 배포                                                       │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  1. UI에서 State/Action Mapping 수정                                         │
│       ↓                                                                     │
│  2. [Save] → DB에 저장, version 증가                                        │
│       ↓                                                                     │
│  3. [Deploy] 클릭                                                           │
│       ↓                                                                     │
│  4. Server: 해당 Robot Type의 연결된 Agent들 조회                            │
│       ↓                                                                     │
│  5. 각 Agent의 Command Queue에 UPDATE_CONFIG 명령 추가                       │
│       ↓                                                                     │
│  6. Agent: Command Poll 시 새 설정 수신                                      │
│       ↓                                                                     │
│  7. Agent: 설정 적용 (State 목록, Action Mapping 업데이트)                   │
│       ↓                                                                     │
│  8. Agent: 적용 완료 보고                                                    │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 11.2 Flow 배포

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  Flow는 Server에서 관리, Agent에 직접 배포하지 않음                           │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  • Flow는 Server DB에만 저장                                                 │
│  • 실행 시 Server가 Step별로 Agent에 명령 전송                               │
│  • Agent는 Flow 전체를 알 필요 없음 (현재 Step만 실행)                        │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 12. DB 스키마

### 12.1 테이블 구조

```sql
-- 로봇 등록
CREATE TABLE robots (
    id VARCHAR(50) PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    type_id VARCHAR(50) REFERENCES state_definitions(id),
    ip_address VARCHAR(45),
    last_seen TIMESTAMP,
    last_telemetry JSONB,
    created_at TIMESTAMP DEFAULT NOW()
);

-- State 정의
CREATE TABLE state_definitions (
    id VARCHAR(50) PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    description TEXT,
    states JSONB NOT NULL,              -- ["idle", "navigating", ...]
    default_state VARCHAR(50),
    action_mappings JSONB NOT NULL,     -- [{action_type, server, during_state}]
    telemetry_topics JSONB,
    teachable_waypoints JSONB,
    version INTEGER DEFAULT 1,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Waypoint
CREATE TABLE waypoints (
    id VARCHAR(50) PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    waypoint_type VARCHAR(50) NOT NULL,
    data JSONB NOT NULL,
    created_by VARCHAR(20),             -- "teach" or "manual"
    description TEXT,
    tags JSONB,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Flow
CREATE TABLE flows (
    id VARCHAR(50) PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    description TEXT,
    preconditions JSONB,
    steps JSONB NOT NULL,
    version INTEGER DEFAULT 1,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Task (실행 인스턴스)
CREATE TABLE tasks (
    id VARCHAR(50) PRIMARY KEY,
    flow_id VARCHAR(50) REFERENCES flows(id),
    robot_id VARCHAR(50) REFERENCES robots(id),
    status VARCHAR(20) NOT NULL,        -- pending, running, completed, failed, cancelled
    current_step_id VARCHAR(50),
    step_results JSONB,
    error_message TEXT,
    created_at TIMESTAMP DEFAULT NOW(),
    started_at TIMESTAMP,
    completed_at TIMESTAMP
);

-- Command Queue
CREATE TABLE command_queue (
    id VARCHAR(50) PRIMARY KEY,
    robot_id VARCHAR(50) REFERENCES robots(id),
    command_type VARCHAR(50) NOT NULL,
    payload JSONB,
    status VARCHAR(20) DEFAULT 'pending',
    created_at TIMESTAMP DEFAULT NOW(),
    processed_at TIMESTAMP
);
```

---

## 13. 개발 우선순위

### Phase 1: 기본 인프라 (Week 1-2)

```
[ ] 프로젝트 구조 생성
[ ] DB 설정 (Neo4j)
[ ] Action Loader (.action 파일 파싱)
[ ] 기본 API 구조 (FastAPI)
[ ] Robot Agent 기본 구조
    [ ] config.yaml 로딩
    [ ] 서버 연결
    [ ] Telemetry 보고
    [ ] Command Poll
```

### Phase 2: State & Robot 관리 (Week 2-3)

```
[ ] State Definition CRUD API
[ ] State Definition UI
    [ ] 목록/상세 화면
    [ ] 편집 화면 (State 추가/삭제)
    [ ] Action Mapping 편집
[ ] Robot 등록/관리
[ ] WebSocket 모니터링 (1Hz)
[ ] 모니터링 대시보드 UI
```

### Phase 3: Waypoint & Teach (Week 3-4)

```
[ ] Waypoint CRUD API
[ ] Waypoint UI
[ ] Teach API
[ ] Teach UI
[ ] Agent Telemetry 확장 (joint_states, eef_pose)
```

### Phase 4: Flow 기본 (Week 4-5)

```
[ ] Flow 모델 설계
[ ] Flow CRUD API
[ ] Flow Editor UI (기본)
    [ ] Step 추가/삭제
    [ ] Action 선택
    [ ] Waypoint 연결
[ ] Flow Executor (순차 실행)
[ ] Task 관리
```

### Phase 5: Flow 고급 (Week 5-6)

```
[ ] Transition 조건 (성공/실패/재시도)
[ ] Fallback Step
[ ] Precondition 평가
[ ] Fleet States 공유
[ ] 수동 확인 (manual_confirm)
[ ] Flow Editor UI 고급
    [ ] 시각적 연결선
    [ ] 조건 편집기
```

### Phase 6: 배포 & 안정화 (Week 6-7)

```
[ ] State Definition 배포
[ ] Docker 구성
[ ] 에러 처리 강화
[ ] 로깅/모니터링
[ ] 테스트 작성
[ ] 문서화
```

---

## 14. 기술 스택

### Backend
- Python 3.10+
- FastAPI
- Neo4j (Cypher)
- WebSocket (fastapi-websockets)
- Pydantic

### Frontend
- React 18+
- TypeScript
- TailwindCSS
- React Query
- React Flow (Flow 시각화)

### Robot Agent
- Python 3.10+
- ROS2 Humble
- rclpy
- requests (HTTP 클라이언트)

### Infrastructure
- Docker / Docker Compose
- Neo4j
- Nginx (리버스 프록시)

---

## 15. 참고 사항

### 15.1 ROS2 네트워크 분리

```
Central Server (192.168.0.x)
├── DDS 없음
├── HTTP/WebSocket만 사용
└── 모든 ROS2 통신은 Agent가 담당

Robot Agent (192.168.1.x)
├── 로컬 DDS만 사용 (localhost)
├── 외부 DDS 연결 없음
└── 서버와는 HTTP로만 통신
```

### 15.2 확장 고려사항

- 다중 서버 (HA 구성)
- 메시지 큐 (Redis, RabbitMQ)
- 이벤트 소싱
- 로봇 시뮬레이션 연동
- 모바일 앱 지원
