// Copyright 2026 Multi-Robot Supervision System
// State Definition Storage Implementation

#include "robot_agent/state/state_definition.hpp"
#include "robot_agent/core/logger.hpp"

#include <filesystem>
#include <fstream>

namespace robot_agent {
namespace state {

namespace {
logging::ComponentLogger log("StateDefinitionStorage");
}

StateDefinitionStorage::StateDefinitionStorage(const std::string& storage_path)
    : storage_path_(storage_path) {

    if (!storage_path_.empty()) {
        std::filesystem::create_directories(storage_path_);
        load_from_disk();
    }
}

bool StateDefinitionStorage::store(const StateDefinition& def) {
    if (def.id.empty()) {
        log.error("Cannot store state definition with empty ID");
        return false;
    }

    {
        tbb::concurrent_hash_map<std::string, StateDefinition>::accessor acc;
        definitions_.insert(acc, def.id);
        acc->second = def;
    }

    log.info("Stored state definition: {} (version {})", def.id, def.version);

    // Persist to disk if storage path is set
    if (!storage_path_.empty()) {
        if (!write_to_file(def)) {
            log.warn("Failed to persist state definition to disk: {}", def.id);
        }
    }

    return true;
}

std::optional<StateDefinition> StateDefinitionStorage::get(const std::string& id) const {
    tbb::concurrent_hash_map<std::string, StateDefinition>::const_accessor acc;
    if (definitions_.find(acc, id)) {
        return acc->second;
    }
    return std::nullopt;
}

std::optional<StateDefinition> StateDefinitionStorage::get_for_agent(
    const std::string& agent_id) const {

    tbb::concurrent_hash_map<std::string, std::string>::const_accessor mapping_acc;
    if (!agent_mappings_.find(mapping_acc, agent_id)) {
        return std::nullopt;
    }

    return get(mapping_acc->second);
}

void StateDefinitionStorage::map_agent(
    const std::string& agent_id,
    const std::string& state_def_id) {

    tbb::concurrent_hash_map<std::string, std::string>::accessor acc;
    agent_mappings_.insert(acc, agent_id);
    acc->second = state_def_id;

    log.debug("Mapped agent {} to state definition {}", agent_id, state_def_id);
}

bool StateDefinitionStorage::exists(const std::string& id) const {
    tbb::concurrent_hash_map<std::string, StateDefinition>::const_accessor acc;
    return definitions_.find(acc, id);
}

std::optional<int> StateDefinitionStorage::get_version(const std::string& id) const {
    auto def = get(id);
    if (def) {
        return def->version;
    }
    return std::nullopt;
}

std::vector<std::string> StateDefinitionStorage::list_ids() const {
    std::vector<std::string> ids;
    for (auto it = definitions_.begin(); it != definitions_.end(); ++it) {
        ids.push_back(it->first);
    }
    return ids;
}

std::unordered_map<std::string, int> StateDefinitionStorage::get_versions_map() const {
    std::unordered_map<std::string, int> versions;
    for (auto it = definitions_.begin(); it != definitions_.end(); ++it) {
        versions[it->first] = it->second.version;
    }
    return versions;
}

bool StateDefinitionStorage::save_to_disk() {
    if (storage_path_.empty()) {
        return false;
    }

    bool success = true;
    for (auto it = definitions_.begin(); it != definitions_.end(); ++it) {
        if (!write_to_file(it->second)) {
            success = false;
        }
    }

    // Save agent mappings
    std::filesystem::path mappings_path = std::filesystem::path(storage_path_) / "agent_mappings.json";
    try {
        nlohmann::json j;
        for (auto it = agent_mappings_.begin(); it != agent_mappings_.end(); ++it) {
            j[it->first] = it->second;
        }

        std::ofstream file(mappings_path);
        if (file.is_open()) {
            file << j.dump(2);
        }
    } catch (const std::exception& e) {
        log.error("Failed to save agent mappings: {}", e.what());
        success = false;
    }

    return success;
}

bool StateDefinitionStorage::load_from_disk() {
    if (storage_path_.empty()) {
        return false;
    }

    std::filesystem::path dir(storage_path_);
    if (!std::filesystem::exists(dir)) {
        return true;  // Nothing to load
    }

    int loaded = 0;
    for (const auto& entry : std::filesystem::directory_iterator(dir)) {
        if (entry.path().extension() == ".json" &&
            entry.path().filename() != "agent_mappings.json") {

            auto def = read_from_file(entry.path().stem().string());
            if (def) {
                tbb::concurrent_hash_map<std::string, StateDefinition>::accessor acc;
                definitions_.insert(acc, def->id);
                acc->second = *def;
                loaded++;
            }
        }
    }

    // Load agent mappings
    std::filesystem::path mappings_path = dir / "agent_mappings.json";
    if (std::filesystem::exists(mappings_path)) {
        try {
            std::ifstream file(mappings_path);
            nlohmann::json j;
            file >> j;

            for (auto& [agent_id, state_def_id] : j.items()) {
                tbb::concurrent_hash_map<std::string, std::string>::accessor acc;
                agent_mappings_.insert(acc, agent_id);
                acc->second = state_def_id.get<std::string>();
            }
        } catch (const std::exception& e) {
            log.error("Failed to load agent mappings: {}", e.what());
        }
    }

    log.info("Loaded {} state definitions from disk", loaded);
    return true;
}

void StateDefinitionStorage::clear() {
    definitions_.clear();
    agent_mappings_.clear();
}

std::string StateDefinitionStorage::get_file_path(const std::string& id) const {
    return (std::filesystem::path(storage_path_) / (id + ".json")).string();
}

bool StateDefinitionStorage::write_to_file(const StateDefinition& def) {
    try {
        nlohmann::json j = def;
        std::ofstream file(get_file_path(def.id));
        if (!file.is_open()) {
            return false;
        }
        file << j.dump(2);
        return true;
    } catch (const std::exception& e) {
        log.error("Failed to write state definition {}: {}", def.id, e.what());
        return false;
    }
}

std::optional<StateDefinition> StateDefinitionStorage::read_from_file(const std::string& id) {
    try {
        std::ifstream file(get_file_path(id));
        if (!file.is_open()) {
            return std::nullopt;
        }

        nlohmann::json j;
        file >> j;

        StateDefinition def = j.get<StateDefinition>();
        return def;
    } catch (const std::exception& e) {
        log.error("Failed to read state definition {}: {}", id, e.what());
        return std::nullopt;
    }
}

}  // namespace state
}  // namespace robot_agent
