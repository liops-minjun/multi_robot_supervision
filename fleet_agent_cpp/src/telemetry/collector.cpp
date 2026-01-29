// Copyright 2026 Multi-Robot Supervision System
// Per-robot telemetry collector implementation

#include "fleet_agent/telemetry/collector.hpp"
#include "fleet_agent/core/logger.hpp"

#include <cmath>

namespace fleet_agent {
namespace telemetry {

RobotTelemetryCollector::RobotTelemetryCollector(
    rclcpp::Node::SharedPtr node,
    const std::string& robot_id,
    const std::string& ros_namespace,
    const TelemetryConfig& telemetry_config,
    TelemetryStore& store
)
    : node_(node)
    , robot_id_(robot_id)
    , ros_namespace_(ros_namespace)
    , config_(telemetry_config)
    , store_(store)
    , pose_limiter_(telemetry_config.pose_rate_hz)
    , joint_state_limiter_(telemetry_config.joint_state_rate_hz)
    , battery_limiter_(telemetry_config.battery_rate_hz)
    , velocity_limiter_(telemetry_config.velocity_rate_hz) {
    // Initialize snapshot for this robot
    store_.update(robot_id_, [](TelemetrySnapshot&) {});
}

RobotTelemetryCollector::~RobotTelemetryCollector() {
    stop();
}

void RobotTelemetryCollector::start() {
    if (active_.exchange(true)) {
        return;  // Already running
    }

    LOG_INFO("[TelemetryCollector] Starting collector for robot: {}", robot_id_);

    // Start discovery thread
    discovery_running_ = true;
    discovery_thread_ = std::thread(&RobotTelemetryCollector::discovery_loop, this);

    // Initial topic discovery
    discover_topics();

    // Always subscribe to TF if enabled
    if (config_.subscribe_tf && !has_tf_sub_) {
        subscribe_tf();
    }
}

void RobotTelemetryCollector::stop() {
    if (!active_.exchange(false)) {
        return;  // Already stopped
    }

    LOG_INFO("[TelemetryCollector] Stopping collector for robot: {}", robot_id_);

    // Stop discovery thread
    discovery_running_ = false;
    if (discovery_thread_.joinable()) {
        discovery_thread_.join();
    }

    // Reset subscriptions
    joint_state_subs_.clear();
    odom_subs_.clear();
    battery_sub_.reset();
    vel_sub_.reset();
    tf_sub_.reset();
    tf_static_sub_.reset();

    // Clear subscribed topics tracking
    {
        std::lock_guard<std::mutex> lock(subscribed_topics_mutex_);
        subscribed_topics_.clear();
    }
    has_tf_sub_ = false;
}

TelemetrySnapshot RobotTelemetryCollector::get_snapshot() const {
    auto snapshot = store_.get(robot_id_);
    return snapshot.value_or(TelemetrySnapshot{});
}

bool RobotTelemetryCollector::has_changed() const {
    return changed_.load();
}

void RobotTelemetryCollector::reset_changed_flag() {
    changed_ = false;
}

void RobotTelemetryCollector::set_robot_id(const std::string& new_robot_id) {
    if (new_robot_id == robot_id_) {
        return;  // No change needed
    }

    std::string old_id = robot_id_;
    robot_id_ = new_robot_id;

    // Rename the store entry so future reads/writes use the new ID
    if (store_.rename(old_id, new_robot_id)) {
        LOG_INFO("[TelemetryCollector] Robot ID updated: {} -> {}", old_id, new_robot_id);
    } else {
        // If rename failed (e.g., old entry doesn't exist), create new entry
        store_.update(new_robot_id, [](TelemetrySnapshot&) {});
        LOG_INFO("[TelemetryCollector] Created new telemetry entry for {}", new_robot_id);
    }
}

void RobotTelemetryCollector::set_execution_state(
    bool is_executing,
    const std::string& task_id,
    const std::string& step_id,
    const std::string& action_type
) {
    update_store([&](TelemetrySnapshot& s) {
        s.is_executing = is_executing;
        s.current_task_id = task_id;
        s.current_step_id = step_id;
        s.current_action_type = action_type;
        if (!is_executing) {
            s.action_progress = 0.0f;
        }
    });
}

void RobotTelemetryCollector::set_action_progress(float progress) {
    update_store([progress](TelemetrySnapshot& s) {
        s.action_progress = progress;
    });
}

void RobotTelemetryCollector::set_robot_state(int state) {
    update_store([state](TelemetrySnapshot& s) {
        s.robot_state = state;
    });
}

// ============================================================
// Topic Discovery
// ============================================================

void RobotTelemetryCollector::discovery_loop() {
    while (discovery_running_) {
        discover_topics();

        // Sleep with interruptible check
        for (int i = 0; i < 50 && discovery_running_; ++i) {
            std::this_thread::sleep_for(std::chrono::milliseconds(100));
        }
    }
}

void RobotTelemetryCollector::discover_topics() {
    if (!node_) return;

    auto topics = node_->get_topic_names_and_types();

    // Log discovery info on first run (per-instance, not global static)
    if (!first_discovery_logged_) {
        first_discovery_logged_ = true;
        LOG_INFO("[TelemetryCollector] Scanning {} topics for telemetry sources (no namespace filter)...",
                 topics.size());
    }

    for (const auto& [topic_name, types] : topics) {
        // Check if already subscribed to this topic
        {
            std::lock_guard<std::mutex> lock(subscribed_topics_mutex_);
            if (subscribed_topics_.count(topic_name) > 0) {
                continue;  // Already subscribed
            }
        }

        for (const auto& type : types) {
            // JointState discovery - subscribe to ALL JointState topics
            if (type == "sensor_msgs/msg/JointState") {
                LOG_INFO("[TelemetryCollector] Discovered JointState topic: {}", topic_name);
                subscribe_joint_state(topic_name);
            }
            // Odometry discovery - subscribe to ALL Odometry topics
            else if (type == "nav_msgs/msg/Odometry") {
                LOG_INFO("[TelemetryCollector] Discovered Odometry topic: {}", topic_name);
                subscribe_odometry(topic_name);
            }
        }
    }
}

// ============================================================
// Subscription Creation
// ============================================================

void RobotTelemetryCollector::subscribe_joint_state(const std::string& topic) {
    // Mark as subscribed first (thread-safe)
    {
        std::lock_guard<std::mutex> lock(subscribed_topics_mutex_);
        if (subscribed_topics_.count(topic) > 0) return;
        subscribed_topics_.insert(topic);
    }

    // Use SensorDataQoS for compatibility with both reliable and best_effort publishers
    auto sub = node_->create_subscription<sensor_msgs::msg::JointState>(
        topic, rclcpp::SensorDataQoS(),
        [this, topic](const sensor_msgs::msg::JointState::SharedPtr msg) {
            on_joint_state(msg, topic);
        }
    );
    joint_state_subs_.push_back(sub);

    LOG_INFO("[TelemetryCollector] Subscribed to JointState: {}", topic);
}

void RobotTelemetryCollector::subscribe_odometry(const std::string& topic) {
    // Mark as subscribed first (thread-safe)
    {
        std::lock_guard<std::mutex> lock(subscribed_topics_mutex_);
        if (subscribed_topics_.count(topic) > 0) return;
        subscribed_topics_.insert(topic);
    }

    // Use SensorDataQoS for compatibility with both reliable and best_effort publishers
    auto sub = node_->create_subscription<nav_msgs::msg::Odometry>(
        topic, rclcpp::SensorDataQoS(),
        [this, topic](const nav_msgs::msg::Odometry::SharedPtr msg) {
            on_odom(msg, topic);
        }
    );
    odom_subs_.push_back(sub);

    LOG_INFO("[TelemetryCollector] Subscribed to Odometry: {}", topic);
}

void RobotTelemetryCollector::subscribe_tf() {
    if (has_tf_sub_) return;

    // Subscribe to /tf with best_effort for compatibility
    // TF publishers typically use reliable, but we use best_effort to ensure compatibility
    auto tf_qos = rclcpp::QoS(100).best_effort().durability_volatile();
    tf_sub_ = node_->create_subscription<tf2_msgs::msg::TFMessage>(
        "/tf", tf_qos,
        [this](const tf2_msgs::msg::TFMessage::SharedPtr msg) {
            on_tf(msg);
        }
    );

    // Subscribe to /tf_static - use volatile durability for intraprocess compatibility
    // Note: transient_local is incompatible with intraprocess communication
    auto tf_static_qos = rclcpp::QoS(100).reliable().durability_volatile();
    tf_static_sub_ = node_->create_subscription<tf2_msgs::msg::TFMessage>(
        "/tf_static", tf_static_qos,
        [this](const tf2_msgs::msg::TFMessage::SharedPtr msg) {
            on_tf(msg);
        }
    );

    // Update store with topic name
    update_store([&](TelemetrySnapshot& s) {
        s.tf_topic = "/tf";
    });

    has_tf_sub_ = true;
    LOG_INFO("[TelemetryCollector] Subscribed to TF topics");
}

// ============================================================
// Callbacks
// ============================================================

void RobotTelemetryCollector::on_joint_state(
    const sensor_msgs::msg::JointState::SharedPtr msg,
    const std::string& topic
) {
    // Check if collector is still active (prevents race with stop())
    if (!active_.load()) return;
    if (!joint_state_limiter_.should_process()) return;

    // Log first joint state received (per-instance, not global static)
    if (!first_joint_state_logged_) {
        first_joint_state_logged_ = true;
        LOG_INFO("[TelemetryCollector] First JointState received from {}: {} joints",
                 topic, msg->name.size());
    }

    update_store([&](TelemetrySnapshot& s) {
        s.joint_state = *msg;
        s.joint_state_topic = topic;
        s.last_joint_state_update = std::chrono::steady_clock::now();
    });

    changed_ = true;
}

void RobotTelemetryCollector::on_odom(
    const nav_msgs::msg::Odometry::SharedPtr msg,
    const std::string& topic
) {
    // Check if collector is still active (prevents race with stop())
    if (!active_.load()) return;
    if (!pose_limiter_.should_process()) return;

    // Log first odometry received (per-instance, not global static)
    if (!first_odom_logged_) {
        first_odom_logged_ = true;
        LOG_INFO("[TelemetryCollector] First Odometry received from {}: frame={} child={}",
                 topic, msg->header.frame_id, msg->child_frame_id);
    }

    update_store([&](TelemetrySnapshot& s) {
        s.odometry = *msg;
        s.odometry_topic = topic;
        s.last_odometry_update = std::chrono::steady_clock::now();

        // Extract convenience values
        s.x = msg->pose.pose.position.x;
        s.y = msg->pose.pose.position.y;
        s.yaw = quaternion_to_yaw(
            msg->pose.pose.orientation.x,
            msg->pose.pose.orientation.y,
            msg->pose.pose.orientation.z,
            msg->pose.pose.orientation.w
        );
        s.linear_velocity = msg->twist.twist.linear.x;
        s.angular_velocity = msg->twist.twist.angular.z;
    });

    changed_ = true;
}

void RobotTelemetryCollector::on_battery(
    const sensor_msgs::msg::BatteryState::SharedPtr msg
) {
    // Check if collector is still active (prevents race with stop())
    if (!active_.load()) return;
    if (!battery_limiter_.should_process()) return;

    update_store([&](TelemetrySnapshot& s) {
        s.battery_percent = msg->percentage * 100.0f;
    });

    changed_ = true;
}

void RobotTelemetryCollector::on_velocity(
    const geometry_msgs::msg::Twist::SharedPtr msg
) {
    // Check if collector is still active (prevents race with stop())
    if (!active_.load()) return;
    if (!velocity_limiter_.should_process()) return;

    update_store([&](TelemetrySnapshot& s) {
        s.linear_velocity = msg->linear.x;
        s.angular_velocity = msg->angular.z;
    });

    changed_ = true;
}

void RobotTelemetryCollector::on_tf(
    const tf2_msgs::msg::TFMessage::SharedPtr msg
) {
    // Check if collector is still active (prevents race with stop())
    if (!active_.load()) return;

    update_store([&](TelemetrySnapshot& s) {
        for (const auto& transform : msg->transforms) {
            // Filter by namespace if configured
            bool should_include = config_.tf_frames.empty();
            if (!should_include) {
                for (const auto& frame : config_.tf_frames) {
                    if (transform.child_frame_id.find(frame) != std::string::npos ||
                        transform.header.frame_id.find(frame) != std::string::npos) {
                        should_include = true;
                        break;
                    }
                }
            }

            // Also filter by robot namespace
            if (should_include && !ros_namespace_.empty()) {
                // Include if frame matches namespace or is a common frame
                bool ns_match = transform.child_frame_id.find(ros_namespace_) != std::string::npos ||
                                transform.child_frame_id == "odom" ||
                                transform.child_frame_id == "map" ||
                                transform.child_frame_id == "base_link" ||
                                transform.child_frame_id == "base_footprint";
                should_include = ns_match;
            }

            if (should_include) {
                s.transforms[transform.child_frame_id] = transform;
            }
        }
        s.last_tf_update = std::chrono::steady_clock::now();
    });

    changed_ = true;
}

// ============================================================
// Helpers
// ============================================================

void RobotTelemetryCollector::update_store(
    std::function<void(TelemetrySnapshot&)> updater
) {
    store_.update(robot_id_, std::move(updater));
}

std::string RobotTelemetryCollector::get_topic(const std::string& base_topic) const {
    if (ros_namespace_.empty() || ros_namespace_ == "/") {
        return "/" + base_topic;
    }
    if (ros_namespace_.back() == '/') {
        return ros_namespace_ + base_topic;
    }
    return ros_namespace_ + "/" + base_topic;
}

double RobotTelemetryCollector::quaternion_to_yaw(double x, double y, double z, double w) {
    // Convert quaternion to yaw (rotation around Z axis)
    double siny_cosp = 2.0 * (w * z + x * y);
    double cosy_cosp = 1.0 - 2.0 * (y * y + z * z);
    return std::atan2(siny_cosp, cosy_cosp);
}

}  // namespace telemetry
}  // namespace fleet_agent
