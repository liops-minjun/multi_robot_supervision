// Copyright 2026 Multi-Robot Supervision System
// State Tracker Implementation

#include "fleet_agent/state/state_tracker.hpp"
#include "fleet_agent/core/logger.hpp"

namespace fleet_agent {
namespace state {

namespace {
logging::ComponentLogger log("StateTracker");
}

// ============================================================
// StateTracker Implementation
// ============================================================

StateTracker::StateTracker(const std::string& agent_id, const std::string& default_state)
    : agent_id_(agent_id)
    , current_state_(default_state)
    , default_state_(default_state)
    , available_states_({default_state, "error"})
    , last_state_change_(std::chrono::steady_clock::now()) {

    log.debug("Created state tracker for robot {}, default state: {}",
              agent_id_, default_state_);
}

void StateTracker::configure(const StateDefinition& def) {
    std::lock_guard<std::mutex> lock(mutex_);

    available_states_ = def.states;
    default_state_ = def.default_state;
    state_def_id_ = def.id;
    state_def_version_ = def.version;

    // Build action mapping lookup
    action_mappings_.clear();
    for (const auto& mapping : def.action_mappings) {
        if (!mapping.action_type.empty()) {
            std::string during = mapping.during_state;
            if (!mapping.during_states.empty()) {
                during = mapping.during_states[0];
            }
            if (!during.empty()) {
                action_mappings_[mapping.action_type] = during;
            }
        }
    }

    // Ensure "error" state exists
    if (std::find(available_states_.begin(), available_states_.end(), "error")
        == available_states_.end()) {
        available_states_.push_back("error");
    }

    // Reset to default state if current state is invalid
    if (!is_valid_state(current_state_)) {
        current_state_ = default_state_;
    }

    configured_ = true;

    log.info("Configured state tracker for robot {} with definition {} (version {}, {} states, {} mappings)",
             agent_id_, def.id, def.version, available_states_.size(), action_mappings_.size());
}

std::string StateTracker::state_definition_id() const {
    std::lock_guard<std::mutex> lock(mutex_);
    return state_def_id_;
}

int StateTracker::state_definition_version() const {
    std::lock_guard<std::mutex> lock(mutex_);
    return state_def_version_;
}

std::string StateTracker::current_state() const {
    std::lock_guard<std::mutex> lock(mutex_);
    return current_state_;
}

std::string StateTracker::default_state() const {
    std::lock_guard<std::mutex> lock(mutex_);
    return default_state_;
}

std::vector<std::string> StateTracker::available_states() const {
    std::lock_guard<std::mutex> lock(mutex_);
    return available_states_;
}

bool StateTracker::is_valid_state(const std::string& state) const {
    // No lock - called from locked context
    return std::find(available_states_.begin(), available_states_.end(), state)
           != available_states_.end();
}

bool StateTracker::on_action_start(const std::string& action_type) {
    std::lock_guard<std::mutex> lock(mutex_);

    auto it = action_mappings_.find(action_type);
    if (it != action_mappings_.end() && is_valid_state(it->second)) {
        return transition_to(it->second, "action_start:" + action_type);
    }

    log.debug("No state mapping for action type: {}", action_type);
    return false;
}

bool StateTracker::on_action_complete(
    bool success,
    const std::optional<std::string>& target_state) {

    std::lock_guard<std::mutex> lock(mutex_);

    std::string new_state;
    std::string trigger;

    if (target_state && is_valid_state(*target_state)) {
        // Use explicit target state from graph step
        new_state = *target_state;
        trigger = success ? "action_success" : "action_failure";
    } else if (success) {
        // Default to idle on success
        new_state = default_state_;
        trigger = "action_success";
    } else {
        // On failure, stay in current state or go to error based on config
        // For now, return to default state
        new_state = default_state_;
        trigger = "action_failure";
    }

    return transition_to(new_state, trigger);
}

void StateTracker::on_error(const std::string& error_message) {
    std::lock_guard<std::mutex> lock(mutex_);

    if (is_valid_state("error")) {
        transition_to("error", "error:" + error_message);
    }

    log.error("Robot {} error: {}", agent_id_, error_message);
}

void StateTracker::clear_error() {
    std::lock_guard<std::mutex> lock(mutex_);

    if (current_state_ == "error") {
        transition_to(default_state_, "error_cleared");
    }
}

bool StateTracker::force_state(const std::string& state, const std::string& trigger) {
    std::lock_guard<std::mutex> lock(mutex_);

    if (!is_valid_state(state)) {
        log.warn("Invalid state for robot {}: {}", agent_id_, state);
        return false;
    }

    return transition_to(state, trigger);
}

std::optional<std::string> StateTracker::get_state_for_action(
    const std::string& action_type) const {

    std::lock_guard<std::mutex> lock(mutex_);

    auto it = action_mappings_.find(action_type);
    if (it != action_mappings_.end()) {
        return it->second;
    }
    return std::nullopt;
}

void StateTracker::set_during_states(const std::vector<std::string>& during_states) {
    if (during_states.empty()) return;

    std::lock_guard<std::mutex> lock(mutex_);

    // Use first state - auto-register graph-provided states if not already valid
    for (const auto& state : during_states) {
        if (!state.empty()) {
            // Dynamically register graph-defined states (e.g., "navigate_during", "pick_during")
            if (!is_valid_state(state)) {
                available_states_.push_back(state);
                log.debug("Dynamically registered graph state: {}", state);
            }
            transition_to(state, "graph_during_state");
            return;
        }
    }
}

void StateTracker::set_success_states(const std::vector<std::string>& success_states) {
    if (success_states.empty()) {
        std::lock_guard<std::mutex> lock(mutex_);
        transition_to(default_state_, "graph_success_default");
        return;
    }

    std::lock_guard<std::mutex> lock(mutex_);

    for (const auto& state : success_states) {
        if (!state.empty()) {
            // Dynamically register graph-defined states (e.g., "navigate_succeed", "pick_succeed")
            if (!is_valid_state(state)) {
                available_states_.push_back(state);
                log.debug("Dynamically registered graph state: {}", state);
            }
            transition_to(state, "graph_success_state");
            return;
        }
    }
}

void StateTracker::set_failure_states(const std::vector<std::string>& failure_states) {
    if (failure_states.empty()) {
        std::lock_guard<std::mutex> lock(mutex_);
        transition_to(default_state_, "graph_failure_default");
        return;
    }

    std::lock_guard<std::mutex> lock(mutex_);

    for (const auto& state : failure_states) {
        if (!state.empty()) {
            // Dynamically register graph-defined states (e.g., "navigate_failed", "pick_aborted")
            if (!is_valid_state(state)) {
                available_states_.push_back(state);
                log.debug("Dynamically registered graph state: {}", state);
            }
            transition_to(state, "graph_failure_state");
            return;
        }
    }
}

std::vector<StateTransition> StateTracker::get_history(size_t limit) const {
    std::lock_guard<std::mutex> lock(mutex_);

    if (limit == 0 || limit >= history_.size()) {
        return std::vector<StateTransition>(history_.begin(), history_.end());
    }

    return std::vector<StateTransition>(
        history_.end() - limit,
        history_.end()
    );
}

void StateTracker::set_state_change_callback(StateChangeCallback callback) {
    std::lock_guard<std::mutex> lock(mutex_);
    state_change_callback_ = std::move(callback);
}

std::chrono::milliseconds StateTracker::time_in_current_state() const {
    auto now = std::chrono::steady_clock::now();
    return std::chrono::duration_cast<std::chrono::milliseconds>(
        now - last_state_change_
    );
}

bool StateTracker::transition_to(const std::string& new_state, const std::string& trigger) {
    // Must be called with mutex held
    if (current_state_ == new_state) {
        return false;
    }

    std::string old_state = current_state_;
    current_state_ = new_state;
    last_state_change_ = std::chrono::steady_clock::now();

    record_transition(old_state, new_state, trigger);

    log.info("Robot {} state: {} -> {} ({})",
             agent_id_, old_state, new_state, trigger);

    notify_state_change(old_state, new_state);

    return true;
}

void StateTracker::record_transition(
    const std::string& from_state,
    const std::string& to_state,
    const std::string& trigger) {

    StateTransition trans;
    trans.from_state = from_state;
    trans.to_state = to_state;
    trans.trigger = trigger;
    trans.timestamp = std::chrono::system_clock::now();

    history_.push_back(trans);

    // Trim history
    while (history_.size() > MAX_HISTORY_SIZE) {
        history_.pop_front();
    }
}

void StateTracker::notify_state_change(
    const std::string& old_state,
    const std::string& new_state) {

    if (state_change_callback_) {
        try {
            state_change_callback_(agent_id_, old_state, new_state);
        } catch (const std::exception& e) {
            log.error("State change callback exception: {}", e.what());
        }
    }
}

// ============================================================
// StateTrackerManager Implementation
// ============================================================

std::shared_ptr<StateTracker> StateTrackerManager::get_tracker(const std::string& agent_id) {
    tbb::concurrent_hash_map<std::string, std::shared_ptr<StateTracker>>::accessor acc;
    if (trackers_.find(acc, agent_id)) {
        return acc->second;
    }

    // Create new tracker
    auto tracker = std::make_shared<StateTracker>(agent_id);

    // Set global callback if configured
    {
        std::lock_guard<std::mutex> lock(callback_mutex_);
        if (global_callback_) {
            tracker->set_state_change_callback(global_callback_);
        }
    }

    trackers_.insert(acc, agent_id);
    acc->second = tracker;

    return tracker;
}

void StateTrackerManager::configure_agent(
    const std::string& agent_id,
    const StateDefinition& def) {

    auto tracker = get_tracker(agent_id);
    tracker->configure(def);
}

std::vector<std::shared_ptr<StateTracker>> StateTrackerManager::get_all_trackers() const {
    std::vector<std::shared_ptr<StateTracker>> result;
    for (auto it = trackers_.begin(); it != trackers_.end(); ++it) {
        result.push_back(it->second);
    }
    return result;
}

std::unordered_map<std::string, int> StateTrackerManager::get_state_versions() const {
    std::unordered_map<std::string, int> versions;
    for (auto it = trackers_.begin(); it != trackers_.end(); ++it) {
        if (it->second->is_configured()) {
            versions[it->second->state_definition_id()] =
                it->second->state_definition_version();
        }
    }
    return versions;
}

void StateTrackerManager::set_state_change_callback(StateTracker::StateChangeCallback callback) {
    std::lock_guard<std::mutex> lock(callback_mutex_);
    global_callback_ = callback;

    // Update existing trackers
    for (auto it = trackers_.begin(); it != trackers_.end(); ++it) {
        it->second->set_state_change_callback(callback);
    }
}

}  // namespace state
}  // namespace fleet_agent
