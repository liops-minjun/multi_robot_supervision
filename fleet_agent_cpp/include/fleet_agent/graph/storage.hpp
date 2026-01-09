// Copyright 2026 Multi-Robot Supervision System
// Action Graph Storage

#pragma once

#include "fleet_agent/core/types.hpp"

#include <filesystem>
#include <optional>
#include <string>
#include <vector>

#include <tbb/concurrent_hash_map.h>

// Forward declaration
namespace fleet {
namespace v1 {
class ActionGraph;
}
}

namespace fleet_agent {
namespace graph {

/**
 * GraphStorage - Local storage for deployed Action Graphs.
 *
 * Manages persistent storage of action graphs:
 * - Stores graphs as JSON files
 * - Maintains in-memory cache for fast access
 * - Supports checksum verification
 * - Thread-safe operations
 *
 * Storage format:
 *   /var/lib/fleet_agent/graphs/
 *   ├── pick_and_place_001.json
 *   ├── patrol_route_002.json
 *   └── charging_workflow_003.json
 *
 * Each file contains the canonical graph format (JSON).
 */
class GraphStorage {
public:
    /**
     * Constructor.
     *
     * @param storage_path Directory for graph storage
     */
    explicit GraphStorage(const std::filesystem::path& storage_path);

    ~GraphStorage();

    // ============================================================
    // CRUD Operations
    // ============================================================

    /**
     * Store an action graph.
     *
     * Saves to file and updates cache.
     *
     * @param graph Graph to store
     * @return true if stored successfully
     */
    bool store(const fleet::v1::ActionGraph& graph);

    /**
     * Load a graph by ID.
     *
     * Checks cache first, then file.
     *
     * @param graph_id Graph identifier
     * @return Graph if found
     */
    std::optional<fleet::v1::ActionGraph> load(const std::string& graph_id);

    /**
     * List all stored graph IDs.
     */
    std::vector<std::string> list_graph_ids();

    /**
     * Remove a graph.
     *
     * @param graph_id Graph to remove
     * @return true if removed
     */
    bool remove(const std::string& graph_id);

    // ============================================================
    // Verification
    // ============================================================

    /**
     * Verify graph checksum.
     *
     * @param graph_id Graph to verify
     * @param expected_checksum Expected checksum (sha256:...)
     * @return true if checksum matches
     */
    bool verify_checksum(
        const std::string& graph_id,
        const std::string& expected_checksum
    );

    /**
     * Check if graph exists.
     */
    bool exists(const std::string& graph_id);

    /**
     * Get graph version.
     */
    std::optional<int> get_version(const std::string& graph_id);

    // ============================================================
    // Cache Management
    // ============================================================

    /**
     * Clear in-memory cache.
     *
     * Files remain on disk.
     */
    void clear_cache();

    /**
     * Reload all graphs from disk into cache.
     */
    void reload_cache();

    /**
     * Get cache size.
     */
    size_t cache_size() const;

private:
    std::filesystem::path storage_path_;

    // In-memory cache: graph_id -> ActionGraph
    tbb::concurrent_hash_map<std::string, fleet::v1::ActionGraph> cache_;

    /**
     * Get file path for a graph.
     */
    std::filesystem::path get_graph_path(const std::string& graph_id);

    /**
     * Compute checksum for a graph.
     */
    std::string compute_checksum(const fleet::v1::ActionGraph& graph);

    /**
     * Write graph to file.
     */
    bool write_to_file(
        const std::filesystem::path& path,
        const fleet::v1::ActionGraph& graph
    );

    /**
     * Read graph from file.
     */
    std::optional<fleet::v1::ActionGraph> read_from_file(
        const std::filesystem::path& path
    );

    /**
     * Ensure storage directory exists.
     */
    void ensure_directory();
};

}  // namespace graph
}  // namespace fleet_agent
