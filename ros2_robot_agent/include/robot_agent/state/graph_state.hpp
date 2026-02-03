// Copyright 2026 Multi-Robot Supervision System
// Graph State Types and Fleet State Cache

#pragma once

#include <chrono>
#include <functional>
#include <mutex>
#include <optional>
#include <string>
#include <unordered_map>
#include <unordered_set>
#include <vector>

#include <tbb/concurrent_hash_map.h>
#include <nlohmann/json.hpp>

namespace robot_agent {
namespace state {

// ============================================================
// State Phase Enum
// ============================================================

enum class StatePhase {
    Unknown = 0,
    Idle = 1,
    Executing = 2,
    Success = 3,
    Failed = 4
};

inline std::string phase_to_string(StatePhase phase) {
    switch (phase) {
        case StatePhase::Idle: return "idle";
        case StatePhase::Executing: return "executing";
        case StatePhase::Success: return "success";
        case StatePhase::Failed: return "failed";
        default: return "unknown";
    }
}

inline StatePhase string_to_phase(const std::string& str) {
    if (str == "idle") return StatePhase::Idle;
    if (str == "executing") return StatePhase::Executing;
    if (str == "success") return StatePhase::Success;
    if (str == "failed") return StatePhase::Failed;
    return StatePhase::Unknown;
}

// ============================================================
// Graph State Definition
// ============================================================

/**
 * GraphState - State definition for behavior tree execution
 *
 * States are auto-generated from behavior tree steps with naming convention:
 * - {step_id}:executing - During step execution
 * - {step_id}:success   - Step completed successfully
 * - {step_id}:failed    - Step failed
 */
struct GraphState {
    std::string code;           // State code (e.g., "pick:executing")
    std::string name;           // Human-readable name
    std::string type;           // "system", "graph", or "custom"
    std::string step_id;        // Associated step ID (for graph states)
    StatePhase phase{StatePhase::Unknown};  // Phase
    std::string color;          // UI color
    std::string description;    // Description
    std::vector<std::string> semantic_tags;  // Semantic tags for cross-agent queries

    // Check if state has a specific semantic tag
    bool has_tag(const std::string& tag) const {
        return std::find(semantic_tags.begin(), semantic_tags.end(), tag) != semantic_tags.end();
    }
};

// JSON serialization for GraphState
inline void to_json(nlohmann::json& j, const GraphState& gs) {
    j = nlohmann::json{
        {"code", gs.code},
        {"name", gs.name},
        {"type", gs.type}
    };
    if (!gs.step_id.empty()) j["step_id"] = gs.step_id;
    if (gs.phase != StatePhase::Unknown) j["phase"] = phase_to_string(gs.phase);
    if (!gs.color.empty()) j["color"] = gs.color;
    if (!gs.description.empty()) j["description"] = gs.description;
    if (!gs.semantic_tags.empty()) j["semantic_tags"] = gs.semantic_tags;
}

inline void from_json(const nlohmann::json& j, GraphState& gs) {
    j.at("code").get_to(gs.code);
    j.at("name").get_to(gs.name);
    if (j.contains("type")) j.at("type").get_to(gs.type);
    if (j.contains("step_id")) j.at("step_id").get_to(gs.step_id);
    if (j.contains("phase")) {
        std::string phase_str;
        j.at("phase").get_to(phase_str);
        gs.phase = string_to_phase(phase_str);
    }
    if (j.contains("color")) j.at("color").get_to(gs.color);
    if (j.contains("description")) j.at("description").get_to(gs.description);
    if (j.contains("semantic_tags")) j.at("semantic_tags").get_to(gs.semantic_tags);
}

// ============================================================
// System States (Predefined)
// ============================================================

inline std::vector<GraphState> get_system_states() {
    return {
        {"idle", "Idle", "system", "", StatePhase::Idle, "#6B7280", "Agent is idle and ready", {"ready", "available"}},
        {"executing", "Executing", "system", "", StatePhase::Executing, "#3B82F6", "Agent is executing an action", {"busy", "working"}},
        {"error", "Error", "system", "", StatePhase::Failed, "#EF4444", "Agent encountered an error", {"error", "fault"}},
        {"offline", "Offline", "system", "", StatePhase::Idle, "#9CA3AF", "Agent is offline", {"unavailable"}}
    };
}

// ============================================================
// Fleet State Entry (Other Agent State)
// ============================================================

/**
 * FleetStateEntry - State of another agent in the fleet
 *
 * Used for cross-agent precondition checking.
 */
struct FleetStateEntry {
    std::string agent_id;
    std::string state_code;
    std::vector<std::string> semantic_tags;
    std::string current_behavior_tree_id;
    bool is_online{false};
    bool is_executing{false};
    std::chrono::system_clock::time_point updated_at;

