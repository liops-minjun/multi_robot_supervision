// Copyright 2026 Multi-Robot Supervision System
// Command Processor - Main command handling pipeline

#pragma once

#include "robot_agent/core/types.hpp"
#include "robot_agent/executor/precondition.hpp"
#include "robot_agent/executor/action_executor.hpp"
#include "robot_agent/executor/task_log_sender.hpp"

#include <atomic>
#include <memory>
#include <mutex>
#include <string>
#include <thread>
#include <unordered_map>

#include <rclcpp/rclcpp.hpp>

// Generated protobuf headers
#include "fleet/v1/service.pb.h"
#include "fleet/v1/commands.pb.h"
#include "fleet/v1/graphs.pb.h"

// Forward declarations
namespace robot_agent {
namespace graph {
class GraphStorage;
}
namespace state {
class StateTrackerManager;
}
}

namespace robot_agent {
namespace executor {

/**
 * CommandProcessor - Central command processing pipeline.
 *
 * Runs in dedicated thread (Thread 4) and handles:
 * - Execute commands from server
 * - Cancel commands
 * - Graph deployment
 * - Graph execution
 * - Precondition evaluation (Hybrid control)
 * - Result routing
 *
 * Thread architecture:
 *   QUIC Receiver → inbound_queue_ → [CommandProcessor] → executors
 *                                           ↓
 *                                   quic_outbound_queue_ → QUIC Sender
 *
 * Usage:
 *   CommandProcessor processor(node, agent_id, inbound_queue, outbound_queue,
 *                              capability_store,
 *                              execution_contexts, graph_storage);
 *   processor.add_robot("robot_001", "/robot_001");
 *   processor.start();
 */
class CommandProcessor {
public:
    CommandProcessor(
        rclcpp::Node::SharedPtr node,
        const std::string& agent_id,
        InboundQueue& inbound_queue,
        QuicOutboundQueue& quic_outbound_queue,
        CapabilityStore& capability_store,
        ExecutionContextMap& execution_contexts,
        graph::GraphStorage& graph_storage,
        state::StateTrackerManager* state_tracker_mgr = nullptr
    );

    ~CommandProcessor();

    // ============================================================
    // Lifecycle
    // ============================================================

    /**
     * Start processing thread.
     */
    void start();

    /**
     * Stop processing thread.
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
    void add_robot(const std::string& agent_id, const std::string& ros_namespace);

    /**
     * Remove a robot.
     */
    void remove_robot(const std::string& agent_id);

    /**
     * Rename a robot (update robot ID mapping).
     * Used when server assigns a new ID after registration.
     *
     * @param old_id Previous robot ID
     * @param new_id New robot ID from server
     * @return true if robot was found and renamed
     */
    bool rename_robot(const std::string& old_id, const std::string& new_id);

    /**
     * Cancel action for a robot.
     *
     * @param agent_id Robot identifier
     * @param reason Cancellation reason
     */
    void cancel_action(const std::string& agent_id, const std::string& reason);

    // ============================================================
    // Direct Command Injection (from protocol handler)
    // ============================================================

    void enqueue_execute_command(const fleet::v1::ExecuteCommand& cmd,
                                 const std::string& message_id);

    // ============================================================
    // State Access (for multi-robot coordination)
    // ============================================================

    /**
     * Update other robots' state (from server broadcast).
     *
     * Used for Hybrid control precondition evaluation.
     */
    void update_fleet_state(
        const std::unordered_map<std::string, int>& robot_states,
        const std::unordered_map<std::string, bool>& robot_executing,
        const std::unordered_map<std::string, float>& robot_staleness = {},
        const std::unordered_map<std::string, bool>& robot_online = {}
    );

    /**
     * Callback type for requesting server query.
     *
     * Set this to enable Hybrid control with server-side state.
     */
    using ServerQueryCallback = std::function<void(
        const std::string& agent_id,  // Which robot's state to query
        std::function<void(int state, bool executing)> response_callback
    )>;

