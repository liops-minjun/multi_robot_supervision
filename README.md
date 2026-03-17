# Multi-Robot Supervision System

다양한 로봇(AMR, 로봇팔, 지게차 등)을 중앙에서 관리하는 Fleet Management System입니다.

## 주요 기능

| 기능 | 설명 |
|------|------|
| **실시간 모니터링** | WebSocket을 통한 로봇 상태, 위치, 텔레메트리 실시간 확인 |
| **Behavior Tree 편집기** | 드래그 앤 드롭 노드 기반 작업 시나리오 구성 (React Flow) |
| **Retry Block** | 실패 시 직전 액션 재시도(max_retries/backoff_ms) 블록 지원 |
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

### 2026-03-11 Update (single-CNC quickstart profile)

- Added a run-ready single CNC profile:
  - `examples/task_distributor_profiles/realtime_cnc01_quickstart.json`
- Purpose:
  - immediate test with only `cnc01` without multi-CNC state/resource overhead
  - includes task/realtime-goal defaults and a default selected agent name (`Task Manager-001`) for quicker bring-up
- Recommended when you want to quickly validate realtime flow end-to-end before scaling to cnc01~cnc06.

### 2026-03-11 Update (Realtime Start disabled reason hint)

- PDDL `Realtime PDDL Loop` section now shows explicit reason text when `Start realtime` is disabled.
- Added both:
  - button `title` tooltip reason
  - inline warning block (`Start realtime 비활성화 사유: ...`)
- Current checks include:
  - distributor not selected
  - no task selected
  - no agent selected
  - no valid realtime goal (enabled + goal_state)
  - execution already running
  - realtime session already active

### 2026-03-11 Update (Task Set Save/Load in Task Editor)

- Added **Task Set JSON 저장/불러오기** in the Task Definitions (ActionGraph) sidebar.
- New controls in task list panel:
  - `저장`: exports all current tasks/templates to one JSON file
  - `불러오기`: imports a task-set JSON and restores tasks + assignments
- Export payload includes:
  - task/template graph core fields (id/name/description/entry/steps/states/planning fields)
  - per-task assignment snapshot (agent_id/agent_name/enabled/priority)
- Import behavior:
  - same ID task exists -> overwrite(update)
  - missing task -> create then apply update fields
  - existing assignments are cleared then restored from JSON
  - if target agent does not exist, assignment is skipped and counted in summary
- UI feedback:
  - import/export progress lock
  - summary notice shown in sidebar after completion

### 2026-03-11 Update (run-ready realtime draft example in `examples/`)

- Added run-focused task-set example:
  - `examples/task_sets/realtime_cnc01_smoke_task_set.json`
- Recommended pair for quick import/start:
  1. Task Definitions -> 태스크 세트 `불러오기`
     - `examples/task_sets/realtime_cnc01_smoke_task_set.json`
  2. PDDL -> Distributor profile `Import JSON`
     - `examples/task_distributor_profiles/realtime_cnc01_quickstart.json`
  3. Select/confirm agent and press `Start realtime`.

> Note: this smoke set uses `/spin` action for all 4 stages so command dispatch can be verified quickly before real CNC park/service actions are wired.

### 2026-03-11 Update (ActionGraph Start/Terminal node patch)

- Fixed issue where START node could disappear after deleting/recreating a task with the same id/name:
  - task canvas reload now checks **template data signature** (`id + version + updated_at + step_count`) instead of only `id`.
- Added **Start Nodes** category in node palette with `Start` item.
- Added new terminal node: **End (Warning)** (yellow).
- Extended terminal node editing:
  - `End (Error)` and `End (Warning)` now accept **Debug Message** text.
  - Debug message is stored in step `message`, and warning/error alert flag is stored in `alert`.
- Updated viewer rendering to show `WARNING` terminal subtype (yellow).

Regression test points:
- Delete task -> recreate same id/name -> open task: START should remain visible.
- Drag `Start` from palette: START node should be restored/repositioned without duplication.
- `End (Error)` / `End (Warning)` message save -> reload task: message should persist.

### 2026-03-11 Update (Start/End node editability patch)

- ActionGraph에서 Start/End 계열 노드 편집 권한을 확대:
  - **Start 노드 드래그 가능**
  - **Start/End/Error/Warning 노드 선택 가능**
  - **Start/End/Error/Warning 노드 삭제 가능** (키보드 Delete/Backspace 및 노드 헤더 X 버튼)
- 팔레트 기반 Start 복구 워크플로우 보완:
  - `Start`를 드래그해 캔버스에 놓으면 기존 START를 교체/복구하고 entry 연결은 유지.

### 2026-03-11 Update (PDDL realtime UX/stability + task ordering patch)

- PDDL Realtime:
  - `Start realtime` 비활성화 판단을 `selectedBTs(cache)` 기준에서 `selected task id` 기준으로 보강.
  - 캐시 지연/복원 직후에도 Task 선택이 정상 인식되도록 개선.
  - Realtime `Stop` 처리 보강:
    - stop 응답이 `{message}` 형식이어도 로컬 세션 상태를 정리하도록 수정.
    - stop 진행중 버튼 비활성화 + 상태 문구 표시.
  - PDDL 페이지 최초 진입 시 task 미선택이면 첫 task 자동 선택(1회)으로 시작 편의 개선.
- ActionGraph task 목록:
  - task 목록을 최신 수정 시각 기준(내림차순)으로 정렬.
  - 새 task 생성 직후 해당 task가 리스트 상단/선택 상태를 안정적으로 유지하도록 `pending created task` 선택 로직 추가.

### 2026-03-11 Update (PDDL realtime stale-session recovery)

- Fixed stuck state where Realtime could remain disabled with:
  - `Start realtime 비활성화 사유: 이미 Realtime 세션이 실행 중입니다...`
  - while `Stop realtime` kept failing.
