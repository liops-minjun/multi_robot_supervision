// Copyright 2026 Multi-Robot Supervision System
// Precondition Evaluator - Start condition evaluation for Hybrid control

#pragma once

#include "robot_agent/core/types.hpp"

#include <optional>
#include <regex>
#include <string>
#include <unordered_map>
#include <vector>

namespace robot_agent {
namespace state { class StateTrackerManager; }
namespace executor {

/**
 * PreconditionEvaluator - Evaluates start_condition expressions.
 *
 * Supports Hybrid control model:
 * - "self" conditions: Evaluated locally (immediate)
 * - Robot state conditions: Check local state cache or query server
 * - Variable conditions: Check execution context variables
 *
 * Expression formats:
 *   "self"                       -> Always satisfied (local decision)
 *   "robot_002.state == idle"    -> Check other robot's state
 *   "robot_002.is_executing == false"
 *   "$prev_step.success == true" -> Check previous step result
 *
 * Usage:
 *   PreconditionEvaluator evaluator;
 *   auto result = evaluator.check_start_condition(
 *       "robot_002.state == idle", "robot_001", ctx);
 *
 *   if (result == Result::SATISFIED) {
 *       // Proceed with action
 *   } else if (result == Result::NOT_SATISFIED) {
 *       // Wait and retry later
 *   }
 */
class PreconditionEvaluator {
public:
    /**
     * Evaluation result.
     */
    enum class Result {
        SATISFIED,       // Condition met, proceed
        NOT_SATISFIED,   // Condition not met, wait
        FAILED,          // Condition cannot be satisfied (error)
        SKIP_STEP,       // Skip this step (e.g., conditional skip)
        NEED_SERVER      // Need to query server for multi-robot state
    };

    /**
     * Evaluation context containing all available data.
     */
    struct Context {
        std::string agent_id;  // Own robot ID
        state::StateTrackerManager* state_tracker_mgr{nullptr};
        const ExecutionContextMap* execution_contexts{nullptr};
        const std::unordered_map<std::string, std::string>* variables{nullptr};

        // For multi-robot coordination: other robots' states
        // This is populated from server state updates
        std::unordered_map<std::string, int> other_robot_states;
        std::unordered_map<std::string, bool> other_robot_executing;
        std::unordered_map<std::string, float> other_robot_staleness;  // staleness in seconds
        std::unordered_map<std::string, bool> other_robot_online;
    };

    struct StartConditionSpec {
        std::string id;
        std::string operator_name;
        std::string quantifier;
        std::string target_type;
        std::string agent_id;
        std::string state;
        std::string state_operator;
        std::vector<std::string> allowed_states;
        double max_staleness_sec{0.0};
        bool require_online{false};
        std::string message;
    };

    PreconditionEvaluator();
    ~PreconditionEvaluator();

    // ============================================================
    // Main Evaluation Methods
    // ============================================================

    /**
     * Check start_condition for Hybrid control.
     *
     * @param start_condition Condition expression
     * @param agent_id Own robot ID
     * @param ctx Evaluation context
     * @return Evaluation result
     */
    Result check_start_condition(
        const std::string& start_condition,
        const std::string& agent_id,
        const Context& ctx
    );

    Result check_start_conditions(
        const std::vector<StartConditionSpec>& conditions,
        const Context& ctx
    );

    /**
     * Evaluate a general expression.
     */
    Result evaluate(const std::string& expression, const Context& ctx);

    /**
     * Check if condition is a "self" type (locally decidable).
     *
     * @param condition Condition string
     * @return true if can be evaluated locally without server
     */
    bool is_local_condition(const std::string& condition) const;

    /**
     * Check if condition requires other robot's state.
     *
     * @param condition Condition string
     * @return Robot ID if multi-robot condition, empty otherwise
     */
    std::optional<std::string> get_required_robot(const std::string& condition) const;

private:
    /**
     * Parsed condition structure.
     */
    struct ParsedCondition {
        enum class Type {
            SELF,           // "self" - always satisfied
            ROBOT_STATE,    // "robot_002.state == idle"
            ROBOT_FIELD,    // "robot_002.is_executing == false"
            VARIABLE,       // "$prev.success == true"
            COMPLEX         // Complex expression (fallback)
        };

        Type type;
        std::string target;      // agent_id or variable name
        std::string field;       // state, is_executing, etc.
        std::string op;          // ==, !=, <, >, <=, >=
        std::string value;       // expected value
        bool negated{false};     // ! prefix
    };

    // Regex patterns for parsing
    std::regex self_pattern_;
    std::regex robot_pattern_;
    std::regex variable_pattern_;

    /**
     * Parse condition string into structured form.
     */
    ParsedCondition parse(const std::string& condition) const;

    /**
     * Evaluate parsed condition.
     */
    Result evaluate_parsed(const ParsedCondition& cond, const Context& ctx);

    /**
     * Check robot state condition against context.
     */
    bool check_robot_state(
        const std::string& agent_id,
        const std::string& expected_state,
        const std::string& op,
        const Context& ctx
    );

    /**
     * Check robot field (is_executing, etc.).
     */
    bool check_robot_field(
        const std::string& agent_id,
        const std::string& field,
        const std::string& expected_value,
        const std::string& op,
        const Context& ctx
    );

    /**
     * Check variable condition.
     */
    bool check_variable(
        const std::string& var_name,
        const std::string& expected_value,
        const std::string& op,
        const Context& ctx
    );

    /**
     * Compare values with operator.
     */
    bool compare(
        const std::string& actual,
        const std::string& expected,
        const std::string& op
    );

    bool compare_numeric(double actual, double expected, const std::string& op);
};

}  // namespace executor
}  // namespace robot_agent
