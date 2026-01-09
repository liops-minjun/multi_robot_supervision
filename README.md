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
│  │   :8081      │  │   Monitor    │  │ :9443/9444   │                       │
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

## 빠른 시작 (Docker)

### 1. 필수 요구사항

- Docker 20.10+
- Docker Compose 1.29+

```bash
# Docker 설치 (Ubuntu)
sudo apt-get update
sudo apt-get install -y docker.io docker-compose

# 현재 사용자를 docker 그룹에 추가 (sudo 없이 사용하려면)
sudo usermod -aG docker $USER
newgrp docker
```

### 2. 서버 실행

```bash
# 전체 스택 빌드 및 실행
docker-compose up -d --build

# 로그 확인
docker-compose logs -f

# 상태 확인
docker-compose ps
```

### 3. 접속

| 서비스 | URL | 설명 |
|--------|-----|------|
| Frontend | http://localhost:3000 | React Web UI |
| Backend API | http://localhost:8081 | REST API |
| Neo4j Browser | http://localhost:7474 | DB 관리 (neo4j/neo4j123) |

### 4. Health Check

```bash
# Backend 상태 확인
curl http://localhost:8081/health

# API 테스트
curl http://localhost:8081/api/robots
curl http://localhost:8081/api/fleet/state
```

### 5. 종료

```bash
# 서비스 중지
docker-compose down

# 데이터 포함 완전 삭제
docker-compose down -v
```

---

## 서비스 포트

| 서비스 | 포트 | 프로토콜 | 설명 |
|--------|------|----------|------|
| Frontend | 3000 | HTTP | React Web UI |
| Backend API | 8081 | HTTP | REST API + WebSocket |
| gRPC (TCP) | 9090 | TCP | Agent 통신 |
| gRPC (QUIC) | 9443 | UDP | QUIC Agent 통신 |
| Raw QUIC | 9444 | UDP | C++ Agent 통신 |
| Neo4j Browser | 7474 | HTTP | Admin UI |
| Neo4j Bolt | 7687 | TCP | Database |

---

## Fleet Agent 설치 (로봇 측)

Fleet Agent는 로봇에서 실행되며 ROS2 환경이 필요합니다.

### 의존성 설치

```bash
# ROS2 Humble (Ubuntu 22.04)
sudo apt update && sudo apt install -y software-properties-common
sudo add-apt-repository universe
sudo curl -sSL https://raw.githubusercontent.com/ros/rosdistro/master/ros.key \
  -o /usr/share/keyrings/ros-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/ros-archive-keyring.gpg] \
  http://packages.ros.org/ros2/ubuntu $(. /etc/os-release && echo $UBUNTU_CODENAME) main" \
  | sudo tee /etc/apt/sources.list.d/ros2.list > /dev/null
sudo apt update
sudo apt install -y ros-humble-desktop ros-humble-nav2-msgs

# C++ 라이브러리
sudo apt install -y \
  build-essential cmake \
  protobuf-compiler libprotobuf-dev \
  libgrpc++-dev protobuf-compiler-grpc \
  libtbb-dev libssl-dev \
  libyaml-cpp-dev libspdlog-dev \
  nlohmann-json3-dev

# MsQuic (QUIC transport)
wget -q https://packages.microsoft.com/config/ubuntu/22.04/packages-microsoft-prod.deb
sudo dpkg -i packages-microsoft-prod.deb
rm packages-microsoft-prod.deb
sudo apt-get update
sudo apt-get install -y libmsquic
```

### 빌드

```bash
cd fleet_agent_cpp

# ROS2 환경 설정
source /opt/ros/humble/setup.bash

# 빌드
colcon build --symlink-install

# 환경 설정
source install/setup.bash
```

### 실행 (Launch 파일 사용)

```bash
# 기본 실행 (localhost 연결)
ros2 launch fleet_agent_cpp fleet_agent.launch.py

# 외부 서버 연결 (가장 일반적인 사용법)
ros2 launch fleet_agent_cpp fleet_agent.launch.py server_ip:=192.168.0.100

# 전체 옵션
ros2 launch fleet_agent_cpp fleet_agent.launch.py \
  server_ip:=192.168.0.100 \
  server_port:=9444 \
  agent_id:=agent_02
```

### Launch 파라미터

| 파라미터 | 기본값 | 설명 |
|----------|--------|------|
| `server_ip` | localhost | Central Server IP 주소 |
| `server_port` | 9444 | QUIC 포트 |
| `agent_id` | agent_01 | Agent 고유 ID |
| `config` | (패키지 내 config) | 설정 파일 경로 |
| `log_level` | info | 로그 레벨 |
| `domain_id` | 0 | ROS_DOMAIN_ID |

### 설정 파일

설정 파일은 빌드 후 자동으로 패키지에 포함됩니다.
커스텀 설정이 필요한 경우:

```bash
# 설정 파일 복사 후 수정
cp install/fleet_agent_cpp/share/fleet_agent_cpp/config/agent.yaml ~/my_agent.yaml

# 커스텀 설정으로 실행
ros2 launch fleet_agent_cpp fleet_agent.launch.py \
  config:=~/my_agent.yaml \
  server_ip:=192.168.0.100
```

