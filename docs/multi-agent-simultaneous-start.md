# Multi-Agent Simultaneous Start Feature Design

## Overview

This document describes the design for a feature that allows multiple agents to start executing the same action graph simultaneously. This is useful for coordinated multi-robot operations like synchronized pick-and-place, formation movements, or parallel processing tasks.

## Requirements

1. **Atomic Validation**: All agents must pass precondition checks before any starts
2. **Synchronized Start**: All agents begin execution at approximately the same time
3. **Failure Handling**: If any agent fails validation, none start (atomic semantics)
4. **Zone Coordination**: Zone reservations must be handled atomically across all agents
5. **Progress Tracking**: Tasks should be linked for coordinated monitoring

## API Design

### New Endpoint

```
POST /api/action-graphs/{graphID}/execute-multi
```

### Request Body

```json
{
  "agent_ids": ["agent_001", "agent_002", "agent_003"],
  "params": {
    "common_param": "value"
  },
  "agent_params": {
    "agent_001": {"target": "zone_a"},
    "agent_002": {"target": "zone_b"}
  },
  "sync_mode": "barrier",  // "barrier" | "best_effort"
  "timeout_sec": 30
}
```

### Response Body

```json
{
  "execution_group_id": "exec_group_uuid",
  "tasks": [
    {"agent_id": "agent_001", "task_id": "task_001", "status": "running"},
    {"agent_id": "agent_002", "task_id": "task_002", "status": "running"}
  ],
  "started_at": "2024-01-01T00:00:00Z",
  "sync_mode": "barrier",
  "message": "Multi-agent execution started"
}
```

### Error Response (Validation Failure)

```json
{
  "error": "validation_failed",
  "message": "Cannot start multi-agent execution",
  "failed_agents": [
    {
      "agent_id": "agent_002",
      "reason": "Agent is already executing task xyz"
    }
  ],
  "passed_agents": ["agent_001", "agent_003"]
}
```

## Implementation Architecture

### 1. Scheduler Changes

Add new method to `internal/executor/scheduler.go`:

```go
// ExecutionGroup represents a coordinated multi-agent execution
type ExecutionGroup struct {
    ID           string
    GraphID      string
    Tasks        map[string]*RunningTask  // agentID -> task
    StartedAt    time.Time
    SyncMode     string
    StartBarrier sync.WaitGroup
}

// StartMultiAgentTask starts synchronized execution for multiple agents
func (s *Scheduler) StartMultiAgentTask(
    ctx context.Context,
    actionGraphID string,
    agentIDs []string,
    commonParams map[string]interface{},
    agentParams map[string]map[string]interface{},
    syncMode string,
) (*ExecutionGroup, error)
```

### 2. State Manager Changes

Add atomic multi-agent operations to `internal/state/manager.go`:

```go
// TryStartMultiExecution attempts to start execution for multiple agents atomically
// Returns (success, failedAgentID, errorMessage)
func (m *GlobalStateManager) TryStartMultiExecution(
    agentIDs []string,
    taskIDs []string,
    stepIDs []string,
    requiredZones map[string][]string,  // agentID -> zones
    preconditions []Precondition,
) (bool, string, string)
```

### 3. Synchronization Barrier

The barrier ensures all agents start at the same time:

```go
func (s *Scheduler) runMultiTask(
    ctx context.Context,
    group *ExecutionGroup,
    task *RunningTask,
    startBarrier *sync.WaitGroup,
) {
    // Wait for all agents to be ready
    startBarrier.Done()  // Signal this agent is ready
    startBarrier.Wait()  // Wait for all agents

    // All agents start execution here simultaneously
    s.runTask(ctx, task)
}
```

## Sequence Diagram

```
Client              API Server           Scheduler           StateManager
   |                    |                    |                    |
   |  POST /execute-multi                    |                    |
   |------------------->|                    |                    |
   |                    |  StartMultiAgentTask                   |
   |                    |------------------->|                    |
   |                    |                    |  TryStartMultiExecution
   |                    |                    |------------------->|
   |                    |                    |    [Atomic Lock]   |
   |                    |                    |    Check all agents|
   |                    |                    |    Reserve zones   |
   |                    |                    |    Update states   |
   |                    |                    |<-------------------|
   |                    |                    |                    |
   |                    |      [For each agent: spawn goroutine]  |
   |                    |                    |                    |
   |                    |<-------------------|                    |
   |<-------------------|                    |                    |
   |  ExecutionGroup response               |                    |
   |                    |                    |                    |
   |                    |  [Goroutines wait at barrier]          |
   |                    |                    |                    |
   |                    |  [All ready -> barrier releases]       |
   |                    |                    |                    |
   |                    |  [All agents start executing]          |
```

## State Manager Implementation

