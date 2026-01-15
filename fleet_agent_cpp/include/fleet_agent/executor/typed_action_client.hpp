// Copyright 2026 Multi-Robot Supervision System
// Dynamic Action Client - Runtime support for any ROS2 action type

#pragma once

#include "fleet_agent/core/logger.hpp"
#include "fleet_agent/capability/type_loader.hpp"

#include <array>
#include <atomic>
#include <chrono>
#include <functional>
#include <memory>
#include <mutex>
#include <string>
#include <unordered_map>

#include <rclcpp/rclcpp.hpp>
#include <rclcpp_action/rclcpp_action.hpp>

#include <rcl_action/rcl_action.h>
#include <rosidl_typesupport_introspection_cpp/message_introspection.hpp>
#include <rosidl_typesupport_introspection_cpp/service_introspection.hpp>
#include <rosidl_typesupport_introspection_cpp/field_types.hpp>

#include <nlohmann/json.hpp>

namespace fleet_agent {
namespace executor {

// Forward declaration for the logger
namespace action_client_logging {
    extern ::fleet_agent::logging::ComponentLogger action_log;
}

inline int64_t now_ms() {
    return std::chrono::duration_cast<std::chrono::milliseconds>(
        std::chrono::steady_clock::now().time_since_epoch()).count();
}

// ============================================================
// Goal Handle Wrapper - Type-erased goal tracking
// ============================================================

struct ActionGoalHandle {
    std::string goal_id;
    std::array<uint8_t, 16> uuid{};  // Store UUID bytes for result request
    std::atomic<bool> active{true};
    std::atomic<int8_t> status{0};  // action_msgs::msg::GoalStatus

    std::function<void(bool success, const std::string& result_json)> result_callback;
    std::function<void(const std::string& feedback_json)> feedback_callback;

    // Type-erased storage for the actual goal handle
    std::shared_ptr<void> typed_handle;
};

// ============================================================
// Abstract Action Client Interface
// ============================================================

class ITypedActionClient {
public:
    virtual ~ITypedActionClient() = default;

    virtual std::shared_ptr<ActionGoalHandle> send_goal(
        const std::string& goal_json,
        std::function<void(bool, const std::string&)> result_callback,
        std::function<void(const std::string&)> feedback_callback = nullptr
    ) = 0;

    virtual void cancel_goal(std::shared_ptr<ActionGoalHandle> handle) = 0;
    virtual bool is_server_ready() const = 0;
    virtual bool wait_for_server(std::chrono::milliseconds timeout) = 0;
    virtual std::string action_type() const = 0;
};

// ============================================================
// JSON <-> ROS2 Message Converter using introspection
// ============================================================

class MessageConverter {
public:
    using MessageMembers = rosidl_typesupport_introspection_cpp::MessageMembers;
    using MessageMember = rosidl_typesupport_introspection_cpp::MessageMember;

    // Allocate message memory and initialize
    static void* allocate_message(const MessageMembers* members);

    // Deallocate message memory
    static void deallocate_message(const MessageMembers* members, void* message);

    // Fill message from JSON
    static bool json_to_message(
        const nlohmann::json& json,
        const MessageMembers* members,
        void* message);

    // Convert message to JSON
    static nlohmann::json message_to_json(
        const MessageMembers* members,
        const void* message);

    // Helper to convert nested type support to MessageMembers (public for DynamicActionClient)
    static const MessageMembers* get_nested_members(const rosidl_message_type_support_t* ts);

private:
    static bool set_field(
        const nlohmann::json& json,
        const MessageMember& member,
        void* field_ptr);

    static nlohmann::json get_field(
        const MessageMember& member,
        const void* field_ptr);

    static void* get_field_ptr(void* message, const MessageMember& member);
    static const void* get_field_ptr(const void* message, const MessageMember& member);
};

// ============================================================
// Dynamic Action Client - Works with any action type at runtime
// ============================================================

class DynamicActionClient : public ITypedActionClient {
public:
    DynamicActionClient(
        rclcpp::Node::SharedPtr node,
        const std::string& action_name,
        const std::string& action_type);

