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
