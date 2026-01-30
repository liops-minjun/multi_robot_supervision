// Copyright 2026 Multi-Robot Supervision System
// Behavior Tree Storage Implementation

#include "fleet_agent/graph/storage.hpp"
#include "fleet_agent/core/logger.hpp"

#include <fstream>
#include <sstream>

#include <google/protobuf/util/json_util.h>
#include <openssl/sha.h>
#include <nlohmann/json.hpp>

// Include generated proto header
#include "fleet/v1/graphs.pb.h"

namespace fleet_agent {
namespace graph {

namespace {
logging::ComponentLogger log("GraphStorage");

// Convert bytes to hex string
std::string bytes_to_hex(const unsigned char* data, size_t len) {
    std::stringstream ss;
    ss << std::hex << std::setfill('0');
    for (size_t i = 0; i < len; ++i) {
        ss << std::setw(2) << static_cast<int>(data[i]);
    }
    return ss.str();
}

}  // namespace

GraphStorage::GraphStorage(const std::filesystem::path& storage_path)
    : storage_path_(storage_path) {

    ensure_directory();
    reload_cache();

    log.info("Initialized with storage path: {}", storage_path_.string());
}

GraphStorage::~GraphStorage() = default;

void GraphStorage::ensure_directory() {
    if (!std::filesystem::exists(storage_path_)) {
        std::filesystem::create_directories(storage_path_);
        log.info("Created storage directory: {}", storage_path_.string());
    }
}

std::filesystem::path GraphStorage::get_behavior_tree_path(const std::string& behavior_tree_id) {
    return storage_path_ / (behavior_tree_id + ".json");
}

std::string GraphStorage::compute_checksum(const fleet::v1::BehaviorTree& behavior_tree) {
    // Serialize to JSON for consistent hashing
    std::string json_str;
    google::protobuf::util::JsonPrintOptions options;
    options.add_whitespace = false;
    options.preserve_proto_field_names = true;

    auto status = google::protobuf::util::MessageToJsonString(behavior_tree, &json_str, options);
    if (!status.ok()) {
        log.error("Failed to serialize behavior tree for checksum: {}", status.ToString());
        return "";
    }

    // Compute SHA256
    unsigned char hash[SHA256_DIGEST_LENGTH];
    SHA256(reinterpret_cast<const unsigned char*>(json_str.data()),
           json_str.size(), hash);

    return "sha256:" + bytes_to_hex(hash, SHA256_DIGEST_LENGTH);
}

bool GraphStorage::write_to_file(
    const std::filesystem::path& path,
    const fleet::v1::BehaviorTree& behavior_tree) {

    std::string json_str;
    google::protobuf::util::JsonPrintOptions options;
    options.add_whitespace = true;  // Pretty print
    options.preserve_proto_field_names = true;

    auto status = google::protobuf::util::MessageToJsonString(behavior_tree, &json_str, options);
    if (!status.ok()) {
        log.error("Failed to serialize behavior tree: {}", status.ToString());
        return false;
    }

    std::ofstream file(path);
    if (!file.is_open()) {
        log.error("Failed to open file for writing: {}", path.string());
        return false;
    }

    file << json_str;
    return true;
}

std::optional<fleet::v1::BehaviorTree> GraphStorage::read_from_file(
    const std::filesystem::path& path) {

    if (!std::filesystem::exists(path)) {
        return std::nullopt;
    }

    std::ifstream file(path);
    if (!file.is_open()) {
        log.error("Failed to open file for reading: {}", path.string());
        return std::nullopt;
    }

    std::stringstream buffer;
    buffer << file.rdbuf();
    std::string json_str = buffer.str();

    fleet::v1::BehaviorTree behavior_tree;
    google::protobuf::util::JsonParseOptions options;
    options.ignore_unknown_fields = true;

    auto status = google::protobuf::util::JsonStringToMessage(json_str, &behavior_tree, options);
    if (!status.ok()) {
        log.error("Failed to parse behavior tree from {}: {}", path.string(), status.ToString());
        return std::nullopt;
    }

    return behavior_tree;
}

// Parse graph_json and populate protobuf fields
void populate_from_graph_json(fleet::v1::BehaviorTree& behavior_tree) {
    const std::string& json_str = behavior_tree.graph_json();
    if (json_str.empty()) return;
    if (behavior_tree.vertices_size() > 0) return;  // Already populated

    try {
        auto j = nlohmann::json::parse(json_str);

        // Extract entry_point
        if (j.contains("entry_point") && j["entry_point"].is_string()) {
            behavior_tree.set_entry_point(j["entry_point"].get<std::string>());
        }

        // Extract vertices
        if (j.contains("vertices") && j["vertices"].is_array()) {
            for (const auto& vj : j["vertices"]) {
                auto* v = behavior_tree.add_vertices();
                if (vj.contains("id")) v->set_id(vj["id"].get<std::string>());

                // Set vertex type
                std::string type_str = vj.value("type", "step");
                if (type_str == "terminal") {
                    v->set_type(fleet::v1::VERTEX_TYPE_TERMINAL);
                    if (vj.contains("terminal")) {
                        auto* term = v->mutable_terminal();
                        const auto& tj = vj["terminal"];
                        std::string term_type = tj.value("terminal_type", "success");
                        if (term_type == "success") {
                            term->set_terminal_type(fleet::v1::TERMINAL_TYPE_SUCCESS);
                        } else if (term_type == "failure") {
                            term->set_terminal_type(fleet::v1::TERMINAL_TYPE_FAILURE);
                        }
                    }
                } else {
                    v->set_type(fleet::v1::VERTEX_TYPE_STEP);

                    // Parse step data
                    if (vj.contains("step")) {
                        auto* step = v->mutable_step();
                        const auto& sj = vj["step"];

                        std::string step_type = sj.value("step_type", "action");
                        if (step_type == "action") {
                            step->set_step_type(fleet::v1::STEP_TYPE_ACTION);

                            if (sj.contains("action")) {
                                auto* action = step->mutable_action();
                                const auto& aj = sj["action"];
                                if (aj.contains("type")) {
                                    action->set_action_type(aj["type"].get<std::string>());
                                }
                                if (aj.contains("server")) {
                                    action->set_action_server(aj["server"].get<std::string>());
                                }
                                if (aj.contains("timeout_sec")) {
                                    action->set_timeout_sec(aj["timeout_sec"].get<float>());
                                }
                                if (aj.contains("params") && aj["params"].is_object()) {
                                    // Preserve the full params object including field_sources
                                    std::string params_str = aj["params"].dump();
                                    action->set_goal_params(params_str);
                                }
                            }
                        } else if (step_type == "wait") {
                            step->set_step_type(fleet::v1::STEP_TYPE_WAIT);
                            if (sj.contains("wait")) {
                                auto* wait = step->mutable_wait();
                                if (sj["wait"].contains("duration_sec")) {
                                    wait->set_duration_sec(sj["wait"]["duration_sec"].get<float>());
                                }
                            }
                        }

                        // Parse state management
                        if (sj.contains("states")) {
                            const auto& states = sj["states"];
                            if (states.contains("during") && states["during"].is_array()) {
                                for (const auto& s : states["during"]) {
                                    step->add_during_states(s.get<std::string>());
                                }
                            }
                            if (states.contains("success") && states["success"].is_array()) {
                                for (const auto& s : states["success"]) {
                                    step->add_success_states(s.get<std::string>());
                                }
                            }
                            if (states.contains("failure") && states["failure"].is_array()) {
                                for (const auto& s : states["failure"]) {
                                    step->add_failure_states(s.get<std::string>());
                                }
                            }
                        }
                    }
                }
            }
        }

        // Extract edges
        if (j.contains("edges") && j["edges"].is_array()) {
            for (const auto& ej : j["edges"]) {
                auto* e = behavior_tree.add_edges();
                if (ej.contains("from")) e->set_from_vertex(ej["from"].get<std::string>());
                if (ej.contains("to")) e->set_to_vertex(ej["to"].get<std::string>());

                std::string edge_type = ej.value("type", "on_success");
                if (edge_type == "on_success") {
                    e->set_type(fleet::v1::EDGE_TYPE_ON_SUCCESS);
                } else if (edge_type == "on_failure") {
                    e->set_type(fleet::v1::EDGE_TYPE_ON_FAILURE);
                } else if (edge_type == "on_timeout") {
                    e->set_type(fleet::v1::EDGE_TYPE_ON_TIMEOUT);
                } else if (edge_type == "conditional") {
                    e->set_type(fleet::v1::EDGE_TYPE_CONDITIONAL);
                }

                if (ej.contains("condition")) {
                    e->set_condition(ej["condition"].get<std::string>());
                }
            }
        }

        log.info("Populated behavior tree from graph_json: {} vertices, {} edges, entry={}",
                 behavior_tree.vertices_size(), behavior_tree.edges_size(), behavior_tree.entry_point());

    } catch (const std::exception& ex) {
        log.error("Failed to parse graph_json: {}", ex.what());
    }
}

bool GraphStorage::store(const fleet::v1::BehaviorTree& behavior_tree) {
    std::string behavior_tree_id = behavior_tree.metadata().id();
    if (behavior_tree_id.empty()) {
        log.error("Behavior tree has no ID");
        return false;
    }

    // Compute checksum if not present
    fleet::v1::BehaviorTree behavior_tree_copy = behavior_tree;
    if (behavior_tree_copy.checksum().empty()) {
        behavior_tree_copy.set_checksum(compute_checksum(behavior_tree_copy));
    }

    // Parse graph_json to populate vertices/edges/entry_point if needed
    populate_from_graph_json(behavior_tree_copy);

    // Write to file
    auto path = get_behavior_tree_path(behavior_tree_id);
    if (!write_to_file(path, behavior_tree_copy)) {
        return false;
    }

    // Update cache
    {
        tbb::concurrent_hash_map<std::string, fleet::v1::BehaviorTree>::accessor acc;
        cache_.insert(acc, behavior_tree_id);
        acc->second = behavior_tree_copy;
    }

    log.info("Stored behavior tree: {} (version {})",
             behavior_tree_id, behavior_tree_copy.metadata().version());
    return true;
}

std::optional<fleet::v1::BehaviorTree> GraphStorage::load(const std::string& behavior_tree_id) {
    // Check cache first
    {
        tbb::concurrent_hash_map<std::string, fleet::v1::BehaviorTree>::const_accessor acc;
        if (cache_.find(acc, behavior_tree_id)) {
            // Check if already populated
            if (acc->second.vertices_size() > 0 || acc->second.entry_point().empty() == false) {
                return acc->second;
            }
        }
    }

    // Load from file
    auto path = get_behavior_tree_path(behavior_tree_id);
    auto behavior_tree = read_from_file(path);

    if (behavior_tree) {
        // Parse graph_json if vertices are empty (legacy format)
        populate_from_graph_json(*behavior_tree);

        // Update cache with populated behavior tree
        tbb::concurrent_hash_map<std::string, fleet::v1::BehaviorTree>::accessor acc;
        cache_.insert(acc, behavior_tree_id);
        acc->second = *behavior_tree;
    }

    return behavior_tree;
}

std::vector<std::string> GraphStorage::list_behavior_tree_ids() {
    std::vector<std::string> ids;

    for (const auto& entry : std::filesystem::directory_iterator(storage_path_)) {
        if (entry.is_regular_file() && entry.path().extension() == ".json") {
            ids.push_back(entry.path().stem().string());
        }
    }

    return ids;
}

bool GraphStorage::remove(const std::string& behavior_tree_id) {
    // Remove from cache
    cache_.erase(behavior_tree_id);

    // Remove file
    auto path = get_behavior_tree_path(behavior_tree_id);
    if (std::filesystem::exists(path)) {
        std::filesystem::remove(path);
        log.info("Removed behavior tree: {}", behavior_tree_id);
        return true;
    }

    return false;
}

bool GraphStorage::verify_checksum(
    const std::string& behavior_tree_id,
    const std::string& expected_checksum) {

    auto behavior_tree = load(behavior_tree_id);
    if (!behavior_tree) {
        return false;
    }

    std::string actual = compute_checksum(*behavior_tree);
    return actual == expected_checksum;
}

bool GraphStorage::exists(const std::string& behavior_tree_id) {
    // Check cache
    {
        tbb::concurrent_hash_map<std::string, fleet::v1::BehaviorTree>::const_accessor acc;
        if (cache_.find(acc, behavior_tree_id)) {
            return true;
        }
    }

    // Check file
    return std::filesystem::exists(get_behavior_tree_path(behavior_tree_id));
}

std::optional<int> GraphStorage::get_version(const std::string& behavior_tree_id) {
    auto behavior_tree = load(behavior_tree_id);
    if (behavior_tree) {
        return behavior_tree->metadata().version();
    }
    return std::nullopt;
}

void GraphStorage::clear_cache() {
    cache_.clear();
}

void GraphStorage::reload_cache() {
    cache_.clear();

    for (const auto& behavior_tree_id : list_behavior_tree_ids()) {
        load(behavior_tree_id);  // This populates the cache
    }

    log.info("Reloaded {} behavior trees into cache", cache_.size());
}

size_t GraphStorage::cache_size() const {
    return cache_.size();
}

}  // namespace graph
}  // namespace fleet_agent
