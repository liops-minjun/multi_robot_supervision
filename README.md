# Multi-Robot Supervision System

다양한 로봇(AMR, 로봇팔, 지게차 등)을 중앙에서 관리하는 Fleet Management System입니다.

## 주요 기능

| 기능 | 설명 |
|------|------|
| **실시간 모니터링** | WebSocket을 통한 로봇 상태, 위치, 텔레메트리 실시간 확인 |
| **Behavior Tree 편집기** | 드래그 앤 드롭으로 작업 시나리오 구성 |
| **Capability Auto-Discovery** | ROS2 Action Server 자동 탐지 - 설정 불필요 |
| **텔레메트리 캡처** | 로봇팔 티칭: 현재 자세를 클릭 한 번으로 저장 |
| **Multi-Robot 협업** | 로봇 간 상태 기반 조건부 실행 |

## 아키텍처

```
┌─────────────────────────────────────────────────────────────────┐
│                      Central Server (서버)                        │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐              │
│  │   React     │  │  Go Backend │  │   Neo4j     │              │
│  │  Frontend   │◄─┤  REST/WS    │◄─┤  Graph DB   │              │
│  │   :3000     │  │   :8081     │  │  :7474/7687 │              │
│  └─────────────┘  └──────┬──────┘  └─────────────┘              │
└──────────────────────────┼──────────────────────────────────────┘
                           │ QUIC (UDP :9443)
          ┌────────────────┼────────────────┐
          ▼                ▼                ▼
   ┌────────────┐   ┌────────────┐   ┌────────────┐
   │Fleet Agent │   │Fleet Agent │   │Fleet Agent │  ← 로봇마다 1개
   │   (C++)    │   │   (C++)    │   │   (C++)    │
   │  ↕ ROS2    │   │  ↕ ROS2    │   │  ↕ ROS2    │
   └────────────┘   └────────────┘   └────────────┘
```

---

# 🖥️ Central Server (서버 측)

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

끝입니다! 브라우저에서 `http://localhost:3000`으로 접속하세요.

## 서비스 포트

| 서비스 | 포트 | 설명 |
|--------|------|------|
| **Frontend** | 3000 | Web UI |
| **Backend API** | 8081 | REST API + WebSocket |
| **QUIC** | 9443/UDP | Agent 통신 |
| **Neo4j Browser** | 7474 | DB 관리 (neo4j/neo4j123) |

## 주요 명령어

```bash
# 로그 확인
docker-compose logs -f

# 중지
docker-compose down

# 데이터 초기화 (주의!)
docker-compose down -v && docker-compose up -d
```

## 개발 모드 (Docker 없이)

```bash
# 필요: Go 1.21+, Node.js 18+, Neo4j

# Neo4j만 Docker로
docker run -d --name neo4j -e NEO4J_AUTH=neo4j/neo4j123 -p 7474:7474 -p 7687:7687 neo4j:5

# Backend + Frontend 실행
./scripts/dev.sh
```

---

# 🤖 Fleet Agent (로봇 측)

Fleet Agent는 각 로봇(또는 로봇 그룹)에서 실행됩니다. ROS2 환경이 필요합니다.

## 요구 사항

- Ubuntu 22.04
- ROS2 Humble
- 서버와 네트워크 연결 (UDP 9443)

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
cd fleet_agent_cpp
source /opt/ros/humble/setup.bash
colcon build --symlink-install
source install/setup.bash
```

### 3. 실행

```bash
# 서버 IP 지정하여 실행
ros2 launch fleet_agent_cpp fleet_agent.launch.py server_ip:=<서버_IP>

# 예시: 서버가 192.168.0.100인 경우
ros2 launch fleet_agent_cpp fleet_agent.launch.py server_ip:=192.168.0.100
```

## Launch 파라미터

| 파라미터 | 기본값 | 설명 |
|----------|--------|------|
| `server_ip` | localhost | Central Server IP |
| `server_port` | 9443 | QUIC 포트 |
| `agent_id` | agent_01 | Agent 고유 ID |
| `log_level` | info | 로그 레벨 (debug/info/warn/error) |

## 설정 파일 (선택)

기본 설정으로 충분하지만, 커스텀 설정이 필요한 경우:

```bash
# 설정 파일 복사
cp install/fleet_agent_cpp/share/fleet_agent_cpp/config/agent.yaml ~/my_config.yaml