### 인증서 설정

인증서는 빌드 시 패키지에 포함됩니다. 프로덕션 환경에서는:

```bash
# Central Server에서 인증서 복사
scp user@server:/path/to/certs/* fleet_agent_cpp/certs/

# 다시 빌드
colcon build --packages-select fleet_agent_cpp
```

### 방화벽 설정 (Central Server)

```bash
sudo ufw allow 9444/udp  # Raw QUIC
sudo ufw allow 9443/udp  # gRPC over QUIC
```

### 네트워크 구성 예시

```
┌─────────────────────────────────────────────────────────────┐
│                    Central Server                            │
│                  192.168.0.100                               │
│         ┌─────────────────────────────┐                     │
│         │  Docker Compose Stack       │                     │
│         │  - Neo4j     :7474, 7687    │                     │
│         │  - Backend   :8081, 9444    │                     │
│         │  - Frontend  :3000          │                     │
│         └─────────────────────────────┘                     │
└─────────────────────────┬───────────────────────────────────┘
                          │ QUIC (UDP 9444)
         ┌────────────────┼────────────────┐
         ▼                ▼                ▼
  ┌────────────┐   ┌────────────┐   ┌────────────┐
  │ Robot 1    │   │ Robot 2    │   │ Robot 3    │
  │ 192.168.0.10│  │ 192.168.0.11│  │ 192.168.0.12│
  │            │   │            │   │            │
  │ Fleet Agent│   │ Fleet Agent│   │ Fleet Agent│
  │ + ROS2     │   │ + ROS2     │   │ + ROS2     │
  └────────────┘   └────────────┘   └────────────┘
```

---

## 로컬 개발 (Docker 없이)

Docker 없이 개발하려면 다음을 참고하세요.

### 필수 요구사항

| 구성 요소 | 버전 | 용도 |
|-----------|------|------|
| Go | 1.21+ | Backend |
| Node.js | 18+ | Frontend |
| Neo4j | 5.x | Database |

### 개발 서버 실행

```bash
# 스크립트로 실행 (Backend + Frontend)
./scripts/dev.sh
```

또는 개별 실행:

```bash
# 1. Neo4j (Docker)
docker run -d --name fleet-neo4j \
  -e NEO4J_AUTH=neo4j/neo4j123 \
  -p 7474:7474 -p 7687:7687 \
  neo4j:5

# 2. Backend
cd central_server_go
export NEO4J_URI=neo4j://localhost:7687
export NEO4J_USER=neo4j
export NEO4J_PASSWORD=neo4j123
go run cmd/server/main.go

# 3. Frontend (새 터미널)
cd central_server/frontend
npm install
npm run dev
```

---

## 프로젝트 구조

```
multi-robot-supervision/
├── central_server_go/          # Go Backend
│   ├── cmd/server/             # 진입점
│   ├── internal/
│   │   ├── api/                # REST API
│   │   ├── db/                 # Neo4j
│   │   ├── grpc/               # gRPC/QUIC
│   │   └── state/              # State Manager
│   └── Dockerfile
│
├── central_server/frontend/    # React Frontend
│   ├── src/
│   │   ├── pages/              # 페이지 컴포넌트
│   │   ├── components/         # 공통 컴포넌트
│   │   └── api/                # API 클라이언트
│   └── Dockerfile
│
├── fleet_agent_cpp/            # C++ Fleet Agent
│   ├── include/fleet_agent/    # 헤더 파일
│   ├── src/                    # 구현 파일
│   └── config/                 # 설정 파일
│
├── docker-compose.yaml         # Docker 스택 정의
├── scripts/                    # 유틸리티 스크립트
└── docs/                       # 문서
```

---

## 기술 스택

| 컴포넌트 | 기술 |
|----------|------|
| Backend | Go 1.21+, Chi Router, Neo4j Driver, quic-go |
| Frontend | React 18, TypeScript, Vite, TailwindCSS, React Flow |
| Database | Neo4j 5.x (Graph DB) |
| Agent | C++17, ROS2 Humble, MsQuic, Protobuf, Intel TBB |
| Transport | QUIC (0-RTT, Connection Migration) |

---

## API 엔드포인트

```
GET    /api/robots                    # 로봇 목록
POST   /api/robots                    # 로봇 등록
GET    /api/robots/{id}/capabilities  # 로봇 capabilities

GET    /api/agents                    # Agent 목록
GET    /api/action-graphs             # Action Graph 목록
POST   /api/action-graphs             # Action Graph 생성
POST   /api/action-graphs/{id}/execute # Graph 실행

GET    /api/fleet/state               # Fleet 전체 상태
WS     /ws/monitor                    # 실시간 모니터링
```

---

## 문제 해결

### Docker 권한 오류

```bash
# Permission denied 오류 시
sudo usermod -aG docker $USER
newgrp docker
```

### Neo4j 연결 실패

```bash
# 컨테이너 상태 확인
docker-compose ps
docker-compose logs neo4j
```

### 데이터 초기화

```bash
# 모든 데이터 삭제 (주의!)
docker-compose down -v
docker-compose up -d --build
```

---

## 라이선스

MIT License
