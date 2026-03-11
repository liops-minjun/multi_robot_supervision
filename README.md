# Multi-Robot Supervision System

다양한 로봇(AMR, 로봇팔, 지게차 등)을 중앙에서 관리하는 Fleet Management System입니다.

## 주요 기능

| 기능 | 설명 |
|------|------|
| **실시간 모니터링** | WebSocket을 통한 로봇 상태, 위치, 텔레메트리 실시간 확인 |
| **Behavior Tree 편집기** | 드래그 앤 드롭 노드 기반 작업 시나리오 구성 (React Flow) |
| **Capability Auto-Discovery** | ROS2 Action Server 자동 탐지 및 스키마 추출 - 설정 불필요 |
| **텔레메트리 캡처** | 로봇팔 티칭: 현재 자세(joint_state)를 클릭 한 번으로 저장 |
| **Multi-Robot 협업** | 로봇 간 상태 기반 조건부 실행 (Precondition) |
| **Agent 자동 ID 할당** | Hardware Fingerprint 기반 자동 ID 할당 및 재연결 시 복구 |
| **Template 시스템** | Behavior Tree 템플릿 생성 및 다중 Agent 할당 |

## 아키텍처

```
┌─────────────────────────────────────────────────────────────────┐
│                      Central Server (서버)                       │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐              │
│  │   React     │  │  Go Backend │  │   Neo4j     │              │
│  │  Frontend   │◄─┤  REST/WS    │◄─┤  Graph DB   │              │
│  │   :3000     │  │   :8081     │  │  :7474/7687 │              │
│  └─────────────┘  └──────┬──────┘  └─────────────┘              │
└──────────────────────────┼──────────────────────────────────────┘
                           │ QUIC (UDP :9444)
          ┌────────────────┼────────────────┐
          ▼                ▼                ▼
   ┌────────────┐   ┌────────────┐   ┌────────────┐
   │   Agent    │   │   Agent    │   │   Agent    │  ← 로봇마다 1개
   │   (C++)    │   │   (C++)    │   │   (C++)    │
   │  ↕ ROS2    │   │  ↕ ROS2    │   │  ↕ ROS2    │
   └────────────┘   └────────────┘   └────────────┘
```

---

# Central Server (서버 측)

서버는 Docker로 쉽게 실행할 수 있습니다.

## Quick Start

```bash
# 1. 클론
git clone https://github.com/liops2023/multi-robot-supervision.git
cd multi-robot-supervision

# 2. 실행 (Neo4j + Backend + Frontend)
docker-compose up -d

# 3. 접속
open http://localhost:3000
```

브라우저에서 `http://localhost:3000`으로 접속하세요.

## 서비스 포트

| 서비스 | 포트 | 설명 |
|--------|------|------|
| **Frontend** | 3000 | Web UI (React SPA) |
| **Backend API** | 8081 | REST API + WebSocket |
| **QUIC (Agent)** | 9444/UDP | C++ Agent 통신 (Raw QUIC) |
| **Neo4j Browser** | 7474 | DB 관리 (neo4j/neo4j123) |
| **Neo4j Bolt** | 7687 | Bolt 프로토콜 |

## 주요 명령어

```bash
# 로그 확인
docker-compose logs -f

# 특정 서비스 로그
docker-compose logs -f go-backend

# 중지
docker-compose down

# 데이터 초기화 (주의!)
docker-compose down -v && docker-compose up -d
```

## 개발 모드 (Docker 없이)

```bash
# 필요: Go 1.21+, Node.js 18+, Neo4j

# Neo4j만 Docker로
docker run -d --name neo4j \
  -e NEO4J_AUTH=neo4j/neo4j123 \
  -p 7474:7474 -p 7687:7687 neo4j:5

# Backend + Frontend 동시 실행
./scripts/dev.sh
```

---

# Agent (로봇 측)

Agent는 각 로봇에서 실행되며 ROS2 환경이 필요합니다. Agent는 서버에 연결하면 자동으로 ID가 할당되고, 재시작해도 동일한 Agent로 인식됩니다.

## 요구 사항

- Ubuntu 22.04
- ROS2 Humble
- 서버와 네트워크 연결 (UDP 9444)

## 설치

### 1. 의존성 설치

```bash
# ROS2 Humble 설치 (이미 설치되어 있다면 건너뛰기)
sudo apt install -y ros-humble-desktop

# 빌드 도구 및 라이브러리
sudo apt install -y \
  build-essential cmake \
  libtbb-dev libssl-dev \
  libyaml-cpp-dev libspdlog-dev \
  nlohmann-json3-dev \
  protobuf-compiler libprotobuf-dev

# MsQuic (QUIC 통신)
wget -q https://packages.microsoft.com/config/ubuntu/22.04/packages-microsoft-prod.deb
sudo dpkg -i packages-microsoft-prod.deb && rm packages-microsoft-prod.deb
sudo apt update && sudo apt install -y libmsquic
```

### 2. 빌드

```bash
cd ros2_robot_agent
source /opt/ros/humble/setup.bash
colcon build --symlink-install
source install/setup.bash
```

### 3. 실행

```bash
# 서버 IP 지정하여 실행 (Agent ID는 서버가 자동 할당)
ros2 launch ros2_robot_agent robot_agent.launch.py server_ip:=<서버_IP>

# 예시: 서버가 192.168.0.100인 경우
ros2 launch ros2_robot_agent robot_agent.launch.py server_ip:=192.168.0.100
```

## Launch 파라미터

| 파라미터 | 기본값 | 설명 |
|----------|--------|------|
| `server_ip` | localhost | Central Server IP |
| `server_port` | 9444 | QUIC 포트 |
| `log_level` | info | 로그 레벨 (debug/info/warn/error) |

## 설정 파일 (선택)

