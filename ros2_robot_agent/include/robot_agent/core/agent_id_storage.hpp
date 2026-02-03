// Copyright 2026 Multi-Robot Supervision System
// Agent ID storage for server-assigned IDs

#pragma once

#include <string>
#include <optional>

namespace robot_agent {

/**
 * AgentIdStorage handles persistent storage of server-assigned agent IDs
 * and generation of hardware fingerprints for ID recovery.
 *
 * Usage flow:
 *   1. On startup, try to load() existing ID from file
 *   2. If no stored ID, generate hardware_fingerprint()
 *   3. Connect to server with empty agent_id and fingerprint
 *   4. Server returns assigned_agent_id
 *   5. save() the assigned ID to file
 *   6. On reconnect, load() returns the stored ID
 */
class AgentIdStorage {
public:
    /**
     * Constructor
     * @param storage_path Path to the agent ID file (e.g., /var/lib/robot_agent/agent_id)
     */
    explicit AgentIdStorage(const std::string& storage_path);

    /**
     * Load agent ID from storage file
     * @return Agent ID if file exists and is valid, nullopt otherwise
     */
    std::optional<std::string> load() const;

    /**
     * Save agent ID to storage file (atomic write)
     * @param agent_id The agent ID to save
     * @return true if successful, false otherwise
     */
    bool save(const std::string& agent_id);

    /**
     * Clear stored agent ID (delete file)
     * @return true if file was deleted or didn't exist
     */
    bool clear();

    /**
     * Check if a stored agent ID exists
     */
    bool exists() const;

    /**
     * Generate a hardware fingerprint for this machine
     * Uses: /etc/machine-id, hostname, network interface names
     * @return SHA256 hash (first 32 hex chars) of combined hardware identifiers
     */
    static std::string generate_hardware_fingerprint();

private:
    std::string storage_path_;

    /**
     * Ensure the directory for storage_path_ exists
     */
    bool ensure_directory_exists() const;
};

}  // namespace robot_agent
