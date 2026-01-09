// Copyright 2026 Multi-Robot Supervision System
// YAML Configuration Loader Implementation

#include "fleet_agent/core/config_loader.hpp"

#include <cstdlib>
#include <fstream>
#include <regex>
#include <sstream>

#include <yaml-cpp/yaml.h>

namespace fleet_agent {

namespace {

// Helper to get optional string from YAML node
std::string get_string(const YAML::Node& node, const std::string& key,
                       const std::string& default_val = "") {
    if (node[key] && node[key].IsScalar()) {
        return expand_env_vars(node[key].as<std::string>());
    }
    return default_val;
}

// Helper to get optional int from YAML node
int get_int(const YAML::Node& node, const std::string& key, int default_val = 0) {
    if (node[key] && node[key].IsScalar()) {
        return node[key].as<int>();
    }
    return default_val;
}

// Helper to get optional float from YAML node
float get_float(const YAML::Node& node, const std::string& key, float default_val = 0.0f) {
    if (node[key] && node[key].IsScalar()) {
        return node[key].as<float>();
    }
    return default_val;
}

// Helper to get optional double from YAML node
double get_double(const YAML::Node& node, const std::string& key, double default_val = 0.0) {
    if (node[key] && node[key].IsScalar()) {
        return node[key].as<double>();
    }
    return default_val;
}

// Helper to get optional bool from YAML node
bool get_bool(const YAML::Node& node, const std::string& key, bool default_val = false) {
    if (node[key] && node[key].IsScalar()) {
        return node[key].as<bool>();
    }
    return default_val;
}

// Helper to get string vector from YAML node
std::vector<std::string> get_string_vector(const YAML::Node& node, const std::string& key) {
    std::vector<std::string> result;
    if (node[key] && node[key].IsSequence()) {
        for (const auto& item : node[key]) {
            result.push_back(expand_env_vars(item.as<std::string>()));
        }
    }
    return result;
}

// Parse TLS configuration
TlsConfig parse_tls_config(const YAML::Node& node) {
    TlsConfig config;
    if (!node) return config;

    config.enabled = get_bool(node, "enabled", false);
    config.ca_cert = get_string(node, "ca_cert");
    config.client_cert = get_string(node, "client_cert");
    config.client_key = get_string(node, "client_key");
    config.verify_server = get_bool(node, "verify_server", true);
    config.server_name_override = get_string(node, "server_name_override");

    return config;
}

// Parse robot configuration
RobotConfig parse_robot_config(const YAML::Node& node) {
    RobotConfig config;

    config.id = get_string(node, "id");
    config.ros_namespace = get_string(node, "ros_namespace", get_string(node, "namespace"));
    config.name = get_string(node, "name", config.id);
    config.tags = get_string_vector(node, "tags");
    config.enabled = get_bool(node, "enabled", true);

    return config;
}

// Parse QUIC configuration
QUICConfig parse_quic_config(const YAML::Node& node) {
    QUICConfig config;
    if (!node) return config;

    config.server_address = get_string(node, "server_address", config.server_address);
    config.server_port = static_cast<uint16_t>(get_int(node, "server_port", config.server_port));

    // TLS certificates
    config.ca_cert = get_string(node, "ca_cert", config.ca_cert);
    config.client_cert = get_string(node, "client_cert", config.client_cert);
    config.client_key = get_string(node, "client_key", config.client_key);

    // ALPN protocol identifier (must match server)
    config.alpn = get_string(node, "alpn", config.alpn);

    // Connection settings
    config.idle_timeout_ms = get_int(node, "idle_timeout_ms", config.idle_timeout_ms);
    config.keepalive_interval_ms = get_int(node, "keepalive_interval_ms", config.keepalive_interval_ms);
    config.handshake_timeout_ms = get_int(node, "handshake_timeout_ms", config.handshake_timeout_ms);

    // Stream settings
    config.max_bidi_streams = static_cast<uint16_t>(get_int(node, "max_bidi_streams", config.max_bidi_streams));
    config.max_uni_streams = static_cast<uint16_t>(get_int(node, "max_uni_streams", config.max_uni_streams));

    // Features
    config.enable_0rtt = get_bool(node, "enable_0rtt", config.enable_0rtt);
    config.enable_datagrams = get_bool(node, "enable_datagrams", config.enable_datagrams);
    config.resumption_ticket_path = get_string(node, "resumption_ticket_path", config.resumption_ticket_path);
    config.congestion_control = get_int(node, "congestion_control", config.congestion_control);

    return config;
}

// Parse server configuration
ServerConfig parse_server_config(const YAML::Node& node) {
    ServerConfig config;
    if (!node) return config;

    config.url = get_string(node, "url");
    config.timeout_sec = get_float(node, "timeout_sec", config.timeout_sec);
    config.quic = parse_quic_config(node["quic"]);

    return config;
}

// Parse communication configuration
CommunicationConfig parse_communication_config(const YAML::Node& node) {
    CommunicationConfig config;
    if (!node) return config;

    config.heartbeat_interval_ms = get_int(node, "heartbeat_interval_ms", config.heartbeat_interval_ms);
    config.command_timeout_sec = get_int(node, "command_timeout_sec", config.command_timeout_sec);
    config.inbound_queue_size = get_int(node, "inbound_queue_size", config.inbound_queue_size);
    config.quic_outbound_queue_size = get_int(node, "quic_outbound_queue_size", config.quic_outbound_queue_size);

    return config;
}

// Parse execution configuration
ExecutionConfig parse_execution_config(const YAML::Node& node, const YAML::Node& timeouts) {
    ExecutionConfig config;

    if (node) {
        config.action_default_timeout_sec = get_float(node, "action_default_timeout_sec", config.action_default_timeout_sec);
        config.precondition_check_interval_ms = get_int(node, "precondition_check_interval_ms", config.precondition_check_interval_ms);
        config.max_concurrent_per_robot = get_int(node, "max_concurrent_per_robot", config.max_concurrent_per_robot);
    }

    // Also check timeouts section
    if (timeouts) {
        config.action_default_timeout_sec = get_float(timeouts, "action_default_sec", config.action_default_timeout_sec);
    }

    return config;
}

// Parse storage configuration
StorageConfig parse_storage_config(const YAML::Node& node, const YAML::Node& paths) {
    StorageConfig config;

    if (node) {
        config.action_graphs_path = get_string(node, "action_graphs_path", config.action_graphs_path);
        config.state_definitions_path = get_string(node, "state_definitions_path", config.state_definitions_path);
        config.state_persistence_path = get_string(node, "state_persistence_path", config.state_persistence_path);
        config.message_queue_path = get_string(node, "message_queue_path", config.message_queue_path);
        config.enable_state_persistence = get_bool(node, "enable_state_persistence", config.enable_state_persistence);
        config.enable_message_persistence = get_bool(node, "enable_message_persistence", config.enable_message_persistence);
    }

    // Also check paths section
    if (paths) {
        config.action_graphs_path = get_string(paths, "action_graphs", config.action_graphs_path);
    }

    return config;
}

// Parse ROS configuration
RosConfig parse_ros_config(const YAML::Node& node) {
    RosConfig config;
    if (!node) return config;

    config.node_name = get_string(node, "node_name", config.node_name);
    config.use_intra_process = get_bool(node, "use_intra_process", config.use_intra_process);
    config.executor_thread_count = get_int(node, "executor_thread_count", config.executor_thread_count);

    return config;
}

// Parse logging configuration
LoggingConfig parse_logging_config(const YAML::Node& node) {
    LoggingConfig config;
    if (!node) return config;

    config.level = get_string(node, "level", config.level);
    config.file = get_string(node, "file");
    config.console = get_bool(node, "console", config.console);
    config.include_timestamp = get_bool(node, "include_timestamp", config.include_timestamp);
    config.max_file_size_mb = get_int(node, "max_file_size_mb", config.max_file_size_mb);
    config.max_files = get_int(node, "max_files", config.max_files);

    return config;
}

}  // namespace

std::string expand_env_vars(const std::string& value) {
    std::string result = value;
    static const std::regex env_regex(R"(\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\})");

    std::smatch match;
    while (std::regex_search(result, match, env_regex)) {
        std::string var_name = match[1].str();
        std::string default_value = match.size() > 2 ? match[2].str() : "";

        const char* env_value = std::getenv(var_name.c_str());
        std::string replacement = env_value ? env_value : default_value;

        result = result.replace(match.position(), match.length(), replacement);
    }

    return result;
}

void validate_config(const AgentConfig& config) {
    if (config.agent_id.empty()) {
        throw ConfigValidationError("agent.id is required");
    }

    // Robots section is optional - capabilities are discovered at agent level
    // If robots are configured, validate them
    for (const auto& robot : config.robots) {
        if (robot.id.empty()) {
            throw ConfigValidationError("Robot id is required");
        }
        if (robot.ros_namespace.empty()) {
            throw ConfigValidationError("Robot ros_namespace is required for robot: " + robot.id);
        }
    }

    // Validate QUIC config (required)
    if (config.server.quic.server_address.empty()) {
        throw ConfigValidationError("QUIC server_address is required");
    }
    if (config.server.quic.server_port == 0) {
        throw ConfigValidationError("QUIC server_port is required");
    }
}

void apply_defaults(AgentConfig& config) {
    // Generate default agent name if not set
    if (config.agent_name.empty()) {
        config.agent_name = "Fleet Agent " + config.agent_id;
    }

    // Apply robot defaults
    for (auto& robot : config.robots) {
        if (robot.name.empty()) {
            robot.name = robot.id;
        }
    }
}

AgentConfig load_config_from_string(const std::string& yaml_content) {
    YAML::Node root;
    try {
        root = YAML::Load(yaml_content);
    } catch (const YAML::Exception& e) {
        throw ConfigLoadError("Failed to parse YAML: " + std::string(e.what()));
    }

    AgentConfig config;

    // Parse agent section
    if (root["agent"]) {
        config.agent_id = get_string(root["agent"], "id");
        config.agent_name = get_string(root["agent"], "name");
    }

    // Parse robots section
    if (root["robots"] && root["robots"].IsSequence()) {
        for (const auto& robot_node : root["robots"]) {
            config.robots.push_back(parse_robot_config(robot_node));
        }
    }

    // Parse server section
    config.server = parse_server_config(root["server"]);

    // Parse communication section
    config.communication = parse_communication_config(root["communication"]);

    // Parse execution section (also check timeouts)
    config.execution = parse_execution_config(root["execution"], root["timeouts"]);

    // Parse storage section (also check paths)
    config.storage = parse_storage_config(root["storage"], root["paths"]);

    // Parse ROS section
    config.ros = parse_ros_config(root["ros"]);

    // Parse logging section
    config.logging = parse_logging_config(root["logging"]);

    // Apply defaults and validate
    apply_defaults(config);
    validate_config(config);

    return config;
}

AgentConfig load_config(const std::string& config_path) {
    std::ifstream file(config_path);
    if (!file.is_open()) {
        throw ConfigLoadError("Cannot open config file: " + config_path);
    }

    std::stringstream buffer;
    buffer << file.rdbuf();

    return load_config_from_string(buffer.str());
}

bool save_config(const AgentConfig& config, const std::string& config_path) {
    YAML::Emitter out;
    out << YAML::BeginMap;

    // Agent section
    out << YAML::Key << "agent" << YAML::Value << YAML::BeginMap;
    out << YAML::Key << "id" << YAML::Value << config.agent_id;
    out << YAML::Key << "name" << YAML::Value << config.agent_name;
    out << YAML::EndMap;

    // Robots section
    out << YAML::Key << "robots" << YAML::Value << YAML::BeginSeq;
    for (const auto& robot : config.robots) {
        out << YAML::BeginMap;
        out << YAML::Key << "id" << YAML::Value << robot.id;
        out << YAML::Key << "ros_namespace" << YAML::Value << robot.ros_namespace;
        out << YAML::Key << "name" << YAML::Value << robot.name;
        if (!robot.tags.empty()) {
            out << YAML::Key << "tags" << YAML::Value << YAML::Flow << robot.tags;
        }
        out << YAML::Key << "enabled" << YAML::Value << robot.enabled;
        out << YAML::EndMap;
    }
    out << YAML::EndSeq;

    // Server section
    out << YAML::Key << "server" << YAML::Value << YAML::BeginMap;
    out << YAML::Key << "url" << YAML::Value << config.server.url;
    out << YAML::Key << "timeout_sec" << YAML::Value << config.server.timeout_sec;

    // QUIC subsection
    out << YAML::Key << "quic" << YAML::Value << YAML::BeginMap;
    out << YAML::Key << "server_address" << YAML::Value << config.server.quic.server_address;
    out << YAML::Key << "server_port" << YAML::Value << config.server.quic.server_port;
    if (!config.server.quic.ca_cert.empty()) {
        out << YAML::Key << "ca_cert" << YAML::Value << config.server.quic.ca_cert;
    }
    if (!config.server.quic.client_cert.empty()) {
        out << YAML::Key << "client_cert" << YAML::Value << config.server.quic.client_cert;
    }
    if (!config.server.quic.client_key.empty()) {
        out << YAML::Key << "client_key" << YAML::Value << config.server.quic.client_key;
    }
    out << YAML::Key << "idle_timeout_ms" << YAML::Value << config.server.quic.idle_timeout_ms;
    out << YAML::Key << "keepalive_interval_ms" << YAML::Value << config.server.quic.keepalive_interval_ms;
    out << YAML::Key << "enable_0rtt" << YAML::Value << config.server.quic.enable_0rtt;
    out << YAML::Key << "enable_datagrams" << YAML::Value << config.server.quic.enable_datagrams;
    out << YAML::EndMap;

    out << YAML::EndMap;  // server

    // Paths section
    out << YAML::Key << "paths" << YAML::Value << YAML::BeginMap;
    out << YAML::Key << "action_graphs" << YAML::Value << config.storage.action_graphs_path;
    out << YAML::EndMap;

    // Timeouts section
    out << YAML::Key << "timeouts" << YAML::Value << YAML::BeginMap;
    out << YAML::Key << "action_default_sec" << YAML::Value << config.execution.action_default_timeout_sec;
    out << YAML::EndMap;

    // Communication section
    out << YAML::Key << "communication" << YAML::Value << YAML::BeginMap;
    out << YAML::Key << "heartbeat_interval_ms" << YAML::Value << config.communication.heartbeat_interval_ms;
    out << YAML::Key << "command_timeout_sec" << YAML::Value << config.communication.command_timeout_sec;
    out << YAML::Key << "inbound_queue_size" << YAML::Value << config.communication.inbound_queue_size;
    out << YAML::Key << "quic_outbound_queue_size" << YAML::Value << config.communication.quic_outbound_queue_size;
    out << YAML::EndMap;

    out << YAML::EndMap;  // root

    std::ofstream file(config_path);
    if (!file.is_open()) {
        return false;
    }

    file << out.c_str();
    return true;
}

std::string get_example_config() {
    return R"(# Fleet Agent Configuration
# Agent-based capability model: all visible action servers belong to this agent

agent:
  id: "agent_01"
  name: "Factory Agent"

server:
  url: "http://192.168.0.200:8081"

  quic:
    server_address: "192.168.0.200"
    server_port: 9443
    alpn: "fleet-agent-raw"
    ca_cert: "/etc/fleet_agent/pki/ca.crt"
    client_cert: "/etc/fleet_agent/pki/client.crt"
    client_key: "/etc/fleet_agent/pki/client.key"
    idle_timeout_ms: 30000
    keepalive_interval_ms: 10000
    enable_0rtt: true
    enable_datagrams: true

storage:
  action_graphs_path: "/var/lib/fleet_agent/graphs"

logging:
  level: "info"
  file: "/var/log/fleet_agent/agent.log"
  console: true

# Robots (optional) - declare robot IDs and namespaces for execution
# robots:
#   - id: "robot_001"
#     ros_namespace: "/robot_001"
#     name: "AMR Robot 1"
)";
}

}  // namespace fleet_agent