기본 설정으로 충분하지만, 커스텀 설정이 필요한 경우:

```bash
# 설정 파일 복사
cp install/ros2_robot_agent/share/ros2_robot_agent/config/agent.yaml ~/my_config.yaml

# 수정 후 실행
ros2 launch ros2_robot_agent robot_agent.launch.py \
  config:=~/my_config.yaml \
  server_ip:=192.168.0.100
```

### 설정 예시 (agent.yaml)

```yaml
# Agent ID/Name을 비워두면 서버가 Hardware Fingerprint 기반으로 자동 할당
agent:
  id: ""                           # 빈 값 → 서버가 자동 할당
  name: ""                         # 빈 값 → "Agent-001" 등 순차 할당
  use_server_assigned_id: true     # 재시작 시에도 동일한 ID 유지

server:
  quic:
    server_address: "192.168.0.200"
    server_port: 9444
    enable_0rtt: true              # 빠른 재연결
    enable_datagrams: true         # 저지연 텔레메트리

communication:
  heartbeat_interval_ms: 100       # 하트비트 주기

storage:
  behavior_trees_path: "/tmp/robot_agent/graphs"
  agent_id_path: "/tmp/robot_agent/agent_id"  # 서버 할당 ID 저장 경로
```

## 연결 확인

Agent 실행 후 서버의 Web UI에서 확인:

1. `http://서버IP:3000` 접속
2. 좌측 사이드바에서 연결된 Agent 확인 (녹색 점: 온라인)
3. Agent의 ROS2 Action Server들이 자동으로 등록됨

---

# 사용 가이드

## 1. Behavior Tree 만들기

1. Web UI에서 **Behavior Tree** 메뉴 클릭
2. **+ 새 템플릿** 버튼 클릭
3. 템플릿 ID와 이름 입력
4. 우측 패널에서 **DISCOVERED ACTIONS**의 액션을 캔버스로 드래그
5. 노드 간 연결 (성공/실패 시 다음 단계)
6. **저장** 버튼 클릭

## 2. 로봇팔 티칭 (텔레메트리 캡처)

로봇팔을 수동으로 원하는 자세로 이동 후:

1. Action 노드 클릭하여 선택
2. **Goal Parameters** 섹션 펼치기
3. 로봇 선택 드롭다운에서 로봇 선택
4. **LIVE** 표시 확인 (실시간 텔레메트리 수신 중)
5. **현재 로봇 자세로 초기화** 버튼 클릭
6. 현재 joint_state 값이 자동으로 입력됨

## 3. Agent에 템플릿 할당

1. 템플릿 목록에서 할당할 템플릿 선택
2. **할당** 탭 클릭
3. **호환 에이전트** 목록에서 할당할 Agent 선택
4. **할당** 버튼 클릭

## 4. Behavior Tree 실행

1. Agent를 선택하고 할당된 템플릿 확인
2. **배포** 버튼으로 Agent에 배포
3. **실행** 버튼 클릭
4. 실시간으로 진행 상황 모니터링 (노드 색상 변화)

---

# API Reference

## REST API (Port 8081)

### Agents

```bash
# Agent 목록
curl http://localhost:8081/api/agents

# Agent 상세 정보
curl http://localhost:8081/api/agents/{agentID}

# Agent의 Capability 목록
curl http://localhost:8081/api/agents/{agentID}/capabilities

# Agent 연결 상태 (하트비트 모니터링)
curl http://localhost:8081/api/agents/connection-status
```

### Capabilities

```bash
# 전체 Capability 목록 (Fleet-wide)
curl http://localhost:8081/api/capabilities

# Action Type별 통계
curl http://localhost:8081/api/capabilities/action-types

# 특정 Action Type의 Capability
curl http://localhost:8081/api/capabilities/action-type/nav2_msgs/action/NavigateToPose
```

### Behavior Trees (Templates)

```bash
# 템플릿 목록
curl http://localhost:8081/api/templates

# 템플릿 생성
curl -X POST http://localhost:8081/api/templates \
  -H "Content-Type: application/json" \
  -d '{"id": "pick_and_place", "name": "Pick and Place"}'

# 템플릿 상세
curl http://localhost:8081/api/templates/{templateID}

# 템플릿 호환 Agent 목록
curl http://localhost:8081/api/templates/{templateID}/compatible-agents

# Agent에 템플릿 할당
curl -X POST http://localhost:8081/api/templates/{templateID}/assignments \
  -H "Content-Type: application/json" \
  -d '{"agent_id": "agent-xxx"}'

# Agent에 배포
curl -X POST http://localhost:8081/api/templates/{templateID}/assignments/{agentID}/deploy
```

### Tasks

```bash
# 실행 중인 Task 목록
curl http://localhost:8081/api/tasks

# Task 상태 확인
curl http://localhost:8081/api/tasks/{taskID}

# Task 취소
curl -X POST http://localhost:8081/api/tasks/{taskID}/cancel

# Task 일시정지
curl -X POST http://localhost:8081/api/tasks/{taskID}/pause

# Task 재개
curl -X POST http://localhost:8081/api/tasks/{taskID}/resume

# Task 로그
curl http://localhost:8081/api/tasks/{taskID}/logs
```

### Fleet State

```bash
# 전체 Fleet 상태
curl http://localhost:8081/api/fleet/state

# Fleet 요약
curl http://localhost:8081/api/fleet/summary

# 특정 로봇의 텔레메트리
curl http://localhost:8081/api/fleet/robots/{robotID}/telemetry

# Joint State
curl http://localhost:8081/api/fleet/robots/{robotID}/telemetry/joint-state

# Odometry
curl http://localhost:8081/api/fleet/robots/{robotID}/telemetry/odometry
```

### System

