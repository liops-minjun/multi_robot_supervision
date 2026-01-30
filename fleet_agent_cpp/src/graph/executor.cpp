// Copyright 2026 Multi-Robot Supervision System
// Graph Executor Implementation

#include "fleet_agent/graph/executor.hpp"
#include "fleet_agent/graph/field_source.hpp"
#include "fleet_agent/core/logger.hpp"
#include "fleet_agent/state/state_tracker.hpp"

#include <algorithm>
#include <cctype>
#include <iterator>
#include <regex>
#include <sstream>

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

std::string GraphExecutor::resolve_nested_path(
    const std::string& var_name,
    const ExecutionContext& ctx) {

    // First try direct lookup
    auto it = ctx.variables.find(var_name);
    if (it != ctx.variables.end()) {
        return it->second;
    }

    // Try to resolve nested JSON path (e.g., "step_id.pose.position.x")
    // Split by '.' and try to find the longest matching prefix that's a JSON value
    std::vector<std::string> parts;
    std::string current;
    for (char c : var_name) {
        if (c == '.') {
            if (!current.empty()) {
                parts.push_back(current);
                current.clear();
            }
        } else {
            current += c;
        }
    }
    if (!current.empty()) {
        parts.push_back(current);
    }

    // Try progressively longer prefixes to find a JSON value
    for (size_t prefix_len = 1; prefix_len < parts.size(); ++prefix_len) {
        std::string prefix;
        for (size_t i = 0; i < prefix_len; ++i) {
            if (i > 0) prefix += ".";
            prefix += parts[i];
        }

        auto prefix_it = ctx.variables.find(prefix);
        if (prefix_it != ctx.variables.end()) {
            // Found a variable - try to parse as JSON and navigate remaining path
            try {
                auto json_val = nlohmann::json::parse(prefix_it->second);

                // Navigate remaining path
                for (size_t i = prefix_len; i < parts.size(); ++i) {
                    if (json_val.is_object() && json_val.contains(parts[i])) {
                        json_val = json_val[parts[i]];
                    } else if (json_val.is_array()) {
                        // Try parsing as array index
                        try {
                            size_t idx = std::stoul(parts[i]);
                            if (idx < json_val.size()) {
                                json_val = json_val[idx];
                            } else {
                                return "";
                            }
                        } catch (...) {
                            return "";
                        }
                    } else {
                        return "";
                    }
                }

                // Return the final value
                if (json_val.is_string()) {
                    return json_val.get<std::string>();
                } else if (json_val.is_number()) {
                    return json_val.dump();
                } else {
                    return json_val.dump();
                }
            } catch (...) {
                // Not valid JSON, continue searching
            }
        }
    }

    return "";
}

