// Copyright 2026 Multi-Robot Supervision System
// Core type definitions for the Fleet Agent

#pragma once

#include <atomic>
#include <chrono>
#include <functional>
#include <optional>
#include <string>
#include <vector>

#include <tbb/concurrent_hash_map.h>
#include <tbb/concurrent_queue.h>
#include <tbb/concurrent_vector.h>

// Forward declarations for protobuf types
namespace fleet {
namespace v1 {
class AgentMessage;
class ServerMessage;
class ActionGraph;
enum RobotState : int;
enum ActionStatus : int;
enum GraphExecutionState : int;
}  // namespace v1
}  // namespace fleet

namespace fleet_agent {

// ============================================================
// Success Criteria for action completion
// ============================================================

struct SuccessCriteria {
    std::string field;      // e.g., "success", "error_code"
    std::string op;         // "==", "!=", "<", ">", "<=", ">=", "in"
    std::string value;      // Expected value (JSON-encoded for complex types)

    bool is_empty() const { return field.empty(); }
};

// ============================================================
// Action Capability - Discovered action server info
// ============================================================

struct ActionCapability {
    std::string action_type;     // "nav2_msgs/action/NavigateToPose"
    std::string action_server;   // "/robot_001/navigate_to_pose"
    std::string package;         // "nav2_msgs"
    std::string action_name;     // "NavigateToPose"

    // JSON schemas
    std::string goal_schema_json;
    std::string result_schema_json;
    std::string feedback_schema_json;

    // Success criteria
    SuccessCriteria success_criteria;

    // Runtime state
    std::atomic<bool> available{true};
    std::atomic<bool> executing{false};
    std::chrono::steady_clock::time_point last_seen;

    ActionCapability() = default;
    ActionCapability(const ActionCapability& other)
        : action_type(other.action_type)
        , action_server(other.action_server)
        , package(other.package)
        , action_name(other.action_name)
        , goal_schema_json(other.goal_schema_json)
        , result_schema_json(other.result_schema_json)
        , feedback_schema_json(other.feedback_schema_json)
        , success_criteria(other.success_criteria)
        , available(other.available.load())
        , executing(other.executing.load())
        , last_seen(other.last_seen) {}

    ActionCapability& operator=(const ActionCapability& other) {
        if (this != &other) {
            action_type = other.action_type;
            action_server = other.action_server;
            package = other.package;
            action_name = other.action_name;
            goal_schema_json = other.goal_schema_json;
            result_schema_json = other.result_schema_json;
            feedback_schema_json = other.feedback_schema_json;
            success_criteria = other.success_criteria;
            available.store(other.available.load());
            executing.store(other.executing.load());
            last_seen = other.last_seen;
        }
        return *this;
    }
};

// ============================================================
// Robot Execution State
// ============================================================

enum class RobotExecutionState {
    IDLE,
    WAITING_PRECONDITION,
    EXECUTING_ACTION,
    WAITING_RESULT,
    ERROR
};

// ============================================================
// Robot Execution Context
// ============================================================

struct RobotExecutionContext {
    std::atomic<RobotExecutionState> state{RobotExecutionState::IDLE};
    std::string current_command_id;
    std::string current_task_id;
    std::string current_step_id;
    std::string current_action_type;

    // Graph execution context
    std::string current_graph_id;
    std::string current_graph_execution_id;
    int current_graph_step_index{-1};

    // Timing
    std::chrono::steady_clock::time_point action_started_at;
    std::chrono::steady_clock::time_point action_deadline;

    RobotExecutionContext() = default;
    RobotExecutionContext(const RobotExecutionContext& other)
        : state(other.state.load())
        , current_command_id(other.current_command_id)
        , current_task_id(other.current_task_id)
        , current_step_id(other.current_step_id)
        , current_action_type(other.current_action_type)
        , current_graph_id(other.current_graph_id)
        , current_graph_execution_id(other.current_graph_execution_id)
        , current_graph_step_index(other.current_graph_step_index)
        , action_started_at(other.action_started_at)
        , action_deadline(other.action_deadline) {}

    RobotExecutionContext& operator=(const RobotExecutionContext& other) {
        if (this != &other) {
            state.store(other.state.load());
            current_command_id = other.current_command_id;
            current_task_id = other.current_task_id;
            current_step_id = other.current_step_id;
            current_action_type = other.current_action_type;
            current_graph_id = other.current_graph_id;
            current_graph_execution_id = other.current_graph_execution_id;
            current_graph_step_index = other.current_graph_step_index;
            action_started_at = other.action_started_at;
            action_deadline = other.action_deadline;
        }
        return *this;
    }
};

// ============================================================
// Inbound Command (from gRPC)
// ============================================================

struct InboundCommand {
    std::string message_id;
    std::chrono::steady_clock::time_point received_at;
    std::shared_ptr<fleet::v1::ServerMessage> message;
};

// ============================================================
// Outbound Message (to gRPC)
// ============================================================

struct OutboundMessage {
    std::shared_ptr<fleet::v1::AgentMessage> message;
    std::chrono::steady_clock::time_point created_at;
    int priority{0};  // Higher = more urgent
};

// ============================================================
// Action Request (internal representation)
// ============================================================

struct ActionRequest {
    std::string command_id;
    std::string agent_id;
    std::string task_id;
    std::string step_id;
    std::string action_type;
    std::string action_server;
    std::string params_json;
    float timeout_sec{120.0f};
    int64_t deadline_ms{0};
};

// ============================================================
// Action Result (internal representation)
// ============================================================

struct ActionResultInternal {
    std::string command_id;
    std::string agent_id;
    std::string task_id;
    std::string step_id;
    int status{0};  // ActionStatus enum
    std::string result_json;
    std::string error;
    int64_t started_at_ms{0};
    int64_t completed_at_ms{0};
};

// ============================================================
// TBB Container Type Aliases
// ============================================================

// Capability store: action_server -> capability info (key is action_server, not action_type)
using CapabilityStore = tbb::concurrent_hash_map<std::string, ActionCapability>;

// Execution context: agent_id -> execution context (1:1 model)
using ExecutionContextMap = tbb::concurrent_hash_map<std::string, RobotExecutionContext>;

// Message queues
using InboundQueue = tbb::concurrent_queue<InboundCommand>;
using QuicOutboundQueue = tbb::concurrent_queue<OutboundMessage>;

// Command queues (for ExecuteCommand from protobuf)
// Note: These use ActionRequest since fleet::v1::ExecuteCommand requires protobuf include
using CommandQueue = tbb::concurrent_queue<ActionRequest>;
using ResultQueue = tbb::concurrent_queue<ActionResultInternal>;

// Agent ID list (1:1 model: agent_id = robot_id)
using AgentIdVector = tbb::concurrent_vector<std::string>;

// ============================================================
// Callback Types
// ============================================================

using ActionResultCallback = std::function<void(const ActionResultInternal&)>;
using ActionFeedbackCallback = std::function<void(const std::string& agent_id, float progress)>;
using ConnectionCallback = std::function<void(bool connected)>;

// ============================================================
// Utility Functions
// ============================================================

// Get current time in milliseconds since epoch
inline int64_t now_ms() {
    return std::chrono::duration_cast<std::chrono::milliseconds>(
        std::chrono::system_clock::now().time_since_epoch()
    ).count();
}

// Get monotonic time point
inline std::chrono::steady_clock::time_point now_steady() {
    return std::chrono::steady_clock::now();
}

}  // namespace fleet_agent
