// Copyright 2026 Multi-Robot Supervision System
// Capability Scanner - Auto-discovery of ROS2 action servers

#pragma once

#include "fleet_agent/core/types.hpp"
#include "fleet_agent/capability/type_loader.hpp"
#include "fleet_agent/capability/schema_extractor.hpp"

#include <memory>
#include <optional>
#include <string>
#include <vector>

#include <rclcpp/rclcpp.hpp>

namespace fleet_agent {
namespace capability {

/**
 * CapabilityScanner - Discovers ROS2 action servers automatically.
 *
 * Scans for action servers in a robot's namespace and extracts
 * capability information including:
 * - Action type and server names
 * - Goal/Result/Feedback JSON schemas
 * - Auto-inferred success criteria
 *
 * Zero-config design: Only the ROS namespace is required.
 * All capabilities are discovered at runtime.
 *
 * Usage:
 *   CapabilityScanner scanner(node, "/robot_001", capability_store);
 *   scanner.scan_all();  // Discover all action servers
 *
 *   auto nav_cap = scanner.get("nav2_msgs/NavigateToPose");
 *   if (nav_cap) {
 *       std::cout << "Server: " << nav_cap->action_server << std::endl;
 *   }
 */
class CapabilityScanner {
public:
    /**
     * Constructor.
     *
     * @param node ROS2 node for querying action servers
     * @param namespace_filter Robot namespace (e.g., "/robot_001")
     * @param store Shared capability store
     */
    CapabilityScanner(
        rclcpp::Node::SharedPtr node,
        const std::string& namespace_filter,
        CapabilityStore& store
    );

    ~CapabilityScanner();

    // ============================================================
    // Scanning Operations
    // ============================================================

    /**
     * Scan all action servers in namespace.
     *
     * Performs initial discovery - should be called at startup.
     * Blocking operation.
     *
     * @return Number of capabilities discovered
     */
    int scan_all();

    /**
     * Refresh capability list.
     *
     * Checks for new/removed action servers.
     * Lighter weight than scan_all().
     *
     * @return Number of changes detected
     */
    int refresh();

    // ============================================================
    // Capability Access
    // ============================================================

    /**
     * Get capability by action type.
     *
     * @param action_type Full or normalized action type
     * @return ActionCapability if found
     */
    std::optional<ActionCapability> get(const std::string& action_type) const;

    /**
     * Get action server path for action type.
     *
     * @param action_type Action type to look up
     * @return Server path (e.g., "/robot_001/navigate_to_pose")
     */
    std::optional<std::string> get_server(const std::string& action_type) const;

    /**
     * Check if action type is available.
     */
    bool is_available(const std::string& action_type) const;

    /**
     * Get all discovered capabilities.
     */
    std::vector<ActionCapability> get_all() const;

    /**
     * Get capabilities as protobuf-compatible vector.
     */
    std::vector<ActionCapability> get_for_registration() const;

    // ============================================================
    // State Management
    // ============================================================

    /**
     * Set executing state for an action type.
     *
     * @param action_type Action type being executed
     * @param executing Whether execution is in progress
     */
    void set_executing(const std::string& action_type, bool executing);

    /**
     * Mark capability as unavailable.
     */
    void set_unavailable(const std::string& action_type);

    /**
     * Get count of discovered capabilities.
     */
    size_t count() const;

private:
    rclcpp::Node::SharedPtr node_;
    std::string namespace_filter_;
    CapabilityStore& store_;

    TypeSupportLoader type_loader_;
    SchemaExtractor schema_extractor_;
    SuccessCriteriaInferrer success_inferrer_;

    /**
     * Scan a single action server.
     *
     * @param server_name Full server path (e.g., "/robot_001/navigate_to_pose")
     * @param action_type Action type (e.g., "nav2_msgs/action/NavigateToPose")
     * @return true if successfully scanned
     */
    bool scan_action_server(
        const std::string& server_name,
        const std::string& action_type
    );

    /**
     * Normalize action type string.
     *
     * Converts various formats to canonical form:
     *   "nav2_msgs/action/NavigateToPose" -> "nav2_msgs/NavigateToPose"
     *   "nav2_msgs/NavigateToPose"        -> "nav2_msgs/NavigateToPose"
     */
    std::string normalize_action_type(const std::string& full_type) const;

    /**
     * Extract package name from action type.
     */
    std::string extract_package(const std::string& action_type) const;

    /**
     * Extract action name from action type.
     */
    std::string extract_action_name(const std::string& action_type) const;

    /**
     * Check if action server is in our namespace.
     */
    bool is_in_namespace(const std::string& server_name) const;

    /**
     * Wait for action server to be available.
     */
    bool wait_for_server(const std::string& server_name, int timeout_ms = 1000);
};

}  // namespace capability
}  // namespace fleet_agent
