// Copyright 2026 Multi-Robot Supervision System
// Main Agent Implementation

#include "fleet_agent/agent.hpp"
#include "fleet_agent/core/logger.hpp"

#include "fleet_agent/capability/scanner.hpp"
#include "fleet_agent/executor/command_processor.hpp"
#include "fleet_agent/graph/storage.hpp"
#include "fleet_agent/transport/tls_credentials.hpp"
#include "fleet_agent/transport/quic_transport.hpp"
#include "fleet_agent/transport/quic_outbound_sender.hpp"
#include "fleet_agent/state/state_definition.hpp"
#include "fleet_agent/state/state_tracker.hpp"
#include "fleet_agent/state/graph_state.hpp"
#include "fleet_agent/protocol/message_handler.hpp"

#include "fleet/v1/service.pb.h"
#include "fleet/v1/commands.pb.h"

#include <rclcpp/rclcpp.hpp>
#include <nlohmann/json.hpp>
#include <unistd.h>  // for getpid()
#include <algorithm>
#include <cctype>
#include <thread>
#include <chrono>
#include <cstdint>
#include <functional>

namespace fleet_agent {

namespace {
logging::ComponentLogger log("Agent");
constexpr size_t kMaxInboundMessageBytes = 16 * 1024 * 1024;

std::string state_to_string(Agent::State state) {
    switch (state) {
        case Agent::State::CREATED: return "CREATED";
        case Agent::State::INITIALIZING: return "INITIALIZING";
        case Agent::State::RUNNING: return "RUNNING";
        case Agent::State::STOPPING: return "STOPPING";
        case Agent::State::STOPPED: return "STOPPED";
        case Agent::State::ERROR: return "ERROR";
        default: return "UNKNOWN";
    }
}

class InboundMessageFramer {
public:
    using MessageCallback = std::function<void(const uint8_t*, size_t)>;

    explicit InboundMessageFramer(MessageCallback cb)
        : callback_(std::move(cb)) {}

