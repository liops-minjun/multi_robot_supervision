// Copyright 2026 Multi-Robot Supervision System
// State Definition Types and Storage

#pragma once

#include <chrono>
#include <mutex>
#include <optional>
#include <string>
#include <unordered_map>
#include <vector>

#include <tbb/concurrent_hash_map.h>
#include <nlohmann/json.hpp>

namespace fleet_agent {
namespace state {

// ============================================================
// State Definition Types
// ============================================================

/**
 * ActionMapping - Maps action type to state during execution
 */
struct ActionMapping {
    std::string action_type;     // e.g., "nav2_msgs/NavigateToPose"
    std::string server;          // e.g., "/navigate_to_pose"
    std::string during_state;    // State while executing (deprecated)
    std::vector<std::string> during_states;  // Multiple states during execution
};

// JSON serialization for ActionMapping
inline void to_json(nlohmann::json& j, const ActionMapping& mapping) {
    j = nlohmann::json{
        {"action_type", mapping.action_type},
        {"server", mapping.server},
    };
    if (!mapping.during_state.empty()) {
        j["during_state"] = mapping.during_state;
    }
    if (!mapping.during_states.empty()) {
        j["during_states"] = mapping.during_states;
    }
}

inline void from_json(const nlohmann::json& j, ActionMapping& mapping) {
    j.at("action_type").get_to(mapping.action_type);
    if (j.contains("server")) {
        j.at("server").get_to(mapping.server);
    }
    if (j.contains("during_state")) {
        j.at("during_state").get_to(mapping.during_state);
    }
    if (j.contains("during_states")) {
        j.at("during_states").get_to(mapping.during_states);
    }
}

/**
 * StateDefinition - Defines valid states and transitions for a robot
 */
struct StateDefinition {
    std::string id;              // Unique identifier (e.g., "gocart250")
    std::string name;            // Display name
    std::string description;
    std::vector<std::string> states;        // Available states
    std::string default_state;              // Initial state
    std::vector<ActionMapping> action_mappings;
    std::vector<std::string> teachable_waypoints;
    int version{1};
    std::chrono::system_clock::time_point updated_at;

    // Check if state is valid
    bool is_valid_state(const std::string& state) const {
        return std::find(states.begin(), states.end(), state) != states.end();
    }

    // Get during state for action type
    std::optional<std::string> get_during_state(const std::string& action_type) const {
        for (const auto& mapping : action_mappings) {
            if (mapping.action_type == action_type) {
                if (!mapping.during_states.empty()) {
                    return mapping.during_states[0];
                }
                if (!mapping.during_state.empty()) {
                    return mapping.during_state;
                }
            }
        }
        return std::nullopt;
    }
};

// JSON serialization for StateDefinition
inline void to_json(nlohmann::json& j, const StateDefinition& sd) {
    j = nlohmann::json{
        {"id", sd.id},
        {"name", sd.name},
        {"description", sd.description},
        {"states", sd.states},
        {"default_state", sd.default_state},
        {"action_mappings", sd.action_mappings},
        {"teachable_waypoints", sd.teachable_waypoints},
        {"version", sd.version}
    };
}

inline void from_json(const nlohmann::json& j, StateDefinition& sd) {
    j.at("id").get_to(sd.id);
    j.at("name").get_to(sd.name);
    if (j.contains("description")) j.at("description").get_to(sd.description);
    j.at("states").get_to(sd.states);
    j.at("default_state").get_to(sd.default_state);
    if (j.contains("action_mappings")) {
        j.at("action_mappings").get_to(sd.action_mappings);
    }
    if (j.contains("teachable_waypoints")) {
        j.at("teachable_waypoints").get_to(sd.teachable_waypoints);
    }
    sd.version = j.value("version", 1);
}

// ============================================================
// State Definition Storage
// ============================================================

/**
 * StateDefinitionStorage - Thread-safe storage for state definitions
 *
 * Stores state definitions received from the central server.
 * Supports persistence to disk for offline operation.
 */
class StateDefinitionStorage {
public:
    /**
     * Constructor.
     *
     * @param storage_path Directory for persistent storage
     */
    explicit StateDefinitionStorage(const std::string& storage_path = "");

    ~StateDefinitionStorage() = default;

    // ============================================================
    // CRUD Operations
    // ============================================================

    /**
     * Store or update a state definition.
     *
     * @param def State definition to store
     * @return true if stored successfully
     */
    bool store(const StateDefinition& def);

    /**
     * Get state definition by ID.
     *
     * @param id State definition ID
     * @return StateDefinition if found
     */
    std::optional<StateDefinition> get(const std::string& id) const;

    /**
     * Get state definition for an agent.
     *
     * @param agent_id Agent identifier
     * @return StateDefinition if mapped
     */
    std::optional<StateDefinition> get_for_agent(const std::string& agent_id) const;

    /**
     * Map agent to state definition.
     *
     * @param agent_id Agent identifier
     * @param state_def_id State definition ID
     */
    void map_agent(const std::string& agent_id, const std::string& state_def_id);

    /**
     * Check if state definition exists.
     */
    bool exists(const std::string& id) const;

    /**
     * Get version of state definition.
     */
    std::optional<int> get_version(const std::string& id) const;

    /**
     * List all state definition IDs.
     */
    std::vector<std::string> list_ids() const;

    /**
     * Get all agent -> state definition mappings.
     */
    std::unordered_map<std::string, int> get_versions_map() const;

    // ============================================================
    // Persistence
    // ============================================================

    /**
     * Save all state definitions to disk.
     */
    bool save_to_disk();

    /**
     * Load state definitions from disk.
     */
    bool load_from_disk();

    /**
     * Clear all cached state definitions.
     */
    void clear();

private:
    std::string storage_path_;

    // State definitions: id -> definition
    tbb::concurrent_hash_map<std::string, StateDefinition> definitions_;

    // Agent mappings: agent_id -> state_def_id
    tbb::concurrent_hash_map<std::string, std::string> agent_mappings_;

    std::string get_file_path(const std::string& id) const;
    bool write_to_file(const StateDefinition& def);
    std::optional<StateDefinition> read_from_file(const std::string& id);
};

}  // namespace state
}  // namespace fleet_agent
