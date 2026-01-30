# Behavior Tree - Neo4j Graph-First Architecture

## Overview

This document defines the graph-first architecture for Behavior Trees using Neo4j
as the single source of truth for graphs, executions, deployments, and state.

Key decisions:
- Neo4j Community, single instance.
- All graph, execution, deployment, state, and log data stored in Neo4j.
- Start conditions are stored as graph relationships (GATED_BY) with order/operator.
- Execution mode is computed per graph: agent mode if all conditions are self-only,
  server mode if any condition is non-self.
- Logs retained for 30 days; older nodes are deleted by a scheduled cleanup job.
- Existing data is reset and re-authored (no migration).

## Architecture Diagram

```
┌──────────────────────────────────────────────────────────────────────────┐
│                           Shared Graph Schema                            │
│  Canonical Graph (JSON + Protobuf) + Neo4j Graph Model                    │
└──────────────────────────────────────────────────────────────────────────┘
                                  │
               ┌──────────────────┼──────────────────┐
               │                  │                  │
               ▼                  ▼                  ▼
┌──────────────────────┐   QUIC + HTTP   ┌──────────────────────┐
│   Central Server     │◄───────────────►│    Fleet Agent       │
│   (Go)               │                 │    (C++)             │
│                      │                 │                      │
│  ┌────────────────┐  │                 │  ┌────────────────┐  │
│  │ Neo4j (SoT)    │  │                 │  │ GraphExecutor  │  │
│  │ Graph + State  │  │                 │  │ Agent Mode     │  │
│  │ Exec + Logs    │  │                 │  └────────────────┘  │
│  └────────────────┘  │                 │  ┌────────────────┐  │
│  ┌────────────────┐  │                 │  │ StateTracker   │  │
│  │ Orchestrator   │  │                 │  │ Telemetry      │  │
│  │ Server Mode    │  │                 │  └────────────────┘  │
│  └────────────────┘  │                 │                      │
└──────────────────────┘                 └──────────────────────┘
```

## Neo4j Graph Model

### Core Nodes

```
(:BehaviorTree {id, name, version, checksum, execution_mode, created_at})
(:Step {id, step_type, action_json, wait_json, condition_json})
(:Terminal {id, terminal_type, message, alert})
(:Condition {id, quantifier, target_type, robot_id, agent_id,
             state_operator, state, allowed_states, max_staleness_sec,
             require_online, message})
(:StateDefinition {id, version})
(:State {name})
(:Agent {id, name, status, connected_at, last_heartbeat})
(:Robot {id, agent_id, state, state_name, is_executing, last_seen})
(:Deployment {id, graph_id, version, agent_id, status, deployed_at, error})
(:Execution {id, graph_id, version, robot_id, state, current_step_id,
             started_at, updated_at})
(:StepExecution {id, step_id, status, started_at, completed_at})
(:ActionCommand {id, action_type, action_server, params_json, timeout_sec,
                 deadline_ms, created_at})
(:ActionResult {status, error, result_json, started_at, completed_at})
(:TelemetrySample {ts, state, state_name, battery, pose_json,
                   velocity_json, extra_json})
```

### Relationships

```
(BehaviorTree)-[:ENTRY_POINT]->(Step)
(BehaviorTree)-[:CONTAINS]->(Step|Terminal)
(Step)-[:ON_SUCCESS|ON_FAILURE|ON_TIMEOUT|ON_CONFIRM|ON_CANCEL]->(Step|Terminal)
(Step)-[:GATED_BY {order, operator}]->(Condition)
(Step)-[:SETS_DURING|SETS_SUCCESS|SETS_FAILURE]->(State)

(StateDefinition)-[:HAS_STATE]->(State)
(StateDefinition)-[:MAPS_ACTION {action_type}]->(State)

(Agent)-[:MANAGES]->(Robot)
(BehaviorTree)-[:DEPLOYED_TO]->(Agent)
(Deployment)-[:OF_GRAPH]->(BehaviorTree)
(Deployment)-[:TO_AGENT]->(Agent)

(Execution)-[:OF_GRAPH]->(BehaviorTree)
(Execution)-[:RUNS_ON]->(Robot)
(Execution)-[:HAS_STEP_EXECUTION]->(StepExecution)
(StepExecution)-[:OF_STEP]->(Step)

(Execution)-[:ISSUED]->(ActionCommand)
(ActionCommand)-[:FOR_STEP]->(Step)
(ActionCommand)-[:ON_ROBOT]->(Robot)
(ActionCommand)-[:RESULT]->(ActionResult)

(Robot)-[:HAS_TELEMETRY]->(TelemetrySample)
```

