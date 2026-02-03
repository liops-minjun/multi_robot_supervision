// Copyright 2026 Multi-Robot Supervision System
// Action Executor - ROS2 action client wrapper

#pragma once

#include "robot_agent/core/types.hpp"
#include "robot_agent/executor/typed_action_client.hpp"

#include <atomic>
#include <chrono>
#include <functional>
#include <memory>
#include <mutex>
#include <string>

#include <rclcpp/rclcpp.hpp>
#include <rclcpp_action/rclcpp_action.hpp>

namespace robot_agent {
namespace executor {

/**
 * ActionExecutor - Executes ROS2 actions for a single robot.
 *
 * Handles:
 * - Dynamic action type loading
 * - Goal creation and sending
 * - Feedback monitoring
 * - Result handling and callback invocation
 * - Timeout management
 * - Cancellation
 *
 * Usage:
 *   ActionExecutor executor(node, "robot_001", "/robot_001", capabilities,
 *                           result_callback, feedback_callback);
 *
 *   ActionRequest request{...};
 *   if (executor.execute(request)) {
 *       // Action started, wait for callbacks
 *   }
 */
class ActionExecutor {
public:
    /**
     * Constructor.
     *
     * @param node ROS2 node
     * @param agent_id Robot identifier
     * @param ros_namespace Robot's ROS namespace
     * @param capabilities Capability store for action resolution
     * @param result_callback Called when action completes
     * @param feedback_callback Called on action feedback
     */
    ActionExecutor(
        rclcpp::Node::SharedPtr node,
        const std::string& agent_id,
        const std::string& ros_namespace,
        CapabilityStore& capabilities,
        ActionResultCallback result_callback,
        ActionFeedbackCallback feedback_callback = nullptr
    );

    ~ActionExecutor();

    // ============================================================
    // Execution Control
    // ============================================================

    /**
     * Execute an action request.
     *
     * @param request Action request details
     * @return true if action was started successfully
     */
    bool execute(const ActionRequest& request);

    /**
     * Cancel current action.
     *
     * @param reason Cancellation reason
     */
    void cancel(const std::string& reason);

    // ============================================================
    // State Queries
    // ============================================================

    /**
     * Check if currently executing.
     */
    bool is_executing() const { return executing_.load(); }

    /**
     * Get current action type.
     */
    std::string current_action_type() const;

    /**
     * Get current command ID.
     */
    std::string current_command_id() const;

    /**
     * Get current task ID.
     */
    std::string current_task_id() const;

    /**
     * Get current step ID.
     */
    std::string current_step_id() const;

    /**
     * Get robot ID.
     */
    const std::string& agent_id() const { return agent_id_; }

private:
    rclcpp::Node::SharedPtr node_;
    std::string agent_id_;
    std::string ros_namespace_;
    CapabilityStore& capabilities_;
    ActionResultCallback result_callback_;
    ActionFeedbackCallback feedback_callback_;

    // Current execution state
    std::atomic<bool> executing_{false};
    ActionRequest current_request_;
    mutable std::mutex request_mutex_;

    // Action client (created per action type)
    std::unique_ptr<ITypedActionClient> action_client_;

    // Current goal handle (for cancellation)
    std::shared_ptr<ActionGoalHandle> current_goal_handle_;

    // Timeout timer
    rclcpp::TimerBase::SharedPtr timeout_timer_;

    // Timing
    std::chrono::steady_clock::time_point started_at_;

    // ============================================================
    // Internal Methods
    // ============================================================

    /**
     * Create action client for action type.
     */
    bool create_action_client(
        const std::string& action_type,
        const std::string& server_name
    );

    /**
     * Resolve action server from capabilities.
     */
    std::optional<std::string> resolve_server(const std::string& action_type);

    /**
     * Internal callbacks from action client.
     */
    void on_goal_accepted();
    void on_result(bool success, const std::string& result_json);
    void on_feedback(const std::string& feedback_json);
    void on_timeout();
    void on_cancel_complete(bool success);

    /**
     * Complete execution and invoke callback.
     */
    void complete_execution(
        int status,  // ActionStatus enum
        const std::string& result_json,
        const std::string& error
    );

    /**
     * Parse feedback JSON to extract progress.
     */
    float extract_progress(const std::string& feedback_json);
};

}  // namespace executor
}  // namespace robot_agent