    // Check freshness
    bool is_fresh(std::chrono::milliseconds max_age = std::chrono::milliseconds(5000)) const {
        auto now = std::chrono::system_clock::now();
        return (now - updated_at) < max_age;
    }
};

// JSON serialization for FleetStateEntry
inline void from_json(const nlohmann::json& j, FleetStateEntry& entry) {
    j.at("agent_id").get_to(entry.agent_id);
    if (j.contains("state_code")) j.at("state_code").get_to(entry.state_code);
    if (j.contains("semantic_tags")) j.at("semantic_tags").get_to(entry.semantic_tags);
    if (j.contains("current_behavior_tree_id")) j.at("current_behavior_tree_id").get_to(entry.current_behavior_tree_id);
    if (j.contains("is_online")) j.at("is_online").get_to(entry.is_online);
    if (j.contains("is_executing")) j.at("is_executing").get_to(entry.is_executing);
    entry.updated_at = std::chrono::system_clock::now();
}

// ============================================================
// Enhanced Precondition Types
// ============================================================

enum class PreconditionType {
    Unknown = 0,
    SelfState = 1,      // Check own agent's state
    AgentState = 2,     // Check specific agent's state
    SemanticTag = 3,    // Check agents with semantic tag
    AnyAgentState = 4   // Check any agent matching filter
};

enum class PreconditionOperator {
    Equals = 0,
    NotEquals = 1,
    In = 2,
    NotIn = 3,
    HasTag = 4,
    NotHasTag = 5
};

/**
 * PreconditionFilter - Filter for matching agents
 */
struct PreconditionFilter {
    std::string behavior_tree_id;   // Filter by behavior tree ID
    std::string capability;         // Filter by capability
    std::vector<std::string> tags;  // Filter by semantic tags
    bool online_only{false};        // Only check online agents
    bool executing_only{false};     // Only check executing agents
    bool include_self{false};       // Include self in query
};

/**
 * EnhancedPrecondition - Cross-agent state checking
 */
struct EnhancedPrecondition {
    std::string id;
    PreconditionType type{PreconditionType::Unknown};
    std::string target_agent_id;    // For AgentState type
    std::string expected_state;     // Expected state code
    std::vector<std::string> expected_states;  // For 'in' operator
    PreconditionOperator op{PreconditionOperator::Equals};
    PreconditionFilter filter;      // For SemanticTag and AnyAgentState
    std::string message;            // Error message if not satisfied
};

/**
 * PreconditionResult - Result of precondition evaluation
 */
struct PreconditionResult {
    bool satisfied{false};
    std::string reason;
    std::vector<std::string> matched_agents;
};

// ============================================================
// Fleet State Cache
// ============================================================

/**
 * FleetStateCache - Thread-safe cache for fleet state
 *
 * Stores state information about other agents for cross-agent
 * precondition evaluation. Updated via server broadcasts.
 */
class FleetStateCache {
public:
    using StateUpdateCallback = std::function<void(const std::string& agent_id, const FleetStateEntry& entry)>;

    FleetStateCache() = default;
    ~FleetStateCache() = default;

    // ============================================================
    // State Updates
    // ============================================================

    /**
     * Update state for an agent.
     *
     * @param entry Fleet state entry
     */
    void update(const FleetStateEntry& entry);

    /**
     * Update multiple agents from server broadcast.
     *
     * @param entries Fleet state entries
     */
    void update_batch(const std::vector<FleetStateEntry>& entries);

    /**
     * Mark agent as offline.
     *
     * @param agent_id Agent to mark offline
     */
    void mark_offline(const std::string& agent_id);

    /**
     * Remove agent from cache.
     *
     * @param agent_id Agent to remove
     */
    void remove(const std::string& agent_id);

    /**
     * Clear all cached states.
     */
    void clear();

    // ============================================================
    // State Queries
    // ============================================================

    /**
     * Get state for an agent.
     *
     * @param agent_id Agent ID
     * @return State entry if found
     */
    std::optional<FleetStateEntry> get(const std::string& agent_id) const;

    /**
     * Get agents with specific state code.
     *
     * @param state_code State code to match
     * @return List of agent IDs
     */
    std::vector<std::string> get_agents_by_state(const std::string& state_code) const;

    /**
     * Get agents with specific semantic tag.
     *
     * @param tag Semantic tag to match
     * @return List of agent IDs
     */
    std::vector<std::string> get_agents_by_tag(const std::string& tag) const;

    /**
     * Get all online agents.
     *
     * @return List of agent IDs
     */
    std::vector<std::string> get_online_agents() const;

    /**
     * Get all agents executing a specific behavior tree.
     *
     * @param behavior_tree_id Behavior tree ID to match
     * @return List of agent IDs
     */
    std::vector<std::string> get_agents_by_behavior_tree(const std::string& behavior_tree_id) const;

    /**
     * Get all cached entries.
     *
     * @return All fleet state entries
     */
    std::vector<FleetStateEntry> get_all() const;

    // ============================================================
    // Precondition Evaluation
    // ============================================================

    /**
     * Evaluate an enhanced precondition.
     *
     * @param self_agent_id ID of the executing agent (self)
     * @param self_state Current state of self
     * @param precondition Precondition to evaluate
     * @return Evaluation result
     */
    PreconditionResult evaluate(
        const std::string& self_agent_id,
        const std::string& self_state,
        const EnhancedPrecondition& precondition
    ) const;

