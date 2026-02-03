# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Development Commands

### Docker (Full Stack)
```bash
docker-compose up -d --build      # Build and start all services
docker-compose down               # Stop services
docker-compose down -v            # Stop and remove volumes (reset data)
docker-compose logs -f            # Follow logs
```

### Go Backend (central_server_go/)
```bash
cd central_server_go
go run cmd/server/main.go         # Run backend (requires Neo4j)
go build -o fleet-server ./cmd/server  # Build binary
go test ./...                     # Run all tests
go test ./internal/api/...        # Test specific package
```

### React Frontend (central_server/frontend/)
```bash
cd central_server/frontend
npm install                       # Install dependencies
npm run dev                       # Development server (port 5173)
npm run build                     # Production build
tsc                               # Type check
```

### Robot Agent C++ (ros2_robot_agent/)
```bash
# Setup ROS2 workspace (one-time)
./setup_test.sh
cd ros2_ws
source /opt/ros/humble/setup.bash

# Build
colcon build --symlink-install
source install/setup.bash

# Run
ros2 launch ros2_robot_agent robot_agent.launch.py server_ip:=192.168.0.100
```

### Development Scripts
```bash
./scripts/dev.sh                  # Start backend + frontend (no Docker)
./scripts/run_test.sh             # Integration test with ROS2 action servers
./scripts/test_capability_api.sh  # Test capability APIs
./scripts/test_graph_api.sh       # Test behavior tree APIs
```

### Regenerate Protobuf (when modifying proto files)
```bash
# Go (in central_server_go/)
protoc --proto_path=./pkg/proto \
  --go_out=./pkg/proto --go_opt=paths=source_relative \
  --go-grpc_out=./pkg/proto --go-grpc_opt=paths=source_relative \
  ./pkg/proto/fleet.proto

# C++ (handled by CMake during colcon build)
```

---

# Multi-Robot Supervision System - Development Guide

## Project Overview

This is a multi-robot fleet management system with:
- **Central Server**: Go backend (unified) + React frontend
- **Fleet Agent**: C++17 ROS2 agent managing multiple robots (QUIC transport)
- **Communication**: QUIC for all agent-server communication (commands, telemetry, heartbeat)

## Architecture

```
Central Server Go (Single Backend)        Robot Agent C++ (ROS2)
├── internal/api/*.go (REST+WS)           ├── include/robot_agent/
├── internal/db/models.go                 │   ├── core/ (types, config, logger)
├── internal/state/manager.go             │   ├── transport/ (quic)
├── internal/executor/scheduler.go        │   ├── capability/ (scanner, store)
├── internal/graph/*.go                   │   ├── telemetry/ (collector, aggregator)
├── internal/grpc/server.go (QUIC)        │   ├── executor/ (command_processor, action)
└── proto/fleet/v1/*.proto                │   └── graph/ (storage, executor)
              │                           └── src/ (implementations)
              └── QUIC (MsQuic) ──────────────────────┘
```

## Performance Characteristics

| Metric | Go Server | Robot Agent C++ |
|--------|-----------|-----------------|
| Memory Usage | ~20-50MB (idle) | ~15-30MB (idle) |
| Request Throughput | ~50,000 req/s | - |
| WebSocket Clients | 100,000+ concurrent | - |
| Telemetry Rate | - | 10Hz per robot |
| Action Latency | - | <5ms dispatch |
| Cold Start | 0.1-0.3 seconds | 0.2-0.5 seconds |
| Docker Image | ~30MB | ~50MB |

## Ports & Services

| Port | Service | Protocol | Description |
|------|---------|----------|-------------|
| 3000 | Frontend | HTTP | nginx + React SPA |
| 8081 | Go Backend | HTTP | REST API + WebSocket |
| 9443 | Go Backend | QUIC | QUIC transport (Agent comm) |
| 7474 | Neo4j | HTTP | Graph DB (Neo4j Browser / REST) |
| 7687 | Neo4j | TCP | Bolt protocol |

