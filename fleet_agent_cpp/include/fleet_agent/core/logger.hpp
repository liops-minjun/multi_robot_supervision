// Copyright 2026 Multi-Robot Supervision System
// Logging utilities

#pragma once

#include <memory>
#include <string>

#include <spdlog/spdlog.h>
#include <spdlog/sinks/stdout_color_sinks.h>
#include <spdlog/sinks/rotating_file_sink.h>

namespace fleet_agent {
namespace logging {

// ============================================================
// Log Level Enum
// ============================================================

enum class LogLevel {
    TRACE,
    DEBUG,
    INFO,
    WARN,
    ERROR,
    CRITICAL,
    OFF
};

// ============================================================
// Logger Configuration
// ============================================================

struct LoggerConfig {
    std::string name{"fleet_agent"};
    LogLevel level{LogLevel::INFO};
    std::string log_file;
    bool console{true};
    bool include_timestamp{true};
    size_t max_file_size_mb{100};
    size_t max_files{5};
};

// ============================================================
// Logger Initialization
// ============================================================

/**
 * Initialize the logging system.
 *
 * @param config Logger configuration
 */
void init(const LoggerConfig& config);

/**
 * Initialize with simple parameters.
 *
 * @param level Log level string (debug, info, warn, error)
 * @param log_file Optional log file path
 */
void init(const std::string& level, const std::string& log_file = "");

/**
 * Shutdown logging system.
 */
void shutdown();

/**
 * Get the main logger.
 *
 * @return Shared pointer to spdlog logger
 */
std::shared_ptr<spdlog::logger> get_logger();

/**
 * Convert string to LogLevel.
 *
 * @param level Level string
 * @return LogLevel enum value
 */
LogLevel parse_level(const std::string& level);

/**
 * Convert LogLevel to spdlog level.
 *
 * @param level LogLevel enum value
 * @return spdlog level enum
 */
spdlog::level::level_enum to_spdlog_level(LogLevel level);

// ============================================================
// Component Logger
// ============================================================

/**
 * ComponentLogger - Prefixed logger for specific components.
 *
 * Usage:
 *   ComponentLogger log("QUICClient");
 *   log.info("Connected to server");
 *   // Output: [QUICClient] Connected to server
 */
class ComponentLogger {
public:
    explicit ComponentLogger(const std::string& component_name);

    template<typename... Args>
    void trace(const char* fmt, Args&&... args) {
        logger_->trace("[{}] {}", prefix_, fmt::format(fmt, std::forward<Args>(args)...));
    }

    template<typename... Args>
    void debug(const char* fmt, Args&&... args) {
        logger_->debug("[{}] {}", prefix_, fmt::format(fmt, std::forward<Args>(args)...));
    }

    template<typename... Args>
    void info(const char* fmt, Args&&... args) {
        logger_->info("[{}] {}", prefix_, fmt::format(fmt, std::forward<Args>(args)...));
    }

    template<typename... Args>
    void warn(const char* fmt, Args&&... args) {
        logger_->warn("[{}] {}", prefix_, fmt::format(fmt, std::forward<Args>(args)...));
    }

    template<typename... Args>
    void error(const char* fmt, Args&&... args) {
        logger_->error("[{}] {}", prefix_, fmt::format(fmt, std::forward<Args>(args)...));
    }

    template<typename... Args>
    void critical(const char* fmt, Args&&... args) {
        logger_->critical("[{}] {}", prefix_, fmt::format(fmt, std::forward<Args>(args)...));
    }

    // Simple string versions
    void trace(const std::string& msg);
    void debug(const std::string& msg);
    void info(const std::string& msg);
    void warn(const std::string& msg);
    void error(const std::string& msg);
    void critical(const std::string& msg);

private:
    std::string prefix_;
    std::shared_ptr<spdlog::logger> logger_;
};

}  // namespace logging

// ============================================================
// Convenience Macros
// ============================================================

// Global logging macros using default logger
#define LOG_TRACE(...) SPDLOG_TRACE(__VA_ARGS__)
#define LOG_DEBUG(...) SPDLOG_DEBUG(__VA_ARGS__)
#define LOG_INFO(...)  SPDLOG_INFO(__VA_ARGS__)
#define LOG_WARN(...)  SPDLOG_WARN(__VA_ARGS__)
#define LOG_ERROR(...) SPDLOG_ERROR(__VA_ARGS__)
#define LOG_CRITICAL(...) SPDLOG_CRITICAL(__VA_ARGS__)

}  // namespace fleet_agent