## Start Conditions (AND/OR List)

Start conditions are stored as Condition nodes connected by GATED_BY edges with
`order` and `operator` fields. Evaluation is left-to-right.

```
(step)-[:GATED_BY {order: 1, operator: "and"}]->(cond1)
(step)-[:GATED_BY {order: 2, operator: "or"}]->(cond2)
```

Self-only conditions enable agent mode. Any non-self condition forces server mode.

## Execution Modes

### Agent Mode
- GraphExecutor runs in the agent.
- Start conditions evaluated locally; unsatisfied -> wait and re-evaluate.
- State transitions are applied via StateTracker and reported via telemetry.

### Server Mode
- Central server orchestrates step transitions and condition evaluation.
- Unsatisfied conditions -> wait/re-evaluate loop.
- ExecuteCommand includes step state transitions (during/success/failure).
- Agent performs state transitions only; server decides next step.

## State Transitions

State transitions are explicitly modeled:

```
(Step)-[:SETS_DURING]->(:State {name:"navigating"})
(Step)-[:SETS_SUCCESS]->(:State {name:"idle"})
(Step)-[:SETS_FAILURE]->(:State {name:"error"})
```

The agent always performs state transitions and publishes telemetry.

## Neo4j Constraints and Indexes (DDL)

```cypher
CREATE CONSTRAINT behavior_tree_id IF NOT EXISTS
FOR (g:BehaviorTree) REQUIRE g.id IS UNIQUE;

CREATE CONSTRAINT step_id IF NOT EXISTS
FOR (s:Step) REQUIRE s.id IS UNIQUE;

CREATE CONSTRAINT execution_id IF NOT EXISTS
FOR (e:Execution) REQUIRE e.id IS UNIQUE;

CREATE CONSTRAINT robot_id IF NOT EXISTS
FOR (r:Robot) REQUIRE r.id IS UNIQUE;

CREATE CONSTRAINT agent_id IF NOT EXISTS
FOR (a:Agent) REQUIRE a.id IS UNIQUE;

CREATE INDEX robot_state IF NOT EXISTS FOR (r:Robot) ON (r.state);
CREATE INDEX robot_last_seen IF NOT EXISTS FOR (r:Robot) ON (r.last_seen);
CREATE INDEX graph_id IF NOT EXISTS FOR (g:BehaviorTree) ON (g.id);
CREATE INDEX execution_state IF NOT EXISTS FOR (e:Execution) ON (e.state);
```

## Query Examples

Find next step on success:

```cypher
MATCH (g:BehaviorTree {id:$graph_id, version:$version})-[:ENTRY_POINT]->(start:Step)
MATCH (start)-[:ON_SUCCESS]->(next)
RETURN next
```

Load start conditions for a step (ordered):

```cypher
MATCH (s:Step {id:$step_id})-[r:GATED_BY]->(c:Condition)
RETURN c, r.order, r.operator
ORDER BY r.order ASC
```

## Log Retention (30 Days)

Logs and telemetry older than 30 days are removed by a scheduled cleanup job.
All timestamps use epoch milliseconds.

```cypher
WITH timestamp() - 30 * 24 * 60 * 60 * 1000 AS cutoff
MATCH (t:TelemetrySample)
WHERE t.ts < cutoff
DETACH DELETE t;

WITH timestamp() - 30 * 24 * 60 * 60 * 1000 AS cutoff
MATCH (e:Execution)
WHERE e.updated_at < cutoff
DETACH DELETE e;

WITH timestamp() - 30 * 24 * 60 * 60 * 1000 AS cutoff
MATCH (cmd:ActionCommand)
WHERE cmd.created_at < cutoff
DETACH DELETE cmd;
```

## Data Reset Policy

All existing graph and execution data is reset and re-authored. No migration
is performed. New graphs are created from scratch under this model.
