// Copyright 2026 Multi-Robot Supervision System
// Action Executor Interface - Abstraction for action client execution

#pragma once

#include <cstdint>
#include <functional>
#include <memory>
#include <string>
#include <optional>

namespace robot_agent::interfaces {

/**
 * ActionStatus - Result status of an action execution.
 */
enum class ActionStatus : uint8_t {
    UNKNOWN = 0,
    SUCCEEDED = 1,
    FAILED = 2,
    CANCELLED = 3,
    TIMEOUT = 4,
    REJECTED = 5
};

/**
 * Convert ActionStatus to string for display/logging.
 */
inline const char* action_status_to_string(ActionStatus status) {
    switch (status) {
        case ActionStatus::SUCCEEDED: return "succeeded";
        case ActionStatus::FAILED: return "failed";
        case ActionStatus::CANCELLED: return "cancelled";
        case ActionStatus::TIMEOUT: return "timeout";
        case ActionStatus::REJECTED: return "rejected";
        default: return "unknown";
    }
}

/**
 * ActionResult - Result of an action execution.
 */
struct ActionResult {
    ActionStatus status{ActionStatus::UNKNOWN};
    std::string result_json;   // JSON-encoded result data
    std::string error;         // Error message if failed
    int64_t duration_ms{0};    // Execution duration in milliseconds
};

/**
 * ActionFeedback - Feedback during action execution.
 */
struct ActionFeedback {
    std::string feedback_json;  // JSON-encoded feedback data
    float progress{0.0f};       // Progress 0.0 - 1.0 (if available)
};

/**
 * IActionExecutor - Abstract interface for action execution.
 *
 * This interface decouples action execution from the specific
 * middleware implementation (ROS2 action client, etc.), enabling:
 *   - Unit testing with mock executors
 *   - Support for different action frameworks
 *   - Unified action handling across different action types
 */
class IActionExecutor {
public:
    virtual ~IActionExecutor() = default;

    // ============================================================
    // Callbacks
    // ============================================================

    using ResultCallback = std::function<void(const ActionResult& result)>;
    using FeedbackCallback = std::function<void(const ActionFeedback& feedback)>;

    // ============================================================
    // Action Execution
    // ============================================================

    /**
     * Send a goal to an action server.
     *
     * @param action_type Full action type string (e.g., "nav2_msgs/action/NavigateToPose")
     * @param server_name Action server name (e.g., "/navigate_to_pose")
     * @param goal_json JSON-encoded goal message
     * @param result_cb Callback for action result
     * @param feedback_cb Optional callback for feedback
     * @param timeout_sec Timeout in seconds (0 = no timeout)
     * @return true if goal was sent successfully
     */
    virtual bool send_goal(
        const std::string& action_type,
        const std::string& server_name,
        const std::string& goal_json,
        ResultCallback result_cb,
        FeedbackCallback feedback_cb = nullptr,
        float timeout_sec = 0.0f) = 0;

    /**
     * Cancel the current goal (if any).
     * @return true if cancellation was initiated
     */
    virtual bool cancel_goal() = 0;

    /**
     * Check if an action is currently executing.
     * @return true if executing
     */
    virtual bool is_executing() const = 0;

    // ============================================================
    // Polling (for non-blocking operation)
    // ============================================================

    /**
     * Poll for action progress.
     * Must be called periodically to process callbacks.
     * This allows integration with timer-based architectures.
     */
    virtual void poll() = 0;

    // ============================================================
    // Action Client Management
    // ============================================================

    /**
     * Check if an action server is available.
     * @param action_type Full action type string
     * @param server_name Action server name
     * @return true if server is available
     */
    virtual bool is_server_available(
        const std::string& action_type,
        const std::string& server_name) const = 0;

    /**
     * Get the current action type being executed (if any).
     * @return Action type or empty string if not executing
     */
    virtual std::string current_action_type() const = 0;

    /**
     * Get the current action server being used (if any).
     * @return Server name or empty string if not executing
     */
    virtual std::string current_server_name() const = 0;
};

}  // namespace robot_agent::interfaces