## Transport Architecture

### gRPC over QUIC (Primary)

The Fleet Agent uses **MsQuic** for all server communication:

```
┌─────────────────┐        QUIC/TLS 1.3        ┌─────────────────┐
│  Fleet Agent    │◄─────────────────────────►│  Central Server │
│                 │                            │                 │
│  ┌───────────┐  │  Bidirectional Streams    │  ┌───────────┐  │
│  │ QUIC      │  │  - Commands (Server→Agent)│  │ QUIC      │  │
│  │ Client    │  │  - Results (Agent→Server) │  │ Server    │  │
│  │ (MsQuic)  │  │  - Heartbeat (Reliable)   │  │ (quic-go) │  │
│  └───────────┘  │                            │  └───────────┘  │
│                 │  QUIC Datagrams (0-RTT)    │                 │
│                 │  - Telemetry (Unreliable)  │                 │
└─────────────────┘                            └─────────────────┘
```

**Key Features:**
- **0-RTT Connection Resumption**: Fast reconnection after network changes
- **Connection Migration**: Seamless handoff when robot moves between APs
- **Stream Multiplexing**: No head-of-line blocking
- **QUIC Datagrams**: Low-latency unreliable telemetry

## Zero-Config Architecture

The system supports **automatic capability discovery** without manual configuration.

### Key Concepts

1. **Auto-Discovery**: Fleet Agent scans ROS2 Action Servers at startup
2. **Schema Introspection**: Extracts Goal/Result/Feedback schemas via ROS2 introspection
3. **Success Criteria Inference**: Auto-infers success/failure criteria from result schema
4. **Capability Registration**: Reports discovered capabilities via gRPC

### Configuration (Minimal)

```yaml
# ros2_robot_agent/config/agent.yaml
agent:
  id: "agent_01"
  name: "Factory Agent"

robots:
  - id: "robot_001"
    namespace: "/robot_001"
    name: "AMR Robot 1"
    tags: ["mobile", "nav"]

server:
  quic:
    server_address: "192.168.0.200"
    server_port: 9443
    ca_cert: "/etc/robot_agent/certs/ca.crt"
    enable_0rtt: true
    enable_datagrams: true
```

### API Endpoints (Port 8081)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/robots` | POST | Register robot with capabilities |
| `/api/robots/{id}` | PATCH | Update robot metadata |
| `/api/robots/{id}/capabilities` | GET | Get robot capabilities |
| `/api/robots/{id}/capabilities` | PUT | Register/sync capabilities |
| `/api/capabilities` | GET | List all capabilities (fleet-wide) |
| `/api/behavior-trees` | CRUD | Behavior Tree management |
| `/api/behavior-trees/{id}/execute` | POST | Execute on robot |
| `/api/behavior-trees/{id}/canonical` | GET | Get graph in canonical format |
| `/api/behavior-trees/{id}/deploy/{agentID}` | POST | Deploy to agent |
| `/api/tasks` | GET | List tasks |
| `/api/tasks/{id}/cancel` | POST | Cancel task |
| `/api/fleet/state` | GET | Get fleet state |
| `/ws/monitor` | WS | Real-time fleet monitoring |

## Graph-Optimized Architecture

### Key Features

1. **Neo4j**: Graph database for native graph storage using Cypher
2. **Canonical Graph Format**: Shared JSON schema for Server-Agent communication
3. **Graph Validation**: Cycle detection, reachability analysis, path finding
4. **Checksum Verification**: Integrity verification for deployments
5. **Hybrid Control**: Multi-robot coordination with preconditions

### Hybrid Control Model

The Fleet Agent supports **Hybrid control** for multi-robot coordination:

```
┌─────────────────────────────────────────────────────────────────┐
│                       Precondition Types                         │
├─────────────────────────────────────────────────────────────────┤
│ self.state == IDLE           │ Check own robot state            │
│ robot_002.state == WAITING   │ Check other robot state (cached) │
│ robot_002.is_executing       │ Check if robot is busy           │
│ step_result.success == true  │ Check previous step result       │
└─────────────────────────────────────────────────────────────────┘
```

**State Sources:**
- **Local Cache**: Updated via server fleet state broadcasts
- **Server Query**: On-demand query for real-time state (callback)

### Canonical Graph Format

```json
{
  "schema_version": "1.0.0",
  "behavior_tree": {
    "id": "pick_and_place_001",
    "name": "Pick and Place",
    "version": 3
  },
  "vertices": [
    {
      "id": "navigate_to_pick",
      "type": "step",
      "step": {
        "step_type": "action",
        "action": {
          "type": "nav2_msgs/action/NavigateToPose",
          "server": "/navigate_to_pose",
          "timeout_sec": 120.0
        },
        "precondition": "robot_002.state == 3"
      }
    }
  ],
  "edges": [
    {"from": "navigate_to_pick", "to": "success_end", "type": "on_success"}
  ],
  "entry_point": "navigate_to_pick",
  "checksum": "sha256:abc123..."
}
```

### Behavior Tree Caching

Both Central Server and Fleet Agent maintain in-memory caches for deployed Behavior Trees to minimize DB/file I/O during task execution.

```
┌─────────────────────────────────────────────────────────────────┐
│                     Central Server (Go)                          │
├─────────────────────────────────────────────────────────────────┤
│  GlobalStateManager.graphCache (state/graph_cache.go)            │
│  ├── templates     map[graphID]*CachedGraph                     │
│  ├── deployed      map[agentID:graphID]*CachedGraph             │
│  └── graphToAgents map[graphID][]agentID  (reverse index)       │
├─────────────────────────────────────────────────────────────────┤
│  Cache Flow:                                                     │
│  • Create/Update → DB write + cache set                         │
│  • Delete → cache invalidate + DB delete                        │
│  • Deploy → cache set on success                                │
│  • Task Start → cache lookup (miss → DB + cache)                │
│  • Background cleanup: every 10min, evict >1hr stale entries    │
└─────────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────────┐
│                     Robot Agent (C++)                            │
├─────────────────────────────────────────────────────────────────┤
│  GraphStorage (graph/storage.hpp)                                │
│  ├── storage_path_   /var/lib/robot_agent/graphs/               │
│  └── cache_          tbb::concurrent_hash_map<id, Graph>        │
├─────────────────────────────────────────────────────────────────┤
│  Cache Flow:                                                     │
│  • Receive from server → file write + cache set                 │
│  • Task execution → cache lookup (miss → file + cache)          │
│  • Startup → reload_cache() from files                          │
└─────────────────────────────────────────────────────────────────┘
```

**Cache Stats API:**
```bash
# Get cache statistics
curl http://localhost:8081/api/system/cache/stats

# Manually evict stale entries
curl -X POST http://localhost:8081/api/system/cache/evict \
  -d '{"max_age_minutes": 30}'
```

## Key Files

### Central Server (Go)
```
central_server_go/
├── cmd/server/main.go          # Entry point
├── internal/
│   ├── api/
│   │   ├── router.go           # HTTP routes
│   │   ├── websocket.go        # WebSocket handler (optimized broadcast)
│   │   ├── robots.go           # Robot CRUD
│   │   ├── action_graphs.go    # Behavior Tree CRUD + execution + caching
│   │   ├── tasks.go            # Task management
│   │   ├── system.go           # Cache stats & management endpoints
│   │   └── responses.go        # Response models
│   ├── db/
│   │   ├── models.go           # GORM models
│   │   └── repository.go       # Database operations
│   ├── state/
│   │   ├── manager.go          # In-memory fleet state (thread-safe)
│   │   └── graph_cache.go      # Behavior Tree in-memory cache
│   ├── executor/
│   │   └── scheduler.go        # Task scheduler (uses graph cache)
│   ├── graph/
│   │   ├── schema.go           # Canonical graph types
│   │   └── converter.go        # DB <-> Canonical conversion
│   └── grpc/
│       └── server.go           # gRPC over QUIC server
├── proto/fleet/v1/*.proto      # Protobuf definitions
└── Dockerfile                  # Multi-stage build (~30MB)
```

