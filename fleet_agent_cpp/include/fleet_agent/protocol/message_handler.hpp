// Copyright 2026 Multi-Robot Supervision System
// Protocol Message Handler - Handles all server messages

#pragma once

#include "fleet_agent/core/types.hpp"
#include "fleet_agent/state/state_definition.hpp"
#include "fleet_agent/state/state_tracker.hpp"
#include "fleet_agent/state/graph_state.hpp"
#include "fleet_agent/graph/storage.hpp"

#include <functional>
#include <memory>
#include <string>

// Forward declarations
namespace fleet {
namespace v1 {
class ServerMessage;
class AgentMessage;
class ExecuteCommand;
class CancelCommand;
class ConfigUpdate;
class PingRequest;
class DeployGraphRequest;
class FleetStateBroadcast;
class StartTaskCommand;
}
}

namespace fleet_agent {

namespace transport { class QUICClient; }
namespace executor { class CommandProcessor; class TaskExecutor; }

namespace protocol {

/**
 * MessageHandler - Central handler for all server protocol messages
 *
 * Processes incoming ServerMessage and generates appropriate AgentMessage responses.
 * Coordinates with various subsystems (state, graph, command processor).
 *
 * Thread-safe and designed for concurrent message processing.
 */
class MessageHandler {
public:
    /**
     * Dependencies for message handling
     */
    struct Dependencies {
        transport::QUICClient* quic_client{nullptr};
        state::StateDefinitionStorage* state_storage{nullptr};
        state::StateTrackerManager* state_tracker_mgr{nullptr};
        state::FleetStateCache* fleet_state_cache{nullptr};
        graph::GraphStorage* graph_storage{nullptr};
        executor::CommandProcessor* command_processor{nullptr};
        executor::TaskExecutor* task_executor{nullptr};
        CommandQueue* command_queue{nullptr};
        QuicOutboundQueue* quic_outbound_queue{nullptr};
        std::string agent_id;
    };

    /**
     * Result of message handling
     */
    struct HandleResult {
        bool success{false};
        std::string error;
        std::shared_ptr<fleet::v1::AgentMessage> response;
    };

    explicit MessageHandler(const Dependencies& deps);
    ~MessageHandler();

    // ============================================================
    // Main Entry Point
    // ============================================================

    /**
     * Handle a server message.
     *
     * Dispatches to appropriate handler based on message type.
     *
     * @param message Incoming server message
     * @return Handle result with optional response
     */
    HandleResult handle(const fleet::v1::ServerMessage& message);

    // ============================================================
    // Individual Handlers
    // ============================================================

    /**
     * Handle ExecuteCommand message.
     *
     * Queues command for execution on robot.
     */
    HandleResult handle_execute_command(const fleet::v1::ExecuteCommand& cmd);

    /**
     * Handle CancelCommand message.
     *
     * Cancels running action on robot.
     */
    HandleResult handle_cancel_command(const fleet::v1::CancelCommand& cmd);

    /**
     * Handle ConfigUpdate message.
     *
     * Updates state definition for robot.
     */
    HandleResult handle_config_update(const fleet::v1::ConfigUpdate& update);

    /**
     * Handle PingRequest message.
     *
     * Responds with PongResponse.
     */
    HandleResult handle_ping(const fleet::v1::PingRequest& ping);

    /**
     * Handle DeployGraphRequest message.
     *
     * Stores action graph and sends confirmation.
     */
    HandleResult handle_deploy_graph(const fleet::v1::DeployGraphRequest& deploy);

    /**
     * Handle FleetStateBroadcast message.
     *
     * Updates local fleet state cache for cross-agent coordination.
     */
    HandleResult handle_fleet_state(const fleet::v1::FleetStateBroadcast& fleet_state);

    /**
     * Handle StartTaskCommand message.
     *
     * Starts agent-driven task execution.
     */
    HandleResult handle_start_task(const fleet::v1::StartTaskCommand& cmd);

    // ============================================================
    // Response Builders
    // ============================================================

    /**
     * Build deployment response message.
     */
    std::shared_ptr<fleet::v1::AgentMessage> build_deploy_response(
        const std::string& correlation_id,
        const std::string& graph_id,
        int version,
        const std::string& checksum,
        bool success,
        const std::string& error = ""
    );

    /**
     * Build pong response message.
     */
    std::shared_ptr<fleet::v1::AgentMessage> build_pong_response(
        const std::string& ping_id,
        int64_t server_timestamp_ms
    );

    /**
     * Build action result message.
     */
    std::shared_ptr<fleet::v1::AgentMessage> build_action_result(
        const ActionResultInternal& result
    );

    /**
     * Build config update acknowledgment.
     */
    std::shared_ptr<fleet::v1::AgentMessage> build_config_ack(
        const std::string& agent_id,
        int version,
        bool success,
        const std::string& error = ""
    );

private:
    Dependencies deps_;

    /**
     * Send response via QUIC.
     */
    bool send_response(std::shared_ptr<fleet::v1::AgentMessage> response);

    /**
     * Parse state definition from ConfigUpdate.
     */
    std::optional<state::StateDefinition> parse_state_definition(
        const fleet::v1::ConfigUpdate& update
    );
};

// ============================================================
// Capability Registration Helper
// ============================================================

/**
 * Build and send capability registration message
 */
class CapabilityRegistrar {
public:
    CapabilityRegistrar(
        transport::QUICClient* quic_client,
        const std::string& agent_id
    );

    /**
     * Register capabilities for a robot.
     *
     * @param agent_id Robot identifier
     * @param capabilities List of discovered capabilities
     * @return true if registration message sent
     */
    bool register_capabilities(
        const std::string& agent_id,
        const std::vector<ActionCapability>& capabilities
    );

    /**
     * Register all robots' capabilities.
     */
    bool register_all(
        const std::vector<std::pair<std::string, std::vector<ActionCapability>>>& robot_capabilities
    );

private:
    transport::QUICClient* quic_client_;
    std::string agent_id_;

    std::shared_ptr<fleet::v1::AgentMessage> build_registration_message(
        const std::string& agent_id,
        const std::vector<ActionCapability>& capabilities
    );
};

}  // namespace protocol
}  // namespace fleet_agent