```bash
# 헬스 체크
curl http://localhost:8081/health

# 캐시 통계
curl http://localhost:8081/api/system/cache/stats

# 오래된 캐시 정리
curl -X POST http://localhost:8081/api/system/cache/evict \
  -d '{"max_age_minutes": 30}'
```

## WebSocket (실시간 모니터링)

```javascript
const ws = new WebSocket('ws://localhost:8081/ws/monitor')

ws.onmessage = (event) => {
  const data = JSON.parse(event.data)
  // data.agents: Agent 상태 배열
  // data.tasks: 실행 중인 Task 배열
  console.log('Fleet state:', data)
}
```

---

# 프로젝트 구조

```
multi-robot-supervision/
├── central_server_go/              # Go Backend
│   ├── cmd/server/                 # 진입점 (main.go)
│   ├── internal/
│   │   ├── api/                    # REST API 핸들러 (50+ endpoints)
│   │   │   ├── router.go           # 라우팅 설정
│   │   │   ├── agents.go           # Agent CRUD
│   │   │   ├── templates.go        # Template (Behavior Tree) 관리
│   │   │   ├── behavior_trees.go   # Behavior Tree 실행/배포
│   │   │   ├── capabilities.go     # Capability 조회
│   │   │   ├── tasks.go            # Task 관리
│   │   │   ├── fleet.go            # Fleet State/Telemetry
│   │   │   └── websocket.go        # WebSocket 브로드캐스트
│   │   ├── db/                     # Neo4j 데이터베이스
│   │   │   ├── models.go           # 데이터 모델
│   │   │   └── repository.go       # DB 쿼리
│   │   ├── state/                  # In-memory 상태 관리
│   │   │   └── manager.go          # GlobalStateManager
│   │   ├── executor/               # Task 스케줄러
│   │   │   └── scheduler.go
│   │   ├── graph/                  # Behavior Tree 처리
│   │   │   ├── schema.go           # Canonical Graph 타입
│   │   │   └── converter.go        # DB ↔ Canonical 변환
│   │   └── grpc/                   # QUIC 서버
│   │       └── raw_quic_handler.go # C++ Agent 통신
│   ├── pkg/proto/                  # Protobuf 정의
│   └── Dockerfile
│
├── central_server/frontend/        # React Frontend
│   ├── src/
│   │   ├── pages/
│   │   │   ├── AgentDashboard/     # Agent 대시보드
│   │   │   ├── ActionGraph/        # Behavior Tree 편집기
│   │   │   │   ├── index.tsx       # 메인 에디터
│   │   │   │   └── nodes/          # 커스텀 노드 컴포넌트
│   │   │   ├── Monitoring/         # 실시간 모니터링
│   │   │   ├── TaskHistory/        # Task 이력
│   │   │   └── PDDL/               # PDDL 플래너 (예정)
│   │   ├── components/             # 공통 컴포넌트
│   │   ├── contexts/               # React Context (WebSocket 등)
│   │   ├── hooks/                  # Custom Hooks
│   │   └── types/                  # TypeScript 타입 정의
│   ├── package.json
│   └── Dockerfile
│
├── ros2_robot_agent/                # C++ Agent (ROS2)
│   ├── include/robot_agent/
│   │   ├── interfaces/             # 인터페이스 (추상 클래스)
│   │   │   ├── transport.hpp       # ITransport
│   │   │   ├── capability_scanner.hpp
│   │   │   └── action_executor.hpp
│   │   ├── core/                   # 핵심 타입 및 설정
│   │   ├── transport/              # QUIC 통신
│   │   ├── capability/             # ROS2 Action Server 탐지
│   │   ├── telemetry/              # 텔레메트리 수집/전송
│   │   ├── executor/               # 액션 실행
│   │   └── graph/                  # Behavior Tree 실행
│   ├── src/                        # 구현
│   ├── proto/                      # Protobuf 정의
│   ├── config/                     # 설정 파일
│   └── launch/                     # ROS2 Launch 파일
│
├── docker-compose.yaml             # Docker 스택
├── CLAUDE.md                       # 개발 가이드
└── README.md                       # 이 파일
```

---

# 문제 해결

## Agent가 서버에 연결 안 됨

```bash
# 1. 서버 방화벽 확인
sudo ufw allow 9444/udp

# 2. Agent 로그 확인
ros2 launch ros2_robot_agent robot_agent.launch.py server_ip:=... log_level:=debug

# 3. 네트워크 연결 확인
ping <서버_IP>

# 4. QUIC 포트 확인
nc -vzu <서버_IP> 9444
```

## Web UI에서 Agent가 안 보임

```bash
# Backend 로그 확인
docker-compose logs -f go-backend

# Agent 목록 API 호출
curl http://localhost:8081/api/agents

# Agent 연결 상태 확인
curl http://localhost:8081/api/agents/connection-status
```

## Agent 재시작 후 다른 Agent로 인식됨

Agent가 재시작 후에도 동일한 ID를 유지하려면:

```yaml
# agent.yaml
agent:
  id: ""                          # 비워두면 서버가 자동 할당
  use_server_assigned_id: true    # 서버 할당 ID 사용

storage:
  agent_id_path: "/tmp/robot_agent/agent_id"  # 할당받은 ID 저장 경로
```

## Docker 권한 오류

```bash
sudo usermod -aG docker $USER
newgrp docker
```

## Neo4j 연결 오류

```bash
# Neo4j 컨테이너 상태 확인
docker-compose logs neo4j

# Neo4j 브라우저 접속
open http://localhost:7474
# 로그인: neo4j / neo4j123
```

---

# 기술 스택

| 컴포넌트 | 기술 |
|----------|------|
| **Backend** | Go 1.21, Chi Router, Neo4j Driver, quic-go |
| **Frontend** | React 18, TypeScript, Vite, TailwindCSS, React Flow, TanStack Query |
| **Database** | Neo4j 5.x (Graph DB) |
| **Agent** | C++17, ROS2 Humble, MsQuic, Protobuf, TBB |
| **통신** | QUIC (0-RTT, Connection Migration, Datagrams) |
| **컨테이너** | Docker, docker-compose |

