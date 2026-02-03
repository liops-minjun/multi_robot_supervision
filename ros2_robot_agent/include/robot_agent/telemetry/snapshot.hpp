// Copyright 2026 Multi-Robot Supervision System
// Telemetry snapshot types for parameter loading

#pragma once

#include <chrono>
#include <mutex>
#include <optional>
#include <string>
#include <unordered_map>
#include <vector>

#include <tbb/concurrent_hash_map.h>

#include <sensor_msgs/msg/joint_state.hpp>
#include <nav_msgs/msg/odometry.hpp>
#include <geometry_msgs/msg/transform_stamped.hpp>

namespace robot_agent {
namespace telemetry {

// ============================================================
// Telemetry Snapshot - Latest values for a single robot
// ============================================================

/**
 * TelemetrySnapshot holds the latest telemetry data from a robot.
 *
 * This includes:
 * - JointState: Joint positions, velocities, efforts
 * - Odometry: Robot pose and velocity
 * - Transforms: TF data for relevant frames
 *
 * Each field has an associated timestamp for staleness checking.
 */
struct TelemetrySnapshot {
    // JointState data
    std::optional<sensor_msgs::msg::JointState> joint_state;
    std::string joint_state_topic;  // ROS2 topic name
    std::chrono::steady_clock::time_point last_joint_state_update;

    // Odometry data
    std::optional<nav_msgs::msg::Odometry> odometry;
    std::string odometry_topic;  // ROS2 topic name
    std::chrono::steady_clock::time_point last_odometry_update;

    // Transform data: child_frame_id -> transform
    std::unordered_map<std::string, geometry_msgs::msg::TransformStamped> transforms;
    std::string tf_topic;  // ROS2 topic name (usually "/tf")
    std::chrono::steady_clock::time_point last_tf_update;

    // Pose from odometry (convenience)
    double x{0.0};
    double y{0.0};
    double yaw{0.0};

    // Velocity from odometry (convenience)
    double linear_velocity{0.0};
    double angular_velocity{0.0};

    // Battery state
    float battery_percent{-1.0f};  // -1 = unknown

    // Execution state (set by executor)
    bool is_executing{false};
    std::string current_task_id;
    std::string current_step_id;
    std::string current_action_type;
    float action_progress{0.0f};
    int robot_state{0};  // RobotState enum value

    // Sequence number for change detection
    uint64_t sequence{0};

    // ============================================================
    // Helper Methods
    // ============================================================

    /**
     * Check if snapshot has any data.
     */
    bool has_data() const {
        return joint_state.has_value() ||
               odometry.has_value() ||
               !transforms.empty();
    }

    /**
     * Check if joint state is available and fresh.
     * @param max_age Maximum age in milliseconds
     */
    bool has_fresh_joint_state(int max_age_ms = 1000) const {
        if (!joint_state.has_value()) return false;
        auto age = std::chrono::steady_clock::now() - last_joint_state_update;
        return age < std::chrono::milliseconds(max_age_ms);
    }

    /**
     * Check if odometry is available and fresh.
     * @param max_age Maximum age in milliseconds
     */
    bool has_fresh_odometry(int max_age_ms = 1000) const {
        if (!odometry.has_value()) return false;
        auto age = std::chrono::steady_clock::now() - last_odometry_update;
        return age < std::chrono::milliseconds(max_age_ms);
    }

    /**
     * Check if transforms are available and fresh.
     * @param max_age Maximum age in milliseconds
     */
    bool has_fresh_transforms(int max_age_ms = 1000) const {
        if (transforms.empty()) return false;
        auto age = std::chrono::steady_clock::now() - last_tf_update;
        return age < std::chrono::milliseconds(max_age_ms);
    }

    /**
     * Get transform for a specific frame.
     */
    std::optional<geometry_msgs::msg::TransformStamped> get_transform(
        const std::string& child_frame_id
    ) const {
        auto it = transforms.find(child_frame_id);
        if (it != transforms.end()) {
            return it->second;
        }
        return std::nullopt;
    }
};

// ============================================================
// Telemetry Store - Thread-safe storage for all robots
// ============================================================

/**
 * TelemetryStore provides thread-safe access to telemetry snapshots
 * for all robots managed by this agent.
 *
 * Uses TBB concurrent_hash_map for high-performance concurrent access.
 */
class TelemetryStore {
public:
    using SnapshotMap = tbb::concurrent_hash_map<std::string, TelemetrySnapshot>;

