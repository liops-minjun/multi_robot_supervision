// Copyright 2026 Multi-Robot Supervision System
// Parameter Field Source Types for Action Graph Parameters

#pragma once

#include <nlohmann/json.hpp>
#include <optional>
#include <string>
#include <unordered_map>
#include <variant>

namespace robot_agent {
namespace graph {

/**
 * ParameterSourceType defines how a parameter field gets its value.
 * Maps to the canonical schema ParameterSourceType.
 */
enum class ParameterSourceType {
    Constant,    // Direct constant value
    StepResult,  // Reference to previous step output
    Expression,  // Expression to evaluate
    Dynamic      // Dynamic parameter from telemetry
};

/**
 * ParameterFieldSource defines how a single field gets its value.
 * Maps to the canonical schema ParameterFieldSource.
 */
struct ParameterFieldSource {
    ParameterSourceType source{ParameterSourceType::Constant};

    // For constant source
    nlohmann::json value;

    // For step_result source
    std::string step_id;
    std::string result_field;  // e.g., "pose.position.x", "poses[0].position"

    // For expression source
    std::string expression;

    // For dynamic source (telemetry field path)
    std::string telemetry_path;
};

/**
 * ActionParamsConfig holds the complete parameter configuration for an action.
 * Maps to the canonical schema ActionParams.
 */
struct ActionParamsConfig {
    std::string source;  // "waypoint", "inline", "dynamic", "mapped"
    std::string waypoint_id;
    nlohmann::json data;  // Inline data values
    std::vector<std::string> fields;  // For dynamic: fields to request

    // Per-field source mapping (when source="mapped")
    std::unordered_map<std::string, ParameterFieldSource> field_sources;

    /**
     * Check if this config uses field source mapping.
     */
    bool uses_field_sources() const {
        return source == "mapped" || !field_sources.empty();
    }
};

// ============================================================
// JSON Parsing Helpers
// ============================================================

/**
 * Parse ParameterSourceType from string.
 */
inline ParameterSourceType parse_source_type(const std::string& str) {
    if (str == "constant") return ParameterSourceType::Constant;
    if (str == "step_result") return ParameterSourceType::StepResult;
    if (str == "expression") return ParameterSourceType::Expression;
    if (str == "dynamic") return ParameterSourceType::Dynamic;
    return ParameterSourceType::Constant;  // Default
}

/**
 * Parse ParameterFieldSource from JSON.
 */
inline ParameterFieldSource parse_field_source(const nlohmann::json& j) {
    ParameterFieldSource fs;

    if (j.contains("source") && j["source"].is_string()) {
        fs.source = parse_source_type(j["source"].get<std::string>());
    }

    if (j.contains("value")) {
        fs.value = j["value"];
    }

    if (j.contains("step_id") && j["step_id"].is_string()) {
        fs.step_id = j["step_id"].get<std::string>();
    }

    if (j.contains("result_field") && j["result_field"].is_string()) {
        fs.result_field = j["result_field"].get<std::string>();
    }

    if (j.contains("expression") && j["expression"].is_string()) {
        fs.expression = j["expression"].get<std::string>();
    }

    return fs;
}

/**
 * Parse ActionParamsConfig from JSON.
 */
inline ActionParamsConfig parse_action_params(const nlohmann::json& j) {
    ActionParamsConfig config;

    if (j.contains("source") && j["source"].is_string()) {
        config.source = j["source"].get<std::string>();
    }

    if (j.contains("waypoint_id") && j["waypoint_id"].is_string()) {
        config.waypoint_id = j["waypoint_id"].get<std::string>();
    }

    if (j.contains("data") && j["data"].is_object()) {
        config.data = j["data"];
    }

    if (j.contains("fields") && j["fields"].is_array()) {
        for (const auto& f : j["fields"]) {
            if (f.is_string()) {
                config.fields.push_back(f.get<std::string>());
            }
        }
    }

    if (j.contains("field_sources") && j["field_sources"].is_object()) {
        for (auto& [key, val] : j["field_sources"].items()) {
            config.field_sources[key] = parse_field_source(val);
        }
    }

    return config;
}

/**
 * Serialize ParameterFieldSource to JSON.
 */
inline nlohmann::json field_source_to_json(const ParameterFieldSource& fs) {
    nlohmann::json j;

    switch (fs.source) {
        case ParameterSourceType::Constant:
            j["source"] = "constant";
            j["value"] = fs.value;
            break;
        case ParameterSourceType::StepResult:
            j["source"] = "step_result";
            j["step_id"] = fs.step_id;
            j["result_field"] = fs.result_field;
            break;
        case ParameterSourceType::Expression:
            j["source"] = "expression";
            j["expression"] = fs.expression;
            break;
        case ParameterSourceType::Dynamic:
            j["source"] = "dynamic";
            break;
    }

    return j;
}

/**
 * Serialize ActionParamsConfig to JSON.
 */
inline nlohmann::json action_params_to_json(const ActionParamsConfig& config) {
    nlohmann::json j;

    j["source"] = config.source;

    if (!config.waypoint_id.empty()) {
        j["waypoint_id"] = config.waypoint_id;
    }

    if (!config.data.empty()) {
        j["data"] = config.data;
    }

    if (!config.fields.empty()) {
        j["fields"] = config.fields;
    }

    if (!config.field_sources.empty()) {
        nlohmann::json fs_json = nlohmann::json::object();
        for (const auto& [key, fs] : config.field_sources) {
            fs_json[key] = field_source_to_json(fs);
        }
        j["field_sources"] = fs_json;
    }

    return j;
}

}  // namespace graph
}  // namespace robot_agent
