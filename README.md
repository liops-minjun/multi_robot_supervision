# Multi-Robot Supervision System

여러 로봇(AMR, 매니퓰레이터, 지게차 등)을 중앙에서 관리하는 Fleet Management System입니다.

## 주요 기능

- **실시간 모니터링**: WebSocket을 통한 로봇 상태 실시간 모니터링
- **Capability Auto-Discovery**: ROS2 Action Server 자동 탐지 및 등록
- **State Management**: Robot Type별 상태 정의 및 Action Mapping
- **Action Graph**: 시각적 그래프 편집기를 통한 작업 시나리오 구성
- **Task 실행**: Graph 기반 작업 실행 및 진행 상황 추적

## 아키텍처

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Web Frontend (React)                               │
│              Dashboard / Monitoring / Action Graph Editor                    │
└─────────────────────────────────┬───────────────────────────────────────────┘
                                  │ REST API / WebSocket
                                  ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Central Server (Go)                                  │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐                       │
│  │  REST API    │  │  WebSocket   │  │ gRPC/QUIC    │                       │
│  │   :8081      │  │   Monitor    │  │ :9090/9444  │                       │
│  └──────────────┘  └──────────────┘  └──────────────┘                       │
│                          Neo4j + State Manager                               │
└─────────────────────────────────┬───────────────────────────────────────────┘
                                  │ QUIC
            ┌─────────────────────┼─────────────────────┐
            ▼                     ▼                     ▼
     ┌────────────┐        ┌────────────┐        ┌────────────┐
     │Fleet Agent │        │Fleet Agent │        │Fleet Agent │
     │   (C++)    │        │   (C++)    │        │   (C++)    │
     │            │        │            │        │            │
     │  ↕ ROS2    │        │  ↕ ROS2    │        │  ↕ ROS2    │
     │  Actions   │        │  Actions   │        │  Actions   │
     └────────────┘        └────────────┘        └────────────┘
```

---

## 로컬 개발 환경 설정

### 1. 필수 요구사항

| 구성 요소 | 버전 | 용도 |
|-----------|------|------|
| Go | 1.21+ | Central Server |
| Node.js | 18+ | Frontend |
| Neo4j | 5.x | Graph Database |
| ROS2 | Humble | Fleet Agent |

### 2. Ubuntu 22.04 설치 가이드

#### Step 1: Go 설치

```bash
# Go 1.22 설치
wget https://go.dev/dl/go1.22.0.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.22.0.linux-amd64.tar.gz

# PATH 설정
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
echo 'export GOPATH=$HOME/go' >> ~/.bashrc
echo 'export PATH=$PATH:$GOPATH/bin' >> ~/.bashrc
source ~/.bashrc

# 확인
go version
```

#### Step 2: Neo4j Community 설치

```bash
# Neo4j Community (Docker 권장)
docker run -d --name fleet-neo4j \
  -e NEO4J_AUTH=neo4j/neo4j123 \
  -p 7474:7474 -p 7687:7687 \
  neo4j:5

# 확인
docker exec fleet-neo4j cypher-shell -u neo4j -p neo4j123 "RETURN 1"
```

#### Step 3: Node.js 설치 (이미 설치된 경우 스킵)

```bash
# Node.js 20 LTS 설치
curl -fsSL https://deb.nodesource.com/setup_20.x | sudo -E bash -
sudo apt install -y nodejs

# 확인
node --version
npm --version
```

#### Step 4: ROS2 Humble 설치 (Fleet Agent 용, 이미 설치된 경우 스킵)

```bash
# ROS2 Humble 설치 (Ubuntu 22.04)
sudo apt update && sudo apt install -y software-properties-common
sudo add-apt-repository universe
sudo apt update && sudo apt install curl -y
sudo curl -sSL https://raw.githubusercontent.com/ros/rosdistro/master/ros.key -o /usr/share/keyrings/ros-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/ros-archive-keyring.gpg] http://packages.ros.org/ros2/ubuntu $(. /etc/os-release && echo $UBUNTU_CODENAME) main" | sudo tee /etc/apt/sources.list.d/ros2.list > /dev/null
sudo apt update
sudo apt install -y ros-humble-desktop

# 환경 설정
echo 'source /opt/ros/humble/setup.bash' >> ~/.bashrc
source ~/.bashrc
```

#### Step 5: Fleet Agent C++ 의존성 설치

```bash
# ROS2 패키지
sudo apt install -y ros-humble-nav2-msgs

# C++ 라이브러리
sudo apt install -y \
  protobuf-compiler libprotobuf-dev \
  libgrpc++-dev libgrpc-dev protobuf-compiler-grpc \
  libtbb-dev \
  libssl-dev \
  libyaml-cpp-dev \
  libspdlog-dev \
  nlohmann-json3-dev

# MsQuic (QUIC transport)
wget https://github.com/microsoft/msquic/releases/download/v2.2.3/libmsquic_2.2.3_amd64.deb
sudo dpkg -i libmsquic_2.2.3_amd64.deb
rm libmsquic_2.2.3_amd64.deb
```

---

## 실행 방법

### Option A: 개별 실행 (개발용)

#### 1. Neo4j 시작

```bash
# Docker로 실행한 경우
docker start fleet-neo4j
```

#### 2. Go Backend 실행

```bash
cd central_server_go

# 환경 변수 설정
export NEO4J_URI=neo4j://localhost:7687
export NEO4J_USER=neo4j
export NEO4J_PASSWORD=neo4j123
export NEO4J_DATABASE=neo4j
export HTTP_PORT=8081
export GRPC_PORT=9090