    /**
     * Get snapshot for a robot.
     * @param robot_id Robot identifier
     * @return Snapshot if exists, nullopt otherwise
     */
    std::optional<TelemetrySnapshot> get(const std::string& robot_id) const {
        SnapshotMap::const_accessor accessor;
        if (snapshots_.find(accessor, robot_id)) {
            return accessor->second;
        }
        return std::nullopt;
    }

    /**
     * Update snapshot with a function.
     * Creates snapshot if it doesn't exist.
     * @param robot_id Robot identifier
     * @param updater Function to update the snapshot
     */
    template <typename Func>
    void update(const std::string& robot_id, Func&& updater) {
        SnapshotMap::accessor accessor;
        snapshots_.insert(accessor, robot_id);
        updater(accessor->second);
        accessor->second.sequence++;
    }

    /**
     * Set entire snapshot for a robot.
     * @param robot_id Robot identifier
     * @param snapshot New snapshot data
     */
    void set(const std::string& robot_id, const TelemetrySnapshot& snapshot) {
        SnapshotMap::accessor accessor;
        snapshots_.insert(accessor, robot_id);
        accessor->second = snapshot;
        accessor->second.sequence++;
    }

    /**
     * Remove snapshot for a robot.
     * @param robot_id Robot identifier
     * @return true if removed
     */
    bool remove(const std::string& robot_id) {
        return snapshots_.erase(robot_id);
    }

    /**
     * Rename a robot (change the key for its snapshot).
     * Used when server assigns a new ID after registration.
     * @param old_id Previous robot ID
     * @param new_id New robot ID from server
     * @return true if renamed successfully
     */
    bool rename(const std::string& old_id, const std::string& new_id) {
        SnapshotMap::accessor old_accessor;
        if (!snapshots_.find(old_accessor, old_id)) {
            return false;  // Old ID not found
        }

        // Check if new ID already exists
        SnapshotMap::const_accessor check_accessor;
        if (snapshots_.find(check_accessor, new_id)) {
            return false;  // New ID already exists
        }

        // Copy snapshot and insert with new key
        TelemetrySnapshot snapshot = old_accessor->second;
        old_accessor.release();

        snapshots_.erase(old_id);

        SnapshotMap::accessor new_accessor;
        snapshots_.insert(new_accessor, new_id);
        new_accessor->second = std::move(snapshot);

        return true;
    }

    /**
     * Check if robot has a snapshot.
     */
    bool has(const std::string& robot_id) const {
        SnapshotMap::const_accessor accessor;
        return snapshots_.find(accessor, robot_id);
    }

    /**
     * Get all robot IDs with snapshots.
     */
    std::vector<std::string> get_robot_ids() const {
        std::vector<std::string> ids;
        for (auto it = snapshots_.begin(); it != snapshots_.end(); ++it) {
            ids.push_back(it->first);
        }
        return ids;
    }

    /**
     * Get number of robots.
     */
    size_t size() const {
        return snapshots_.size();
    }

    /**
     * Clear all snapshots.
     */
    void clear() {
        snapshots_.clear();
    }

    /**
     * Apply a function to all snapshots (read-only).
     */
    template <typename Func>
    void for_each(Func&& func) const {
        for (auto it = snapshots_.begin(); it != snapshots_.end(); ++it) {
            func(it->first, it->second);
        }
    }

    /**
     * Evict stale entries that haven't been updated within the given duration.
     * @param max_age Maximum age before eviction
     * @return Number of entries evicted
     */
    size_t evict_stale(std::chrono::seconds max_age = std::chrono::seconds(300)) {
        auto now = std::chrono::steady_clock::now();
        std::vector<std::string> stale_ids;

        // Find stale entries
        for (auto it = snapshots_.begin(); it != snapshots_.end(); ++it) {
            const auto& snapshot = it->second;
            // Check if all update timestamps are stale
            bool all_stale = true;

            if (snapshot.joint_state.has_value()) {
                if (now - snapshot.last_joint_state_update < max_age) {
                    all_stale = false;
                }
            }
            if (snapshot.odometry.has_value()) {
                if (now - snapshot.last_odometry_update < max_age) {
                    all_stale = false;
                }
            }
            if (!snapshot.transforms.empty()) {
                if (now - snapshot.last_tf_update < max_age) {
                    all_stale = false;
                }
            }

            // If snapshot has no data or all data is stale, mark for eviction
            if (!snapshot.has_data() || all_stale) {
                // Only evict if sequence hasn't changed recently (not actively used)
                stale_ids.push_back(it->first);
            }
        }

        // Remove stale entries
        for (const auto& id : stale_ids) {
            snapshots_.erase(id);
        }

        return stale_ids.size();
    }

private:
    SnapshotMap snapshots_;
};

}  // namespace telemetry
}  // namespace robot_agent
