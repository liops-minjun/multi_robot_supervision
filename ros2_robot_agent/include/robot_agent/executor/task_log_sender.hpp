// Copyright 2026 Multi-Robot Supervision System
// Task Execution Log Sender

#pragma once

#include "robot_agent/core/types.hpp"

#include <memory>
#include <string>
#include <unordered_map>

namespace robot_agent {
namespace executor {

/**
 * Log level for task execution logs.
 * Mirrors fleet::v1::TaskLogLevel enum.
 */
enum class TaskLogLevel {
    DEBUG = 0,
    INFO = 1,
    WARN = 2,
    ERROR = 3
};

/**
 * TaskLogSender - Sends task execution logs to the central server.
 *
 * This class provides a simple interface for sending structured logs
 * about task execution to the server for monitoring and debugging.
 *
 * Usage:
 *   TaskLogSender sender(agent_id, outbound_queue);
 *   sender.set_task_context(task_id, step_id, command_id);
 *   sender.info("Starting action", "ActionExecutor");
 *   sender.error("Action failed", "ActionExecutor", {{"reason", "timeout"}});
 */
class TaskLogSender {
public:
    /**
     * Constructor.
     *
     * @param agent_id The agent ID for all logs
     * @param outbound_queue Queue to push log messages to
     */
    TaskLogSender(const std::string& agent_id,
                  QuicOutboundQueue& outbound_queue);

    ~TaskLogSender() = default;

    // Non-copyable, movable
    TaskLogSender(const TaskLogSender&) = delete;
    TaskLogSender& operator=(const TaskLogSender&) = delete;
    TaskLogSender(TaskLogSender&&) = default;
    TaskLogSender& operator=(TaskLogSender&&) = default;

    // ============================================================
    // Task Context Management
    // ============================================================

    /**
     * Set the current task context for subsequent logs.
     */
    void set_task_context(const std::string& task_id,
                         const std::string& step_id = "",
                         const std::string& command_id = "");

    /**
     * Clear the current task context.
     */
    void clear_task_context();

    // ============================================================
    // Logging Methods
    // ============================================================

    /**
     * Send a debug level log.
     */
    void debug(const std::string& message,
               const std::string& component,
               const std::unordered_map<std::string, std::string>& metadata = {});

    /**
     * Send an info level log.
     */
    void info(const std::string& message,
              const std::string& component,
              const std::unordered_map<std::string, std::string>& metadata = {});

    /**
     * Send a warning level log.
     */
    void warn(const std::string& message,
              const std::string& component,
              const std::unordered_map<std::string, std::string>& metadata = {});

    /**
     * Send an error level log.
     */
    void error(const std::string& message,
               const std::string& component,
               const std::unordered_map<std::string, std::string>& metadata = {});

    /**
     * Send a log with specified level.
     */
    void log(TaskLogLevel level,
             const std::string& message,
             const std::string& component,
             const std::unordered_map<std::string, std::string>& metadata = {});

    /**
     * Send a log with explicit task context (ignores set_task_context).
     */
    void log_with_context(TaskLogLevel level,
                          const std::string& task_id,
                          const std::string& step_id,
                          const std::string& command_id,
                          const std::string& message,
                          const std::string& component,
                          const std::unordered_map<std::string, std::string>& metadata = {});

    // ============================================================
    // Configuration
    // ============================================================

    /**
     * Update agent ID (used when server assigns a new ID).
     */
    void set_agent_id(const std::string& agent_id) { agent_id_ = agent_id; }

    // ============================================================
    // Accessors
    // ============================================================

    const std::string& agent_id() const { return agent_id_; }
    const std::string& current_task_id() const { return current_task_id_; }
    const std::string& current_step_id() const { return current_step_id_; }
    const std::string& current_command_id() const { return current_command_id_; }

private:
    /**
     * Internal method to send a log message.
     */
    void send_log(TaskLogLevel level,
                  const std::string& task_id,
                  const std::string& step_id,
                  const std::string& command_id,
                  const std::string& message,
                  const std::string& component,
                  const std::unordered_map<std::string, std::string>& metadata);

    std::string agent_id_;
    QuicOutboundQueue& outbound_queue_;

    // Current task context
    std::string current_task_id_;
    std::string current_step_id_;
    std::string current_command_id_;
};

}  // namespace executor
}  // namespace robot_agent