### Robot Agent (C++)
```
ros2_robot_agent/
├── CMakeLists.txt              # Build configuration (MsQuic required)
├── include/robot_agent/
│   ├── core/
│   │   ├── types.hpp           # TBB containers, data structures
│   │   ├── config.hpp          # Configuration types
│   │   ├── logger.hpp          # spdlog wrapper
│   │   └── shutdown.hpp        # Graceful shutdown
│   ├── transport/
│   │   └── quic_transport.hpp  # MsQuic QUIC client
│   ├── capability/
│   │   ├── scanner.hpp         # ROS2 action server discovery
│   │   └── store.hpp           # Capability storage
│   ├── telemetry/
│   │   ├── collector.hpp       # Per-robot telemetry collector
│   │   └── aggregator.hpp      # Telemetry aggregation & transmission
│   ├── executor/
│   │   ├── action_executor.hpp # ROS2 action client wrapper
│   │   ├── command_processor.hpp # Command handling pipeline
│   │   └── precondition.hpp    # Precondition evaluator
│   ├── graph/
│   │   ├── storage.hpp         # Local graph storage
│   │   └── executor.hpp        # Graph execution engine
│   └── agent.hpp               # Main agent class
├── src/                        # Implementations
├── config/
│   └── agent.example.yaml      # Example configuration
└── proto/fleet/v1/*.proto      # Protobuf definitions (shared)
```

## Threading Architecture (Fleet Agent)

```
┌─────────────────────────────────────────────────────────────────┐
│                    Fleet Agent Threads                           │
├─────────────────────────────────────────────────────────────────┤
│ Thread 1: ROS2 Executor      │ Spin node, handle callbacks      │
│ Thread 2: QUIC Client        │ MsQuic event loop                │
│ Thread 3: CommandProcessor   │ Process inbound commands         │
│ Thread 4: TelemetryAggregator│ Aggregate & transmit telemetry   │
│ Thread N: TelemetryCollector │ Per-robot ROS2 subscriptions     │
└─────────────────────────────────────────────────────────────────┘

Queue Architecture:
  QUIC Receiver → InboundQueue → CommandProcessor → ActionExecutor
                                        ↓
  TelemetryCollector → TelemetryStore → TelemetryAggregator
                                        ↓
                         QuicOutboundQueue → QUIC Sender
```

## Database Tables

### Core Tables
- `agents` - Fleet agents (1 agent = N robots)
- `robots` - Individual robots (with namespace, tags fields)
- `robot_capabilities` - Auto-discovered capabilities per robot
- `behavior_trees` - Behavior Tree definitions (templates)
- `agent_behavior_trees` - Behavior Tree assignments to agents
- `behavior_tree_deployment_logs` - Deployment audit trail
- `tasks` - Running/completed tasks
- `waypoints` - Saved positions/poses
- `state_definitions` - Robot type configurations (Legacy, optional)

## Update Checklist

### When Adding New Behavior Tree Step Fields

1. [ ] Update `internal/db/models.go` - BehaviorTree.Steps JSON structure
2. [ ] Update `internal/graph/schema.go` - Canonical graph types
3. [ ] Update `ros2_robot_agent/include/robot_agent/graph/storage.hpp`
4. [ ] Update `ros2_robot_agent/src/graph/executor.cpp`
5. [ ] Update frontend Behavior Tree Editor (if UI needed)

### When Modifying gRPC Messages

1. [ ] Update `proto/fleet/v1/*.proto` - Protobuf definitions
2. [ ] Run `protoc` to regenerate Go and C++ code
3. [ ] Update `internal/grpc/server.go` - Server handlers
4. [ ] Update `ros2_robot_agent/src/transport/quic_transport.cpp` - Client handlers

