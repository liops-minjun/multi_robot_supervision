// Copyright 2026 Multi-Robot Supervision System
// Graph State Types and Fleet State Cache - Implementation

#include "fleet_agent/state/graph_state.hpp"

#include <algorithm>

namespace fleet_agent {
namespace state {

// ============================================================
// FleetStateCache Implementation
// ============================================================

void FleetStateCache::update(const FleetStateEntry& entry) {
    std::lock_guard<std::mutex> lock(mutex_);

    auto it = entries_.find(entry.agent_id);
    if (it != entries_.end()) {
        // Update existing entry, update indexes
        update_indexes(it->second, entry);
        it->second = entry;
    } else {
        // New entry
        entries_[entry.agent_id] = entry;
        add_to_indexes(entry);
    }

    last_update_ = std::chrono::system_clock::now();

    if (update_callback_) {
        update_callback_(entry.agent_id, entry);
    }
}

void FleetStateCache::update_batch(const std::vector<FleetStateEntry>& entries) {
    std::lock_guard<std::mutex> lock(mutex_);

    for (const auto& entry : entries) {
        auto it = entries_.find(entry.agent_id);
        if (it != entries_.end()) {
            update_indexes(it->second, entry);
            it->second = entry;
        } else {
            entries_[entry.agent_id] = entry;
            add_to_indexes(entry);
        }

        if (update_callback_) {
            update_callback_(entry.agent_id, entry);
        }
    }

    last_update_ = std::chrono::system_clock::now();
}

void FleetStateCache::mark_offline(const std::string& agent_id) {
    std::lock_guard<std::mutex> lock(mutex_);

    auto it = entries_.find(agent_id);
    if (it != entries_.end()) {
        FleetStateEntry old_entry = it->second;
        it->second.is_online = false;
        it->second.updated_at = std::chrono::system_clock::now();
        update_indexes(old_entry, it->second);

        if (update_callback_) {
            update_callback_(agent_id, it->second);
        }
    }
}

void FleetStateCache::remove(const std::string& agent_id) {
    std::lock_guard<std::mutex> lock(mutex_);

    auto it = entries_.find(agent_id);
    if (it != entries_.end()) {
        remove_from_indexes(agent_id);
        entries_.erase(it);
    }
}

void FleetStateCache::clear() {
    std::lock_guard<std::mutex> lock(mutex_);
    entries_.clear();
    state_index_.clear();
    tag_index_.clear();
    graph_index_.clear();
}

std::optional<FleetStateEntry> FleetStateCache::get(const std::string& agent_id) const {
    std::lock_guard<std::mutex> lock(mutex_);

    auto it = entries_.find(agent_id);
    if (it != entries_.end()) {
        return it->second;
    }
    return std::nullopt;
}

std::vector<std::string> FleetStateCache::get_agents_by_state(const std::string& state_code) const {
    std::lock_guard<std::mutex> lock(mutex_);

    auto it = state_index_.find(state_code);
    if (it != state_index_.end()) {
        return std::vector<std::string>(it->second.begin(), it->second.end());
    }
    return {};
}

std::vector<std::string> FleetStateCache::get_agents_by_tag(const std::string& tag) const {
    std::lock_guard<std::mutex> lock(mutex_);

    auto it = tag_index_.find(tag);
    if (it != tag_index_.end()) {
        return std::vector<std::string>(it->second.begin(), it->second.end());
    }
    return {};
}

std::vector<std::string> FleetStateCache::get_online_agents() const {
    std::lock_guard<std::mutex> lock(mutex_);

    std::vector<std::string> result;
    for (const auto& [agent_id, entry] : entries_) {
        if (entry.is_online) {
            result.push_back(agent_id);
        }
    }
    return result;
}

std::vector<std::string> FleetStateCache::get_agents_by_graph(const std::string& graph_id) const {
    std::lock_guard<std::mutex> lock(mutex_);

    auto it = graph_index_.find(graph_id);
    if (it != graph_index_.end()) {
        return std::vector<std::string>(it->second.begin(), it->second.end());
    }
    return {};
}

std::vector<FleetStateEntry> FleetStateCache::get_all() const {
    std::lock_guard<std::mutex> lock(mutex_);

    std::vector<FleetStateEntry> result;
    result.reserve(entries_.size());
    for (const auto& [_, entry] : entries_) {
        result.push_back(entry);
    }
    return result;
}

PreconditionResult FleetStateCache::evaluate(
    const std::string& self_agent_id,
    const std::string& self_state,
    const EnhancedPrecondition& precondition
) const {
    std::lock_guard<std::mutex> lock(mutex_);

    PreconditionResult result;
    result.satisfied = false;

    switch (precondition.type) {
        case PreconditionType::SelfState: {
            // Check own state
            if (precondition.op == PreconditionOperator::Equals) {
                result.satisfied = (self_state == precondition.expected_state);
            } else if (precondition.op == PreconditionOperator::NotEquals) {
                result.satisfied = (self_state != precondition.expected_state);
            } else if (precondition.op == PreconditionOperator::In) {
                result.satisfied = std::find(
                    precondition.expected_states.begin(),
                    precondition.expected_states.end(),
                    self_state
                ) != precondition.expected_states.end();
            }
            if (!result.satisfied) {
                result.reason = "Self state '" + self_state + "' does not match expected '" +
                                precondition.expected_state + "'";
            }
            result.matched_agents.push_back(self_agent_id);
            break;
        }

        case PreconditionType::AgentState: {
            // Check specific agent's state
            auto it = entries_.find(precondition.target_agent_id);
            if (it == entries_.end()) {
                result.reason = "Agent '" + precondition.target_agent_id + "' not found in cache";
                break;
            }

            const auto& entry = it->second;
            if (precondition.filter.online_only && !entry.is_online) {
                result.reason = "Agent '" + precondition.target_agent_id + "' is offline";
                break;
            }

            if (precondition.op == PreconditionOperator::Equals) {
                result.satisfied = (entry.state_code == precondition.expected_state);
            } else if (precondition.op == PreconditionOperator::NotEquals) {
                result.satisfied = (entry.state_code != precondition.expected_state);
            }

            if (!result.satisfied) {
                result.reason = "Agent '" + precondition.target_agent_id + "' state '" +
                                entry.state_code + "' does not match expected '" +
                                precondition.expected_state + "'";
            }
            result.matched_agents.push_back(precondition.target_agent_id);
            break;
        }

        case PreconditionType::SemanticTag: {
            // Find agents with matching semantic tag
            if (precondition.filter.tags.empty()) {
                result.reason = "No semantic tags specified in filter";
                break;
            }

            std::vector<std::string> matching_agents;
            for (const auto& [agent_id, entry] : entries_) {
                // Skip self if not included
                if (agent_id == self_agent_id && !precondition.filter.include_self) {
                    continue;
                }

                // Apply filters
                if (precondition.filter.online_only && !entry.is_online) {
                    continue;
                }
                if (precondition.filter.executing_only && !entry.is_executing) {
                    continue;
                }
                if (!precondition.filter.graph_id.empty() &&
                    entry.current_graph_id != precondition.filter.graph_id) {
                    continue;
                }

                // Check if agent has all required tags
                bool has_all_tags = true;
                for (const auto& tag : precondition.filter.tags) {
                    if (std::find(entry.semantic_tags.begin(), entry.semantic_tags.end(), tag) ==
                        entry.semantic_tags.end()) {
                        has_all_tags = false;
                        break;
                    }
                }

                if (has_all_tags) {
                    // Check state condition
                    bool state_matches = true;
                    if (!precondition.expected_state.empty()) {
                        if (precondition.op == PreconditionOperator::Equals) {
                            state_matches = (entry.state_code == precondition.expected_state);
                        } else if (precondition.op == PreconditionOperator::NotEquals) {
                            state_matches = (entry.state_code != precondition.expected_state);
                        }
                    }

                    if (state_matches) {
                        matching_agents.push_back(agent_id);
                    }
                }
            }

            result.matched_agents = matching_agents;
            result.satisfied = !matching_agents.empty();
            if (!result.satisfied) {
                result.reason = "No agents found matching semantic tag filter";
            }
            break;
        }

        case PreconditionType::AnyAgentState: {
            // Check if any agent matches the filter and state
            std::vector<std::string> matching_agents;
            for (const auto& [agent_id, entry] : entries_) {
                if (agent_id == self_agent_id && !precondition.filter.include_self) {
                    continue;
                }

                if (precondition.filter.online_only && !entry.is_online) {
                    continue;
                }
                if (precondition.filter.executing_only && !entry.is_executing) {
                    continue;
                }

                // Check state
                bool state_matches = true;
                if (!precondition.expected_state.empty()) {
                    if (precondition.op == PreconditionOperator::Equals) {
                        state_matches = (entry.state_code == precondition.expected_state);
                    } else if (precondition.op == PreconditionOperator::NotEquals) {
                        state_matches = (entry.state_code != precondition.expected_state);
                    }
                }

                if (state_matches) {
                    matching_agents.push_back(agent_id);
                }
            }

            result.matched_agents = matching_agents;
            result.satisfied = !matching_agents.empty();
            if (!result.satisfied) {
                result.reason = "No agents found matching filter with state '" +
                                precondition.expected_state + "'";
            }
            break;
        }

        default:
            result.reason = "Unknown precondition type";
            break;
    }

    return result;
}

size_t FleetStateCache::size() const {
    std::lock_guard<std::mutex> lock(mutex_);
    return entries_.size();
}

std::chrono::system_clock::time_point FleetStateCache::last_update_time() const {
    std::lock_guard<std::mutex> lock(mutex_);
    return last_update_;
}

void FleetStateCache::set_update_callback(StateUpdateCallback callback) {
    std::lock_guard<std::mutex> lock(mutex_);
    update_callback_ = std::move(callback);
}

void FleetStateCache::add_to_indexes(const FleetStateEntry& entry) {
    // State index
    if (!entry.state_code.empty()) {
        state_index_[entry.state_code].insert(entry.agent_id);
    }

    // Tag index
    for (const auto& tag : entry.semantic_tags) {
        tag_index_[tag].insert(entry.agent_id);
    }

    // Graph index
    if (!entry.current_graph_id.empty()) {
        graph_index_[entry.current_graph_id].insert(entry.agent_id);
    }
}

void FleetStateCache::remove_from_indexes(const std::string& agent_id) {
    auto it = entries_.find(agent_id);
    if (it == entries_.end()) return;

    const auto& entry = it->second;

    // Remove from state index
    if (!entry.state_code.empty()) {
        auto state_it = state_index_.find(entry.state_code);
        if (state_it != state_index_.end()) {
            state_it->second.erase(agent_id);
            if (state_it->second.empty()) {
                state_index_.erase(state_it);
            }
        }
    }

    // Remove from tag index
    for (const auto& tag : entry.semantic_tags) {
        auto tag_it = tag_index_.find(tag);
        if (tag_it != tag_index_.end()) {
            tag_it->second.erase(agent_id);
            if (tag_it->second.empty()) {
                tag_index_.erase(tag_it);
            }
        }
    }

    // Remove from graph index
    if (!entry.current_graph_id.empty()) {
        auto graph_it = graph_index_.find(entry.current_graph_id);
        if (graph_it != graph_index_.end()) {
            graph_it->second.erase(agent_id);
            if (graph_it->second.empty()) {
                graph_index_.erase(graph_it);
            }
        }
    }
}

void FleetStateCache::update_indexes(const FleetStateEntry& old_entry, const FleetStateEntry& new_entry) {
    // Update state index
    if (old_entry.state_code != new_entry.state_code) {
        if (!old_entry.state_code.empty()) {
            auto it = state_index_.find(old_entry.state_code);
            if (it != state_index_.end()) {
                it->second.erase(old_entry.agent_id);
                if (it->second.empty()) {
                    state_index_.erase(it);
                }
            }
        }
        if (!new_entry.state_code.empty()) {
            state_index_[new_entry.state_code].insert(new_entry.agent_id);
        }
    }

    // Update tag index
    std::unordered_set<std::string> old_tags(old_entry.semantic_tags.begin(), old_entry.semantic_tags.end());
    std::unordered_set<std::string> new_tags(new_entry.semantic_tags.begin(), new_entry.semantic_tags.end());

    for (const auto& tag : old_tags) {
        if (new_tags.find(tag) == new_tags.end()) {
            auto it = tag_index_.find(tag);
            if (it != tag_index_.end()) {
                it->second.erase(old_entry.agent_id);
                if (it->second.empty()) {
                    tag_index_.erase(it);
                }
            }
        }
    }
    for (const auto& tag : new_tags) {
        if (old_tags.find(tag) == old_tags.end()) {
            tag_index_[tag].insert(new_entry.agent_id);
        }
    }

    // Update graph index
    if (old_entry.current_graph_id != new_entry.current_graph_id) {
        if (!old_entry.current_graph_id.empty()) {
            auto it = graph_index_.find(old_entry.current_graph_id);
            if (it != graph_index_.end()) {
                it->second.erase(old_entry.agent_id);
                if (it->second.empty()) {
                    graph_index_.erase(it);
                }
            }
        }
        if (!new_entry.current_graph_id.empty()) {
            graph_index_[new_entry.current_graph_id].insert(new_entry.agent_id);
        }
    }
}

// ============================================================
// EnhancedStateManager Implementation
// ============================================================

EnhancedStateManager::EnhancedStateManager(const std::string& agent_id)
    : agent_id_(agent_id)
    , current_state_code_("idle")
    , state_updated_at_(std::chrono::system_clock::now())
{
    // Load system states
    for (const auto& state : get_system_states()) {
        graph_states_[state.code] = state;
    }
}

std::string EnhancedStateManager::current_state_code() const {
    std::lock_guard<std::mutex> lock(mutex_);
    return current_state_code_;
}

std::vector<std::string> EnhancedStateManager::current_semantic_tags() const {
    std::lock_guard<std::mutex> lock(mutex_);
    return current_semantic_tags_;
}

std::string EnhancedStateManager::current_graph_id() const {
    std::lock_guard<std::mutex> lock(mutex_);
    return current_graph_id_;
}

void EnhancedStateManager::set_state(
    const std::string& state_code,
    const std::vector<std::string>& semantic_tags,
    const std::string& graph_id
) {
    std::lock_guard<std::mutex> lock(mutex_);
    current_state_code_ = state_code;
    current_semantic_tags_ = semantic_tags;
    current_graph_id_ = graph_id;
    state_updated_at_ = std::chrono::system_clock::now();
}

void EnhancedStateManager::transition_to_step(
    const std::string& step_id,
    StatePhase phase,
    const std::vector<std::string>& semantic_tags
) {
    std::lock_guard<std::mutex> lock(mutex_);

    // Construct state code
    current_state_code_ = step_id + ":" + phase_to_string(phase);
    current_semantic_tags_ = semantic_tags;
    state_updated_at_ = std::chrono::system_clock::now();

    // Add phase-based semantic tags
    switch (phase) {
        case StatePhase::Executing:
            if (std::find(current_semantic_tags_.begin(), current_semantic_tags_.end(), "busy") ==
                current_semantic_tags_.end()) {
                current_semantic_tags_.push_back("busy");
            }
            break;
        case StatePhase::Success:
            if (std::find(current_semantic_tags_.begin(), current_semantic_tags_.end(), "completed") ==
                current_semantic_tags_.end()) {
                current_semantic_tags_.push_back("completed");
            }
            break;
        case StatePhase::Failed:
            if (std::find(current_semantic_tags_.begin(), current_semantic_tags_.end(), "error") ==
                current_semantic_tags_.end()) {
                current_semantic_tags_.push_back("error");
            }
            break;
        default:
            break;
    }
}

void EnhancedStateManager::reset_to_idle() {
    std::lock_guard<std::mutex> lock(mutex_);
    current_state_code_ = "idle";
    current_semantic_tags_ = {"ready", "available"};
    current_graph_id_.clear();
    state_updated_at_ = std::chrono::system_clock::now();
}

void EnhancedStateManager::load_graph_states(
    const std::string& graph_id,
    const std::vector<GraphState>& states
) {
    std::lock_guard<std::mutex> lock(mutex_);

    current_graph_id_ = graph_id;

    // Clear previous step states
    step_states_.clear();

    // Add graph states
    for (const auto& state : states) {
        graph_states_[state.code] = state;

        // Index by step_id
        if (!state.step_id.empty()) {
            step_states_[state.step_id].push_back(state.code);
        }
    }
}

std::optional<GraphState> EnhancedStateManager::get_graph_state(const std::string& code) const {
    std::lock_guard<std::mutex> lock(mutex_);

    auto it = graph_states_.find(code);
    if (it != graph_states_.end()) {
        return it->second;
    }
    return std::nullopt;
}

std::vector<GraphState> EnhancedStateManager::get_states_for_step(const std::string& step_id) const {
    std::lock_guard<std::mutex> lock(mutex_);

    std::vector<GraphState> result;
    auto it = step_states_.find(step_id);
    if (it != step_states_.end()) {
        for (const auto& code : it->second) {
            auto state_it = graph_states_.find(code);
            if (state_it != graph_states_.end()) {
                result.push_back(state_it->second);
            }
        }
    }
    return result;
}

PreconditionResult EnhancedStateManager::evaluate_precondition(
    const EnhancedPrecondition& precondition
) const {
    std::lock_guard<std::mutex> lock(mutex_);
    return fleet_cache_.evaluate(agent_id_, current_state_code_, precondition);
}

PreconditionResult EnhancedStateManager::evaluate_preconditions(
    const std::vector<EnhancedPrecondition>& preconditions
) const {
    PreconditionResult combined;
    combined.satisfied = true;

    for (const auto& precondition : preconditions) {
        auto result = evaluate_precondition(precondition);
        if (!result.satisfied) {
            combined.satisfied = false;
            combined.reason = result.reason;
            // Collect all matched agents
            combined.matched_agents.insert(
                combined.matched_agents.end(),
                result.matched_agents.begin(),
                result.matched_agents.end()
            );
            break;  // Short-circuit on first failure
        }
        combined.matched_agents.insert(
            combined.matched_agents.end(),
            result.matched_agents.begin(),
            result.matched_agents.end()
        );
    }

    return combined;
}

FleetStateEntry EnhancedStateManager::to_fleet_entry() const {
    std::lock_guard<std::mutex> lock(mutex_);

    FleetStateEntry entry;
    entry.agent_id = agent_id_;
    entry.state_code = current_state_code_;
    entry.semantic_tags = current_semantic_tags_;
    entry.current_graph_id = current_graph_id_;
    entry.is_online = true;
    entry.is_executing = (current_state_code_.find(":executing") != std::string::npos);
    entry.updated_at = state_updated_at_;

    return entry;
}

}  // namespace state
}  // namespace fleet_agent