    void set_server_query_callback(ServerQueryCallback callback);

private:
    // Dependencies
    rclcpp::Node::SharedPtr node_;
    std::string agent_id_;
    InboundQueue& inbound_queue_;
    QuicOutboundQueue& quic_outbound_queue_;
    CapabilityStore& capability_store_;
    ExecutionContextMap& execution_contexts_;
    graph::GraphStorage& graph_storage_;
    state::StateTrackerManager* state_tracker_mgr_{nullptr};

    // Per-robot executors
    std::unordered_map<std::string, std::unique_ptr<ActionExecutor>> executors_;
    std::mutex executors_mutex_;

    // Precondition evaluator
    PreconditionEvaluator precond_evaluator_;

    // Task log sender for streaming execution logs
    std::unique_ptr<TaskLogSender> task_log_sender_;

    // Processing thread
    std::atomic<bool> running_{false};
    std::thread processor_thread_;

    // Multi-robot state cache (for Hybrid control)
    std::unordered_map<std::string, int> fleet_states_;
    std::unordered_map<std::string, bool> fleet_executing_;
    std::unordered_map<std::string, float> fleet_staleness_;
    std::unordered_map<std::string, bool> fleet_online_;
    std::mutex fleet_state_mutex_;

    // Server query callback
    ServerQueryCallback server_query_callback_;

    struct CommandStateTransitions {
        std::vector<std::string> during_states;
        std::vector<std::string> success_states;
        std::vector<std::string> failure_states;
        // Additional info for detailed logging
        std::string action_type;
        std::string action_server;
        std::string goal_params;  // JSON params for logging
    };

    std::unordered_map<std::string, CommandStateTransitions> command_states_;
    std::mutex command_states_mutex_;

    // ============================================================
    // Main Processing Loop
    // ============================================================

    void process_loop();

    // ============================================================
    // Message Handlers
    // ============================================================

    void handle_message(const InboundCommand& cmd);
    void handle_execute_command(const fleet::v1::ExecuteCommand& cmd,
                               const std::string& message_id);
    void handle_cancel_command(const fleet::v1::CancelCommand& cmd);
    void handle_deploy_graph(const fleet::v1::DeployGraphRequest& req);
    void handle_ping(const fleet::v1::PingRequest& ping);
    void handle_fleet_state_broadcast(const fleet::v1::FleetStateBroadcast& broadcast);

    // ============================================================
    // Action Result Handling
    // ============================================================

    /**
     * Called by ActionExecutor when action completes.
     */
    void on_action_result(const ActionResultInternal& result);

    /**
     * Called by ActionExecutor on feedback.
     */
    void on_action_feedback(const std::string& agent_id, float progress);

    // ============================================================
    // Result Sending
    // ============================================================

    void send_action_result(const ActionResultInternal& result);
    void send_action_feedback(
        const std::string& command_id,
        const std::string& agent_id,
        const std::string& task_id,
        const std::string& step_id,
        float progress
    );
    void send_deploy_response(
        const std::string& correlation_id,
        bool success,
        const std::string& graph_id = "",
        int version = 0,
        const std::string& error = ""
    );
    void send_pong(const std::string& ping_id, int64_t server_timestamp);

    /**
     * Send immediate state update to server (bypasses heartbeat interval).
     * Called when action starts or completes to ensure real-time state visibility.
     */
    void send_immediate_state_update(
        const std::string& agent_id,
        const std::string& state_name,
        bool is_executing,
        const std::string& task_id = "",
        const std::string& step_id = ""
    );

    // ============================================================
    // Helpers
    // ============================================================

    /**
     * Build precondition context for evaluation.
     */
    PreconditionEvaluator::Context build_precond_context(const std::string& agent_id);

    /**
     * Get executor for robot.
     */
    ActionExecutor* get_executor(const std::string& agent_id);

    /**
     * Update execution context for robot.
     */
    void update_execution_context(
        const std::string& agent_id,
        RobotExecutionState state,
        const std::string& command_id = "",
        const std::string& task_id = "",
        const std::string& step_id = "",
        const std::string& action_type = ""
    );
};

}  // namespace executor
}  // namespace robot_agent