std::string GraphExecutor::substitute_variables(
    const std::string& input,
    const ExecutionContext& ctx) {

    std::string result = input;
    std::regex var_pattern(R"(\$\{([a-zA-Z0-9_.]+)\})");

    std::smatch match;
    while (std::regex_search(result, match, var_pattern)) {
        std::string var_name = match[1].str();
        std::string replacement = resolve_nested_path(var_name, ctx);

        if (replacement.empty()) {
            log.warn("Variable {} not found or could not be resolved", var_name);
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

    // Parse and resolve parameters
    if (!action.goal_params().empty()) {
        std::string params_str(action.goal_params().begin(), action.goal_params().end());

        try {
            auto params_json = nlohmann::json::parse(params_str);

            // Check if params contains field_sources (canonical format)
            if (params_json.contains("field_sources") && params_json["field_sources"].is_object()) {
                // Parse as ActionParamsConfig and resolve field sources
                auto params_config = parse_action_params(params_json);

                log.info("Step {} has {} field_sources configured",
                         vertex.id(), params_config.field_sources.size());

                auto resolved = resolve_action_params(params_config, ctx);
                request.params_json = resolved.dump();

                log.info("Resolved params for step {}: {}",
                         vertex.id(), request.params_json);
            } else if (params_json.contains("data") && params_json["data"].is_object()) {
                // Inline data format - substitute variables in data values
                nlohmann::json data = params_json["data"];
                request.params_json = substitute_variables(data.dump(), ctx);
            } else {
                // Simple format - just substitute variables
                request.params_json = substitute_variables(params_str, ctx);
            }
        } catch (const std::exception& ex) {
            log.warn("Failed to parse params JSON, using raw substitution: {}", ex.what());
            request.params_json = substitute_variables(params_str, ctx);
        }
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
    size_t field_count = 0;
    try {
        auto result = nlohmann::json::parse(result_json);
        for (auto& [key, value] : result.items()) {
            ctx.variables[step_id + "." + key] = value.dump();
            field_count++;
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

    log.info("Applied step {} result: success={}, stored {} variables (e.g. {}.success, {}.message)",
             step_id, success, field_count + 3, step_id, step_id);
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

// ============================================================
// Field Source Resolution
// ============================================================

nlohmann::json GraphExecutor::resolve_field_source(
    const ParameterFieldSource& source,
    const ExecutionContext& ctx) {

    switch (source.source) {
        case ParameterSourceType::Constant:
            return source.value;

        case ParameterSourceType::StepResult: {
            // Build variable reference: step_id.result_field
            std::string var_ref = source.step_id;
            if (!source.result_field.empty()) {
                var_ref += "." + source.result_field;
            }

            log.info("Resolving step_result binding: {} -> {}.{}",
                     var_ref, source.step_id, source.result_field);

            std::string resolved = resolve_nested_path(var_ref, ctx);
            if (resolved.empty()) {
                log.warn("Failed to resolve step_result: {}.{}", source.step_id, source.result_field);
                return nullptr;
            }

            log.info("Resolved value: {}", resolved);

            // Try to parse as JSON (for nested objects/arrays)
            try {
                return nlohmann::json::parse(resolved);
            } catch (...) {
                // Return as string if not valid JSON
                return resolved;
            }
        }

        case ParameterSourceType::Expression: {
            // Substitute variables in expression and evaluate
            std::string expr = substitute_variables(source.expression, ctx);

            // Simple expression evaluation for common patterns
            // TODO: Full expression parser for complex expressions
            try {
                // Try to parse as number
                if (expr.find_first_not_of("0123456789.-+eE") == std::string::npos) {
                    if (expr.find('.') != std::string::npos || expr.find('e') != std::string::npos) {
                        return std::stod(expr);
                    } else {
                        return std::stoll(expr);
                    }
                }
                // Try to parse as JSON
                return nlohmann::json::parse(expr);
            } catch (...) {
                return expr;
            }
        }

        case ParameterSourceType::Dynamic:
            // Dynamic parameters would be resolved from telemetry
            // For now, return null and log warning
            log.warn("Dynamic parameter source not yet implemented");
            return nullptr;
    }

    return nullptr;
}

nlohmann::json GraphExecutor::resolve_action_params(
    const ActionParamsConfig& params,
    const ExecutionContext& ctx) {

    // Start with base data if present
    nlohmann::json result = params.data.empty() ? nlohmann::json::object() : params.data;

    // Apply field sources
    for (const auto& [field_path, source] : params.field_sources) {
        nlohmann::json value = resolve_field_source(source, ctx);

        if (!value.is_null()) {
            set_json_path(result, field_path, value);
            log.debug("Resolved field '{}' -> {}", field_path, value.dump());
        } else {
            log.warn("Field '{}' resolved to null", field_path);
        }
    }

    return result;
}

void GraphExecutor::set_json_path(
    nlohmann::json& target,
    const std::string& path,
    const nlohmann::json& value) {

    if (path.empty()) {
        target = value;
        return;
    }

    // Parse path components (e.g., "pose.position.x" or "poses[0].position")
    std::vector<std::variant<std::string, size_t>> components;
    std::string current;
    bool in_bracket = false;

    for (size_t i = 0; i < path.size(); ++i) {
        char c = path[i];

        if (c == '[') {
            if (!current.empty()) {
                components.push_back(current);
                current.clear();
            }
            in_bracket = true;
        } else if (c == ']') {
            if (in_bracket && !current.empty()) {
                try {
                    components.push_back(static_cast<size_t>(std::stoul(current)));
                } catch (...) {
                    // Invalid array index, treat as string
                    components.push_back(current);
                }
                current.clear();
            }
            in_bracket = false;
        } else if (c == '.' && !in_bracket) {
            if (!current.empty()) {
                components.push_back(current);
                current.clear();
            }
        } else {
            current += c;
        }
    }
    if (!current.empty()) {
        components.push_back(current);
    }

    if (components.empty()) {
        target = value;
        return;
    }

    // Navigate to parent and set value
    nlohmann::json* current_node = &target;

    for (size_t i = 0; i < components.size() - 1; ++i) {
        if (std::holds_alternative<std::string>(components[i])) {
            const std::string& key = std::get<std::string>(components[i]);

            // Check next component to determine if we need object or array
            if (!current_node->is_object()) {
                *current_node = nlohmann::json::object();
            }
            if (!current_node->contains(key)) {
                if (i + 1 < components.size() && std::holds_alternative<size_t>(components[i + 1])) {
                    (*current_node)[key] = nlohmann::json::array();
                } else {
                    (*current_node)[key] = nlohmann::json::object();
                }
            }
            current_node = &(*current_node)[key];

        } else {
            size_t idx = std::get<size_t>(components[i]);

            if (!current_node->is_array()) {
                *current_node = nlohmann::json::array();
            }
            // Extend array if needed
            while (current_node->size() <= idx) {
                current_node->push_back(nlohmann::json::object());
            }
            current_node = &(*current_node)[idx];
        }
    }

    // Set the final value
    const auto& last = components.back();
    if (std::holds_alternative<std::string>(last)) {
        const std::string& key = std::get<std::string>(last);
        if (!current_node->is_object()) {
            *current_node = nlohmann::json::object();
        }
        (*current_node)[key] = value;
    } else {
        size_t idx = std::get<size_t>(last);
        if (!current_node->is_array()) {
            *current_node = nlohmann::json::array();
        }
        while (current_node->size() <= idx) {
            current_node->push_back(nullptr);
        }
        (*current_node)[idx] = value;
    }
}

}  // namespace graph
}  // namespace fleet_agent
