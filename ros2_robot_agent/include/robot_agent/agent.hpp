// Copyright 2026 Multi-Robot Supervision System
// Robot Agent - Tick-based architecture (refactored)
//
// Architecture: Interface-based design with dependency injection support
// See CLAUDE.md "Development Principles" section for guidelines

#pragma once

#include "robot_agent/core/types.hpp"
#include "robot_agent/core/config.hpp"
#include "robot_agent/core/shutdown.hpp"
#include "robot_agent/core/agent_id_storage.hpp"
#include "robot_agent/executor/typed_action_client.hpp"

// Interfaces for dependency injection
#include "robot_agent/interfaces/transport.hpp"
#include "robot_agent/interfaces/capability_scanner.hpp"
#include "robot_agent/interfaces/action_executor.hpp"

#include <rclcpp/rclcpp.hpp>
#include <tbb/concurrent_queue.h>
#include <tbb/concurrent_hash_map.h>
#include <memory>
#include <string>
#include <unordered_map>
#include <vector>
#include <chrono>
#include <optional>
#include <atomic>
#include <thread>
#include <condition_variable>

// Forward declarations
namespace robot_agent {
namespace capability { class CapabilityScanner; }
namespace graph { class GraphStorage; class GraphExecutor; }
namespace transport { class QUICClient; class QUICStream; }
namespace state { class StateTrackerManager; class FleetStateCache; class StateDefinitionStorage; }
namespace telemetry { class TelemetryStore; class RobotTelemetryCollector; }
namespace factory { class AgentFactory; struct AgentComponents; }
}

namespace fleet::v1 {
class ServerMessage;
class AgentMessage;
class BehaviorTree;
class Vertex;
class DeployGraphRequest;
class ExecuteCommand;
}

namespace robot_agent {

/**
 * Agent - Tick-based robot agent with multi-threaded ROS2 execution.
 *
 * Architecture:
 *   ┌─────────────────────────────────────────────────────┐
 *   │  ROS2 MultiThreadedExecutor                         │
 *   │    ├── Timer callbacks (with mutex protection)      │
 *   │    │     ├── main_timer_ (10ms)  → tick()          │
 *   │    │     ├── heartbeat_timer_ (100ms)              │
 *   │    │     └── slow_timer_ (1s)                      │
 *   │    └── Subscriber callbacks (parallel)              │
 *   │          ├── JointState, TF, Odometry              │
 *   │          └── Action client feedback                 │
 *   └─────────────────────────────────────────────────────┘
 *
 * Thread model: Multi-threaded with mutex-protected shared state
 * - Subscribers can process high-frequency topics in parallel
 * - Timer callbacks are serialized via mutexes for task state
 */
class Agent {
public:
    enum class State {
        CREATED,
        INITIALIZING,
        RUNNING,
        STOPPING,
        STOPPED,
        ERROR
    };

    /**
     * Production constructor - creates components internally.
     *
     * @param config Agent configuration
     */
    explicit Agent(const AgentConfig& config);

    /**
     * DI constructor - accepts pre-created components for testing.
     *
     * This constructor allows injection of mock or custom implementations:
     *   AgentComponents components;
     *   components.transport = std::make_unique<MockTransport>();
     *   components.scanner = std::make_unique<MockScanner>();
     *   components.executor = std::make_unique<MockExecutor>();
     *   auto agent = std::make_unique<Agent>(config, std::move(components));
     *
     * @param config Agent configuration
     * @param transport Injected transport (ITransport implementation)
     * @param scanner Injected scanner (ICapabilityScanner implementation)
     * @param executor Injected executor (IActionExecutor implementation)
     */
    Agent(const AgentConfig& config,
          std::unique_ptr<interfaces::ITransport> transport,
          std::unique_ptr<interfaces::ICapabilityScanner> scanner,
          std::unique_ptr<interfaces::IActionExecutor> executor);

    ~Agent();

    Agent(const Agent&) = delete;
    Agent& operator=(const Agent&) = delete;

    // Lifecycle
    bool initialize();
    bool start();
    void stop();
    int run();