## 성능 특성

| 항목 | 수치 |
|------|------|
| Backend 메모리 | ~20-50MB (idle) |
| HTTP 처리량 | ~50,000 req/s |
| WebSocket 클라이언트 | 100,000+ 동시 연결 |
| Agent 텔레메트리 | 10Hz per robot |
| Action Latency | <5ms |
| Cold Start | 0.1-0.3초 |

---

# 라이선스

MIT License

---

# 최근 수정 사항

## 2026-03-09 - PDDL execute 즉시 취소 문제 수정

- 증상: PDDL에서 `풀기` 후 `실행`을 누르면 `cancelled by user`로 즉시 종료됨
- 원인: `ExecutePlan()`이 HTTP 요청의 `r.Context()`를 그대로 사용해서, 응답 반환 직후 plan execution이 함께 cancel됨
- 수정: `central_server_go/internal/api/pddl.go`에서
  `StartPlanExecution(r.Context(), ...)` 를
  `StartPlanExecution(context.WithoutCancel(r.Context()), ...)` 로 변경
- 효과: HTTP 응답이 끝난 뒤에도 plan execution이 계속 진행됨

상세 메모:
- `~/mcs_dev/PDDL_EXECUTION_FIX_NOTES.txt`

## 2026-03-09 - PDDL draft 복원 후 goal 초기화 버그 수정

- 증상:
  - PDDL 화면을 벗어났다가 돌아오면 selected task/distributor는 복원되지만 goal이 다시 비워지는 문제 발생
- 원인:
  - draft 복원 직후 기존 selection reset effect가 한 번 더 실행되면서 goal / initial override를 다시 초기화
- 수정:
  - 복원된 selectionKey를 한 번 무시하는 guard 추가
  - 이제 복원 직후에는 goal / initial override가 유지됨
- 추가 개선:
  - solve 실패 시 generic AxiosError 대신 backend가 내려준 실제 error/message를 우선 표시하도록 개선
- 주의:
  - selection 변경 시 reset 동작과 연결되어 있으므로 goal 유지/초기화 모두 회귀 테스트 필요

상세 메모:
- `~/mcs_dev/PDDL_EXECUTION_FIX_NOTES.txt`

## 2026-03-09 - PDDL 화면 Draft 자동 저장/복원

- 목적: PDDL 화면에서 task 선택, agent 선택, goal, initial override를 설정한 뒤 다른 화면(예: task 수정)으로 갔다가 돌아와도 작업 중인 draft를 유지
- 수정:
  - PDDL 화면 상태를 localStorage에 자동 저장
  - 페이지 진입 시 draft 자동 복원
  - 복원된 task ID의 상세 정보가 캐시에 없으면 자동 재조회
  - 기존 selection reset 로직이 복원 직후 goal/initial state를 지워버리지 않도록 guard 추가
- 자동 저장 대상:
  - selected tasks
  - selected distributor
  - selected agents
  - goal state
  - initial state override
  - initial state editor open/close 상태
- 주의:
  - PDDL selection 변경 시 기존 plan/execution 초기화 흐름과 맞물리므로 회귀 테스트 필요

상세 메모:
- `~/mcs_dev/PDDL_EXECUTION_FIX_NOTES.txt`

## 2026-03-09 - PDDL 다중 Task 선택 지원

- 목적: PDDL에서 task(behavior tree)를 하나가 아니라 여러 개 선택해서 planning / execution 할 수 있도록 확장
- 프론트 수정:
  - PDDL task 선택 UI를 단일 선택에서 다중 선택으로 변경
  - preview / create / solve / execute 요청에 `behavior_tree_ids` 추가
- 백엔드 수정:
  - planning problem에 여러 behavior tree ID 저장
  - `solveProblem()`이 선택된 여러 BT를 모두 읽어서 여러 `PlanTask`로 planner에 전달
  - `PlanExecutor`가 assignment별 `behavior_tree_id`로 실제 실행하도록 변경
  - plan execution 응답에 `behavior_tree_ids`, step별 `behavior_tree_id` 포함
- 효과:
  - PDDL이 단일 task 실행 래퍼 수준을 넘어, 여러 task를 한 planning problem 안에서 다룰 수 있는 기반 마련
- 주의:
  - 영향 범위가 커서 기존 단일 task preview / solve / execute도 반드시 회귀 테스트 필요

상세 메모:
- `~/mcs_dev/PDDL_EXECUTION_FIX_NOTES.txt`

## 2026-03-09 - ActionGraph planning result/during 버튼 레이아웃 수정

- 증상:
  - ActionGraph의 `Current Task Planning -> Result States`에서 `+` 버튼이 카드 오른쪽 밖으로 밀려나 보이고 클릭이 잘 되지 않음
  - 같은 영역의 `During State`도 좁은 폭에서 버튼/입력창 정렬이 불안정함
- 수정:
  - `central_server/frontend/src/pages/ActionGraph/nodes/StateActionNode.tsx`에서 `During State`, `Result States` 행을 wrapping 가능한 flex 레이아웃으로 변경
  - 상태 선택 입력은 `min-w-0`, 값 입력/버튼은 `shrink-0` 처리해서 카드 안에 유지되도록 수정
- 주의:
  - ActionGraph 노드 편집 UI를 건드렸으므로 `Result States` 추가/삭제, `During State` 설정/초기화, Runtime Resources UI를 함께 회귀 테스트 필요

상세 메모:
- `~/mcs_dev/PDDL_EXECUTION_FIX_NOTES.txt`

## 2026-03-09 - ActionGraph task-level PDDL Config 모달로 통합