### When Adding New Agent Config Options

1. [ ] Update `ros2_robot_agent/include/robot_agent/core/config.hpp`
2. [ ] Update `ros2_robot_agent/src/core/config_loader.cpp`
3. [ ] Update `ros2_robot_agent/config/agent.example.yaml`
4. [ ] Update `ros2_robot_agent/src/agent.cpp` - Usage

### When Changing DB Schema

1. [ ] Update `internal/db/models.go` - GORM models
2. [ ] Run migrations or auto-migrate
3. [ ] Update API response models in `internal/api/responses.go`

## Quick Start

```bash
# 1. Start all services
docker-compose up -d

# 2. Check health
curl http://localhost:8081/health

# 3. Register robot with capabilities
curl -X POST http://localhost:8081/api/robots \
  -H "Content-Type: application/json" \
  -d '{
    "id": "robot_001",
    "name": "AMR Robot 1",
    "agent_id": "agent_01",
    "namespace": "/robot_001",
    "capabilities": [
      {
        "action_type": "nav2_msgs/action/NavigateToPose",
        "action_server": "/robot_001/navigate_to_pose"
      }
    ]
  }'

# 4. Get fleet state
curl http://localhost:8081/api/fleet/state

# 5. Access frontend
open http://localhost:3000

# 6. Build Fleet Agent (on robot)
cd ros2_robot_agent
mkdir build && cd build
cmake .. -DCMAKE_BUILD_TYPE=Release
make -j$(nproc)
```

## Environment Variables

### Go Backend
```
NEO4J_USER=neo4j
NEO4J_PASSWORD=neo4j123
NEO4J_DATABASE=neo4j
NEO4J_URI=neo4j://neo4j:7687
HTTP_PORT=8081
QUIC_PORT=9443
DEFINITIONS_PATH=/app/definitions
```

### Fleet Agent (config/agent.yaml)
```yaml
agent:
  id: "agent_01"
  name: "Factory Agent"

server:
  quic:
    server_address: "192.168.0.200"
    server_port: 9443
    ca_cert: "/etc/robot_agent/certs/ca.crt"
    client_cert: "/etc/robot_agent/certs/agent.crt"
    client_key: "/etc/robot_agent/certs/agent.key"
    idle_timeout_ms: 30000
    keepalive_interval_ms: 10000
    enable_0rtt: true
    enable_datagrams: true

paths:
  behavior_trees: "/opt/robot_agent/behavior_trees"
  resumption_ticket: "/var/lib/robot_agent/quic_ticket"

telemetry:
  interval_ms: 100
  delta_encoding: true

discovery:
  scan_interval_sec: 30
  action_timeout_sec: 5.0
```

## Common Pitfalls

### 1. Behavior Tree Version Mismatch
- Server increments `BehaviorTree.version` on every save
- `AgentBehaviorTree.server_version` must be updated
- Agent stores `deployed_version` for comparison

### 2. QUIC Connection Issues
- Ensure TLS certificates are valid and trusted
- Check firewall allows UDP on port 9443
- Verify MsQuic is properly installed (`libmsquic.so`)

### 3. Step Transition Logic
- `on_success` can be string (step_id) or dict (conditional)
- Terminal steps complete the behavior tree
- Preconditions must be satisfied before step execution

### 4. WebSocket Data Format
- Timestamp: ISO 8601 format (RFC3339)
- Robots: array of robot state objects
- Tasks: array of active tasks with progress

### 5. Hybrid Control Preconditions
- Use `self.` prefix for own robot state
- Use `robot_id.` prefix for other robot state
- State values are integers (enum values from protobuf)

## Prerequisites