    State get_state() const { return state_.load(); }
    bool is_running() const { return state_ == State::RUNNING; }
    const std::string& agent_id() const { return effective_agent_id_; }
    std::shared_ptr<rclcpp::Node> node() const { return node_; }

    // Allow factory access to private members
    friend class factory::AgentFactory;

private:
    // ============================================================
    // Configuration
    // ============================================================
    AgentConfig config_;
    std::atomic<State> state_{State::CREATED};

    std::unique_ptr<AgentIdStorage> agent_id_storage_;
    std::string effective_agent_id_;
    std::string hardware_fingerprint_;

    // ============================================================
    // ROS2 (Multi-threaded)
    // ============================================================
    std::shared_ptr<rclcpp::Node> node_;
    std::unique_ptr<rclcpp::executors::MultiThreadedExecutor> executor_;

    // Timers - ALL processing happens in these callbacks
    rclcpp::TimerBase::SharedPtr main_timer_;       // 10ms - main tick
    rclcpp::TimerBase::SharedPtr heartbeat_timer_;  // 100ms - heartbeat
    rclcpp::TimerBase::SharedPtr slow_timer_;       // 1000ms - capability scan, reconnect

    // ============================================================
    // Injected Interfaces (DI - used when provided via DI constructor)
    // ============================================================
    std::unique_ptr<interfaces::ITransport> transport_interface_;
    std::unique_ptr<interfaces::ICapabilityScanner> scanner_interface_;
    std::unique_ptr<interfaces::IActionExecutor> executor_interface_;
    bool using_injected_components_{false};  // True when using DI constructor

    // ============================================================
    // QUIC Transport (legacy - used when NOT using DI constructor)
    // ============================================================
    std::unique_ptr<transport::QUICClient> quic_client_;

    // Inbound message framing (accessed from QUIC callback thread)
    std::vector<uint8_t> inbound_buffer_;
    uint32_t expected_msg_len_{0};
    std::mutex inbound_mutex_;  // Only for QUIC callback synchronization

    // Outbound message buffer (lock-free concurrent queue)
    tbb::concurrent_queue<std::shared_ptr<fleet::v1::AgentMessage>> outbound_queue_;

    // Persistent outbound stream (reused across flushes to avoid blocking)
    std::shared_ptr<transport::QUICStream> outbound_stream_;

    // Dedicated sender thread for non-blocking transmission
    std::thread sender_thread_;
    std::atomic<bool> sender_running_{false};
    std::condition_variable sender_cv_;
    std::mutex sender_mutex_;

    // Connection state
    std::atomic<bool> connected_{false};
    std::chrono::steady_clock::time_point last_reconnect_attempt_;
    std::atomic<int> reconnect_delay_ms_{1000};

    // ============================================================
    // Capabilities
    // ============================================================
    std::unique_ptr<CapabilityStore> capability_store_;
    std::unique_ptr<capability::CapabilityScanner> capability_scanner_;

    // ============================================================
    // Graph Storage
    // ============================================================
    std::unique_ptr<graph::GraphStorage> graph_storage_;

    // ============================================================
    // State Management
    // ============================================================
    std::unique_ptr<state::StateDefinitionStorage> state_storage_;
    std::unique_ptr<state::StateTrackerManager> state_tracker_mgr_;
    std::unique_ptr<state::FleetStateCache> fleet_state_cache_;

    // ============================================================
    // Telemetry
    // ============================================================
    std::unique_ptr<telemetry::TelemetryStore> telemetry_store_;
    std::vector<std::unique_ptr<telemetry::RobotTelemetryCollector>> telemetry_collectors_;

    // ============================================================
    // Action Execution (Integrated - no separate TaskExecutor)
    // ============================================================

    // Action clients cache: "action_type|server_name" -> client (concurrent access)
    tbb::concurrent_hash_map<std::string, std::unique_ptr<executor::DynamicActionClient>> action_clients_;

    // Current task execution state (single task at a time, mutex protected)
    struct TaskContext {
        std::string task_id;
        std::string behavior_tree_id;
        std::unique_ptr<fleet::v1::BehaviorTree> behavior_tree;

