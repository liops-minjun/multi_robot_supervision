// Copyright 2026 Multi-Robot Supervision System
// Dynamic Action Client - Runtime support for any ROS2 action type

#pragma once

#include "robot_agent/core/logger.hpp"

#include <atomic>
#include <chrono>
#include <functional>
#include <memory>
#include <mutex>
#include <string>
#include <unordered_map>
#include <vector>

#include <rclcpp/rclcpp.hpp>
#include <rclcpp_action/rclcpp_action.hpp>

#include <rcl_action/rcl_action.h>
#include <rcpputils/shared_library.hpp>

#include <rosidl_typesupport_introspection_cpp/message_introspection.hpp>
#include <rosidl_typesupport_introspection_cpp/field_types.hpp>
#include <rosidl_runtime_cpp/message_initialization.hpp>

#include <nlohmann/json.hpp>

namespace robot_agent {
namespace executor {

// Forward declaration for logging
namespace dynamic_action_logging {
    extern ::robot_agent::logging::ComponentLogger log;
}

// ============================================================
// Dynamic Goal Handle - Type-erased goal tracking
// ============================================================

struct DynamicGoalHandle {
    std::string goal_id;
    std::atomic<bool> active{true};
    std::atomic<int8_t> status{0};

    std::function<void(bool success, const std::string& result_json)> result_callback;
    std::function<void(const std::string& feedback_json)> feedback_callback;

    // Raw pointer to internal goal handle (type-erased)
    std::shared_ptr<void> internal_handle;
};

// ============================================================
// Type Support Loader - Dynamically load action type support
// ============================================================

class TypeSupportLoader {
public:
    struct ActionTypeSupport {
        const rosidl_action_type_support_t* action_ts = nullptr;
        const rosidl_typesupport_introspection_cpp::MessageMembers* goal_members = nullptr;
        const rosidl_typesupport_introspection_cpp::MessageMembers* result_members = nullptr;
        const rosidl_typesupport_introspection_cpp::MessageMembers* feedback_members = nullptr;
        std::shared_ptr<rcpputils::SharedLibrary> library;
    };

    static std::shared_ptr<ActionTypeSupport> load(const std::string& action_type);

private:
    static std::string get_library_path(const std::string& package_name);
    static std::string get_symbol_name(const std::string& action_type, const std::string& suffix);

    static std::mutex cache_mutex_;
    static std::unordered_map<std::string, std::shared_ptr<ActionTypeSupport>> cache_;
};

// ============================================================
// JSON <-> ROS2 Message Converter
// ============================================================

class MessageConverter {
public:
    // Convert JSON to ROS2 message bytes
    static bool json_to_message(
        const nlohmann::json& json,
        const rosidl_typesupport_introspection_cpp::MessageMembers* members,
        void* message);

    // Convert ROS2 message to JSON
    static nlohmann::json message_to_json(
        const rosidl_typesupport_introspection_cpp::MessageMembers* members,
        const void* message);

private:
    static bool set_field_from_json(
        const nlohmann::json& json,
        const rosidl_typesupport_introspection_cpp::MessageMember& member,
        void* field_ptr);

    static nlohmann::json get_field_as_json(
        const rosidl_typesupport_introspection_cpp::MessageMember& member,
        const void* field_ptr);
};

// ============================================================
// Dynamic Action Client - Works with any action type at runtime
// ============================================================

class DynamicActionClient {
public:
    DynamicActionClient(
        rclcpp::Node::SharedPtr node,
        const std::string& action_name,
        const std::string& action_type);

    ~DynamicActionClient();

    // Send goal with JSON parameters
    std::shared_ptr<DynamicGoalHandle> send_goal(
        const std::string& goal_json,
        std::function<void(bool, const std::string&)> result_callback,
        std::function<void(const std::string&)> feedback_callback = nullptr);

    // Cancel active goal
    void cancel_goal(std::shared_ptr<DynamicGoalHandle> handle);

    // Check if server is ready
    bool is_server_ready() const;

    // Wait for server with timeout
    bool wait_for_server(std::chrono::milliseconds timeout);

    // Get action type
    std::string action_type() const { return action_type_; }

private:
    void on_goal_response(
        std::shared_ptr<DynamicGoalHandle> handle,
        std::shared_ptr<void> goal_handle_ptr);

    void on_feedback(
        std::shared_ptr<DynamicGoalHandle> handle,
        const void* feedback);

    void on_result(
        std::shared_ptr<DynamicGoalHandle> handle,
        int result_code,
        const void* result);

    rclcpp::Node::SharedPtr node_;
    std::string action_name_;
    std::string action_type_;

    std::shared_ptr<TypeSupportLoader::ActionTypeSupport> type_support_;

    // Generic action client (using rcl_action directly)
    std::shared_ptr<rclcpp_action::ClientBase> client_;

    std::unordered_map<std::string, std::shared_ptr<DynamicGoalHandle>> active_goals_;
    std::mutex goals_mutex_;
};

// ============================================================
// Action Client Factory - Creates dynamic clients for any type
// ============================================================

class DynamicActionClientFactory {
public:
    static std::unique_ptr<DynamicActionClient> create(
        rclcpp::Node::SharedPtr node,
        const std::string& action_server,
        const std::string& action_type) {

        dynamic_action_logging::log.info("Creating dynamic action client for {} (type: {})",
                                         action_server, action_type);

        try {
            return std::make_unique<DynamicActionClient>(node, action_server, action_type);
        } catch (const std::exception& e) {
            dynamic_action_logging::log.error("Failed to create dynamic action client: {}", e.what());
            return nullptr;
        }
    }

    // All action types are supported dynamically
    static bool is_supported(const std::string& /*action_type*/) {
        return true;  // We support any action type dynamically
    }
};

}  // namespace executor
}  // namespace robot_agent