### Fleet Agent Build Requirements
- **ROS2 Humble** (Ubuntu 22.04)
- **MsQuic** (required): `sudo apt install libmsquic`
- **TBB**: `sudo apt install libtbb-dev`
- **Protobuf**: `sudo apt install libprotobuf-dev protobuf-compiler`
- **spdlog**: `sudo apt install libspdlog-dev`
- **nlohmann-json**: `sudo apt install nlohmann-json3-dev`

---

## Development Principles (개발 원칙)

### 1. Interface-First Design (인터페이스 우선 설계)

모든 주요 컴포넌트는 인터페이스(IXxx)를 먼저 정의하고 구현해야 합니다.

```
┌─────────────────────────────────────────────────────────────────┐
│                         Agent (Orchestrator)                     │
├─────────────────────────────────────────────────────────────────┤
│  ┌──────────────────┐  ┌──────────────────┐  ┌───────────────┐  │
│  │   ITransport*    │  │ICapabilityScanner*│  │IActionExecutor*│ │
│  │   (interface)    │  │   (interface)     │  │  (interface)  │  │
│  └────────┬─────────┘  └────────┬─────────┘  └───────┬───────┘  │
│           │                     │                     │          │
│  ┌────────▼─────────┐  ┌───────▼──────────┐  ┌──────▼────────┐  │
│  │QUICTransport     │  │CapabilityScanner │  │ROS2Action     │  │
│  │Adapter           │  │Adapter           │  │Executor       │  │
│  └──────────────────┘  └──────────────────┘  └───────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

**핵심 인터페이스:**
- `interfaces/transport.hpp` - ITransport (네트워크 통신)
- `interfaces/capability_scanner.hpp` - ICapabilityScanner (ROS2 액션 탐색)
- `interfaces/action_executor.hpp` - IActionExecutor (액션 실행)

**원칙:**
- Agent 클래스는 인터페이스만 사용, 구체 클래스 직접 사용 금지
- Mock 객체를 통한 단위 테스트 가능하게 설계
- 새 컴포넌트 추가 시 인터페이스 먼저 정의

### 2. Dependency Injection (의존성 주입)

컴포넌트는 생성자를 통해 주입됩니다.

```cpp
// Production usage (via Factory)
auto agent = AgentFactory::create_agent("config.yaml");

// Testing usage (with mocks)
AgentComponents components;
components.transport = std::make_unique<MockTransport>();
components.scanner = std::make_unique<MockScanner>();
components.executor = std::make_unique<MockExecutor>();
auto agent = AgentFactory::create_agent_with_components(config, std::move(components));
```

**Factory 패턴:**
- `factory/agent_factory.hpp` - AgentFactory 클래스
- `create_agent()` - 프로덕션용 기본 컴포넌트로 Agent 생성
- `create_agent_with_components()` - 커스텀 컴포넌트로 Agent 생성 (테스트용)

### 3. Single Responsibility (단일 책임)

각 클래스는 하나의 책임만 가집니다.

| 클래스 | 책임 | 목표 라인 수 |
|--------|------|-------------|
| Agent | 오케스트레이션 (타이머, 메시지 라우팅) | ~500 lines |
| GraphExecutor | 그래프 탐색 로직 | ~300 lines |
| IActionExecutor | ROS2 액션 클라이언트 관리 | ~200 lines |
| ICapabilityScanner | ROS2 액션 서버 탐색 | ~200 lines |
| ITransport | 네트워크 통신 | ~150 lines |

**원칙:**
- 클래스가 500줄을 넘으면 책임 분리 고려
- 그래프 로직은 GraphExecutor에만 존재
- 액션 실행은 IActionExecutor에만 존재

### 4. Adapter Pattern for Legacy Code (어댑터 패턴)

기존 클래스의 인터페이스 변경 없이 어댑터로 감쌉니다.

```cpp
// 기존 구현체 (수정 없음)
class QUICClient { ... };