- Improvements:
  - Polling `/pddl/realtime-sessions/{id}` now auto-clears local session state when server says session is missing (404 / not found).
  - Stop handler now also treats `400 + not found` as stale-session cleanup case.
- `isRealtimeActive` now requires actual session payload (not just leftover session id), reducing false "running" lock.
- Stop button shown whenever a session id exists, so stale sessions can be manually cleaned.

### 2026-03-11 Update (planner enabling-step selection)

- Improved task-level planner to handle prerequisite chains (not only direct goal-effect tasks):
  - Previous behavior: choose only tasks that immediately set requested goal values.
  - New behavior: if no direct-goal task is currently executable, planner can select an executable task that **enables** preconditions of a goal-producing task.
- This fixes common chain case like:
  - goal: `cnc01_service_done=true`
  - where `go_to_cnc_and_park` must run before `run_cnc_service_cycle_01`.
- Also increased internal iteration budget for chained progression.

### 2026-03-11 Update (PDDL visualization workspace + realtime sequence view)

- PDDL 화면 UI를 시각화 중심으로 정리했습니다.
- Planning 설정(Resource/State/Agent/Goal)을 하나의 **Planning Setup Workspace** 섹션으로 묶고,
  상단 버튼(`PDDL Config 열기/숨기기`)으로 접고 펼칠 수 있게 변경했습니다.
- `Realtime PDDL Loop` 섹션은 기본 접힘(`defaultOpen=false`)으로 변경해 작업 집중도를 높였습니다.
- 새로운 **Realtime Agent Task Sequence** 섹션 추가:
  - agent별 `이전 → 지금 → 이후` task 블록 시각화
  - 카드 리스트는 스크롤 가능(max height)하여 agent가 많아도 확인 가능
  - task 블록 클릭 시 해당 task의 BT 흐름을 모달로 표시(ActionGraphViewer)

### 2026-03-12 Update (agent placeholder 지원: `{{agent.name}}`, `${agent_name}`)

- Task 편집의 `PDDL Config`에서 agent placeholder를 사용할 수 있도록 확장했습니다.
- 지원 placeholder:
  - planning state 변수명/값: `{{agent.name}}`, `{{agent.id}}`, `{{agent}}`
  - action goal runtime binding: `${agent_name}`, `${agent.name}`, `${agent_id}`, `${agent.id}`, `${agent}`
- 변경 포인트:
  - PDDL Config variable suggestion이 state 이름에서 agent 이름/ID 패턴을 자동 일반화
  - Available Variables 영역에 Agents/Placeholders 보드 추가
  - Goal Parameters runtime binding 목록에 agent 바인딩 항목 추가
- 백엔드 planner/grounding 확장:
  - `{{agent.*}}`가 포함된 planning task는 선택 agent 기준으로 per-agent task로 grounding
  - 선택된 agent 정보(`agent_name`, `agent_id`, dot 표기 포함)를 runtime params에 주입
  - bound-agent task는 해당 agent capability/online 조건으로 검증 및 할당

### 2026-03-12 Update (PDDL State bulk create via `{{resource.name}}`)

- PDDL 메뉴의 State 생성 UI를 강화했습니다.
- 이제 state 이름에 `{{resource.name}}` / `{{resource.id}}` / `{{resource}}` placeholder를 넣고 `+`를 누르면,
  Task Distributor의 resource instance 개수만큼 state를 일괄 생성합니다.
  - 예: `at_{{resource.name}}` + type=`bool` + initial=`false`
  - 생성 결과: `at_cnc01`, `at_cnc02`, `at_cnc03`, ...
- 중복 state 이름은 자동으로 skip하고, 생성/skip 요약 메시지를 표시합니다.
- State 생성 입력에 `initial value` 입력 UI를 추가했습니다(bool/int/string).

### 2026-03-12 Update (PDDL State bulk create now supports `{{agent.name}}` too)

- PDDL 메뉴 State 일괄 생성에서 agent placeholder도 확장되도록 수정했습니다.
  - 지원: `{{agent.name}}`, `{{agent.id}}`, `{{agent}}`
- 예:
  - `{{agent.name}}_status` → `Task Manager-001_status`, `Task Manager-002_status`, ...
- `{{resource.*}}`와 `{{agent.*}}`를 함께 쓰면 교차 확장되어 상태가 생성됩니다.

### 2026-03-12 Update (Realtime PDDL telemetry runtime-state overlay)

- Realtime PDDL 세션에 **외부/텔레메트리 상태값을 런타임 오버레이**로 주입할 수 있도록 확장했습니다.
- 핵심 포인트:
  - 세션의 `CurrentState`(planner 효과 누적)는 유지
  - 외부 상태는 `LiveState`로 오버레이되어 goal 선택/solve 시 반영
  - TTL을 줄 수 있어 stale 값 자동 만료 가능

신규 API:

- `POST /api/task-distributors/{distributorID}/runtime-state`
  - body:
    - `source` (string, optional)
    - `values` (map[string]string, required)
    - `ttl_sec` (number, optional)
- `GET /api/task-distributors/{distributorID}/runtime-state`
  - 해당 distributor의 활성 realtime session + merged live state 조회
- `DELETE /api/task-distributors/{distributorID}/runtime-state?source=...`
  - source 지정 시 해당 source만 삭제, 없으면 전체 runtime overlay 삭제

추가 연동:

- Agent가 `TaskStateUpdate(task_id="__planning_state__")`로 보내는 `variables`를
  realtime session runtime-state로 자동 반영합니다.
  - 적용 범위: 해당 agent를 포함한 realtime session들
  - 기본 TTL: 5초(신호 끊기면 자동 소거)

---

