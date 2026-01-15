// Copyright 2026 Multi-Robot Supervision System
// Dynamic Action Client Implementation

#include "fleet_agent/executor/typed_action_client.hpp"
#include "fleet_agent/core/logger.hpp"

#include <rcl/rcl.h>
#include <rcl_action/rcl_action.h>
#include <rosidl_typesupport_introspection_cpp/identifier.hpp>
#include <rosidl_typesupport_introspection_cpp/field_types.hpp>
#include <rosidl_typesupport_introspection_cpp/service_introspection.hpp>

#include <cstring>
#include <random>
#include <thread>

namespace fleet_agent {
namespace executor {

namespace action_client_logging {
::fleet_agent::logging::ComponentLogger action_log("DynamicActionClient");
}

namespace {
logging::ComponentLogger log("DynamicActionClient");
}

// ============================================================
// MessageConverter Implementation
// ============================================================

void* MessageConverter::allocate_message(const MessageMembers* members) {
    if (!members) return nullptr;

    void* message = malloc(members->size_of_);
    if (message) {
        memset(message, 0, members->size_of_);
        // Call init function if available
        if (members->init_function) {
            members->init_function(message, rosidl_runtime_cpp::MessageInitialization::ALL);
        }
    }
    return message;
}

void MessageConverter::deallocate_message(const MessageMembers* members, void* message) {
    if (!members || !message) return;

    // Call fini function if available
    if (members->fini_function) {
        members->fini_function(message);
    }
    free(message);
}

void* MessageConverter::get_field_ptr(void* message, const MessageMember& member) {
    return static_cast<char*>(message) + member.offset_;
}

const void* MessageConverter::get_field_ptr(const void* message, const MessageMember& member) {
    return static_cast<const char*>(message) + member.offset_;
}

// Helper to convert nested type support to MessageMembers
const MessageConverter::MessageMembers* MessageConverter::get_nested_members(
    const rosidl_message_type_support_t* ts) {
    if (!ts) return nullptr;

    const rosidl_message_type_support_t* introspection_ts =
        get_message_typesupport_handle(
            ts, rosidl_typesupport_introspection_cpp::typesupport_identifier);

    if (introspection_ts && introspection_ts->data) {
        return static_cast<const MessageMembers*>(introspection_ts->data);
    }
    return nullptr;
}

bool MessageConverter::json_to_message(
    const nlohmann::json& json,
    const MessageMembers* members,
    void* message) {

    if (!members || !message) {
        log.error("Invalid members or message pointer");
        return false;
    }

    for (uint32_t i = 0; i < members->member_count_; ++i) {
        const auto& member = members->members_[i];
        void* field_ptr = get_field_ptr(message, member);

        // Check if JSON has this field
        if (json.contains(member.name_)) {
            if (!set_field(json[member.name_], member, field_ptr)) {
                log.warn("Failed to set field: {}", member.name_);
            }
        }
    }

    return true;
}

bool MessageConverter::set_field(
    const nlohmann::json& json,
    const MessageMember& member,
    void* field_ptr) {

    using namespace rosidl_typesupport_introspection_cpp;

    // Handle arrays
    if (member.is_array_) {
        // For simplicity, only handle basic arrays for now
        // Full implementation would handle all array types
        log.debug("Array field {} - using default", member.name_);
        return true;
    }

    try {
        switch (member.type_id_) {
            case ROS_TYPE_BOOL:
                *static_cast<bool*>(field_ptr) = json.get<bool>();
                break;
            case ROS_TYPE_BYTE:
            case ROS_TYPE_UINT8:
                *static_cast<uint8_t*>(field_ptr) = json.get<uint8_t>();
                break;
            case ROS_TYPE_INT8:
                *static_cast<int8_t*>(field_ptr) = json.get<int8_t>();
                break;
            case ROS_TYPE_UINT16:
                *static_cast<uint16_t*>(field_ptr) = json.get<uint16_t>();
                break;
            case ROS_TYPE_INT16:
                *static_cast<int16_t*>(field_ptr) = json.get<int16_t>();
                break;
            case ROS_TYPE_UINT32:
                *static_cast<uint32_t*>(field_ptr) = json.get<uint32_t>();
                break;
            case ROS_TYPE_INT32:
                *static_cast<int32_t*>(field_ptr) = json.get<int32_t>();
                break;
            case ROS_TYPE_UINT64:
                *static_cast<uint64_t*>(field_ptr) = json.get<uint64_t>();
                break;
            case ROS_TYPE_INT64:
                *static_cast<int64_t*>(field_ptr) = json.get<int64_t>();
                break;
            case ROS_TYPE_FLOAT:
                *static_cast<float*>(field_ptr) = json.get<float>();
                break;
            case ROS_TYPE_DOUBLE:
                *static_cast<double*>(field_ptr) = json.get<double>();
                break;
            case ROS_TYPE_STRING:
                *static_cast<std::string*>(field_ptr) = json.get<std::string>();
                break;
            case ROS_TYPE_MESSAGE:
                // Nested message - recursive call
                if (member.members_ && json.is_object()) {
                    const MessageMembers* nested = get_nested_members(member.members_);
                    if (nested) {
                        return json_to_message(json, nested, field_ptr);
                    }
                }
                break;
            default:
                log.debug("Unhandled type {} for field {}", member.type_id_, member.name_);
                break;
        }
        return true;
    } catch (const std::exception& e) {
        log.debug("Failed to convert field {}: {}", member.name_, e.what());
        return false;
    }
}

nlohmann::json MessageConverter::message_to_json(
    const MessageMembers* members,
    const void* message) {

    nlohmann::json result = nlohmann::json::object();

    if (!members || !message) {
        return result;
    }

    for (uint32_t i = 0; i < members->member_count_; ++i) {
        const auto& member = members->members_[i];
        const void* field_ptr = get_field_ptr(message, member);
        result[member.name_] = get_field(member, field_ptr);
    }

    return result;
}

nlohmann::json MessageConverter::get_field(
    const MessageMember& member,
    const void* field_ptr) {

    using namespace rosidl_typesupport_introspection_cpp;

    // Handle arrays
    if (member.is_array_) {
        return nlohmann::json::array();  // Simplified
    }

    switch (member.type_id_) {
        case ROS_TYPE_BOOL:
            return *static_cast<const bool*>(field_ptr);
        case ROS_TYPE_BYTE:
        case ROS_TYPE_UINT8:
            return *static_cast<const uint8_t*>(field_ptr);
        case ROS_TYPE_INT8:
            return *static_cast<const int8_t*>(field_ptr);
        case ROS_TYPE_UINT16:
            return *static_cast<const uint16_t*>(field_ptr);
        case ROS_TYPE_INT16:
            return *static_cast<const int16_t*>(field_ptr);
        case ROS_TYPE_UINT32:
            return *static_cast<const uint32_t*>(field_ptr);
        case ROS_TYPE_INT32:
            return *static_cast<const int32_t*>(field_ptr);
        case ROS_TYPE_UINT64:
            return *static_cast<const uint64_t*>(field_ptr);
        case ROS_TYPE_INT64:
            return *static_cast<const int64_t*>(field_ptr);
        case ROS_TYPE_FLOAT:
            return *static_cast<const float*>(field_ptr);
        case ROS_TYPE_DOUBLE:
            return *static_cast<const double*>(field_ptr);
        case ROS_TYPE_STRING:
            return *static_cast<const std::string*>(field_ptr);
        case ROS_TYPE_MESSAGE:
            if (member.members_) {
                const MessageMembers* nested = get_nested_members(member.members_);
                if (nested) {
                    return message_to_json(nested, field_ptr);
                }
            }
            return nlohmann::json::object();
        default:
            return nullptr;
    }
}

// ============================================================
// DynamicActionClient Implementation
// ============================================================

DynamicActionClient::DynamicActionClient(
    rclcpp::Node::SharedPtr node,
    const std::string& action_name,
    const std::string& action_type)
    : node_(node)
    , action_name_(action_name)
    , action_type_(action_type)
    , action_client_(rcl_action_get_zero_initialized_client()) {

    log.info("Creating DynamicActionClient for {} (type: {})", action_name, action_type);

    // Load type support dynamically
    type_info_ = type_loader_.load(action_type);
    if (!type_info_ || !type_info_->valid) {
        throw std::runtime_error("Failed to load type support for: " + action_type);
    }

    // Get introspection members for Goal/Result/Feedback (for JSON conversion)
    if (type_info_->goal_ts) {
        goal_members_ = get_introspection_members(type_info_->goal_ts);
    }
    if (type_info_->result_ts) {
        result_members_ = get_introspection_members(type_info_->result_ts);
    }
    if (type_info_->feedback_ts) {
        feedback_members_ = get_introspection_members(type_info_->feedback_ts);
    }

    // Get service introspection members for RCL action API
    if (type_info_->action_ts) {
        // SendGoal service
        if (type_info_->action_ts->goal_service_type_support) {
            get_service_introspection_members(
                type_info_->action_ts->goal_service_type_support,
                &send_goal_request_members_,
                &send_goal_response_members_);
        }

        // GetResult service
        if (type_info_->action_ts->result_service_type_support) {
            get_service_introspection_members(
                type_info_->action_ts->result_service_type_support,
                &get_result_request_members_,
                &get_result_response_members_);
        }

        // FeedbackMessage (wraps UUID + feedback)
        if (type_info_->action_ts->feedback_message_type_support) {
            feedback_message_members_ = get_introspection_members(
                type_info_->action_ts->feedback_message_type_support);
        }
    }

    if (!send_goal_request_members_) {
        throw std::runtime_error("Failed to get SendGoal_Request introspection members for: " + action_type);
    }

    log.info("Type support loaded successfully for {}", action_type);
}

DynamicActionClient::~DynamicActionClient() {
    if (poll_timer_) {
        poll_timer_->cancel();
    }
    cleanup_client();
}

const rosidl_typesupport_introspection_cpp::MessageMembers*
DynamicActionClient::get_introspection_members(const rosidl_message_type_support_t* ts) {
    if (!ts) return nullptr;

    // Get introspection type support
    const rosidl_message_type_support_t* introspection_ts =
        get_message_typesupport_handle(
            ts, rosidl_typesupport_introspection_cpp::typesupport_identifier);

    if (introspection_ts && introspection_ts->data) {
        return static_cast<const rosidl_typesupport_introspection_cpp::MessageMembers*>(
            introspection_ts->data);
    }
    return nullptr;
}

void DynamicActionClient::get_service_introspection_members(
    const rosidl_service_type_support_t* srv_ts,
    const rosidl_typesupport_introspection_cpp::MessageMembers** request_members,
    const rosidl_typesupport_introspection_cpp::MessageMembers** response_members) {

    if (!srv_ts) return;

    // Get introspection type support for the service
    const rosidl_service_type_support_t* introspection_ts =
        get_service_typesupport_handle(
            srv_ts, rosidl_typesupport_introspection_cpp::typesupport_identifier);

    if (introspection_ts && introspection_ts->data) {
        auto service_members = static_cast<const rosidl_typesupport_introspection_cpp::ServiceMembers*>(
            introspection_ts->data);
        if (request_members) {
            *request_members = service_members->request_members_;
        }
        if (response_members) {
            *response_members = service_members->response_members_;
        }
    }
}

void DynamicActionClient::generate_uuid(uint8_t* uuid) {
    static std::random_device rd;
    static std::mt19937 gen(rd());
    static std::uniform_int_distribution<uint8_t> dist(0, 255);

    for (int i = 0; i < 16; ++i) {
        uuid[i] = dist(gen);
    }

    // Set version (4) and variant bits
    uuid[6] = (uuid[6] & 0x0F) | 0x40;  // Version 4
    uuid[8] = (uuid[8] & 0x3F) | 0x80;  // Variant 1
}

bool DynamicActionClient::fill_goal_id(void* request_msg, std::array<uint8_t, 16>* out_uuid) {
    if (!request_msg || !send_goal_request_members_) {
        return false;
    }

    // Find goal_id field in SendGoal_Request
    for (uint32_t i = 0; i < send_goal_request_members_->member_count_; ++i) {
        const auto& member = send_goal_request_members_->members_[i];
        if (std::string(member.name_) == "goal_id") {
            void* goal_id_ptr = static_cast<char*>(request_msg) + member.offset_;

            // goal_id is a unique_identifier_msgs/msg/UUID which has uuid[16] array
            // Get the UUID message members
            if (member.members_) {
                const auto* uuid_members = MessageConverter::get_nested_members(member.members_);
                if (uuid_members) {
                    // Find the uuid array field
                    for (uint32_t j = 0; j < uuid_members->member_count_; ++j) {
                        const auto& uuid_member = uuid_members->members_[j];
                        if (std::string(uuid_member.name_) == "uuid") {
                            void* uuid_array_ptr = static_cast<char*>(goal_id_ptr) + uuid_member.offset_;
                            generate_uuid(static_cast<uint8_t*>(uuid_array_ptr));
                            // Copy UUID to output if requested
                            if (out_uuid) {
                                std::memcpy(out_uuid->data(), uuid_array_ptr, 16);
                            }
                            return true;
                        }
                    }
                }
            }
            break;
        }
    }

    log.warn("Could not find goal_id.uuid field in SendGoal_Request");
    return false;
}

bool DynamicActionClient::fill_goal_id_from_uuid(void* request_msg, const std::array<uint8_t, 16>& uuid) {
    if (!request_msg || !get_result_request_members_) {
        return false;
    }

    // Find goal_id field in GetResult_Request
    for (uint32_t i = 0; i < get_result_request_members_->member_count_; ++i) {
        const auto& member = get_result_request_members_->members_[i];
        if (std::string(member.name_) == "goal_id") {
            void* goal_id_ptr = static_cast<char*>(request_msg) + member.offset_;

            // goal_id is a unique_identifier_msgs/msg/UUID which has uuid[16] array
            if (member.members_) {
                const auto* uuid_members = MessageConverter::get_nested_members(member.members_);
                if (uuid_members) {
                    for (uint32_t j = 0; j < uuid_members->member_count_; ++j) {
                        const auto& uuid_member = uuid_members->members_[j];
                        if (std::string(uuid_member.name_) == "uuid") {
                            void* uuid_array_ptr = static_cast<char*>(goal_id_ptr) + uuid_member.offset_;
                            std::memcpy(uuid_array_ptr, uuid.data(), 16);
                            return true;
                        }
                    }
                }
            }
            break;
        }
    }

    log.warn("Could not find goal_id.uuid field in GetResult_Request");
    return false;
}

bool DynamicActionClient::fill_goal_field(void* request_msg, const nlohmann::json& json) {
    if (!request_msg || !send_goal_request_members_) {
        return false;
    }

    // Find goal field in SendGoal_Request
    for (uint32_t i = 0; i < send_goal_request_members_->member_count_; ++i) {
        const auto& member = send_goal_request_members_->members_[i];
        if (std::string(member.name_) == "goal") {
            void* goal_ptr = static_cast<char*>(request_msg) + member.offset_;

            // The goal field's type should match goal_members_
            if (goal_members_) {
                return MessageConverter::json_to_message(json, goal_members_, goal_ptr);
            }
            break;
        }
    }

    log.warn("Could not find goal field in SendGoal_Request");
    return false;
}

bool DynamicActionClient::init_client() {
    if (client_initialized_) {
        return true;
    }

    if (!type_info_ || !type_info_->action_ts) {
        log.error("No action type support available");
        return false;
    }

    rcl_action_client_options_t options = rcl_action_client_get_default_options();

    rcl_ret_t ret = rcl_action_client_init(
        &action_client_,
        node_->get_node_base_interface()->get_rcl_node_handle(),
        type_info_->action_ts,
        action_name_.c_str(),
        &options);

    if (ret != RCL_RET_OK) {
        log.error("Failed to init rcl_action_client: {}", rcl_get_error_string().str);
        rcl_reset_error();
        return false;
    }

    client_initialized_ = true;
    log.info("RCL action client initialized for {}", action_name_);

    // Start polling timer
    poll_timer_ = node_->create_wall_timer(
        std::chrono::milliseconds(50),
        [this]() { poll_for_responses(); }
    );

    return true;
}

void DynamicActionClient::cleanup_client() {
    if (client_initialized_) {
        rcl_action_client_fini(
            &action_client_,
            node_->get_node_base_interface()->get_rcl_node_handle());
        client_initialized_ = false;
    }
}

bool DynamicActionClient::is_server_ready() const {
    if (!client_initialized_) {
        return false;
    }

    bool is_ready = false;
    rcl_ret_t ret = rcl_action_server_is_available(
        node_->get_node_base_interface()->get_rcl_node_handle(),
        &action_client_,
        &is_ready);

    return ret == RCL_RET_OK && is_ready;
}

bool DynamicActionClient::wait_for_server(std::chrono::milliseconds timeout) {
    if (!init_client()) {
        return false;
    }

    log.info("Waiting for action server {} (timeout: {}ms)", action_name_, timeout.count());

    auto start = std::chrono::steady_clock::now();
    while (std::chrono::steady_clock::now() - start < timeout) {
        if (is_server_ready()) {
            log.info("Action server {} is ready", action_name_);
            return true;
        }
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }

    log.warn("Action server {} not available after {}ms", action_name_, timeout.count());
    return false;
}

std::shared_ptr<ActionGoalHandle> DynamicActionClient::send_goal(
    const std::string& goal_json,
    std::function<void(bool, const std::string&)> result_callback,
    std::function<void(const std::string&)> feedback_callback) {

    if (!client_initialized_) {
        log.error("Client not initialized");
        if (result_callback) {
            result_callback(false, R"({"error": "Client not initialized"})");
        }
        return nullptr;
    }

    // Parse JSON
    nlohmann::json json;
    try {
        json = nlohmann::json::parse(goal_json);
    } catch (const std::exception& e) {
        log.error("Failed to parse goal JSON: {}", e.what());
        if (result_callback) {
            result_callback(false, R"({"error": "Invalid JSON"})");
        }
        return nullptr;
    }

    // Allocate SendGoal_Request message (contains goal_id + goal)
    void* request_msg = MessageConverter::allocate_message(send_goal_request_members_);
    if (!request_msg) {
        log.error("Failed to allocate SendGoal_Request message");
        if (result_callback) {
            result_callback(false, R"({"error": "Failed to allocate message"})");
        }
        return nullptr;
    }

    // Create goal handle first to store UUID
    auto handle = std::make_shared<ActionGoalHandle>();
    handle->result_callback = result_callback;
    handle->feedback_callback = feedback_callback;

    // Fill goal_id with a random UUID and store it in handle
    if (!fill_goal_id(request_msg, &handle->uuid)) {
        log.warn("Failed to fill goal_id, using default");
    }

    // Fill goal field from JSON
    if (!fill_goal_field(request_msg, json)) {
        log.warn("Some goal fields may not have been set correctly");
    }

    int64_t sequence = next_sequence_++;

    // Send goal request
    rcl_ret_t ret = rcl_action_send_goal_request(&action_client_, request_msg, &sequence);

    MessageConverter::deallocate_message(send_goal_request_members_, request_msg);

    if (ret != RCL_RET_OK) {
        log.error("Failed to send goal: {}", rcl_get_error_string().str);
        rcl_reset_error();
        if (result_callback) {
            result_callback(false, R"({"error": "Failed to send goal"})");
        }
        return nullptr;
    }

    // Store handle
    {
        std::lock_guard<std::mutex> lock(goals_mutex_);
        handle->goal_id = "goal_" + std::to_string(sequence);
        active_goals_[sequence] = handle;
    }

    log.info("Goal sent with sequence {}", sequence);
    return handle;
}

void DynamicActionClient::cancel_goal(std::shared_ptr<ActionGoalHandle> handle) {
    if (!handle || !handle->active.load()) {
        return;
    }

    log.info("Cancelling goal {}", handle->goal_id);
    handle->active.store(false);

    // Send cancel request
    // Note: Full implementation would use rcl_action_send_cancel_request
}

void DynamicActionClient::poll_for_responses() {
    if (!client_initialized_) {
        return;
    }

    // Check for goal responses (SendGoal_Response)
    rmw_request_id_t request_header;
    if (send_goal_response_members_) {
        void* goal_response = MessageConverter::allocate_message(send_goal_response_members_);
        if (goal_response) {
            rcl_ret_t ret = rcl_action_take_goal_response(&action_client_, &request_header, goal_response);
            if (ret == RCL_RET_OK) {
                log.debug("Received goal response for sequence {}", request_header.sequence_number);
                process_goal_response(request_header.sequence_number);
            }
            MessageConverter::deallocate_message(send_goal_response_members_, goal_response);
        }
    }

    // Check for results (GetResult_Response)
    if (get_result_response_members_) {
        void* result_response = MessageConverter::allocate_message(get_result_response_members_);
        if (result_response) {
            rcl_ret_t ret = rcl_action_take_result_response(&action_client_, &request_header, result_response);
            if (ret == RCL_RET_OK) {
                log.info("Received result for sequence {}", request_header.sequence_number);

                // GetResult_Response contains { status: int8, result: Result }
                // We need to extract the result field and convert it
                nlohmann::json response_json = MessageConverter::message_to_json(get_result_response_members_, result_response);
                log.info("Result response JSON: {}", response_json.dump());

                // Extract result field if present, otherwise use whole response
                nlohmann::json result_json = response_json;
                if (response_json.contains("result")) {
                    result_json = response_json["result"];
                }

                // Get status from response
                int8_t status = 0;
                if (response_json.contains("status")) {
                    status = response_json["status"].get<int8_t>();
                }
                log.info("Result status: {} (4=SUCCEEDED, 5=CANCELED, 6=ABORTED)", static_cast<int>(status));

                std::shared_ptr<ActionGoalHandle> handle;
                {
                    std::lock_guard<std::mutex> lock(goals_mutex_);
                    auto it = active_goals_.find(request_header.sequence_number);
                    if (it != active_goals_.end()) {
                        handle = it->second;
                        active_goals_.erase(it);
                    }
                }

                if (handle && handle->result_callback) {
                    // status == 4 means SUCCEEDED in action_msgs/msg/GoalStatus
                    bool success = (status == 4);
                    log.info("Calling result callback: success={}", success);
                    handle->active.store(false);
                    handle->result_callback(success, result_json.dump());
                }
            }
            MessageConverter::deallocate_message(get_result_response_members_, result_response);
        }
    }

    // Check for feedback (FeedbackMessage wraps goal_id + feedback)
    if (feedback_message_members_) {
        void* feedback_msg = MessageConverter::allocate_message(feedback_message_members_);
        if (feedback_msg) {
            rcl_ret_t ret = rcl_action_take_feedback(&action_client_, feedback_msg);
            if (ret == RCL_RET_OK) {
                // FeedbackMessage = { goal_id: UUID, feedback: Feedback }
                // Extract just the feedback part
                nlohmann::json msg_json = MessageConverter::message_to_json(feedback_message_members_, feedback_msg);
                nlohmann::json feedback_json = msg_json;
                if (msg_json.contains("feedback")) {
                    feedback_json = msg_json["feedback"];
                }

                // Broadcast to all active goals (simplified - should match by goal_id)
                std::lock_guard<std::mutex> lock(goals_mutex_);
                for (auto& [seq, handle] : active_goals_) {
                    if (handle->active.load() && handle->feedback_callback) {
                        handle->feedback_callback(feedback_json.dump());
                    }
                }
            }
            MessageConverter::deallocate_message(feedback_message_members_, feedback_msg);
        }
    }
}

void DynamicActionClient::process_goal_response(int64_t sequence) {
    std::lock_guard<std::mutex> lock(goals_mutex_);
    auto it = active_goals_.find(sequence);
    if (it != active_goals_.end()) {
        log.info("Goal {} accepted", it->second->goal_id);

        // Send a result request to get the action result
        // We need to send a GetResult_Request containing the goal_id (UUID)
        if (get_result_request_members_) {
            void* result_request = MessageConverter::allocate_message(get_result_request_members_);
            if (result_request) {
                // Fill the goal_id with the UUID we stored when sending the goal
                if (!fill_goal_id_from_uuid(result_request, it->second->uuid)) {
                    log.warn("Failed to fill goal_id in result request");
                }

                int64_t result_sequence = sequence;
                rcl_ret_t ret = rcl_action_send_result_request(&action_client_, result_request, &result_sequence);
                if (ret != RCL_RET_OK) {
                    log.warn("Failed to send result request: {}", rcl_get_error_string().str);
                    rcl_reset_error();
                } else {
                    log.debug("Result request sent for goal {} with matching UUID", it->second->goal_id);
                }
                MessageConverter::deallocate_message(get_result_request_members_, result_request);
            }
        }
    }
}

void DynamicActionClient::process_result(int64_t /*sequence*/) {
    // Handled in poll_for_responses
}

void DynamicActionClient::process_feedback() {
    // Handled in poll_for_responses
}

}  // namespace executor
}  // namespace fleet_agent
