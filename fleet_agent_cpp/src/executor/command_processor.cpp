// Copyright 2026 Multi-Robot Supervision System
// Command Processor Implementation

#include "fleet_agent/executor/command_processor.hpp"
#include "fleet_agent/graph/storage.hpp"
#include "fleet_agent/state/state_tracker.hpp"
#include "fleet_agent/core/logger.hpp"
#include "fleet_agent/core/shutdown.hpp"

#include <algorithm>
#include <cctype>
#include <iterator>

// Generated protobuf headers
#include "fleet/v1/service.pb.h"
#include "fleet/v1/commands.pb.h"

namespace fleet_agent {
namespace executor {

namespace {
logging::ComponentLogger log("CommandProcessor");
}

CommandProcessor::CommandProcessor(
    rclcpp::Node::SharedPtr node,
    const std::string& agent_id,
    InboundQueue& inbound_queue,
    QuicOutboundQueue& quic_outbound_queue,
    CapabilityStore& capability_store,
    ExecutionContextMap& execution_contexts,
    graph::GraphStorage& graph_storage,
    state::StateTrackerManager* state_tracker_mgr)
    : node_(node)
    , agent_id_(agent_id)
    , inbound_queue_(inbound_queue)
    , quic_outbound_queue_(quic_outbound_queue)
    , capability_store_(capability_store)
    , execution_contexts_(execution_contexts)
    , graph_storage_(graph_storage)
    , state_tracker_mgr_(state_tracker_mgr)
    , task_log_sender_(std::make_unique<TaskLogSender>(agent_id, quic_outbound_queue)) {

    log.info("Initialized for agent: {} (state tracking: {})",
             agent_id_, state_tracker_mgr_ != nullptr ? "enabled" : "disabled");
}

CommandProcessor::~CommandProcessor() {
    stop();
}

void CommandProcessor::start() {
    if (running_.load()) {
        return;
    }

    running_.store(true);
    processor_thread_ = std::thread([this]() {
        process_loop();
    });

    log.info("Started processing thread");
}

void CommandProcessor::stop() {
    if (!running_.load()) {
        return;
    }

    running_.store(false);

    // Wake up the processing thread if waiting on queue
    inbound_queue_.notify_all();

    if (processor_thread_.joinable()) {
        processor_thread_.join();
    }

    log.info("Stopped processing thread");
}

void CommandProcessor::add_robot(const std::string& agent_id, const std::string& ros_namespace) {
    std::lock_guard<std::mutex> lock(executors_mutex_);

    if (executors_.find(agent_id) != executors_.end()) {
        log.warn("Robot {} already added", agent_id);
        return;
    }

    auto executor = std::make_unique<ActionExecutor>(
        node_,
        agent_id,
        ros_namespace,
        capability_store_,
        [this](const ActionResultInternal& result) {
            this->on_action_result(result);
        },
        [this](const std::string& agent_id, float progress) {
            this->on_action_feedback(agent_id, progress);
        }
    );

    executors_[agent_id] = std::move(executor);

    // Initialize execution context
    ExecutionContextMap::accessor acc;
    execution_contexts_.insert(acc, agent_id);
    acc->second = RobotExecutionContext();

    log.info("Added robot: {} (namespace: {})", agent_id, ros_namespace);
}

void CommandProcessor::remove_robot(const std::string& agent_id) {
    std::lock_guard<std::mutex> lock(executors_mutex_);

    auto it = executors_.find(agent_id);
    if (it != executors_.end()) {
        executors_.erase(it);
        log.info("Removed robot: {}", agent_id);
    }
}

bool CommandProcessor::rename_robot(const std::string& old_id, const std::string& new_id) {
    std::lock_guard<std::mutex> lock(executors_mutex_);

    // Rename in executors_ map
    auto it = executors_.find(old_id);
    if (it == executors_.end()) {
        log.warn("Cannot rename robot {} to {} - old ID not found", old_id, new_id);
        return false;
    }

    if (executors_.find(new_id) != executors_.end()) {
        log.warn("Cannot rename robot {} to {} - new ID already exists", old_id, new_id);
        return false;
    }

    auto executor = std::move(it->second);
    executors_.erase(it);
    executors_[new_id] = std::move(executor);

    // Rename in execution_contexts_ map
    ExecutionContextMap::accessor old_acc;
    if (execution_contexts_.find(old_acc, old_id)) {
        auto ctx = std::move(old_acc->second);
        execution_contexts_.erase(old_acc);

        ExecutionContextMap::accessor new_acc;
        execution_contexts_.insert(new_acc, new_id);
        new_acc->second = std::move(ctx);
    }

    // Update TaskLogSender's agent ID (1:1 model: agent_id == robot_id)
    if (task_log_sender_) {
        task_log_sender_->set_agent_id(new_id);
    }

    // Update our own agent_id_
    agent_id_ = new_id;

    log.info("Renamed robot {} to {}", old_id, new_id);
    return true;
}

void CommandProcessor::cancel_action(const std::string& agent_id, const std::string& reason) {
    auto* executor = get_executor(agent_id);
    if (executor && executor->is_executing()) {
        executor->cancel(reason);
        log.info("Cancelled action for robot {}: {}", agent_id, reason);
    } else {
        log.warn("No active action to cancel for robot {}", agent_id);
    }
}

void CommandProcessor::enqueue_execute_command(
    const fleet::v1::ExecuteCommand& cmd,
    const std::string& message_id) {
    handle_execute_command(cmd, message_id);
}

void CommandProcessor::update_fleet_state(
    const std::unordered_map<std::string, int>& robot_states,
    const std::unordered_map<std::string, bool>& robot_executing,
    const std::unordered_map<std::string, float>& robot_staleness,
    const std::unordered_map<std::string, bool>& robot_online) {

    std::lock_guard<std::mutex> lock(fleet_state_mutex_);
    fleet_states_ = robot_states;
    fleet_executing_ = robot_executing;
    fleet_staleness_ = robot_staleness;
    fleet_online_ = robot_online;
}

void CommandProcessor::set_server_query_callback(ServerQueryCallback callback) {
    server_query_callback_ = std::move(callback);
}

void CommandProcessor::process_loop() {
    log.debug("Process loop started");

    while (running_.load() && FLEET_AGENT_RUNNING) {
        InboundCommand cmd;

        // Wait for command with condition variable notification
        // Uses NotifiableQueue::wait_pop() for low-latency (<1ms) wake-up
        if (inbound_queue_.wait_pop(cmd, std::chrono::milliseconds(100), running_)) {
            try {
                handle_message(cmd);
            } catch (const std::exception& e) {
                log.error("Error processing message: {}", e.what());
            }
        }
        // No sleep needed - wait_pop handles efficient waiting
    }

    log.debug("Process loop ended");
}

void CommandProcessor::handle_message(const InboundCommand& cmd) {
    if (!cmd.message) {
        return;
    }

    const auto& server_msg = *cmd.message;
    log.debug("Processing message: {} (seq={})",
              server_msg.message_id(), server_msg.sequence());

    switch (server_msg.payload_case()) {
        case fleet::v1::ServerMessage::kExecute:
            handle_execute_command(server_msg.execute(), server_msg.message_id());
            break;

        case fleet::v1::ServerMessage::kCancel:
            handle_cancel_command(server_msg.cancel());
            break;

        case fleet::v1::ServerMessage::kDeployGraph:
            handle_deploy_graph(server_msg.deploy_graph());
            break;

        case fleet::v1::ServerMessage::kPing:
            handle_ping(server_msg.ping());
            break;

        case fleet::v1::ServerMessage::kAck:
            // Server acknowledgment, log and ignore
            log.debug("Received ACK for message: {}",
                     server_msg.ack().acked_message_id());
            break;

        case fleet::v1::ServerMessage::kConfigUpdate:
            // TODO: Handle config updates
            log.debug("Received config update for robot: {}",
                     server_msg.config_update().agent_id());
            break;

        case fleet::v1::ServerMessage::kFleetState:
            handle_fleet_state_broadcast(server_msg.fleet_state());
            break;

        default:
            log.warn("Unknown message payload type");
            break;
    }
}

void CommandProcessor::handle_execute_command(
    const fleet::v1::ExecuteCommand& cmd,
    const std::string& message_id) {

    log.info("Execute command: {} for robot {} (action_type: {}, action_server: '{}')",
             cmd.command_id(), cmd.agent_id(), cmd.action_type(), cmd.action_server());

    // Set task context for logging
    task_log_sender_->set_task_context(cmd.task_id(), cmd.step_id(), cmd.command_id());

    // Stream execution log
    task_log_sender_->info(
        "Received execute command",
        "CommandProcessor",
        {
            {"action_type", cmd.action_type()},
            {"action_server", cmd.action_server()},
            {"timeout_sec", std::to_string(cmd.timeout_sec())}
        }
    );

    // Get executor for robot
    auto* executor = get_executor(cmd.agent_id());
    if (!executor) {
        log.error("No executor for robot: {}", cmd.agent_id());
        task_log_sender_->error(
            "No executor found for robot",
            "CommandProcessor",
            {{"agent_id", cmd.agent_id()}}
        );
        // Send failure result
        ActionResultInternal result;
        result.command_id = cmd.command_id();
        result.agent_id = cmd.agent_id();
        result.task_id = cmd.task_id();
        result.step_id = cmd.step_id();
        result.status = static_cast<int>(fleet::v1::ACTION_STATUS_REJECTED);
        result.error = "Robot not found";
        result.completed_at_ms = now_ms();
        send_action_result(result);
        return;
    }

    // SAFETY CHECK: Validate capability/action server is available
    // This prevents execution when action server is offline
    {
        CapabilityStore::const_accessor acc;
        bool capability_found = capability_store_.find(acc, cmd.action_server());

        if (!capability_found) {
            log.error("Action server not found in capabilities: '{}'", cmd.action_server());
            // Debug: List all registered capabilities
            std::string available_caps;
            for (auto it = capability_store_.begin(); it != capability_store_.end(); ++it) {
                if (!available_caps.empty()) available_caps += ", ";
                available_caps += "'" + it->first + "'";
            }
            log.error("Available capabilities: [{}]", available_caps);

            task_log_sender_->error(
                "Action server not found in capabilities",
                "CommandProcessor",
                {
                    {"action_server", cmd.action_server()},
                    {"action_type", cmd.action_type()}
                }
            );
            ActionResultInternal result;
            result.command_id = cmd.command_id();
            result.agent_id = cmd.agent_id();
            result.task_id = cmd.task_id();
            result.step_id = cmd.step_id();
            result.status = static_cast<int>(fleet::v1::ACTION_STATUS_REJECTED);
            result.error = "Action server not found: " + cmd.action_server();
            result.completed_at_ms = now_ms();
            send_action_result(result);
            return;
        }

        if (!acc->second.available.load()) {
            log.error("Action server is not available: {}", cmd.action_server());
            task_log_sender_->error(
                "Action server is offline/unavailable",
                "CommandProcessor",
                {
                    {"action_server", cmd.action_server()},
                    {"action_type", cmd.action_type()}
                }
            );
            ActionResultInternal result;
            result.command_id = cmd.command_id();
            result.agent_id = cmd.agent_id();
            result.task_id = cmd.task_id();
            result.step_id = cmd.step_id();
            result.status = static_cast<int>(fleet::v1::ACTION_STATUS_REJECTED);
            result.error = "Action server unavailable: " + cmd.action_server();
            result.completed_at_ms = now_ms();
            send_action_result(result);
            return;
        }

        log.debug("Capability validation passed: {} ({})", cmd.action_server(), cmd.action_type());
    }

    // Check start conditions for Hybrid control
    if (cmd.start_conditions_size() > 0) {
        auto ctx = build_precond_context(cmd.agent_id());
        std::vector<PreconditionEvaluator::StartConditionSpec> conditions;
        conditions.reserve(static_cast<size_t>(cmd.start_conditions_size()));
        for (const auto& cond : cmd.start_conditions()) {
            PreconditionEvaluator::StartConditionSpec spec;
            spec.id = cond.id();
            spec.operator_name = cond.operator_();
            spec.quantifier = cond.quantifier();
            spec.target_type = cond.target_type();
            spec.agent_id = cond.agent_id();
            spec.agent_id = cond.agent_id();
            spec.state = cond.state();
            spec.state_operator = cond.state_operator();
            spec.allowed_states.assign(cond.allowed_states().begin(), cond.allowed_states().end());
            spec.max_staleness_sec = cond.max_staleness_sec();
            spec.require_online = cond.require_online();
            spec.message = cond.message();
            conditions.push_back(std::move(spec));
        }

        auto result = precond_evaluator_.check_start_conditions(conditions, ctx);

        if (result == PreconditionEvaluator::Result::NOT_SATISFIED) {
            // For direct commands, we don't wait - reject if not satisfied
            log.warn("Start condition not satisfied for command {}", cmd.command_id());
            task_log_sender_->warn(
                "Start condition not satisfied - rejecting command",
                "CommandProcessor"
            );
            ActionResultInternal res;
            res.command_id = cmd.command_id();
            res.agent_id = cmd.agent_id();
            res.task_id = cmd.task_id();
            res.step_id = cmd.step_id();
            res.status = static_cast<int>(fleet::v1::ACTION_STATUS_REJECTED);
            res.error = "Start condition not satisfied";
            res.completed_at_ms = now_ms();
            send_action_result(res);
            return;
        }

        if (result == PreconditionEvaluator::Result::NEED_SERVER) {
            // Need to query server for multi-robot state
            // For now, treat as not satisfied
            log.warn("Multi-robot condition needs server query");
            task_log_sender_->warn(
                "Multi-robot condition needs server query",
                "CommandProcessor"
            );
        }
    }

    // Build action request
    ActionRequest request;
    request.command_id = cmd.command_id();
    request.agent_id = cmd.agent_id();
    request.task_id = cmd.task_id();
    request.step_id = cmd.step_id();
    request.action_type = cmd.action_type();
    request.action_server = cmd.action_server();
    request.params_json = std::string(cmd.params().begin(), cmd.params().end());
    request.timeout_sec = cmd.timeout_sec();
    request.deadline_ms = cmd.deadline_ms();

    // Update execution context
    update_execution_context(
        cmd.agent_id(),
        RobotExecutionState::EXECUTING_ACTION,
        cmd.command_id(),
        cmd.task_id(),
        cmd.step_id(),
        cmd.action_type()
    );

    // Store action info for detailed logging on completion
    {
        CommandStateTransitions transitions;
        transitions.during_states.assign(cmd.during_states().begin(), cmd.during_states().end());
        transitions.success_states.assign(cmd.success_states().begin(), cmd.success_states().end());
        transitions.failure_states.assign(cmd.failure_states().begin(), cmd.failure_states().end());
        transitions.action_type = cmd.action_type();
        transitions.action_server = cmd.action_server();
        transitions.goal_params = request.params_json;
        std::lock_guard<std::mutex> lock(command_states_mutex_);
        command_states_[cmd.command_id()] = std::move(transitions);
    }

    bool hasCustomStates = cmd.during_states_size() > 0 ||
                           cmd.success_states_size() > 0 ||
                           cmd.failure_states_size() > 0;

    // Update state tracker: action started
    std::string current_state = "executing";
    if (state_tracker_mgr_) {
        auto tracker = state_tracker_mgr_->get_tracker(cmd.agent_id());
        if (tracker) {
            if (cmd.during_states_size() > 0) {
                std::vector<std::string> during(cmd.during_states().begin(), cmd.during_states().end());
                tracker->set_during_states(during);
            } else {
                tracker->on_action_start(cmd.action_type());
            }
            current_state = tracker->current_state();
        }
    }

    // Send immediate state update to server (don't wait for heartbeat)
    send_immediate_state_update(
        cmd.agent_id(),
        current_state,
        true,  // is_executing
        cmd.task_id(),
        cmd.step_id()
    );

    // Log action execution start
    task_log_sender_->info(
        "Starting action execution",
        "CommandProcessor",
        {
            {"action_type", request.action_type},
            {"action_server", request.action_server},
            {"params", request.params_json.substr(0, 200)}  // Truncate params for logging
        }
    );

    // Execute
    if (!executor->execute(request)) {
        log.error("Failed to start action for command {}", cmd.command_id());
        task_log_sender_->error(
            "Failed to start action execution",
            "CommandProcessor",
            {{"reason", "executor->execute() returned false"}}
        );

        update_execution_context(cmd.agent_id(), RobotExecutionState::ERROR);

        ActionResultInternal result;
        result.command_id = cmd.command_id();
        result.agent_id = cmd.agent_id();
        result.task_id = cmd.task_id();
        result.step_id = cmd.step_id();
        result.status = static_cast<int>(fleet::v1::ACTION_STATUS_REJECTED);
        result.error = "Failed to start action";
        result.completed_at_ms = now_ms();
        send_action_result(result);
    } else {
        task_log_sender_->info(
            "Action execution started successfully",
            "CommandProcessor"
        );
    }
}

void CommandProcessor::handle_cancel_command(const fleet::v1::CancelCommand& cmd) {
    log.info("Cancel command: {} for robot {} (reason: {})",
             cmd.command_id(), cmd.agent_id(), cmd.reason());

    auto* executor = get_executor(cmd.agent_id());
    if (executor && executor->is_executing()) {
        if (executor->current_command_id() == cmd.command_id()) {
            executor->cancel(cmd.reason());
        }
    }
}

void CommandProcessor::handle_deploy_graph(const fleet::v1::DeployGraphRequest& req) {
    const auto& graph = req.graph();
    const auto& meta = graph.metadata();

    // Detailed graph logging
    log.info("[GRAPH] ════════════════════════════════════════════════");
    log.info("[GRAPH] 📥 Received Action Graph");
    log.info("[GRAPH]   ID: {}", meta.id());
    log.info("[GRAPH]   Name: {}", meta.name());
    log.info("[GRAPH]   Version: {}", meta.version());
    log.info("[GRAPH]   Entry: {}", graph.entry_point());

    // Truncate checksum for display
    std::string checksum_preview = graph.checksum();
    if (checksum_preview.length() > 16) {
        checksum_preview = checksum_preview.substr(0, 16) + "...";
    }
    log.info("[GRAPH]   Checksum: {}", checksum_preview);
    log.info("[GRAPH] ────────────────────────────────────────────────");

    // Log each vertex
    int step_num = 1;
    for (const auto& vertex : graph.vertices()) {
        if (vertex.type() == fleet::v1::VERTEX_TYPE_STEP) {
            const auto& step = vertex.step();

            // Step type string
            std::string step_type_str;
            switch (step.step_type()) {
                case fleet::v1::STEP_TYPE_ACTION: step_type_str = "action"; break;
                case fleet::v1::STEP_TYPE_WAIT: step_type_str = "wait"; break;
                case fleet::v1::STEP_TYPE_CONDITION: step_type_str = "condition"; break;
                default: step_type_str = "unknown"; break;
            }

            log.info("[GRAPH]   [{:02d}] {} ({})", step_num++, vertex.id(), step_type_str);

            if (step.has_action()) {
                const auto& action = step.action();
                log.info("[GRAPH]       └─ Action: {}", action.action_type());
                log.info("[GRAPH]          Server: {}", action.action_server());
                log.info("[GRAPH]          Timeout: {:.1f}s", action.timeout_sec());

                // Log params with variable reference highlighting
                std::string params(action.goal_params().begin(), action.goal_params().end());
                if (!params.empty()) {
                    // Truncate for display
                    std::string params_preview = params;
                    if (params_preview.length() > 60) {
                        params_preview = params_preview.substr(0, 57) + "...";
                    }

                    if (params.find("${") != std::string::npos) {
                        log.info("[GRAPH]          Params: {} [📎 USES VARIABLES]", params_preview);
                    } else {
                        log.info("[GRAPH]          Params: {}", params_preview);
                    }
                }

            }

            // Log start conditions if present
            if (step.start_conditions_size() > 0) {
                log.info("[GRAPH]          ⏳ Start conditions: {} total", step.start_conditions_size());
                for (const auto& cond : step.start_conditions()) {
                    log.info("[GRAPH]             - {}: {} {} {}",
                             cond.id(), cond.target_type(), cond.state_operator(), cond.state());
                }
            }
        } else if (vertex.type() == fleet::v1::VERTEX_TYPE_TERMINAL) {
            std::string terminal_str = (vertex.terminal().terminal_type() == fleet::v1::TERMINAL_TYPE_SUCCESS)
                                       ? "✓ success" : "✗ failure";
            log.info("[GRAPH]   [END] {} ({})", vertex.id(), terminal_str);
        }
    }

    log.info("[GRAPH] ════════════════════════════════════════════════");

    // Store graph
    bool success = graph_storage_.store(graph);
    log.info("[GRAPH] Storage: {}", success ? "✓ Saved" : "✗ Failed");

    send_deploy_response(
        req.correlation_id(),
        success,
        meta.id(),
        meta.version(),
        success ? "" : "Failed to store graph"
    );
}

void CommandProcessor::handle_ping(const fleet::v1::PingRequest& ping) {
    send_pong(ping.ping_id(), ping.timestamp_ms());
}

void CommandProcessor::handle_fleet_state_broadcast(const fleet::v1::FleetStateBroadcast& broadcast) {
    std::unordered_map<std::string, int> states;
    std::unordered_map<std::string, bool> executing;
    std::unordered_map<std::string, float> staleness;
    std::unordered_map<std::string, bool> online;

    for (const auto& agent : broadcast.agents()) {
        // Skip self
        if (agent.agent_id() == agent_id_) {
            continue;
        }

        // Parse state string to int
        std::string state_str = agent.state();
        int state_int = 0;  // Unknown
        if (state_str == "idle") state_int = 1;
        else if (state_str == "executing") state_int = 2;
        else if (state_str == "error") state_int = 3;
        else if (state_str == "charging") state_int = 4;
        else if (state_str == "manual") state_int = 5;
        else if (state_str == "emergency") state_int = 6;

        states[agent.agent_id()] = state_int;
        executing[agent.agent_id()] = agent.is_executing();
        staleness[agent.agent_id()] = agent.staleness_sec();
        online[agent.agent_id()] = agent.is_online();
    }

    update_fleet_state(states, executing, staleness, online);

    log.debug("Fleet state updated: {} agents", broadcast.agents_size());
}

void CommandProcessor::on_action_result(const ActionResultInternal& result) {
    log.info("Action result: {} status={}", result.command_id, result.status);

    // Update task context and log result
    task_log_sender_->set_task_context(result.task_id, result.step_id, result.command_id);

    // Update execution context
    bool success = (result.status == static_cast<int>(fleet::v1::ACTION_STATUS_SUCCEEDED));

    // Log action completion
    std::string status_str;
    switch (result.status) {
        case static_cast<int>(fleet::v1::ACTION_STATUS_SUCCEEDED): status_str = "SUCCEEDED"; break;
        case static_cast<int>(fleet::v1::ACTION_STATUS_FAILED): status_str = "FAILED"; break;
        case static_cast<int>(fleet::v1::ACTION_STATUS_CANCELLED): status_str = "CANCELLED"; break;
        case static_cast<int>(fleet::v1::ACTION_STATUS_TIMEOUT): status_str = "TIMEOUT"; break;
        case static_cast<int>(fleet::v1::ACTION_STATUS_REJECTED): status_str = "REJECTED"; break;
        default: status_str = "UNKNOWN"; break;
    }

    // Retrieve stored action info for detailed logging
    CommandStateTransitions transitions;
    bool hasTransitions = false;
    {
        std::lock_guard<std::mutex> lock(command_states_mutex_);
        auto it = command_states_.find(result.command_id);
        if (it != command_states_.end()) {
            transitions = it->second;
            command_states_.erase(it);
            hasTransitions = true;
        }
    }

    // Format duration as human-readable
    int64_t duration_ms = result.completed_at_ms - result.started_at_ms;
    std::string duration_str;
    if (duration_ms >= 1000) {
        duration_str = std::to_string(duration_ms / 1000) + "." +
                      std::to_string((duration_ms % 1000) / 100) + "s";
    } else {
        duration_str = std::to_string(duration_ms) + "ms";
    }

    // Truncate result JSON for logging (max 300 chars)
    std::string result_preview = result.result_json;
    if (result_preview.length() > 300) {
        result_preview = result_preview.substr(0, 297) + "...";
    }

    if (success) {
        task_log_sender_->info(
            "Action completed successfully",
            "ActionExecutor",
            {
                {"status", status_str},
                {"duration", duration_str},
                {"duration_ms", std::to_string(duration_ms)},
                {"action_type", hasTransitions ? transitions.action_type : "unknown"},
                {"action_server", hasTransitions ? transitions.action_server : "unknown"},
                {"result", result_preview}
            }
        );
    } else {
        task_log_sender_->error(
            "Action failed: " + result.error,
            "ActionExecutor",
            {
                {"status", status_str},
                {"duration", duration_str},
                {"duration_ms", std::to_string(duration_ms)},
                {"action_type", hasTransitions ? transitions.action_type : "unknown"},
                {"action_server", hasTransitions ? transitions.action_server : "unknown"},
                {"error", result.error},
                {"result", result_preview}
            }
        );
    }

    update_execution_context(
        result.agent_id,
        success ? RobotExecutionState::IDLE : RobotExecutionState::ERROR
    );

    // Send result to server
    send_action_result(result);

    // Update state tracker
    std::string final_state = "idle";
    if (state_tracker_mgr_) {
        auto tracker = state_tracker_mgr_->get_tracker(result.agent_id);
        if (tracker) {
            if (hasTransitions) {
                if (success) {
                    tracker->set_success_states(transitions.success_states);
                } else {
                    tracker->set_failure_states(transitions.failure_states);
                }
            } else {
                tracker->on_action_complete(success, std::nullopt);
            }

            final_state = tracker->current_state();
            log.info("Robot {} state updated to: {} (action {})",
                     result.agent_id, final_state,
                     success ? "succeeded" : "failed");
        }
    }

    // Send immediate state update to server (don't wait for heartbeat)
    send_immediate_state_update(
        result.agent_id,
        final_state,
        false,  // is_executing = false (action completed)
        result.task_id,
        result.step_id
    );
}

void CommandProcessor::on_action_feedback(const std::string& agent_id, float progress) {
    // Send feedback to server
    auto* executor = get_executor(agent_id);
    if (executor && executor->is_executing()) {
        send_action_feedback(
            executor->current_command_id(),
            agent_id,
            executor->current_task_id(),
            executor->current_step_id(),
            progress
        );
    }
}

PreconditionEvaluator::Context CommandProcessor::build_precond_context(
    const std::string& agent_id) {

    PreconditionEvaluator::Context ctx;
    ctx.agent_id = agent_id;
    ctx.state_tracker_mgr = state_tracker_mgr_;
    ctx.execution_contexts = &execution_contexts_;

    // Copy fleet state
    {
        std::lock_guard<std::mutex> lock(fleet_state_mutex_);
        ctx.other_robot_states = fleet_states_;
        ctx.other_robot_executing = fleet_executing_;
        ctx.other_robot_staleness = fleet_staleness_;
        ctx.other_robot_online = fleet_online_;
    }

    return ctx;
}

ActionExecutor* CommandProcessor::get_executor(const std::string& agent_id) {
    std::lock_guard<std::mutex> lock(executors_mutex_);
    auto it = executors_.find(agent_id);
    if (it != executors_.end()) {
        return it->second.get();
    }
    return nullptr;
}

void CommandProcessor::update_execution_context(
    const std::string& agent_id,
    RobotExecutionState state,
    const std::string& command_id,
    const std::string& task_id,
    const std::string& step_id,
    const std::string& action_type) {

    ExecutionContextMap::accessor acc;
    if (execution_contexts_.find(acc, agent_id)) {
        acc->second.state.store(state);
        if (!command_id.empty()) acc->second.current_command_id = command_id;
        if (!task_id.empty()) acc->second.current_task_id = task_id;
        if (!step_id.empty()) acc->second.current_step_id = step_id;
        if (!action_type.empty()) acc->second.current_action_type = action_type;

        if (state == RobotExecutionState::EXECUTING_ACTION) {
            acc->second.action_started_at = std::chrono::steady_clock::now();
        }
    }

}

// ============================================================
// Result Sending Methods
// ============================================================

void CommandProcessor::send_action_result(const ActionResultInternal& result) {
    auto msg = std::make_shared<fleet::v1::AgentMessage>();
    msg->set_agent_id(agent_id_);
    msg->set_timestamp_ms(now_ms());

    auto* action_result = msg->mutable_action_result();
    action_result->set_command_id(result.command_id);
    action_result->set_agent_id(result.agent_id);
    action_result->set_task_id(result.task_id);
    action_result->set_step_id(result.step_id);
    action_result->set_status(static_cast<fleet::v1::ActionStatus>(result.status));
    action_result->set_result(result.result_json);
    action_result->set_error(result.error);
    action_result->set_started_at_ms(result.started_at_ms);
    action_result->set_completed_at_ms(result.completed_at_ms);

    OutboundMessage out;
    out.message = msg;
    out.created_at = std::chrono::steady_clock::now();
    out.priority = 10;  // High priority for results

    quic_outbound_queue_.push(std::move(out));
}

void CommandProcessor::send_action_feedback(
    const std::string& command_id,
    const std::string& agent_id,
    const std::string& task_id,
    const std::string& step_id,
    float progress) {

    auto msg = std::make_shared<fleet::v1::AgentMessage>();
    msg->set_agent_id(agent_id_);
    msg->set_timestamp_ms(now_ms());

    auto* feedback = msg->mutable_action_feedback();
    feedback->set_command_id(command_id);
    feedback->set_agent_id(agent_id);
    feedback->set_task_id(task_id);
    feedback->set_step_id(step_id);
    feedback->set_progress(progress);
    feedback->set_timestamp_ms(now_ms());

    OutboundMessage out;
    out.message = msg;
    out.created_at = std::chrono::steady_clock::now();
    out.priority = 5;  // Medium priority for feedback

    quic_outbound_queue_.push(std::move(out));
}

void CommandProcessor::send_immediate_state_update(
    const std::string& agent_id,
    const std::string& state_name,
    bool is_executing,
    const std::string& task_id,
    const std::string& step_id) {

    auto msg = std::make_shared<fleet::v1::AgentMessage>();
    msg->set_agent_id(agent_id_);
    msg->set_timestamp_ms(now_ms());

    // Send as heartbeat message for immediate state visibility
    auto* heartbeat = msg->mutable_heartbeat();
    heartbeat->set_agent_id(agent_id);
    heartbeat->set_state(state_name);
    heartbeat->set_is_executing(is_executing);
    if (!task_id.empty()) {
        heartbeat->set_current_task_id(task_id);
    }
    if (!step_id.empty()) {
        heartbeat->set_current_step_id(step_id);
    }

    OutboundMessage out;
    out.message = msg;
    out.created_at = std::chrono::steady_clock::now();
    out.priority = 15;  // High priority for immediate state updates

    quic_outbound_queue_.push(std::move(out));

    log.debug("Sent immediate state update: agent={} state={} executing={}",
              agent_id, state_name, is_executing);
}

void CommandProcessor::send_deploy_response(
    const std::string& correlation_id,
    bool success,
    const std::string& graph_id,
    int version,
    const std::string& error) {

    auto msg = std::make_shared<fleet::v1::AgentMessage>();
    msg->set_agent_id(agent_id_);
    msg->set_timestamp_ms(now_ms());

    auto* response = msg->mutable_deploy_response();
    response->set_correlation_id(correlation_id);
    response->set_success(success);
    response->set_graph_id(graph_id);
    response->set_deployed_version(version);
    if (!error.empty()) {
        response->set_error(error);
    }

    OutboundMessage out;
    out.message = msg;
    out.created_at = std::chrono::steady_clock::now();
    out.priority = 10;

    quic_outbound_queue_.push(std::move(out));
}

void CommandProcessor::send_pong(const std::string& ping_id, int64_t server_timestamp) {
    auto msg = std::make_shared<fleet::v1::AgentMessage>();
    msg->set_agent_id(agent_id_);
    msg->set_timestamp_ms(now_ms());

    auto* pong = msg->mutable_pong();
    pong->set_ping_id(ping_id);
    pong->set_server_timestamp_ms(server_timestamp);
    pong->set_agent_timestamp_ms(now_ms());

    OutboundMessage out;
    out.message = msg;
    out.created_at = std::chrono::steady_clock::now();
    out.priority = 15;  // Highest priority for ping/pong

    quic_outbound_queue_.push(std::move(out));
}

}  // namespace executor
}  // namespace fleet_agent