# 빌드 및 실행
go build -o fleet-server ./cmd/server/main.go
./fleet-server
```

#### 3. Frontend 실행 (새 터미널)

```bash
cd central_server/frontend

# 의존성 설치 (최초 1회)
npm install

# 개발 서버 실행
npm run dev
```

#### 4. Fleet Agent 실행 (새 터미널, ROS2 환경)

```bash
cd fleet_agent_cpp

# ROS2 환경 설정
source /opt/ros/humble/setup.bash

# 빌드
colcon build --symlink-install

# 환경 설정
source install/setup.bash

# Agent 실행
ros2 run fleet_agent fleet_agent_node --ros-args -p agent_id:=agent_01
```

### Option B: 스크립트 실행

```bash
# 개발 서버 실행 (Backend + Frontend)
./scripts/dev.sh

# 또는 전체 Docker 스택
./scripts/run_server.sh
```

---

## 서비스 포트

| 서비스 | 포트 | 설명 |
|--------|------|------|
| Frontend | 5173 (dev) / 3000 (prod) | React Web UI |
| Backend API | 8081 | REST API + WebSocket |
| gRPC (TCP) | 9090 | Agent 통신 |
| gRPC (QUIC/UDP) | 9443 | QUIC Agent 통신 |
| Raw QUIC (C++ Agents) | 9444 | QUIC Agent 통신 |
| Neo4j (Browser) | 7474 | Admin UI |
| Neo4j (Bolt) | 7687 | Database |

---

## Health Check

```bash
# Backend 상태 확인
curl http://localhost:8081/health

# API 테스트
curl http://localhost:8081/api/robots
curl http://localhost:8081/api/agents
curl http://localhost:8081/api/fleet/state

```

---

## 프로젝트 구조

```
multi-robot-supervision/
├── central_server_go/          # Go Backend (통합 서버)
│   ├── cmd/server/             # 진입점
│   ├── internal/
│   │   ├── api/                # REST API handlers
│   │   ├── db/                 # Neo4j repository
│   │   ├── grpc/               # gRPC over QUIC handlers
│   │   ├── state/              # Global state manager
│   │   └── graph/              # Action Graph logic
│   └── Dockerfile
│
├── central_server/
│   └── frontend/               # React Frontend
│       ├── src/
│       │   ├── pages/          # Page components
│       │   ├── components/     # Reusable components
│       │   ├── api/            # API client
│       │   └── types/          # TypeScript types
│       └── package.json
│
├── fleet_agent_cpp/            # C++ Fleet Agent
│   ├── include/fleet_agent/
│   │   ├── core/               # Types, config, logger
│   │   ├── state/              # State tracking
│   │   ├── graph/              # Graph executor
│   │   ├── executor/           # Action execution
│   │   ├── transport/          # QUIC (MsQuic)
│   │   └── protocol/           # Message handlers
│   ├── src/
│   ├── proto/                  # Protobuf definitions
│   └── CMakeLists.txt
│
├── scripts/                    # 실행 스크립트
│   ├── dev.sh                  # 개발 서버 실행
│   ├── run_server.sh           # Docker 스택 실행
│   └── test_*.sh               # 테스트 스크립트
│
├── docker-compose.yaml
├── CLAUDE.md                   # 개발 가이드
└── README.md
```

---

## 기술 스택

### Central Server (Go)
- Go 1.21+
- Chi Router (REST API)
- Neo4j Go Driver (Bolt/Cypher)
- gorilla/websocket
- gRPC over QUIC (quic-go)

### Frontend
- React 18 + TypeScript
- Vite
- TailwindCSS
- React Query
- React Flow (Graph Editor)

### Fleet Agent (C++)
- C++17
- ROS2 Humble
- MsQuic (QUIC transport)
- Protobuf
- Intel TBB (concurrent containers)

---

## API 문서

Backend 실행 후:
- REST API: `http://localhost:8081/api/`
- WebSocket: `ws://localhost:8081/ws/monitor`

### 주요 API 엔드포인트

```
GET    /api/robots                    # 로봇 목록
GET    /api/robots/{id}               # 로봇 상세
POST   /api/robots                    # 로봇 등록
GET    /api/robots/{id}/capabilities  # 로봇 capabilities

GET    /api/agents                    # Agent 목록
GET    /api/agents/{id}               # Agent 상세

GET    /api/action-graphs             # Action Graph 목록
POST   /api/action-graphs             # Action Graph 생성
POST   /api/action-graphs/{id}/execute # Graph 실행

GET    /api/state-definitions         # State 정의 목록
POST   /api/state-definitions         # State 정의 생성

GET    /api/fleet/state               # Fleet 전체 상태
WS     /ws/monitor                    # 실시간 모니터링
```

---

## 문제 해결

### Neo4j 연결 실패

```bash
# Docker 상태 확인
docker ps --filter "name=fleet-neo4j"

# 로그 확인
docker logs -f fleet-neo4j

# 연결 확인
docker exec fleet-neo4j cypher-shell -u neo4j -p neo4j "RETURN 1"
```

### Go 빌드 오류

```bash
# 의존성 정리
cd central_server_go
go mod tidy
go mod download
```

### 데이터 초기화 (Neo4j)

```bash
# 모든 데이터 삭제 (주의: 복구 불가)
./scripts/reset_neo4j.sh
```

### Frontend 빌드 오류

```bash
cd central_server/frontend
rm -rf node_modules package-lock.json
npm install
```

---

## 라이선스

MIT License
