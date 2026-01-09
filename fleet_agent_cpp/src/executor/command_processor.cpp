// Copyright 2026 Multi-Robot Supervision System
// Command Processor Implementation

#include "fleet_agent/executor/command_processor.hpp"
#include "fleet_agent/graph/storage.hpp"
#include "fleet_agent/graph/executor.hpp"
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

std::string status_to_outcome(int status) {
    switch (status) {
        case static_cast<int>(fleet::v1::ACTION_STATUS_SUCCEEDED):
            return "success";
        case static_cast<int>(fleet::v1::ACTION_STATUS_CANCELLED):
            return "cancelled";
        case static_cast<int>(fleet::v1::ACTION_STATUS_TIMEOUT):
            return "timeout";
        case static_cast<int>(fleet::v1::ACTION_STATUS_REJECTED):
            return "rejected";
        case static_cast<int>(fleet::v1::ACTION_STATUS_FAILED):
            return "failed";
        default:
            return "failed";
    }
}

std::string normalize_outcome_value(const std::string& value) {
    std::string lower;
    lower.reserve(value.size());
    std::transform(value.begin(), value.end(), std::back_inserter(lower), ::tolower);

    if (lower == "success" || lower == "succeeded") return "success";
    if (lower == "failed" || lower == "failure" || lower == "error") return "failed";
    if (lower == "aborted" || lower == "abort") return "aborted";
    if (lower == "cancelled" || lower == "canceled" || lower == "cancel") return "cancelled";
    if (lower == "timeout" || lower == "timed_out") return "timeout";
    if (lower == "rejected") return "rejected";
    return lower;
}

bool outcome_matches_value(const std::string& actual, const std::string& expected) {
    if (expected.empty()) {
        return true;
    }
    const auto actual_norm = normalize_outcome_value(actual);
    const auto expected_norm = normalize_outcome_value(expected);
    if (actual_norm == expected_norm) {
        return true;
    }
    if ((actual_norm == "failed" && expected_norm == "aborted") ||
        (actual_norm == "aborted" && expected_norm == "failed")) {
        return true;
    }
    return false;
}

struct ParsedEndState {
    std::string outcome;
    std::string condition;
    std::string state;
};

std::vector<ParsedEndState> parse_end_states(
    const std::string& graph_json,
    const std::string& step_id) {

    std::vector<ParsedEndState> results;
    if (graph_json.empty() || step_id.empty()) {
        return results;
    }

    try {
        auto root = nlohmann::json::parse(graph_json);
        if (!root.is_object() || !root.contains("vertices") || !root["vertices"].is_array()) {
            return results;
        }

        for (const auto& vertex : root["vertices"]) {
            if (!vertex.is_object()) {
                continue;
            }
            if (!vertex.contains("id") || !vertex["id"].is_string()) {
                continue;
            }
            if (vertex["id"].get<std::string>() != step_id) {
                continue;
            }
            if (!vertex.contains("step") || !vertex["step"].is_object()) {
                continue;
            }
            const auto& step = vertex["step"];
            if (!step.contains("end_states") || !step["end_states"].is_array()) {
                return results;
            }
            for (const auto& end_state : step["end_states"]) {
                if (!end_state.is_object()) {
                    continue;
                }
                ParsedEndState parsed;
                if (end_state.contains("outcome") && end_state["outcome"].is_string()) {
                    parsed.outcome = end_state["outcome"].get<std::string>();
                }
                if (end_state.contains("condition") && end_state["condition"].is_string()) {
                    parsed.condition = end_state["condition"].get<std::string>();
                }
                if (end_state.contains("state") && end_state["state"].is_string()) {
                    parsed.state = end_state["state"].get<std::string>();
                }
                if (!parsed.state.empty()) {
                    results.push_back(std::move(parsed));
                }
            }
            break;
        }
    } catch (...) {
        return results;
    }

    return results;
}
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
    , state_tracker_mgr_(state_tracker_mgr) {

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

    if (processor_thread_.joinable()) {
        processor_thread_.join();
    }

    log.info("Stopped processing thread");
}

