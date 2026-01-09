// Copyright 2026 Multi-Robot Supervision System
// JSON Schema extractor implementation

#include "fleet_agent/capability/schema_extractor.hpp"
#include "fleet_agent/core/logger.hpp"

#include <rosidl_typesupport_introspection_cpp/identifier.hpp>

namespace fleet_agent {
namespace capability {

namespace {
logging::ComponentLogger log("SchemaExtractor");

// ROS2 type IDs (from rosidl_typesupport_introspection_cpp)
constexpr uint8_t ROS_TYPE_FLOAT = 1;
constexpr uint8_t ROS_TYPE_DOUBLE = 2;
constexpr uint8_t ROS_TYPE_LONG_DOUBLE = 3;
constexpr uint8_t ROS_TYPE_CHAR = 4;
constexpr uint8_t ROS_TYPE_WCHAR = 5;
constexpr uint8_t ROS_TYPE_BOOLEAN = 6;
constexpr uint8_t ROS_TYPE_OCTET = 7;
constexpr uint8_t ROS_TYPE_UINT8 = 8;
constexpr uint8_t ROS_TYPE_INT8 = 9;
constexpr uint8_t ROS_TYPE_UINT16 = 10;
constexpr uint8_t ROS_TYPE_INT16 = 11;
constexpr uint8_t ROS_TYPE_UINT32 = 12;
constexpr uint8_t ROS_TYPE_INT32 = 13;
constexpr uint8_t ROS_TYPE_UINT64 = 14;
constexpr uint8_t ROS_TYPE_INT64 = 15;
constexpr uint8_t ROS_TYPE_STRING = 16;
constexpr uint8_t ROS_TYPE_WSTRING = 17;
constexpr uint8_t ROS_TYPE_MESSAGE = 18;

}  // namespace

SchemaExtractor::SchemaExtractor() = default;

std::string SchemaExtractor::ros_type_to_json_type(uint8_t type_id) {
    switch (type_id) {
        case ROS_TYPE_BOOLEAN:
            return "boolean";
        case ROS_TYPE_FLOAT:
        case ROS_TYPE_DOUBLE:
        case ROS_TYPE_LONG_DOUBLE:
            return "number";
        case ROS_TYPE_CHAR:
        case ROS_TYPE_WCHAR:
        case ROS_TYPE_OCTET:
        case ROS_TYPE_UINT8:
        case ROS_TYPE_INT8:
        case ROS_TYPE_UINT16:
        case ROS_TYPE_INT16:
        case ROS_TYPE_UINT32:
        case ROS_TYPE_INT32:
        case ROS_TYPE_UINT64:
        case ROS_TYPE_INT64:
            return "integer";
        case ROS_TYPE_STRING:
        case ROS_TYPE_WSTRING:
            return "string";
        case ROS_TYPE_MESSAGE:
            return "object";
        default:
            return "string";
    }
}

std::string SchemaExtractor::ros_type_name(uint8_t type_id) {
    switch (type_id) {
        case ROS_TYPE_BOOLEAN: return "bool";
        case ROS_TYPE_FLOAT: return "float32";
        case ROS_TYPE_DOUBLE: return "float64";
        case ROS_TYPE_LONG_DOUBLE: return "float128";
        case ROS_TYPE_CHAR: return "char";
        case ROS_TYPE_WCHAR: return "wchar";
        case ROS_TYPE_OCTET: return "byte";
        case ROS_TYPE_UINT8: return "uint8";
        case ROS_TYPE_INT8: return "int8";
        case ROS_TYPE_UINT16: return "uint16";
        case ROS_TYPE_INT16: return "int16";
        case ROS_TYPE_UINT32: return "uint32";
        case ROS_TYPE_INT32: return "int32";
        case ROS_TYPE_UINT64: return "uint64";
        case ROS_TYPE_INT64: return "int64";
        case ROS_TYPE_STRING: return "string";
        case ROS_TYPE_WSTRING: return "wstring";
        case ROS_TYPE_MESSAGE: return "message";
        default: return "unknown";
    }
}

nlohmann::json SchemaExtractor::member_to_property(const MessageMember* member, int depth) {
    nlohmann::json prop;

    // Handle arrays
    if (member->is_array_) {
        prop["type"] = "array";

        // Items schema
        nlohmann::json items;
        if (member->type_id_ == ROS_TYPE_MESSAGE) {
            if (depth < MAX_DEPTH && member->members_ && member->members_->data) {
                items = members_to_schema(
                    static_cast<const MessageMembers*>(member->members_->data), depth + 1);
            } else {
                items["type"] = "object";
            }
        } else {
            items["type"] = ros_type_to_json_type(member->type_id_);
        }
        prop["items"] = items;

        // Array bounds
        if (member->array_size_ > 0) {
            if (member->is_upper_bound_) {
                prop["maxItems"] = member->array_size_;
            } else {
                prop["minItems"] = member->array_size_;
                prop["maxItems"] = member->array_size_;
            }
        }
    }
    // Handle nested messages
    else if (member->type_id_ == ROS_TYPE_MESSAGE) {
        if (depth < MAX_DEPTH && member->members_ && member->members_->data) {
            prop = members_to_schema(
                static_cast<const MessageMembers*>(member->members_->data), depth + 1);
        } else {
            prop["type"] = "object";
            prop["description"] = "Nested message (depth limit reached)";
        }
    }
    // Handle primitive types
    else {
        prop["type"] = ros_type_to_json_type(member->type_id_);
    }

    // Add ROS type as description
    prop["x-ros-type"] = ros_type_name(member->type_id_);

    // Add default value hint if available
    if (member->default_value_) {
        prop["x-has-default"] = true;
    }

    return prop;
}

nlohmann::json SchemaExtractor::members_to_schema(const MessageMembers* members, int depth) {
    nlohmann::json schema;
    schema["type"] = "object";

    if (!members || depth >= MAX_DEPTH) {
        return schema;
    }

    nlohmann::json properties = nlohmann::json::object();
    std::vector<std::string> required;

    for (size_t i = 0; i < members->member_count_; ++i) {
        const MessageMember* member = &members->members_[i];
        if (!member || !member->name_) continue;

        std::string name(member->name_);
        properties[name] = member_to_property(member, depth);

        // All fields are considered required by default in ROS2
        required.push_back(name);
    }

    schema["properties"] = properties;

    // Add namespace info
    if (members->message_namespace_ && members->message_name_) {
        schema["x-ros-namespace"] = members->message_namespace_;
        schema["x-ros-name"] = members->message_name_;
    }

    return schema;
}

std::string SchemaExtractor::extract_json_schema(const rosidl_message_type_support_t* ts) {
    nlohmann::json schema = extract_schema(ts);
    return schema.dump(2);
}

nlohmann::json SchemaExtractor::extract_schema(const rosidl_message_type_support_t* ts) {
    if (!ts) {
        return nlohmann::json::object();
    }

    // Get introspection type support
    const rosidl_message_type_support_t* introspection_ts =
        get_message_typesupport_handle(
            ts, rosidl_typesupport_introspection_cpp::typesupport_identifier);

    if (!introspection_ts || !introspection_ts->data) {
        log.warn("Failed to get introspection type support");
        return nlohmann::json::object();
    }

    const MessageMembers* members =
        static_cast<const MessageMembers*>(introspection_ts->data);

    return members_to_schema(members, 0);
}

// ============================================================
// SuccessCriteriaInferrer Implementation
// ============================================================

SuccessCriteriaInferrer::SuccessCriteriaInferrer() {
    // Define known success patterns in priority order
    patterns_ = {
        {"success", "==", "true", 1},
        {"succeeded", "==", "true", 2},
        {"reached_goal", "==", "true", 3},
        {"error_code", "==", "0", 4},
        {"result_code", "==", "0", 5},
        {"code", "==", "0", 6},
        {"status", "in", "[\"SUCCESS\",\"SUCCEEDED\",\"OK\"]", 7},
    };
}

bool SuccessCriteriaInferrer::has_field(const nlohmann::json& schema,
                                        const std::string& field_name) {
    if (!schema.contains("properties")) {
        return false;
    }
    return schema["properties"].contains(field_name);
}

std::string SuccessCriteriaInferrer::get_field_type(const nlohmann::json& schema,
                                                    const std::string& field_name) {
    if (!has_field(schema, field_name)) {
        return "";
    }

    const auto& prop = schema["properties"][field_name];
    if (prop.contains("type")) {
        return prop["type"].get<std::string>();
    }
    return "";
}

SuccessCriteria SuccessCriteriaInferrer::infer(const std::string& result_schema_json) {
    if (result_schema_json.empty()) {
        return SuccessCriteria{};
    }

    try {
        nlohmann::json schema = nlohmann::json::parse(result_schema_json);
        return infer(schema);
    } catch (const std::exception& e) {
        log.warn("Failed to parse result schema: {}", e.what());
        return SuccessCriteria{};
    }
}

SuccessCriteria SuccessCriteriaInferrer::infer(const nlohmann::json& result_schema) {
    // Try patterns in priority order
    for (const auto& pattern : patterns_) {
        if (has_field(result_schema, pattern.field)) {
            std::string field_type = get_field_type(result_schema, pattern.field);

            // Validate type compatibility
            bool type_compatible = false;
            if (pattern.op == "==" || pattern.op == "!=") {
                if (pattern.value == "true" || pattern.value == "false") {
                    type_compatible = (field_type == "boolean");
                } else if (pattern.value == "0" || pattern.value.find_first_not_of("0123456789") == std::string::npos) {
                    type_compatible = (field_type == "integer");
                } else {
                    type_compatible = (field_type == "string");
                }
            } else if (pattern.op == "in") {
                type_compatible = (field_type == "string" || field_type == "integer");
            }

            if (type_compatible || field_type.empty()) {
                log.debug("Inferred success criteria: {} {} {}",
                         pattern.field, pattern.op, pattern.value);
                return SuccessCriteria{pattern.field, pattern.op, pattern.value};
            }
        }
    }

    // No pattern matched - completion itself is success
    log.debug("No success pattern found, using completion as success");
    return SuccessCriteria{};
}

}  // namespace capability
}  // namespace fleet_agent