## Update Note (2026-03-12)

- PDDL > Task Distributor 목록에서 각 분배기의 `ID`를 이름 아래에 표시하도록 UI 개선.
- 목적: runtime-state 테스트(`mcs_runtime_state_cli`) 시 TD_ID를 웹에서 바로 확인.


---

## Update Note (2026-03-12, RTM Execute Auto-Heal)

- RTM Start 실행 시, 선택한 BT가 `deployed` 상태가 아니면 프론트에서 **자동 deploy 1회 후 execute** 하도록 개선.
- 백엔드 `ExecuteBehaviorTree`에서 deployment_status gating을 제거하여, scheduler auto-deploy 경로를 통해 실행 가능하도록 개선.
- QUIC deploy 요청에 20초 timeout을 적용하여 `Deploying graph to RTM...` 무한 대기 문제를 완화.
- `deploying` 상태가 45초 이상 지속되면 API 응답 시 `failed`로 자동 정리(stale deployment normalization).
- Deploy/Execute 실패 시 UI alert에 서버 상세 에러 메시지(`error`/`message`)가 표시되도록 개선.


---

## Update Note (2026-03-13, RTM Deploy 504 loop mitigation)

- Agent Dashboard Start 경로에서 execute 직전 assignment의 최신 `deployment_status`를 서버에서 재조회하도록 보강.
- `deployment_status=deploying` 상태에서는 Start 시 재배포를 반복하지 않고 execute 경로로 진행하도록 수정.
- auto-deploy는 `pending/failed/outdated` 상태에서만 시도하도록 제한.
- auto-deploy 오류 시 상태를 1회 재조회하여 이미 `deployed/deploying`이면 실행을 계속하도록 보강.
- `deploying` 상태 배너에서 Deploy/Retry 버튼을 숨겨 중복 deploy 클릭으로 인한 504 루프를 완화.
- execute 실패 시 assigned graph / agent state 쿼리를 즉시 invalidate 하여 UI 상태 동기화 개선.


---

## Update Note (2026-03-13, Realtime solve ignores undeclared runtime telemetry keys)

- Realtime PDDL solve 경로에서 `initial_state`를 Task Distributor의 선언된 planning state 목록으로 필터링하도록 수정했습니다.
- 목적: agent telemetry runtime-state에 planning model에 없는 키(예: `*_battery_valid`)가 포함되어도
  `initial state variable ... is not declared in planning_state` 오류로 solve가 중단되지 않도록 하기 위함입니다.
- 적용 파일: `central_server_go/internal/api/realtime_pddl.go`


---

## Update Note (2026-03-14, Realtime Stop stale execution cleanup)

- `Stop realtime` 시 세션 상태만 멈추고 일부 실행이 `running`으로 남아 리소스 락이 유지되는 케이스를 보완했습니다.
- Stop 처리 시:
  - 세션의 `ActiveExecutionID`를 즉시 비우고
  - 세션 컨텍스트를 먼저 cancel한 뒤
  - 현재 active execution 취소 + `problem_id`가 `realtime:<sessionID>:`로 시작하는
    모든 `running/pending` execution을 추가로 정리하도록 수정했습니다.
- 적용 파일: `central_server_go/internal/api/realtime_pddl.go`


---

## Update Note (2026-03-14, QUIC StartTask send timeout guard)

- Realtime PDDL 실행 중 일부 step이 `running/pending`으로 고착되던 케이스를 완화했습니다.
- 원인: `StartTask` 전송 경로에서 QUIC stream open/write가 timeout 없이 대기할 수 있어,
  scheduler dispatch 경로가 반환되지 않는 상황이 발생할 수 있었음.
- 수정:
  - `RawQUICHandler.sendToAgent()`에 5초 timeout context 적용
  - stream write deadline(5초) 적용
- 적용 파일: `central_server_go/internal/grpc/raw_quic_handler.go`
- 기대 효과:
  - StartTask 전송 경로의 무기한 block 방지
  - timeout 발생 시 다음 tick/heartbeat에서 재시도 가능


---

## Update Note (2026-03-14, Goal state manual entry + placeholder expansion)

- PDDL Goal/Realtime Goal 편집기에서 state 변수를 목록 선택만 하던 제한을 완화했습니다.
- Goal Editor에 **직접 변수 입력** UI를 추가하여 다음과 같은 변수명을 수동으로 등록할 수 있습니다:
  - `{{resource.name}}_status`
  - `{{agent.name}}_location`
- Planner solve 경로에서 `goal_state`의 placeholder를 실행 직전에 확장하도록 보강했습니다.
  - 지원: `{{resource.*}}`, `{{agent.*}}`
  - resource/agent 교차 확장 지원
  - unresolved placeholder 또는 동일 goal 변수에 대한 충돌값은 명확한 에러로 반환
- 적용 파일:
  - `central_server/frontend/src/pages/PDDL/components/GoalEditor.tsx`
  - `central_server_go/internal/api/pddl.go`


---

## Update Note (2026-03-14, Realtime dispatch stuck prevention)

- Realtime 실행 중 step이 `pending/running`에 고착되던 케이스를 재발 방지하도록 보강했습니다.
- 증상:
  - `run_cnc_service_cycle_02` 같은 step이 시작되지 못한 채 queue에 남아
  - `send start_task failed ... failed to open stream: context deadline exceeded` 로그가 반복
  - planner result state 반영 지연으로 Session state / Live state 괴리 발생

### 변경 사항

1) Scheduler dispatch 재시도 상한
- 파일: `central_server_go/internal/executor/scheduler.go`
- `maxDispatchAttempts = 3` 도입
- queued task dispatch 실패가 상한을 넘으면 task를 `failed`로 종료 처리하여 무한 pending 차단