void CommandProcessor::add_robot(const std::string& robot_id, const std::string& ros_namespace) {
    std::lock_guard<std::mutex> lock(executors_mutex_);

    if (executors_.find(robot_id) != executors_.end()) {
        log.warn("Robot {} already added", robot_id);
        return;
    }

    auto executor = std::make_unique<ActionExecutor>(
        node_,
        robot_id,
        ros_namespace,
        capability_store_,
        [this](const ActionResultInternal& result) {
            this->on_action_result(result);
        },
        [this](const std::string& robot_id, float progress) {
            this->on_action_feedback(robot_id, progress);
        }
    );

    executors_[robot_id] = std::move(executor);

    // Initialize execution context
    ExecutionContextMap::accessor acc;
    execution_contexts_.insert(acc, robot_id);
    acc->second = RobotExecutionContext();

    log.info("Added robot: {} (namespace: {})", robot_id, ros_namespace);
}

void CommandProcessor::remove_robot(const std::string& robot_id) {
    std::lock_guard<std::mutex> lock(executors_mutex_);

    auto it = executors_.find(robot_id);
    if (it != executors_.end()) {
        executors_.erase(it);
        log.info("Removed robot: {}", robot_id);
    }
}

void CommandProcessor::cancel_action(const std::string& robot_id, const std::string& reason) {
    auto* executor = get_executor(robot_id);
    if (executor && executor->is_executing()) {
        executor->cancel(reason);
        log.info("Cancelled action for robot {}: {}", robot_id, reason);
    } else {
        log.warn("No active action to cancel for robot {}", robot_id);
    }
}

void CommandProcessor::enqueue_execute_command(
    const fleet::v1::ExecuteCommand& cmd,
    const std::string& message_id) {
    handle_execute_command(cmd, message_id);
}

void CommandProcessor::enqueue_execute_graph(
    const fleet::v1::ExecuteGraphRequest& req) {
    handle_execute_graph(req);
}

void CommandProcessor::update_fleet_state(
    const std::unordered_map<std::string, int>& robot_states,
    const std::unordered_map<std::string, bool>& robot_executing) {

    std::lock_guard<std::mutex> lock(fleet_state_mutex_);
    fleet_states_ = robot_states;
    fleet_executing_ = robot_executing;
}

void CommandProcessor::set_server_query_callback(ServerQueryCallback callback) {
    server_query_callback_ = std::move(callback);
}

void CommandProcessor::process_loop() {
    log.debug("Process loop started");

    while (running_.load() && FLEET_AGENT_RUNNING) {
        InboundCommand cmd;

        // Try to get command from queue (with timeout to allow shutdown check)
        // Note: TBB concurrent_queue doesn't have blocking pop, so we use try_pop with sleep
        if (inbound_queue_.try_pop(cmd)) {
            try {
                handle_message(cmd);
            } catch (const std::exception& e) {
                log.error("Error processing message: {}", e.what());
            }
        } else {
            // No message, sleep briefly
            std::this_thread::sleep_for(std::chrono::milliseconds(10));
        }

        // Also check any pending preconditions for graph executions
        // (Hybrid control: retry waiting steps)
        for (auto it = active_graphs_.begin(); it != active_graphs_.end(); ++it) {
            if (it->second.waiting_for_precondition) {
                try_execute_graph_step(it->first);
            }
        }
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

        case fleet::v1::ServerMessage::kExecuteGraph:
            handle_execute_graph(server_msg.execute_graph());
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
                     server_msg.config_update().robot_id());
            break;

        default:
            log.warn("Unknown message payload type");
            break;
    }
}

