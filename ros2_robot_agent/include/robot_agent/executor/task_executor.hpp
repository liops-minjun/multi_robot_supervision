// Copyright 2026 Multi-Robot Supervision System
// Task Executor - Agent-driven graph execution engine

#pragma once

#include "robot_agent/core/types.hpp"
#include "robot_agent/graph/executor.hpp"
#include "robot_agent/graph/storage.hpp"
#include "robot_agent/executor/action_executor.hpp"

#include <atomic>
#include <chrono>
#include <functional>
#include <memory>
#include <mutex>
#include <string>
#include <thread>
#include <unordered_map>

#include <rclcpp/rclcpp.hpp>

// Generated protobuf headers
#include "fleet/v1/service.pb.h"
#include "fleet/v1/commands.pb.h"
#include "fleet/v1/common.pb.h"
#include "fleet/v1/graphs.pb.h"

namespace robot_agent {
namespace state { class StateTrackerManager; }
namespace executor {

/**
 * TaskExecutor - Agent-driven task execution engine.
 *
 * Handles complete Behavior Tree execution on the agent side:
 * - Receives StartTaskCommand from server
 * - Executes tree steps sequentially
 * - Passes results between steps (variable substitution)
 * - Reports state updates to server
 * - Handles timeouts and cancellation
 *
 * Thread architecture:
 *   Server (StartTaskCommand) → TaskExecutor → ActionExecutor
 *                                    ↓
 *                          TaskStateUpdate → Server
 *
 * Usage:
 *   TaskExecutor executor(node, agent_id, graph_executor, graph_storage,
 *                         action_executor, outbound_queue);
 *   executor.start();
 *   executor.start_task(task_id, behavior_tree_id, robot_id, params);
 */
class TaskExecutor {
public:
    /**
     * Task status.
     */
    enum class TaskStatus {
        PENDING,
        RUNNING,
        WAITING_PRECONDITION,
        EXECUTING_ACTION,
        COMPLETED,
        FAILED,
        CANCELLED
    };

    /**
     * Running task information.
     */
    struct RunningTask {
        std::string task_id;
        std::string behavior_tree_id;
        std::string robot_id;
        fleet::v1::BehaviorTree behavior_tree;
        graph::GraphExecutor::ExecutionContext ctx;
        TaskStatus status{TaskStatus::PENDING};
        std::chrono::steady_clock::time_point started_at;
        std::chrono::steady_clock::time_point last_state_report;
        std::string blocking_reason;
        bool action_pending{false};  // Waiting for action completion
    };

    /**
     * Constructor.
     *
     * @param node ROS2 node
     * @param agent_id Agent identifier
     * @param graph_executor Graph executor for DAG traversal
     * @param graph_storage Graph storage for retrieving graphs
     * @param outbound_queue Queue for sending messages to server
     * @param state_tracker_mgr Optional state tracker manager
     */
    TaskExecutor(
        rclcpp::Node::SharedPtr node,
        const std::string& agent_id,
        graph::GraphExecutor& graph_executor,
        graph::GraphStorage& graph_storage,
        QuicOutboundQueue& outbound_queue,
        state::StateTrackerManager* state_tracker_mgr = nullptr
    );

    ~TaskExecutor();

    // ============================================================
    // Lifecycle
    // ============================================================

    /**
     * Start the executor thread.
     */
    void start();

    /**
     * Stop the executor thread.
     */
    void stop();

    /**
     * Check if running.
     */
    bool is_running() const { return running_.load(); }

    // ============================================================
    // Robot Management
    // ============================================================

    /**
     * Add a robot to manage.
     *
     * Creates ActionExecutor for the robot.
     */
    void add_robot(const std::string& robot_id, const std::string& ros_namespace,
                   CapabilityStore& capabilities);

    /**
     * Remove a robot.
     */
    void remove_robot(const std::string& robot_id);

    /**
     * Rename a robot (update robot ID mapping).
     * Used when server assigns a new ID after registration.
     *
     * @param old_id Previous robot ID
     * @param new_id New robot ID from server
     * @return true if robot was found and renamed
     */
    bool rename_robot(const std::string& old_id, const std::string& new_id);

    // ============================================================
    // Task Control
    // ============================================================

