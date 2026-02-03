// Copyright 2026 Multi-Robot Supervision System
// Capability Scanner Adapter Implementation

#include "robot_agent/capability/capability_scanner_adapter.hpp"
#include "robot_agent/core/logger.hpp"

namespace robot_agent::capability {

CapabilityScannerAdapter::CapabilityScannerAdapter(
    std::shared_ptr<CapabilityScanner> scanner,
    CapabilityStore& store)
    : scanner_(std::move(scanner))
    , store_(store)
{
}

void CapabilityScannerAdapter::scan() {
    if (!scanner_) {
        LOG_ERROR("CapabilityScannerAdapter: No scanner provided");
        return;
    }

    int count = scanner_->scan_all();
    LOG_INFO("CapabilityScannerAdapter: Scanned {} capabilities", count);

    // Notify change callback
    std::lock_guard<std::mutex> lock(callback_mutex_);
    if (change_callback_) {
        change_callback_(get_capabilities());
    }
}

void CapabilityScannerAdapter::refresh() {
    if (!scanner_) {
        LOG_ERROR("CapabilityScannerAdapter: No scanner provided");
        return;
    }

    int changes = scanner_->refresh();
    if (changes > 0) {
        LOG_DEBUG("CapabilityScannerAdapter: Refreshed {} capability changes", changes);

        // Notify change callback
        std::lock_guard<std::mutex> lock(callback_mutex_);
        if (change_callback_) {
            change_callback_(get_capabilities());
        }
    }
}

std::vector<interfaces::CapabilityInfo> CapabilityScannerAdapter::get_capabilities() const {
    std::vector<interfaces::CapabilityInfo> result;

    // Get all capabilities from scanner
    auto caps = scanner_->get_all();
    result.reserve(caps.size());

    for (const auto& cap : caps) {
        result.push_back(convert(cap));
    }

    return result;
}

std::optional<interfaces::CapabilityInfo> CapabilityScannerAdapter::get_capability(
    const std::string& action_type) const
{
    auto cap = scanner_->get(action_type);
    if (cap) {
        return convert(*cap);
    }
    return std::nullopt;
}

std::optional<interfaces::CapabilityInfo> CapabilityScannerAdapter::get_capability_by_server(
    const std::string& server_name) const
{
    auto cap = scanner_->get_server(server_name);
    if (cap) {
        // Look up the capability by server name from the store
        CapabilityStore::const_accessor accessor;
        if (store_.find(accessor, server_name)) {
            return convert(accessor->second);
        }
    }
    return std::nullopt;
}

interfaces::LifecycleState CapabilityScannerAdapter::get_lifecycle_state(
    const std::string& node_name) const
{
    if (!scanner_) {
        return interfaces::LifecycleState::UNKNOWN;
    }

    // Query lifecycle state from scanner
    auto state = scanner_->query_lifecycle_state(node_name);
    return convert_lifecycle_state(state);
}

bool CapabilityScannerAdapter::is_lifecycle_node(const std::string& node_name) const {
    if (!scanner_) {
        return false;
    }
    return scanner_->is_lifecycle_node(node_name);
}

void CapabilityScannerAdapter::set_change_callback(CapabilityChangeCallback cb) {
    std::lock_guard<std::mutex> lock(callback_mutex_);
    change_callback_ = std::move(cb);
}

void CapabilityScannerAdapter::update_lifecycle_states() {
    if (scanner_) {
        scanner_->update_lifecycle_states();

        // Notify change callback after update
        std::lock_guard<std::mutex> lock(callback_mutex_);
        if (change_callback_) {
            change_callback_(get_capabilities());
        }
    }
}

interfaces::CapabilityInfo CapabilityScannerAdapter::convert(const ActionCapability& cap) {
    interfaces::CapabilityInfo info;

    info.action_type = cap.action_type;
    info.action_server = cap.action_server;
    info.package = cap.package;
    info.action_name = cap.action_name;
    info.node_name = cap.node_name;

    info.is_available = cap.available.load();
    info.lifecycle_state = convert_lifecycle_state(cap.lifecycle_state.load());

    info.goal_schema = cap.goal_schema_json;
    info.result_schema = cap.result_schema_json;
    info.feedback_schema = cap.feedback_schema_json;

    // Convert success criteria
    if (!cap.success_criteria.is_empty()) {
        interfaces::CapabilityInfo::SuccessCriteria criteria;
        criteria.field = cap.success_criteria.field;
        criteria.op = cap.success_criteria.op;
        criteria.value = cap.success_criteria.value;
        info.success_criteria = criteria;
    }

    return info;
}

interfaces::LifecycleState CapabilityScannerAdapter::convert_lifecycle_state(
    robot_agent::LifecycleState state)
{
    // Both enums have the same values, so direct cast is safe
    switch (state) {
        case robot_agent::LifecycleState::UNKNOWN:
            return interfaces::LifecycleState::UNKNOWN;
        case robot_agent::LifecycleState::UNCONFIGURED:
            return interfaces::LifecycleState::UNCONFIGURED;
        case robot_agent::LifecycleState::INACTIVE:
            return interfaces::LifecycleState::INACTIVE;
        case robot_agent::LifecycleState::ACTIVE:
            return interfaces::LifecycleState::ACTIVE;
        case robot_agent::LifecycleState::FINALIZED:
            return interfaces::LifecycleState::FINALIZED;
        default:
            return interfaces::LifecycleState::UNKNOWN;
    }
}

}  // namespace robot_agent::capability
