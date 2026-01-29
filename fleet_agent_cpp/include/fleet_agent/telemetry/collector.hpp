// Copyright 2026 Multi-Robot Supervision System
// Per-robot telemetry collector with topic discovery

#pragma once

#include "fleet_agent/core/types.hpp"
#include "fleet_agent/core/config.hpp"
#include "fleet_agent/telemetry/snapshot.hpp"

#include <atomic>
#include <chrono>
#include <functional>
#include <memory>
#include <mutex>
#include <string>
#include <thread>
#include <unordered_set>
#include <vector>

#include <rclcpp/rclcpp.hpp>
#include <nav_msgs/msg/odometry.hpp>
#include <sensor_msgs/msg/battery_state.hpp>
#include <sensor_msgs/msg/joint_state.hpp>
#include <geometry_msgs/msg/twist.hpp>
#include <tf2_msgs/msg/tf_message.hpp>

namespace fleet_agent {
namespace telemetry {

/**
 * RobotTelemetryCollector - Collects telemetry from ROS2 topics for a single robot.
 *
 * Features:
 * - Dynamic topic discovery for JointState, Odometry, TF
 * - Rate limiting to reduce CPU usage on high-frequency topics
 * - Thread-safe updates to shared TelemetryStore
 * - Change detection for delta encoding
 *
 * Usage:
 *   TelemetryStore store;
 *   RobotTelemetryCollector collector(node, "robot_001", "/robot_001", config, store);
 *   collector.start();
 */
class RobotTelemetryCollector {
public:
    /**
     * Constructor.
     *
     * @param node ROS2 node for creating subscriptions
     * @param robot_id Robot identifier
     * @param ros_namespace Robot's ROS2 namespace (e.g., "/robot_001")
     * @param telemetry_config Telemetry topic configuration
     * @param store Shared telemetry store reference
     */
    RobotTelemetryCollector(
        rclcpp::Node::SharedPtr node,
        const std::string& robot_id,
        const std::string& ros_namespace,
        const TelemetryConfig& telemetry_config,
        TelemetryStore& store
    );

    ~RobotTelemetryCollector();

    // ============================================================
    // Lifecycle
    // ============================================================

    /**
     * Start collecting telemetry.
     * Creates ROS2 subscriptions and starts discovery thread.
     */
    void start();

    /**
     * Stop collecting telemetry.
     * Destroys ROS2 subscriptions and stops discovery.
     */
    void stop();

    /**
     * Check if collector is active.
     */
    bool is_active() const { return active_.load(); }

    // ============================================================
    // State Access
    // ============================================================

    /**
     * Get a snapshot of current telemetry.
     * Thread-safe copy of the latest data.
     */
    TelemetrySnapshot get_snapshot() const;

    /**
     * Check if telemetry has changed since last check.
     */
    bool has_changed() const;

    /**
     * Reset the change flag.
     */
    void reset_changed_flag();

    /**
     * Get robot ID.
     */
    const std::string& robot_id() const { return robot_id_; }

    /**
     * Update robot ID (called when server assigns new ID).
     * This renames the telemetry store entry and updates future writes.
     */
    void set_robot_id(const std::string& new_robot_id);

    // ============================================================
    // State Modification (from executor)
    // ============================================================

    /**
     * Set execution state.
     * Called by executor when action starts/completes.
     */
    void set_execution_state(
        bool is_executing,
        const std::string& task_id = "",
        const std::string& step_id = "",
        const std::string& action_type = ""
    );

    /**
     * Set action progress.
     * Called by executor on feedback.
     */
    void set_action_progress(float progress);

    /**
     * Set robot state.
     */
    void set_robot_state(int state);

private:
    rclcpp::Node::SharedPtr node_;
    std::string robot_id_;
    std::string ros_namespace_;
    TelemetryConfig config_;
    TelemetryStore& store_;

    std::atomic<bool> active_{false};
    std::atomic<bool> changed_{false};

