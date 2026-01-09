// Copyright 2026 Multi-Robot Supervision System
// Dynamic type loader for ROS2 action types

#pragma once

#include <memory>
#include <mutex>
#include <optional>
#include <string>
#include <unordered_map>

#include <rosidl_typesupport_introspection_cpp/message_introspection.hpp>

namespace fleet_agent {
namespace capability {

/**
 * TypeSupportLoader - Dynamically loads ROS2 action type support.
 *
 * Uses dlopen/dlsym to load ROS2 message type support libraries at runtime,
 * enabling introspection of action Goal/Result/Feedback types without
 * compile-time knowledge of the specific action types.
 *
 * Usage:
 *   TypeSupportLoader loader;
 *   auto info = loader.load("nav2_msgs/action/NavigateToPose");
 *   if (info) {
 *       // Use info->goal_ts for Goal message introspection
 *   }
 */
class TypeSupportLoader {
public:
    /**
     * Loaded action type information.
     */
    struct ActionTypeInfo {
        const rosidl_message_type_support_t* goal_ts{nullptr};
        const rosidl_message_type_support_t* result_ts{nullptr};
        const rosidl_message_type_support_t* feedback_ts{nullptr};
        void* library_handle{nullptr};
        std::string package;
        std::string action_name;
        bool valid{false};
    };

    TypeSupportLoader();
    ~TypeSupportLoader();

    // Delete copy operations
    TypeSupportLoader(const TypeSupportLoader&) = delete;
    TypeSupportLoader& operator=(const TypeSupportLoader&) = delete;

    /**
     * Load type support for an action type.
     *
     * @param action_type Full action type string, e.g., "nav2_msgs/action/NavigateToPose"
     * @return ActionTypeInfo with type support handles, or std::nullopt on failure
     *
     * Internal process:
     * 1. Parse package name ("nav2_msgs")
     * 2. Find library path via ament_index
     * 3. dlopen the library
     * 4. dlsym to get type support getter functions
     * 5. Call getter functions to obtain type support handles
     */
    std::optional<ActionTypeInfo> load(const std::string& action_type);

    /**
     * Check if an action type is already loaded.
     */
    bool is_loaded(const std::string& action_type) const;

    /**
     * Get cached type info if available.
     */
    std::optional<ActionTypeInfo> get_cached(const std::string& action_type) const;

    /**
     * Clear all cached libraries.
     */
    void clear_cache();

private:
    // Library cache: package name -> library handle
    std::unordered_map<std::string, void*> lib_cache_;

    // Type info cache: action_type -> ActionTypeInfo
    std::unordered_map<std::string, ActionTypeInfo> type_cache_;

    mutable std::mutex cache_mutex_;

    /**
     * Parse action type into package and action name.
     *
     * @param action_type e.g., "nav2_msgs/action/NavigateToPose"
     * @return pair of (package, action_name), e.g., ("nav2_msgs", "NavigateToPose")
     */
    std::pair<std::string, std::string> parse_action_type(const std::string& action_type);

    /**
     * Resolve library path for a package.
     *
     * Uses ament_index to find:
     *   /opt/ros/<distro>/lib/lib<pkg>__rosidl_typesupport_introspection_cpp.so
     */
    std::string resolve_library_path(const std::string& package);

    /**
     * Get or load library handle.
     */
    void* get_or_load_library(const std::string& package);

    /**
     * Get type support handle from library.
     *
     * @param lib_handle dlopen handle
     * @param package Package name
     * @param action_name Action name
     * @param suffix "_Goal", "_Result", or "_Feedback"
     */
    const rosidl_message_type_support_t* get_type_support(
        void* lib_handle,
        const std::string& package,
        const std::string& action_name,
        const std::string& suffix
    );
};

}  // namespace capability
}  // namespace fleet_agent
