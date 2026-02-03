// Copyright 2026 Multi-Robot Supervision System
// ROS2 Action Executor Implementation

#include "robot_agent/executor/ros2_action_executor.hpp"
#include "robot_agent/core/logger.hpp"

#include <nlohmann/json.hpp>

namespace robot_agent::executor {

ROS2ActionExecutor::ROS2ActionExecutor(rclcpp::Node::SharedPtr node)
    : node_(std::move(node))
{
    LOG_INFO("ROS2ActionExecutor: Created");
}

ROS2ActionExecutor::~ROS2ActionExecutor() {
    // Cancel any active goal
    if (is_executing_) {
        cancel_goal();
    }

    // Clear client cache
    {
        std::lock_guard<std::mutex> lock(cache_mutex_);
        client_cache_.clear();
    }

    LOG_INFO("ROS2ActionExecutor: Destroyed");
}

bool ROS2ActionExecutor::send_goal(
    const std::string& action_type,
    const std::string& server_name,
    const std::string& goal_json,
    ResultCallback result_cb,
    FeedbackCallback feedback_cb,
    float timeout_sec)
{
    // Check if already executing
    if (is_executing_) {
        LOG_WARN("ROS2ActionExecutor: Cannot send goal - already executing");
        return false;
    }

    // Get or create client
    auto* client = get_or_create_client(server_name, action_type);
    if (!client) {
        LOG_ERROR("ROS2ActionExecutor: Failed to create client for {} ({})",
                  server_name, action_type);
        return false;
    }

    // Wait for server to be ready
    if (!client->is_server_ready()) {
        LOG_DEBUG("ROS2ActionExecutor: Waiting for server {}", server_name);
        if (!client->wait_for_server(std::chrono::milliseconds(5000))) {
            LOG_ERROR("ROS2ActionExecutor: Server {} not available", server_name);

            // Return failure via callback
            if (result_cb) {
                interfaces::ActionResult result;
                result.status = interfaces::ActionStatus::REJECTED;
                result.error = "Action server not available: " + server_name;
                result_cb(result);
            }
            return false;
        }
    }

    // Store callbacks and state
    {
        std::lock_guard<std::mutex> lock(execution_mutex_);
        result_callback_ = std::move(result_cb);
        feedback_callback_ = std::move(feedback_cb);
        current_action_type_ = action_type;
        current_server_name_ = server_name;
        timeout_sec_ = timeout_sec;
        action_started_at_ = std::chrono::steady_clock::now();
    }

    // Send goal
    auto handle = client->send_goal(
        goal_json,
        [this](bool success, const std::string& result_json) {
            on_result(success, result_json);
        },
        [this](const std::string& feedback_json) {
            on_feedback(feedback_json);
        });

    if (!handle) {
        LOG_ERROR("ROS2ActionExecutor: Failed to send goal to {}", server_name);

        // Return failure via callback
        std::lock_guard<std::mutex> lock(execution_mutex_);
        if (result_callback_) {
            interfaces::ActionResult result;
            result.status = interfaces::ActionStatus::FAILED;
            result.error = "Failed to send goal";
            result_callback_(result);
        }
        clear_execution_state();
        return false;
    }

    // Store handle and mark as executing
    {
        std::lock_guard<std::mutex> lock(execution_mutex_);
        current_goal_handle_ = handle;
    }
    is_executing_ = true;

    LOG_INFO("ROS2ActionExecutor: Sent goal to {} ({})", server_name, action_type);
    return true;
}

bool ROS2ActionExecutor::cancel_goal() {
    if (!is_executing_) {
        LOG_DEBUG("ROS2ActionExecutor: Nothing to cancel");
        return false;
    }

    std::shared_ptr<ActionGoalHandle> handle;
    std::string server_name;

    {
        std::lock_guard<std::mutex> lock(execution_mutex_);
        handle = current_goal_handle_;
        server_name = current_server_name_;
    }

    if (!handle) {
        LOG_WARN("ROS2ActionExecutor: No goal handle to cancel");
        clear_execution_state();
        return false;
    }

    // Get client
    DynamicActionClient* client = nullptr;
    {
        std::lock_guard<std::mutex> lock(cache_mutex_);
        auto it = client_cache_.find(server_name);
        if (it != client_cache_.end()) {
            client = it->second.get();
        }
    }

    if (client) {
        client->cancel_goal(handle);
        LOG_INFO("ROS2ActionExecutor: Cancelled goal on {}", server_name);
    }

    // The result callback will be called with cancelled status
    return true;
}

bool ROS2ActionExecutor::is_executing() const {
    return is_executing_;
}

void ROS2ActionExecutor::poll() {
    if (!is_executing_) {
        return;
    }

    // Check for timeout
    if (timeout_sec_ > 0.0f) {
        auto now = std::chrono::steady_clock::now();
        auto elapsed = std::chrono::duration<float>(now - action_started_at_).count();

        if (elapsed >= timeout_sec_) {
            LOG_WARN("ROS2ActionExecutor: Action timed out after {:.1f}s", elapsed);

            // Cancel the goal
            cancel_goal();

            // Report timeout
            std::lock_guard<std::mutex> lock(execution_mutex_);
            if (result_callback_) {
                interfaces::ActionResult result;
                result.status = interfaces::ActionStatus::TIMEOUT;
                result.error = "Action timed out";
                result.duration_ms = static_cast<int64_t>(elapsed * 1000);
                result_callback_(result);
            }

            clear_execution_state();
        }
    }
}

bool ROS2ActionExecutor::is_server_available(
    const std::string& action_type,
    const std::string& server_name) const
{
    // Try to get existing client
    {
        std::lock_guard<std::mutex> lock(cache_mutex_);
        auto it = client_cache_.find(server_name);
        if (it != client_cache_.end()) {
            return it->second->is_server_ready();
        }
    }

    // Create temporary client to check
    try {
        auto client = std::make_unique<DynamicActionClient>(
            node_, server_name, action_type);
        return client->is_server_ready();
    } catch (const std::exception& e) {
        LOG_DEBUG("ROS2ActionExecutor: Server {} not available: {}",
                  server_name, e.what());
        return false;
    }
}

std::string ROS2ActionExecutor::current_action_type() const {
    std::lock_guard<std::mutex> lock(execution_mutex_);
    return current_action_type_;
}

std::string ROS2ActionExecutor::current_server_name() const {
    std::lock_guard<std::mutex> lock(execution_mutex_);
    return current_server_name_;
}

void ROS2ActionExecutor::clear_client_cache() {
    std::lock_guard<std::mutex> lock(cache_mutex_);
    client_cache_.clear();
    LOG_DEBUG("ROS2ActionExecutor: Cleared client cache");
}

size_t ROS2ActionExecutor::cached_client_count() const {
    std::lock_guard<std::mutex> lock(cache_mutex_);
    return client_cache_.size();
}

DynamicActionClient* ROS2ActionExecutor::get_or_create_client(
    const std::string& server_name,
    const std::string& action_type)
{
    std::lock_guard<std::mutex> lock(cache_mutex_);

    // Check cache
    auto it = client_cache_.find(server_name);
    if (it != client_cache_.end()) {
        // Verify action type matches
        if (it->second->action_type() == action_type) {
            return it->second.get();
        }
        // Different action type, remove and recreate
        LOG_WARN("ROS2ActionExecutor: Action type mismatch for {}, recreating client",
                 server_name);
        client_cache_.erase(it);
    }

    // Create new client
    try {
        auto client = std::make_unique<DynamicActionClient>(
            node_, server_name, action_type);
        auto* ptr = client.get();
        client_cache_[server_name] = std::move(client);
        LOG_DEBUG("ROS2ActionExecutor: Created client for {} ({})",
                  server_name, action_type);
        return ptr;
    } catch (const std::exception& e) {
        LOG_ERROR("ROS2ActionExecutor: Failed to create client for {} ({}): {}",
                  server_name, action_type, e.what());
        return nullptr;
    }
}

void ROS2ActionExecutor::on_result(bool success, const std::string& result_json) {
    auto elapsed_ms = std::chrono::duration_cast<std::chrono::milliseconds>(
        std::chrono::steady_clock::now() - action_started_at_).count();

    LOG_INFO("ROS2ActionExecutor: Action completed - success={}, duration={}ms",
             success, elapsed_ms);

    ResultCallback callback;
    {
        std::lock_guard<std::mutex> lock(execution_mutex_);
        callback = std::move(result_callback_);
    }

    if (callback) {
        interfaces::ActionResult result;
        result.status = success ? interfaces::ActionStatus::SUCCEEDED
                                : interfaces::ActionStatus::FAILED;
        result.result_json = result_json;
        result.duration_ms = elapsed_ms;

        if (!success) {
            // Try to extract error from result JSON
            try {
                auto json = nlohmann::json::parse(result_json);
                if (json.contains("error")) {
                    result.error = json["error"].get<std::string>();
                } else if (json.contains("error_message")) {
                    result.error = json["error_message"].get<std::string>();
                }
            } catch (...) {
                // Ignore JSON parse errors
            }
        }

        callback(result);
    }

    clear_execution_state();
}

void ROS2ActionExecutor::on_feedback(const std::string& feedback_json) {
    FeedbackCallback callback;
    {
        std::lock_guard<std::mutex> lock(execution_mutex_);
        callback = feedback_callback_;
    }

    if (callback) {
        interfaces::ActionFeedback feedback;
        feedback.feedback_json = feedback_json;

        // Try to extract progress from feedback
        try {
            auto json = nlohmann::json::parse(feedback_json);
            if (json.contains("progress")) {
                feedback.progress = json["progress"].get<float>();
            } else if (json.contains("percent_complete")) {
                feedback.progress = json["percent_complete"].get<float>() / 100.0f;
            }
        } catch (...) {
            // Ignore JSON parse errors
        }

        callback(feedback);
    }
}

void ROS2ActionExecutor::clear_execution_state() {
    is_executing_ = false;

    std::lock_guard<std::mutex> lock(execution_mutex_);
    current_action_type_.clear();
    current_server_name_.clear();
    current_goal_handle_.reset();
    result_callback_ = nullptr;
    feedback_callback_ = nullptr;
    timeout_sec_ = 0.0f;
}

}  // namespace robot_agent::executor
