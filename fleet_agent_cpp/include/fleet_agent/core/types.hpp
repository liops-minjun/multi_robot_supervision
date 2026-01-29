// Copyright 2026 Multi-Robot Supervision System
// Core type definitions for the Fleet Agent

#pragma once

#include <atomic>
#include <chrono>
#include <condition_variable>
#include <functional>
#include <mutex>
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
// Lifecycle State (ROS2 Lifecycle Node)
// ============================================================

/**
 * LifecycleState - ROS2 Lifecycle Node state.
 *
 * Maps to standard ROS2 lifecycle states:
 *   UNKNOWN: Non-lifecycle node or state unknown
 *   UNCONFIGURED: Node created but not configured
 *   INACTIVE: Configured but not processing
 *   ACTIVE: Fully operational
 *   FINALIZED: Shutting down
 */
enum class LifecycleState : uint8_t {
    UNKNOWN = 0,
    UNCONFIGURED = 1,
    INACTIVE = 2,
    ACTIVE = 3,
    FINALIZED = 4
};

/**
 * Convert LifecycleState to string for display/logging/proto.
 */
inline const char* lifecycle_state_to_string(LifecycleState state) {
    switch (state) {
        case LifecycleState::UNCONFIGURED: return "unconfigured";
        case LifecycleState::INACTIVE: return "inactive";
        case LifecycleState::ACTIVE: return "active";
        case LifecycleState::FINALIZED: return "finalized";
        default: return "unknown";
    }
}

/**
 * Parse LifecycleState from string.
 */
inline LifecycleState lifecycle_state_from_string(const std::string& str) {
    if (str == "unconfigured") return LifecycleState::UNCONFIGURED;
    if (str == "inactive") return LifecycleState::INACTIVE;
    if (str == "active") return LifecycleState::ACTIVE;
    if (str == "finalized") return LifecycleState::FINALIZED;
    return LifecycleState::UNKNOWN;
}

/**
 * Convert LifecycleState to proto enum value.
 */
