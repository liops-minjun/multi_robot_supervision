// Copyright 2026 Multi-Robot Supervision System
// Agent Factory Implementation

#include "robot_agent/factory/agent_factory.hpp"
#include "robot_agent/agent.hpp"
#include "robot_agent/core/logger.hpp"
#include "robot_agent/core/config.hpp"

// Adapter implementations
#include "robot_agent/transport/quic_transport_adapter.hpp"
#include "robot_agent/capability/capability_scanner_adapter.hpp"
#include "robot_agent/executor/ros2_action_executor.hpp"

// Concrete implementations (for creating adapters)
#include "robot_agent/transport/quic_transport.hpp"
#include "robot_agent/capability/scanner.hpp"

#include <yaml-cpp/yaml.h>
#include <filesystem>

namespace robot_agent::factory {

std::unique_ptr<Agent> AgentFactory::create_agent(const std::string& config_path) {
    // Load configuration
    if (!std::filesystem::exists(config_path)) {
        LOG_ERROR("AgentFactory: Config file not found: {}", config_path);
        return nullptr;
    }

    try {
        YAML::Node yaml = YAML::LoadFile(config_path);
        AgentConfig config;

        // Parse agent section
        if (yaml["agent"]) {
            auto agent = yaml["agent"];
            if (agent["id"]) config.agent_id = agent["id"].as<std::string>();
            if (agent["name"]) config.agent_name = agent["name"].as<std::string>();
            if (agent["namespace"]) config.ros_namespace = agent["namespace"].as<std::string>();
            if (agent["use_server_assigned_id"]) {
                config.use_server_assigned_id = agent["use_server_assigned_id"].as<bool>();
            }
        }

        // Parse server section
        if (yaml["server"]) {
            auto server = yaml["server"];
            if (server["url"]) config.server.url = server["url"].as<std::string>();
            if (server["timeout_sec"]) config.server.timeout_sec = server["timeout_sec"].as<float>();

            // Parse QUIC configuration
            if (server["quic"]) {
                auto quic = server["quic"];
                if (quic["server_address"]) {
                    config.server.quic.server_address = quic["server_address"].as<std::string>();
                }
                if (quic["server_port"]) {
                    config.server.quic.server_port = quic["server_port"].as<uint16_t>();
                }
                if (quic["ca_cert"]) {
                    config.server.quic.ca_cert = quic["ca_cert"].as<std::string>();
                }
                if (quic["client_cert"]) {
                    config.server.quic.client_cert = quic["client_cert"].as<std::string>();
                }
                if (quic["client_key"]) {
                    config.server.quic.client_key = quic["client_key"].as<std::string>();
                }
                if (quic["idle_timeout_ms"]) {
                    config.server.quic.idle_timeout_ms = quic["idle_timeout_ms"].as<int>();
                }
                if (quic["keepalive_interval_ms"]) {
                    config.server.quic.keepalive_interval_ms = quic["keepalive_interval_ms"].as<int>();
                }
                if (quic["enable_0rtt"]) {
                    config.server.quic.enable_0rtt = quic["enable_0rtt"].as<bool>();
                }
                if (quic["enable_datagrams"]) {
                    config.server.quic.enable_datagrams = quic["enable_datagrams"].as<bool>();
                }
            }
        }

        // Parse storage section
        if (yaml["storage"]) {
            auto storage = yaml["storage"];
            if (storage["behavior_trees_path"]) {
                config.storage.behavior_trees_path = storage["behavior_trees_path"].as<std::string>();
            }
            if (storage["agent_id_path"]) {
                config.storage.agent_id_path = storage["agent_id_path"].as<std::string>();
            }
        }

        // Parse logging section
        if (yaml["logging"]) {
            auto logging = yaml["logging"];
            if (logging["level"]) config.logging.level = logging["level"].as<std::string>();
            if (logging["file"]) config.logging.file = logging["file"].as<std::string>();
            if (logging["console"]) config.logging.console = logging["console"].as<bool>();
        }

        return create_agent(config);

    } catch (const YAML::Exception& e) {
        LOG_ERROR("AgentFactory: Failed to parse config: {}", e.what());
        return nullptr;
    }
}

std::unique_ptr<Agent> AgentFactory::create_agent(const AgentConfig& config) {
    LOG_INFO("AgentFactory: Creating agent with id={}",
             config.agent_id.empty() ? "(server-assigned)" : config.agent_id);

    try {
        // Create Agent with config
        // Note: Current Agent implementation creates its own components internally.
        // After Task #5 (Agent refactoring), this will use DI constructor.
        auto agent = std::make_unique<Agent>(config);

        LOG_INFO("AgentFactory: Agent created successfully");
        return agent;

    } catch (const std::exception& e) {
        LOG_ERROR("AgentFactory: Failed to create agent: {}", e.what());
        return nullptr;
    }
}

std::unique_ptr<Agent> AgentFactory::create_agent_with_components(
    const AgentConfig& config,
    AgentComponents components)
{
    if (!components.is_valid()) {
        LOG_ERROR("AgentFactory: Invalid components provided");
        return nullptr;
    }

    LOG_INFO("AgentFactory: Creating agent with custom components (DI mode)");

    try {
        // Use DI constructor with injected components
        return std::make_unique<Agent>(
            config,
            std::move(components.transport),
            std::move(components.scanner),
            std::move(components.executor));

    } catch (const std::exception& e) {
        LOG_ERROR("AgentFactory: Failed to create agent: {}", e.what());
        return nullptr;
    }
}

std::unique_ptr<interfaces::ITransport> AgentFactory::create_default_transport(
    const AgentConfig& config)
{
    LOG_DEBUG("AgentFactory: Creating default QUIC transport");

    // Create transport-layer QUICConfig from agent config
    auto quic_config = create_quic_config(config);

    // Create QUICClient
    auto quic_client = std::make_shared<transport::QUICClient>(quic_config);

    // Create adapter with TLS credentials
    return std::make_unique<transport::QUICTransportAdapter>(
        quic_client,
        quic_config,
        config.server.quic.ca_cert,
        config.server.quic.client_cert,
        config.server.quic.client_key);
}

std::unique_ptr<interfaces::ICapabilityScanner> AgentFactory::create_default_scanner(
    rclcpp::Node::SharedPtr node,
    const std::string& namespace_filter,
    CapabilityStore& store)
{
    LOG_DEBUG("AgentFactory: Creating default capability scanner for namespace: {}",
              namespace_filter);

    // Create concrete CapabilityScanner
    auto scanner = std::make_shared<capability::CapabilityScanner>(
        node, namespace_filter, store);

    // Wrap in adapter
    return std::make_unique<capability::CapabilityScannerAdapter>(scanner, store);
}

std::unique_ptr<interfaces::IActionExecutor> AgentFactory::create_default_executor(
    rclcpp::Node::SharedPtr node)
{
    LOG_DEBUG("AgentFactory: Creating default ROS2 action executor");

    return std::make_unique<executor::ROS2ActionExecutor>(node);
}

AgentComponents AgentFactory::create_default_components(
    rclcpp::Node::SharedPtr node,
    const AgentConfig& config,
    CapabilityStore& store)
{
    LOG_INFO("AgentFactory: Creating default components");

    AgentComponents components;

    // Create transport
    components.transport = create_default_transport(config);
    if (!components.transport) {
        LOG_ERROR("AgentFactory: Failed to create transport");
        return components;
    }

    // Create scanner
    components.scanner = create_default_scanner(
        node, config.ros_namespace, store);
    if (!components.scanner) {
        LOG_ERROR("AgentFactory: Failed to create scanner");
        return components;
    }

    // Create executor
    components.executor = create_default_executor(node);
    if (!components.executor) {
        LOG_ERROR("AgentFactory: Failed to create executor");
        return components;
    }

    LOG_INFO("AgentFactory: All default components created successfully");
    return components;
}

transport::QUICConfig AgentFactory::create_quic_config(const AgentConfig& config) {
    transport::QUICConfig quic_config;

    // Convert from AgentConfig::QUICConfig to transport::QUICConfig
    const auto& src = config.server.quic;

    quic_config.idle_timeout = std::chrono::milliseconds(src.idle_timeout_ms);
    quic_config.keepalive_interval = std::chrono::milliseconds(src.keepalive_interval_ms);
    quic_config.handshake_timeout = std::chrono::milliseconds(src.handshake_timeout_ms);

    quic_config.max_bidi_streams = src.max_bidi_streams;
    quic_config.max_uni_streams = src.max_uni_streams;

    quic_config.enable_resumption = src.enable_0rtt;
    quic_config.resumption_ticket_path = src.resumption_ticket_path;

    quic_config.enable_datagrams = src.enable_datagrams;
    quic_config.alpn = src.alpn;

    quic_config.congestion_control = static_cast<uint16_t>(src.congestion_control);

    return quic_config;
}

}  // namespace robot_agent::factory
