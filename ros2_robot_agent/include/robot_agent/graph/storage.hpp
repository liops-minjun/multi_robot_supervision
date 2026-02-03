// Copyright 2026 Multi-Robot Supervision System
// Behavior Tree Storage

#pragma once

#include "robot_agent/core/types.hpp"

#include <filesystem>
#include <optional>
#include <string>
#include <vector>

#include <tbb/concurrent_hash_map.h>

// Forward declaration
namespace fleet {
namespace v1 {
class BehaviorTree;
}
}

namespace robot_agent {
namespace graph {

/**
 * GraphStorage - Local storage for deployed Behavior Trees.
 *
 * Manages persistent storage of behavior trees:
 * - Stores behavior trees as JSON files
 * - Maintains in-memory cache for fast access
 * - Supports checksum verification
 * - Thread-safe operations
 *
 * Storage format:
 *   /var/lib/robot_agent/graphs/
 *   ├── pick_and_place_001.json
 *   ├── patrol_route_002.json
 *   └── charging_workflow_003.json
 *
 * Each file contains the canonical behavior tree format (JSON).
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
     * Store a behavior tree.
     *
     * Saves to file and updates cache.
     *
     * @param behavior_tree Behavior tree to store
     * @return true if stored successfully
     */
    bool store(const fleet::v1::BehaviorTree& behavior_tree);

    /**
     * Load a behavior tree by ID.
     *
     * Checks cache first, then file.
     *
     * @param behavior_tree_id Behavior tree identifier
     * @return Behavior tree if found
     */
    std::optional<fleet::v1::BehaviorTree> load(const std::string& behavior_tree_id);

    /**
     * List all stored behavior tree IDs.
     */
    std::vector<std::string> list_behavior_tree_ids();

    /**
     * Remove a behavior tree.
     *
     * @param behavior_tree_id Behavior tree to remove
     * @return true if removed
     */
    bool remove(const std::string& behavior_tree_id);

    // ============================================================
    // Verification
    // ============================================================

    /**
     * Verify behavior tree checksum.
     *
     * @param behavior_tree_id Behavior tree to verify
     * @param expected_checksum Expected checksum (sha256:...)
     * @return true if checksum matches
     */
    bool verify_checksum(
        const std::string& behavior_tree_id,
        const std::string& expected_checksum
    );

    /**
     * Check if behavior tree exists.
     */
    bool exists(const std::string& behavior_tree_id);

    /**
     * Get behavior tree version.
     */
    std::optional<int> get_version(const std::string& behavior_tree_id);

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

    // In-memory cache: behavior_tree_id -> BehaviorTree
    tbb::concurrent_hash_map<std::string, fleet::v1::BehaviorTree> cache_;

    /**
     * Get file path for a behavior tree.
     */
    std::filesystem::path get_behavior_tree_path(const std::string& behavior_tree_id);

    /**
     * Compute checksum for a behavior tree.
     */
    std::string compute_checksum(const fleet::v1::BehaviorTree& behavior_tree);

    /**
     * Write behavior tree to file.
     */
    bool write_to_file(
        const std::filesystem::path& path,
        const fleet::v1::BehaviorTree& behavior_tree
    );

    /**
     * Read behavior tree from file.
     */
    std::optional<fleet::v1::BehaviorTree> read_from_file(
        const std::filesystem::path& path
    );

    /**
     * Ensure storage directory exists.
     */
    void ensure_directory();
};

}  // namespace graph
}  // namespace robot_agent