- 목적:
  - `During State`, `Result States`, `required_resources` 같은 PDDL planning 설정을 액션 노드 단위가 아니라 태스크 전체 단위로 관리하도록 UI 정리
- 수정:
  - 사이드바 상단에 `PDDL Config` 버튼 추가
  - 버튼 클릭 시 태스크 전체 planning 설정을 관리하는 모달 오픈
  - 모달에서 다음 항목을 관리:
    - Task Distributor 선택
    - During State
    - Result States
    - Available state/resource 설명
    - Action Node runtime acquire/release에서 자동 집계된 required_resources 요약
    - `저장`, `닫기` 버튼
  - Action Node 내부의 중복된 `Current Task Planning` UI 제거
- 유지한 것:
  - `Runtime Resources`는 step별 acquire / release 설정이므로 Action Node에 그대로 유지
- 추가 수정:
  - 새로 드롭한 Action Node도 현재 `task_distributor_id`를 즉시 이어받도록 해서 Runtime Resources가 바로 설정 가능하게 수정
- 제거/정리한 불필요 설정:
  - node-level task planning 편집 UI
  - required_resources 수동 입력 흐름 (이제 자동 집계 결과만 사용)
  - 관련 중복 frontend node-data 필드
- 주의:
  - ActionGraph 편집 흐름 전반에 영향이 있으므로 Task Distributor 선택, modal 저장/닫기, Runtime Resources 자동 동기화, 기존 task 로드 시 planning 값 표시를 회귀 테스트해야 함

상세 메모:
- `~/mcs_dev/PDDL_EXECUTION_FIX_NOTES.txt`

## 2026-03-09 - ActionGraph PDDL 단순화 (task-level required/result/resource)

- 목적:
  - PDDL planning 설정을 액션 노드 단위가 아니라 태스크 전체 단위로 단순화해서, "이 태스크가 시작되려면 무엇이 필요하고", "끝나면 어떤 값이 바뀌는지"를 한 곳에서 관리할 수 있게 정리
- 프론트 수정:
  - ActionGraph `PDDL Config` 모달에서 다음만 관리하도록 변경
    - Task Distributor 선택
    - Required States (`preconditions`)
    - Result States (`result_states`)
    - Required Resources (`required_resources`)
  - Action Node 내부의 `Runtime Resources` UI 제거
  - PDDL 화면 task 요약에 `Need`(preconditions) 개수 표시
- 백엔드 수정:
  - `PlanningTaskSpec`에 `preconditions` 추가
  - planner가 task 선택 시 현재 planning state와 `preconditions`를 비교해 실행 가능 task만 고르도록 수정
- 효과:
  - 사용자는 step-level acquire/release 같은 세세한 설정 없이 task-level planning만 우선 구성 가능
  - 예: `cnc_01_empty == true` 가 필요하고, 성공 후 `cnc_01_service_done = true` 가 되는 식으로 간단히 설정 가능
- 주의:
  - 이전에 Action Node Runtime Resources 기반으로 required_resources를 잡던 task는 이제 task-level `PDDL Config`에서 다시 확인/설정해야 함
  - 영향 범위가 커서 기존 single-task / multi-task planning 흐름 모두 회귀 테스트 필요

상세 메모:
- `~/mcs_dev/PDDL_EXECUTION_FIX_NOTES.txt`

## 2026-03-10 - Generic resource-grounded PDDL task 지원

- 목적:
  - CNC/charger마다 behavior tree를 따로 만들지 않고, 하나의 generic task 템플릿을 여러 resource 인스턴스에 재사용할 수 있도록 확장
- 백엔드 수정:
  - planner solve 전에 `type:` resource를 가진 task를 concrete resource instance별 task로 grounding
  - task-level planning metadata에서 다음 placeholder 지원
    - `{{resource.name}}`
    - `{{resource.id}}`
    - `{{resource.kind}}`
    - `{{resource.type_name}}`
    - `{{resource.type_id}}`
  - grounded task는 runtime params (`resource_name`, `resource.id` 등)를 함께 들고 실행 단계까지 전달
  - reachability check가 task preconditions도 고려하도록 개선
- 프론트 수정:
  - ActionGraph `PDDL Config` 설명에 generic resource placeholder / runtime param 사용법 추가
- 사용 예:
  - Required States: `{{resource.name}}_empty == true`
  - Result States: `{{resource.name}}_empty = false`
  - Action goal param: `${resource_name}` 또는 `${resource.name}`
- 효과:
  - `go_to_cnc_and_park`, `run_cnc_service_cycle` 같은 task를 CNC 인스턴스별로 복제하지 않고도 planning 가능
- 주의:
  - generic task를 쓰려면 Task Distributor에 실제 state 변수(`cnc01_empty`, `cnc02_empty` 등)는 여전히 선언되어 있어야 함
  - 기존 concrete task는 그대로 동작해야 하지만 single-task / multi-task solve/execute 회귀 테스트 필요

상세 메모:
- `~/mcs_dev/PDDL_EXECUTION_FIX_NOTES.txt`

## 2026-03-10 - Generic PDDL placeholder / action param UX 보완

- 목적:
  - generic resource task를 실제 UI에서 자연스럽게 설정할 수 있도록 placeholder 기반 state 입력과 action goal param runtime binding UX를 보완
- 프론트 수정:
  - `PDDL Config`의 Required States / Result States를 strict select-only 방식에서
    **직접 입력 + datalist 추천** 방식으로 변경
  - common placeholder 빠른 입력 버튼 추가
    - `{{resource.name}}_empty`
    - `at_{{resource.name}}`
  - placeholder state(`{{resource.name}}_empty` 등)가 저장 시 필터링되어 사라지지 않도록 보완
  - string goal param에서 `PDDL / 실행 변수 사용` 옵션 추가
    - `${resource_name}`
    - `${resource.name}`
    - `${resource_id}`
    - `${resource.id}`
    - `${resource_type_name}`
    - `${resource.type_name}`
    - `${resource_type_id}`
    - `${resource.type_id}`
