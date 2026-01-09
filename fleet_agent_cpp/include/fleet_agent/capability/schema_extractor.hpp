// Copyright 2026 Multi-Robot Supervision System
// JSON Schema extractor from ROS2 message types

#pragma once

#include "fleet_agent/core/types.hpp"

#include <string>
#include <vector>

#include <rosidl_typesupport_introspection_cpp/message_introspection.hpp>
#include <nlohmann/json.hpp>

namespace fleet_agent {
namespace capability {

/**
 * SchemaExtractor - Extracts JSON Schema from ROS2 message type support.
 *
 * Converts ROS2 message introspection data to JSON Schema format
 * for server-side validation and UI generation.
 *
 * Example output for NavigateToPose_Goal:
 * {
 *   "type": "object",
 *   "properties": {
 *     "pose": {
 *       "type": "object",
 *       "properties": {
 *         "header": {...},
 *         "pose": {
 *           "type": "object",
 *           "properties": {
 *             "position": {"type": "object", "properties": {...}},
 *             "orientation": {"type": "object", "properties": {...}}
 *           }
 *         }
 *       }
 *     }
 *   }
 * }
 */
class SchemaExtractor {
public:
    using MessageMembers = rosidl_typesupport_introspection_cpp::MessageMembers;
    using MessageMember = rosidl_typesupport_introspection_cpp::MessageMember;

    SchemaExtractor();

    /**
     * Extract JSON Schema from type support handle.
     *
     * @param ts ROS2 message type support handle
     * @return JSON Schema as string
     */
    std::string extract_json_schema(const rosidl_message_type_support_t* ts);

    /**
     * Extract JSON Schema as JSON object.
     *
     * @param ts ROS2 message type support handle
     * @return JSON Schema as nlohmann::json
     */
    nlohmann::json extract_schema(const rosidl_message_type_support_t* ts);

private:
    static constexpr int MAX_DEPTH = 10;  // Prevent infinite recursion

    /**
     * Convert MessageMembers to JSON Schema.
     *
     * @param members ROS2 message members
     * @param depth Current recursion depth
     * @return JSON Schema object
     */
    nlohmann::json members_to_schema(const MessageMembers* members, int depth = 0);

    /**
     * Convert single member to JSON Schema property.
     *
     * @param member ROS2 message member
     * @param depth Current recursion depth
     * @return JSON Schema property object
     */
    nlohmann::json member_to_property(const MessageMember* member, int depth);

    /**
     * Convert ROS2 type ID to JSON type string.
     *
     * Mappings:
     *   ROS_TYPE_BOOL    -> "boolean"
     *   ROS_TYPE_INT*    -> "integer"
     *   ROS_TYPE_FLOAT*  -> "number"
     *   ROS_TYPE_STRING  -> "string"
     *   ROS_TYPE_MESSAGE -> "object" (recursive)
     *   Arrays           -> "array"
     */
    std::string ros_type_to_json_type(uint8_t type_id);

    /**
     * Get ROS2 type name for documentation.
     */
    std::string ros_type_name(uint8_t type_id);
};

/**
 * SuccessCriteriaInferrer - Infers success criteria from Result schema.
 *
 * Analyzes the Result message schema to automatically determine
 * success/failure conditions based on common patterns.
 *
 * Known patterns:
 * - "success" field (bool) -> success == true
 * - "error_code" field (int) -> error_code == 0
 * - "result_code" field (int) -> result_code == 0
 * - "status" field (string) -> status in ["SUCCESS", "SUCCEEDED"]
 */
class SuccessCriteriaInferrer {
public:
    SuccessCriteriaInferrer();

    /**
     * Infer success criteria from Result JSON Schema.
     *
     * @param result_schema_json JSON Schema of Result message
     * @return Inferred SuccessCriteria
     */
    SuccessCriteria infer(const std::string& result_schema_json);

    /**
     * Infer from JSON object.
     */
    SuccessCriteria infer(const nlohmann::json& result_schema);

private:
    // Pattern definitions: (field_name, operator, expected_value)
    struct Pattern {
        std::string field;
        std::string op;
        std::string value;
        int priority;  // Lower = higher priority
    };

    std::vector<Pattern> patterns_;

    /**
     * Check if schema contains a field.
     */
    bool has_field(const nlohmann::json& schema, const std::string& field_name);

    /**
     * Get field type from schema.
     */
    std::string get_field_type(const nlohmann::json& schema, const std::string& field_name);
};

}  // namespace capability
}  // namespace fleet_agent
