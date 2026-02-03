// Copyright 2026 Multi-Robot Supervision System
// Capability Scanner implementation

#include "robot_agent/capability/scanner.hpp"
#include "robot_agent/core/logger.hpp"

#include <rcl_action/action_client.h>
#include <rcl_action/default_qos.h>
#include <rcl_action/graph.h>
#include <rcl/error_handling.h>
#include <rcl/graph.h>

#include <algorithm>
#include <chrono>
#include <future>
#include <map>
#include <unordered_set>

namespace robot_agent {
namespace capability {

namespace {
logging::ComponentLogger log("CapabilityScanner");

constexpr auto kMissingGrace = std::chrono::seconds(2);
constexpr auto kStatusTimeout = std::chrono::seconds(3);


/**
 * Check if a specific action server is responsive.
 *
 * Uses multiple signals to determine if an action server is truly alive:
 * 1. Publisher count for status topic (most reliable for death detection)
 * 2. rcl_action_server_is_available (graph cache - can be stale)
 * 3. Service names in graph
 *
 * IMPORTANT: Publisher count is checked FIRST because when a server dies,
 * publishers disappear quickly while the graph cache can remain stale.
 */
bool check_action_server_alive(
    rclcpp::Node::SharedPtr node,
    TypeSupportLoader* type_loader,
    const std::unordered_set<std::string>& service_names,
    const std::string& action_name,
    const std::string& action_type) {

    // FIRST: Check publisher counts - most reliable indicator when server dies
    std::string status_topic = action_name + "/_action/status";
    std::string feedback_topic = action_name + "/_action/feedback";

    size_t status_pubs = node->count_publishers(status_topic);
    size_t feedback_pubs = node->count_publishers(feedback_topic);

    // If no publishers for status topic, server is definitely dead
    // This catches cases where rcl_action_server_is_available returns stale data
    if (status_pubs == 0) {
        log.info("Action server {} DEAD: no status publishers (type: {})",
                 action_name, action_type);
        return false;
    }

    // SECOND: Check service names in graph
    std::string send_goal_service = action_name + "/_action/send_goal";
    std::string get_result_service = action_name + "/_action/get_result";
    std::string cancel_service = action_name + "/_action/cancel_goal";

    bool send_goal_exists = service_names.find(send_goal_service) != service_names.end();
    bool get_result_exists = service_names.find(get_result_service) != service_names.end();
    bool cancel_exists = service_names.find(cancel_service) != service_names.end();
    bool has_services = send_goal_exists && get_result_exists && cancel_exists;

    // THIRD: Check rcl_action availability (may be stale but useful as secondary)
    bool rcl_available = false;
    if (type_loader) {
        auto type_info = type_loader->load(action_type);
        if (type_info && type_info->action_ts) {
            rcl_action_client_t client = rcl_action_get_zero_initialized_client();
            auto options = rcl_action_client_get_default_options();
            auto node_base = node->get_node_base_interface();
            rcl_node_t* rcl_node = node_base->get_rcl_node_handle();

            rcl_ret_t init_ret = rcl_action_client_init(
                &client, rcl_node, type_info->action_ts, action_name.c_str(), &options);
            if (init_ret == RCL_RET_OK) {
                rcl_ret_t avail_ret = rcl_action_server_is_available(rcl_node, &client, &rcl_available);
                if (avail_ret != RCL_RET_OK) {
                    rcl_reset_error();
                    rcl_available = false;
                }
                rcl_action_client_fini(&client, rcl_node);
            } else {
                rcl_reset_error();
            }
        }
    }

    // Combined decision: need status publishers AND (services OR rcl_available)
    bool is_alive = (status_pubs > 0) && (has_services || rcl_available);

    log.debug(
        "Action server {} availability: status_pubs={}, feedback_pubs={}, "
        "services={}, rcl_avail={} -> {} (type: {})",
        action_name,
        status_pubs,
        feedback_pubs,
        has_services,
        rcl_available,
        is_alive ? "ALIVE" : "DEAD",
        action_type);

    return is_alive;
}

/**
 * Discover action servers using the rcl_action graph API.
 *
 * This is the same method used by `ros2 action list` and properly
 * discovers all action servers including those with hidden services.
 *
 * Returns map of action_server_name -> vector of action_types
 */
std::map<std::string, std::vector<std::string>> discover_action_servers(
    rclcpp::Node::SharedPtr node,
    TypeSupportLoader* type_loader) {

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

    // Snapshot service names once per discovery to avoid repeated graph queries.
    std::unordered_set<std::string> service_names;
    try {
        auto services = node->get_service_names_and_types();
        service_names.reserve(services.size());
        for (const auto& entry : services) {
            service_names.insert(entry.first);
        }
    } catch (const std::exception& e) {
        log.warn("Failed to get service names: {}", e.what());
    }

    // Convert rcl_names_and_types_t to our map format
    // Also verify each action server is actually responsive
    for (size_t i = 0; i < action_names_and_types.names.size; ++i) {
        std::string action_name = action_names_and_types.names.data[i];
        rcl_names_and_types_t * types = &action_names_and_types;

        if (types->types[i].size == 0) {
            continue;
        }

        // Verify the action server is actually alive (not just cached in graph)
        std::string action_type = types->types[i].data[0];
        bool is_alive = check_action_server_alive(
            node, type_loader, service_names, action_name, action_type);
        if (!is_alive) {
            log.debug("Action server {} appears in graph but is not responsive, skipping",
                      action_name);
            continue;
        }

        for (size_t j = 0; j < types->types[i].size; ++j) {
            std::string type = types->types[i].data[j];
            result[action_name].push_back(type);
            log.debug("Found responsive action server: {} (type: {})",
                      action_name, type);
        }
    }

    // Clean up
    rcl_ret_t fini_ret = rcl_names_and_types_fini(&action_names_and_types);
    if (fini_ret != RCL_RET_OK) {
        log.warn("Failed to finalize names_and_types: {}", rcl_get_error_string().str);
        rcl_reset_error();
    }

    CLOG_INFO_THROTTLE(log, 30.0, "Action graph discovery found {} responsive action servers", result.size());
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

    auto action_servers = discover_action_servers(node_, &type_loader_);
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
    auto action_servers = discover_action_servers(node_, &type_loader_);

    for (const auto& [server_name, types] : action_servers) {
        (void)types;
        ensure_status_subscription(server_name);
    }
    for (auto it = store_.begin(); it != store_.end(); ++it) {
        ensure_status_subscription(it->first);
    }

    {
        std::lock_guard<std::mutex> lock(status_mutex_);
        for (const auto& [server_name, sub] : status_subscriptions_) {
            if (!sub) {
                status_publishers_alive_[server_name] = false;
                continue;
            }
            status_publishers_alive_[server_name] = sub->get_publisher_count() > 0;
        }
    }

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
    // Use topic-based discovery (ROS2 Humble compatible)
    auto action_servers = discover_action_servers(node_, &type_loader_);
    int changes = 0;

    std::unordered_set<std::string> service_names;
    try {
        auto services = node_->get_service_names_and_types();
        service_names.reserve(services.size());
        for (const auto& entry : services) {
            service_names.insert(entry.first);
        }
    } catch (const std::exception& e) {
        log.warn("Failed to get service names: {}", e.what());
    }

    auto has_action_services = [&service_names](const std::string& server_name) -> bool {
        return service_names.find(server_name + "/_action/send_goal") != service_names.end()
            && service_names.find(server_name + "/_action/get_result") != service_names.end()
            && service_names.find(server_name + "/_action/cancel_goal") != service_names.end();
    };

    for (const auto& [server_name, types] : action_servers) {
        (void)types;
        ensure_status_subscription(server_name);
    }
    for (auto it = store_.begin(); it != store_.end(); ++it) {
        ensure_status_subscription(it->first);
    }

    // NOTE: We do NOT update status_publishers_alive_ from get_publisher_count() here
    // because it returns stale cached graph data. Instead, we rely on:
    // 1. Liveliness callback (immediate notification when publisher dies)
    // 2. Status message reception callback (sets alive when message received)
    // 3. Status message timeout (marks unavailable if no messages for kStatusTimeout)

    // Log current state for debugging (info level for visibility)
    size_t filtered_count = 0;
    for (const auto& [name, types] : action_servers) {
        if (is_in_namespace(name)) {
            filtered_count++;
        }
    }
    CLOG_INFO_THROTTLE(log, 30.0, "Refresh: ROS2 graph has {} action servers ({} in namespace '{}')",
                       action_servers.size(), filtered_count, namespace_filter_);

    const auto now = std::chrono::steady_clock::now();

    // Track which capabilities are still present
    std::unordered_set<std::string> current_actions;

    log.debug("REFRESH: Checking {} action servers against {} stored capabilities",
              action_servers.size(), store_.size());

    for (const auto& [server_name, types] : action_servers) {
        if (!is_in_namespace(server_name)) {
            continue;
        }

        for (const auto& action_type : types) {
            // Use server_name as key (consistent with scan_action_server)
            current_actions.insert(server_name);

            // Check if this is a new capability
            bool in_store = false;
            {
                CapabilityStore::const_accessor acc;
                in_store = store_.find(acc, server_name);
                // acc released when scope ends
            }
            log.debug("  Server {} in_store={}", server_name, in_store);
            if (!in_store) {
                // New capability discovered
                if (scan_action_server(server_name, action_type)) {
                    changes++;
                    log.info("New capability discovered: {} at {}",
                             normalize_action_type(action_type), server_name);
                }
            } else {
                // Server is in current action graph - discover_action_servers()
                // already confirmed it's alive via check_action_server_alive()
                // (status_pubs > 0, services exist, rcl_available).
                // Trust discovery result and mark available.
                CapabilityStore::accessor write_acc;
                if (store_.find(write_acc, server_name)) {
                    write_acc->second.last_seen = now;

                    // Reset status timeout so downstream checks stay consistent
                    {
                        std::lock_guard<std::mutex> lock(status_mutex_);
                        status_last_seen_[server_name] = now;
                    }

                    if (!write_acc->second.available.load()) {
                        write_acc->second.available.store(true);
                        changes++;
                        log.info("Capability available (verified by discovery): {}", server_name);
                    }
                }
            }
        }
    }

    // Mark removed capabilities as unavailable
    std::vector<std::string> to_remove;

    for (auto it = store_.begin(); it != store_.end(); ++it) {
        if (current_actions.find(it->first) == current_actions.end()) {
            auto status_alive = get_status_publisher_alive(it->first);
            bool has_services = has_action_services(it->first);
            bool is_executing = it->second.executing.load();

            // Calculate status timeout
            bool status_timed_out = false;
            auto last_status = get_last_status_seen(it->first);
            if (!last_status.has_value() ||
                (now - last_status.value()) > kStatusTimeout) {
                status_timed_out = true;
            }

            // Keep if: executing OR (has services AND not timed out) OR (liveliness alive AND not timed out)
            bool should_keep = false;
            if (is_executing) {
                should_keep = true;
            } else if (has_services && !status_timed_out) {
                should_keep = true;
            } else if (status_alive.value_or(false) && !status_timed_out) {
                should_keep = true;
            }

            if (should_keep) {
                continue;
            }
            auto missing_for = now - it->second.last_seen;
            if (missing_for < kMissingGrace) {
                continue;
            }
            // Only add to remove list if it was available
            if (it->second.available.load()) {
                to_remove.push_back(it->first);
            }
        }
    }

    if (!to_remove.empty()) {
        log.info("Detected {} unavailable action servers", to_remove.size());
    }

    for (const auto& key : to_remove) {
        CapabilityStore::accessor acc;
        if (store_.find(acc, key)) {
            acc->second.available.store(false);
            log.info("Capability marked unavailable (missing from action graph): {}", key);
            changes++;
        }
    }

    // Final availability check for all capabilities using non-destructive signals
    for (auto it = store_.begin(); it != store_.end(); ++it) {
        auto status_alive = get_status_publisher_alive(it->first);

        CapabilityStore::accessor acc;
        if (!store_.find(acc, it->first)) {
            continue;
        }

        bool has_services = has_action_services(it->first);
        bool is_executing = acc->second.executing.load();

        // Calculate status timeout
        bool status_timed_out = false;
        auto last_status = get_last_status_seen(it->first);
        if (!last_status.has_value() ||
            (now - last_status.value()) > kStatusTimeout) {
            status_timed_out = true;
        }

        // Alive if: executing OR (has services AND not timed out) OR (liveliness alive AND not timed out)
        bool alive = false;
        if (is_executing) {
            alive = true;
        } else if (has_services && !status_timed_out) {
            alive = true;
        } else if (status_alive.value_or(false) && !status_timed_out) {
            alive = true;
        }

        if (!alive && acc->second.available.load()) {
            acc->second.available.store(false);
            log.info("Capability marked unavailable (has_services={}, timed_out={}): {}",
                     has_services, status_timed_out, it->first);
            changes++;
        } else if (alive && !acc->second.available.load()) {
            acc->second.available.store(true);
            log.info("Capability available again (has_services={}): {}",
                     has_services, it->first);
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

void CapabilityScanner::ensure_status_subscription(const std::string& server_name) {
    std::lock_guard<std::mutex> lock(status_mutex_);
    if (status_subscriptions_.find(server_name) != status_subscriptions_.end()) {
        return;
    }

    std::string topic = server_name + "/_action/status";
    auto qos = rclcpp::QoS(
        rclcpp::QoSInitialization::from_rmw(rcl_action_qos_profile_status_default),
        rcl_action_qos_profile_status_default);

    rclcpp::SubscriptionOptions options;
    options.use_intra_process_comm = rclcpp::IntraProcessSetting::Disable;
    options.event_callbacks.liveliness_callback =
        [this, server_name](rclcpp::QOSLivelinessChangedInfo & info) {
            std::lock_guard<std::mutex> lock(status_mutex_);
            bool alive = info.alive_count > 0;
            status_publishers_alive_[server_name] = alive;
            log.info("Action server {} liveliness: alive_count={}, not_alive_count={}",
                     server_name, info.alive_count, info.not_alive_count);
        };
    options.event_callbacks.incompatible_qos_callback =
        [server_name](rclcpp::QOSRequestedIncompatibleQoSInfo & info) {
            log.warn("Action server {} QoS incompatible: total_count={} (last_policy_id={})",
                     server_name, info.total_count, info.last_policy_kind);
        };

    auto sub = node_->create_subscription<action_msgs::msg::GoalStatusArray>(
        topic, qos,
        [this, server_name](action_msgs::msg::GoalStatusArray::SharedPtr) {
            std::lock_guard<std::mutex> lock(status_mutex_);
            status_last_seen_[server_name] = std::chrono::steady_clock::now();
            status_publishers_alive_[server_name] = true;
        },
        options);

    status_subscriptions_[server_name] = sub;
    log.info("Subscribed to action status: {}", topic);
}

std::optional<std::chrono::steady_clock::time_point> CapabilityScanner::get_last_status_seen(
    const std::string& server_name) const {
    std::lock_guard<std::mutex> lock(status_mutex_);
    auto it = status_last_seen_.find(server_name);
    if (it == status_last_seen_.end()) {
        return std::nullopt;
    }
    return it->second;
}

std::optional<bool> CapabilityScanner::get_status_publisher_alive(
    const std::string& server_name) const {
    std::lock_guard<std::mutex> lock(status_mutex_);
    auto it = status_publishers_alive_.find(server_name);
    if (it == status_publishers_alive_.end()) {
        return std::nullopt;
    }
    return it->second;
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

// ============================================================
// Lifecycle State Management
// ============================================================

LifecycleState CapabilityScanner::query_lifecycle_state(const std::string& node_name) {
    // Check cache first
    {
        std::lock_guard<std::mutex> lock(lifecycle_mutex_);

        // If we already know this node is NOT a lifecycle node, skip
        auto is_lifecycle_it = node_is_lifecycle_.find(node_name);
        if (is_lifecycle_it != node_is_lifecycle_.end() && !is_lifecycle_it->second) {
            return LifecycleState::UNKNOWN;
        }
    }

    // Construct the /get_state service name
    std::string service_name = node_name + "/get_state";

    // Get or create client
    rclcpp::Client<lifecycle_msgs::srv::GetState>::SharedPtr client;
    {
        std::lock_guard<std::mutex> lock(lifecycle_mutex_);

        auto it = lifecycle_clients_.find(node_name);
        if (it == lifecycle_clients_.end()) {
            // Create new client
            client = node_->create_client<lifecycle_msgs::srv::GetState>(service_name);
            lifecycle_clients_[node_name] = client;
        } else {
            client = it->second;
        }
    }

    // Wait for service (short timeout)
    if (!client->wait_for_service(std::chrono::milliseconds(100))) {
        // Service not available - not a lifecycle node or node not responsive
        std::lock_guard<std::mutex> lock(lifecycle_mutex_);
        node_is_lifecycle_[node_name] = false;
        node_lifecycle_state_[node_name] = LifecycleState::UNKNOWN;
        log.debug("Node {} does not have /get_state service (not a lifecycle node)", node_name);
        return LifecycleState::UNKNOWN;
    }

    // Mark as lifecycle node
    {
        std::lock_guard<std::mutex> lock(lifecycle_mutex_);
        node_is_lifecycle_[node_name] = true;
    }

    // Send request
    auto request = std::make_shared<lifecycle_msgs::srv::GetState::Request>();
    auto future = client->async_send_request(request);

    // Wait for response with timeout
    if (future.wait_for(std::chrono::milliseconds(500)) != std::future_status::ready) {
        log.warn("Lifecycle state query timed out for node: {}", node_name);
        return LifecycleState::UNKNOWN;
    }

    try {
        auto response = future.get();

        // Map ROS2 lifecycle state ID to our enum
        LifecycleState state = LifecycleState::UNKNOWN;
        switch (response->current_state.id) {
            case lifecycle_msgs::msg::State::PRIMARY_STATE_UNCONFIGURED:
                state = LifecycleState::UNCONFIGURED;
                break;
            case lifecycle_msgs::msg::State::PRIMARY_STATE_INACTIVE:
                state = LifecycleState::INACTIVE;
                break;
            case lifecycle_msgs::msg::State::PRIMARY_STATE_ACTIVE:
                state = LifecycleState::ACTIVE;
                break;
            case lifecycle_msgs::msg::State::PRIMARY_STATE_FINALIZED:
                state = LifecycleState::FINALIZED;
                break;
            default:
                log.debug("Unknown lifecycle state id {} for node {}", response->current_state.id, node_name);
                state = LifecycleState::UNKNOWN;
                break;
        }

        // Cache the state
        {
            std::lock_guard<std::mutex> lock(lifecycle_mutex_);
            node_lifecycle_state_[node_name] = state;
        }

        log.debug("Node {} lifecycle state: {} (id={})",
                  node_name, lifecycle_state_to_string(state), response->current_state.id);

        return state;

    } catch (const std::exception& e) {
        log.warn("Failed to get lifecycle state for {}: {}", node_name, e.what());
        return LifecycleState::UNKNOWN;
    }
}

bool CapabilityScanner::is_lifecycle_node(const std::string& node_name) {
    // Check cache first
    {
        std::lock_guard<std::mutex> lock(lifecycle_mutex_);
        auto it = node_is_lifecycle_.find(node_name);
        if (it != node_is_lifecycle_.end()) {
            return it->second;
        }
    }

    // Query state to determine if it's a lifecycle node
    // (this will populate the cache as a side effect)
    query_lifecycle_state(node_name);

    std::lock_guard<std::mutex> lock(lifecycle_mutex_);
    auto it = node_is_lifecycle_.find(node_name);
    return it != node_is_lifecycle_.end() && it->second;
}

void CapabilityScanner::update_lifecycle_states() {
    log.debug("Updating lifecycle states for {} capabilities", store_.size());

    // Collect all unique node names
    std::unordered_set<std::string> node_names;
    for (auto it = store_.begin(); it != store_.end(); ++it) {
        // Try to extract node name from action server path
        // Action server path format: /namespace/action_server_name
        // Node name is typically derived from the action server

        // For now, we assume node name is the action server without the action suffix
        // This is a heuristic - in practice, the node name should be discovered
        // or configured explicitly

        std::string server = it->second.action_server;
        if (!server.empty()) {
            // Common pattern: node name = action server path (they're often the same)
            // For lifecycle nodes, we need the node name, not the action server name
            // This might need adjustment based on actual naming conventions

            // Try direct query using action server path as node name
            // (works when node has same name as its action server)
            node_names.insert(server);

            // Also check the capability's node_name field if set
            if (!it->second.node_name.empty()) {
                node_names.insert(it->second.node_name);
            }
        }
    }

    // Query lifecycle state for each node
    for (const auto& node_name : node_names) {
        auto state = query_lifecycle_state(node_name);

        // Update capabilities that match this node
        for (auto it = store_.begin(); it != store_.end(); ++it) {
            if (it->second.action_server == node_name || it->second.node_name == node_name) {
                CapabilityStore::accessor acc;
                if (store_.find(acc, it->first)) {
                    LifecycleState prev_state = acc->second.lifecycle_state.load();
                    acc->second.lifecycle_state.store(state);

                    // Update availability based on lifecycle state
                    if (state == LifecycleState::ACTIVE) {
                        // Lifecycle node is active - mark as available
                        acc->second.available.store(true);
                        if (prev_state != LifecycleState::ACTIVE) {
                            log.info("Capability {} now ACTIVE (lifecycle node)", it->first);
                        }
                    } else if (state == LifecycleState::INACTIVE ||
                               state == LifecycleState::UNCONFIGURED ||
                               state == LifecycleState::FINALIZED) {
                        // Lifecycle node is not active - mark as unavailable
                        if (acc->second.available.load()) {
                            acc->second.available.store(false);
                            log.info("Capability {} marked unavailable (lifecycle state: {})",
                                     it->first, lifecycle_state_to_string(state));
                        }
                    }
                    // UNKNOWN state: don't change availability (fall back to existing detection)
                }
            }
        }
    }
}

}  // namespace capability
}  // namespace robot_agent