inline int lifecycle_state_to_proto(LifecycleState state) {
    // Maps to fleet.v1.LifecycleState enum
    switch (state) {
        case LifecycleState::UNKNOWN: return 0;
        case LifecycleState::UNCONFIGURED: return 1;
        case LifecycleState::INACTIVE: return 2;
        case LifecycleState::ACTIVE: return 3;
        case LifecycleState::FINALIZED: return 4;
        default: return 0;
    }
}

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
    std::string node_name;       // Node hosting the action server (for lifecycle queries)

    // JSON schemas
    std::string goal_schema_json;
    std::string result_schema_json;
    std::string feedback_schema_json;

    // Success criteria
    SuccessCriteria success_criteria;

    // Runtime state
    std::atomic<bool> available{true};
    std::atomic<bool> executing{false};
    std::atomic<LifecycleState> lifecycle_state{LifecycleState::UNKNOWN};
    std::chrono::steady_clock::time_point last_seen;

    ActionCapability() = default;
    ActionCapability(const ActionCapability& other)
        : action_type(other.action_type)
        , action_server(other.action_server)
        , package(other.package)
        , action_name(other.action_name)
        , node_name(other.node_name)
        , goal_schema_json(other.goal_schema_json)
        , result_schema_json(other.result_schema_json)
        , feedback_schema_json(other.feedback_schema_json)
        , success_criteria(other.success_criteria)
        , available(other.available.load())
        , executing(other.executing.load())
        , lifecycle_state(other.lifecycle_state.load())
        , last_seen(other.last_seen) {}

    ActionCapability& operator=(const ActionCapability& other) {
        if (this != &other) {
            action_type = other.action_type;
            action_server = other.action_server;
            package = other.package;
            action_name = other.action_name;
            node_name = other.node_name;
            goal_schema_json = other.goal_schema_json;
            result_schema_json = other.result_schema_json;
            feedback_schema_json = other.feedback_schema_json;
            success_criteria = other.success_criteria;
            available.store(other.available.load());
            executing.store(other.executing.load());
            lifecycle_state.store(other.lifecycle_state.load());
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
// NotifiableQueue - Lock-free queue with condition variable notification
// ============================================================

/**
 * NotifiableQueue wraps TBB concurrent_queue with condition variable support.
 *
 * This allows consumers to efficiently wait for new items without polling,
 * reducing latency from ~10ms (polling) to <1ms (immediate notification).
 *
 * Thread-safe for multiple producers and single consumer.
 */
template <typename T>
class NotifiableQueue {
public:
    /**
     * Push an item to the queue and notify waiting consumers.
     */
    void push(T item) {
        queue_.push(std::move(item));
        cv_.notify_one();
    }

    /**
     * Try to pop an item without blocking.
     * @return true if item was popped
     */
    bool try_pop(T& item) {
        return queue_.try_pop(item);
    }

    /**
     * Wait for an item with timeout.
     *
     * Efficiently blocks until an item is available or timeout expires.
     * Uses condition variable for immediate wake-up on push.
     *
     * @param item Output item
     * @param timeout Maximum wait time
     * @param running_flag External running flag to check (false = stop)
     * @return true if item was popped, false on timeout or stop
     */
    template <typename Rep, typename Period>
    bool wait_pop(T& item,
                  std::chrono::duration<Rep, Period> timeout,
                  const std::atomic<bool>& running_flag) {
        // Fast path: try without waiting
        if (queue_.try_pop(item)) {
            return true;
        }

        // Slow path: wait for notification
        std::unique_lock<std::mutex> lock(mtx_);
        cv_.wait_for(lock, timeout, [this, &running_flag] {
            return !running_flag.load() || queue_.unsafe_size() > 0;
        });

        if (!running_flag.load()) {
            return false;
        }

        // Try again after notification
        return queue_.try_pop(item);
    }

    /**
     * Get approximate queue size (not thread-safe snapshot).
     */
    size_t unsafe_size() const {
        return queue_.unsafe_size();
    }

    /**
     * Check if queue is empty (approximate).
     */
    bool empty() const {
        return queue_.empty();
    }

    /**
     * Clear all items from queue.
     */
    void clear() {
        T item;
        while (queue_.try_pop(item)) {}
    }

    /**
     * Wake up any waiting consumers (for shutdown).
     */
    void notify_all() {
        cv_.notify_all();
    }

private:
    tbb::concurrent_queue<T> queue_;
    mutable std::mutex mtx_;
    std::condition_variable cv_;
};

// ============================================================
// TBB Container Type Aliases
// ============================================================

// Capability store: action_server -> capability info (key is action_server, not action_type)
using CapabilityStore = tbb::concurrent_hash_map<std::string, ActionCapability>;

// Execution context: agent_id -> execution context (1:1 model)
using ExecutionContextMap = tbb::concurrent_hash_map<std::string, RobotExecutionContext>;

// Message queues (using NotifiableQueue for low-latency notification)
using InboundQueue = NotifiableQueue<InboundCommand>;
using QuicOutboundQueue = NotifiableQueue<OutboundMessage>;

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
// Canonical Data Type System
// ============================================================

/**
 * CanonicalDataType defines the canonical data types for parameter binding.
 * Shared between Central Server and Fleet Agent for type compatibility checking.
 */
enum class CanonicalDataType {
    // Primitive types
    Bool,
    Int8, Int16, Int32, Int64,
    Uint8, Uint16, Uint32, Uint64,
    Float32, Float64,
    String,
    // Complex types
    Object,
    Array,
    // Any type (for dynamic/expression sources)
    Any
};

/**
 * TypeCategory groups canonical types for compatibility checking.
 */
enum class TypeCategory {
    Boolean,
    Integer,
    Float,
    String,
    Object,
    Array,
    Any
};

/**
 * Get category for a canonical data type.
 */
inline TypeCategory get_type_category(CanonicalDataType type) {
    switch (type) {
        case CanonicalDataType::Bool:
            return TypeCategory::Boolean;
        case CanonicalDataType::Int8:
        case CanonicalDataType::Int16:
        case CanonicalDataType::Int32:
        case CanonicalDataType::Int64:
        case CanonicalDataType::Uint8:
        case CanonicalDataType::Uint16:
        case CanonicalDataType::Uint32:
        case CanonicalDataType::Uint64:
            return TypeCategory::Integer;
        case CanonicalDataType::Float32:
        case CanonicalDataType::Float64:
            return TypeCategory::Float;
        case CanonicalDataType::String:
            return TypeCategory::String;
        case CanonicalDataType::Object:
            return TypeCategory::Object;
        case CanonicalDataType::Array:
            return TypeCategory::Array;
        default:
            return TypeCategory::Any;
    }
}

/**
 * Convert string to CanonicalDataType.
 */
inline CanonicalDataType string_to_data_type(const std::string& type_str) {
    if (type_str == "bool" || type_str == "boolean") return CanonicalDataType::Bool;
    if (type_str == "int8") return CanonicalDataType::Int8;
    if (type_str == "int16") return CanonicalDataType::Int16;
    if (type_str == "int32" || type_str == "int") return CanonicalDataType::Int32;
    if (type_str == "int64") return CanonicalDataType::Int64;
    if (type_str == "uint8" || type_str == "byte") return CanonicalDataType::Uint8;
    if (type_str == "uint16") return CanonicalDataType::Uint16;
    if (type_str == "uint32") return CanonicalDataType::Uint32;
    if (type_str == "uint64") return CanonicalDataType::Uint64;
    if (type_str == "float32" || type_str == "float") return CanonicalDataType::Float32;
    if (type_str == "float64" || type_str == "double") return CanonicalDataType::Float64;
    if (type_str == "string") return CanonicalDataType::String;
    if (type_str == "object") return CanonicalDataType::Object;
    if (type_str == "array") return CanonicalDataType::Array;
    return CanonicalDataType::Any;
}

/**
 * Convert CanonicalDataType to string.
 */
inline std::string data_type_to_string(CanonicalDataType type) {
    switch (type) {
        case CanonicalDataType::Bool: return "bool";
        case CanonicalDataType::Int8: return "int8";
        case CanonicalDataType::Int16: return "int16";
        case CanonicalDataType::Int32: return "int32";
        case CanonicalDataType::Int64: return "int64";
        case CanonicalDataType::Uint8: return "uint8";
        case CanonicalDataType::Uint16: return "uint16";
        case CanonicalDataType::Uint32: return "uint32";
        case CanonicalDataType::Uint64: return "uint64";
        case CanonicalDataType::Float32: return "float32";
        case CanonicalDataType::Float64: return "float64";
        case CanonicalDataType::String: return "string";
        case CanonicalDataType::Object: return "object";
        case CanonicalDataType::Array: return "array";
        case CanonicalDataType::Any: return "any";
    }
    return "any";
}

/**
 * DataTypeInfo contains full type information including array/object structure.
 */
struct DataTypeInfo {
    CanonicalDataType type{CanonicalDataType::Any};
    TypeCategory category{TypeCategory::Any};
    bool is_array{false};
    std::optional<std::shared_ptr<DataTypeInfo>> array_element_type;
    std::string ros_type;  // Original ROS2 type

    DataTypeInfo() = default;
    DataTypeInfo(CanonicalDataType t, bool arr = false, const std::string& ros = "")
        : type(t), category(get_type_category(t)), is_array(arr), ros_type(ros) {}
};

/**
 * TypeCompatibilityResult describes the result of a type compatibility check.
 */
struct TypeCompatibilityResult {
    bool compatible{false};
    bool requires_conversion{false};
    std::string conversion_type;  // "implicit", "explicit", "lossy"
    std::string warning_message;
};

/**
 * Check if source type is compatible with target type.
 */
inline TypeCompatibilityResult check_type_compatibility(
    const DataTypeInfo& source,
    const DataTypeInfo& target
) {
    TypeCompatibilityResult result;

    // Any type is always compatible
    if (source.type == CanonicalDataType::Any || target.type == CanonicalDataType::Any) {
        result.compatible = true;
        return result;
    }

    // Exact type match
    if (source.type == target.type && source.is_array == target.is_array) {
        result.compatible = true;
        return result;
    }

    // Array compatibility
    if (source.is_array != target.is_array) {
        // Array to non-array: need index access
        if (source.is_array && !target.is_array && source.array_element_type) {
            auto element_result = check_type_compatibility(*source.array_element_type.value(), target);
            if (element_result.compatible) {
                result.compatible = true;
                result.requires_conversion = true;
                result.conversion_type = "explicit";
                result.warning_message = "Array access required - use [index] syntax";
                return result;
            }
        }
        result.compatible = false;
        return result;
    }

    TypeCategory src_cat = source.category;
    TypeCategory tgt_cat = target.category;

    // Boolean only with boolean
    if (src_cat == TypeCategory::Boolean || tgt_cat == TypeCategory::Boolean) {
        result.compatible = (src_cat == tgt_cat);
        return result;
    }

    // Integer to float (implicit, safe)
    if (src_cat == TypeCategory::Integer && tgt_cat == TypeCategory::Float) {
        result.compatible = true;
        result.requires_conversion = true;
        result.conversion_type = "implicit";
        return result;
    }

    // Float to integer (lossy)
    if (src_cat == TypeCategory::Float && tgt_cat == TypeCategory::Integer) {
        result.compatible = true;
        result.requires_conversion = true;
        result.conversion_type = "lossy";
        result.warning_message = "Float to integer conversion may lose precision";
        return result;
    }

    // Integer type widening/narrowing
    if (src_cat == TypeCategory::Integer && tgt_cat == TypeCategory::Integer) {
        result.compatible = true;
        result.requires_conversion = true;
        result.conversion_type = "implicit";
        return result;
    }

    // Float type precision
    if (src_cat == TypeCategory::Float && tgt_cat == TypeCategory::Float) {
        result.compatible = true;
        result.requires_conversion = true;
        result.conversion_type = "implicit";
        return result;
    }

    // String only with string
    if (src_cat == TypeCategory::String || tgt_cat == TypeCategory::String) {
        result.compatible = (src_cat == tgt_cat);
        return result;
    }

    // Object compatibility
    if (src_cat == TypeCategory::Object && tgt_cat == TypeCategory::Object) {
        result.compatible = true;
        return result;
    }

    result.compatible = false;
    return result;
}

/**
 * Parse ROS2 type string to canonical type.
 */
inline DataTypeInfo ros_type_to_canonical(const std::string& ros_type) {
    // Handle array types
    bool is_array = false;
    std::string base_type = ros_type;
    if (ros_type.size() > 2 && ros_type.substr(ros_type.size() - 2) == "[]") {
        is_array = true;
        base_type = ros_type.substr(0, ros_type.size() - 2);
    }

    // Convert base type
    CanonicalDataType canonical = string_to_data_type(base_type);
    if (canonical == CanonicalDataType::Any && !base_type.empty()) {
        // Non-primitive type -> Object
        canonical = CanonicalDataType::Object;
    }

    DataTypeInfo info(canonical, is_array, ros_type);

    if (is_array) {
        auto element_type = std::make_shared<DataTypeInfo>(canonical, false, base_type);
        info.array_element_type = element_type;
    }

    return info;
}

/**
 * FieldPathSegment represents a segment of a field path with optional array index.
 */
struct FieldPathSegment {
    std::string field;
    std::optional<int> array_index;
};

/**
 * Parse a field path with array access (e.g., "poses[0].position.x").
 */
inline std::vector<FieldPathSegment> parse_field_path(const std::string& path) {
    std::vector<FieldPathSegment> segments;
    std::string current_field;

    size_t i = 0;
    while (i < path.size()) {
        if (path[i] == '.') {
            if (!current_field.empty()) {
                segments.push_back({current_field, std::nullopt});
                current_field.clear();
            }
            i++;
        } else if (path[i] == '[') {
            // Save current field if any
            if (!current_field.empty()) {
                FieldPathSegment seg;
                seg.field = current_field;
                current_field.clear();

                // Parse array index
                size_t j = i + 1;
                while (j < path.size() && path[j] != ']') j++;
                if (j < path.size()) {
                    std::string idx_str = path.substr(i + 1, j - i - 1);
                    try {
                        seg.array_index = std::stoi(idx_str);
                    } catch (...) {
                        // Invalid index, ignore
                    }
                }
                segments.push_back(seg);
                i = j + 1;
            } else {
                i++;
            }
        } else {
            current_field += path[i];
            i++;
        }
    }

    if (!current_field.empty()) {
        segments.push_back({current_field, std::nullopt});
    }

    return segments;
}

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