2) QUIC stale connection 강제 재연결
- 파일: `central_server_go/internal/grpc/raw_quic_handler.go`
- `SendStartTask`에서 stream open timeout 계열 오류 시:
  - 해당 agent QUIC connection 강제 close
  - `handleDisconnect`로 offline 처리
  - agent 재접속을 유도해 command stream 회복

### 기대 효과

- start_task 전송 실패가 반복되어도 동일 task가 무기한 queue에 고착되지 않음
- 연결 이상 시 빠르게 reconnect 경로로 전환되어 다음 tick에서 재시도 가능


---

## Update Note (2026-03-14, Realtime Goal activation condition manual input + `{{agent.name}}` support)

- Realtime Goal의 **활성 조건 변수 입력**을 목록 선택 전용에서 `input + datalist`로 변경했습니다.
  - 직접 입력 가능: `{{agent.name}}_status`, `{{agent.name}}_location`, `{{resource.name}}_status` 등
  - 상태 변수 + placeholder 추천 목록 제공
- 백엔드 Realtime 활성조건 평가에서 `{{agent.*}}`, `{{resource.*}}` placeholder를 해석하도록 보강했습니다.
  - 세션의 agent/resource 바인딩 조합으로 확장 평가
  - placeholder 조건은 바인딩 조합 중 하나라도 만족하면 true
  - 기존 정적 조건(`agent001_status == idle`) 동작은 유지
- 적용 파일:
  - `central_server/frontend/src/pages/PDDL/components/RealtimeGoalEditor.tsx`
  - `central_server_go/internal/api/realtime_pddl.go`


---

## Update Note (2026-03-14, Realtime activation-binding to execution target)

- Realtime Goal 활성조건에서 `{{agent.name}}_*` / `{{resource.name}}_*`가 만족되면,
  이제 해당 바인딩(agent/resource)이 실제 solve/dispatch에도 반영됩니다.

### 변경 사항

1) 활성조건 평가가 binding을 반환
- `realtimeActivationConditionsMet`가 조건을 만족한 agent/resource 바인딩을 반환
- `selectRealtimeGoal`이 selected goal + selected binding을 함께 반환

2) solve 대상 고정
- selected binding에 agent가 있으면 solve 시 `AgentIDs`를 해당 agent 1개로 제한
- goal_state는 solve 전에 selected binding으로 placeholder 치환 적용

3) 실패 캐시 분리
- `goalFailureKey`에 binding 정보를 포함해
  같은 goal/state라도 다른 agent/resource binding은 별도로 취급

4) UI/응답 가시성
- Realtime session 응답에 아래 필드 추가:
  - `selected_agent_id`, `selected_agent_name`
  - `selected_resource_id`, `selected_resource_name`
- PDDL 화면 상단 배지에 선택된 agent/resource 표시

### 적용 파일

- `central_server_go/internal/api/realtime_pddl.go`
- `central_server/frontend/src/types/index.ts`
- `central_server/frontend/src/pages/PDDL/index.tsx`


---

## Update Note (2026-03-14, Realtime multi-agent fairness + retry behavior)

- 다중 agent에서 활성조건 `{{agent.name}}_*`가 동시에 만족될 때
  항상 같은 agent로 편향되던 동작을 완화했습니다.
- 실행 실패 후 동일 상태에서 재시도를 막던 동작을 완화해
  transient 오류(예: deploy/stream timeout) 뒤 자동 재시도가 가능해졌습니다.

### 변경 사항

1) agent 선택 라운드로빈
- Realtime session에 마지막 선택 agent를 보관하고
  다음 tick 활성조건 평가 시 해당 agent 다음 순서부터 탐색합니다.
- 결과적으로 agent1/agent2가 모두 idle이면 한쪽으로만 몰리지 않고 교대로 선택될 수 있습니다.

2) 실패 재시도 완화
- execution이 `failed/cancelled`로 끝났을 때
  동일 goal/state를 영구 차단하지 않도록 조정했습니다.
- 다음 tick에서 동일 상태라도 재계획/재실행을 시도합니다.

### 적용 파일

- `central_server_go/internal/api/realtime_pddl.go`


---

## Update Note (2026-03-14, QUIC stale-stream self-heal expansion)

- 일부 agent가 online/idle로 보이지만 실제 StartTask가 내려가지 않는 반쯤 열린 연결(stale stream) 상황을 줄이기 위해
  stale 감지 범위를 확장했습니다.

### 변경 사항

- `SendStartTask` 뿐 아니라 아래 경로에서도 stream-open timeout 계열 오류를 stale로 판단:
  - `SendPing`
  - `broadcastFleetState`
- stale로 판단되면 해당 agent QUIC connection을 강제 close하고 `handleDisconnect`를 호출해
  offline 처리 후 재접속 루프로 유도합니다.

### 적용 파일

- `central_server_go/internal/grpc/raw_quic_handler.go`


---

## Update Note (2026-03-14, Realtime tick multi-dispatch for idle agents)

- Realtime PDDL tick에서 기존처럼 단일 active execution만 처리하지 않고,
  **동일 tick에서 idle agent별로 실행을 여러 개 발행**할 수 있도록 변경했습니다.

### 변경 사항

1) 세션 내부 실행 추적 구조 확장
- 파일: `central_server_go/internal/api/realtime_pddl.go`
- `ActiveExecutions(map[execution_id]context)` 도입
- execution context에 plan/goal/binding(agent/resource)/started_at 저장

2) tick 동작 변경 (병렬 dispatch 느낌)
- 기존: `syncExecution` 후 active가 있으면 즉시 return (직렬)
- 변경:
  - `syncExecutions`로 모든 active execution 상태 동기화
  - running/pending execution에서 busy agent/resource 집계
  - idle agent를 순회하며 goal selection/solve/dispatch를 개별 수행
  - 성공 시 같은 tick 안에서 다음 idle agent도 계속 dispatch 시도