- 효과:
  - generic task에서 `{{resource.name}}_empty == true` 같은 조건을 직접 입력 가능
  - `navigate_and_park.marker_name` 같은 string goal param에 `${resource_name}`를 UI에서 바로 선택 가능
- 주의:
  - `[TYPE] CNC`는 여러 CNC에 재사용할 generic task일 때만 필요하고, 고정된 `cnc01` 전용 task면 instance resource만 선택해도 됨
  - 영향 범위가 goal parameter binding UI에도 있으므로 기존 `이전 Step 결과 사용(step_result)` 흐름 회귀 테스트 필요
- 추가 보완:
  - primitive/string goal param에서 `PDDL / 실행 변수 사용`이 "사용 가능한 변수 없음"으로 뜨던 전달 누락 버그 수정
  - GoalParametersSection의 runtime binding 목록이 실제 parameter editor까지 전달되도록 정리

상세 메모:
- `~/mcs_dev/PDDL_EXECUTION_FIX_NOTES.txt`

## 2026-03-10 - PDDL authoring/execution follow-up fixes

- 목적:
  - generic CNC PDDL 테스트 중 나온 3가지 문제를 보완
    1. placeholder state 변수 선택 UX 개선
    2. PDDL 실행 상태가 메뉴 이동 후에도 유지되도록 보완
    3. PDDL 실행 시 agent가 최신 BT가 아니라 구버전 deployed graph를 쓰던 문제 수정
- 프론트 수정:
  - ActionGraph `PDDL Config`의 Required / Result state 변수 입력칸을 포커스하면 즉시 추천 목록이 뜨도록 변경
  - 추천 목록은 실제 distributor state + instance state에서 추론한 generic placeholder를 함께 표시
    - 예: `{{resource.name}}_empty`, `at_{{resource.name}}`, `{{resource.name}}_service_done`
  - PDDL 페이지 draft persistence에 마지막 `plan`, `executionId`도 저장/복원하도록 확장
  - 다른 메뉴로 갔다가 돌아와도 실행 polling을 다시 시작해서 BT 실행 제어 보드가 비워지지 않도록 수정
- 백엔드 수정:
  - 원인 확인: 선택 agent에 배포된 BT가 `deployment_status=outdated`, `deployed_version < server_version` 상태여서
    최근 편집한 `${resource_name}` 바인딩이 실제 실행에 반영되지 않았음
  - `Scheduler.StartTask()`에서 실행 직전 agent의 graph assignment를 확인하고, 구버전/미배포 상태면 최신 BT를 자동 deploy 하도록 수정
- 효과:
  - generic state placeholder를 더 자연스럽게 입력 가능
  - PDDL 실행 중 메뉴 이동 후에도 진행 상태를 계속 확인 가능
  - 최근 수정한 BT(goal param runtime binding 포함)가 PDDL 실행에서 실제 agent 쪽에도 반영됨
- 주의 / 회귀 테스트:
  - `PDDL Config` 입력칸 클릭 시 placeholder 추천이 바로 보이는지 확인
  - PDDL 실행 후 다른 메뉴 갔다가 돌아와도 실행 보드가 그대로 유지되는지 확인
  - 최근 수정한 BT를 수동 deploy 없이 실행했을 때 최신 버전이 반영되는지 확인
- 기존 non-PDDL BT 실행도 task start 시 자동 deploy 경로를 타게 될 수 있으므로 함께 회귀 테스트 필요

상세 메모:
- `~/mcs_dev/PDDL_EXECUTION_FIX_NOTES.txt`

## 2026-03-10 - PDDL Config 저장 범위 / 최신 그래프 재배포 보강

- 목적:
  - `PDDL Config` 저장 버튼을 눌렀을 때 PDDL 메타데이터만이 아니라 현재 task 그래프 자체도 함께 안정적으로 저장되도록 보강
  - 최근 편집된 BT가 마지막 deploy 시점보다 새로울 경우, 실행 전에 다시 deploy 되도록 판단 기준을 강화
- 프론트 수정:
  - `TaskPddlConfigModal` 저장 시 이제 아래를 한 번에 같이 저장
    - `steps`
    - `entry_point`
    - `task_distributor_id`
    - `planning_task`
  - 즉 모달의 `저장` 버튼이 task 전체 스냅샷을 함께 저장하므로, action 블록을 편집한 뒤 모달에서 저장하고 다른 메뉴로 이동했다가 돌아와도 그래프가 사라질 가능성을 줄임
- 백엔드 수정:
  - `Scheduler.ensureGraphDeployed()`가 더 이상 단순히 `deployed_version == server_version`만으로 최신 배포라고 판단하지 않음
  - 아래 조건을 모두 만족할 때만 deploy 생략:
    - `deployment_status == deployed`
    - `deployed_version == server_version`
    - `deployed_at` 존재
    - `behavior_tree.updated_at <= deployed_at`
  - 따라서 BT를 수정한 뒤 agent 쪽에 이전 버전/동일 버전 그래프가 남아 있어도, 마지막 deploy 시점보다 서버 그래프가 새로우면 실행 전에 다시 deploy
- 기대 효과:
  - `PDDL Config` 저장 후 다른 메뉴로 이동했다 와도 action node / entry point / PDDL 설정이 함께 유지되어야 함
  - generic binding `${resource_name}` 같은 최신 goal parameter 변경이 `CNC_01` 같은 과거 값 대신 실제 최신 그래프로 반영될 가능성이 높아짐
