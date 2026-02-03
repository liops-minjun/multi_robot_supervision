// Copyright 2026 Multi-Robot Supervision System
// Robot Agent - Tick-based implementation

#include "robot_agent/agent.hpp"
#include "robot_agent/core/logger.hpp"

#include "robot_agent/capability/scanner.hpp"
#include "robot_agent/graph/storage.hpp"
#include "robot_agent/transport/quic_transport.hpp"
#include "robot_agent/state/state_definition.hpp"
#include "robot_agent/state/state_tracker.hpp"
#include "robot_agent/state/graph_state.hpp"
#include "robot_agent/telemetry/collector.hpp"
#include "robot_agent/telemetry/snapshot.hpp"

#include "fleet/v1/service.pb.h"
#include "fleet/v1/common.pb.h"
#include "fleet/v1/commands.pb.h"
#include "fleet/v1/graphs.pb.h"

#include <nlohmann/json.hpp>

namespace robot_agent {

namespace {
logging::ComponentLogger log("Agent");
constexpr size_t kMaxMessageBytes = 16 * 1024 * 1024;
}

// ============================================================
// Constructor / Destructor
// ============================================================

Agent::Agent(const AgentConfig& config) : config_(config) {
    agent_id_storage_ = std::make_unique<AgentIdStorage>(config_.storage.agent_id_path);
    hardware_fingerprint_ = AgentIdStorage::generate_hardware_fingerprint();

    // Resolve effective agent ID
    if (!config_.agent_id.empty()) {
        effective_agent_id_ = config_.agent_id;
    } else if (config_.use_server_assigned_id) {
        if (auto stored = agent_id_storage_->load()) {
            effective_agent_id_ = *stored;
        } else {
            effective_agent_id_ = "agent_" + hardware_fingerprint_.substr(0, 8);
        }
    }

    log.info("Agent created: {} (fingerprint: {})", effective_agent_id_, hardware_fingerprint_);
}

Agent::Agent(const AgentConfig& config,
             std::unique_ptr<interfaces::ITransport> transport,
             std::unique_ptr<interfaces::ICapabilityScanner> scanner,
             std::unique_ptr<interfaces::IActionExecutor> executor)
    : config_(config)
    , transport_interface_(std::move(transport))
    , scanner_interface_(std::move(scanner))
    , executor_interface_(std::move(executor))
    , using_injected_components_(true)
{
    agent_id_storage_ = std::make_unique<AgentIdStorage>(config_.storage.agent_id_path);
    hardware_fingerprint_ = AgentIdStorage::generate_hardware_fingerprint();

    // Resolve effective agent ID
    if (!config_.agent_id.empty()) {
        effective_agent_id_ = config_.agent_id;
    } else if (config_.use_server_assigned_id) {
        if (auto stored = agent_id_storage_->load()) {
            effective_agent_id_ = *stored;
        } else {
            effective_agent_id_ = "agent_" + hardware_fingerprint_.substr(0, 8);
        }
    }

    log.info("Agent created (DI): {} (fingerprint: {})", effective_agent_id_, hardware_fingerprint_);
    log.info("  Using injected components: transport={}, scanner={}, executor={}",
             transport_interface_ ? "yes" : "no",
             scanner_interface_ ? "yes" : "no",
             executor_interface_ ? "yes" : "no");
}

Agent::~Agent() {
    stop();
}

// ============================================================
// Lifecycle
// ============================================================

bool Agent::initialize() {
    State expected = State::CREATED;
    if (!state_.compare_exchange_strong(expected, State::INITIALIZING)) {
        log.error("Cannot initialize from state");
        return false;
    }

    log.info("Initializing agent...");

    if (!init_ros2()) return false;
    if (!init_transport()) return false;
    if (!init_state_management()) return false;
    if (!init_components()) return false;
    if (!init_telemetry()) return false;

    setup_shutdown_handler();

    log.info("Agent initialization complete");
    return true;
}

bool Agent::start() {
    State expected = State::INITIALIZING;
    if (!state_.compare_exchange_strong(expected, State::RUNNING)) {
        log.error("Cannot start from current state");
        return false;
    }

    log.info("Starting agent...");

    // Start telemetry collectors
    for (auto& collector : telemetry_collectors_) {
        collector->start();
    }

    // Initial connection attempt
    if (quic_client_) {
        log.info("Attempting initial QUIC connection...");
        if (try_connect()) {
            log.info("QUIC connection successful, connected_={}", connected_.load());
            register_with_server();
        } else {
            log.warn("Initial QUIC connection failed, will retry in slow_tick");
        }
    } else {
        log.error("QUIC client not initialized!");
    }

    // Discover capabilities
    discover_capabilities();

    // Create timers (ALL processing happens here)
    main_timer_ = node_->create_wall_timer(
        std::chrono::milliseconds(10),
        [this]() { tick(); });

    heartbeat_timer_ = node_->create_wall_timer(
        std::chrono::milliseconds(config_.communication.heartbeat_interval_ms),
        [this]() { send_heartbeat(); });

    slow_timer_ = node_->create_wall_timer(
        std::chrono::milliseconds(1000),
        [this]() { slow_tick(); });

    // Sender thread disabled - using direct flush in tick() instead
    // start_sender_thread();

    log.info("Agent started (tick-based architecture with direct flush)");
    log.info("State summary: connected={} quic_client={}",
             connected_.load(), quic_client_ != nullptr);
    return true;
}

void Agent::stop() {
    State expected = State::RUNNING;
    if (!state_.compare_exchange_strong(expected, State::STOPPING)) {
        if (state_ == State::STOPPED || state_ == State::CREATED) return;
    }

    log.info("Stopping agent...");

    // Cancel timers
    if (main_timer_) main_timer_->cancel();
    if (heartbeat_timer_) heartbeat_timer_->cancel();
    if (slow_timer_) slow_timer_->cancel();

    // Sender thread disabled
    // stop_sender_thread();

    // Stop telemetry
    for (auto& collector : telemetry_collectors_) {
        collector->stop();
    }

    // Disconnect QUIC
    if (quic_client_) {
        quic_client_->disconnect();
    }

    state_ = State::STOPPED;
    log.info("Agent stopped");
}

int Agent::run() {
    if (!initialize()) return 1;
    if (!start()) return 1;

    // Blocking spin
    executor_->spin();

    stop();
    return 0;
}

// ============================================================
// Initialization
// ============================================================

bool Agent::init_ros2() {
    log.info("Initializing ROS2...");

    if (!rclcpp::ok()) {
        rclcpp::init(0, nullptr);
    }

    rclcpp::NodeOptions options;
    options.use_intra_process_comms(true);
    node_ = std::make_shared<rclcpp::Node>("robot_agent_" + config_.agent_id, options);

    // Use MultiThreadedExecutor for parallel subscriber processing
    executor_ = std::make_unique<rclcpp::executors::MultiThreadedExecutor>();
    executor_->add_node(node_);

    log.info("ROS2 initialized (MultiThreadedExecutor)");
    return true;
}

bool Agent::init_transport() {
    log.info("Initializing QUIC transport...");

    const auto& cfg = config_.server.quic;
    if (cfg.ca_cert.empty()) {
        log.info("QUIC certificates not configured - skipping");
        return true;
    }

    transport::QUICConfig quic_cfg;
    quic_cfg.idle_timeout = std::chrono::milliseconds(cfg.idle_timeout_ms);
    quic_cfg.keepalive_interval = std::chrono::milliseconds(cfg.keepalive_interval_ms);
    quic_cfg.enable_resumption = cfg.enable_0rtt;
    quic_cfg.alpn = cfg.alpn;

    quic_client_ = std::make_unique<transport::QUICClient>(quic_cfg);
    quic_client_->set_connection_handler([this](bool c) { on_connection_change(c); });

    if (!quic_client_->initialize(cfg.ca_cert, cfg.client_cert, cfg.client_key)) {
        log.warn("QUIC initialization failed");
        quic_client_.reset();
        return true;  // Continue without QUIC
    }

    log.info("QUIC transport initialized");
    return true;
}

bool Agent::init_state_management() {
    log.info("Initializing state management...");

    state_storage_ = std::make_unique<state::StateDefinitionStorage>(
        config_.storage.state_definitions_path);
    state_tracker_mgr_ = std::make_unique<state::StateTrackerManager>();
    fleet_state_cache_ = std::make_unique<state::FleetStateCache>();

    // Initialize tracker for this agent
    auto tracker = state_tracker_mgr_->get_tracker(effective_agent_id_);
    if (auto def = state_storage_->get_for_agent(effective_agent_id_)) {
        tracker->configure(*def);
    }

    return true;
}

bool Agent::init_components() {
    log.info("Initializing components...");

    capability_store_ = std::make_unique<CapabilityStore>();
    capability_scanner_ = std::make_unique<capability::CapabilityScanner>(
        node_, "", *capability_store_);

    graph_storage_ = std::make_unique<graph::GraphStorage>(
        config_.storage.behavior_trees_path);

    return true;
}

bool Agent::init_telemetry() {
    log.info("Initializing telemetry...");

    telemetry_store_ = std::make_unique<telemetry::TelemetryStore>();

    TelemetryConfig tel_cfg;
    tel_cfg.pose_rate_hz = 10.0;
    tel_cfg.joint_state_rate_hz = 10.0;
    tel_cfg.subscribe_tf = true;

    // Create collector (1:1 model - agent is the robot)
    std::string ns = config_.ros_namespace;
    if (ns.empty() && !config_.robots.empty()) {
        ns = config_.robots[0].ros_namespace;
    }

    auto collector = std::make_unique<telemetry::RobotTelemetryCollector>(
        node_, effective_agent_id_, ns, tel_cfg, *telemetry_store_);
    telemetry_collectors_.push_back(std::move(collector));

    return true;
}

void Agent::setup_shutdown_handler() {
    ShutdownHandler::instance().register_callback([this]() {
        log.info("Shutdown signal received");
        if (executor_) executor_->cancel();
    });
}

// ============================================================
// Timer Callbacks
// ============================================================

void Agent::tick() {
    static int tick_count = 0;
    tick_count++;
    CLOG_INFO_THROTTLE(log, 30.0, "[tick] count={} connected={} queue_size={}",
                       tick_count, connected_.load(), outbound_queue_.unsafe_size());

    // 1. Poll QUIC for incoming data
    poll_quic_receive();

    // 2. Poll all action clients for responses
    poll_all_clients();

    // 3. Process current task (if any)
    process_task();

    // 4. Flush outbound messages directly (sender_thread disabled for debugging)
    flush_outbound();
}

void Agent::send_heartbeat() {
    auto msg = std::make_shared<fleet::v1::AgentMessage>();
    msg->set_agent_id(effective_agent_id_);
    msg->set_timestamp_ms(now_ms());

    auto* hb = msg->mutable_heartbeat();
    hb->set_agent_id(effective_agent_id_);

    // Get state from tracker
    std::string state_name = "idle";
    if (state_tracker_mgr_) {
        if (auto tracker = state_tracker_mgr_->get_tracker(effective_agent_id_)) {
            state_name = tracker->current_state();
        }
    }

    hb->set_state(state_name);

    // Read exec_state atomically (lock-free)
    bool is_exec = exec_state_.is_executing.load(std::memory_order_acquire);
    auto action_type = std::atomic_load(&exec_state_.action_type);
    auto task_id = std::atomic_load(&exec_state_.task_id);
    auto step_id = std::atomic_load(&exec_state_.step_id);

    hb->set_is_executing(is_exec);
    if (!action_type->empty()) hb->set_current_action(*action_type);
    if (!task_id->empty()) hb->set_current_task_id(*task_id);
    if (!step_id->empty()) hb->set_current_step_id(*step_id);

    // Add telemetry
    bool has_tel = false;
    if (telemetry_store_) {
        auto snapshot = telemetry_store_->get(effective_agent_id_);
        if (snapshot && snapshot->has_data()) {
            has_tel = true;
            auto* tel = hb->mutable_telemetry();
            tel->set_robot_id(effective_agent_id_);

            if (snapshot->joint_state) {
                auto* js = tel->mutable_joint_state();
                for (const auto& n : snapshot->joint_state->name) js->add_name(n);
                for (auto p : snapshot->joint_state->position) js->add_position(p);
                for (auto v : snapshot->joint_state->velocity) js->add_velocity(v);
                for (auto e : snapshot->joint_state->effort) js->add_effort(e);
            }

            if (snapshot->odometry) {
                auto* od = tel->mutable_odometry();
                od->set_frame_id(snapshot->odometry->header.frame_id);
                od->set_child_frame_id(snapshot->odometry->child_frame_id);
                od->set_topic_name(snapshot->odometry_topic);

                // Pose
                auto* pose = od->mutable_pose();
                pose->mutable_position()->set_x(snapshot->odometry->pose.pose.position.x);
                pose->mutable_position()->set_y(snapshot->odometry->pose.pose.position.y);
                pose->mutable_position()->set_z(snapshot->odometry->pose.pose.position.z);
                pose->mutable_orientation()->set_x(snapshot->odometry->pose.pose.orientation.x);
                pose->mutable_orientation()->set_y(snapshot->odometry->pose.pose.orientation.y);
                pose->mutable_orientation()->set_z(snapshot->odometry->pose.pose.orientation.z);
                pose->mutable_orientation()->set_w(snapshot->odometry->pose.pose.orientation.w);

                // Twist (velocity)
                auto* twist = od->mutable_twist();
                twist->mutable_linear()->set_x(snapshot->odometry->twist.twist.linear.x);
                twist->mutable_linear()->set_y(snapshot->odometry->twist.twist.linear.y);
                twist->mutable_linear()->set_z(snapshot->odometry->twist.twist.linear.z);
                twist->mutable_angular()->set_x(snapshot->odometry->twist.twist.angular.x);
                twist->mutable_angular()->set_y(snapshot->odometry->twist.twist.angular.y);
                twist->mutable_angular()->set_z(snapshot->odometry->twist.twist.angular.z);
            }

            // Transforms
            for (const auto& [frame_id, tf] : snapshot->transforms) {
                auto* tf_data = tel->add_transforms();
                tf_data->set_frame_id(tf.header.frame_id);
                tf_data->set_child_frame_id(tf.child_frame_id);
                tf_data->mutable_translation()->set_x(tf.transform.translation.x);
                tf_data->mutable_translation()->set_y(tf.transform.translation.y);
                tf_data->mutable_translation()->set_z(tf.transform.translation.z);
                tf_data->mutable_rotation()->set_x(tf.transform.rotation.x);
                tf_data->mutable_rotation()->set_y(tf.transform.rotation.y);
                tf_data->mutable_rotation()->set_z(tf.transform.rotation.z);
                tf_data->mutable_rotation()->set_w(tf.transform.rotation.w);
            }
        }
    }

    // Log heartbeat summary (throttled: every 10 seconds)
    CLOG_INFO_THROTTLE(log, 10.0, "[Heartbeat] state={} exec={} task={} step={} tel={}",
                       state_name, is_exec,
                       task_id->empty() ? "-" : task_id->substr(0, 8),
                       step_id->empty() ? "-" : *step_id,
                       has_tel);

    queue_message(msg);
}

void Agent::slow_tick() {
    // Update lifecycle states for all capabilities
    if (capability_scanner_) {
        capability_scanner_->update_lifecycle_states();
    }

    // Capability scan
    if (capability_scanner_) {
        int changes = capability_scanner_->refresh();
        if (changes > 0) {
            log.info("Detected {} capability changes", changes);
            send_capabilities();
        }
    }

    // Reconnection check
    if (quic_client_ && !connected_) {
        auto now = std::chrono::steady_clock::now();
        auto elapsed = std::chrono::duration_cast<std::chrono::milliseconds>(
            now - last_reconnect_attempt_).count();

        if (elapsed >= reconnect_delay_ms_) {
            log.info("Attempting reconnection (delay: {}ms)", reconnect_delay_ms_);
            last_reconnect_attempt_ = now;

            if (try_connect()) {
                register_with_server();
                send_capabilities();
                reconnect_delay_ms_ = 1000;  // Reset backoff
            } else {
                reconnect_delay_ms_ = std::min(reconnect_delay_ms_ * 2, 30000);
            }
        }
    }
}

// ============================================================
// QUIC Communication
// ============================================================

void Agent::poll_quic_receive() {
    // MsQuic uses callback-based receiving - data arrives via on_quic_data()
    // No manual polling needed; this function is a no-op for tick-based design
    // The QUIC callbacks are processed by MsQuic's internal thread
}

void Agent::flush_outbound() {
    static int flush_log_count = 0;
    flush_log_count++;

    if (!quic_client_ || !connected_) {
        if (flush_log_count % 100 == 1) {
            log.warn("[flush] Skip: quic={} connected={} queue_size=?",
                     quic_client_ != nullptr, connected_.load());
        }
        return;
    }

    if (outbound_queue_.empty()) {
        return;
    }

    // Get or create persistent stream for outbound messages
    if (!outbound_stream_ || !outbound_stream_->is_open()) {
        outbound_stream_ = quic_client_->get_stream();
        if (!outbound_stream_) {
            log.warn("[flush] Failed to get stream");
            return;
        }
        log.info("[flush] Created outbound stream");
    }

    int sent_count = 0;
    std::shared_ptr<fleet::v1::AgentMessage> msg;
    while (outbound_queue_.try_pop(msg)) {
        std::string serialized;
        if (!msg->SerializeToString(&serialized)) {
            log.warn("[flush] Failed to serialize message");
            continue;
        }

        // Length-prefix framing
        std::vector<uint8_t> data;
        data.reserve(4 + serialized.size());
        uint32_t len = static_cast<uint32_t>(serialized.size());
        data.push_back((len >> 24) & 0xFF);
        data.push_back((len >> 16) & 0xFF);
        data.push_back((len >> 8) & 0xFF);
        data.push_back(len & 0xFF);
        data.insert(data.end(), serialized.begin(), serialized.end());

        if (!outbound_stream_->write_async(data.data(), data.size(), false)) {
            log.warn("[flush] Failed to write to stream, will recreate");
            outbound_stream_.reset();
            outbound_queue_.push(msg);
            break;
        }
        sent_count++;
    }

    if (sent_count > 0) {
        static int total_sent = 0;
        total_sent += sent_count;
        CLOG_INFO_THROTTLE(log, 30.0, "[flush] total_sent={} (last_batch={})",
                           total_sent, sent_count);
    }
}

void Agent::queue_message(std::shared_ptr<fleet::v1::AgentMessage> msg) {
    outbound_queue_.push(std::move(msg));
    // Wake up sender thread
    sender_cv_.notify_one();
}

void Agent::start_sender_thread() {
    sender_running_ = true;
    sender_thread_ = std::thread(&Agent::sender_loop, this);
    log.info("Sender thread started");
}

void Agent::stop_sender_thread() {
    sender_running_ = false;
    sender_cv_.notify_all();
    if (sender_thread_.joinable()) {
        sender_thread_.join();
    }
    log.info("Sender thread stopped");
}

void Agent::sender_loop() {
    log.info("[SenderThread] ===== STARTED =====");

    int loop_count = 0;
    int skip_count = 0;
    while (sender_running_) {
        // Wait for messages or shutdown
        {
            std::unique_lock<std::mutex> lock(sender_mutex_);
            sender_cv_.wait_for(lock, std::chrono::milliseconds(10), [this] {
                return !sender_running_ || !outbound_queue_.empty();
            });
        }

        if (!sender_running_) break;

        loop_count++;

        // Log frequently at startup, then every 100 iterations
        bool should_log = (loop_count <= 10) || (loop_count % 100 == 0);
        if (should_log) {
            log.info("[SenderThread] loop={} quic={} connected={} queue_size={}",
                     loop_count, quic_client_ != nullptr, connected_.load(),
                     outbound_queue_.unsafe_size());
        }

        // Check connection
        if (!quic_client_ || !connected_) {
            skip_count++;
            if (skip_count % 100 == 1 && !outbound_queue_.empty()) {
                log.warn("[SenderThread] Skipping - no connection (quic={}, connected={}), queue_size={}",
                         quic_client_ != nullptr, connected_.load(), outbound_queue_.unsafe_size());
            }
            continue;
        }
        skip_count = 0;  // Reset when connected

        // Get or create stream
        if (!outbound_stream_ || !outbound_stream_->is_open()) {
            outbound_stream_ = quic_client_->get_stream();
            if (!outbound_stream_) {
                log.warn("[SenderThread] Failed to get stream");
                std::this_thread::sleep_for(std::chrono::milliseconds(100));
                continue;
            }
            log.info("[SenderThread] Created outbound stream");
        }

        // Drain queue
        int sent_count = 0;
        std::shared_ptr<fleet::v1::AgentMessage> msg;
        while (outbound_queue_.try_pop(msg)) {
            std::string serialized;
            if (!msg->SerializeToString(&serialized)) {
                log.warn("[SenderThread] Failed to serialize");
                continue;
            }

            // Length-prefix framing
            std::vector<uint8_t> data;
            data.reserve(4 + serialized.size());
            uint32_t len = static_cast<uint32_t>(serialized.size());
            data.push_back((len >> 24) & 0xFF);
            data.push_back((len >> 16) & 0xFF);
            data.push_back((len >> 8) & 0xFF);
            data.push_back(len & 0xFF);
            data.insert(data.end(), serialized.begin(), serialized.end());

            if (!outbound_stream_->write_async(data.data(), data.size(), false)) {
                log.warn("[SenderThread] Write failed, recreating stream");
                outbound_stream_.reset();
                outbound_queue_.push(msg);  // Requeue
                break;
            }
            sent_count++;
        }

        if (sent_count > 0) {
            log.info("[SenderThread] Sent {} messages", sent_count);
        }
    }

    log.info("[SenderThread] ===== STOPPED =====");
}

void Agent::on_quic_data(const uint8_t* data, size_t len) {
    std::lock_guard<std::mutex> lock(inbound_mutex_);
    inbound_buffer_.insert(inbound_buffer_.end(), data, data + len);

    // Process complete messages
    while (true) {
        if (expected_msg_len_ == 0) {
            if (inbound_buffer_.size() < 4) break;

            expected_msg_len_ =
                (static_cast<uint32_t>(inbound_buffer_[0]) << 24) |
                (static_cast<uint32_t>(inbound_buffer_[1]) << 16) |
                (static_cast<uint32_t>(inbound_buffer_[2]) << 8) |
                static_cast<uint32_t>(inbound_buffer_[3]);

            inbound_buffer_.erase(inbound_buffer_.begin(), inbound_buffer_.begin() + 4);

            if (expected_msg_len_ > kMaxMessageBytes) {
                log.error("Message too large: {} bytes", expected_msg_len_);
                inbound_buffer_.clear();
                expected_msg_len_ = 0;
                break;
            }
        }

        if (inbound_buffer_.size() < expected_msg_len_) break;

        on_complete_message(inbound_buffer_.data(), expected_msg_len_);
        inbound_buffer_.erase(inbound_buffer_.begin(),
                               inbound_buffer_.begin() + expected_msg_len_);
        expected_msg_len_ = 0;
    }
}

void Agent::on_complete_message(const uint8_t* data, size_t len) {
    fleet::v1::ServerMessage msg;
    if (!msg.ParseFromArray(data, static_cast<int>(len))) {
        log.error("Failed to parse ServerMessage");
        return;
    }
    on_server_message(msg);
}

void Agent::on_server_message(const fleet::v1::ServerMessage& msg) {
    if (msg.has_start_task()) {
        const auto& cmd = msg.start_task();
        std::unordered_map<std::string, std::string> params;
        for (const auto& [k, v] : cmd.params()) {
            params[k] = v;
        }
        handle_start_task(cmd.task_id(), cmd.graph_id(), params);
    }
    else if (msg.has_cancel()) {
        handle_cancel_task(msg.cancel().task_id(), msg.cancel().reason());
    }
    else if (msg.has_deploy_graph()) {
        handle_deploy_graph(msg.deploy_graph());
    }
    else if (msg.has_ping()) {
        handle_ping(msg.ping().ping_id(), msg.ping().timestamp_ms());
    }
    else if (msg.has_fleet_state()) {
        std::unordered_map<std::string, std::string> states;
        std::unordered_map<std::string, bool> executing;
        for (const auto& as : msg.fleet_state().agents()) {
            states[as.agent_id()] = as.state();
            executing[as.agent_id()] = as.is_executing();
        }
        handle_fleet_state(states, executing);
    }
}

void Agent::on_connection_change(bool connected) {
    connected_ = connected;
    if (connected) {
        log.info("QUIC connected");
        reconnect_delay_ms_ = 1000;
    } else {
        log.warn("QUIC disconnected");
        // Reset persistent stream on disconnect
        outbound_stream_.reset();
    }
}

bool Agent::try_connect() {
    if (!quic_client_) return false;

    const auto& cfg = config_.server.quic;
    quic_client_->disconnect();

    if (!quic_client_->connect(cfg.server_address, cfg.server_port)) {
        return false;
    }

    setup_quic_handler();
    connected_ = true;
    log.info("Connected to {}:{}", cfg.server_address, cfg.server_port);
    return true;
}

bool Agent::register_with_server() {
    if (!quic_client_ || !connected_) return false;

    log.info("Registering with server...");

    fleet::v1::RegisterAgentRequest req;
    req.set_agent_id("");  // Let server assign
    req.set_name(config_.agent_name);
    req.set_hardware_fingerprint(hardware_fingerprint_);

    std::string serialized;
    req.SerializeToString(&serialized);

    std::vector<uint8_t> data;
    data.reserve(4 + serialized.size());
    uint32_t len = static_cast<uint32_t>(serialized.size());
    data.push_back((len >> 24) & 0xFF);
    data.push_back((len >> 16) & 0xFF);
    data.push_back((len >> 8) & 0xFF);
    data.push_back(len & 0xFF);
    data.insert(data.end(), serialized.begin(), serialized.end());

    auto stream = quic_client_->get_stream();
    if (!stream || !stream->write(data.data(), data.size(), false)) {
        log.error("Failed to send registration");
        return false;
    }

    // Wait for response (blocking with timeout)
    std::vector<uint8_t> resp_buf;
    bool received = false;

    stream->set_data_callback([&](const uint8_t* d, size_t l, bool) {
        resp_buf.insert(resp_buf.end(), d, d + l);
        if (resp_buf.size() >= 4) {
            uint32_t exp_len = (resp_buf[0] << 24) | (resp_buf[1] << 16) |
                               (resp_buf[2] << 8) | resp_buf[3];
            if (resp_buf.size() >= 4 + exp_len) {
                received = true;
            }
        }
    });

    // Simple polling wait
    for (int i = 0; i < 50 && !received; ++i) {
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }

    stream->close();

    if (!received || resp_buf.size() < 4) {
        log.error("Registration timeout");
        return false;
    }

    uint32_t resp_len = (resp_buf[0] << 24) | (resp_buf[1] << 16) |
                        (resp_buf[2] << 8) | resp_buf[3];

    fleet::v1::RegisterAgentResponse resp;
    if (!resp.ParseFromArray(resp_buf.data() + 4, resp_len)) {
        log.error("Failed to parse registration response");
        return false;
    }

    if (!resp.success()) {
        log.error("Registration rejected: {}", resp.error());
        return false;
    }

    if (!resp.assigned_agent_id().empty()) {
        std::string old_id = effective_agent_id_;
        effective_agent_id_ = resp.assigned_agent_id();
        agent_id_storage_->save(effective_agent_id_);
        log.info("Server assigned ID: {}", effective_agent_id_);

        // Update telemetry collectors to use the new ID
        // This ensures telemetry is stored/retrieved under the correct ID
        for (auto& collector : telemetry_collectors_) {
            collector->set_robot_id(effective_agent_id_);
        }
        log.info("Updated {} telemetry collector(s) with new ID", telemetry_collectors_.size());
    }

    return true;
}

void Agent::setup_quic_handler() {
    if (!quic_client_) return;

    auto* conn = quic_client_->connection();
    if (!conn) return;

    conn->set_stream_callback([this](std::shared_ptr<transport::QUICStream> stream) {
        stream->set_data_callback([this](const uint8_t* d, size_t l, bool) {
            on_quic_data(d, l);
        });
    });
}

// ============================================================
// Command Handling
// ============================================================

void Agent::handle_start_task(const std::string& task_id,
                               const std::string& behavior_tree_id,
                               const std::unordered_map<std::string, std::string>& params) {
    log.info("Starting task: {} (behavior tree: {})", task_id, behavior_tree_id);

    if (current_task_) {
        log.warn("Task already running: {}", current_task_->task_id);
        return;
    }

    // Load behavior tree
    auto behavior_tree = graph_storage_->load(behavior_tree_id);
    if (!behavior_tree) {
        log.error("Behavior tree not found: {}", behavior_tree_id);
        return;
    }

    // Find entry point directly from loaded behavior tree
    std::string entry_point = behavior_tree->entry_point();
    if (entry_point.empty() && behavior_tree->vertices_size() > 0) {
        // Fallback: use first vertex
        entry_point = behavior_tree->vertices(0).id();
    }
    if (entry_point.empty()) {
        log.error("No entry point in behavior tree (vertices={}, entry_point='{}')",
                  behavior_tree->vertices_size(), behavior_tree->entry_point());
        return;
    }

    log.info("Task entry point: {} (behavior tree has {} vertices, {} edges)",
             entry_point, behavior_tree->vertices_size(), behavior_tree->edges_size());

    // Initialize task context
    TaskContext ctx;
    ctx.task_id = task_id;
    ctx.behavior_tree_id = behavior_tree_id;
    ctx.behavior_tree = std::make_unique<fleet::v1::BehaviorTree>(*behavior_tree);
    ctx.variables = params;
    ctx.started_at = std::chrono::steady_clock::now();
    ctx.status = TaskContext::Status::RUNNING;
    ctx.current_step_id = entry_point;

    current_task_ = std::move(ctx);

    update_exec_state(true, "", task_id, entry_point);
    send_task_state_update();
}

void Agent::handle_cancel_task(const std::string& task_id, const std::string& reason) {
    log.info("Cancelling task: {} ({})", task_id, reason);

    if (!current_task_ || current_task_->task_id != task_id) {
        log.warn("Task not found: {}", task_id);
        return;
    }

    // Cancel current action if any
    if (current_task_->current_goal) {
        // Find the action client and cancel
        for (auto& [key, client] : action_clients_) {
            client->cancel_goal(current_task_->current_goal);
        }
    }

    current_task_->status = TaskContext::Status::CANCELLED;
    current_task_->error_message = reason;
    complete_task(false, reason);
}

void Agent::handle_deploy_graph(const fleet::v1::DeployGraphRequest& req) {
    const auto& behavior_tree = req.graph();
    const auto& correlation_id = req.correlation_id();
    const auto& id = behavior_tree.metadata().id();
    int32_t version = behavior_tree.metadata().version();

    log.info("Deploying behavior tree: {} (correlation: {}, force: {})",
             id, correlation_id, req.force());

    // Build response message
    auto resp_msg = std::make_shared<fleet::v1::AgentMessage>();
    resp_msg->set_agent_id(effective_agent_id_);
    resp_msg->set_timestamp_ms(now_ms());
    auto* resp = resp_msg->mutable_deploy_response();
    resp->set_correlation_id(correlation_id);
    resp->set_graph_id(id);
    resp->set_deployed_version(version);

    bool success = false;
    if (graph_storage_) {
        success = graph_storage_->store(behavior_tree);
    }

    if (success) {
        log.info("Behavior tree stored successfully: {} v{}", id, version);
        resp->set_success(true);
        resp->set_checksum(behavior_tree.checksum());
    } else {
        log.error("Failed to store behavior tree: {}", id);
        resp->set_success(false);
        resp->set_error("Failed to store behavior tree");
    }

    // Send response back to server
    queue_message(resp_msg);
    log.info("Deploy response sent for behavior tree: {} success={}", id, success);
}

void Agent::handle_ping(const std::string& ping_id, int64_t server_ts) {
    auto msg = std::make_shared<fleet::v1::AgentMessage>();
    msg->set_agent_id(effective_agent_id_);
    msg->set_timestamp_ms(now_ms());

    auto* pong = msg->mutable_pong();
    pong->set_ping_id(ping_id);
    pong->set_server_timestamp_ms(server_ts);
    pong->set_agent_timestamp_ms(now_ms());

    queue_message(msg);
}

void Agent::handle_fleet_state(const std::unordered_map<std::string, std::string>& states,
                                const std::unordered_map<std::string, bool>& executing) {
    if (fleet_state_cache_) {
        for (const auto& [id, state_str] : states) {
            state::FleetStateEntry entry;
            entry.agent_id = id;
            entry.state_code = state_str;
            entry.is_executing = executing.count(id) ? executing.at(id) : false;
            entry.is_online = true;
            entry.updated_at = std::chrono::system_clock::now();
            fleet_state_cache_->update(entry);
        }
    }
}

// ============================================================
// Task Execution
// ============================================================

void Agent::process_task() {
    if (!current_task_) return;

    auto& task = *current_task_;

    switch (task.status) {
        case TaskContext::Status::RUNNING:
            // Execute current step
            execute_step(task.current_step_id);
            break;

        case TaskContext::Status::WAITING_ACTION:
            // Waiting for action result - nothing to do (poll_all_clients handles it)
            break;

        case TaskContext::Status::COMPLETED:
        case TaskContext::Status::FAILED:
        case TaskContext::Status::CANCELLED:
            // Task finished - clear it
            current_task_.reset();
            break;
    }
}

void Agent::execute_step(const std::string& step_id) {
    auto& task = *current_task_;

    auto vertex = find_vertex(step_id);
    if (!vertex) {
        log.error("Vertex not found: {}", step_id);
        complete_task(false, "Vertex not found: " + step_id);
        return;
    }

    // Check if this is a terminal vertex
    if (vertex->type() == fleet::v1::VERTEX_TYPE_TERMINAL) {
        if (vertex->has_terminal()) {
            auto terminal_type = vertex->terminal().terminal_type();
            if (terminal_type == fleet::v1::TERMINAL_TYPE_SUCCESS) {
                complete_task(true);
                return;
            } else if (terminal_type == fleet::v1::TERMINAL_TYPE_FAILURE) {
                complete_task(false, "Reached failure end");
                return;
            }
        }
        complete_task(true);  // Default to success for unknown terminal types
        return;
    }

    // Execute action
    if (!vertex->has_step() || !vertex->step().has_action()) {
        log.error("Vertex {} has no action", step_id);
        complete_task(false, "No action in vertex");
        return;
    }

    const auto& step = vertex->step();
    const auto& action = step.action();
    const auto& action_type = action.action_type();
    const auto& server = action.action_server();

    log.info("Executing step: {} (action: {}, server: {})", step_id, action_type, server);

    // Apply during_states to state tracker
    if (step.during_states_size() > 0 && state_tracker_mgr_) {
        if (auto tracker = state_tracker_mgr_->get_tracker(effective_agent_id_)) {
            std::string during_state = step.during_states(0);  // Use first during_state
            tracker->force_state(during_state, "step_start");
            log.info("Applied during_state: {}", during_state);
        }
    }

    // Get or create action client
    auto* client = get_or_create_client(action_type, server);
    if (!client) {
        log.error("Failed to get action client for {}", action_type);
        complete_task(false, "Failed to create action client");
        return;
    }

    // Build goal from parameters (goal_params is bytes, need to convert)
    nlohmann::json goal_json;
    const auto& goal_params = action.goal_params();
    if (!goal_params.empty()) {
        try {
            auto params_json = nlohmann::json::parse(
                std::string(goal_params.begin(), goal_params.end()));

            // Check if params contains field_sources (canonical format from UI)
            if (params_json.contains("field_sources") && params_json["field_sources"].is_object()) {
                log.info("Step {} has field_sources, resolving bindings...", step_id);

                // Start with data if present
                if (params_json.contains("data") && params_json["data"].is_object()) {
                    goal_json = params_json["data"];
                }

                // Resolve each field source
                for (auto& [field_path, source_config] : params_json["field_sources"].items()) {
                    std::string source_type = source_config.value("source", "constant");

                    if (source_type == "constant") {
                        // Use constant value directly
                        if (source_config.contains("value")) {
                            goal_json[field_path] = source_config["value"];
                            log.info("  {} = {} (constant)", field_path, source_config["value"].dump());
                        }
                    } else if (source_type == "step_result") {
                        // Resolve from previous step result
                        std::string src_step_id = source_config.value("step_id", "");
                        std::string result_field = source_config.value("result_field", "");

                        if (!src_step_id.empty() && !result_field.empty()) {
                            std::string var_key = src_step_id + "." + result_field;
                            log.info("  {} <- {}.{}", field_path, src_step_id, result_field);

                            if (task.variables.count(var_key)) {
                                std::string var_value = task.variables[var_key];
                                try {
                                    goal_json[field_path] = nlohmann::json::parse(var_value);
                                } catch (...) {
                                    goal_json[field_path] = var_value;
                                }
                                log.info("    Resolved: {} = {}", field_path, goal_json[field_path].dump());
                            } else {
                                log.warn("    Variable {} not found in task.variables", var_key);
                                // List available variables for debugging
                                log.info("    Available variables:");
                                for (const auto& [k, v] : task.variables) {
                                    log.info("      {} = {}", k, v.substr(0, 50));
                                }
                            }
                        }
                    }
                }

                log.info("Resolved goal_json: {}", goal_json.dump());
            } else {
                // No field_sources - use as-is or simple data format
                if (params_json.contains("data") && params_json["data"].is_object()) {
                    goal_json = params_json["data"];
                } else {
                    goal_json = params_json;
                }
            }
        } catch (const std::exception& e) {
            log.warn("Failed to parse action goal_params as JSON: {}", e.what());
        }
    }

    // Legacy variable substitution for ${var} syntax
    for (auto& [key, value] : goal_json.items()) {
        if (value.is_string()) {
            std::string s = value.get<std::string>();
            // Simple variable substitution: ${step_id.field}
            if (s.length() > 3 && s[0] == '$' && s[1] == '{' && s.back() == '}') {
                std::string var_name = s.substr(2, s.length() - 3);
                if (task.variables.count(var_name)) {
                    try {
                        goal_json[key] = nlohmann::json::parse(task.variables[var_name]);
                    } catch (...) {
                        goal_json[key] = task.variables[var_name];
                    }
                }
            }
        }
    }

    // Send goal
    task.current_action_type = action_type;
    update_exec_state(true, action_type, task.task_id, step_id);

    auto goal_handle = client->send_goal(
        goal_json.dump(),
        [this](bool success, const std::string& result) {
            on_action_result(success, result);
        },
        nullptr  // No feedback callback for simplicity
    );

    if (!goal_handle) {
        log.error("Failed to send goal");
        complete_task(false, "Failed to send action goal");
        return;
    }

    task.current_goal = goal_handle;
    task.status = TaskContext::Status::WAITING_ACTION;
}

void Agent::on_action_result(bool success, const std::string& result_json) {
    if (!current_task_) {
        log.warn("Received action result but no task running");
        return;
    }

    auto& task = *current_task_;
    log.info("Action result: success={}, step={}", success, task.current_step_id);

    // Store result in variables
    task.variables[task.current_step_id + ".result"] = result_json;
    task.variables[task.current_step_id + ".success"] = success ? "true" : "false";

    // Parse result JSON and store individual fields for field_sources binding
    try {
        auto result = nlohmann::json::parse(result_json);
        for (auto& [key, value] : result.items()) {
            std::string var_key = task.current_step_id + "." + key;
            task.variables[var_key] = value.dump();
            log.info("Stored variable: {} = {}", var_key, value.dump());
        }
    } catch (const std::exception& e) {
        log.warn("Failed to parse result JSON for variable extraction: {}", e.what());
    }

    // Apply success_states or failure_states
    if (state_tracker_mgr_) {
        if (auto tracker = state_tracker_mgr_->get_tracker(effective_agent_id_)) {
            auto vertex = find_vertex(task.current_step_id);
            if (vertex && vertex->has_step()) {
                const auto& step = vertex->step();
                if (success && step.success_states_size() > 0) {
                    tracker->force_state(step.success_states(0), "step_success");
                    log.info("Applied success_state: {}", step.success_states(0));
                } else if (!success && step.failure_states_size() > 0) {
                    tracker->force_state(step.failure_states(0), "step_failure");
                    log.info("Applied failure_state: {}", step.failure_states(0));
                }
            }
        }
    }

    task.current_goal.reset();
    task.status = TaskContext::Status::RUNNING;

    advance_task(success);
}

void Agent::advance_task(bool step_success) {
    auto& task = *current_task_;

    auto next = get_next_step(task.current_step_id, step_success);
    if (!next) {
        // No next step - complete based on step success
        complete_task(step_success, step_success ? "" : "Step failed with no recovery path");
        return;
    }

    task.current_step_id = *next;
    log.info("Advancing to step: {}", *next);

    send_task_state_update();
}

void Agent::complete_task(bool success, const std::string& error) {
    if (!current_task_) return;

    auto& task = *current_task_;
    log.info("Task {} completed: success={}, error={}",
             task.task_id, success, error.empty() ? "none" : error);

    task.status = success ? TaskContext::Status::COMPLETED : TaskContext::Status::FAILED;
    task.error_message = error;

    // Update state
    if (state_tracker_mgr_) {
        auto tracker = state_tracker_mgr_->get_tracker(effective_agent_id_);
        if (tracker) {
            tracker->force_state(success ? "idle" : "error", "task_complete");
        }
    }

    update_exec_state(false);
    send_task_state_update();

    // Clear task (will be done on next process_task call)
}

std::optional<fleet::v1::Vertex> Agent::find_vertex(const std::string& step_id) {
    if (!current_task_ || !current_task_->behavior_tree) return std::nullopt;

    for (const auto& v : current_task_->behavior_tree->vertices()) {
        if (v.id() == step_id) {
            return v;
        }
    }
    return std::nullopt;
}

std::optional<std::string> Agent::get_entry_point() {
    if (!current_task_ || !current_task_->behavior_tree) return std::nullopt;

    if (!current_task_->behavior_tree->entry_point().empty()) {
        return current_task_->behavior_tree->entry_point();
    }

    // Fallback: first vertex
    if (!current_task_->behavior_tree->vertices().empty()) {
        return current_task_->behavior_tree->vertices(0).id();
    }

    return std::nullopt;
}

std::optional<std::string> Agent::get_next_step(const std::string& current, bool success) {
    if (!current_task_ || !current_task_->behavior_tree) return std::nullopt;

    for (const auto& edge : current_task_->behavior_tree->edges()) {
        if (edge.from_vertex() != current) continue;

        if (success && edge.type() == fleet::v1::EDGE_TYPE_ON_SUCCESS) {
            return edge.to_vertex();
        }
        if (!success && edge.type() == fleet::v1::EDGE_TYPE_ON_FAILURE) {
            return edge.to_vertex();
        }
        // EDGE_TYPE_CONDITIONAL acts as a fallback
        if (edge.type() == fleet::v1::EDGE_TYPE_CONDITIONAL) {
            return edge.to_vertex();
        }
    }

    return std::nullopt;
}

// ============================================================
// Action Client Management
// ============================================================

executor::DynamicActionClient* Agent::get_or_create_client(
    const std::string& action_type,
    const std::string& server_name) {

    std::string key = action_type + "|" + server_name;

    // Try to find existing client (read-only accessor)
    {
        decltype(action_clients_)::const_accessor acc;
        if (action_clients_.find(acc, key)) {
            return acc->second.get();
        }
    }

    // Create new client
    log.info("Creating action client: {} -> {}", action_type, server_name);

    auto client = std::make_unique<executor::DynamicActionClient>(
        node_, server_name, action_type);

    if (!client->wait_for_server(std::chrono::seconds(5))) {
        log.error("Action server not available: {}", server_name);
        return nullptr;
    }

    auto* ptr = client.get();

    // Insert with write accessor (thread-safe)
    decltype(action_clients_)::accessor acc;
    if (action_clients_.insert(acc, key)) {
        // Successfully inserted new entry
        acc->second = std::move(client);
    }
    // If insert returns false, another thread already inserted - return that one
    return acc->second.get();
}

void Agent::poll_all_clients() {
    // Iterate using range-based for with concurrent_hash_map
    for (auto it = action_clients_.begin(); it != action_clients_.end(); ++it) {
        it->second->poll_for_responses();
    }
}

// ============================================================
// Capability Management
// ============================================================

void Agent::discover_capabilities() {
    log.info("Discovering capabilities...");

    // Wait for ROS2 graph
    std::this_thread::sleep_for(std::chrono::seconds(2));

    int found = capability_scanner_->scan_all();
    log.info("Discovered {} capabilities", found);

    send_capabilities();
}

void Agent::send_capabilities() {
    if (!connected_) return;

    auto caps = capability_scanner_->get_for_registration();
    if (caps.empty()) return;

    auto msg = std::make_shared<fleet::v1::AgentMessage>();
    msg->set_agent_id(effective_agent_id_);
    msg->set_timestamp_ms(now_ms());

    auto* reg = msg->mutable_capability_registration();
    reg->set_agent_id(effective_agent_id_);

    for (const auto& cap : caps) {
        auto* c = reg->add_capabilities();
        c->set_action_type(cap.action_type);
        c->set_action_server(cap.action_server);
        c->set_is_available(cap.available.load());  // Use actual availability status

        // Set JSON schemas
        if (!cap.goal_schema_json.empty()) {
            c->set_goal_schema(cap.goal_schema_json);
        }
        if (!cap.result_schema_json.empty()) {
            c->set_result_schema(cap.result_schema_json);
        }
        if (!cap.feedback_schema_json.empty()) {
            c->set_feedback_schema(cap.feedback_schema_json);
        }

        // Set success criteria
        if (!cap.success_criteria.field.empty()) {
            auto* criteria = c->mutable_success_criteria();
            criteria->set_field(cap.success_criteria.field);
            criteria->set_operator_(cap.success_criteria.op);
            criteria->set_value(cap.success_criteria.value);
        }

        // Set lifecycle state
        auto lc_state = cap.lifecycle_state.load();
        switch (lc_state) {
            case LifecycleState::UNCONFIGURED:
                c->set_lifecycle_state(fleet::v1::LIFECYCLE_STATE_UNCONFIGURED);
                break;
            case LifecycleState::INACTIVE:
                c->set_lifecycle_state(fleet::v1::LIFECYCLE_STATE_INACTIVE);
                break;
            case LifecycleState::ACTIVE:
                c->set_lifecycle_state(fleet::v1::LIFECYCLE_STATE_ACTIVE);
                break;
            case LifecycleState::FINALIZED:
                c->set_lifecycle_state(fleet::v1::LIFECYCLE_STATE_FINALIZED);
                break;
            default:
                c->set_lifecycle_state(fleet::v1::LIFECYCLE_STATE_UNKNOWN);
                break;
        }
    }

    queue_message(msg);
    log.info("Sent {} capabilities", caps.size());
}

// ============================================================
// State Reporting
// ============================================================

void Agent::send_task_state_update() {
    if (!current_task_) return;

    auto& task = *current_task_;

    auto msg = std::make_shared<fleet::v1::AgentMessage>();
    msg->set_agent_id(effective_agent_id_);
    msg->set_timestamp_ms(now_ms());

    auto* update = msg->mutable_task_state();
    update->set_task_id(task.task_id);
    update->set_current_step_id(task.current_step_id);

    switch (task.status) {
        case TaskContext::Status::RUNNING:
        case TaskContext::Status::WAITING_ACTION:
            update->set_state(fleet::v1::TASK_STATE_RUNNING);
            break;
        case TaskContext::Status::COMPLETED:
            update->set_state(fleet::v1::TASK_STATE_COMPLETED);
            break;
        case TaskContext::Status::FAILED:
            update->set_state(fleet::v1::TASK_STATE_FAILED);
            if (!task.error_message.empty()) {
                update->set_blocking_reason(task.error_message);
            }
            break;
        case TaskContext::Status::CANCELLED:
            update->set_state(fleet::v1::TASK_STATE_CANCELLED);
            if (!task.error_message.empty()) {
                update->set_blocking_reason(task.error_message);
            }
            break;
    }

    queue_message(msg);
}

void Agent::update_exec_state(bool executing,
                               const std::string& action,
                               const std::string& task_id,
                               const std::string& step_id) {
    // Atomic updates using shared_ptr (lock-free)
    std::atomic_store(&exec_state_.action_type, std::make_shared<const std::string>(action));
    std::atomic_store(&exec_state_.task_id, std::make_shared<const std::string>(task_id));
    std::atomic_store(&exec_state_.step_id, std::make_shared<const std::string>(step_id));
    exec_state_.is_executing.store(executing, std::memory_order_release);
    exec_state_.version.fetch_add(1, std::memory_order_relaxed);
}

// ============================================================
// Helpers
// ============================================================

int64_t Agent::now_ms() {
    return std::chrono::duration_cast<std::chrono::milliseconds>(
        std::chrono::system_clock::now().time_since_epoch()).count();
}

}  // namespace robot_agent
