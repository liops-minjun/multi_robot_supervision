// Copyright 2026 Multi-Robot Supervision System
// Action Executor Implementation

#include "fleet_agent/executor/action_executor.hpp"
#include "fleet_agent/core/logger.hpp"

// Generated protobuf headers for ActionStatus enum
#include "fleet/v1/common.pb.h"

#include <nlohmann/json.hpp>
#include <rclcpp/serialization.hpp>

namespace fleet_agent {
namespace executor {

namespace {
logging::ComponentLogger log("ActionExecutor");
}

// ============================================================
// ActionExecutor Implementation
// ============================================================

ActionExecutor::ActionExecutor(
    rclcpp::Node::SharedPtr node,
    const std::string& robot_id,
    const std::string& ros_namespace,
    CapabilityStore& capabilities,
    ActionResultCallback result_callback,
    ActionFeedbackCallback feedback_callback)
    : node_(node)
    , robot_id_(robot_id)
    , ros_namespace_(ros_namespace)
    , capabilities_(capabilities)
    , result_callback_(result_callback)
    , feedback_callback_(feedback_callback) {

    log.info("[{}] Initialized", robot_id_);
}

ActionExecutor::~ActionExecutor() {
    if (executing_.load()) {
        cancel("Executor destroyed");
    }
}

std::optional<std::string> ActionExecutor::resolve_server(const std::string& action_type) {
    // Normalize action_type for comparison (remove "/action/" if present)
    std::string normalized = action_type;
    size_t pos = normalized.find("/action/");
    if (pos != std::string::npos) {
        normalized = normalized.substr(0, pos) + "/" + normalized.substr(pos + 8);
    }

    // CapabilityStore uses action_server as key, so we need to iterate
    // and find by action_type field, preferring robot namespace match
    std::optional<std::string> fallback_server;

    for (auto it = capabilities_.begin(); it != capabilities_.end(); ++it) {
        const auto& cap = it->second;
        if (!cap.available.load()) {
            continue;
        }

        // Check if action_type matches (exact or normalized)
        if (cap.action_type != action_type && cap.action_type != normalized) {
            continue;
        }

        // Prefer server that matches robot's namespace prefix
        if (!ros_namespace_.empty() &&
            cap.action_server.find(ros_namespace_) == 0) {
            log.debug("[{}] Resolved {} -> {} (namespace match)",
                     robot_id_, action_type, cap.action_server);
            return cap.action_server;
        }

        // Store as fallback if no namespace match yet
        if (!fallback_server) {
            fallback_server = cap.action_server;
        }
    }

    if (fallback_server) {
        log.debug("[{}] Resolved {} -> {} (fallback)",
                 robot_id_, action_type, *fallback_server);
        return fallback_server;
    }

    log.warn("[{}] No server found for action type: {}", robot_id_, action_type);
    return std::nullopt;
}

bool ActionExecutor::create_action_client(
    const std::string& action_type,
    const std::string& server_name) {

    try {
        action_client_ = std::make_unique<GenericActionClient>(
            node_, server_name, action_type);

        // Wait for server
        if (!action_client_->wait_for_server(std::chrono::milliseconds(5000))) {
            log.error("[{}] Action server {} not available", robot_id_, server_name);
            action_client_.reset();
            return false;
        }

        return true;
    } catch (const std::exception& e) {
        log.error("[{}] Failed to create action client: {}", robot_id_, e.what());
        return false;
    }
}

bool ActionExecutor::execute(const ActionRequest& request) {
    if (executing_.load()) {
        log.warn("[{}] Already executing, rejecting request {}", robot_id_, request.command_id);
        return false;
    }

    // Resolve action server
    std::string server_name;
    if (!request.action_server.empty()) {
        server_name = request.action_server;
    } else {
        auto resolved = resolve_server(request.action_type);
        if (!resolved) {
            log.error("[{}] Cannot resolve server for {}", robot_id_, request.action_type);
            return false;
        }
        server_name = *resolved;
    }

    // Create action client
    if (!create_action_client(request.action_type, server_name)) {
        return false;
    }

    // Store request (with resolved server_name for later use)
    {
        std::lock_guard<std::mutex> lock(request_mutex_);
        current_request_ = request;
        current_request_.action_server = server_name;  // Store resolved server
    }

    executing_.store(true);
    started_at_ = std::chrono::steady_clock::now();

    // Mark capability as executing (use action_server as key)
    CapabilityStore::accessor acc;
    if (capabilities_.find(acc, server_name)) {
        acc->second.executing.store(true);
    }

    log.info("[{}] Starting action {} on server {}",
             robot_id_, request.action_type, server_name);

    // Start timeout timer
    if (request.timeout_sec > 0) {
        auto timeout_duration = std::chrono::duration_cast<std::chrono::nanoseconds>(
            std::chrono::duration<float>(request.timeout_sec));

        timeout_timer_ = node_->create_wall_timer(
            timeout_duration,
            [this]() { this->on_timeout(); }
        );
    }

    // Send goal
    auto goal_handle = action_client_->send_goal(
        request.params_json,
        [this](bool success, const std::string& result_json) {
            this->on_result(success, result_json);
        },
        [this](const std::string& feedback_json) {
            this->on_feedback(feedback_json);
        }
    );

    if (!goal_handle) {
        log.error("[{}] Failed to send goal", robot_id_);
        executing_.store(false);
        return false;
    }

    on_goal_accepted();
    return true;
}

void ActionExecutor::cancel(const std::string& reason) {
    if (!executing_.load()) {
        return;
    }

    log.info("[{}] Cancelling action: {}", robot_id_, reason);

    // Cancel timer
    if (timeout_timer_) {
        timeout_timer_->cancel();
        timeout_timer_.reset();
    }

    // Cancel goal
    if (action_client_) {
        // Note: In real implementation, need to track goal handle
        // action_client_->cancel_goal(current_goal_handle_);
    }

    complete_execution(
        static_cast<int>(fleet::v1::ACTION_STATUS_CANCELLED),
        "{}",
        "Cancelled: " + reason
    );
}

void ActionExecutor::on_goal_accepted() {
    log.debug("[{}] Goal accepted", robot_id_);
}

void ActionExecutor::on_result(bool success, const std::string& result_json) {
    // Cancel timeout timer
    if (timeout_timer_) {
        timeout_timer_->cancel();
        timeout_timer_.reset();
    }

    int status = success
        ? static_cast<int>(fleet::v1::ACTION_STATUS_SUCCEEDED)
        : static_cast<int>(fleet::v1::ACTION_STATUS_FAILED);

    complete_execution(status, result_json, success ? "" : "Action failed");
}

void ActionExecutor::on_feedback(const std::string& feedback_json) {
    if (feedback_callback_) {
        float progress = extract_progress(feedback_json);
        feedback_callback_(robot_id_, progress);
    }
}

void ActionExecutor::on_timeout() {
    log.warn("[{}] Action timed out", robot_id_);

    // Cancel timer (shouldn't fire again)
    if (timeout_timer_) {
        timeout_timer_->cancel();
        timeout_timer_.reset();
    }

    // Cancel action
    if (action_client_) {
        // action_client_->cancel_goal(current_goal_handle_);
    }

    complete_execution(
        static_cast<int>(fleet::v1::ACTION_STATUS_TIMEOUT),
        "{}",
        "Action timed out"
    );
}

void ActionExecutor::complete_execution(
    int status,
    const std::string& result_json,
    const std::string& error) {

    if (!executing_.load()) {
        return;  // Already completed
    }

    executing_.store(false);

    // Mark capability as not executing (use action_server as key)
    std::string action_server;
    {
        std::lock_guard<std::mutex> lock(request_mutex_);
        action_server = current_request_.action_server;
    }

    CapabilityStore::accessor acc;
    if (capabilities_.find(acc, action_server)) {
        acc->second.executing.store(false);
    }

    // Build result
    ActionResultInternal result;
    {
        std::lock_guard<std::mutex> lock(request_mutex_);
        result.command_id = current_request_.command_id;
        result.robot_id = robot_id_;
        result.task_id = current_request_.task_id;
        result.step_id = current_request_.step_id;
    }
    result.status = status;
    result.result_json = result_json;
    result.error = error;
    result.started_at_ms = std::chrono::duration_cast<std::chrono::milliseconds>(
        started_at_.time_since_epoch()).count();
    result.completed_at_ms = now_ms();

    log.info("[{}] Action completed: status={}, error='{}'",
             robot_id_, status, error);

    // Invoke callback
    if (result_callback_) {
        result_callback_(result);
    }
}

float ActionExecutor::extract_progress(const std::string& feedback_json) {
    try {
        auto j = nlohmann::json::parse(feedback_json);
        if (j.contains("progress")) {
            return j["progress"].get<float>();
        }
        if (j.contains("completion_percentage")) {
            return j["completion_percentage"].get<float>() / 100.0f;
        }
        if (j.contains("distance_remaining") && j.contains("distance_total")) {
            float remaining = j["distance_remaining"].get<float>();
            float total = j["distance_total"].get<float>();
            if (total > 0) {
                return 1.0f - (remaining / total);
            }
        }
    } catch (...) {
        // Ignore parse errors
    }
    return 0.0f;
}

std::string ActionExecutor::current_action_type() const {
    std::lock_guard<std::mutex> lock(request_mutex_);
    return current_request_.action_type;
}

std::string ActionExecutor::current_command_id() const {
    std::lock_guard<std::mutex> lock(request_mutex_);
    return current_request_.command_id;
}

std::string ActionExecutor::current_task_id() const {
    std::lock_guard<std::mutex> lock(request_mutex_);
    return current_request_.task_id;
}

std::string ActionExecutor::current_step_id() const {
    std::lock_guard<std::mutex> lock(request_mutex_);
    return current_request_.step_id;
}

// ============================================================
// GenericActionClient Implementation (Skeleton)
// ============================================================

GenericActionClient::GenericActionClient(
    rclcpp::Node::SharedPtr node,
    const std::string& action_name,
    const std::string& action_type)
    : node_(node)
    , action_name_(action_name)
    , action_type_(action_type) {

    // Note: Full implementation requires:
    // 1. Creating generic service clients for _action/send_goal, etc.
    // 2. Creating generic subscriptions for feedback/status
    // 3. Serialization/deserialization using type support

    log.debug("Created GenericActionClient for {} ({})", action_name, action_type);
}

GenericActionClient::~GenericActionClient() = default;

std::shared_ptr<GenericActionClient::GoalHandle> GenericActionClient::send_goal(
    const std::string& goal_json,
    std::function<void(bool, const std::string&)> result_callback,
    std::function<void(const std::string&)> feedback_callback) {

    auto handle = std::make_shared<GoalHandle>();
    handle->goal_id = "goal_" + std::to_string(now_ms());
    handle->result_callback = result_callback;
    handle->feedback_callback = feedback_callback;

    {
        std::lock_guard<std::mutex> lock(goals_mutex_);
        active_goals_[handle->goal_id] = handle;
    }

    // TODO: Implement actual goal sending using generic service client
    // For now, this is a placeholder that simulates success after 2 seconds

    log.debug("Sending goal {} to {}", handle->goal_id, action_name_);

    // Simulate async result (in production, this comes from ROS2 callbacks)
    // node_->create_wall_timer(std::chrono::seconds(2), [handle, result_callback]() {
    //     if (result_callback) {
    //         result_callback(true, "{}");
    //     }
    // });

    return handle;
}

void GenericActionClient::cancel_goal(std::shared_ptr<GoalHandle> handle) {
    if (!handle || !handle->active.load()) {
        return;
    }

    handle->active.store(false);

    // TODO: Implement actual cancellation using generic service client

    {
        std::lock_guard<std::mutex> lock(goals_mutex_);
        active_goals_.erase(handle->goal_id);
    }
}

bool GenericActionClient::is_server_ready() const {
    // TODO: Check if service clients are ready
    return true;
}

bool GenericActionClient::wait_for_server(std::chrono::milliseconds timeout) {
    // TODO: Wait for service clients to be ready
    (void)timeout;
    return true;
}

std::vector<uint8_t> GenericActionClient::serialize_goal(const std::string& goal_json) {
    // TODO: Convert JSON to ROS2 message bytes using type support
    (void)goal_json;
    return {};
}

std::string GenericActionClient::deserialize_result(const std::vector<uint8_t>& data) {
    // TODO: Convert ROS2 message bytes to JSON using type support
    (void)data;
    return "{}";
}

std::string GenericActionClient::deserialize_feedback(const std::vector<uint8_t>& data) {
    // TODO: Convert ROS2 message bytes to JSON using type support
    (void)data;
    return "{}";
}

void GenericActionClient::on_feedback_received(std::shared_ptr<rclcpp::SerializedMessage> msg) {
    // TODO: Deserialize and dispatch to appropriate goal handle
    (void)msg;
}

void GenericActionClient::on_status_received(std::shared_ptr<rclcpp::SerializedMessage> msg) {
    // TODO: Update goal status and trigger result polling if complete
    (void)msg;
}

void GenericActionClient::poll_result(std::shared_ptr<GoalHandle> handle) {
    // TODO: Call get_result service and invoke callback
    (void)handle;
}

}  // namespace executor
}  // namespace fleet_agent
