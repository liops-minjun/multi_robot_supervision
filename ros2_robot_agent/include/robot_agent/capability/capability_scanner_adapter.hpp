// Copyright 2026 Multi-Robot Supervision System
// Capability Scanner Adapter - Adapts CapabilityScanner to ICapabilityScanner interface

#pragma once

#include "robot_agent/interfaces/capability_scanner.hpp"
#include "robot_agent/capability/scanner.hpp"
#include "robot_agent/core/types.hpp"

#include <memory>
#include <mutex>

namespace robot_agent::capability {

/**
 * CapabilityScannerAdapter - Adapts CapabilityScanner to ICapabilityScanner interface.
 *
 * This adapter wraps the existing CapabilityScanner and CapabilityStore
 * to conform to the ICapabilityScanner interface, enabling:
 *   - Dependency injection in Agent class
 *   - Unit testing with mock scanners
 *   - Type conversion between ActionCapability and CapabilityInfo
 *
 * Usage:
 *   auto store = std::make_shared<CapabilityStore>();
 *   auto scanner = std::make_shared<CapabilityScanner>(node, namespace, *store);
 *   auto adapter = std::make_unique<CapabilityScannerAdapter>(scanner, store);
 *   adapter->scan();
 */
class CapabilityScannerAdapter : public interfaces::ICapabilityScanner {
public:
    /**
     * Constructor.
     *
     * @param scanner Shared pointer to CapabilityScanner
     * @param store Reference to CapabilityStore for access to capabilities
     */
    CapabilityScannerAdapter(
        std::shared_ptr<CapabilityScanner> scanner,
        CapabilityStore& store
    );

    ~CapabilityScannerAdapter() override = default;

    // ============================================================
    // ICapabilityScanner Implementation
    // ============================================================

    /**
     * Perform a full scan for action servers.
     */
    void scan() override;

    /**
     * Refresh availability status of known capabilities.
     */
    void refresh() override;

    /**
     * Get all discovered capabilities.
     */
    std::vector<interfaces::CapabilityInfo> get_capabilities() const override;

    /**
     * Get a specific capability by action type.
     */
    std::optional<interfaces::CapabilityInfo> get_capability(
        const std::string& action_type) const override;

    /**
     * Get a specific capability by action server name.
     */
    std::optional<interfaces::CapabilityInfo> get_capability_by_server(
        const std::string& server_name) const override;

    /**
     * Query the lifecycle state of a node.
     */
    interfaces::LifecycleState get_lifecycle_state(
        const std::string& node_name) const override;

    /**
     * Check if a node is a lifecycle node.
     */
    bool is_lifecycle_node(const std::string& node_name) const override;

    /**
     * Set callback for capability changes.
     */
    void set_change_callback(CapabilityChangeCallback cb) override;

    // ============================================================
    // Additional Methods
    // ============================================================

    /**
     * Get underlying CapabilityScanner for advanced operations.
     */
    CapabilityScanner* scanner() const { return scanner_.get(); }

    /**
     * Get underlying CapabilityStore for direct access.
     */
    CapabilityStore& store() const { return store_; }

    /**
     * Update lifecycle states for all capabilities.
     * Delegates to scanner's update_lifecycle_states().
     */
    void update_lifecycle_states();

private:
    std::shared_ptr<CapabilityScanner> scanner_;
    CapabilityStore& store_;

    CapabilityChangeCallback change_callback_;
    mutable std::mutex callback_mutex_;

    /**
     * Convert ActionCapability to CapabilityInfo.
     */
    static interfaces::CapabilityInfo convert(const ActionCapability& cap);

    /**
     * Convert robot_agent::LifecycleState to interfaces::LifecycleState.
     */
    static interfaces::LifecycleState convert_lifecycle_state(robot_agent::LifecycleState state);
};

}  // namespace robot_agent::capability