    // ============================================================
    // Topic Discovery
    // ============================================================

    std::atomic<bool> discovery_running_{false};
    std::thread discovery_thread_;
    static constexpr auto kDiscoveryInterval = std::chrono::seconds(5);

    void discovery_loop();
    void discover_topics();

    // Track subscribed topics (topic_name -> true)
    std::unordered_set<std::string> subscribed_topics_;
    std::mutex subscribed_topics_mutex_;
    std::atomic<bool> has_tf_sub_{false};

    // First message logging flags (per-instance, not static)
    bool first_discovery_logged_{false};
    bool first_joint_state_logged_{false};
    bool first_odom_logged_{false};

    // ============================================================
    // Rate Limiting
    // ============================================================

    struct RateLimiter {
        std::chrono::nanoseconds min_interval;
        std::chrono::steady_clock::time_point last_time{};

        static constexpr double kMinRateHz = 0.001;  // 1 message per 1000 seconds minimum
        static constexpr double kMaxRateHz = 10000.0;  // 10kHz maximum

        explicit RateLimiter(double rate_hz) {
            // Clamp rate_hz to prevent overflow and unreasonable values
            if (rate_hz <= 0 || rate_hz < kMinRateHz) {
                min_interval = std::chrono::nanoseconds(0);  // No rate limiting
            } else if (rate_hz > kMaxRateHz) {
                min_interval = std::chrono::nanoseconds(
                    static_cast<int64_t>(1e9 / kMaxRateHz));
            } else {
                min_interval = std::chrono::nanoseconds(
                    static_cast<int64_t>(1e9 / rate_hz));
            }
        }

        bool should_process() {
            auto now = std::chrono::steady_clock::now();
            if (now - last_time >= min_interval) {
                last_time = now;
                return true;
            }
            return false;
        }
    };

    RateLimiter pose_limiter_;
    RateLimiter joint_state_limiter_;
    RateLimiter battery_limiter_;
    RateLimiter velocity_limiter_;

    // ============================================================
    // ROS2 Subscriptions (multiple topics per type)
    // ============================================================

    std::vector<rclcpp::Subscription<nav_msgs::msg::Odometry>::SharedPtr> odom_subs_;
    std::vector<rclcpp::Subscription<sensor_msgs::msg::JointState>::SharedPtr> joint_state_subs_;
    rclcpp::Subscription<sensor_msgs::msg::BatteryState>::SharedPtr battery_sub_;
    rclcpp::Subscription<geometry_msgs::msg::Twist>::SharedPtr vel_sub_;
    rclcpp::Subscription<tf2_msgs::msg::TFMessage>::SharedPtr tf_sub_;
    rclcpp::Subscription<tf2_msgs::msg::TFMessage>::SharedPtr tf_static_sub_;

    // ============================================================
    // Subscription Creation
    // ============================================================

    void subscribe_joint_state(const std::string& topic);
    void subscribe_odometry(const std::string& topic);
    void subscribe_tf();

    // ============================================================
    // Callbacks
    // ============================================================

    void on_odom(const nav_msgs::msg::Odometry::SharedPtr msg, const std::string& topic);
    void on_battery(const sensor_msgs::msg::BatteryState::SharedPtr msg);
    void on_velocity(const geometry_msgs::msg::Twist::SharedPtr msg);
    void on_joint_state(const sensor_msgs::msg::JointState::SharedPtr msg, const std::string& topic);
    void on_tf(const tf2_msgs::msg::TFMessage::SharedPtr msg);

    // ============================================================
    // Helpers
    // ============================================================

    /**
     * Update store with thread-safe accessor.
     */
    void update_store(std::function<void(TelemetrySnapshot&)> updater);

    /**
     * Get full topic name with namespace.
     */
    std::string get_topic(const std::string& base_topic) const;

    /**
     * Convert quaternion to yaw angle.
     */
    static double quaternion_to_yaw(double x, double y, double z, double w);
};

}  // namespace telemetry
}  // namespace fleet_agent
