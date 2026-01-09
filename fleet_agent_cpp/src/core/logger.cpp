// Copyright 2026 Multi-Robot Supervision System
// Logging utilities implementation

#include "fleet_agent/core/logger.hpp"

#include <algorithm>
#include <spdlog/sinks/basic_file_sink.h>

namespace fleet_agent {
namespace logging {

namespace {
std::shared_ptr<spdlog::logger> g_logger;
bool g_initialized = false;
}  // namespace

LogLevel parse_level(const std::string& level) {
    std::string lower_level = level;
    std::transform(lower_level.begin(), lower_level.end(), lower_level.begin(), ::tolower);

    if (lower_level == "trace") return LogLevel::TRACE;
    if (lower_level == "debug") return LogLevel::DEBUG;
    if (lower_level == "info") return LogLevel::INFO;
    if (lower_level == "warn" || lower_level == "warning") return LogLevel::WARN;
    if (lower_level == "error" || lower_level == "err") return LogLevel::ERROR;
    if (lower_level == "critical" || lower_level == "fatal") return LogLevel::CRITICAL;
    if (lower_level == "off" || lower_level == "none") return LogLevel::OFF;

    return LogLevel::INFO;  // Default
}

spdlog::level::level_enum to_spdlog_level(LogLevel level) {
    switch (level) {
        case LogLevel::TRACE: return spdlog::level::trace;
        case LogLevel::DEBUG: return spdlog::level::debug;
        case LogLevel::INFO: return spdlog::level::info;
        case LogLevel::WARN: return spdlog::level::warn;
        case LogLevel::ERROR: return spdlog::level::err;
        case LogLevel::CRITICAL: return spdlog::level::critical;
        case LogLevel::OFF: return spdlog::level::off;
        default: return spdlog::level::info;
    }
}

void init(const LoggerConfig& config) {
    if (g_initialized) {
        // Already initialized, just update level
        if (g_logger) {
            g_logger->set_level(to_spdlog_level(config.level));
        }
        return;
    }

    std::vector<spdlog::sink_ptr> sinks;

    // Console sink
    if (config.console) {
        auto console_sink = std::make_shared<spdlog::sinks::stdout_color_sink_mt>();
        console_sink->set_level(to_spdlog_level(config.level));
        sinks.push_back(console_sink);
    }

    // File sink
    if (!config.log_file.empty()) {
        try {
            auto file_sink = std::make_shared<spdlog::sinks::rotating_file_sink_mt>(
                config.log_file,
                config.max_file_size_mb * 1024 * 1024,
                config.max_files
            );
            file_sink->set_level(to_spdlog_level(config.level));
            sinks.push_back(file_sink);
        } catch (const spdlog::spdlog_ex& ex) {
            // Log to console if file sink fails
            spdlog::error("Failed to create file sink: {}", ex.what());
        }
    }

    // Create logger
    g_logger = std::make_shared<spdlog::logger>(config.name, sinks.begin(), sinks.end());
    g_logger->set_level(to_spdlog_level(config.level));

    // Set pattern
    if (config.include_timestamp) {
        g_logger->set_pattern("[%Y-%m-%d %H:%M:%S.%e] [%^%l%$] %v");
    } else {
        g_logger->set_pattern("[%^%l%$] %v");
    }

    // Register as default logger
    spdlog::set_default_logger(g_logger);

    // Enable backtrace for error/critical messages
    g_logger->enable_backtrace(32);

    g_initialized = true;
}

void init(const std::string& level, const std::string& log_file) {
    LoggerConfig config;
    config.level = parse_level(level);
    config.log_file = log_file;
    init(config);
}

void shutdown() {
    if (g_logger) {
        g_logger->flush();
    }
    spdlog::shutdown();
    g_initialized = false;
    g_logger.reset();
}

std::shared_ptr<spdlog::logger> get_logger() {
    if (!g_initialized) {
        // Initialize with defaults if not done
        init(LoggerConfig{});
    }
    return g_logger;
}

// ============================================================
// ComponentLogger Implementation
// ============================================================

ComponentLogger::ComponentLogger(const std::string& component_name)
    : prefix_(component_name)
    , logger_(get_logger()) {
}

void ComponentLogger::trace(const std::string& msg) {
    logger_->trace("[{}] {}", prefix_, msg);
}

void ComponentLogger::debug(const std::string& msg) {
    logger_->debug("[{}] {}", prefix_, msg);
}

void ComponentLogger::info(const std::string& msg) {
    logger_->info("[{}] {}", prefix_, msg);
}

void ComponentLogger::warn(const std::string& msg) {
    logger_->warn("[{}] {}", prefix_, msg);
}

void ComponentLogger::error(const std::string& msg) {
    logger_->error("[{}] {}", prefix_, msg);
}

void ComponentLogger::critical(const std::string& msg) {
    logger_->critical("[{}] {}", prefix_, msg);
}

}  // namespace logging
}  // namespace fleet_agent