void CommandProcessor::handle_execute_command(
    const fleet::v1::ExecuteCommand& cmd,
    const std::string& message_id) {

    log.info("Execute command: {} for robot {} (action: {})",
             cmd.command_id(), cmd.robot_id(), cmd.action_type());

    // Get executor for robot
    auto* executor = get_executor(cmd.robot_id());
    if (!executor) {
        log.error("No executor for robot: {}", cmd.robot_id());
        // Send failure result
        ActionResultInternal result;
        result.command_id = cmd.command_id();
        result.robot_id = cmd.robot_id();
        result.task_id = cmd.task_id();
        result.step_id = cmd.step_id();
        result.status = static_cast<int>(fleet::v1::ACTION_STATUS_REJECTED);
        result.error = "Robot not found";
        result.completed_at_ms = now_ms();
        send_action_result(result);
        return;
    }

    // Check start conditions for Hybrid control
    if (cmd.start_conditions_size() > 0) {
        auto ctx = build_precond_context(cmd.robot_id());
        std::vector<PreconditionEvaluator::StartConditionSpec> conditions;
        conditions.reserve(static_cast<size_t>(cmd.start_conditions_size()));
        for (const auto& cond : cmd.start_conditions()) {
            PreconditionEvaluator::StartConditionSpec spec;
            spec.id = cond.id();
            spec.operator_name = cond.operator_();
            spec.quantifier = cond.quantifier();
            spec.target_type = cond.target_type();
            spec.robot_id = cond.robot_id();
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
            ActionResultInternal res;
            res.command_id = cmd.command_id();
            res.robot_id = cmd.robot_id();
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
        }
    }

    // Build action request
    ActionRequest request;
    request.command_id = cmd.command_id();
    request.robot_id = cmd.robot_id();
    request.task_id = cmd.task_id();
    request.step_id = cmd.step_id();
    request.action_type = cmd.action_type();
    request.action_server = cmd.action_server();
    request.params_json = std::string(cmd.params().begin(), cmd.params().end());
    request.timeout_sec = cmd.timeout_sec();
    request.deadline_ms = cmd.deadline_ms();

    // Update execution context
    update_execution_context(
        cmd.robot_id(),
        RobotExecutionState::EXECUTING_ACTION,
        cmd.command_id(),
        cmd.task_id(),
        cmd.step_id(),
        cmd.action_type()
    );

    bool hasCustomStates = cmd.during_states_size() > 0 ||
                           cmd.success_states_size() > 0 ||
                           cmd.failure_states_size() > 0;

    if (hasCustomStates) {
        CommandStateTransitions transitions;
        transitions.during_states.assign(cmd.during_states().begin(), cmd.during_states().end());
        transitions.success_states.assign(cmd.success_states().begin(), cmd.success_states().end());
        transitions.failure_states.assign(cmd.failure_states().begin(), cmd.failure_states().end());
        std::lock_guard<std::mutex> lock(command_states_mutex_);
        command_states_[cmd.command_id()] = std::move(transitions);
    }

    // Update state tracker: action started
    if (state_tracker_mgr_) {
        auto tracker = state_tracker_mgr_->get_tracker(cmd.robot_id());
        if (tracker) {
            if (cmd.during_states_size() > 0) {
                std::vector<std::string> during(cmd.during_states().begin(), cmd.during_states().end());
                tracker->set_during_states(during);
            } else {
                tracker->on_action_start(cmd.action_type());
            }
        }
    }

    // Execute
    if (!executor->execute(request)) {
        log.error("Failed to start action for command {}", cmd.command_id());

        update_execution_context(cmd.robot_id(), RobotExecutionState::ERROR);

        ActionResultInternal result;
        result.command_id = cmd.command_id();
        result.robot_id = cmd.robot_id();
        result.task_id = cmd.task_id();
        result.step_id = cmd.step_id();
        result.status = static_cast<int>(fleet::v1::ACTION_STATUS_REJECTED);
        result.error = "Failed to start action";
        result.completed_at_ms = now_ms();
        send_action_result(result);
    }
}

void CommandProcessor::handle_cancel_command(const fleet::v1::CancelCommand& cmd) {
    log.info("Cancel command: {} for robot {} (reason: {})",
             cmd.command_id(), cmd.robot_id(), cmd.reason());

    auto* executor = get_executor(cmd.robot_id());
    if (executor && executor->is_executing()) {
        if (executor->current_command_id() == cmd.command_id()) {
            executor->cancel(cmd.reason());
        }
    }
}

void CommandProcessor::handle_deploy_graph(const fleet::v1::DeployGraphRequest& req) {
    log.info("Deploy graph: {} (correlation: {})",
             req.graph().metadata().id(), req.correlation_id());

    bool success = graph_storage_.store(req.graph());

    send_deploy_response(
        req.correlation_id(),
        success,
        req.graph().metadata().id(),
        req.graph().metadata().version(),
        success ? "" : "Failed to store graph"
    );
}