3) Stop 시 전체 active execution 취소
- 단일 `active_execution_id`만 취소하던 방식에서
  세션의 모든 active execution ID를 취소하도록 확장

4) 응답 확장
- realtime session 응답에 `active_execution_ids` 필드 추가
- `live_state` 병합 시 active execution들의 planning state를 함께 반영

### 검증

- `docker-compose build go-backend` 성공


---

## Update Note (2026-03-14, Realtime Agent Task Sequence multi-execution aggregation)

- Realtime에서 `active_execution_id` 하나만 기준으로 시퀀스를 표시하던 문제를 보완했습니다.
- 이제 `active_execution_ids` 전체를 폴링/병합하여, 한 agent 실행 중 다른 agent 행이 `없음`으로 비어 보이는 현상을 줄였습니다.

### 변경 사항

1) 타입 확장
- 파일: `central_server/frontend/src/types/index.ts`
- `RealtimeSession.active_execution_ids?: string[]` 추가

2) 프론트 폴링 로직 확장
- 파일: `central_server/frontend/src/pages/PDDL/index.tsx`
- Realtime 세션에서 active execution ID 목록을 수집
- 각 execution을 병렬 조회하여 `realtimeExecutionMap`으로 유지
- 리소스 할당도 다중 execution 기준으로 merge

3) 시퀀스/디스패치 집계 소스 변경
- `Realtime Agent Task Sequence`와 `agent dispatch` 집계 시
  단일 `execution.steps` 대신 다중 execution에서 병합한 step 목록을 사용
- 결과적으로 agent001/agent002가 동시에 진행되는 상황을 화면에서 연속적으로 확인 가능

### 검증

- `docker-compose build frontend` 성공

### 추가 보정 (2026-03-14, Sequence 초기 completed 표시 완화)

- Realtime 시작 직후 일부 agent에서 `이전=completed/지금=completed`가 먼저 보이는 혼란을 줄이기 위해
  sequence 표시 규칙을 보정했습니다.
- 변경:
  - 다중 execution 폴링 시 이미 terminal 상태(`completed/failed/cancelled`) execution은 sequence 집계에서 제외
  - timeline에 `running/pending`이 없고 완료 항목만 있으면, 완료 항목을 `지금`이 아니라 `이전`으로만 표시


---

## Update Note (2026-03-16, Realtime 중복 CNC 점유/즉시 완료(스킵) 방지)

다중 agent realtime 실행에서 간헐적으로
- 두 agent가 같은 CNC(cnc01)를 동시에 선택하거나
- 로봇이 실제로 움직이지 않았는데 step이 바로 completed로 넘어가는
문제를 보완했습니다.

### 변경 사항

1) Realtime busy resource 집계 강화
- 파일: `central_server_go/internal/api/realtime_pddl.go`
- execution의 selected binding뿐 아니라 **plan assignment runtime params**에서도
  busy resource를 수집하도록 변경:
  - `resource_id`, `resource_name`, `resource.id`, `resource.name`
  - `__fleet_resource_bindings`(존재 시)
- 적용 지점:
  - active execution 동기화 시 busy 집계
  - 같은 tick에서 새 execution 시작 직후 busy map 반영

2) PlanExecutor waitForTask의 오판정 수정
- 파일: `central_server_go/internal/executor/plan_executor.go`
- 기존:
  - task가 active map에 없고 DB가 terminal이 아니어도 일부 경로에서 성공으로 처리
- 변경:
  - `pending/running/paused`는 계속 대기
  - DB 미조회/전파 지연 시 재시도(최대 20 poll)
  - `completed/failed/cancelled`에서만 종료 판정

### 검증

- `docker-compose build go-backend` 성공

---

## Update Note (2026-03-16, Realtime fixed-goal busy CNC conflict guard)

다중 agent realtime에서 goal이 `cnc01_status=running` 같은 **고정 변수 기반**일 때,
동일 tick에 busy CNC가 다시 선택될 수 있는 경로를 보완했습니다.

### 변경 사항

- 파일: `central_server_go/internal/api/realtime_pddl.go`
- `tick()`를 agent별 단일 goal 선택에서 **goal 후보 순회 방식**으로 보완:
  - 한 goal이 충돌/실패하면 같은 agent에 대해 다음 goal 후보를 시도
- solve 결과 plan에 대해 busy resource 충돌 검사 추가:
  - `resource_id`, `resource_name`, `resource.id`, `resource.name`, `__fleet_resource_bindings`
- busy-resource 충돌은 transient로 간주하여 failed-state 캐시에 고정하지 않음
  (resource 해제 후 자동 재시도 가능)

### 기대 효과

- 같은 tick에서 두 agent가 동일 CNC를 연속 선택하는 상황 완화
- 고정형 realtime goal 구성에서도 resource 점유 충돌 방지 일관성 향상

### 검증

- `docker-compose build go-backend` 성공
- `docker-compose rm -f go-backend && docker-compose up -d go-backend` 재기동 성공

---

## Update Note (2026-03-16, Realtime multi-agent: stale deploy + partial-effect state drift fix)

Realtime 다중 agent 실행에서 아래 문제가 확인되어 보완했습니다.

- `agent002`는 park는 완료했지만 `run_cnc_service_cycle_01`이 반복 실패
- `cnc02_status`가 `idle`로 남아 `agent001`이 동일 CNC로 재할당되는 역전 발생
- failed execution에서 앞 step 성공 결과(예: `agent002_location=cnc02`)가 상태에 반영되지 않음

### 원인

1) 에이전트 로그에 `Behavior tree not found: run_cnc_service_cycle_01`
- 서버 assignment 메타는 `deployed`였지만, 에이전트 내부 저장소에는 그래프가 없어 실행 실패.