    // ============================================================
    // Statistics
    // ============================================================

    /**
     * Get cache size.
     */
    size_t size() const;

    /**
     * Get timestamp of last update.
     */
    std::chrono::system_clock::time_point last_update_time() const;

    // ============================================================
    // Callbacks
    // ============================================================

    /**
     * Set callback for state updates.
     */
    void set_update_callback(StateUpdateCallback callback);

private:
    mutable std::mutex mutex_;
    std::unordered_map<std::string, FleetStateEntry> entries_;

    // Indexes for O(1) lookups
    std::unordered_map<std::string, std::unordered_set<std::string>> state_index_;  // state_code -> agent_ids
    std::unordered_map<std::string, std::unordered_set<std::string>> tag_index_;    // tag -> agent_ids
    std::unordered_map<std::string, std::unordered_set<std::string>> behavior_tree_index_;  // behavior_tree_id -> agent_ids

    std::chrono::system_clock::time_point last_update_;
    StateUpdateCallback update_callback_;

    // Index management
    void add_to_indexes(const FleetStateEntry& entry);
    void remove_from_indexes(const std::string& agent_id);
    void update_indexes(const FleetStateEntry& old_entry, const FleetStateEntry& new_entry);
};

// ============================================================
// State Manager (Enhanced)
// ============================================================

/**
 * EnhancedStateManager - Manages local state and fleet state cache
 *
 * Combines local StateTracker functionality with fleet state caching
 * for cross-agent precondition evaluation.
 */
class EnhancedStateManager {
public:
    /**
     * Constructor.
     *
     * @param agent_id This agent's ID
     */
    explicit EnhancedStateManager(const std::string& agent_id);

    // ============================================================
    // Local State Management
    // ============================================================

    /**
     * Get current state code.
     */
    std::string current_state_code() const;

    /**
     * Get current semantic tags.
     */
    std::vector<std::string> current_semantic_tags() const;

    /**
     * Get current behavior tree ID.
     */
    std::string current_behavior_tree_id() const;

    /**
     * Set current state.
     *
     * @param state_code State code
     * @param semantic_tags Semantic tags
     * @param behavior_tree_id Currently executing behavior tree ID
     */
    void set_state(
        const std::string& state_code,
        const std::vector<std::string>& semantic_tags = {},
        const std::string& behavior_tree_id = ""
    );

    /**
     * Transition to step state.
     *
     * @param step_id Step ID
     * @param phase Phase (executing, success, failed)
     * @param semantic_tags Additional semantic tags
     */
    void transition_to_step(
        const std::string& step_id,
        StatePhase phase,
        const std::vector<std::string>& semantic_tags = {}
    );

    /**
     * Return to idle state.
     */
    void reset_to_idle();

    // ============================================================
    // Graph State Management
    // ============================================================

    /**
     * Load graph states for behavior tree.
     *
     * @param behavior_tree_id Behavior tree ID
     * @param states Graph states
     */
    void load_graph_states(
        const std::string& behavior_tree_id,
        const std::vector<GraphState>& states
    );

    /**
     * Get graph state by code.
     *
     * @param code State code
     * @return GraphState if found
     */
    std::optional<GraphState> get_graph_state(const std::string& code) const;

    /**
     * Get states for step.
     *
     * @param step_id Step ID
     * @return States associated with the step
     */
    std::vector<GraphState> get_states_for_step(const std::string& step_id) const;

    // ============================================================
    // Fleet State Access
    // ============================================================

    /**
     * Get fleet state cache (read-only).
     */
    const FleetStateCache& fleet_cache() const { return fleet_cache_; }

    /**
     * Get mutable fleet state cache.
     */
    FleetStateCache& fleet_cache() { return fleet_cache_; }

    // ============================================================
    // Precondition Evaluation
    // ============================================================

    /**
     * Evaluate enhanced precondition.
     *
     * @param precondition Precondition to evaluate
     * @return Evaluation result
     */
    PreconditionResult evaluate_precondition(const EnhancedPrecondition& precondition) const;

    /**
     * Evaluate multiple preconditions with AND logic.
     *
     * @param preconditions Preconditions to evaluate
     * @return Combined result (all must pass)
     */
    PreconditionResult evaluate_preconditions(
        const std::vector<EnhancedPrecondition>& preconditions
    ) const;

    // ============================================================
    // State Export
    // ============================================================

    /**
     * Export current state as FleetStateEntry (for heartbeat).
     */
    FleetStateEntry to_fleet_entry() const;

private:
    std::string agent_id_;
    mutable std::mutex mutex_;

    // Current state
    std::string current_state_code_{"idle"};
    std::vector<std::string> current_semantic_tags_;
    std::string current_behavior_tree_id_;
    std::chrono::system_clock::time_point state_updated_at_;

    // Graph states cache
    std::unordered_map<std::string, GraphState> graph_states_;  // code -> state
    std::unordered_map<std::string, std::vector<std::string>> step_states_;  // step_id -> state codes

    // Fleet state cache
    FleetStateCache fleet_cache_;
};

}  // namespace state
}  // namespace robot_agent