void CommandProcessor::handle_execute_graph(const fleet::v1::ExecuteGraphRequest& req) {
    log.info("Execute graph: {} for robot {} (execution: {})",
             req.graph_id(), req.robot_id(), req.execution_id());

    // Load graph from storage
    auto graph = graph_storage_.load(req.graph_id());
    if (!graph) {
        log.error("Graph not found: {}", req.graph_id());
        send_graph_status(
            req.execution_id(),
            req.graph_id(),
            req.robot_id(),
            static_cast<int>(fleet::v1::GRAPH_EXECUTION_FAILED),
            "",
            0,
            "Graph not found"
        );
        return;
    }

    // Create execution context
    GraphExecution exec;
    exec.execution_id = req.execution_id();
    exec.graph_id = req.graph_id();
    exec.robot_id = req.robot_id();
    exec.current_vertex_id = graph->entry_point();
    exec.current_step_index = 0;
    exec.started_at = std::chrono::steady_clock::now();
    exec.step_started_at = exec.started_at;
    exec.state = static_cast<int>(fleet::v1::GRAPH_EXECUTION_RUNNING);

    // Parse parameters
    if (!req.params().empty()) {
        try {
            auto params_json = nlohmann::json::parse(
                std::string(req.params().begin(), req.params().end()));
            for (auto& [key, value] : params_json.items()) {
                exec.variables[key] = value.dump();
            }
        } catch (...) {
            log.warn("Failed to parse execution params");
        }
    }

    // Store execution
    {
        tbb::concurrent_hash_map<std::string, GraphExecution>::accessor acc;
        active_graphs_.insert(acc, req.execution_id());
        acc->second = std::move(exec);
    }

    // Send running status
    send_graph_status(
        req.execution_id(),
        req.graph_id(),
        req.robot_id(),
        static_cast<int>(fleet::v1::GRAPH_EXECUTION_RUNNING),
        graph->entry_point(),
        0
    );

    // Try to execute first step
    try_execute_graph_step(req.execution_id());
}

void CommandProcessor::handle_ping(const fleet::v1::PingRequest& ping) {
    send_pong(ping.ping_id(), ping.timestamp_ms());
}

void CommandProcessor::on_action_result(const ActionResultInternal& result) {
    log.info("Action result: {} status={}", result.command_id, result.status);

    // Update execution context
    bool success = (result.status == static_cast<int>(fleet::v1::ACTION_STATUS_SUCCEEDED));
    update_execution_context(
        result.robot_id,
        success ? RobotExecutionState::IDLE : RobotExecutionState::ERROR
    );

    // Determine if this was part of a graph execution
    std::string execution_id;
    for (auto it = active_graphs_.begin(); it != active_graphs_.end(); ++it) {
        if (it->second.robot_id == result.robot_id &&
            !it->second.waiting_for_precondition) {
            execution_id = it->first;
            break;
        }
    }

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

    // Send result to server
    send_action_result(result);

    if (!execution_id.empty()) {
        // Graph step - advance to next, state updates handled in graph flow
        advance_graph_execution(execution_id, status_to_outcome(result.status), result.result_json, transitions, hasTransitions);
        return;
    }

    // Update state tracker: action completed (non-graph)
    if (state_tracker_mgr_) {
        auto tracker = state_tracker_mgr_->get_tracker(result.robot_id);
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

            log.info("Robot {} state updated to: {} (action {})",
                     result.robot_id, tracker->current_state(),
                     success ? "succeeded" : "failed");
        }
    }
}

void CommandProcessor::on_action_feedback(const std::string& robot_id, float progress) {
    // Send feedback to server
    auto* executor = get_executor(robot_id);
    if (executor && executor->is_executing()) {
        send_action_feedback(
            executor->current_command_id(),
            robot_id,
            executor->current_task_id(),
            executor->current_step_id(),
            progress
        );
    }
}

