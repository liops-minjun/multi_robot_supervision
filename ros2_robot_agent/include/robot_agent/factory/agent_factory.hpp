// Copyright 2026 Multi-Robot Supervision System
// Agent Factory - Dependency injection for Agent components

#pragma once

#include "robot_agent/core/config.hpp"
#include "robot_agent/core/types.hpp"  // For CapabilityStore
#include "robot_agent/interfaces/transport.hpp"
#include "robot_agent/interfaces/capability_scanner.hpp"
#include "robot_agent/interfaces/action_executor.hpp"

#include <memory>
#include <string>

#include <rclcpp/rclcpp.hpp>

// Forward declarations
namespace robot_agent {
class Agent;
namespace transport { class QUICClient; struct QUICConfig; }
namespace capability { class CapabilityScanner; }
}

namespace robot_agent::factory {

/**
 * AgentComponents - Container for injected interface implementations.
 *
 * Used by AgentFactory to create and hold the component instances
 * that will be injected into the Agent.
 */
struct AgentComponents {
    std::unique_ptr<interfaces::ITransport> transport;
    std::unique_ptr<interfaces::ICapabilityScanner> scanner;
    std::unique_ptr<interfaces::IActionExecutor> executor;

    // Validity check
    bool is_valid() const {
        return transport != nullptr &&
               scanner != nullptr &&
               executor != nullptr;
    }
};

/**
 * AgentFactory - Creates Agent instances with proper dependency injection.
 *
 * This factory provides:
 *   - Default component creation for production use
 *   - Custom component injection for testing
 *   - Proper initialization order
 *
 * Production Usage:
 *   auto agent = AgentFactory::create_agent("config.yaml");
 *   agent->initialize();
 *   agent->run();
 *
 * Testing Usage:
 *   AgentComponents components;
 *   components.transport = std::make_unique<MockTransport>();
 *   components.scanner = std::make_unique<MockScanner>();
 *   components.executor = std::make_unique<MockExecutor>();
 *   auto agent = AgentFactory::create_agent_with_components(config, std::move(components));
 */
class AgentFactory {
public:
    /**
     * Create a production Agent from config file.
     *
     * Creates all default components (QUIC transport, ROS2 scanner, ROS2 executor)
     * and initializes the Agent.
     *
     * @param config_path Path to YAML configuration file
     * @return Unique pointer to Agent, or nullptr on failure
     */
    static std::unique_ptr<Agent> create_agent(const std::string& config_path);

    /**
     * Create a production Agent from AgentConfig.
     *
     * @param config Agent configuration
     * @return Unique pointer to Agent, or nullptr on failure
     */
    static std::unique_ptr<Agent> create_agent(const AgentConfig& config);

    /**
     * Create Agent with custom components (for testing/DI).
     *
     * Allows injection of mock or custom implementations.
     *
     * @param config Agent configuration
     * @param components Pre-created components to inject
     * @return Unique pointer to Agent, or nullptr on failure
     */
    static std::unique_ptr<Agent> create_agent_with_components(
        const AgentConfig& config,
        AgentComponents components);

    /**
     * Create default transport (QUICTransportAdapter).
     *
     * @param config Agent configuration with QUIC settings
     * @return Transport interface implementation
     */
    static std::unique_ptr<interfaces::ITransport> create_default_transport(
        const AgentConfig& config);

    /**
     * Create default capability scanner (CapabilityScannerAdapter).
     *
     * @param node ROS2 node for creating subscriptions
     * @param namespace_filter Robot namespace to scan
     * @param store Reference to capability store
     * @return Capability scanner interface implementation
     */
    static std::unique_ptr<interfaces::ICapabilityScanner> create_default_scanner(
        rclcpp::Node::SharedPtr node,
        const std::string& namespace_filter,
        CapabilityStore& store);

    /**
     * Create default action executor (ROS2ActionExecutor).
     *
     * @param node ROS2 node for creating action clients
     * @return Action executor interface implementation
     */
    static std::unique_ptr<interfaces::IActionExecutor> create_default_executor(
        rclcpp::Node::SharedPtr node);

    /**
     * Create all default components.
     *
     * Convenience method that creates transport, scanner, and executor
     * with default implementations.
     *
     * @param node ROS2 node
     * @param config Agent configuration
     * @param store Reference to capability store
     * @return AgentComponents with all components initialized
     */
    static AgentComponents create_default_components(
        rclcpp::Node::SharedPtr node,
        const AgentConfig& config,
        CapabilityStore& store);

private:
    /**
     * Create QUIC configuration from agent config.
     */
    static transport::QUICConfig create_quic_config(const AgentConfig& config);
};

}  // namespace robot_agent::factory