// 어댑터 (ITransport 인터페이스 구현)
class QUICTransportAdapter : public ITransport {
    std::shared_ptr<QUICClient> client_;
public:
    bool connect(const std::string& addr, uint16_t port) override {
        return client_->connect(addr, port);
    }
};
```

**어댑터 목록:**
- `QUICTransportAdapter` - QUICClient를 ITransport로 변환
- `CapabilityScannerAdapter` - CapabilityScanner를 ICapabilityScanner로 변환
- `ROS2ActionExecutor` - DynamicActionClient를 IActionExecutor로 변환

### 5. No Code Duplication (코드 중복 금지)

로직은 한 곳에만 존재해야 합니다.

**금지 사항:**
- 그래프 탐색 로직이 Agent와 GraphExecutor에 모두 존재
- 동일한 변환 함수가 여러 파일에 존재
- 유사한 에러 처리 코드의 복사/붙여넣기

**해결책:**
- 공통 로직은 별도 함수/클래스로 추출
- 유틸리티 함수는 core/ 디렉토리에 배치
- 변환 함수는 어댑터 클래스 내에 정의

### 6. Type Conversion Guidelines (타입 변환 가이드)

인터페이스와 구현체 간 타입 변환 시:

```cpp
// ActionCapability (concrete) → CapabilityInfo (interface)
interfaces::CapabilityInfo CapabilityScannerAdapter::convert(const ActionCapability& cap) {
    interfaces::CapabilityInfo info;
    info.action_type = cap.action_type;
    info.action_server = cap.action_server;
    info.is_available = cap.available.load();
    return info;
}

// LifecycleState enum 변환
interfaces::LifecycleState convert_lifecycle_state(robot_agent::LifecycleState state) {
    switch (state) {
        case robot_agent::LifecycleState::ACTIVE:
            return interfaces::LifecycleState::ACTIVE;
        // ...
    }
}
```

### 7. File Organization (파일 구성)

```
ros2_robot_agent/
├── include/robot_agent/
│   ├── interfaces/              # 인터페이스 정의 (순수 가상 클래스)
│   │   ├── transport.hpp        # ITransport
│   │   ├── capability_scanner.hpp # ICapabilityScanner
│   │   └── action_executor.hpp  # IActionExecutor
│   ├── factory/                 # 팩토리 클래스
│   │   └── agent_factory.hpp
│   ├── transport/               # Transport 구현
│   │   ├── quic_transport.hpp   # QUICClient (concrete)
│   │   └── quic_transport_adapter.hpp # ITransport adapter
│   ├── capability/              # Capability 구현
│   │   ├── scanner.hpp          # CapabilityScanner (concrete)
│   │   └── capability_scanner_adapter.hpp # ICapabilityScanner adapter
│   └── executor/                # Executor 구현
│       ├── dynamic_action_client.hpp # DynamicActionClient (concrete)
│       └── ros2_action_executor.hpp  # IActionExecutor implementation
└── src/                         # 구현 파일들 (동일 구조)
```

### 8. When Adding New Components (새 컴포넌트 추가 시)

1. **인터페이스 정의** (`interfaces/i_xxx.hpp`)
2. **구현체 생성** 또는 **어댑터 생성** (기존 클래스 감싸기)
3. **팩토리 메서드 추가** (`AgentFactory::create_default_xxx()`)
4. **CMakeLists.txt 업데이트** (새 소스 파일 추가)
5. **테스트 작성** (Mock 객체 사용)

### 9. Naming Conventions (네이밍 규칙)

| 타입 | 패턴 | 예시 |
|------|------|------|
| 인터페이스 | `I` + PascalCase | `ITransport`, `IActionExecutor` |
| 어댑터 | Concrete + `Adapter` | `QUICTransportAdapter` |
| 팩토리 메서드 | `create_` + snake_case | `create_default_transport()` |
| 콜백 타입 | PascalCase + `Callback` | `ResultCallback`, `FeedbackCallback` |
| 결과 구조체 | PascalCase + `Result/Info` | `ActionResult`, `CapabilityInfo` |
