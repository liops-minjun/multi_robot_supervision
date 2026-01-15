// Copyright 2026 Multi-Robot Supervision System
// Graph Executor Implementation

#include "fleet_agent/graph/executor.hpp"
#include "fleet_agent/core/logger.hpp"
#include "fleet_agent/state/state_tracker.hpp"

#include <algorithm>
#include <cctype>
#include <iterator>
#include <regex>

// Include generated proto headers
#include "fleet/v1/graphs.pb.h"
#include "fleet/v1/common.pb.h"

#include <nlohmann/json.hpp>

namespace fleet_agent {
namespace graph {

namespace {
logging::ComponentLogger log("GraphExecutor");

std::string normalize_outcome(const std::string& value) {
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

bool outcome_matches(const std::string& actual, const std::string& expected) {
    if (expected.empty()) {
        return true;
    }
    auto actual_norm = normalize_outcome(actual);
    auto expected_norm = normalize_outcome(expected);
    if (actual_norm == expected_norm) {
        return true;
    }
    if ((actual_norm == "failed" && expected_norm == "aborted") ||
        (actual_norm == "aborted" && expected_norm == "failed")) {
        return true;
    }
    return false;
}

struct EdgeConditionConfig {
    std::string outcome;
    std::string state;
};

EdgeConditionConfig parse_edge_condition(const std::string& raw) {
    EdgeConditionConfig cfg;
    if (raw.empty()) {
        return cfg;
    }

    if (raw.front() == '{') {
        try {
            auto parsed = nlohmann::json::parse(raw);
            if (parsed.is_object()) {
                if (parsed.contains("outcome") && parsed["outcome"].is_string()) {
                    cfg.outcome = parsed["outcome"].get<std::string>();
                }
                if (parsed.contains("state") && parsed["state"].is_string()) {
                    cfg.state = parsed["state"].get<std::string>();
                }
                return cfg;
            }
        } catch (...) {
            // Fall back to raw outcome string
        }
    }

    // Treat raw string as outcome
    cfg.outcome = raw;
    return cfg;
}

}

GraphExecutor::GraphExecutor(
    state::StateTrackerManager* state_tracker_mgr,
    ExecutionContextMap& execution_contexts)
    : state_tracker_mgr_(state_tracker_mgr)
    , execution_contexts_(execution_contexts) {

    log.info("Initialized");
}

GraphExecutor::~GraphExecutor() = default;

GraphExecutor::ExecutionContext GraphExecutor::start_execution(
    const std::string& execution_id,
    const std::string& agent_id,
    const fleet::v1::ActionGraph& graph,
    const std::unordered_map<std::string, std::string>& params) {

    ExecutionContext ctx;
    ctx.execution_id = execution_id;
    ctx.graph_id = graph.metadata().id();
    ctx.agent_id = agent_id;
    ctx.current_vertex_id = graph.entry_point();
    ctx.current_step_index = 0;
    ctx.state = static_cast<int>(fleet::v1::GRAPH_EXECUTION_RUNNING);
    ctx.variables = params;
    ctx.started_at = std::chrono::steady_clock::now();
    ctx.step_started_at = ctx.started_at;

    log.info("Started execution {} of graph {} for robot {}",
             execution_id, ctx.graph_id, agent_id);

    return ctx;
}

std::unordered_map<std::string, const fleet::v1::Vertex*>
GraphExecutor::build_vertex_map(const fleet::v1::ActionGraph& graph) {
    std::unordered_map<std::string, const fleet::v1::Vertex*> map;
    for (const auto& vertex : graph.vertices()) {
        map[vertex.id()] = &vertex;
    }
    return map;
}

std::optional<std::string> GraphExecutor::find_next_vertex_id(
    const fleet::v1::ActionGraph& graph,
    const std::string& from_vertex_id,
    fleet::v1::EdgeType edge_type) {

    for (const auto& edge : graph.edges()) {
        if (edge.from_vertex() == from_vertex_id && edge.type() == edge_type) {
            return edge.to_vertex();
        }
    }

    // If looking for on_success but not found, try unconditional edge
    if (edge_type == fleet::v1::EDGE_TYPE_ON_SUCCESS) {
        for (const auto& edge : graph.edges()) {
            if (edge.from_vertex() == from_vertex_id &&
                edge.type() == fleet::v1::EDGE_TYPE_UNKNOWN) {
                return edge.to_vertex();
            }
        }
    }

    return std::nullopt;
}

std::optional<fleet::v1::Vertex> GraphExecutor::get_vertex(
    const fleet::v1::ActionGraph& graph,
    const std::string& vertex_id) {

    for (const auto& vertex : graph.vertices()) {
        if (vertex.id() == vertex_id) {
            return vertex;
        }
    }
    return std::nullopt;
}

bool GraphExecutor::is_terminal(const fleet::v1::Vertex& vertex) {
    return vertex.type() == fleet::v1::VERTEX_TYPE_TERMINAL;
}

bool GraphExecutor::is_execution_complete(const ExecutionContext& ctx) {
    return ctx.state == static_cast<int>(fleet::v1::GRAPH_EXECUTION_COMPLETED) ||
           ctx.state == static_cast<int>(fleet::v1::GRAPH_EXECUTION_FAILED) ||
           ctx.state == static_cast<int>(fleet::v1::GRAPH_EXECUTION_CANCELLED);
}

std::optional<fleet::v1::Vertex> GraphExecutor::get_next_step(
    ExecutionContext& ctx,
    const fleet::v1::ActionGraph& graph,
    const std::string& outcome,
    std::string* matched_condition) {

    if (matched_condition) {
        matched_condition->clear();
    }

    const std::string normalized_outcome = normalize_outcome(outcome);
    std::optional<std::string> next_id;

    // Conditional edges - match by outcome
    std::string default_id;
    for (const auto& edge : graph.edges()) {
        if (edge.from_vertex() != ctx.current_vertex_id) {
            continue;
        }
        if (edge.type() != fleet::v1::EDGE_TYPE_CONDITIONAL) {
            continue;
        }

        EdgeConditionConfig cfg = parse_edge_condition(edge.condition());

        // If no outcome specified, this is a default edge
        if (cfg.outcome.empty()) {
            if (default_id.empty()) {
                default_id = edge.to_vertex();
            }
            continue;
        }

        // Check if outcome matches
        if (outcome_matches(normalized_outcome, cfg.outcome)) {
            next_id = edge.to_vertex();
            if (matched_condition) {
                *matched_condition = cfg.outcome;
            }
            break;
        }
    }

    if (!next_id && !default_id.empty()) {
        next_id = default_id;
        if (matched_condition) {
            matched_condition->clear();
        }
    }

    if (!next_id) {
        // Fallback to standard edges
        fleet::v1::EdgeType edge_type = fleet::v1::EDGE_TYPE_ON_FAILURE;
        if (normalized_outcome == "success") {
            edge_type = fleet::v1::EDGE_TYPE_ON_SUCCESS;
        } else if (normalized_outcome == "timeout") {
            edge_type = fleet::v1::EDGE_TYPE_ON_TIMEOUT;
        }

        next_id = find_next_vertex_id(graph, ctx.current_vertex_id, edge_type);

        if (!next_id && edge_type == fleet::v1::EDGE_TYPE_ON_FAILURE) {
            next_id = find_next_vertex_id(graph, ctx.current_vertex_id,
                                          fleet::v1::EDGE_TYPE_ON_SUCCESS);
        }
    }

    if (!next_id) {
        log.debug("No outgoing edge from vertex {}", ctx.current_vertex_id);
        return std::nullopt;
    }

    // Get next vertex
    auto next_vertex = get_vertex(graph, *next_id);
    if (!next_vertex) {
        log.error("Next vertex {} not found", *next_id);
        return std::nullopt;
    }

    // Update context
    ctx.current_vertex_id = *next_id;
    ctx.current_step_index++;
    ctx.step_started_at = std::chrono::steady_clock::now();

    // Check if terminal
    if (is_terminal(*next_vertex)) {
        bool success = (next_vertex->terminal().terminal_type() ==
                       fleet::v1::TERMINAL_TYPE_SUCCESS);
        ctx.state = success
            ? static_cast<int>(fleet::v1::GRAPH_EXECUTION_COMPLETED)
            : static_cast<int>(fleet::v1::GRAPH_EXECUTION_FAILED);

        log.info("Execution {} reached terminal: {}",
                 ctx.execution_id, success ? "success" : "failure");
    }

    return next_vertex;
}

std::string GraphExecutor::substitute_variables(
    const std::string& input,
    const ExecutionContext& ctx) {

    std::string result = input;
    std::regex var_pattern(R"(\$\{([a-zA-Z0-9_.]+)\})");

    std::smatch match;
    while (std::regex_search(result, match, var_pattern)) {
        std::string var_name = match[1].str();
        std::string replacement;

        // Look up variable
        auto it = ctx.variables.find(var_name);
        if (it != ctx.variables.end()) {
            replacement = it->second;
        } else {
            log.warn("Variable {} not found", var_name);
            replacement = "";
        }

        result = result.replace(match.position(), match.length(), replacement);
    }

    return result;
}

std::optional<ActionRequest> GraphExecutor::create_action_request(
    const ExecutionContext& ctx,
    const fleet::v1::Vertex& vertex) {

    if (vertex.type() != fleet::v1::VERTEX_TYPE_STEP) {
        return std::nullopt;
    }

    const auto& step = vertex.step();
    if (step.step_type() != fleet::v1::STEP_TYPE_ACTION) {
        return std::nullopt;
    }

    const auto& action = step.action();

    ActionRequest request;
    request.command_id = ctx.execution_id + "_step_" + std::to_string(ctx.current_step_index);
    request.agent_id = ctx.agent_id;
    request.task_id = ctx.execution_id;
    request.step_id = vertex.id();
    request.action_type = action.action_type();
    request.action_server = action.action_server();
    request.timeout_sec = action.timeout_sec();

    // Substitute variables in parameters
    if (!action.goal_params().empty()) {
        std::string params_str(action.goal_params().begin(), action.goal_params().end());
        request.params_json = substitute_variables(params_str, ctx);
    }

    // Add step-level parameters
    for (const auto& [key, value] : step.params()) {
        // Merge into params_json if it's JSON
        try {
            auto params = nlohmann::json::parse(request.params_json.empty() ? "{}" : request.params_json);
            params[key] = substitute_variables(value, ctx);
            request.params_json = params.dump();
        } catch (...) {
            // Ignore JSON errors
        }
    }

    return request;
}

void GraphExecutor::apply_step_result(
    ExecutionContext& ctx,
    const std::string& step_id,
    const std::string& outcome,
    const std::string& result_json) {

    const std::string normalized_outcome = normalize_outcome(outcome);
    const bool success = normalized_outcome == "success";

    // Store result in variables
    ctx.variables[step_id + ".success"] = success ? "true" : "false";
    ctx.variables[step_id + ".result"] = result_json;
    if (!normalized_outcome.empty()) {
        ctx.variables[step_id + ".outcome"] = normalized_outcome;
    }

    // Parse result JSON and extract fields
    try {
        auto result = nlohmann::json::parse(result_json);
        for (auto& [key, value] : result.items()) {
            ctx.variables[step_id + "." + key] = value.dump();
        }
    } catch (...) {
        // Ignore JSON errors
    }

    // Update prev_step variables for convenience
    ctx.variables["prev_step.success"] = success ? "true" : "false";
    if (!normalized_outcome.empty()) {
        ctx.variables["prev_step.outcome"] = normalized_outcome;
    }
    ctx.variables["prev_step.result"] = result_json;

    log.debug("Applied step {} result: success={}", step_id, success);
}

std::string GraphExecutor::evaluate_condition(
    const fleet::v1::ConditionStep& condition,
    const ExecutionContext& ctx) {

    // Evaluate expression
    std::string expr = substitute_variables(condition.expression(), ctx);

    // Simple evaluation: check branches
    for (const auto& branch : condition.branches()) {
        std::string branch_cond = substitute_variables(branch.condition(), ctx);

        // Simple string equality check
        // In production, use a proper expression evaluator
        if (expr == branch_cond || branch_cond == "true" || branch_cond == "default") {
            return branch.next_vertex_id();
        }
    }

    // No branch matched - use first branch as default
    if (condition.branches_size() > 0) {
        return condition.branches(0).next_vertex_id();
    }

    log.warn("No condition branch matched for expression: {}", expr);
    return "";
}

executor::PreconditionEvaluator::Result GraphExecutor::check_step_condition(
    const fleet::v1::Vertex& vertex,
    const ExecutionContext& ctx) {

    if (vertex.type() != fleet::v1::VERTEX_TYPE_STEP) {
        return executor::PreconditionEvaluator::Result::SATISFIED;
    }

    const auto& step = vertex.step();
    if (step.pre_states_size() > 0) {
        std::string current_state;
        if (state_tracker_mgr_) {
            auto tracker = state_tracker_mgr_->get_tracker(ctx.agent_id);
            if (tracker) {
                current_state = tracker->current_state();
            }
        }
        if (current_state.empty()) {
            return executor::PreconditionEvaluator::Result::NOT_SATISFIED;
        }

        bool matched = false;
        for (const auto& state : step.pre_states()) {
            if (state == current_state) {
                matched = true;
                break;
            }
        }
        if (!matched) {
            return executor::PreconditionEvaluator::Result::NOT_SATISFIED;
        }
    }
    if (step.start_conditions_size() == 0) {
        return executor::PreconditionEvaluator::Result::SATISFIED;
    }

    // Build precondition context
    executor::PreconditionEvaluator::Context precond_ctx;
    precond_ctx.agent_id = ctx.agent_id;
    precond_ctx.state_tracker_mgr = state_tracker_mgr_;
    precond_ctx.execution_contexts = &execution_contexts_;
    precond_ctx.variables = &ctx.variables;

    std::vector<executor::PreconditionEvaluator::StartConditionSpec> conditions;
    conditions.reserve(static_cast<size_t>(step.start_conditions_size()));
    for (const auto& cond : step.start_conditions()) {
        executor::PreconditionEvaluator::StartConditionSpec spec;
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

    return precond_evaluator_.check_start_conditions(conditions, precond_ctx);
}

}  // namespace graph
}  // namespace fleet_agent
