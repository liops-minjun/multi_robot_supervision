// Copyright 2026 Multi-Robot Supervision System
// State Tracker - Manages robot state transitions

#pragma once

#include "robot_agent/state/state_definition.hpp"
#include "robot_agent/core/types.hpp"

#include <atomic>
#include <chrono>
#include <deque>
#include <functional>
#include <mutex>
#include <optional>
#include <string>

namespace robot_agent {
namespace state {

/**
 * StateTransition - Records a state change
 */
struct StateTransition {
    std::string from_state;
    std::string to_state;
    std::string trigger;        // What caused the transition (action_type, manual, error)
    std::chrono::system_clock::time_point timestamp;
};

/**
 * StateTracker - Tracks and manages state transitions for a robot
 *
 * Features:
 * - Validates state transitions against StateDefinition
 * - Automatically transitions based on action execution
 * - Maintains state history for debugging
 * - Thread-safe operations
 * - Notifies on state changes
 *
 * Usage:
 *   StateTracker tracker("robot_001");
 *   tracker.configure(state_definition);
 *
 *   tracker.on_action_start("nav2_msgs/NavigateToPose");  // -> "navigating"
 *   tracker.on_action_complete(true);                     // -> "idle"
 */
class StateTracker {
public:
    using StateChangeCallback = std::function<void(
        const std::string& agent_id,
        const std::string& old_state,
        const std::string& new_state
    )>;

    /**
     * Constructor.
     *
     * @param agent_id Robot identifier
     * @param default_state Initial state (default: "idle")
     */
    explicit StateTracker(
        const std::string& agent_id,
        const std::string& default_state = "idle"
    );

    ~StateTracker() = default;

    // Non-copyable
    StateTracker(const StateTracker&) = delete;
    StateTracker& operator=(const StateTracker&) = delete;

    // ============================================================
    // Configuration
    // ============================================================

    /**
     * Configure from state definition.
     *
     * Sets available states and action mappings.
     *
     * @param def State definition
     */
    void configure(const StateDefinition& def);

    /**
     * Check if configured with a state definition.
     */
    bool is_configured() const { return configured_.load(); }

    /**
     * Get current state definition ID.
     */
    std::string state_definition_id() const;

    /**
     * Get state definition version.
     */
    int state_definition_version() const;

    // ============================================================
    // State Queries
    // ============================================================

    /**
     * Get current state.
     */
    std::string current_state() const;

    /**
     * Get default state.
     */
    std::string default_state() const;

    /**
     * Get available states.
     */
    std::vector<std::string> available_states() const;

    /**
     * Check if state is valid.
     */
    bool is_valid_state(const std::string& state) const;

    /**
     * Get robot ID.
     */
    const std::string& agent_id() const { return agent_id_; }

    // ============================================================
    // State Transitions
    // ============================================================

    /**
     * Called when an action starts executing.
     *
     * Transitions to the "during" state for the action type.
     *
     * @param action_type Action type being executed
     * @return true if state was changed
     */
    bool on_action_start(const std::string& action_type);

    /**
     * Called when an action completes.
     *
     * Transitions to default state on success, or stays/goes to error.
     *
     * @param success Whether action succeeded
     * @param target_state Optional explicit target state (from graph step)
     * @return true if state was changed
     */
    bool on_action_complete(
        bool success,
        const std::optional<std::string>& target_state = std::nullopt
    );

    /**
     * Called when an error occurs.
     *
     * Transitions to "error" state if available.
     *
     * @param error_message Error description
     */
    void on_error(const std::string& error_message = "");

    /**
     * Clear error state and return to default.
     */
    void clear_error();

    /**
     * Force a specific state (for manual override or graph control).
     *
     * @param state Target state
     * @param trigger Trigger description
     * @return true if state was changed
     */
    bool force_state(const std::string& state, const std::string& trigger = "manual");

    // ============================================================
    // Graph Execution Support
    // ============================================================

    /**
     * Get the state for an action type (from action_mappings).
     *
     * @param action_type Action type
     * @return During state, or nullopt if not mapped
     */
    std::optional<std::string> get_state_for_action(const std::string& action_type) const;

    /**
     * Transition to states specified in graph step.
     *
     * @param during_states States to set during execution
     */
    void set_during_states(const std::vector<std::string>& during_states);

    /**
     * Transition to success states.
     *
     * @param success_states States to set on success
     */
    void set_success_states(const std::vector<std::string>& success_states);

    /**
     * Transition to failure states.
     *
     * @param failure_states States to set on failure
     */
    void set_failure_states(const std::vector<std::string>& failure_states);

    // ============================================================
    // History and Callbacks
    // ============================================================

    /**
     * Get state transition history.
     *
     * @param limit Maximum entries to return (default: all)
     */
    std::vector<StateTransition> get_history(size_t limit = 0) const;

    /**
     * Set state change callback.
     *
     * Called whenever state changes.
     */
    void set_state_change_callback(StateChangeCallback callback);

    /**
     * Get time since last state change.
     */
    std::chrono::milliseconds time_in_current_state() const;

private:
    std::string agent_id_;
    std::string current_state_;
    std::string default_state_;
    std::vector<std::string> available_states_;
    std::unordered_map<std::string, std::string> action_mappings_;  // action_type -> during_state

    std::string state_def_id_;
    int state_def_version_{0};
    std::atomic<bool> configured_{false};

    mutable std::mutex mutex_;
    std::deque<StateTransition> history_;
    static constexpr size_t MAX_HISTORY_SIZE = 100;

    std::chrono::steady_clock::time_point last_state_change_;
    StateChangeCallback state_change_callback_;

    /**
     * Internal state transition.
     *
     * @param new_state Target state
     * @param trigger What caused the transition
     * @return true if state actually changed
     */
    bool transition_to(const std::string& new_state, const std::string& trigger);

    /**
     * Record transition in history.
     */
    void record_transition(
        const std::string& from_state,
        const std::string& to_state,
        const std::string& trigger
    );

    /**
     * Notify callback of state change.
     */
    void notify_state_change(
        const std::string& old_state,
        const std::string& new_state
    );
};

// ============================================================
// State Tracker Manager
// ============================================================

/**
 * StateTrackerManager - Manages state trackers for multiple robots
 */
class StateTrackerManager {
public:
    StateTrackerManager() = default;
    ~StateTrackerManager() = default;

    /**
     * Get or create state tracker for robot.
     */
    std::shared_ptr<StateTracker> get_tracker(const std::string& agent_id);

    /**
     * Configure tracker with state definition.
     */
    void configure_agent(
        const std::string& agent_id,
        const StateDefinition& def
    );

    /**
     * Get all trackers.
     */
    std::vector<std::shared_ptr<StateTracker>> get_all_trackers() const;

    /**
     * Get state versions for all robots (for heartbeat).
     */
    std::unordered_map<std::string, int> get_state_versions() const;

    /**
     * Set global state change callback.
     */
    void set_state_change_callback(StateTracker::StateChangeCallback callback);

private:
    tbb::concurrent_hash_map<std::string, std::shared_ptr<StateTracker>> trackers_;
    StateTracker::StateChangeCallback global_callback_;
    mutable std::mutex callback_mutex_;
};

}  // namespace state
}  // namespace robot_agent
