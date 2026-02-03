// Copyright 2026 Multi-Robot Supervision System
// ROS2 Action Executor - Implements IActionExecutor for ROS2 action clients

#pragma once

#include "robot_agent/interfaces/action_executor.hpp"
#include "robot_agent/executor/typed_action_client.hpp"

#include <atomic>
#include <chrono>
#include <memory>
#include <mutex>
#include <string>
#include <unordered_map>

#include <rclcpp/rclcpp.hpp>

namespace robot_agent::executor {

/**
 * ROS2ActionExecutor - Implements IActionExecutor for ROS2 action servers.
 *
 * This class manages DynamicActionClient instances and provides the
 * IActionExecutor interface for executing actions on ROS2 action servers.
 *
 * Features:
 *   - Dynamic action type support (any ROS2 action type)
 *   - Client caching for performance
 *   - Timeout management
 *   - Result/feedback callback forwarding
 *
 * Usage:
 *   auto executor = std::make_unique<ROS2ActionExecutor>(node);
 *   executor->send_goal(
 *       "nav2_msgs/action/NavigateToPose",
 *       "/navigate_to_pose",
 *       goal_json,
 *       result_callback,
 *       feedback_callback,
 *       60.0f);
 */
class ROS2ActionExecutor : public interfaces::IActionExecutor {
public:
    /**
     * Constructor.
     *
     * @param node ROS2 node for creating action clients
     */
    explicit ROS2ActionExecutor(rclcpp::Node::SharedPtr node);

    ~ROS2ActionExecutor() override;

    // ============================================================
    // IActionExecutor Implementation
    // ============================================================

    /**
     * Send a goal to an action server.
     *
     * @param action_type Full action type string
     * @param server_name Action server name
     * @param goal_json JSON-encoded goal message
     * @param result_cb Callback for action result
     * @param feedback_cb Optional callback for feedback
     * @param timeout_sec Timeout in seconds (0 = no timeout)
     * @return true if goal was sent successfully
     */
    bool send_goal(
        const std::string& action_type,
        const std::string& server_name,
        const std::string& goal_json,
        ResultCallback result_cb,
        FeedbackCallback feedback_cb = nullptr,
        float timeout_sec = 0.0f) override;

    /**
     * Cancel the current goal.
     */
    bool cancel_goal() override;

    /**
     * Check if an action is currently executing.
     */
    bool is_executing() const override;

    /**
     * Poll for action progress.
     * Checks for timeout and processes any pending callbacks.
     */
    void poll() override;

    /**
     * Check if an action server is available.
     */
    bool is_server_available(
        const std::string& action_type,
        const std::string& server_name) const override;

    /**
     * Get the current action type being executed.
     */
    std::string current_action_type() const override;

    /**
     * Get the current action server being used.
     */
    std::string current_server_name() const override;

    // ============================================================
    // Additional Methods
    // ============================================================

    /**
     * Clear the client cache.
     * Useful when action servers have been restarted.
     */
    void clear_client_cache();

    /**
     * Get the number of cached clients.
     */
    size_t cached_client_count() const;

private:
    rclcpp::Node::SharedPtr node_;

    // Client cache: action_server -> DynamicActionClient
    mutable std::unordered_map<std::string, std::unique_ptr<DynamicActionClient>> client_cache_;
    mutable std::mutex cache_mutex_;

    // Current execution state
    std::atomic<bool> is_executing_{false};
    std::string current_action_type_;
    std::string current_server_name_;
    std::shared_ptr<ActionGoalHandle> current_goal_handle_;
    mutable std::mutex execution_mutex_;

    // Timeout tracking
    std::chrono::steady_clock::time_point action_started_at_;
    float timeout_sec_{0.0f};

    // Stored callbacks
    ResultCallback result_callback_;
    FeedbackCallback feedback_callback_;

    /**
     * Get or create an action client for the given server.
     *
     * @param server_name Action server name
     * @param action_type Action type
     * @return Pointer to client (null if creation failed)
     */
    DynamicActionClient* get_or_create_client(
        const std::string& server_name,
        const std::string& action_type);

    /**
     * Handle action result from the client.
     */
    void on_result(bool success, const std::string& result_json);

    /**
     * Handle feedback from the client.
     */
    void on_feedback(const std::string& feedback_json);

    /**
     * Clear current execution state.
     */
    void clear_execution_state();
};

}  // namespace robot_agent::executor