    ~DynamicActionClient() override;

    std::shared_ptr<ActionGoalHandle> send_goal(
        const std::string& goal_json,
        std::function<void(bool, const std::string&)> result_callback,
        std::function<void(const std::string&)> feedback_callback = nullptr) override;

    void cancel_goal(std::shared_ptr<ActionGoalHandle> handle) override;

    bool is_server_ready() const override;

    bool wait_for_server(std::chrono::milliseconds timeout) override;

    std::string action_type() const override { return action_type_; }

private:
    rclcpp::Node::SharedPtr node_;
    std::string action_name_;
    std::string action_type_;

    // Type support from dynamic loader
    capability::TypeSupportLoader type_loader_;
    std::optional<capability::TypeSupportLoader::ActionTypeInfo> type_info_;

    // RCL action client handle
    rcl_action_client_t action_client_;
    bool client_initialized_{false};

    // Introspection members
    const rosidl_typesupport_introspection_cpp::MessageMembers* goal_members_{nullptr};
    const rosidl_typesupport_introspection_cpp::MessageMembers* result_members_{nullptr};
    const rosidl_typesupport_introspection_cpp::MessageMembers* feedback_members_{nullptr};

    // Service request/response members for RCL action API
    const rosidl_typesupport_introspection_cpp::MessageMembers* send_goal_request_members_{nullptr};
    const rosidl_typesupport_introspection_cpp::MessageMembers* send_goal_response_members_{nullptr};
    const rosidl_typesupport_introspection_cpp::MessageMembers* get_result_request_members_{nullptr};
    const rosidl_typesupport_introspection_cpp::MessageMembers* get_result_response_members_{nullptr};
    const rosidl_typesupport_introspection_cpp::MessageMembers* feedback_message_members_{nullptr};

    // Goal tracking
    std::unordered_map<int64_t, std::shared_ptr<ActionGoalHandle>> active_goals_;
    std::mutex goals_mutex_;
    int64_t next_sequence_{1};

    // Timer for polling results/feedback
    rclcpp::TimerBase::SharedPtr poll_timer_;

    void poll_for_responses();
    void process_goal_response(int64_t sequence);
    void process_result(int64_t sequence);
    void process_feedback();

    bool init_client();
    void cleanup_client();

    const rosidl_typesupport_introspection_cpp::MessageMembers* get_introspection_members(
        const rosidl_message_type_support_t* ts);

    // Get service request/response introspection members
    void get_service_introspection_members(
        const rosidl_service_type_support_t* srv_ts,
        const rosidl_typesupport_introspection_cpp::MessageMembers** request_members,
        const rosidl_typesupport_introspection_cpp::MessageMembers** response_members);

    // Generate a random UUID for goal_id
    void generate_uuid(uint8_t* uuid);

    // Fill the goal_id field in a SendGoal_Request message and optionally output the UUID
    bool fill_goal_id(void* request_msg, std::array<uint8_t, 16>* out_uuid = nullptr);

    // Fill the goal_id field in a GetResult_Request message with an existing UUID
    bool fill_goal_id_from_uuid(void* request_msg, const std::array<uint8_t, 16>& uuid);

    // Find and fill the goal field in a SendGoal_Request message
    bool fill_goal_field(void* request_msg, const nlohmann::json& json);
};

// ============================================================
// Action Client Factory - Creates dynamic clients for any type
// ============================================================

class ActionClientFactory {
public:
    static std::unique_ptr<ITypedActionClient> create(
        rclcpp::Node::SharedPtr node,
        const std::string& action_server,
        const std::string& action_type) {

        action_client_logging::action_log.info("Creating dynamic action client for {} (type: {})",
                                 action_server, action_type);

        try {
            return std::make_unique<DynamicActionClient>(node, action_server, action_type);
        } catch (const std::exception& e) {
            action_client_logging::action_log.error("Failed to create action client: {}", e.what());
            return nullptr;
        }
    }

    // All action types are now supported dynamically
    static bool is_supported(const std::string& /*action_type*/) {
        return true;  // Dynamic support for any action type
    }
};

}  // namespace executor
}  // namespace fleet_agent
