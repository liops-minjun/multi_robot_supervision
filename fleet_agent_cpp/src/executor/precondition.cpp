// Copyright 2026 Multi-Robot Supervision System
// Precondition Evaluator Implementation

#include "fleet_agent/executor/precondition.hpp"
#include "fleet_agent/core/logger.hpp"
#include "fleet_agent/state/state_tracker.hpp"

#include <algorithm>
#include <cctype>
#include <chrono>
#include <sstream>

namespace fleet_agent {
namespace executor {

namespace {
logging::ComponentLogger log("PreconditionEvaluator");

// Trim whitespace from string
std::string trim(const std::string& s) {
    auto start = s.find_first_not_of(" \t\n\r");
    if (start == std::string::npos) return "";
    auto end = s.find_last_not_of(" \t\n\r");
    return s.substr(start, end - start + 1);
}

// Convert string to lowercase
std::string to_lower(const std::string& s) {
    std::string result = s;
    std::transform(result.begin(), result.end(), result.begin(), ::tolower);
    return result;
}

// Parse state name to int
int parse_state(const std::string& state_str) {
    std::string lower = to_lower(state_str);
    if (lower == "idle" || lower == "robot_state_idle") return 1;
    if (lower == "executing" || lower == "robot_state_executing") return 2;
    if (lower == "error" || lower == "robot_state_error") return 3;
    if (lower == "charging" || lower == "robot_state_charging") return 4;
    if (lower == "manual" || lower == "robot_state_manual") return 5;
    if (lower == "emergency" || lower == "robot_state_emergency") return 6;
    return 0;  // Unknown
}

bool is_self_condition(const PreconditionEvaluator::StartConditionSpec& cond,
                       const std::string& robot_id) {
    if (!cond.quantifier.empty() && to_lower(cond.quantifier) != "self") {
        return false;
    }
    if (!cond.target_type.empty() && to_lower(cond.target_type) != "self") {
        return false;
    }
    if (!cond.robot_id.empty() && cond.robot_id != robot_id) {
        return false;
    }
    if (!cond.agent_id.empty()) {
        return false;
    }
    return true;
}

bool evaluate_state_condition(const PreconditionEvaluator::StartConditionSpec& cond,
                              const PreconditionEvaluator::Context& ctx) {
    if (!ctx.state_tracker_mgr || ctx.robot_id.empty()) {
        return false;
    }

    auto tracker = ctx.state_tracker_mgr->get_tracker(ctx.robot_id);
    if (!tracker) {
        return false;
    }

    std::string actual = to_lower(tracker->current_state());

    if (!cond.allowed_states.empty()) {
        bool found = false;
        for (const auto& state : cond.allowed_states) {
            if (to_lower(state) == actual) {
                found = true;
                break;
            }
        }
        if (to_lower(cond.state_operator) == "not_in") {
            return !found;
        }
        return found;
    }

    if (cond.state.empty()) {
        return true;
    }

    std::string expected = to_lower(cond.state);
    std::string op = to_lower(cond.state_operator);
    if (op.empty() || op == "==" || op == "eq") {
        return actual == expected;
    }
    if (op == "!=" || op == "ne") {
        return actual != expected;
    }
    return actual == expected;
}

}  // namespace

PreconditionEvaluator::PreconditionEvaluator() {
    // Initialize regex patterns
    // "self" pattern
    self_pattern_ = std::regex(R"(^\s*self\s*$)", std::regex::icase);

    // Robot pattern: "robot_id.field op value"
    // e.g., "robot_002.state == idle" or "robot_002.is_executing == false"
    robot_pattern_ = std::regex(
        R"(^\s*([a-zA-Z0-9_-]+)\.(state|is_executing|current_action)\s*(==|!=|<|>|<=|>=)\s*(.+)\s*$)",
        std::regex::icase
    );

    // Variable pattern: "$var_name.field op value"
    // e.g., "$prev_step.success == true"
    variable_pattern_ = std::regex(
        R"(^\s*\$([a-zA-Z0-9_]+)\.?([a-zA-Z0-9_]*)\s*(==|!=|<|>|<=|>=)\s*(.+)\s*$)"
    );

}

PreconditionEvaluator::~PreconditionEvaluator() = default;

bool PreconditionEvaluator::is_local_condition(const std::string& condition) const {
    std::string trimmed = trim(condition);

    // "self" is always local
    if (std::regex_match(trimmed, self_pattern_)) {
        return true;
    }

    // Variable conditions are local
    if (trimmed[0] == '$') {
        return true;
    }

    // Robot conditions about other robots need server
    std::smatch match;
    if (std::regex_match(trimmed, match, robot_pattern_)) {
        // Check if it's about another robot
        return false;  // Multi-robot conditions need server
    }

    return true;  // Default to local for unknown patterns
}

std::optional<std::string> PreconditionEvaluator::get_required_robot(
    const std::string& condition) const {

    std::string trimmed = trim(condition);

    std::smatch match;
    if (std::regex_match(trimmed, match, robot_pattern_)) {
        return match[1].str();  // Return robot ID
    }

    return std::nullopt;
}

PreconditionEvaluator::ParsedCondition PreconditionEvaluator::parse(
    const std::string& condition) const {

    ParsedCondition parsed;
    std::string trimmed = trim(condition);

    // Check for negation
    if (!trimmed.empty() && trimmed[0] == '!') {
        parsed.negated = true;
        trimmed = trim(trimmed.substr(1));
    }

    // "self"
    if (std::regex_match(trimmed, self_pattern_)) {
        parsed.type = ParsedCondition::Type::SELF;
        return parsed;
    }

    // Robot condition
    std::smatch match;
    if (std::regex_match(trimmed, match, robot_pattern_)) {
        parsed.target = match[1].str();
        parsed.field = to_lower(match[2].str());
        parsed.op = match[3].str();
        parsed.value = trim(match[4].str());

        if (parsed.field == "state") {
            parsed.type = ParsedCondition::Type::ROBOT_STATE;
        } else {
            parsed.type = ParsedCondition::Type::ROBOT_FIELD;
        }
        return parsed;
    }

    // Variable condition
    if (std::regex_match(trimmed, match, variable_pattern_)) {
        parsed.type = ParsedCondition::Type::VARIABLE;
        parsed.target = match[1].str();
        parsed.field = match[2].str();
        parsed.op = match[3].str();
        parsed.value = trim(match[4].str());
        return parsed;
    }

    // Unknown - treat as complex
    parsed.type = ParsedCondition::Type::COMPLEX;
    parsed.value = trimmed;
    return parsed;
}

PreconditionEvaluator::Result PreconditionEvaluator::check_start_condition(
    const std::string& start_condition,
    const std::string& robot_id,
    const Context& ctx) {

    if (start_condition.empty()) {
        return Result::SATISFIED;  // No condition = always proceed
    }

    log.debug("Checking condition '{}' for robot {}", start_condition, robot_id);

    auto parsed = parse(start_condition);
    return evaluate_parsed(parsed, ctx);
}

PreconditionEvaluator::Result PreconditionEvaluator::check_start_conditions(
    const std::vector<StartConditionSpec>& conditions,
    const Context& ctx) {

    if (conditions.empty()) {
        return Result::SATISFIED;
    }

    bool result = true;
    for (size_t i = 0; i < conditions.size(); ++i) {
        const auto& cond = conditions[i];
        std::string op = to_lower(cond.operator_name);
        if (op.empty()) {
            op = "and";
        }

        if (!is_self_condition(cond, ctx.robot_id)) {
            return Result::NEED_SERVER;
        }

        bool passed = true;
        if (!cond.state.empty() || !cond.allowed_states.empty() ||
            cond.max_staleness_sec > 0.0 || cond.require_online) {
            passed = evaluate_state_condition(cond, ctx);
        }

        if (i == 0) {
            result = passed;
        } else if (op == "or") {
            result = result || passed;
        } else {
            result = result && passed;
        }

        if (!result && op != "or") {
            break;
        }
    }

    return result ? Result::SATISFIED : Result::NOT_SATISFIED;
}

PreconditionEvaluator::Result PreconditionEvaluator::evaluate(
    const std::string& expression,
    const Context& ctx) {

    auto parsed = parse(expression);
    return evaluate_parsed(parsed, ctx);
}

PreconditionEvaluator::Result PreconditionEvaluator::evaluate_parsed(
    const ParsedCondition& cond,
    const Context& ctx) {

    bool result = false;

    switch (cond.type) {
        case ParsedCondition::Type::SELF:
            result = true;
            break;

        case ParsedCondition::Type::ROBOT_STATE:
            result = check_robot_state(cond.target, cond.value, cond.op, ctx);
            break;

        case ParsedCondition::Type::ROBOT_FIELD:
            result = check_robot_field(cond.target, cond.field, cond.value, cond.op, ctx);
            break;

        case ParsedCondition::Type::VARIABLE:
            result = check_variable(cond.target + "." + cond.field, cond.value, cond.op, ctx);
            break;

        case ParsedCondition::Type::COMPLEX:
            log.warn("Complex condition not supported: {}", cond.value);
            return Result::FAILED;
    }

    if (cond.negated) {
        result = !result;
    }

    return result ? Result::SATISFIED : Result::NOT_SATISFIED;
}

bool PreconditionEvaluator::check_robot_state(
    const std::string& robot_id,
    const std::string& expected_state,
    const std::string& op,
    const Context& ctx) {

    // First check local fleet state cache
    auto it = ctx.other_robot_states.find(robot_id);
    if (it == ctx.other_robot_states.end()) {
        // Fall back to local execution context if available
        if (ctx.execution_contexts) {
            ExecutionContextMap::const_accessor acc;
            if (ctx.execution_contexts->find(acc, robot_id)) {
                int actual_state = 0;
                switch (acc->second.state.load()) {
                    case RobotExecutionState::EXECUTING_ACTION:
                    case RobotExecutionState::WAITING_RESULT:
                        actual_state = parse_state("executing");
                        break;
                    case RobotExecutionState::ERROR:
                        actual_state = parse_state("error");
                        break;
                    case RobotExecutionState::WAITING_PRECONDITION:
                    case RobotExecutionState::IDLE:
                    default:
                        actual_state = parse_state("idle");
                        break;
                }
                int expected = parse_state(expected_state);
                return compare_numeric(actual_state, expected, op);
            }
        }

        log.debug("Robot state not found for {}, condition not satisfied", robot_id);
        return false;
    }

    int actual_state = it->second;
    int expected = parse_state(expected_state);
    return compare_numeric(actual_state, expected, op);
}

bool PreconditionEvaluator::check_robot_field(
    const std::string& robot_id,
    const std::string& field,
    const std::string& expected_value,
    const std::string& op,
    const Context& ctx) {

    if (field == "is_executing") {
        if (ctx.execution_contexts) {
            ExecutionContextMap::const_accessor acc;
            if (ctx.execution_contexts->find(acc, robot_id)) {
                const auto state = acc->second.state.load();
                bool actual = (state == RobotExecutionState::EXECUTING_ACTION ||
                               state == RobotExecutionState::WAITING_RESULT);
                bool expected = (to_lower(expected_value) == "true");
                if (op == "==") return actual == expected;
                if (op == "!=") return actual != expected;
            }
        }

        // Check fleet executing cache
        auto it = ctx.other_robot_executing.find(robot_id);
        if (it != ctx.other_robot_executing.end()) {
            bool actual = it->second;
            bool expected = (to_lower(expected_value) == "true");
            if (op == "==") return actual == expected;
            if (op == "!=") return actual != expected;
        }
    }

    if (field == "current_action") {
        if (ctx.execution_contexts) {
            ExecutionContextMap::const_accessor acc;
            if (ctx.execution_contexts->find(acc, robot_id)) {
                return compare(acc->second.current_action_type, expected_value, op);
            }
        }
    }

    log.debug("Field {} not found for robot {}", field, robot_id);
    return false;
}

bool PreconditionEvaluator::check_variable(
    const std::string& var_name,
    const std::string& expected_value,
    const std::string& op,
    const Context& ctx) {

    if (!ctx.variables) {
        return false;
    }

    auto it = ctx.variables->find(var_name);
    if (it == ctx.variables->end()) {
        log.debug("Variable {} not found", var_name);
        return false;
    }

    return compare(it->second, expected_value, op);
}

bool PreconditionEvaluator::compare(
    const std::string& actual,
    const std::string& expected,
    const std::string& op) {

    // Try numeric comparison first
    try {
        double a = std::stod(actual);
        double e = std::stod(expected);
        return compare_numeric(a, e, op);
    } catch (...) {
        // Fall through to string comparison
    }

    // String comparison
    std::string a = to_lower(actual);
    std::string e = to_lower(expected);

    if (op == "==") return a == e;
    if (op == "!=") return a != e;
    if (op == "<") return a < e;
    if (op == ">") return a > e;
    if (op == "<=") return a <= e;
    if (op == ">=") return a >= e;

    return false;
}

bool PreconditionEvaluator::compare_numeric(
    double actual,
    double expected,
    const std::string& op) {

    constexpr double epsilon = 1e-9;

    if (op == "==") return std::abs(actual - expected) < epsilon;
    if (op == "!=") return std::abs(actual - expected) >= epsilon;
    if (op == "<") return actual < expected;
    if (op == ">") return actual > expected;
    if (op == "<=") return actual <= expected;
    if (op == ">=") return actual >= expected;

    return false;
}

}  // namespace executor
}  // namespace fleet_agent