void CommandProcessor::try_execute_graph_step(const std::string& execution_id) {
    GraphExecution exec;
    {
        tbb::concurrent_hash_map<std::string, GraphExecution>::const_accessor acc;
        if (!active_graphs_.find(acc, execution_id)) {
            return;
        }
        exec = acc->second;
    }

    auto graph = graph_storage_.load(exec.graph_id);
    if (!graph) {
        complete_graph_execution(execution_id, false, "Graph not found");
        return;
    }

    if (!graph_executor_) {
        graph_executor_ = std::make_unique<graph::GraphExecutor>(
            state_tracker_mgr_, execution_contexts_);
    }

    graph::GraphExecutor::ExecutionContext ctx;
    ctx.execution_id = exec.execution_id;
    ctx.graph_id = exec.graph_id;
    ctx.robot_id = exec.robot_id;
    ctx.current_vertex_id = exec.current_vertex_id;
    ctx.current_step_index = exec.current_step_index;
    ctx.state = exec.state;
    ctx.variables = exec.variables;
    ctx.started_at = exec.started_at;
    ctx.step_started_at = exec.step_started_at;
    ctx.waiting_for_precondition = exec.waiting_for_precondition;
    ctx.waiting_condition = exec.waiting_condition;

    auto* executor = get_executor(exec.robot_id);
    if (!executor) {
        complete_graph_execution(execution_id, false, "Robot not found");
        return;
    }

    for (int safety = 0; safety < 100; ++safety) {
        auto vertexOpt = graph_executor_->get_vertex(*graph, ctx.current_vertex_id);
        if (!vertexOpt) {
            complete_graph_execution(execution_id, false, "Vertex not found");
            return;
        }

        const auto& vertex = *vertexOpt;
        if (graph_executor_->is_terminal(vertex)) {
            bool success = vertex.terminal().terminal_type() == fleet::v1::TERMINAL_TYPE_SUCCESS;
            complete_graph_execution(execution_id, success, "");
            return;
        }

        if (vertex.type() != fleet::v1::VERTEX_TYPE_STEP) {
            complete_graph_execution(execution_id, false, "Unsupported vertex type");
            return;
        }

        const auto& step = vertex.step();

        if (step.step_type() == fleet::v1::STEP_TYPE_CONDITION) {
            std::string next = graph_executor_->evaluate_condition(step.condition(), ctx);
            if (next.empty()) {
                complete_graph_execution(execution_id, false, "Condition did not match");
                return;
            }
            ctx.current_vertex_id = next;
            ctx.current_step_index++;
            ctx.step_started_at = std::chrono::steady_clock::now();
            exec.waiting_for_precondition = false;
            exec.waiting_condition.clear();
            send_graph_status(
                execution_id,
                exec.graph_id,
                exec.robot_id,
                static_cast<int>(fleet::v1::GRAPH_EXECUTION_RUNNING),
                ctx.current_vertex_id,
                ctx.current_step_index
            );
            continue;
        }

        if (step.step_type() == fleet::v1::STEP_TYPE_WAIT) {
            bool ready = false;
            if (step.wait().has_condition() && !step.wait().condition().empty()) {
                auto precondCtx = build_precond_context(exec.robot_id, &exec);
                auto result = precond_evaluator_.evaluate(step.wait().condition(), precondCtx);
                if (result == PreconditionEvaluator::Result::SATISFIED) {
                    ready = true;
                } else {
                    exec.waiting_for_precondition = true;
                    exec.waiting_condition = step.wait().condition();
                }
            } else if (step.wait().duration_sec() > 0) {
                auto elapsed = std::chrono::steady_clock::now() - ctx.step_started_at;
                auto duration = std::chrono::duration<double>(step.wait().duration_sec());
                if (elapsed >= duration) {
                    ready = true;
                } else {
                    exec.waiting_for_precondition = true;
                    exec.waiting_condition = "timer";
                }
            } else {
                ready = true;
            }

            if (!ready) {
                exec.current_vertex_id = ctx.current_vertex_id;
                exec.current_step_index = ctx.current_step_index;
                exec.variables = ctx.variables;
                exec.step_started_at = ctx.step_started_at;
                {
                    tbb::concurrent_hash_map<std::string, GraphExecution>::accessor acc;
                    if (active_graphs_.find(acc, execution_id)) {
                        acc->second = exec;
                    }
                }
                update_execution_context(exec.robot_id, RobotExecutionState::WAITING_PRECONDITION,
                                         "", exec.execution_id, exec.current_vertex_id, "");
                send_graph_status(
                    execution_id,
                    exec.graph_id,
                    exec.robot_id,
                    static_cast<int>(fleet::v1::GRAPH_EXECUTION_RUNNING),
                    ctx.current_vertex_id,
                    ctx.current_step_index
                );
                return;
            }

            graph_executor_->apply_step_result(ctx, ctx.current_vertex_id, "success", "{}");
            auto nextVertex = graph_executor_->get_next_step(ctx, *graph, "success");
            if (!nextVertex) {
                complete_graph_execution(execution_id, true, "");
                return;
            }
            exec.waiting_for_precondition = false;
            exec.waiting_condition.clear();
            send_graph_status(
                execution_id,
                exec.graph_id,
                exec.robot_id,
                static_cast<int>(fleet::v1::GRAPH_EXECUTION_RUNNING),
                ctx.current_vertex_id,
                ctx.current_step_index
            );
            continue;
        }

        if (step.step_type() != fleet::v1::STEP_TYPE_ACTION) {
            complete_graph_execution(execution_id, false, "Unsupported step type");
            return;
        }

        auto precondResult = graph_executor_->check_step_condition(vertex, ctx);
        if (precondResult == PreconditionEvaluator::Result::NOT_SATISFIED ||
            precondResult == PreconditionEvaluator::Result::NEED_SERVER) {
            exec.waiting_for_precondition = true;
            exec.waiting_condition = "start_conditions";
            exec.current_vertex_id = ctx.current_vertex_id;
            exec.current_step_index = ctx.current_step_index;
            exec.variables = ctx.variables;
            exec.step_started_at = ctx.step_started_at;
            {
                tbb::concurrent_hash_map<std::string, GraphExecution>::accessor acc;
                if (active_graphs_.find(acc, execution_id)) {
                    acc->second = exec;
                }
            }
            update_execution_context(exec.robot_id, RobotExecutionState::WAITING_PRECONDITION,
                                     "", exec.execution_id, exec.current_vertex_id, "");
            return;
        }

        exec.waiting_for_precondition = false;
        exec.waiting_condition.clear();

        auto requestOpt = graph_executor_->create_action_request(ctx, vertex);
        if (!requestOpt) {
            complete_graph_execution(execution_id, false, "Failed to build action request");
            return;
        }
        auto request = *requestOpt;

        bool hasCustomStates = step.during_states_size() > 0 ||
                               step.success_states_size() > 0 ||
                               step.failure_states_size() > 0;
        if (hasCustomStates) {
            CommandStateTransitions transitions;
            transitions.during_states.assign(step.during_states().begin(), step.during_states().end());
            transitions.success_states.assign(step.success_states().begin(), step.success_states().end());
            transitions.failure_states.assign(step.failure_states().begin(), step.failure_states().end());
            std::lock_guard<std::mutex> lock(command_states_mutex_);
            command_states_[request.command_id] = std::move(transitions);
        }

        if (state_tracker_mgr_) {
            auto tracker = state_tracker_mgr_->get_tracker(exec.robot_id);
            if (tracker) {
                if (step.during_states_size() > 0) {
                    std::vector<std::string> during(step.during_states().begin(),
                                                    step.during_states().end());
                    tracker->set_during_states(during);
                } else {
                    tracker->on_action_start(step.action().action_type());
                }
            }
        }

        update_execution_context(
            exec.robot_id,
            RobotExecutionState::EXECUTING_ACTION,
            request.command_id,
            request.task_id,
            request.step_id,
            request.action_type
        );

        if (!executor->execute(request)) {
            update_execution_context(exec.robot_id, RobotExecutionState::ERROR);
            complete_graph_execution(execution_id, false, "Failed to start action");
            return;
        }

        exec.current_vertex_id = ctx.current_vertex_id;
        exec.current_step_index = ctx.current_step_index;
        exec.variables = ctx.variables;
        exec.step_started_at = ctx.step_started_at;
        {
            tbb::concurrent_hash_map<std::string, GraphExecution>::accessor acc;
            if (active_graphs_.find(acc, execution_id)) {
                acc->second = exec;
            }
        }
        send_graph_status(
            execution_id,
            exec.graph_id,
            exec.robot_id,
            static_cast<int>(fleet::v1::GRAPH_EXECUTION_RUNNING),
            ctx.current_vertex_id,
            ctx.current_step_index
        );
        return;
    }

    complete_graph_execution(execution_id, false, "Graph execution loop overflow");
}

