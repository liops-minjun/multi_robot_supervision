// Copyright 2026 Multi-Robot Supervision System
// Main Agent Class

#pragma once

#include "fleet_agent/core/types.hpp"
#include "fleet_agent/core/config.hpp"
#include "fleet_agent/core/shutdown.hpp"

#include <memory>
#include <string>
#include <thread>
#include <unordered_map>
#include <vector>

// Forward declarations
namespace rclcpp {
class Node;
class Executor;
}

namespace fleet_agent {

namespace capability { class CapabilityScanner; }
namespace executor { class CommandProcessor; }
namespace graph { class GraphStorage; }
namespace transport { class QUICClient; class TlsCredentials; class QUICOutboundSender; }
namespace state { class StateDefinitionStorage; class StateTrackerManager; }
namespace protocol { class MessageHandler; class CapabilityRegistrar; }

/**
 * Agent - Main Fleet Agent orchestrator.
 *
 * Coordinates all subsystems:
 * - ROS2 Node and Executor
 * - Capability discovery
 * - Command processing
 * - Graph storage and execution
 * - QUIC transport
 *
 * Thread model (4 threads):
 * 1. ROS2 Executor - spins ROS2 callbacks
 * 2. QUIC Handler - bidirectional server communication
 * 3. Command Processor - processes commands
 * 4. Heartbeat Sender - sends heartbeat updates
 */
class Agent {
public:
    /**
     * Agent state.
     */
    enum class State {
        CREATED,
        INITIALIZING,
        RUNNING,
        STOPPING,
        STOPPED,
        ERROR
    };

    /**
     * Constructor.
     *
     * @param config Agent configuration
     */
    explicit Agent(const AgentConfig& config);

    ~Agent();

    // Non-copyable, non-movable
    Agent(const Agent&) = delete;
    Agent& operator=(const Agent&) = delete;

    // ============================================================
    // Lifecycle
    // ============================================================

    /**
     * Initialize the agent.
     *
     * Creates all components but doesn't start processing.
     *
     * @return true if initialization successful
     */
    bool initialize();

    /**
     * Start the agent.
     *
     * Starts all threads and begins operation.
     *
     * @return true if started successfully
     */
    bool start();

    /**
     * Stop the agent.
     *
     * Gracefully stops all components and threads.
     */
    void stop();

    /**
     * Run the agent (blocking).
     *
     * Calls initialize(), start(), then blocks until shutdown signal.
     *
     * @return Exit code (0 for success)
     */
    int run();

    /**
     * Get current state.
     */
    State get_state() const;

    /**
     * Check if agent is running.
     */
    bool is_running() const;

    // ============================================================
    // Accessors
    // ============================================================

    /**
     * Get agent ID.
     */
    const std::string& agent_id() const;

    /**
     * Get managed robot IDs.
     */
    std::vector<std::string> agent_ids() const;

    /**
     * Get ROS2 node.
     */
    std::shared_ptr<rclcpp::Node> node() const;

private:
    AgentConfig config_;
    std::atomic<State> state_{State::CREATED};

    // ROS2
    std::shared_ptr<rclcpp::Node> node_;
    std::shared_ptr<rclcpp::Executor> ros_executor_;
    std::thread ros_thread_;

    // Shared state (TBB containers)
    std::unique_ptr<CapabilityStore> capability_store_;
    std::unique_ptr<ExecutionContextMap> execution_contexts_;
    std::unique_ptr<InboundQueue> inbound_queue_;
    std::unique_ptr<QuicOutboundQueue> quic_outbound_queue_;
    std::unique_ptr<CommandQueue> command_queue_;

    // Components
    std::unique_ptr<capability::CapabilityScanner> capability_scanner_;
    std::unique_ptr<executor::CommandProcessor> command_processor_;
    std::unique_ptr<graph::GraphStorage> graph_storage_;

    // State Management (NEW)
    std::unique_ptr<state::StateDefinitionStorage> state_storage_;
    std::unique_ptr<state::StateTrackerManager> state_tracker_mgr_;

    // Protocol Handling (NEW)
    std::unique_ptr<protocol::MessageHandler> message_handler_;
    std::unique_ptr<protocol::CapabilityRegistrar> capability_registrar_;

    // Transport
    std::shared_ptr<transport::TlsCredentials> tls_credentials_;
    std::unique_ptr<transport::QUICClient> quic_client_;
    std::unique_ptr<transport::QUICOutboundSender> quic_outbound_sender_;

    // Note: ShutdownHandler is a singleton, accessed via ShutdownHandler::instance()

    // Capability storage per robot
    std::unordered_map<std::string, std::vector<ActionCapability>> robot_capabilities_;

    // Periodic capability scanning
    std::thread capability_scan_thread_;
    std::atomic<bool> capability_scan_running_{false};

    // Periodic heartbeat sender
    std::thread heartbeat_thread_;
    std::atomic<bool> heartbeat_running_{false};

    // Auto-reconnection with exponential backoff
    std::thread reconnect_thread_;
    std::atomic<bool> reconnect_running_{false};
    std::atomic<bool> connection_lost_{false};
    std::mutex reconnect_mutex_;
    std::condition_variable reconnect_cv_;

    // ============================================================
    // Internal Methods
    // ============================================================

    /**
     * Initialize ROS2 node.
     */
    bool init_ros2();

    /**
     * Initialize shared state containers.
     */
    bool init_state();

    /**
     * Initialize transport (QUIC).
     */
    bool init_transport();

    /**
     * Initialize components.
     */
    bool init_components();

    /**
     * Initialize state management.
     */
    bool init_state_management();

    /**
     * Initialize protocol handlers.
     */
    bool init_protocol();

    /**
     * Start ROS2 executor thread.
     */
    void start_ros2();

    /**
     * Discover and register robot capabilities.
     */
    void discover_capabilities();

    /**
     * Start/stop heartbeat sender.
     */
    void start_heartbeat_thread();
    void stop_heartbeat_thread();

    /**
     * Register agent with central server via HTTP API.
     *
     * @return true if registration successful
     */
    bool register_with_server();

    /**
     * Handle incoming server message.
     */
    void on_server_message(const fleet::v1::ServerMessage& message);

    /**
     * Handle QUIC connection state change.
     */
    void on_quic_connection_change(bool connected);

    /**
     * Configure QUIC stream handler for inbound ServerMessage frames.
     */
    void setup_quic_stream_handler();

    /**
     * Setup signal handlers for graceful shutdown.
     */
    void setup_shutdown_handler();

    /**
     * Start periodic capability scanning thread.
     * Scans for new/removed action servers every 1 second.
     */
    void start_capability_scan_thread();

    /**
     * Stop periodic capability scanning thread.
     */
    void stop_capability_scan_thread();

    /**
     * Re-register capabilities when changes detected.
     */
    void on_capabilities_changed();

    /**
     * Re-register cached capabilities after reconnect.
     */
    void resync_capabilities();

    /**
     * Start auto-reconnection thread.
     * Handles reconnection with exponential backoff when connection is lost.
     */
    void start_reconnect_thread();

    /**
     * Stop auto-reconnection thread.
     */
    void stop_reconnect_thread();

    /**
     * Reconnection loop with exponential backoff.
     * Runs in reconnect_thread_.
     */
    void reconnect_loop();

    /**
     * Attempt to reconnect to the server.
     * @return true if reconnection successful
     */
    bool attempt_reconnect();
};

}  // namespace fleet_agent