- 주의 / 회귀 테스트:
  - task graph를 수정한 뒤 `PDDL Config -> 저장`만 눌러도 그래프가 함께 유지되는지 확인
  - generic CNC 테스트에서 `go_to_cnc_01_and_park`가 여전히 stale `CNC_01`이 아니라 실제 resource 이름으로 실행되는지 확인
  - deploy 판단이 더 엄격해져서 기존 task start 시 첫 실행 지연이 약간 늘 수 있으므로 기존 BT 실행도 함께 점검 필요

상세 메모:
- `~/mcs_dev/PDDL_EXECUTION_FIX_NOTES.txt`


MCS-side concrete runtime graph binding
======================================

Date:
- 2026-03-10

Goal:
- Stop relying on agent-side `${resource_name}` expression evaluation for PDDL-selected resources.
- Make MCS/server concretize runtime bindings before execution so task-scoped values such as `cnc01` are baked into the graph that gets dispatched.

What changed:
- `Scheduler.StartTask()` now prepares an execution graph after runtime params are known.
- If action goal params contain expression bindings that can be resolved from execution params (for example `${resource_name}`), the server materializes a concrete runtime graph before dispatch.
- The materialized graph is deployed to the target agent with a task-scoped temporary graph ID like `<graph_id>__exec__<task>`.
- Expression-based action params that are fully resolvable are converted to constant field sources on the server side.
- Inline/data payload strings that include runtime placeholders are also substituted on the server side.
- `SendStartTask` now uses the runtime graph ID when a concrete execution graph was prepared.
- After task completion, the server invalidates the temporary deployed graph from its local graph cache.

Main files changed:
- central_server_go/internal/executor/scheduler.go
- central_server_go/internal/executor/runtime_graph_materializer.go

Expected effect:
- Generic PDDL tasks such as `go_to_cnc_01_and_park` should now send the selected PDDL resource value (for example `cnc01`) to the ROS action instead of falling back to stale/default values like `CNC_01`.
- This shifts the concrete binding responsibility to MCS/server, which matches the current task-template/PDDL design better.

Important regression test points:
- Re-run the generic CNC flow and confirm park action logs now show the bound resource name from PDDL (e.g. `cnc01`).
- Re-test normal non-PDDL BT execution because execution now may deploy a task-scoped runtime graph when runtime expressions are present.
- Expect first task start with runtime bindings to be a bit slower because the server deploys a concrete runtime graph right before dispatch.

## 2026-03-10 - PDDL 취소/재실행/RTM stale BT 보강

- 목적:
  - 다음 단계 진행 전에 아래 3가지 막히는 문제를 먼저 보완
    1. PDDL 실행 후 `실행 취소`를 눌러도 실제 runtime task/action이 계속 돌던 문제
    2. 한 번 실행이 끝난 뒤 다시 `풀기 -> 실행`하면 wave 1에 들어가지만 실제 액션이 dispatch되지 않던 문제
    3. RTM의 현재 할당 BT에서 이미 없어졌거나 stale한 BT를 누르면 패널이 검은 화면처럼 비던 문제
- 백엔드 수정:
  - `PlanExecutor.CancelExecution()`이 이제 plan context만 취소하는 것이 아니라, 이미 시작된 runtime task들도 `Scheduler.CancelTask(...)`로 함께 취소하도록 보강
  - plan/group 실행 중 context 취소가 들어오면 실패가 아니라 `cancelled` 경로로 정리되도록 보완
  - `Scheduler.StartTask()`가 task queue 등록 직후 `DispatchIdleAgents()`를 호출하도록 변경
    - 이미 agent가 idle이면 다음 heartbeat를 기다리지 않고 즉시 dispatch 가능
- 프론트 수정:
  - Agent Dashboard / RTM current assigned BT 패널에서 stale/deleted BT fetch 실패 시
    - retry를 멈추고
    - 다른 유효한 BT로 fallback 하거나 선택을 해제
    - 검은 패널 대신 안내 메시지를 표시
- 기대 효과:
  - `실행 취소`가 실제 action 중단까지 이어져야 함
  - 같은 PDDL 문제를 연속 재실행해도 첫 wave가 바로 dispatch되어야 함
  - RTM stale BT 선택 시 화면이 비지 않고 안내/복구가 되어야 함
- 주의 / 회귀 테스트:
  - 일반(non-PDDL) task 취소도 cancellation 전파 영향이 있을 수 있으므로 함께 확인 필요
  - task queue 직후 즉시 dispatch가 들어가므로 기존 일반 BT 실행 타이밍도 함께 점검 필요

상세 메모:
- `~/mcs_dev/PDDL_EXECUTION_FIX_NOTES.txt`

## 2026-03-10 - 태스크 이름/ID 수정 기능 추가

- 목적:
  - Task Definitions에서 태스크 정체성(identity) 자체를 수정할 수 있도록 보완
  - 기존에는 생성 시점에만 ID/이름을 정하고 이후 변경이 어려웠음
- 추가 API:
  - `PATCH /api/templates/{templateID}/identity`
  - 요청 본문: `{ "new_id"?: string, "new_name"?: string }`
- 백엔드 변경:
  - `UpdateTemplateIdentity` 핸들러 추가
  - `RenameBehaviorTreeIdentity(...)` 추가
    - ID 변경 시 연관 참조를 함께 업데이트:
      - `ActionGraph.id`, graph 구조 노드의 `graph_id`
      - `AgentActionGraph.behavior_tree_id`
      - `Task.behavior_tree_id`
      - `Agent.current_graph_id`
      - `PlanningProblem.behavior_tree_id`, `behavior_tree_ids`
  - ID 변경 시 기존 할당은 재배포가 필요하므로 `outdated` 상태로 강등
- 프론트 변경:
  - ActionGraph 툴바(편집 모드)에 `이름/ID` 버튼 추가
  - `RenameTemplateIdentityModal` 추가
  - 수정 성공 시 새 ID로 선택 전환 + 관련 캐시 갱신