2) Realtime 동기화가 execution 전체가 실패하면 result state를 모두 버림
- step1 성공 + step2 실패 케이스에서도 step1 효과가 current_state에 반영되지 않음.

### 변경 사항

1) Runtime plan execution 시 base graph 강제 redeploy
- 파일:
  - `central_server_go/internal/executor/scheduler.go`
  - `central_server_go/internal/executor/runtime_graph_materializer.go`
  - `central_server_go/internal/state/graph_cache.go`
  - `central_server_go/internal/grpc/raw_quic_handler.go`
- plan execution 경로에서 `prepareExecutionGraph(..., forceBaseDeploy=true)` 적용
- `ensureGraphDeployed(..., force bool)`로 확장하여 stale deployed 메타 우회
- `ensureGraphDeployed` fast-path에 graph cache 배포 여부를 추가해
  assignment 메타만으로 skip하지 않도록 보완
- agent disconnect 시 해당 agent의 deployed graph cache를 전부 invalidate
  하여 재접속 후 stale cache 재사용 방지

2) failed/cancelled execution의 partial effects 반영
- 파일: `central_server_go/internal/api/realtime_pddl.go`
- `applyRealtimePartialEffects(...)` 추가
- 실패 execution에서도 `snapshot.Steps` 중 `completed` step의 result_state는 current_state에 반영

3) agent idle 판정 강화
- 파일: `central_server_go/internal/api/realtime_pddl.go`
- 후보 상태 키를 전체 평가하여 하나라도 non-idle이면 idle로 보지 않도록 보수화

### 기대 효과

- agent 재기동/graph 유실 이후에도 runtime task 실행 안정성 향상
- park 성공 후 후속 step 실패 시에도 위치 상태 drift 감소
- 다중 agent에서 동일 CNC 재할당/점유 역전 케이스 완화

### 검증

- `docker-compose build go-backend` 성공
- `docker-compose rm -f go-backend && docker-compose up -d go-backend` 성공
- `run_cnc_service_cycle_01`를 agent002에 직접 실행:
  - 실행 시작 API `200 OK`
  - 생성 task 상태 `completed` 확인

---

## Update Note (2026-03-16, Realtime multi-agent starvation guard)

Realtime 다중 agent에서 특정 agent만 반복 dispatch되고 다른 agent가 idle로 남는
starvation 케이스를 완화했습니다.

### 변경 사항

- 파일: `central_server_go/internal/api/realtime_pddl.go`
- `tick()`의 target-resource affinity skip 로직 제거
  - 기존: 다른 idle agent가 target resource에 있으면 현재 agent dispatch를 skip
  - 변경: skip하지 않고 solve/dispatch를 시도
- resource 충돌 회피는 기존 busy-resource 충돌 검사(`planBusyResourceConflicts`)로 일관 처리

### 기대 효과

- stale/지연 location 상태로 인한 agent 편향 완화
- 다중 agent 병렬 dispatch 안정성 향상

### 검증

- `docker-compose build go-backend` 성공
- `docker-compose rm -f go-backend && docker-compose up -d go-backend` 성공
- `docker-compose ps go-backend` healthy 확인

---

## Update Note (2026-03-16, Realtime go_to step false-complete root cause fix)

Realtime/PDDL에서 `go_to_cnc_and_park`가 실제 park action 수행 없이 빠르게
completed 처리되는 케이스를 수정했습니다.

### 원인

- Deploy 시 canonical edge의 조건이 누락됨
  - canonical graph edge: `type=conditional`, `config.condition` 형태
  - QUIC deploy 직렬화 코드가 top-level `condition`만 읽고
    `config.condition`을 무시하고 있었음
- 결과적으로 agent가 조건 없는 conditional edge를 받아
  실패/비정상 경로에서도 success terminal 전이 가능

### 변경 사항

- 파일: `central_server_go/internal/grpc/raw_quic_handler.go`
- `buildDeployGraphMessage()`의 edge 파싱 구조 확장
  - `edges[].config.condition` 지원 추가
  - 우선순위: `edges[].condition` → 비어있으면 `edges[].config.condition`
  - `buildEdgeMessage(..., condition)`에 조건을 확실히 전달

### 검증

- `docker-compose build go-backend` 성공
- `docker-compose rm -f go-backend && docker-compose up -d go-backend` 성공
- `docker-compose ps go-backend` healthy 확인
- 재배포 후 agent 로컬 graph(`/tmp/robot_agent/graphs/go_to_cnc_and_park.json`)에서
  conditional edge의 `condition` 필드가 정상 포함됨 확인

---

## Update Note (2026-03-16, Realtime 2-phase dispatch arbitration for multi-agent conflict prevention)

Realtime 다중 agent 실행에서 같은 tick에 자원 중복 점유 시도(동일 CNC로 동시 park 시도)가
발생할 수 있는 경로를 줄이기 위해 dispatch 방식을 보강했습니다.

### 변경 사항

- 파일: `central_server_go/internal/api/realtime_pddl.go`
- `tick()` 스케줄링을 2-phase로 변경
  1. idle agent별 candidate plan 생성
  2. 후보 plan들을 goal priority + agent 순서로 정렬 후 global arbitration
- arbitration에서 같은 tick 자원 예약 강화
  - `planBusyResourceConflicts(...)` 기반 충돌 검사
  - binding의 `resource_id` / `resource_name`도 예약 반영
- 비충돌 후보만 실행 시작하여 같은 tick 중복 dispatch를 방지

### 기대 효과

- 동일 CNC에 대한 same-tick 중복 dispatch 감소
- multi-agent 병렬 실행 시 자원 충돌 회피 일관성 향상