```go
// TryStartMultiExecution atomically validates and starts multi-agent execution
func (m *GlobalStateManager) TryStartMultiExecution(
    executions []MultiExecutionRequest,
    preconditions []Precondition,
) (bool, string, string) {
    m.mu.Lock()
    defer m.mu.Unlock()

    // Phase 1: Validate all agents
    for _, exec := range executions {
        robot, exists := m.robots[exec.AgentID]
        if !exists {
            return false, exec.AgentID, fmt.Sprintf("agent %s not found", exec.AgentID)
        }
        if robot.IsExecuting {
            return false, exec.AgentID, fmt.Sprintf("agent %s is already executing", exec.AgentID)
        }
    }

    // Phase 2: Check all preconditions
    for _, exec := range executions {
        robot := m.robots[exec.AgentID]
        for _, cond := range preconditions {
            if !m.evaluatePreconditionLocked(robot, cond) {
                return false, exec.AgentID, cond.Message
            }
        }
    }

    // Phase 3: Check zone conflicts
    allZones := make(map[string]string)  // zoneID -> agentID (who wants it)
    for _, exec := range executions {
        for _, zone := range exec.RequiredZones {
            // Check if another agent in this batch wants the same zone
            if existing, conflict := allZones[zone]; conflict {
                return false, exec.AgentID,
                    fmt.Sprintf("zone %s conflicts between %s and %s", zone, existing, exec.AgentID)
            }
            // Check if zone is already reserved by external agent
            if holder, exists := m.zones[zone]; exists &&
               time.Now().Before(holder.ExpiresAt) &&
               !containsAgent(executions, holder.AgentID) {
                return false, exec.AgentID,
                    fmt.Sprintf("zone %s is reserved by agent %s", zone, holder.AgentID)
            }
            allZones[zone] = exec.AgentID
        }
    }

    // Phase 4: All validations passed - commit changes atomically
    now := time.Now()
    for _, exec := range executions {
        robot := m.robots[exec.AgentID]
        robot.IsExecuting = true
        robot.CurrentTaskID = exec.TaskID
        robot.CurrentStepID = exec.StepID
        robot.LastSeen = now

        // Reserve zones
        for _, zone := range exec.RequiredZones {
            m.zones[zone] = &ZoneReservation{
                ZoneID:     zone,
                AgentID:    exec.AgentID,
                ReservedAt: now,
                ExpiresAt:  now.Add(m.zoneExpiryDuration),
            }
        }
    }

    return true, "", ""
}
```

## Frontend Changes

### UI Components

1. **Multi-Select Agent List**: Allow selecting multiple agents for execution
2. **Execute All Button**: Triggers multi-agent execution
3. **Execution Group View**: Shows coordinated task progress

### API Client Update

```typescript
// central_server/frontend/src/api/client.ts

export const actionGraphApi = {
  // ... existing methods ...

  executeMulti: async (
    graphId: string,
    agentIds: string[],
    params?: {
      commonParams?: Record<string, unknown>
      agentParams?: Record<string, Record<string, unknown>>
      syncMode?: 'barrier' | 'best_effort'
      timeoutSec?: number
    }
  ): Promise<ExecutionGroupResponse> => {
    const { data } = await api.post(`/action-graphs/${graphId}/execute-multi`, {
      agent_ids: agentIds,
      params: params?.commonParams,
      agent_params: params?.agentParams,
      sync_mode: params?.syncMode || 'barrier',
      timeout_sec: params?.timeoutSec || 30,
    })
    return data
  },
}
```

## Database Changes

### New Table: execution_groups

```sql
CREATE TABLE execution_groups (
    id TEXT PRIMARY KEY,
    action_graph_id TEXT NOT NULL,
    sync_mode TEXT NOT NULL DEFAULT 'barrier',
    status TEXT NOT NULL DEFAULT 'pending',
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (action_graph_id) REFERENCES action_graphs(id)
);
```

### Update: tasks table

```sql
ALTER TABLE tasks ADD COLUMN execution_group_id TEXT REFERENCES execution_groups(id);
```

## Edge Cases & Error Handling

### 1. Partial Agent Failure During Execution

If one agent fails during execution, others continue. The ExecutionGroup tracks individual task statuses.

### 2. Network Partition

If an agent loses connectivity:
- Barrier mode: Timeout after configured duration, cancel all
- Best-effort mode: Start available agents, mark unavailable as failed

### 3. Precondition Race Condition

The atomic lock in StateManager prevents race conditions between validation and execution start.

### 4. Zone Conflict Resolution

Zones are reserved atomically. If the same zone is needed by multiple agents in the batch:
- Error if both need exclusive access
- Allow if zone supports shared access (future enhancement)

## Implementation Phases

### Phase 1: Core Implementation
- Add `TryStartMultiExecution` to StateManager
- Add `StartMultiAgentTask` to Scheduler
- Add `/execute-multi` API endpoint

### Phase 2: Frontend Integration
- Multi-select agent UI
- Execute All button
- Execution group status display

### Phase 3: Advanced Features
- Shared zone support
- Staggered start (with configurable delay)
- Execution group cancellation
- Progress synchronization points

## Testing Strategy

1. **Unit Tests**: Test atomic validation logic in StateManager
2. **Integration Tests**: Test full execution flow with mock agents
3. **Load Tests**: Test barrier synchronization under load
4. **Failure Tests**: Test partial failure scenarios