- 기대 효과:
  - 태스크 생성 후에도 이름/ID를 안전하게 정리 가능
  - ID 변경 후에도 주요 참조(할당/작업/계획)가 깨지지 않도록 보호
- 주의 / 회귀 테스트:
  - ID 변경 직후 첫 실행은 재배포(outdated→deployed) 경로를 탈 수 있어 시작이 약간 느릴 수 있음
  - 기존 PDDL 실행/취소 흐름도 함께 재검증 권장

상세 메모:
- `~/mcs_dev/PDDL_EXECUTION_FIX_NOTES.txt`

## 2026-03-11 - Realtime PDDL Loop MVP

- 목적:
  - 기존 one-shot `Preview -> Solve -> Execute` 중심 PDDL 메뉴를, 우선순위 goal 후보를 반복 평가하는 realtime PDDL 루프 방향으로 확장 시작
- 백엔드 추가:
  - `GET /api/pddl/realtime-sessions`
  - `POST /api/pddl/realtime-sessions`
  - `GET /api/pddl/realtime-sessions/{sessionID}`
  - `POST /api/pddl/realtime-sessions/{sessionID}/stop`
  - in-memory realtime session manager 추가
    - 세션별 planner state 유지
    - goal 후보(priority 오름차순) 선택
    - activation condition 만족 + goal 미충족인 첫 후보를 solve
    - solve 성공 시 runtime plan execution 시작
    - 완료 시 task result state를 세션 current_state에 반영
    - 동일 state/goal 조합에서 실패 시 즉시 무한 재시도하지 않도록 차단
- 실행기(PlanExecutor) 보강:
  - `StartRuntimePlanExecution(...)` 추가
  - DB에 `planning_problems` row를 저장하지 않아도 runtime execution 가능
  - runtime execution이 직접 initial planning state / task distributor context를 받을 수 있게 확장
- 프론트(PDDL 페이지) 추가:
  - `Realtime PDDL Loop` 섹션 추가
  - realtime goal rule 편집기 추가
    - goal 이름
    - priority
    - activation conditions
    - goal state
  - tick interval 입력
  - realtime start / stop 버튼
  - session state / live state 표시
  - 현재 one-shot goal을 realtime goal 후보로 복사하는 버튼 추가
  - draft persistence에 realtime goal / tick interval / active realtime session ID 포함
- 현재 한계:
  - 아직 telemetry 기반 planning state 반영은 미구현
  - 즉, 지금은 realtime session 내부 planner state + task result state 반영까지가 MVP
  - 다음 단계에서 battery / cnc running-done / lane occupancy 연동 필요
- 회귀 테스트 포인트:
  - 기존 one-shot PDDL preview / solve / execute가 여전히 동작하는지
  - realtime start/stop이 일반 plan execution cancel 흐름을 깨지 않는지
  - priority 숫자가 낮을수록 먼저 선택되는지
  - completed execution 이후 session `current_state`가 result states로 갱신되는지
  - 실패한 동일 state/goal 조합을 매 tick마다 무한 재시도하지 않는지

상세 메모:
- `~/mcs_dev/PDDL_EXECUTION_FIX_NOTES.txt`

## 2026-03-11 - Task Distributor Profile JSON (Control-room Save/Load)

- 목적:
  - PDDL 메뉴의 Task Distributor 컨트롤룸 설정(태스크 편집 내부가 아닌 운영 설정)을 파일로 저장/복원할 수 있도록 개선
  - 환경 재구성 시 매번 수동 입력하는 부담 감소
- 프론트(PDDL 페이지) 추가:
  - `Export JSON` 버튼
    - 현재 선택된 distributor 기준으로 설정을 JSON 파일로 다운로드
  - `Import JSON` 버튼
    - JSON 파일을 읽어 distributor 설정을 복원
- Import 시 적용 범위(컨트롤룸 설정):
  - distributor 이름/설명
  - 상태(State) 목록
  - 리소스(Resource) 목록 (type/instance + parent 관계)
  - 선택 태스크(가능한 경우 name/id 매칭)
  - 선택 에이전트(가능한 경우 name/id 매칭)
  - initial_state / goal_state
  - realtime goals / tick interval
- 동작 정책:
  - 같은 distributor를 찾으면(우선 id, 없으면 name) 해당 distributor를 profile 기준으로 동기화
  - 없으면 새 distributor 생성 후 profile 적용
  - import 직후 선택 상태/계획 상태를 갱신하고 realtime 세션은 초기화
- 샘플 프로필 파일 추가:
  - `examples/task_distributor_profiles/realtime_cnc_starter.json`
  - CNC realtime loop 시작용 기본 예시(요청한 task 이름 포함)
- 회귀 테스트 포인트:
  - 기존 one-shot PDDL solve/execute와 realtime start/stop이 그대로 동작하는지
  - profile import 후 resource parent(type-instance) 관계가 정확히 복원되는지
  - profile import 후 task/agent selection이 정상 매핑되는지(name/id 불일치 시 무시되는지)

상세 메모:
- `~/mcs_dev/PDDL_EXECUTION_FIX_NOTES.txt`

### 2026-03-11 Update (starter profile scope expanded)

- `examples/task_distributor_profiles/realtime_cnc_starter.json` was expanded to include a fuller baseline:
  - CNC resources: `cnc01`~`cnc06` (+ `cnc0` type)
  - Charger resources: `charger01`, `charger02` (+ `charger` type)
  - CNC/agent planning states expanded (including `at_cnc01`~`at_cnc06`, `cnc01_status`~`cnc06_status`, `agent01_battery_low`~`agent03_battery_low`, `pending_leave`)
- This makes the starter profile closer to the real multi-CNC realtime test setup instead of a minimal single-CNC seed.
