// Copyright 2026 Multi-Robot Supervision System
// Graph Executor - DAG traversal and execution

#pragma once

#include "fleet_agent/core/types.hpp"
#include "fleet_agent/executor/precondition.hpp"

#include <chrono>
#include <memory>
#include <optional>
#include <string>
#include <unordered_map>

// Forward declarations
namespace fleet {
namespace v1 {
class ActionGraph;
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
 * GraphExecutor - Manages Action Graph execution.
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
     * Execution context for a running graph.
     */
    struct ExecutionContext {
        std::string execution_id;
        std::string graph_id;
        std::string robot_id;

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
     * Start a new graph execution.
     *
     * @param execution_id Unique execution identifier
     * @param robot_id Robot to execute on
     * @param graph Action graph to execute
     * @param params Initial parameters
     * @return Execution context
     */
    ExecutionContext start_execution(
        const std::string& execution_id,
        const std::string& robot_id,
        const fleet::v1::ActionGraph& graph,
        const std::unordered_map<std::string, std::string>& params = {}
    );

    /**
     * Get next step to execute.
     *
     * Follows edges based on current step result.
     *
     * @param ctx Execution context
     * @param graph Action graph
     * @param outcome Outcome string for current step (e.g., "success", "failed")
     * @param matched_condition Optional output for matched conditional edge expression
     * @return Next vertex, or nullopt if terminal reached
     */
    std::optional<fleet::v1::Vertex> get_next_step(
        ExecutionContext& ctx,
        const fleet::v1::ActionGraph& graph,
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
     * Get vertex by ID from graph.
     */
    std::optional<fleet::v1::Vertex> get_vertex(
        const fleet::v1::ActionGraph& graph,
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
    build_vertex_map(const fleet::v1::ActionGraph& graph);

    /**
     * Find outgoing edge from vertex.
     */
    std::optional<std::string> find_next_vertex_id(
        const fleet::v1::ActionGraph& graph,
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
};

}  // namespace graph
}  // namespace fleet_agent