void CommandProcessor::advance_graph_execution(
    const std::string& execution_id,
    const std::string& outcome,
    const std::string& result_json,
    const CommandStateTransitions& transitions,
    bool has_transitions) {

    GraphExecution exec;
    {
        tbb::concurrent_hash_map<std::string, GraphExecution>::const_accessor acc;
        if (!active_graphs_.find(acc, execution_id)) {
            return;
        }
        exec = acc->second;
    }

    auto graph = graph_storage_.load(exec.graph_id);
    if (!graph) {
        complete_graph_execution(execution_id, false, "Graph not found");
        return;
    }

    if (!graph_executor_) {
        graph_executor_ = std::make_unique<graph::GraphExecutor>(
            state_tracker_mgr_, execution_contexts_);
    }

    graph::GraphExecutor::ExecutionContext ctx;
    ctx.execution_id = exec.execution_id;
    ctx.graph_id = exec.graph_id;
    ctx.robot_id = exec.robot_id;
    ctx.current_vertex_id = exec.current_vertex_id;
    ctx.current_step_index = exec.current_step_index;
    ctx.state = exec.state;
    ctx.variables = exec.variables;
    ctx.started_at = exec.started_at;
    ctx.step_started_at = exec.step_started_at;
    ctx.waiting_for_precondition = exec.waiting_for_precondition;
    ctx.waiting_condition = exec.waiting_condition;

    graph_executor_->apply_step_result(
        ctx,
        exec.current_vertex_id,
        outcome,
        result_json
    );

    std::string matched_condition;
    auto nextVertex = graph_executor_->get_next_step(ctx, *graph, outcome, &matched_condition);

    // Apply outcome-based state transition (graph execution only)
    if (state_tracker_mgr_) {
        auto tracker = state_tracker_mgr_->get_tracker(exec.robot_id);
        if (tracker) {
            std::string selected_state;
            const auto end_states = parse_end_states(graph->graph_json(), exec.current_vertex_id);

            if (!end_states.empty()) {
                if (!matched_condition.empty()) {
                    for (const auto& end_state : end_states) {
                        if (!outcome_matches_value(outcome, end_state.outcome)) {
                            continue;
                        }
                        if (end_state.condition == matched_condition) {
                            selected_state = end_state.state;
                            break;
                        }
                    }
                }

                if (selected_state.empty()) {
                    for (const auto& end_state : end_states) {
                        if (!outcome_matches_value(outcome, end_state.outcome)) {
                            continue;
                        }
                        if (end_state.condition.empty() ||
                            end_state.condition == "default" ||
                            end_state.condition == "else") {
                            selected_state = end_state.state;
                            break;
                        }
                    }
                }

                if (selected_state.empty()) {
                    for (const auto& end_state : end_states) {
                        if (outcome_matches_value(outcome, end_state.outcome)) {
                            selected_state = end_state.state;
                            break;
                        }
                    }
                }
            }

            const bool success = normalize_outcome_value(outcome) == "success";
            if (!selected_state.empty()) {
                if (success) {
                    tracker->set_success_states({selected_state});
                } else {
                    tracker->set_failure_states({selected_state});
                }
            } else if (has_transitions) {
                if (success) {
                    tracker->set_success_states(transitions.success_states);
                } else {
                    tracker->set_failure_states(transitions.failure_states);
                }
            } else {
                tracker->on_action_complete(success, std::nullopt);
            }
        }
    }

    if (!nextVertex) {
        complete_graph_execution(execution_id, normalize_outcome_value(outcome) == "success", "");
        return;
    }

    exec.current_vertex_id = ctx.current_vertex_id;
    exec.current_step_index = ctx.current_step_index;
    exec.variables = ctx.variables;
    exec.step_started_at = ctx.step_started_at;
    exec.waiting_for_precondition = false;
    exec.waiting_condition.clear();

    {
        tbb::concurrent_hash_map<std::string, GraphExecution>::accessor acc;
        if (active_graphs_.find(acc, execution_id)) {
            acc->second = exec;
        }
    }

    send_graph_status(
        execution_id,
        exec.graph_id,
        exec.robot_id,
        static_cast<int>(fleet::v1::GRAPH_EXECUTION_RUNNING),
        exec.current_vertex_id,
        exec.current_step_index
    );

    try_execute_graph_step(execution_id);
}