    /**
     * Start a new task.
     *
     * Called when server sends StartTaskCommand.
     *
     * @param task_id Unique task identifier
     * @param behavior_tree_id Behavior tree to execute
     * @param robot_id Robot to execute on
     * @param params Initial parameters
     * @return true if task was started successfully
     */
    bool start_task(
        const std::string& task_id,
        const std::string& behavior_tree_id,
        const std::string& robot_id,
        const std::unordered_map<std::string, std::string>& params = {}
    );

    /**
     * Cancel a running task.
     *
     * @param task_id Task to cancel
     * @param reason Cancellation reason
     * @return true if task was found and cancelled
     */
    bool cancel_task(const std::string& task_id, const std::string& reason = "");

    /**
     * Get task status.
     *
     * @param task_id Task identifier
     * @return Task status, or nullopt if not found
     */
    std::optional<TaskStatus> get_task_status(const std::string& task_id) const;

    /**
     * Get all running task IDs.
     */
    std::vector<std::string> get_running_task_ids() const;

    // ============================================================
    // Fleet State (for precondition evaluation)
    // ============================================================

    /**
     * Update fleet state cache from server broadcast.
     */
    void update_fleet_state(
        const std::unordered_map<std::string, int>& robot_states,
        const std::unordered_map<std::string, bool>& robot_executing
    );

private:
    // Constants
    static constexpr auto kTickInterval = std::chrono::milliseconds(100);
    static constexpr auto kStateReportInterval = std::chrono::milliseconds(500);

    // Dependencies
    rclcpp::Node::SharedPtr node_;
    std::string agent_id_;
    graph::GraphExecutor& graph_executor_;
    graph::GraphStorage& graph_storage_;
    QuicOutboundQueue& outbound_queue_;
    state::StateTrackerManager* state_tracker_mgr_{nullptr};

    // Per-robot executors
    std::unordered_map<std::string, std::unique_ptr<ActionExecutor>> executors_;
    std::mutex executors_mutex_;

    // Running tasks
    std::unordered_map<std::string, RunningTask> tasks_;
    mutable std::mutex tasks_mutex_;

    // Fleet state cache
    std::unordered_map<std::string, int> fleet_states_;
    std::unordered_map<std::string, bool> fleet_executing_;
    std::mutex fleet_state_mutex_;

    // Execution thread
    std::atomic<bool> running_{false};
    std::thread executor_thread_;

    // ============================================================
    // Main Execution Loop
    // ============================================================

    /**
     * Main tick loop.
     */
    void execution_loop();

    /**
     * Process a single task.
     */
    void process_task(RunningTask& task);

    /**
     * Execute current step of task.
     */
    void execute_current_step(RunningTask& task);

    /**
     * Check precondition for current step.
     *
     * @return true if precondition is satisfied
     */
    bool check_precondition(RunningTask& task);

    // ============================================================
    // Action Handling
    // ============================================================

    /**
     * Called when action completes.
     */
    void on_action_result(const ActionResultInternal& result);

    /**
     * Handle step result and move to next step.
     */
    void handle_step_result(RunningTask& task, const ActionResultInternal& result);

    /**
     * Convert action status to outcome string.
     */
    std::string action_status_to_outcome(int status);

    // ============================================================
    // Task Completion
    // ============================================================

    /**
     * Complete a task.
     */
    void complete_task(RunningTask& task, bool success, const std::string& error = "");

    /**
     * Calculate progress (0.0 - 1.0) for task.
     */
    float calculate_progress(const RunningTask& task);

    // ============================================================
    // Server Communication
    // ============================================================

    /**
     * Report task state to server.
     */
    void report_state_to_server(RunningTask& task);

    /**
     * Report all task states to server.
     */
    void report_all_states();

    /**
     * Report step result to server.
     */
    void report_step_result_to_server(
        const RunningTask& task,
        const ActionResultInternal& result
    );

    /**
     * Convert TaskStatus to proto TaskState.
     */
    fleet::v1::TaskState task_status_to_proto(TaskStatus status);

    // ============================================================
    // Helpers
    // ============================================================

    /**
     * Get current vertex for task.
     */
    std::optional<fleet::v1::Vertex> get_current_vertex(const RunningTask& task);

    /**
     * Get executor for robot.
     */
    ActionExecutor* get_executor(const std::string& robot_id);

    /**
     * Truncate string for logging.
     */
    static std::string truncate(const std::string& str, size_t max_len);
};

}  // namespace executor
}  // namespace robot_agent
