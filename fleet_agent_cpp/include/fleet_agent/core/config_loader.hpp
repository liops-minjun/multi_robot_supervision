// Copyright 2026 Multi-Robot Supervision System
// YAML Configuration Loader

#pragma once

#include "fleet_agent/core/config.hpp"
#include <string>
#include <stdexcept>

namespace fleet_agent {

// ============================================================
// Configuration Loading Exception
// ============================================================

class ConfigLoadError : public std::runtime_error {
public:
    explicit ConfigLoadError(const std::string& msg)
        : std::runtime_error(msg) {}
};

class ConfigValidationError : public std::runtime_error {
public:
    explicit ConfigValidationError(const std::string& msg)
        : std::runtime_error(msg) {}
};

// ============================================================
// Configuration Loader Interface
// ============================================================

/**
 * Load agent configuration from YAML file.
 *
 * @param config_path Path to YAML configuration file
 * @return Loaded and validated AgentConfig
 * @throws ConfigLoadError if file cannot be read or parsed
 * @throws ConfigValidationError if configuration is invalid
 *
 * Example usage:
 *   auto config = fleet_agent::load_config("/etc/fleet_agent/agent.yaml");
 */
AgentConfig load_config(const std::string& config_path);

/**
 * Load configuration from YAML string.
 *
 * @param yaml_content YAML content string
 * @return Loaded and validated AgentConfig
 */
AgentConfig load_config_from_string(const std::string& yaml_content);

/**
 * Expand environment variables in string.
 *
 * Supports formats:
 *   ${VAR}          - Required variable
 *   ${VAR:-default} - Variable with default value
 *
 * @param value String potentially containing env var references
 * @return String with env vars expanded
 */
std::string expand_env_vars(const std::string& value);

/**
 * Validate configuration.
 *
 * @param config Configuration to validate
 * @throws ConfigValidationError if configuration is invalid
 */
void validate_config(const AgentConfig& config);

/**
 * Apply default values to configuration.
 *
 * @param config Configuration to modify
 */
void apply_defaults(AgentConfig& config);

/**
 * Save configuration to YAML file.
 *
 * @param config Configuration to save
 * @param config_path Path to output file
 * @return true if saved successfully
 */
bool save_config(const AgentConfig& config, const std::string& config_path);

/**
 * Get example configuration YAML string.
 *
 * @return Example YAML configuration
 */
std::string get_example_config();

}  // namespace fleet_agent