        std::string current_step_id;
        std::string current_action_type;
        std::shared_ptr<executor::ActionGoalHandle> current_goal;

        std::unordered_map<std::string, std::string> variables;  // step_id.field -> value
        std::chrono::steady_clock::time_point started_at;

        enum class Status {
            RUNNING,
            WAITING_ACTION,
            COMPLETED,
            FAILED,
            CANCELLED
        } status{Status::RUNNING};

        std::string error_message;
    };

    std::optional<TaskContext> current_task_;
    std::mutex task_mutex_;  // Task state modification requires lock

    // Execution state (for heartbeat reporting, all atomic for lock-free reads)
    struct ExecState {
        std::atomic<bool> is_executing{false};
        std::atomic<uint64_t> version{0};  // Increment on each update for consistency
        // Use shared_ptr for lock-free string updates
        std::shared_ptr<const std::string> action_type{std::make_shared<std::string>()};
        std::shared_ptr<const std::string> task_id{std::make_shared<std::string>()};
        std::shared_ptr<const std::string> step_id{std::make_shared<std::string>()};
    };
    ExecState exec_state_;

    // ============================================================
    // Initialization
    // ============================================================
    bool init_ros2();
    bool init_transport();
    bool init_state_management();
    bool init_components();
    bool init_telemetry();
    void setup_shutdown_handler();

    // ============================================================
    // Timer Callbacks (ALL processing here)
    // ============================================================
    void tick();              // 10ms - main processing
    void send_heartbeat();    // 100ms
    void slow_tick();         // 1000ms - capability scan, reconnect check

    // ============================================================
    // QUIC Communication
    // ============================================================
    void poll_quic_receive();
    void flush_outbound();
    void queue_message(std::shared_ptr<fleet::v1::AgentMessage> msg);
    void sender_loop();  // Dedicated sender thread function
    void start_sender_thread();
    void stop_sender_thread();

    void on_quic_data(const uint8_t* data, size_t len);
    void on_complete_message(const uint8_t* data, size_t len);
    void on_server_message(const fleet::v1::ServerMessage& msg);
    void on_connection_change(bool connected);

    bool try_connect();
    bool register_with_server();
    void setup_quic_handler();

    // ============================================================
    // Command Handling
    // ============================================================
    void handle_start_task(const std::string& task_id,
                           const std::string& behavior_tree_id,
                           const std::unordered_map<std::string, std::string>& params);
    void handle_cancel_task(const std::string& task_id, const std::string& reason);
    void handle_deploy_graph(const fleet::v1::DeployGraphRequest& req);
    void handle_ping(const std::string& ping_id, int64_t server_ts);
    void handle_fleet_state(const std::unordered_map<std::string, std::string>& states,
                            const std::unordered_map<std::string, bool>& executing);

    // ============================================================
    // Task Execution (Integrated GraphExecutor + ActionExecutor)
    // ============================================================
    void process_task();      // Called each tick if task is active
    void execute_step(const std::string& step_id);
    void on_action_result(bool success, const std::string& result_json);
    void advance_task(bool step_success);
    void complete_task(bool success, const std::string& error = "");

    std::optional<fleet::v1::Vertex> find_vertex(const std::string& step_id);
    std::optional<std::string> get_entry_point();
    std::optional<std::string> get_next_step(const std::string& current, bool success);

    // ============================================================
    // Action Client Management
    // ============================================================
    executor::DynamicActionClient* get_or_create_client(
        const std::string& action_type,
        const std::string& server_name);
    void poll_all_clients();

    // ============================================================
    // Capability Management
    // ============================================================
    void discover_capabilities();
    void send_capabilities();

    // ============================================================
    // State Reporting
    // ============================================================
    void send_task_state_update();
    void update_exec_state(bool executing,
                           const std::string& action = "",
                           const std::string& task_id = "",
                           const std::string& step_id = "");

    // ============================================================
    // Helpers
    // ============================================================
    static int64_t now_ms();
};

}  // namespace robot_agent