### 참고 (ABORT 관련)

- 액션 ABORT 자체는 스케줄러와 별개로 FMS/API 오류로도 발생할 수 있습니다.
  - 예: `req_login_api failed`, `invalid task name`, worker-task 미지원
- 이번 패치는 “중복 dispatch/충돌 선택” 경로를 줄이는 목적입니다.

### 검증

- `docker-compose build go-backend` 성공
- `docker-compose down && docker-compose up -d --build` 성공

---

## Update Note (2026-03-16, Realtime idle 판단 개선: stale error 상태로 인한 단일 agent 편향 완화)

Realtime 다중 agent 실행에서 한 agent만 반복 선택되는 현상을 줄이기 위해
agent idle 판정 로직을 보강했습니다.

### 배경

- merged state에 agent별 alias 키가 함께 존재할 수 있습니다.
  - 예: `agent001_status=idle` + `77195..._status=error`
- 기존 idle 판정은 status/mode 후보를 모두 AND 평가하여
  하나의 stale `error` 값만으로 agent를 non-idle로 볼 수 있었습니다.

### 변경 사항

- 파일: `central_server_go/internal/api/realtime_pddl.go`
- `isRealtimeAgentIdle(...)` 로직 수정
  1. `*_is_executing` 키를 **최우선**으로 idle/busy 판정
     - `true/1/yes/executing/running`이면 busy
     - exec flag가 존재하면 status/mode는 보조 판정으로 사용하지 않음
  2. status/mode fallback 시 alias 우선순위 적용
     - `agentName_*` > `normalizedAgentID_*` > `rawAgentID_*`
     - 그룹 단위 판정으로 alias 혼합에 의한 과도한 non-idle 판정 방지

### 기대 효과

- stale status(error) 오염으로 인한 agent starvation 완화
- multi-agent realtime dispatch 안정성 향상

### 검증

- `docker-compose rm -f go-backend && docker-compose up -d --build go-backend`
- `docker-compose ps go-backend` healthy 확인

---

## Update Note (2026-03-16, Realtime provisional reservation for same-tick multi-agent fallback)

다중 agent realtime dispatch에서 한 tick 내 두 번째 agent가 대체 리소스(cnc02)로
fallback하지 못하고 탈락하던 문제를 수정했습니다.

### 원인

- 기존 2-phase 후보 생성은 모든 agent 후보를 동일 초기 busy set으로 계산했습니다.
- 두 agent가 동시에 후보를 만들면 둘 다 cnc01 기반 plan을 들고 오기 쉬웠고,
  arbitration에서 첫 후보만 채택, 둘째는 충돌로 drop 되었습니다.
- 둘째 agent는 같은 tick에서 cnc02 재탐색 기회가 없었습니다.

### 변경 사항

- 파일: `central_server_go/internal/api/realtime_pddl.go`
- tick 후보 생성 로직을 provisional reservation 기반으로 변경:
  1. agent 순서대로 후보를 생성
  2. 후보 채택 시 provisional busy resource/agent를 즉시 갱신
  3. 다음 agent는 갱신된 busy set으로 solve하여 대체 리소스를 선택 가능

### 기대 효과

- 같은 tick에서 resource 충돌 시 두 번째 agent가 다른 CNC로 fallback 가능
- “항상 한 대만 움직이는” 현상 완화

### 검증

- `docker-compose rm -f go-backend && docker-compose up -d --build go-backend`
- `docker-compose ps go-backend` healthy 확인
- backend 재기동 후 realtime sessions 초기화 확인

---

## Update Note (2026-03-16, Realtime activation-condition stale status normalization)

Realtime 다중 agent에서 `{{agent.name}}_status == idle` 조건을 쓰는 경우,
telemetry에 stale `error/warning` 문자열이 남아 있으면 실제 비실행 상태(`*_is_executing=false`)여도
해당 agent가 후보에서 제외될 수 있던 문제를 보완했습니다.

### 변경 사항

- 파일: `central_server_go/internal/api/realtime_pddl.go`
- `planningConditionsMet(...)` 비교 전에
  `normalizePlanningConditionCurrentValue(...)` 적용
- 정규화 규칙:
  - 기대값: `idle` 또는 `ready`
  - 변수: `*_status` / `*_mode`
  - 대응 `*_is_executing`가 false 계열
  - 현재값이 `error` 또는 `warning`
  - 위 조건이면 비교용 현재값을 `idle`로 간주

### 기대 효과

- stale status 문자열로 인한 agent starvation 완화
- 두 agent 동시 dispatch(리소스 충돌 없는 범위)의 실사용 안정성 향상

### 검증

- `docker-compose rm -f go-backend && docker-compose up -d --build go-backend`
- `docker-compose ps go-backend` healthy 확인

---

## Update Note (2026-03-16, Realtime dispatch fail-fast 완화 + task failure 원인 가시화)

Realtime PDDL에서 `go_to_cnc_and_park`가 간헐적으로 연속 실패하면서
agent 상태가 `error/idle`로 튀는 현상을 줄이기 위한 안정화 패치입니다.

### 변경 사항

1) Scheduler dispatch 재시도 강화
- 파일: `central_server_go/internal/executor/scheduler.go`
- `start_task` 전송 오류 시 즉시 fail로 올리지 않고 **지수 backoff 재시도**하도록 변경
  (1s, 2s, 4s ... 최대 15s).
- 단기 transient 오류 구간에서는 pending 유지, 장시간 오류(`maxDispatchErrorWindow=45s`)
  지속 시에만 failed 처리.
- dispatch lease를 `5s -> 20s`로 확장해 false retry 가능성 완화.

