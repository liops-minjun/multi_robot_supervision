// Copyright 2026 Multi-Robot Supervision System
// Configuration structures

#pragma once

#include <string>
#include <vector>

namespace fleet_agent {

// ============================================================
// Robot Configuration
// ============================================================

struct RobotConfig {
    std::string id;
    std::string ros_namespace;   // Required: e.g., "/robot_001"
    std::string name;            // Optional display name
    std::vector<std::string> tags;
    bool enabled{true};
};

// ============================================================
// TLS Configuration
// ============================================================

struct TlsConfig {
    bool enabled{false};
    std::string ca_cert;          // CA certificate path or PEM
    std::string client_cert;      // Client certificate path or PEM
    std::string client_key;       // Client key path or PEM
    bool verify_server{true};
    std::string server_name_override;  // For testing
};

// ============================================================
// QUIC Configuration
// ============================================================

struct QUICConfig {
    std::string server_address{"localhost"};
    uint16_t server_port{9444};

    // TLS certificates (required for QUIC)
    std::string ca_cert;
    std::string client_cert;
    std::string client_key;

    // ALPN protocol identifier (must match server: "fleet-agent-raw")
    std::string alpn{"fleet-agent-raw"};

    // Connection settings
    int idle_timeout_ms{30000};
    int keepalive_interval_ms{10000};
    int handshake_timeout_ms{10000};

    // Stream settings
    uint16_t max_bidi_streams{1000};
    uint16_t max_uni_streams{100};

    // Features
    bool enable_0rtt{true};
    bool enable_datagrams{true};
    std::string resumption_ticket_path{"/var/lib/fleet_agent/quic_ticket"};

    // Congestion control: 0=Cubic, 1=BBR
    int congestion_control{0};
};

// ============================================================
// Server Configuration
// ============================================================

struct ServerConfig {
    std::string url;              // HTTP API URL
    float timeout_sec{5.0f};
    QUICConfig quic;              // QUIC transport (required)
};

// ============================================================
// Communication Configuration
// ============================================================

struct CommunicationConfig {
    int heartbeat_interval_ms{1000};
    int command_timeout_sec{30};
    int inbound_queue_size{1024};
    int quic_outbound_queue_size{1024};
};

// ============================================================
// Execution Configuration
// ============================================================

struct ExecutionConfig {
    float action_default_timeout_sec{120.0f};
    int precondition_check_interval_ms{100};
    int max_concurrent_per_robot{1};
};

// ============================================================
// Storage Configuration
// ============================================================

struct StorageConfig {
    std::string action_graphs_path{"/var/lib/fleet_agent/graphs"};
    std::string state_definitions_path{"/var/lib/fleet_agent/state_definitions"};
    std::string state_persistence_path{"/var/lib/fleet_agent/state"};
    std::string message_queue_path{"/var/lib/fleet_agent/queue"};
    bool enable_state_persistence{true};
    bool enable_message_persistence{true};
};

// ============================================================
// ROS Configuration
// ============================================================

struct RosConfig {
    std::string node_name{"fleet_agent"};
    bool use_intra_process{true};
    int executor_thread_count{4};
};

// ============================================================
// Logging Configuration
// ============================================================

struct LoggingConfig {
    std::string level{"info"};    // debug, info, warn, error
    std::string file;             // Optional log file path
    bool console{true};
    bool include_timestamp{true};
    int max_file_size_mb{100};
    int max_files{5};
};

// ============================================================
// Main Agent Configuration
// ============================================================

struct AgentConfig {
    std::string agent_id;
    std::string agent_name{"Fleet Agent"};
    std::string ros_namespace;           // ROS namespace (e.g., "/robot_001")
    std::vector<std::string> tags;       // Agent tags

    // Deprecated: kept for config file backwards compatibility
    std::vector<RobotConfig> robots;
    ServerConfig server;
    CommunicationConfig communication;
    ExecutionConfig execution;
    StorageConfig storage;
    RosConfig ros;
    LoggingConfig logging;
};

}  // namespace fleet_agent
