// Copyright 2026 Multi-Robot Supervision System
// Dynamic type loader implementation

#include "robot_agent/capability/type_loader.hpp"
#include "robot_agent/core/logger.hpp"

#include <dlfcn.h>
#include <sstream>
#include <filesystem>

#include <ament_index_cpp/get_package_share_directory.hpp>
#include <ament_index_cpp/get_package_prefix.hpp>

namespace robot_agent {
namespace capability {

namespace {
logging::ComponentLogger log("TypeLoader");
}

TypeSupportLoader::TypeSupportLoader() = default;

TypeSupportLoader::~TypeSupportLoader() {
    clear_cache();
}

std::pair<std::string, std::string> TypeSupportLoader::parse_action_type(
    const std::string& action_type) {

    // Handle different formats:
    // "nav2_msgs/action/NavigateToPose"
    // "nav2_msgs/NavigateToPose"
    // "nav2_msgs::action::NavigateToPose"

    std::string package;
    std::string action_name;

    // Check for C++ style separator
    std::string type = action_type;
    size_t pos = type.find("::");
    while (pos != std::string::npos) {
        type.replace(pos, 2, "/");
        pos = type.find("::");
    }

    // Split by '/'
    std::vector<std::string> parts;
    std::stringstream ss(type);
    std::string part;
    while (std::getline(ss, part, '/')) {
        if (!part.empty()) {
            parts.push_back(part);
        }
    }

    if (parts.size() >= 2) {
        package = parts[0];
        // Last part is the action name
        action_name = parts.back();
    }

    return {package, action_name};
}

std::string TypeSupportLoader::resolve_library_path(const std::string& package) {
    try {
        // Get package prefix (e.g., /opt/ros/humble)
        std::string prefix = ament_index_cpp::get_package_prefix(package);

        // Construct library path
        std::filesystem::path lib_path = prefix;
        lib_path /= "lib";
        lib_path /= "lib" + package + "__rosidl_typesupport_introspection_cpp.so";

        if (std::filesystem::exists(lib_path)) {
            return lib_path.string();
        }

        // Try alternative naming
        lib_path = prefix;
        lib_path /= "lib";
        lib_path /= "lib" + package + "__rosidl_typesupport_cpp.so";

        if (std::filesystem::exists(lib_path)) {
            return lib_path.string();
        }

        log.warn("Library not found for package: {}", package);
        return "";

    } catch (const std::exception& e) {
        log.error("Failed to resolve library path for {}: {}", package, e.what());
        return "";
    }
}

std::string TypeSupportLoader::resolve_action_library_path(const std::string& package) {
    try {
        std::string prefix = ament_index_cpp::get_package_prefix(package);

        std::filesystem::path lib_path = prefix;
        lib_path /= "lib";
        lib_path /= "lib" + package + "__rosidl_typesupport_cpp.so";

        if (std::filesystem::exists(lib_path)) {
            return lib_path.string();
        }

        log.warn("Action type support library not found for package: {}", package);
        return "";
    } catch (const std::exception& e) {
        log.error("Failed to resolve action library path for {}: {}", package, e.what());
        return "";
    }
}

void* TypeSupportLoader::get_or_load_library(const std::string& package) {
    // Check cache first
    auto it = lib_cache_.find(package);
    if (it != lib_cache_.end()) {
        return it->second;
    }

    // Resolve library path
    std::string lib_path = resolve_library_path(package);
    if (lib_path.empty()) {
        return nullptr;
    }

    // Load library
    void* handle = dlopen(lib_path.c_str(), RTLD_LAZY);
    if (!handle) {
        log.error("dlopen failed for {}: {}", lib_path, dlerror());
        return nullptr;
    }

    log.debug("Loaded library: {}", lib_path);
    lib_cache_[package] = handle;
    return handle;
}

void* TypeSupportLoader::get_or_load_action_library(const std::string& package) {
    auto it = action_lib_cache_.find(package);
    if (it != action_lib_cache_.end()) {
        return it->second;
    }

    std::string lib_path = resolve_action_library_path(package);
    if (lib_path.empty()) {
        return nullptr;
    }

    void* handle = dlopen(lib_path.c_str(), RTLD_LAZY);
    if (!handle) {
        log.error("dlopen failed for {}: {}", lib_path, dlerror());
        return nullptr;
    }

    log.debug("Loaded action type support library: {}", lib_path);
    action_lib_cache_[package] = handle;
    return handle;
}

const rosidl_message_type_support_t* TypeSupportLoader::get_type_support(
    void* lib_handle,
    const std::string& package,
    const std::string& action_name,
    const std::string& suffix) {

    // Construct symbol name
    // Example: rosidl_typesupport_introspection_cpp__get_message_type_support_handle__nav2_msgs__action__NavigateToPose_Goal
    std::string symbol_name =
        "rosidl_typesupport_introspection_cpp__get_message_type_support_handle__" +
        package + "__action__" + action_name + suffix;

    // Try to find symbol
    dlerror();  // Clear any existing error
    void* symbol = dlsym(lib_handle, symbol_name.c_str());
    const char* error = dlerror();

    if (error) {
        log.debug("Symbol not found: {} ({})", symbol_name, error);

        // Try alternative symbol naming (without 'action' in path)
        symbol_name =
            "rosidl_typesupport_introspection_cpp__get_message_type_support_handle__" +
            package + "__" + action_name + suffix;

        dlerror();
        symbol = dlsym(lib_handle, symbol_name.c_str());
        error = dlerror();

        if (error) {
            log.warn("Alternative symbol also not found: {}", symbol_name);
            return nullptr;
        }
    }

    // Call the getter function
    using TypeSupportGetter = const rosidl_message_type_support_t* (*)();
    auto getter = reinterpret_cast<TypeSupportGetter>(symbol);
    return getter();
}

const rosidl_action_type_support_t* TypeSupportLoader::get_action_type_support(
    void* lib_handle,
    const std::string& package,
    const std::string& action_name) {

    // Example: rosidl_typesupport_cpp__get_action_type_support_handle__nav2_msgs__action__NavigateToPose
    std::string symbol_name =
        "rosidl_typesupport_cpp__get_action_type_support_handle__" +
        package + "__action__" + action_name;

    dlerror();
    void* symbol = dlsym(lib_handle, symbol_name.c_str());
    const char* error = dlerror();

    if (error) {
        log.debug("Action type support symbol not found: {} ({})", symbol_name, error);

        // Try alternative symbol naming (without 'action' in path)
        symbol_name =
            "rosidl_typesupport_cpp__get_action_type_support_handle__" +
            package + "__" + action_name;

        dlerror();
        symbol = dlsym(lib_handle, symbol_name.c_str());
        error = dlerror();

        if (error) {
            log.warn("Alternative action symbol also not found: {}", symbol_name);
            return nullptr;
        }
    }

    using ActionTypeSupportGetter = const rosidl_action_type_support_t* (*)();
    auto getter = reinterpret_cast<ActionTypeSupportGetter>(symbol);
    return getter();
}

std::optional<TypeSupportLoader::ActionTypeInfo> TypeSupportLoader::load(
    const std::string& action_type) {

    std::lock_guard<std::mutex> lock(cache_mutex_);

    // Check type cache first
    auto cache_it = type_cache_.find(action_type);
    if (cache_it != type_cache_.end()) {
        return cache_it->second;
    }

    // Parse action type
    auto [package, action_name] = parse_action_type(action_type);
    if (package.empty() || action_name.empty()) {
        log.error("Invalid action type format: {}", action_type);
        return std::nullopt;
    }

    // Load library (introspection for schemas)
    void* lib_handle = get_or_load_library(package);
    if (!lib_handle) {
        return std::nullopt;
    }

    void* action_lib_handle = get_or_load_action_library(package);

    // Get type supports
    ActionTypeInfo info;
    info.package = package;
    info.action_name = action_name;
    info.library_handle = lib_handle;
    info.action_library_handle = action_lib_handle;

    info.goal_ts = get_type_support(lib_handle, package, action_name, "_Goal");
    info.result_ts = get_type_support(lib_handle, package, action_name, "_Result");
    info.feedback_ts = get_type_support(lib_handle, package, action_name, "_Feedback");
    if (action_lib_handle) {
        info.action_ts = get_action_type_support(action_lib_handle, package, action_name);
    }

    if (!info.goal_ts && !info.result_ts && !info.feedback_ts) {
        log.warn("No type supports found for: {}", action_type);
        return std::nullopt;
    }

    info.valid = true;

    // Cache and return
    type_cache_[action_type] = info;
    log.info("Loaded type support for: {} (Goal={}, Result={}, Feedback={}, Action={})",
             action_type,
             info.goal_ts != nullptr,
             info.result_ts != nullptr,
             info.feedback_ts != nullptr,
             info.action_ts != nullptr);

    return info;
}

bool TypeSupportLoader::is_loaded(const std::string& action_type) const {
    std::lock_guard<std::mutex> lock(cache_mutex_);
    return type_cache_.find(action_type) != type_cache_.end();
}

std::optional<TypeSupportLoader::ActionTypeInfo> TypeSupportLoader::get_cached(
    const std::string& action_type) const {

    std::lock_guard<std::mutex> lock(cache_mutex_);
    auto it = type_cache_.find(action_type);
    if (it != type_cache_.end()) {
        return it->second;
    }
    return std::nullopt;
}

void TypeSupportLoader::clear_cache() {
    std::lock_guard<std::mutex> lock(cache_mutex_);

    // Close libraries
    for (auto& [package, handle] : lib_cache_) {
        if (handle) {
            dlclose(handle);
        }
    }

    for (auto& [package, handle] : action_lib_cache_) {
        if (handle) {
            dlclose(handle);
        }
    }

    lib_cache_.clear();
    action_lib_cache_.clear();
    type_cache_.clear();
}

}  // namespace capability
}  // namespace robot_agent
