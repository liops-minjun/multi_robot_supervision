// Copyright 2026 Multi-Robot Supervision System
// Graph Storage Implementation

#include "fleet_agent/graph/storage.hpp"
#include "fleet_agent/core/logger.hpp"

#include <fstream>
#include <sstream>

#include <google/protobuf/util/json_util.h>
#include <openssl/sha.h>

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

std::filesystem::path GraphStorage::get_graph_path(const std::string& graph_id) {
    return storage_path_ / (graph_id + ".json");
}

std::string GraphStorage::compute_checksum(const fleet::v1::ActionGraph& graph) {
    // Serialize to JSON for consistent hashing
    std::string json_str;
    google::protobuf::util::JsonPrintOptions options;
    options.add_whitespace = false;
    options.preserve_proto_field_names = true;

    auto status = google::protobuf::util::MessageToJsonString(graph, &json_str, options);
    if (!status.ok()) {
        log.error("Failed to serialize graph for checksum: {}", status.ToString());
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
    const fleet::v1::ActionGraph& graph) {

    std::string json_str;
    google::protobuf::util::JsonPrintOptions options;
    options.add_whitespace = true;  // Pretty print
    options.preserve_proto_field_names = true;

    auto status = google::protobuf::util::MessageToJsonString(graph, &json_str, options);
    if (!status.ok()) {
        log.error("Failed to serialize graph: {}", status.ToString());
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

std::optional<fleet::v1::ActionGraph> GraphStorage::read_from_file(
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

    fleet::v1::ActionGraph graph;
    google::protobuf::util::JsonParseOptions options;
    options.ignore_unknown_fields = true;

    auto status = google::protobuf::util::JsonStringToMessage(json_str, &graph, options);
    if (!status.ok()) {
        log.error("Failed to parse graph from {}: {}", path.string(), status.ToString());
        return std::nullopt;
    }

    return graph;
}

bool GraphStorage::store(const fleet::v1::ActionGraph& graph) {
    std::string graph_id = graph.metadata().id();
    if (graph_id.empty()) {
        log.error("Graph has no ID");
        return false;
    }

    // Compute checksum if not present
    fleet::v1::ActionGraph graph_copy = graph;
    if (graph_copy.checksum().empty()) {
        graph_copy.set_checksum(compute_checksum(graph_copy));
    }

    // Write to file
    auto path = get_graph_path(graph_id);
    if (!write_to_file(path, graph_copy)) {
        return false;
    }

    // Update cache
    {
        tbb::concurrent_hash_map<std::string, fleet::v1::ActionGraph>::accessor acc;
        cache_.insert(acc, graph_id);
        acc->second = graph_copy;
    }

    log.info("Stored graph: {} (version {})",
             graph_id, graph_copy.metadata().version());
    return true;
}

std::optional<fleet::v1::ActionGraph> GraphStorage::load(const std::string& graph_id) {
    // Check cache first
    {
        tbb::concurrent_hash_map<std::string, fleet::v1::ActionGraph>::const_accessor acc;
        if (cache_.find(acc, graph_id)) {
            return acc->second;
        }
    }

    // Load from file
    auto path = get_graph_path(graph_id);
    auto graph = read_from_file(path);

    if (graph) {
        // Update cache
        tbb::concurrent_hash_map<std::string, fleet::v1::ActionGraph>::accessor acc;
        cache_.insert(acc, graph_id);
        acc->second = *graph;
    }

    return graph;
}

std::vector<std::string> GraphStorage::list_graph_ids() {
    std::vector<std::string> ids;

    for (const auto& entry : std::filesystem::directory_iterator(storage_path_)) {
        if (entry.is_regular_file() && entry.path().extension() == ".json") {
            ids.push_back(entry.path().stem().string());
        }
    }

    return ids;
}

bool GraphStorage::remove(const std::string& graph_id) {
    // Remove from cache
    cache_.erase(graph_id);

    // Remove file
    auto path = get_graph_path(graph_id);
    if (std::filesystem::exists(path)) {
        std::filesystem::remove(path);
        log.info("Removed graph: {}", graph_id);
        return true;
    }

    return false;
}

bool GraphStorage::verify_checksum(
    const std::string& graph_id,
    const std::string& expected_checksum) {

    auto graph = load(graph_id);
    if (!graph) {
        return false;
    }

    std::string actual = compute_checksum(*graph);
    return actual == expected_checksum;
}

bool GraphStorage::exists(const std::string& graph_id) {
    // Check cache
    {
        tbb::concurrent_hash_map<std::string, fleet::v1::ActionGraph>::const_accessor acc;
        if (cache_.find(acc, graph_id)) {
            return true;
        }
    }

    // Check file
    return std::filesystem::exists(get_graph_path(graph_id));
}

std::optional<int> GraphStorage::get_version(const std::string& graph_id) {
    auto graph = load(graph_id);
    if (graph) {
        return graph->metadata().version();
    }
    return std::nullopt;
}

void GraphStorage::clear_cache() {
    cache_.clear();
}

void GraphStorage::reload_cache() {
    cache_.clear();

    for (const auto& graph_id : list_graph_ids()) {
        load(graph_id);  // This populates the cache
    }

    log.info("Reloaded {} graphs into cache", cache_.size());
}

size_t GraphStorage::cache_size() const {
    return cache_.size();
}

}  // namespace graph
}  // namespace fleet_agent
