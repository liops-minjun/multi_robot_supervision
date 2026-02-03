// Copyright 2026 Multi-Robot Supervision System
// Capability Scanner Interface - Abstraction for action server discovery

#pragma once

#include <cstdint>
#include <functional>
#include <memory>
#include <string>
#include <vector>
#include <optional>

namespace robot_agent::interfaces {

/**
 * LifecycleState - ROS2 Lifecycle Node state.
 *
 * Maps to standard ROS2 lifecycle states:
 *   UNCONFIGURED: Node created but not configured
 *   INACTIVE: Configured but not processing
 *   ACTIVE: Fully operational
 *   FINALIZED: Shutting down
 *   UNKNOWN: Non-lifecycle node or state unknown
 */
enum class LifecycleState : uint8_t {
    UNKNOWN = 0,
    UNCONFIGURED = 1,
    INACTIVE = 2,
    ACTIVE = 3,
    FINALIZED = 4
};

/**
 * Convert LifecycleState to string for display/logging.
 */
inline const char* lifecycle_state_to_string(LifecycleState state) {
    switch (state) {
        case LifecycleState::UNCONFIGURED: return "unconfigured";
        case LifecycleState::INACTIVE: return "inactive";
        case LifecycleState::ACTIVE: return "active";
        case LifecycleState::FINALIZED: return "finalized";
        default: return "unknown";
    }
}

/**
 * Parse LifecycleState from string.
 */
inline LifecycleState lifecycle_state_from_string(const std::string& str) {
    if (str == "unconfigured") return LifecycleState::UNCONFIGURED;
    if (str == "inactive") return LifecycleState::INACTIVE;
    if (str == "active") return LifecycleState::ACTIVE;
    if (str == "finalized") return LifecycleState::FINALIZED;
    return LifecycleState::UNKNOWN;
}

/**
 * CapabilityInfo - Information about a discovered action server capability.
 */
struct CapabilityInfo {
    std::string action_type;      // e.g., "nav2_msgs/action/NavigateToPose"
    std::string action_server;    // e.g., "/navigate_to_pose"
    std::string package;          // e.g., "nav2_msgs"
    std::string action_name;      // e.g., "NavigateToPose"
    std::string node_name;        // Node hosting the action server

    // Availability
    bool is_available{false};
    LifecycleState lifecycle_state{LifecycleState::UNKNOWN};

    // Schemas (JSON strings)
    std::string goal_schema;
    std::string result_schema;
    std::string feedback_schema;

    // Success criteria
    struct SuccessCriteria {
        std::string field;
        std::string op;      // "==", "!=", "<", ">"
        std::string value;
    };
    std::optional<SuccessCriteria> success_criteria;
};

/**
 * ICapabilityScanner - Abstract interface for action server discovery.
 *
 * This interface decouples capability discovery from the specific
 * middleware implementation (ROS2, etc.), enabling:
 *   - Unit testing with mock scanners
 *   - Support for different middleware systems
 *   - Lifecycle node state querying
 */
class ICapabilityScanner {
public:
    virtual ~ICapabilityScanner() = default;

    // ============================================================
    // Scanning
    // ============================================================

    /**
     * Perform a full scan for action servers.
     * Discovers new action servers and updates existing ones.
     */
    virtual void scan() = 0;

    /**
     * Refresh availability status of known capabilities.
     * Faster than full scan, only checks if existing servers are still alive.
     */
    virtual void refresh() = 0;

    // ============================================================
    // Capability Access
    // ============================================================

    /**
     * Get all discovered capabilities.
     * @return Vector of capability information
     */
    virtual std::vector<CapabilityInfo> get_capabilities() const = 0;

    /**
     * Get a specific capability by action type.
     * @param action_type Full action type string
     * @return Capability info if found
     */
    virtual std::optional<CapabilityInfo> get_capability(
        const std::string& action_type) const = 0;

    /**
     * Get a specific capability by action server name.
     * @param server_name Action server name (e.g., "/navigate_to_pose")
     * @return Capability info if found
     */
    virtual std::optional<CapabilityInfo> get_capability_by_server(
        const std::string& server_name) const = 0;

    // ============================================================
    // Lifecycle State
    // ============================================================

    /**
     * Query the lifecycle state of a node.
     * @param node_name Full node name (e.g., "/fibonacci_action_server")
     * @return Lifecycle state (UNKNOWN if not a lifecycle node)
     */
    virtual LifecycleState get_lifecycle_state(
        const std::string& node_name) const = 0;

    /**
     * Check if a node is a lifecycle node.
     * @param node_name Full node name
     * @return true if the node supports lifecycle management
     */
    virtual bool is_lifecycle_node(const std::string& node_name) const = 0;

    // ============================================================
    // Callbacks
    // ============================================================

    using CapabilityChangeCallback = std::function<void(
        const std::vector<CapabilityInfo>& capabilities)>;

    /**
     * Set callback for capability changes.
     * Called when capabilities are added, removed, or their status changes.
     * @param cb Callback function
     */
    virtual void set_change_callback(CapabilityChangeCallback cb) = 0;
};

}  // namespace robot_agent::interfaces
