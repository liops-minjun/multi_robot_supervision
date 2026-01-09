# Multi-Robot Supervision System - Development Guide

## Project Overview

This is a multi-robot fleet management system with:
- **Central Server**: Go backend (unified) + React frontend
- **Fleet Agent**: C++17 ROS2 agent managing multiple robots (QUIC transport)
- **Communication**: QUIC for all agent-server communication (commands, telemetry, heartbeat)

## Architecture

```
Central Server Go (Single Backend)        Fleet Agent C++ (ROS2)
├── internal/api/*.go (REST+WS)           ├── include/fleet_agent/
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

| Metric | Go Server | Fleet Agent C++ |
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
# fleet_agent_cpp/config/agent.yaml
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
    ca_cert: "/etc/fleet_agent/certs/ca.crt"
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
| `/api/action-graphs` | CRUD | Action Graph management |
| `/api/action-graphs/{id}/execute` | POST | Execute on robot |
| `/api/action-graphs/{id}/canonical` | GET | Get graph in canonical format |
| `/api/action-graphs/{id}/deploy/{agentID}` | POST | Deploy to agent |
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
  "action_graph": {
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

### Action Graph Caching

Both Central Server and Fleet Agent maintain in-memory caches for deployed Action Graphs to minimize DB/file I/O during task execution.

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
│                     Fleet Agent (C++)                            │
├─────────────────────────────────────────────────────────────────┤
│  GraphStorage (graph/storage.hpp)                                │
│  ├── storage_path_   /var/lib/fleet_agent/graphs/               │
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
│   │   ├── action_graphs.go    # Action Graph CRUD + execution + caching
│   │   ├── tasks.go            # Task management
│   │   ├── system.go           # Cache stats & management endpoints
│   │   └── responses.go        # Response models
│   ├── db/
│   │   ├── models.go           # GORM models
│   │   └── repository.go       # Database operations
│   ├── state/
│   │   ├── manager.go          # In-memory fleet state (thread-safe)
│   │   └── graph_cache.go      # Action Graph in-memory cache
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

### Fleet Agent (C++)
```
fleet_agent_cpp/
├── CMakeLists.txt              # Build configuration (MsQuic required)
├── include/fleet_agent/
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
- `action_graphs` - Action Graph definitions (templates)
- `agent_action_graphs` - Action Graph assignments to agents
- `action_graph_deployment_logs` - Deployment audit trail
- `tasks` - Running/completed tasks
- `waypoints` - Saved positions/poses
- `state_definitions` - Robot type configurations (Legacy, optional)

## Update Checklist

### When Adding New Action Graph Step Fields

1. [ ] Update `internal/db/models.go` - ActionGraph.Steps JSON structure
2. [ ] Update `internal/graph/schema.go` - Canonical graph types
3. [ ] Update `fleet_agent_cpp/include/fleet_agent/graph/storage.hpp`
4. [ ] Update `fleet_agent_cpp/src/graph/executor.cpp`
5. [ ] Update frontend Action Graph Editor (if UI needed)

### When Modifying gRPC Messages

1. [ ] Update `proto/fleet/v1/*.proto` - Protobuf definitions
2. [ ] Run `protoc` to regenerate Go and C++ code
3. [ ] Update `internal/grpc/server.go` - Server handlers
4. [ ] Update `fleet_agent_cpp/src/transport/quic_transport.cpp` - Client handlers

### When Adding New Agent Config Options

1. [ ] Update `fleet_agent_cpp/include/fleet_agent/core/config.hpp`
2. [ ] Update `fleet_agent_cpp/src/core/config_loader.cpp`
3. [ ] Update `fleet_agent_cpp/config/agent.example.yaml`
4. [ ] Update `fleet_agent_cpp/src/agent.cpp` - Usage

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
cd fleet_agent_cpp
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
    ca_cert: "/etc/fleet_agent/certs/ca.crt"
    client_cert: "/etc/fleet_agent/certs/agent.crt"
    client_key: "/etc/fleet_agent/certs/agent.key"
    idle_timeout_ms: 30000
    keepalive_interval_ms: 10000
    enable_0rtt: true
    enable_datagrams: true

paths:
  action_graphs: "/opt/fleet_agent/action_graphs"
  resumption_ticket: "/var/lib/fleet_agent/quic_ticket"

telemetry:
  interval_ms: 100
  delta_encoding: true

discovery:
  scan_interval_sec: 30
  action_timeout_sec: 5.0
```

## Common Pitfalls

### 1. Action Graph Version Mismatch
- Server increments `ActionGraph.version` on every save
- `AgentActionGraph.server_version` must be updated
- Agent stores `deployed_version` for comparison

### 2. QUIC Connection Issues
- Ensure TLS certificates are valid and trusted
- Check firewall allows UDP on port 9443
- Verify MsQuic is properly installed (`libmsquic.so`)

### 3. Step Transition Logic
- `on_success` can be string (step_id) or dict (conditional)
- Terminal steps complete the action graph
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