    void on_data(const uint8_t* data, size_t len, bool fin) {
        if (data && len > 0) {
            buffer_.insert(buffer_.end(), data, data + len);
        }

        while (true) {
            if (expected_len_ == 0) {
                if (buffer_.size() < 4) {
                    break;
                }

                expected_len_ = (static_cast<uint32_t>(buffer_[0]) << 24) |
                                (static_cast<uint32_t>(buffer_[1]) << 16) |
                                (static_cast<uint32_t>(buffer_[2]) << 8) |
                                static_cast<uint32_t>(buffer_[3]);

                buffer_.erase(buffer_.begin(), buffer_.begin() + 4);

                if (expected_len_ == 0) {
                    continue;
                }

                if (expected_len_ > kMaxInboundMessageBytes) {
                    log.error("Inbound message too large: {} bytes", expected_len_);
                    buffer_.clear();
                    expected_len_ = 0;
                    break;
                }
            }

            if (buffer_.size() < expected_len_) {
                break;
            }

            if (callback_) {
                callback_(buffer_.data(), expected_len_);
            }

            buffer_.erase(buffer_.begin(), buffer_.begin() + expected_len_);
            expected_len_ = 0;
        }

        if (fin && (!buffer_.empty() || expected_len_ != 0)) {
            log.warn("Stream closed with partial message (buffer={}, expected={})",
                     buffer_.size(), expected_len_);
            buffer_.clear();
            expected_len_ = 0;
        }
    }

private:
    MessageCallback callback_;
    std::vector<uint8_t> buffer_;
    uint32_t expected_len_{0};
};
}  // namespace

Agent::Agent(const AgentConfig& config)
    : config_(config) {

    log.info("Creating agent: {}", config_.agent_id);
}

Agent::~Agent() {
    stop();
}

bool Agent::initialize() {
    State expected = State::CREATED;
    if (!state_.compare_exchange_strong(expected, State::INITIALIZING)) {
        log.error("Cannot initialize from state: {}", state_to_string(expected));
        return false;
    }

    log.info("Initializing agent...");

    // Initialize logger
    log.info("[1/7] Initializing logging...");
    logging::init(config_.logging.level, config_.logging.file);
    log.info("[1/7] Logging initialized");

    // Initialize components in order
    log.info("[2/7] Initializing ROS2...");
    if (!init_ros2()) {
        log.error("[2/7] ROS2 initialization FAILED");
        state_ = State::ERROR;
        return false;
    }
    log.info("[2/7] ROS2 initialized");

    log.info("[3/7] Initializing shared state...");
    if (!init_state()) {
        log.error("[3/7] State initialization FAILED");
        state_ = State::ERROR;
        return false;
    }
    log.info("[3/7] Shared state initialized");

    log.info("[4/7] Initializing transport (QUIC)...");
    if (!init_transport()) {
        log.error("[4/7] Transport initialization FAILED");
        state_ = State::ERROR;
        return false;
    }
    log.info("[4/7] Transport initialized");

    log.info("[5/7] Initializing state management...");
    if (!init_state_management()) {
        log.error("[5/7] State management initialization FAILED");
        state_ = State::ERROR;
        return false;
    }
    log.info("[5/7] State management initialized");

    log.info("[6/7] Initializing components...");
    if (!init_components()) {
        log.error("[6/7] Components initialization FAILED");
        state_ = State::ERROR;
        return false;
    }
    log.info("[6/7] Components initialized");

    log.info("[7/7] Initializing protocol handlers...");
    if (!init_protocol()) {
        log.error("[7/7] Protocol initialization FAILED");
        state_ = State::ERROR;
        return false;
    }
    log.info("[7/7] Protocol handlers initialized");

    setup_shutdown_handler();

    log.info("=== Agent initialization complete ===");
    return true;
}

bool Agent::start() {
    State expected = State::INITIALIZING;
    if (!state_.compare_exchange_strong(expected, State::RUNNING)) {
        log.error("Cannot start from state: {}", state_to_string(expected));
        return false;
    }

    log.info("Starting agent...");

    // Start ROS2 executor thread
    start_ros2();

    // Start transport (QUIC - optional)
    if (quic_client_) {
        if (quic_client_->is_connected()) {
            log.info("QUIC transport connected");
            // Register agent with central server via QUIC
            if (!register_with_server()) {
                log.warn("Agent QUIC registration failed - continuing anyway");
            }
        } else {
            log.warn("QUIC client not connected - running without QUIC transport");
            connection_lost_.store(true);
        }
    } else {
        log.info("Running without QUIC transport");
    }

    // Start QUIC outbound sender (must start before heartbeat sender)
    if (quic_outbound_sender_) {
        quic_outbound_sender_->start();
    }

    // Start heartbeat sender
    start_heartbeat_thread();

    // Start command processor
    if (command_processor_) {
        command_processor_->start();
    }

    // Discover capabilities for all robots
    discover_capabilities();

    // Start periodic capability scanning (every 1 second)
    start_capability_scan_thread();

    // Start auto-reconnection thread
    start_reconnect_thread();

    log.info("Agent started: {} robots managed",
             config_.robots.size());

    return true;
}

void Agent::stop() {
    State expected = State::RUNNING;
    if (!state_.compare_exchange_strong(expected, State::STOPPING)) {
        // Check if already stopped
        if (state_ == State::STOPPED || state_ == State::CREATED) {
            return;
        }
        log.warn("Stopping from unexpected state: {}", state_to_string(state_.load()));
    }

    log.info("Stopping agent...");

    // Stop auto-reconnection thread first
    stop_reconnect_thread();

    // Stop periodic capability scanning
    stop_capability_scan_thread();

    // Stop command processor
    if (command_processor_) {
        command_processor_->stop();
    }

    // Stop heartbeat sender
    stop_heartbeat_thread();

    // Stop QUIC outbound sender (after heartbeat sender stops queueing)
    if (quic_outbound_sender_) {
        quic_outbound_sender_->stop();
    }

    // Stop transport
    if (quic_client_) {
        quic_client_->disconnect();
    }

    // Stop ROS2 executor
    if (ros_executor_) {
        ros_executor_->cancel();
    }
    if (ros_thread_.joinable()) {
        ros_thread_.join();
    }

    state_ = State::STOPPED;
    log.info("Agent stopped");
}

int Agent::run() {
    if (!initialize()) {
        log.error("Initialization failed");
        return 1;
    }

    if (!start()) {
        log.error("Start failed");
        return 1;
    }

    // Wait for shutdown signal
    ShutdownHandler::instance().wait_for_shutdown();

    stop();
    return 0;
}

Agent::State Agent::get_state() const {
    return state_;
}

bool Agent::is_running() const {
    return state_ == State::RUNNING;
}

const std::string& Agent::agent_id() const {
    return config_.agent_id;
}

std::vector<std::string> Agent::agent_ids() const {
    std::vector<std::string> ids;
    ids.reserve(config_.robots.size());
    for (const auto& robot : config_.robots) {
        ids.push_back(robot.id);
    }
    return ids;
}

std::shared_ptr<rclcpp::Node> Agent::node() const {
    return node_;
}

bool Agent::init_ros2() {
    log.info("  [ROS2] Checking ROS2 context...");

    // Initialize ROS2 if not already
    if (!rclcpp::ok()) {
        log.info("  [ROS2] Calling rclcpp::init()...");
        rclcpp::init(0, nullptr);
    }
    log.info("  [ROS2] ROS2 context ready");

    // Create node
    log.info("  [ROS2] Creating node: fleet_agent_{}", config_.agent_id);
    rclcpp::NodeOptions options;
    options.use_intra_process_comms(true);
    node_ = std::make_shared<rclcpp::Node>(
        "fleet_agent_" + config_.agent_id, options);
    log.info("  [ROS2] Node created successfully");

    // Create executor
    log.info("  [ROS2] Creating MultiThreadedExecutor...");
    ros_executor_ = std::make_shared<rclcpp::executors::MultiThreadedExecutor>();
    ros_executor_->add_node(node_);
    log.info("  [ROS2] Executor ready with node attached");

    return true;
}

bool Agent::init_state() {
    log.debug("Initializing shared state...");

    capability_store_ = std::make_unique<CapabilityStore>();
    execution_contexts_ = std::make_unique<ExecutionContextMap>();
    inbound_queue_ = std::make_unique<InboundQueue>();
    quic_outbound_queue_ = std::make_unique<QuicOutboundQueue>();
    command_queue_ = std::make_unique<CommandQueue>();

    return true;
}

bool Agent::init_transport() {
    log.debug("Initializing transport...");

    // QUIC client configuration
    const auto& quic_cfg = config_.server.quic;
    transport::QUICConfig quic_config;
    quic_config.idle_timeout = std::chrono::milliseconds(quic_cfg.idle_timeout_ms);
    quic_config.keepalive_interval = std::chrono::milliseconds(quic_cfg.keepalive_interval_ms);
    quic_config.handshake_timeout = std::chrono::milliseconds(quic_cfg.handshake_timeout_ms);
    quic_config.enable_resumption = quic_cfg.enable_0rtt;
    quic_config.enable_datagrams = quic_cfg.enable_datagrams;
    quic_config.resumption_ticket_path = quic_cfg.resumption_ticket_path;
    quic_config.max_bidi_streams = quic_cfg.max_bidi_streams;
    quic_config.max_uni_streams = quic_cfg.max_uni_streams;
    quic_config.alpn = quic_cfg.alpn;  // ALPN must match Go server

    // QUIC is optional - try to initialize but don't fail if it doesn't work
    quic_client_ = std::make_unique<transport::QUICClient>(quic_config);
    quic_client_->set_connection_handler(
        [this](bool connected) {
            on_quic_connection_change(connected);
        });

    bool quic_ok = false;
    if (!quic_cfg.ca_cert.empty() && !quic_cfg.client_cert.empty()) {
        // Initialize with TLS certificates
        if (quic_client_->initialize(
                quic_cfg.ca_cert,
                quic_cfg.client_cert,
                quic_cfg.client_key)) {

            // Try to connect (non-blocking attempt)
            if (quic_client_->connect(quic_cfg.server_address, quic_cfg.server_port)) {
                log.info("QUIC transport connected to {}:{}",
                         quic_cfg.server_address, quic_cfg.server_port);
                quic_ok = true;
            } else {
                log.warn("QUIC connection failed - running without QUIC transport");
            }
        } else {
            log.warn("QUIC initialization failed - running without QUIC transport");
        }
    } else {
        log.info("QUIC certificates not configured - skipping QUIC transport");
    }

    // Initialize QUIC outbound sender (handles sending queued messages to server)
    if (quic_client_ && quic_outbound_queue_) {
        transport::QUICOutboundSender::Config sender_config;
        sender_config.poll_interval = std::chrono::milliseconds(10);
        sender_config.max_retries = 3;
        sender_config.retry_delay = std::chrono::milliseconds(100);

        quic_outbound_sender_ = std::make_unique<transport::QUICOutboundSender>(
            quic_client_.get(),
            *quic_outbound_queue_,
            config_.agent_id,
            sender_config);

        // Set connection callback for outbound sender
        quic_outbound_sender_->set_connection_callback(
            [this](bool connected) {
                on_quic_connection_change(connected);
            });

        // Notify sender if already connected
        if (quic_ok) {
            quic_outbound_sender_->notify_connected();
        }

        log.info("QUIC outbound sender initialized");
    }

    log.info("Transport initialized");
    return true;
}

bool Agent::init_components() {
    log.debug("Initializing components...");

    // Capability scanner - scan ALL action servers (no namespace filtering)
    // This allows discovery of any action server in the ROS2 network
    capability_scanner_ = std::make_unique<capability::CapabilityScanner>(
        node_, "", *capability_store_);

    // Graph storage
    graph_storage_ = std::make_unique<graph::GraphStorage>(
        config_.storage.action_graphs_path);

    // Command processor (with state tracker integration)
    command_processor_ = std::make_unique<executor::CommandProcessor>(
        node_,
        config_.agent_id,
        *inbound_queue_,
        *quic_outbound_queue_,
        *capability_store_,
        *execution_contexts_,
        *graph_storage_,
        state_tracker_mgr_.get());

    // Add robots to command processor
    // In 1:1 model, agent is the robot - use agent_id as robot_id
    if (config_.robots.empty()) {
        // 1:1 model: agent_id is the robot_id
        command_processor_->add_robot(config_.agent_id, "");
        log.info("1:1 model: registered agent {} as robot", config_.agent_id);
    } else {
        for (const auto& robot : config_.robots) {
            command_processor_->add_robot(robot.id, robot.ros_namespace);
        }
    }

    log.info("Components initialized");
    return true;
}

void Agent::start_ros2() {
    ros_thread_ = std::thread([this]() {
        log.debug("ROS2 executor thread started");
        ros_executor_->spin();
        log.debug("ROS2 executor thread stopped");
    });
}

void Agent::start_heartbeat_thread() {
    if (heartbeat_running_.load()) {
        return;
    }

    if (!quic_outbound_queue_) {
        log.warn("Heartbeat sender disabled: outbound queue not initialized");
        return;
    }

    heartbeat_running_.store(true);
    heartbeat_thread_ = std::thread([this]() {
        const auto interval = std::chrono::milliseconds(config_.communication.heartbeat_interval_ms);

        while (heartbeat_running_.load() && FLEET_AGENT_RUNNING) {
            auto loop_start = std::chrono::steady_clock::now();

            auto msg = std::make_shared<fleet::v1::AgentMessage>();
            msg->set_agent_id(config_.agent_id);
            msg->set_timestamp_ms(now_ms());

            auto* heartbeat = msg->mutable_heartbeat();
            heartbeat->set_agent_id(config_.agent_id);

            // Get execution state for this agent (1:1 model: agent_id = robot_id)
            bool is_executing = false;
            std::string current_action;
            std::string current_task_id;
            std::string current_step_id;

            if (execution_contexts_) {
                ExecutionContextMap::const_accessor acc;
                if (execution_contexts_->find(acc, config_.agent_id)) {
                    const auto exec_state = acc->second.state.load();
                    is_executing = (exec_state == RobotExecutionState::EXECUTING_ACTION ||
                                    exec_state == RobotExecutionState::WAITING_RESULT);
                    current_action = acc->second.current_action_type;
                    current_task_id = acc->second.current_task_id;
                    current_step_id = acc->second.current_step_id;
                }
            }

            // Get state from tracker
            std::string state_name = "idle";
            if (state_tracker_mgr_) {
                auto tracker = state_tracker_mgr_->get_tracker(config_.agent_id);
                if (tracker) {
                    state_name = tracker->current_state();
                }
            }

            log.debug("[Heartbeat] Agent {} state: {}, executing: {}",
                     config_.agent_id, state_name, is_executing);

            heartbeat->set_state(state_name);
            heartbeat->set_is_executing(is_executing);
            if (!current_action.empty()) {
                heartbeat->set_current_action(current_action);
            }
            if (!current_task_id.empty()) {
                heartbeat->set_current_task_id(current_task_id);
            }
            if (!current_step_id.empty()) {
                heartbeat->set_current_step_id(current_step_id);
            }

            OutboundMessage out;
            out.message = msg;
            out.created_at = std::chrono::steady_clock::now();
            out.priority = 0;
            quic_outbound_queue_->push(std::move(out));

            auto elapsed = std::chrono::steady_clock::now() - loop_start;
            auto sleep_time = interval - std::chrono::duration_cast<std::chrono::milliseconds>(elapsed);
            if (sleep_time > std::chrono::milliseconds(0)) {
                std::this_thread::sleep_for(sleep_time);
            }
        }
    });
}

void Agent::stop_heartbeat_thread() {
    if (!heartbeat_running_.load()) {
        return;
    }

    heartbeat_running_.store(false);
    if (heartbeat_thread_.joinable()) {
        heartbeat_thread_.join();
    }
}

void Agent::discover_capabilities() {
    log.info("Discovering capabilities for {} robots...", config_.robots.size());

    // Wait for ROS2 graph discovery to complete
    // DDS discovery typically takes 1-3 seconds
    log.info("Waiting for ROS2 graph discovery...");

    const int max_wait_sec = 5;
    const int poll_interval_ms = 500;
    int waited_ms = 0;

    while (waited_ms < max_wait_sec * 1000) {
        // Check if any other nodes are discovered
        auto node_names = node_->get_node_names();
        // Exclude our own node
        int other_nodes = 0;
        for (const auto& name : node_names) {
            if (name.find("fleet_agent") == std::string::npos) {
                other_nodes++;
            }
        }

        if (other_nodes > 0) {
            log.info("ROS2 graph discovery complete: found {} other nodes after {}ms",
                     other_nodes, waited_ms);
            break;
        }

        std::this_thread::sleep_for(std::chrono::milliseconds(poll_interval_ms));
        waited_ms += poll_interval_ms;
    }

    if (waited_ms >= max_wait_sec * 1000) {
        log.warn("ROS2 graph discovery timeout after {}s, proceeding anyway", max_wait_sec);
    }

    // Debug: Print what the node can see
    log.info("=== DEBUG: ROS2 Graph State ===");

    // List all nodes
    auto all_nodes = node_->get_node_names();
    log.info("Visible nodes ({}):", all_nodes.size());
    for (const auto& n : all_nodes) {
        log.info("  - {}", n);
    }

    // List all topics
    auto all_topics = node_->get_topic_names_and_types();
    log.info("Visible topics ({}):", all_topics.size());
    for (const auto& [topic, types] : all_topics) {
        for (const auto& t : types) {
            log.info("  - {} [{}]", topic, t);
        }
    }

    // List all services (including hidden)
    auto all_services = node_->get_service_names_and_types();
    log.info("Visible services ({}):", all_services.size());
    int action_services = 0;
    for (const auto& [service, types] : all_services) {
        if (service.find("_action") != std::string::npos) {
            action_services++;
            for (const auto& t : types) {
                log.info("  - {} [{}]", service, t);
            }
        }
    }
    log.info("Action-related services: {}", action_services);

    log.info("=== END DEBUG ===");

    robot_capabilities_.clear();

    // Scan all action servers visible to this agent
    int discovered = capability_scanner_->scan_all();
    log.info("Discovered {} total capabilities", discovered);

    // Get all discovered capabilities (no namespace filtering - agent-based)
    auto all_caps = capability_scanner_->get_all();
    auto available_caps = capability_scanner_->get_for_registration();

    // Store capabilities for local use (keyed by first robot for backwards compatibility)
    if (!config_.robots.empty()) {
        robot_capabilities_[config_.robots[0].id] = all_caps;
    }

    log.info("Agent {} has {} capabilities (available: {})",
             config_.agent_id, all_caps.size(), available_caps.size());

    // Register ALL capabilities under agent_id (not per-robot)
    if (capability_registrar_ && quic_client_ && quic_client_->is_connected()) {
        // Use agent_id for registration (server will store in agent_capabilities table)
        if (capability_registrar_->register_capabilities(config_.agent_id, available_caps)) {
            log.info("Registered {} capabilities for agent {} via QUIC",
                     available_caps.size(), config_.agent_id);
        } else {
            log.warn("Failed to register capabilities for agent {} via QUIC",
                     config_.agent_id);
        }
    }
}

bool Agent::register_with_server() {
    if (!quic_client_ || !quic_client_->is_connected()) {
        log.warn("QUIC not connected - skipping registration");
        return false;
    }

    log.info("Registering agent with central server via QUIC");

    // Build RegisterAgentRequest protobuf message (1:1 model: agent = robot)
    fleet::v1::RegisterAgentRequest request;
    request.set_agent_id(config_.agent_id);
    request.set_name(config_.agent_name);
    request.set_client_version("1.0.0");

    // Set namespace (use agent's ros_namespace or fallback to first robot's namespace)
    std::string ns = config_.ros_namespace;
    if (ns.empty() && !config_.robots.empty()) {
        ns = config_.robots[0].ros_namespace;
    }
    if (!ns.empty()) {
        request.set_namespace_(ns);
    }

    // Serialize to bytes
    std::string serialized;
    if (!request.SerializeToString(&serialized)) {
        log.error("Failed to serialize RegisterAgentRequest");
        return false;
    }

    // Build length-prefixed message (4-byte big-endian length + data)
    std::vector<uint8_t> data;
    data.reserve(4 + serialized.size());

    uint32_t len = static_cast<uint32_t>(serialized.size());
    data.push_back(static_cast<uint8_t>((len >> 24) & 0xFF));
    data.push_back(static_cast<uint8_t>((len >> 16) & 0xFF));
    data.push_back(static_cast<uint8_t>((len >> 8) & 0xFF));
    data.push_back(static_cast<uint8_t>(len & 0xFF));
    data.insert(data.end(), serialized.begin(), serialized.end());

    // Get a QUIC stream
    auto stream = quic_client_->get_stream();
    if (!stream) {
        log.error("Failed to get QUIC stream for registration");
        return false;
    }

    // Send via stream
    bool success = stream->write(data.data(), data.size(), false);
    if (success) {
        quic_client_->release_stream(stream);
    } else {
        stream->close();
    }

    if (!success) {
        log.error("Failed to send RegisterAgentRequest via QUIC stream");
        return false;
    }

    log.info("Agent registration sent via QUIC: {} with {} robots",
             config_.agent_id, config_.robots.size());
    return true;
}

void Agent::on_server_message(const fleet::v1::ServerMessage& message) {
    log.debug("Received server message");

    // Use MessageHandler for all message types
    if (message_handler_) {
        auto result = message_handler_->handle(message);
        if (!result.success) {
            log.error("Failed to handle server message: {}", result.error);
        }
        return;
    }

    // Fallback to direct handling if MessageHandler not initialized
    // Route to command processor via inbound queue
    if (message.has_execute()) {
        InboundCommand inbound;
        inbound.message_id = message.message_id();
        inbound.received_at = std::chrono::steady_clock::now();
        inbound.message = std::make_shared<fleet::v1::ServerMessage>(message);
        inbound_queue_->push(std::move(inbound));
    } else if (message.has_deploy_graph()) {
        // Handle graph deployment
        const auto& deploy = message.deploy_graph();
        if (graph_storage_->store(deploy.graph())) {
            log.info("Stored action graph: {}", deploy.graph().metadata().id());
        } else {
            log.error("Failed to store action graph");
        }
    }
}

void Agent::on_quic_connection_change(bool connected) {
    if (connected) {
        log.info("QUIC connected to central server");
        // Clear connection lost flag
        connection_lost_.store(false);

        // Notify outbound sender that connection is available
        if (quic_outbound_sender_) {
            quic_outbound_sender_->notify_connected();
        }

        // Avoid heavy work in MsQuic callback thread; reconnect loop will resync.
    } else {
        log.warn("QUIC disconnected from central server");

        // Notify outbound sender that connection is lost
        if (quic_outbound_sender_) {
            quic_outbound_sender_->notify_disconnected();
        }

        // Signal reconnection thread
        if (state_ == State::RUNNING) {
            connection_lost_.store(true);
            reconnect_cv_.notify_one();
        }
    }
}

void Agent::setup_quic_stream_handler() {
    if (!quic_client_) {
        return;
    }

    auto* conn = quic_client_->connection();
    if (!conn) {
        log.debug("QUIC connection not ready for stream handler");
        return;
    }

    conn->set_stream_callback([this](std::shared_ptr<transport::QUICStream> stream) {
        if (!stream) {
            return;
        }

        const auto stream_id = stream->id();
        log.debug("Accepted QUIC stream {}", stream_id);

        auto framer = std::make_shared<InboundMessageFramer>(
            [this, stream_id](const uint8_t* data, size_t len) {
                fleet::v1::ServerMessage message;
                if (!message.ParseFromArray(data, static_cast<int>(len))) {
                    log.error("Failed to parse ServerMessage from stream {}", stream_id);
                    return;
                }
                on_server_message(message);
            });

        stream->set_data_callback([framer](const uint8_t* data, size_t len, bool fin) {
            framer->on_data(data, len, fin);
        });

        stream->set_close_callback([stream_id](uint64_t error_code) {
            if (error_code != 0) {
                log.warn("QUIC stream {} closed with error {}", stream_id, error_code);
            } else {
                log.debug("QUIC stream {} closed", stream_id);
            }
        });
    });
}

void Agent::setup_shutdown_handler() {
    // ShutdownHandler is a singleton - just register our callback
    ShutdownHandler::instance().register_callback([this]() {
        log.info("Shutdown signal received");
        stop();
    });
}

bool Agent::init_state_management() {
    log.debug("Initializing state management...");

    // Create state definition storage
    state_storage_ = std::make_unique<state::StateDefinitionStorage>(
        config_.storage.state_definitions_path);

    // Create state tracker manager
    state_tracker_mgr_ = std::make_unique<state::StateTrackerManager>();

    // Set global state change callback for logging
    state_tracker_mgr_->set_state_change_callback(
        [this](const std::string& agent_id,
               const std::string& old_state,
               const std::string& new_state) {
            log.debug("Robot {} state changed: {} -> {}", agent_id, old_state, new_state);
        });

    // Initialize state tracker for this agent
    auto tracker = state_tracker_mgr_->get_tracker(config_.agent_id);

    // Try to load existing state definition for this agent
    auto state_def = state_storage_->get_for_agent(config_.agent_id);
    if (state_def) {
        tracker->configure(*state_def);
        log.info("Loaded state definition for agent {}: {} (version {})",
                 config_.agent_id, state_def->id, state_def->version);
    } else {
        log.debug("No state definition found for agent {}, using defaults",
                  config_.agent_id);
    }

    // Create fleet state cache for cross-agent coordination
    fleet_state_cache_ = std::make_unique<state::FleetStateCache>();
    log.debug("Fleet state cache initialized for cross-agent coordination");

    log.info("State management initialized");
    return true;
}

bool Agent::init_protocol() {
    log.debug("Initializing protocol handlers...");

    // Create message handler with dependencies
    protocol::MessageHandler::Dependencies deps;
    deps.quic_client = quic_client_.get();
    deps.state_storage = state_storage_.get();
    deps.state_tracker_mgr = state_tracker_mgr_.get();
    deps.fleet_state_cache = fleet_state_cache_.get();
    deps.graph_storage = graph_storage_.get();
    deps.command_processor = command_processor_.get();
    deps.command_queue = command_queue_.get();
    deps.quic_outbound_queue = quic_outbound_queue_.get();
    deps.agent_id = config_.agent_id;

    message_handler_ = std::make_unique<protocol::MessageHandler>(deps);

    // Create capability registrar
    capability_registrar_ = std::make_unique<protocol::CapabilityRegistrar>(
        quic_client_.get(), config_.agent_id);

    // Configure inbound QUIC stream handling for ServerMessage frames
    setup_quic_stream_handler();

    log.info("Protocol handlers initialized");
    return true;
}

void Agent::start_capability_scan_thread() {
    if (capability_scan_running_.load()) {
        log.warn("Capability scan thread already running");
        return;
    }

    capability_scan_running_.store(true);
    capability_scan_thread_ = std::thread([this]() {
        log.info("Capability scan thread started (1 second interval)");

        while (capability_scan_running_.load()) {
            // Sleep for 1 second
            std::this_thread::sleep_for(std::chrono::seconds(1));

            if (!capability_scan_running_.load()) {
                break;
            }

            // Refresh capability list
            int changes = capability_scanner_->refresh();

            if (changes > 0) {
                log.info("Detected {} capability changes, re-registering...", changes);
                on_capabilities_changed();
            }
        }

        log.info("Capability scan thread stopped");
    });
}

void Agent::stop_capability_scan_thread() {
    if (!capability_scan_running_.load()) {
        return;
    }

    log.debug("Stopping capability scan thread...");
    capability_scan_running_.store(false);

    if (capability_scan_thread_.joinable()) {
        capability_scan_thread_.join();
    }

    log.debug("Capability scan thread stopped");
}

void Agent::on_capabilities_changed() {
    // Get updated capability list
    auto all_caps = capability_scanner_->get_all();
    auto available_caps = capability_scanner_->get_for_registration();

    // Update robot_capabilities_ map (for local use)
    if (!config_.robots.empty()) {
        robot_capabilities_[config_.robots[0].id] = all_caps;
    }

    log.info("Agent {} capabilities changed: {} total (available: {})",
             config_.agent_id, all_caps.size(), available_caps.size());

    // Re-register capabilities with server via QUIC (agent-based)
    if (capability_registrar_ && quic_client_ && quic_client_->is_connected()) {
        if (capability_registrar_->register_capabilities(config_.agent_id, available_caps)) {
            log.info("Re-registered {} capabilities for agent {} via QUIC",
                     available_caps.size(), config_.agent_id);
        } else {
            log.warn("Failed to re-register capabilities for agent {} via QUIC",
                     config_.agent_id);
        }
    } else {
        log.warn("QUIC not connected - cannot re-register capabilities");
    }
}

void Agent::resync_capabilities() {
    if (!capability_registrar_ || !capability_scanner_) {
        log.warn("Capability registrar or scanner not ready - skipping resync");
        return;
    }

    if (!quic_client_ || !quic_client_->is_connected()) {
        log.warn("QUIC not connected - skipping capability resync");
        return;
    }

    auto available_caps = capability_scanner_->get_for_registration();
    log.info("Resyncing {} capabilities after reconnect", available_caps.size());

    if (!capability_registrar_->register_capabilities(config_.agent_id, available_caps)) {
        log.warn("Capability resync failed for agent {}", config_.agent_id);
    }
}

void Agent::start_reconnect_thread() {
    if (reconnect_running_.load()) {
        log.warn("Reconnect thread already running");
        return;
    }

    reconnect_running_.store(true);
    reconnect_thread_ = std::thread([this]() {
        reconnect_loop();
    });
    log.info("Auto-reconnection thread started");
}

void Agent::stop_reconnect_thread() {
    if (!reconnect_running_.load()) {
        return;
    }

    log.debug("Stopping reconnect thread...");
    reconnect_running_.store(false);

    // Wake up the thread if it's waiting
    {
        std::lock_guard<std::mutex> lock(reconnect_mutex_);
        connection_lost_.store(true);
    }
    reconnect_cv_.notify_one();

    if (reconnect_thread_.joinable()) {
        reconnect_thread_.join();
    }
    log.debug("Reconnect thread stopped");
}

void Agent::reconnect_loop() {
    log.info("Reconnect loop started");

    // Exponential backoff parameters
    const int initial_delay_ms = 1000;    // Start with 1 second
    const int max_delay_ms = 30000;       // Max 30 seconds
    const double backoff_multiplier = 2.0;

    int current_delay_ms = initial_delay_ms;

    while (reconnect_running_.load()) {
        // Wait for connection loss signal or periodic check
        {
            std::unique_lock<std::mutex> lock(reconnect_mutex_);
            // Wait with timeout to allow periodic checking
            reconnect_cv_.wait_for(lock, std::chrono::seconds(5), [this]() {
                return connection_lost_.load() || !reconnect_running_.load();
            });
        }

        // Check if we should stop
        if (!reconnect_running_.load()) {
            break;
        }

        // Skip if QUIC client doesn't exist
        if (!quic_client_) {
            log.debug("No QUIC client - skipping reconnection");
            continue;
        }

        if (!connection_lost_.load() && !quic_client_->is_connected()) {
            connection_lost_.store(true);
        }

        // Check if reconnection is needed
        if (!connection_lost_.load()) {
            continue;
        }

        // Already connected?
        if (quic_client_->is_connected()) {
            connection_lost_.store(false);
            current_delay_ms = initial_delay_ms;  // Reset backoff
            continue;
        }

        // Attempt reconnection
        log.info("Attempting reconnection (backoff: {}ms)...", current_delay_ms);

        if (attempt_reconnect()) {
            log.info("Reconnection successful!");
            connection_lost_.store(false);
            current_delay_ms = initial_delay_ms;  // Reset backoff on success

            // Re-register with server
            if (register_with_server()) {
                log.info("Re-registered with server after reconnection");
                resync_capabilities();
            }
        } else {
            log.warn("Reconnection failed, next attempt in {}ms", current_delay_ms);

            // Wait with exponential backoff before next attempt
            {
                std::unique_lock<std::mutex> lock(reconnect_mutex_);
                reconnect_cv_.wait_for(lock, std::chrono::milliseconds(current_delay_ms), [this]() {
                    return !reconnect_running_.load();
                });
            }

            // Increase delay with exponential backoff
            current_delay_ms = std::min(
                static_cast<int>(current_delay_ms * backoff_multiplier),
                max_delay_ms
            );
        }
    }

    log.info("Reconnect loop stopped");
}

bool Agent::attempt_reconnect() {
    if (!quic_client_) {
        return false;
    }

    const auto& quic_cfg = config_.server.quic;

    // Disconnect any existing connection first
    quic_client_->disconnect();

    // Small delay to ensure clean disconnect
    std::this_thread::sleep_for(std::chrono::milliseconds(100));

    // Try to connect
    if (quic_client_->connect(quic_cfg.server_address, quic_cfg.server_port)) {
        setup_quic_stream_handler();

        log.info("QUIC reconnected to {}:{}", quic_cfg.server_address, quic_cfg.server_port);
        return true;
    }

    return false;
}

}  // namespace fleet_agent