2) Task 실패 에러 메시지 전파 강화
- 파일: `central_server_go/internal/executor/scheduler.go`
- RunningTask에 `ErrorMessage`를 저장하고 종료 시 DB 상태 업데이트에 함께 기록.
- 파일: `central_server_go/internal/executor/plan_executor.go`
- `waitForTask()`가 in-memory task 실패 시에도 상세 에러를 반환하도록 보완.

3) QUIC TaskStateUpdate 에러 fallback 추가
- 파일: `central_server_go/internal/grpc/raw_quic_handler.go`
- step result 에러가 비어있어도 blocking reason / 기본 terminal 메시지를 fallback으로 기록.
- scheduler completion callback에도 동일 에러 문자열 전달.

### 기대 효과

- Realtime PDDL에서 transient dispatch 오류로 인한 즉시 실패 빈도 감소
- `task failed` 한 줄만 보이던 문제 완화(원인 추적 가능성 향상)
- agent 상태 플래핑(`error <-> idle`)의 원인 파악/대응 용이

---

## Update Note (2026-03-16, Runtime binding 근본 대응: per-task deploy 제거)

PDDL 경로에서만 간헐적으로 발생하던 `go_to_cnc_and_park` 즉시 실패/스킵 이슈의
핵심 원인인 **runtime binding 시 per-task concrete graph deploy**를 제거했습니다.

### 변경 사항

1) 서버 runtime materializer 동작 변경
- 파일: `central_server_go/internal/executor/runtime_graph_materializer.go`
- 기존:
  - runtime placeholder 감지 시 concrete graph 생성
  - task마다 새 graph deploy 후 실행
- 변경:
  - placeholder가 있어도 static graph 유지
  - runtime params를 StartTask로 전달해 agent가 실행 시 치환
  - 즉, **deploy는 그래프 버전 변경 시에만** 수행

2) runtime task 강제 redeploy 정책 제거
- 파일: `central_server_go/internal/executor/scheduler.go`
- 기존 `planExecutionID != ""`일 때 force redeploy 하던 경로 제거
- stale/outdated일 때만 auto-redeploy 유지

3) Agent runtime 변수 치환 강화
- 파일:
  - `ros2_robot_agent/src/graph/executor.cpp`
  - `ros2_robot_agent/src/agent.cpp`
- `${...}` 변수 치환을 JSON(object/array) 내부까지 재귀 지원
- `field_sources.constant`, `data`, 일반 params 모두 치환되도록 보강

### 기대 효과

- PDDL 실행 시 task마다 deploy race/timeout이 끼어들던 구조 제거
- 단독 task 실행과 유사한 안정성으로 수렴
- Realtime loop에서 즉시 fail/skip 반복 빈도 감소

---

## Update Note (2026-03-16, Realtime resource binding 전달 정확도 + park running 고착 완화)

### 1) Realtime에서 resource 배정은 분리되는데 실제 goal이 `cnc01`로 고정되던 문제

- 현상:
  - Planner/Realtime UI 상으로는 `agent001->cnc01`, `agent002->cnc02`처럼 분리되어 보이지만
    실제 `navigate_and_park` goal은 기본값(`cnc01`)로 전달되는 케이스 발생.
- 원인:
  - BT goal params의 `field_sources.source=expression`(예: `${resource_name}`)을
    agent 실행 경로가 해석하지 못해 data 기본값이 그대로 사용됨.
- 수정:
  - 파일: `ros2_robot_agent/src/agent.cpp`
  - `field_sources` 처리에 `expression` 분기 추가:
    - `${...}` runtime 변수 치환
    - JSON parse 시도 후 실패 시 문자열로 반영
- 기대 효과:
  - Realtime/PDDL runtime binding으로 결정된 `resource_name`이 실제 action goal로 전달됨.

### 2) park 물리 완료 후에도 task가 `running`으로 오래 남는 고착 완화

- 현상:
  - Concert report/worker polling이 불안정한 환경(SAS0008, websocket reconnect 반복)에서
    park 완료 판정이 지연되어 step/task가 장시간 running 유지.
- 수정(AMR action server 측):
  - 파일: `amr_gc250/amr_gc250/navigate_and_park_action_server.py`
  - 보강:
    1. report polling 중 worker 상태 병행 확인 (non-idle -> idle 관측 시 success 가능)
    2. worker poll transient 에러 재연결 시도 추가
    3. `poll_unavailable_success_after`(default 45s) 도입:
       - polling unavailable이 지속되면 무기한 running 대신 경고와 함께 success 종료
- 기대 효과:
  - polling 불안정 상황에서 running 고착 완화, Realtime orchestration 진행성 향상.

## 2026-03-16 - Realtime reserved_by + telemetry location 기반 동기화 (leave 없는 운영 보강)

- 목적:
  - 멀티 에이전트 realtime PDDL에서 동일 CNC 중복 접근을 줄이고,
    `leave_cnc` 없이도 예약 상태가 stale로 남지 않도록 보강
- 백엔드(`realtime_pddl.go`) 변경:
  - resource 후보 필터에 `*_reserved_by` 상태 반영
    - 다른 agent가 예약한 resource는 해당 agent 후보에서 제외
  - live/runtime merge 단계에서 예약 상태 reconcile 추가
    - stale release: holder agent의 `*_location`이 해당 resource가 아니면 `reserved_by=none`
    - auto claim: 비어있는 `reserved_by`에서 특정 resource에 위치한 agent가 1명일 때 자동 점유 반영
- 전제:
  - Task Distributor state에 `cnc01_reserved_by`, `cnc02_reserved_by` 같은 string 상태를 선언해야 함
  - task precondition/result에서도 reserved_by를 함께 사용해야 planner 일관성이 높아짐
- 검증:
  - `docker-compose build go-backend` 빌드 통과
- 상세:
  - `~/mcs_dev/PDDL_EXECUTION_FIX_NOTES.txt`
