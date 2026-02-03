// Copyright 2026 Multi-Robot Supervision System
// Telemetry aggregator for periodic transmission

#pragma once

#include "robot_agent/core/types.hpp"
#include "robot_agent/telemetry/snapshot.hpp"

#include <atomic>
#include <chrono>
#include <memory>
#include <string>
#include <thread>
#include <unordered_map>
#include <vector>

namespace robot_agent {
namespace telemetry {

/**
 * TelemetryAggregator - Aggregates and transmits telemetry for all robots.
 *
 * Runs in a dedicated thread and periodically:
 * 1. Reads telemetry from all robots in TelemetryStore
 * 2. Performs delta detection to only send changed data
 * 3. Creates QUIC Heartbeat messages for reliable transmission
 *
 * The heartbeat now includes JointState, Odometry, and TF data for
 * parameter loading in the frontend.
 *
 * Usage:
 *   TelemetryAggregator aggregator(agent_id, telemetry_store, quic_queue);
 *   aggregator.add_robot("robot_001");
 *   aggregator.start();
 */
class TelemetryAggregator {
public:
    /**
     * Constructor.
     *
     * @param agent_id Agent identifier
     * @param store Shared telemetry store
     * @param quic_queue Queue for QUIC messages
     * @param interval Aggregation interval (default 100ms)
     */
    TelemetryAggregator(
        const std::string& agent_id,
        TelemetryStore& store,
        QuicOutboundQueue& quic_queue,
        std::chrono::milliseconds interval = std::chrono::milliseconds(100)
    );

    ~TelemetryAggregator();

    // ============================================================
    // Lifecycle
    // ============================================================

    /**
     * Start aggregation thread.
     */
    void start();

    /**
     * Stop aggregation thread.
     * Blocks until thread terminates.
     */
    void stop();

    /**
     * Check if aggregator is running.
     */
    bool is_running() const { return running_.load(); }

    // ============================================================
    // Robot Management
    // ============================================================

    /**
     * Add a robot to monitor.
     */
    void add_robot(const std::string& robot_id);

    /**
     * Remove a robot from monitoring.
     */
    void remove_robot(const std::string& robot_id);

    /**
     * Rename a robot (update robot ID mapping).
     * Used when server assigns a new ID after registration.
     *
     * @param old_id Previous robot ID
     * @param new_id New robot ID from server
     * @return true if robot was found and renamed
     */
    bool rename_robot(const std::string& old_id, const std::string& new_id);

    /**
     * Get number of monitored robots.
     */
    size_t robot_count() const;

    // ============================================================
    // Configuration
    // ============================================================

    /**
     * Set aggregation interval.
     */
    void set_interval(std::chrono::milliseconds interval);

    /**
     * Enable/disable delta encoding.
     */
    void set_delta_enabled(bool enabled);

    /**
     * Enable/disable telemetry transmission.
     * When disabled, heartbeats are still sent but without telemetry data.
     */
    void set_telemetry_enabled(bool enabled);

    /**
     * Update agent ID (used when server assigns a new ID).
     */
    void set_agent_id(const std::string& agent_id);

private:
    std::string agent_id_;
    TelemetryStore& store_;
    QuicOutboundQueue& quic_queue_;
    std::chrono::milliseconds interval_;

    // Robot tracking
    std::vector<std::string> robot_ids_;
    mutable std::mutex robots_mutex_;

    // Thread control
    std::atomic<bool> running_{false};
    std::thread aggregator_thread_;

    // Feature flags
    std::atomic<bool> delta_enabled_{true};
    std::atomic<bool> telemetry_enabled_{true};

    // ============================================================
    // Delta Detection
    // ============================================================

    // Last sequence numbers for delta detection
    std::unordered_map<std::string, uint64_t> last_sequences_;

    // Last transmitted timestamps
    std::unordered_map<std::string, std::chrono::steady_clock::time_point> last_sent_;

    // ============================================================
    // Main Loop
    // ============================================================

    void aggregation_loop();

    // ============================================================
    // Message Creation
    // ============================================================

    /**
     * Create QUIC Heartbeat message with telemetry.
     * Contains all robot telemetry for reliable transmission.
     */
    OutboundMessage create_quic_heartbeat();

    /**
     * Build protobuf TelemetryPayload from snapshot.
     */
    void build_telemetry_payload(
        const std::string& robot_id,
        const TelemetrySnapshot& snapshot,
        void* pb_payload  // fleet::v1::TelemetryPayload*
    );

    // ============================================================
    // Helpers
    // ============================================================

    /**
     * Check if robot telemetry has changed.
     */
    bool has_changed(const std::string& robot_id, const TelemetrySnapshot& snapshot);

    /**
     * Get snapshot from store for robot.
     */
    std::optional<TelemetrySnapshot> get_snapshot(const std::string& robot_id);

    /**
     * Cleanup orphaned entries in delta detection maps.
     * Removes entries for robots that are no longer in robot_ids_.
     * @return Number of orphaned entries cleaned up
     */
    size_t cleanup_orphaned_entries();
};

}  // namespace telemetry
}  // namespace robot_agent