void CommandProcessor::complete_graph_execution(
    const std::string& execution_id,
    bool success,
    const std::string& error) {

    GraphExecution exec;
    {
        tbb::concurrent_hash_map<std::string, GraphExecution>::const_accessor acc;
        if (active_graphs_.find(acc, execution_id)) {
            exec = acc->second;
        }
    }

    active_graphs_.erase(execution_id);

    if (!exec.robot_id.empty()) {
        update_execution_context(exec.robot_id, RobotExecutionState::IDLE);
    }

    // Send status
    send_graph_status(
        execution_id,
        exec.graph_id,
        exec.robot_id,
        success
            ? static_cast<int>(fleet::v1::GRAPH_EXECUTION_COMPLETED)
            : static_cast<int>(fleet::v1::GRAPH_EXECUTION_FAILED),
        "",
        exec.current_step_index,
        error
    );
}

PreconditionEvaluator::Context CommandProcessor::build_precond_context(
    const std::string& robot_id,
    const GraphExecution* graph_exec) {

    PreconditionEvaluator::Context ctx;
    ctx.robot_id = robot_id;
    ctx.state_tracker_mgr = state_tracker_mgr_;
    ctx.execution_contexts = &execution_contexts_;

    if (graph_exec) {
        ctx.variables = &graph_exec->variables;
    }

    // Copy fleet state
    {
        std::lock_guard<std::mutex> lock(fleet_state_mutex_);
        ctx.other_robot_states = fleet_states_;
        ctx.other_robot_executing = fleet_executing_;
    }

    return ctx;
}

