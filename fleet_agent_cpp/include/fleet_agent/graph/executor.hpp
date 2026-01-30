// Copyright 2026 Multi-Robot Supervision System
// Behavior Tree Executor - DAG traversal and execution

#pragma once

#include "fleet_agent/core/types.hpp"
#include "fleet_agent/executor/precondition.hpp"
#include "fleet_agent/graph/field_source.hpp"

#include <chrono>
#include <memory>
#include <optional>
#include <string>
#include <unordered_map>

// Forward declarations
namespace fleet {
namespace v1 {
class BehaviorTree;
class Vertex;
class StepVertex;
class ConditionStep;
enum EdgeType : int;
enum GraphExecutionState : int;
}
}

namespace fleet_agent {
namespace state { class StateTrackerManager; }
namespace graph {

/**
 * GraphExecutor - Manages Behavior Tree execution.
 *
 * Handles DAG traversal, step execution, and transition logic:
 * - Entry point determination
 * - Edge following (on_success, on_failure, on_timeout)
 * - Condition evaluation
 * - Variable management (step results)
 * - Terminal detection
 *
 * Hybrid control integration:
 * - Local decisions for "self" conditions
 * - Server query for multi-robot conditions
 */
class GraphExecutor {
public:
    /**
     * Execution context for a running behavior tree.
     */
    struct ExecutionContext {
        std::string execution_id;
        std::string behavior_tree_id;
        std::string agent_id;

        // Current position
        std::string current_vertex_id;
        int current_step_index{0};
        int state{0};  // GraphExecutionState

        // Execution variables (step results, parameters)
        std::unordered_map<std::string, std::string> variables;

        // Timing
        std::chrono::steady_clock::time_point started_at;
        std::chrono::steady_clock::time_point step_started_at;

        // Waiting state for Hybrid control
        bool waiting_for_precondition{false};
        std::string waiting_condition;
    };

    /**
     * Constructor.
     */
    GraphExecutor(
        state::StateTrackerManager* state_tracker_mgr,
        ExecutionContextMap& execution_contexts
    );

    ~GraphExecutor();

    // ============================================================
    // Execution Control
    // ============================================================

    /**
     * Start a new behavior tree execution.
     *
     * @param execution_id Unique execution identifier
     * @param agent_id Robot to execute on
     * @param behavior_tree Behavior tree to execute
     * @param params Initial parameters
     * @return Execution context
     */
    ExecutionContext start_execution(
        const std::string& execution_id,
        const std::string& agent_id,
        const fleet::v1::BehaviorTree& behavior_tree,
        const std::unordered_map<std::string, std::string>& params = {}
    );

    /**
     * Get next step to execute.
     *
     * Follows edges based on current step result.
     *
     * @param ctx Execution context
     * @param behavior_tree Behavior tree
     * @param outcome Outcome string for current step (e.g., "success", "failed")
     * @param matched_condition Optional output for matched conditional edge expression
     * @return Next vertex, or nullopt if terminal reached
     */
    std::optional<fleet::v1::Vertex> get_next_step(
        ExecutionContext& ctx,
        const fleet::v1::BehaviorTree& behavior_tree,
        const std::string& outcome,
        std::string* matched_condition = nullptr
    );

    /**
     * Create ActionRequest from a vertex.
     *
     * @param ctx Execution context
     * @param vertex Vertex to execute
     * @return Action request, or nullopt if not an action step
     */
    std::optional<ActionRequest> create_action_request(
        const ExecutionContext& ctx,
        const fleet::v1::Vertex& vertex
    );

     /**
     * Apply step result to context.
     *
     * Updates variables with step output.
     */
    void apply_step_result(
        ExecutionContext& ctx,
        const std::string& step_id,
        const std::string& outcome,
        const std::string& result_json
    );

    // ============================================================
    // Condition Evaluation
    // ============================================================

    /**
     * Evaluate condition vertex.
     *
     * @param condition Condition step definition
     * @param ctx Execution context
     * @return ID of next vertex based on condition
     */
    std::string evaluate_condition(
        const fleet::v1::ConditionStep& condition,
        const ExecutionContext& ctx
    );

    /**
     * Check if current step's start condition is satisfied.
     *
     * For Hybrid control.
     */
    executor::PreconditionEvaluator::Result check_step_condition(
        const fleet::v1::Vertex& vertex,
        const ExecutionContext& ctx
    );

    // ============================================================
    // Graph Inspection
    // ============================================================

    /**
     * Get vertex by ID from behavior tree.
     */
    std::optional<fleet::v1::Vertex> get_vertex(
        const fleet::v1::BehaviorTree& behavior_tree,
        const std::string& vertex_id
    );

    /**
     * Check if vertex is terminal.
     */
    bool is_terminal(const fleet::v1::Vertex& vertex);

    /**
     * Check if execution is complete.
     */
    bool is_execution_complete(const ExecutionContext& ctx);

private:
    state::StateTrackerManager* state_tracker_mgr_{nullptr};
    ExecutionContextMap& execution_contexts_;
    executor::PreconditionEvaluator precond_evaluator_;

    /**
     * Build vertex lookup map for fast access.
     */
    std::unordered_map<std::string, const fleet::v1::Vertex*>
    build_vertex_map(const fleet::v1::BehaviorTree& behavior_tree);

    /**
     * Find outgoing edge from vertex.
     */
    std::optional<std::string> find_next_vertex_id(
        const fleet::v1::BehaviorTree& behavior_tree,
        const std::string& from_vertex_id,
        fleet::v1::EdgeType edge_type
    );

    /**
     * Substitute variables in string.
     *
     * Replaces ${var_name} with variable values.
     */
    std::string substitute_variables(
        const std::string& input,
        const ExecutionContext& ctx
    );

    /**
     * Resolve nested JSON path from variables.
     *
     * Supports paths like "step_id.pose.position.x" where
     * "step_id.result" contains a JSON object.
     *
     * @param var_name Variable name with potential nested path
     * @param ctx Execution context with variables
     * @return Resolved value as string, or empty string if not found
     */
    std::string resolve_nested_path(
        const std::string& var_name,
        const ExecutionContext& ctx
    );

    /**
     * Resolve a single field source to a JSON value.
     *
     * @param source Field source configuration
     * @param ctx Execution context with variables
     * @return Resolved JSON value
     */
    nlohmann::json resolve_field_source(
        const ParameterFieldSource& source,
        const ExecutionContext& ctx
    );

    /**
     * Resolve all field sources in params config to final JSON.
     *
     * Merges field_sources resolutions into data to produce
     * the final goal parameters JSON.
     *
     * @param params Action params configuration
     * @param ctx Execution context with variables
     * @return Final resolved JSON parameters
     */
    nlohmann::json resolve_action_params(
        const ActionParamsConfig& params,
        const ExecutionContext& ctx
    );

    /**
     * Set a nested JSON value by path.
     *
     * Supports paths like "pose.position.x" or "poses[0].position".
     *
     * @param target Target JSON object
     * @param path Dot-separated path
     * @param value Value to set
     */
    void set_json_path(
        nlohmann::json& target,
        const std::string& path,
        const nlohmann::json& value
    );
};

}  // namespace graph
}  // namespace fleet_agent
