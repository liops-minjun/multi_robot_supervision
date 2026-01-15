// Copyright 2026 Multi-Robot Supervision System
// Task Execution Log Sender Implementation

#include "fleet_agent/executor/task_log_sender.hpp"
#include "fleet_agent/core/logger.hpp"

// Generated protobuf headers
#include "fleet/v1/service.pb.h"
#include "fleet/v1/commands.pb.h"

namespace fleet_agent {
namespace executor {

namespace {
logging::ComponentLogger log("TaskLogSender");
}

TaskLogSender::TaskLogSender(const std::string& agent_id,
                             QuicOutboundQueue& outbound_queue)
    : agent_id_(agent_id)
    , outbound_queue_(outbound_queue) {
}

void TaskLogSender::set_task_context(const std::string& task_id,
                                     const std::string& step_id,
                                     const std::string& command_id) {
    current_task_id_ = task_id;
    current_step_id_ = step_id;
    current_command_id_ = command_id;
}

void TaskLogSender::clear_task_context() {
    current_task_id_.clear();
    current_step_id_.clear();
    current_command_id_.clear();
}

void TaskLogSender::debug(const std::string& message,
                          const std::string& component,
                          const std::unordered_map<std::string, std::string>& metadata) {
    log(TaskLogLevel::DEBUG, message, component, metadata);
}

void TaskLogSender::info(const std::string& message,
                         const std::string& component,
                         const std::unordered_map<std::string, std::string>& metadata) {
    log(TaskLogLevel::INFO, message, component, metadata);
}

void TaskLogSender::warn(const std::string& message,
                         const std::string& component,
                         const std::unordered_map<std::string, std::string>& metadata) {
    log(TaskLogLevel::WARN, message, component, metadata);
}

void TaskLogSender::error(const std::string& message,
                          const std::string& component,
                          const std::unordered_map<std::string, std::string>& metadata) {
    log(TaskLogLevel::ERROR, message, component, metadata);
}

void TaskLogSender::log(TaskLogLevel level,
                        const std::string& message,
                        const std::string& component,
                        const std::unordered_map<std::string, std::string>& metadata) {
    send_log(level, current_task_id_, current_step_id_, current_command_id_,
             message, component, metadata);
}

void TaskLogSender::log_with_context(TaskLogLevel level,
                                     const std::string& task_id,
                                     const std::string& step_id,
                                     const std::string& command_id,
                                     const std::string& message,
                                     const std::string& component,
                                     const std::unordered_map<std::string, std::string>& metadata) {
    send_log(level, task_id, step_id, command_id, message, component, metadata);
}

void TaskLogSender::send_log(TaskLogLevel level,
                             const std::string& task_id,
                             const std::string& step_id,
                             const std::string& command_id,
                             const std::string& message,
                             const std::string& component,
                             const std::unordered_map<std::string, std::string>& metadata) {
    auto msg = std::make_shared<fleet::v1::AgentMessage>();
    msg->set_agent_id(agent_id_);
    msg->set_timestamp_ms(now_ms());

    auto* task_log = msg->mutable_task_log();
    task_log->set_agent_id(agent_id_);
    task_log->set_task_id(task_id);
    task_log->set_step_id(step_id);
    task_log->set_command_id(command_id);
    task_log->set_level(static_cast<fleet::v1::TaskLogLevel>(level));
    task_log->set_message(message);
    task_log->set_timestamp_ms(now_ms());
    task_log->set_component(component);

    // Add metadata
    auto* meta_map = task_log->mutable_metadata();
    for (const auto& [key, value] : metadata) {
        (*meta_map)[key] = value;
    }

    OutboundMessage out;
    out.message = msg;
    out.created_at = std::chrono::steady_clock::now();
    out.priority = 3;  // Lower priority than action results/feedback

    outbound_queue_.push(std::move(out));

    // Also log locally for debugging
    switch (level) {
        case TaskLogLevel::DEBUG:
            ::fleet_agent::executor::log.debug("[{}] {} - {}", component, task_id, message);
            break;
        case TaskLogLevel::INFO:
            ::fleet_agent::executor::log.info("[{}] {} - {}", component, task_id, message);
            break;
        case TaskLogLevel::WARN:
            ::fleet_agent::executor::log.warn("[{}] {} - {}", component, task_id, message);
            break;
        case TaskLogLevel::ERROR:
            ::fleet_agent::executor::log.error("[{}] {} - {}", component, task_id, message);
            break;
    }
}

}  // namespace executor
}  // namespace fleet_agent
