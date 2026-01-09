// Copyright 2026 Multi-Robot Supervision System
// Capability Scanner implementation

#include "fleet_agent/capability/scanner.hpp"
#include "fleet_agent/core/logger.hpp"

#include <rcl_action/graph.h>
#include <rcl/graph.h>

#include <algorithm>
#include <chrono>
#include <map>
#include <unordered_set>

namespace fleet_agent {
namespace capability {

namespace {
logging::ComponentLogger log("CapabilityScanner");

/**
 * Discover action servers using the rcl_action graph API.
 *
 * This is the same method used by `ros2 action list` and properly
 * discovers all action servers including those with hidden services.
 *
 * Returns map of action_server_name -> vector of action_types
 */
std::map<std::string, std::vector<std::string>> discover_action_servers(
    rclcpp::Node::SharedPtr node) {

    std::map<std::string, std::vector<std::string>> result;

    // Get the rcl node handle from the rclcpp node
    auto node_base = node->get_node_base_interface();
    rcl_node_t * rcl_node = node_base->get_rcl_node_handle();

    // Initialize allocator and names_and_types structure
    rcl_allocator_t allocator = rcl_get_default_allocator();
    rcl_names_and_types_t action_names_and_types = rcl_get_zero_initialized_names_and_types();

    // Call the rcl_action API to get all action names and types
    rcl_ret_t ret = rcl_action_get_names_and_types(
        rcl_node,
        &allocator,
        &action_names_and_types);

    if (ret != RCL_RET_OK) {
        log.error("Failed to get action names and types: {}", rcl_get_error_string().str);
        rcl_reset_error();
        return result;
    }

    // Convert rcl_names_and_types_t to our map format
    for (size_t i = 0; i < action_names_and_types.names.size; ++i) {
        std::string action_name = action_names_and_types.names.data[i];
        rcl_names_and_types_t * types = &action_names_and_types;

        for (size_t j = 0; j < types->types[i].size; ++j) {
            std::string action_type = types->types[i].data[j];
            result[action_name].push_back(action_type);
            log.debug("Found action server via rcl_action API: {} (type: {})",
                     action_name, action_type);
        }
    }

    // Clean up
    rcl_ret_t fini_ret = rcl_names_and_types_fini(&action_names_and_types);
    if (fini_ret != RCL_RET_OK) {
        log.warn("Failed to finalize names_and_types: {}", rcl_get_error_string().str);
        rcl_reset_error();
    }

    log.info("Action graph discovery found {} action servers", result.size());
    return result;
}

}

CapabilityScanner::CapabilityScanner(
    rclcpp::Node::SharedPtr node,
    const std::string& namespace_filter,
    CapabilityStore& store)
    : node_(node)
    , namespace_filter_(namespace_filter)
    , store_(store) {

    // Ensure namespace starts with /
    if (!namespace_filter_.empty() && namespace_filter_[0] != '/') {
        namespace_filter_ = "/" + namespace_filter_;
    }

    log.info("Initialized for namespace: {}", namespace_filter_);
}

CapabilityScanner::~CapabilityScanner() = default;

std::string CapabilityScanner::normalize_action_type(const std::string& full_type) const {
    // Convert "nav2_msgs/action/NavigateToPose" to "nav2_msgs/NavigateToPose"
    std::string normalized = full_type;

    // Remove "/action/" if present
    size_t pos = normalized.find("/action/");
    if (pos != std::string::npos) {
        normalized = normalized.substr(0, pos) + "/" + normalized.substr(pos + 8);
    }

    return normalized;
}

std::string CapabilityScanner::extract_package(const std::string& action_type) const {
    size_t pos = action_type.find('/');
    if (pos != std::string::npos) {
        return action_type.substr(0, pos);
    }
    return "";
}

std::string CapabilityScanner::extract_action_name(const std::string& action_type) const {
    size_t pos = action_type.rfind('/');
    if (pos != std::string::npos) {
        return action_type.substr(pos + 1);
    }
    return action_type;
}

bool CapabilityScanner::is_in_namespace(const std::string& server_name) const {
    if (namespace_filter_.empty()) {
        return true;
    }
    return server_name.find(namespace_filter_) == 0;
}

bool CapabilityScanner::wait_for_server(const std::string& server_name, int timeout_ms) {
    // This is a simplified check - in production, use rclcpp_action::Client
    // For ROS2 Humble, we use topic-based discovery
    (void)timeout_ms;  // Not used in this simplified check

    auto action_servers = discover_action_servers(node_);
    return action_servers.find(server_name) != action_servers.end();
}

bool CapabilityScanner::scan_action_server(
    const std::string& server_name,
    const std::string& action_type) {

    log.debug("Scanning action server: {} ({})", server_name, action_type);

    // Load type support
    auto type_info = type_loader_.load(action_type);
    if (!type_info || !type_info->valid) {
        log.warn("Failed to load type support for: {}", action_type);
        return false;
    }

    // Extract schemas
    std::string goal_schema, result_schema, feedback_schema;

    if (type_info->goal_ts) {
        goal_schema = schema_extractor_.extract_json_schema(type_info->goal_ts);
    }
    if (type_info->result_ts) {
        result_schema = schema_extractor_.extract_json_schema(type_info->result_ts);
    }
    if (type_info->feedback_ts) {
        feedback_schema = schema_extractor_.extract_json_schema(type_info->feedback_ts);
    }

    // Infer success criteria from result schema
    SuccessCriteria success_criteria;
    if (!result_schema.empty()) {
        success_criteria = success_inferrer_.infer(result_schema);
    }

    // Build capability info
    ActionCapability capability;
    capability.action_type = normalize_action_type(action_type);
    capability.action_server = server_name;
    capability.package = type_info->package;
    capability.action_name = type_info->action_name;
    capability.goal_schema_json = goal_schema;
    capability.result_schema_json = result_schema;
    capability.feedback_schema_json = feedback_schema;
    capability.success_criteria = success_criteria;
    capability.available.store(true);
    capability.executing.store(false);
    capability.last_seen = std::chrono::steady_clock::now();

    // Store capability (using action_server as key to allow multiple servers of same type)
    CapabilityStore::accessor acc;
    store_.insert(acc, capability.action_server);
    acc->second = capability;

    log.info("Discovered capability: {} at {}",
             capability.action_type, capability.action_server);

    return true;
}

int CapabilityScanner::scan_all() {
    log.info("Starting full capability scan for namespace: {}", namespace_filter_);

    // Get all action servers using topic-based discovery (ROS2 Humble compatible)
    auto action_servers = discover_action_servers(node_);

    int discovered = 0;
    for (const auto& [server_name, types] : action_servers) {
        // Filter by namespace
        if (!is_in_namespace(server_name)) {
            continue;
        }

        // Each action server can have multiple types (rare but possible)
        for (const auto& action_type : types) {
            if (scan_action_server(server_name, action_type)) {
                discovered++;
            }
        }
    }

    log.info("Capability scan complete. Discovered {} capabilities.", discovered);
    return discovered;
}

int CapabilityScanner::refresh() {
    log.debug("Refreshing capability list");

    // Use topic-based discovery (ROS2 Humble compatible)
    auto action_servers = discover_action_servers(node_);
    int changes = 0;

    // Track which capabilities are still present
    std::unordered_set<std::string> current_actions;

    for (const auto& [server_name, types] : action_servers) {
        if (!is_in_namespace(server_name)) {
            continue;
        }

        for (const auto& action_type : types) {
            // Use server_name as key (consistent with scan_action_server)
            current_actions.insert(server_name);

            // Check if this is a new capability
            CapabilityStore::const_accessor acc;
            if (!store_.find(acc, server_name)) {
                // New capability discovered
                if (scan_action_server(server_name, action_type)) {
                    changes++;
                    log.info("New capability discovered: {} at {}",
                             normalize_action_type(action_type), server_name);
                }
            } else {
                // Update last_seen time
                CapabilityStore::accessor write_acc;
                if (store_.find(write_acc, server_name)) {
                    bool was_available = write_acc->second.available.load();
                    write_acc->second.last_seen = std::chrono::steady_clock::now();
                    write_acc->second.available.store(true);
                    if (!was_available) {
                        changes++;
                        log.info("Capability available again: {}", server_name);
                    }
                }
            }
        }
    }

    // Mark removed capabilities as unavailable
    std::vector<std::string> to_remove;
    for (auto it = store_.begin(); it != store_.end(); ++it) {
        if (current_actions.find(it->first) == current_actions.end()) {
            to_remove.push_back(it->first);
        }
    }

    for (const auto& key : to_remove) {
        CapabilityStore::accessor acc;
        if (store_.find(acc, key)) {
            acc->second.available.store(false);
            log.info("Capability no longer available: {}", key);
            changes++;
        }
    }

    if (changes > 0) {
        log.info("Refresh complete. {} changes detected.", changes);
    }

    return changes;
}

std::optional<ActionCapability> CapabilityScanner::get(const std::string& action_type_or_server) const {
    // First try direct lookup (if it's an action_server key like "/test_A_action")
    CapabilityStore::const_accessor acc;
    if (store_.find(acc, action_type_or_server)) {
        return acc->second;
    }

    // If not found, search by action_type (iterate all capabilities)
    std::string normalized = normalize_action_type(action_type_or_server);
    for (auto it = store_.begin(); it != store_.end(); ++it) {
        if (it->second.action_type == normalized || it->second.action_type == action_type_or_server) {
            return it->second;
        }
    }

    return std::nullopt;
}

std::optional<std::string> CapabilityScanner::get_server(const std::string& action_type) const {
    auto cap = get(action_type);
    if (cap) {
        return cap->action_server;
    }
    return std::nullopt;
}

bool CapabilityScanner::is_available(const std::string& action_type) const {
    auto cap = get(action_type);
    return cap && cap->available.load();
}

std::vector<ActionCapability> CapabilityScanner::get_all() const {
    std::vector<ActionCapability> result;
    for (auto it = store_.begin(); it != store_.end(); ++it) {
        result.push_back(it->second);
    }
    return result;
}

std::vector<ActionCapability> CapabilityScanner::get_for_registration() const {
    // Return ALL capabilities (both available and unavailable) so the server
    // can track which action servers are currently offline.
    // The is_available flag in each capability indicates the current status.
    std::vector<ActionCapability> result;
    for (auto it = store_.begin(); it != store_.end(); ++it) {
        result.push_back(it->second);
    }
    return result;
}

void CapabilityScanner::set_executing(const std::string& action_type_or_server, bool executing) {
    // First try direct lookup (if it's an action_server key)
    CapabilityStore::accessor acc;
    if (store_.find(acc, action_type_or_server)) {
        acc->second.executing.store(executing);
        log.debug("Set {} executing={}", action_type_or_server, executing);
        return;
    }

    // If not found, search by action_type
    std::string normalized = normalize_action_type(action_type_or_server);
    for (auto it = store_.begin(); it != store_.end(); ++it) {
        if (it->second.action_type == normalized) {
            CapabilityStore::accessor write_acc;
            if (store_.find(write_acc, it->first)) {
                write_acc->second.executing.store(executing);
                log.debug("Set {} executing={}", it->first, executing);
            }
            return;
        }
    }
}

void CapabilityScanner::set_unavailable(const std::string& action_type_or_server) {
    // First try direct lookup (if it's an action_server key)
    CapabilityStore::accessor acc;
    if (store_.find(acc, action_type_or_server)) {
        acc->second.available.store(false);
        log.warn("Marked {} as unavailable", action_type_or_server);
        return;
    }

    // If not found, search by action_type
    std::string normalized = normalize_action_type(action_type_or_server);
    for (auto it = store_.begin(); it != store_.end(); ++it) {
        if (it->second.action_type == normalized) {
            CapabilityStore::accessor write_acc;
            if (store_.find(write_acc, it->first)) {
                write_acc->second.available.store(false);
                log.warn("Marked {} as unavailable", it->first);
            }
            return;
        }
    }
}

size_t CapabilityScanner::count() const {
    return store_.size();
}

}  // namespace capability
}  // namespace fleet_agent