ActionExecutor* CommandProcessor::get_executor(const std::string& robot_id) {
    std::lock_guard<std::mutex> lock(executors_mutex_);
    auto it = executors_.find(robot_id);
    if (it != executors_.end()) {
        return it->second.get();
    }
    return nullptr;
}

void CommandProcessor::update_execution_context(
    const std::string& robot_id,
    RobotExecutionState state,
    const std::string& command_id,
    const std::string& task_id,
    const std::string& step_id,
    const std::string& action_type) {

    ExecutionContextMap::accessor acc;
    if (execution_contexts_.find(acc, robot_id)) {
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
    action_result->set_robot_id(result.robot_id);
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
    const std::string& robot_id,
    const std::string& task_id,
    const std::string& step_id,
    float progress) {

    auto msg = std::make_shared<fleet::v1::AgentMessage>();
    msg->set_agent_id(agent_id_);
    msg->set_timestamp_ms(now_ms());

    auto* feedback = msg->mutable_action_feedback();
    feedback->set_command_id(command_id);
    feedback->set_robot_id(robot_id);
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

void CommandProcessor::send_graph_status(
    const std::string& execution_id,
    const std::string& graph_id,
    const std::string& robot_id,
    int state,
    const std::string& current_vertex,
    int current_step_index,
    const std::string& error) {

    auto msg = std::make_shared<fleet::v1::AgentMessage>();
    msg->set_agent_id(agent_id_);
    msg->set_timestamp_ms(now_ms());

    auto* status = msg->mutable_graph_status();
    status->set_execution_id(execution_id);
    if (!graph_id.empty()) {
        status->set_graph_id(graph_id);
    }
    if (!robot_id.empty()) {
        status->set_robot_id(robot_id);
    }
    status->set_state(static_cast<fleet::v1::GraphExecutionState>(state));
    if (!current_vertex.empty()) {
        status->set_current_vertex_id(current_vertex);
    }
    if (current_step_index > 0) {
        status->set_current_step_index(current_step_index);
    }
    if (!error.empty()) {
        status->set_error(error);
    }
    status->set_updated_at_ms(now_ms());

    OutboundMessage out;
    out.message = msg;
    out.created_at = std::chrono::steady_clock::now();
    out.priority = 10;

    quic_outbound_queue_.push(std::move(out));
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