# 수정 후 실행
ros2 launch fleet_agent_cpp fleet_agent.launch.py \
  config:=~/my_config.yaml \
  server_ip:=192.168.0.100
```

## 연결 확인

Agent 실행 후 서버의 Web UI에서 확인:

1. `http://서버IP:3000` 접속
2. 좌측 사이드바에서 연결된 Agent 확인
3. Agent의 ROS2 Action Server들이 자동으로 등록됨

---

# 📖 사용 가이드

## 1. Behavior Tree 만들기

1. Web UI에서 **Behavior Tree** 메뉴 클릭
2. **+ 새 Behavior Tree** 버튼 클릭
3. 우측 패널에서 **DISCOVERED ACTIONS**의 액션을 캔버스로 드래그
4. 노드 간 연결 (성공/실패 시 다음 단계)
5. **저장** 버튼 클릭

## 2. 로봇팔 티칭 (텔레메트리 캡처)

로봇팔을 수동으로 원하는 자세로 이동 후:

1. Action 노드의 **Goal 파라미터** 섹션 펼치기
2. 로봇 선택 드롭다운에서 로봇 선택
3. **LIVE** 표시 확인 (실시간 텔레메트리 수신 중)
4. **현재 로봇 자세로 초기화** 버튼 클릭
5. 현재 joint_state 값이 자동으로 입력됨

## 3. Behavior Tree 실행

1. Behavior Tree 목록에서 실행할 Tree 선택
2. **배포** 버튼으로 Agent에 배포
3. **실행** 버튼 클릭
4. 실시간으로 진행 상황 모니터링

---

# 🔧 API Reference

## REST API (Port 8081)

```bash
# 로봇 목록
curl http://localhost:8081/api/robots

# Fleet 상태
curl http://localhost:8081/api/fleet/state

# Behavior Tree 목록
curl http://localhost:8081/api/behavior-trees

# Behavior Tree 실행
curl -X POST http://localhost:8081/api/behavior-trees/{id}/execute \
  -H "Content-Type: application/json" \
  -d '{"robot_id": "robot_001"}'
```

## WebSocket (실시간 모니터링)

```javascript
const ws = new WebSocket('ws://localhost:8081/ws/monitor')
ws.onmessage = (event) => {
  const data = JSON.parse(event.data)
  console.log('Fleet state:', data)
}
```

---

# 📁 프로젝트 구조

```
multi-robot-supervision/
├── central_server_go/           # Go Backend
│   ├── cmd/server/              # 진입점
│   ├── internal/api/            # REST API 핸들러
│   ├── internal/graph/          # Behavior Tree 처리
│   └── internal/grpc/           # QUIC 서버
│
├── central_server/frontend/     # React Frontend
│   ├── src/pages/               # 페이지 (Dashboard, BehaviorTree 등)
│   ├── src/components/          # 공통 컴포넌트
│   └── src/contexts/            # React Context (Telemetry 등)
│
├── fleet_agent_cpp/             # C++ Fleet Agent
│   ├── include/fleet_agent/     # 헤더
│   ├── src/                     # 구현
│   ├── config/                  # 설정 파일
│   └── launch/                  # ROS2 Launch 파일
│
└── docker-compose.yaml          # Docker 스택
```

---

# 🐛 문제 해결

## Agent가 서버에 연결 안 됨

```bash
# 1. 서버 방화벽 확인
sudo ufw allow 9443/udp

# 2. Agent 로그 확인
ros2 launch fleet_agent_cpp fleet_agent.launch.py server_ip:=... log_level:=debug

# 3. 네트워크 연결 확인
ping <서버_IP>
```

## Web UI에서 로봇이 안 보임

```bash
# Backend 로그 확인
docker-compose logs -f backend

# Agent가 연결되었는지 확인
curl http://localhost:8081/api/agents
```

## Docker 권한 오류

```bash
sudo usermod -aG docker $USER
newgrp docker
```

---

# 🔗 기술 스택

| 컴포넌트 | 기술 |
|----------|------|
| **Backend** | Go 1.21, Chi Router, Neo4j, quic-go |
| **Frontend** | React 18, TypeScript, Vite, TailwindCSS, React Flow |
| **Database** | Neo4j 5.x (Graph DB) |
| **Agent** | C++17, ROS2 Humble, MsQuic, Protobuf |
| **통신** | QUIC (0-RTT, Connection Migration 지원) |

---

# 📜 라이선스

MIT License
