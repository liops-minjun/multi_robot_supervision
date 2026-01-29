// Copyright 2026 Multi-Robot Supervision System
// Telemetry aggregator implementation

#include "fleet_agent/telemetry/aggregator.hpp"
#include "fleet_agent/core/logger.hpp"

#include "fleet/v1/service.pb.h"
#include "fleet/v1/common.pb.h"

namespace fleet_agent {
namespace telemetry {

TelemetryAggregator::TelemetryAggregator(
    const std::string& agent_id,
    TelemetryStore& store,
    QuicOutboundQueue& quic_queue,
    std::chrono::milliseconds interval
)
    : agent_id_(agent_id)
    , store_(store)
    , quic_queue_(quic_queue)
    , interval_(interval) {
}

TelemetryAggregator::~TelemetryAggregator() {
    stop();
}

void TelemetryAggregator::start() {
    if (running_.exchange(true)) {
        return;  // Already running
    }

    LOG_INFO("[TelemetryAggregator] Starting for agent: {}", agent_id_);
    aggregator_thread_ = std::thread(&TelemetryAggregator::aggregation_loop, this);
}

void TelemetryAggregator::stop() {
    if (!running_.exchange(false)) {
        return;  // Already stopped
    }

    LOG_INFO("[TelemetryAggregator] Stopping for agent: {}", agent_id_);
    if (aggregator_thread_.joinable()) {
        aggregator_thread_.join();
    }
}

void TelemetryAggregator::add_robot(const std::string& robot_id) {
    std::lock_guard<std::mutex> lock(robots_mutex_);
    if (std::find(robot_ids_.begin(), robot_ids_.end(), robot_id) == robot_ids_.end()) {
        robot_ids_.push_back(robot_id);
        LOG_INFO("[TelemetryAggregator] Added robot: {}", robot_id);
    }
}

void TelemetryAggregator::remove_robot(const std::string& robot_id) {
    std::lock_guard<std::mutex> lock(robots_mutex_);
    auto it = std::find(robot_ids_.begin(), robot_ids_.end(), robot_id);
    if (it != robot_ids_.end()) {
        robot_ids_.erase(it);
        last_sequences_.erase(robot_id);
        last_sent_.erase(robot_id);
        LOG_INFO("[TelemetryAggregator] Removed robot: {}", robot_id);
    }
}

bool TelemetryAggregator::rename_robot(const std::string& old_id, const std::string& new_id) {
    std::lock_guard<std::mutex> lock(robots_mutex_);

    auto it = std::find(robot_ids_.begin(), robot_ids_.end(), old_id);
    if (it == robot_ids_.end()) {
        LOG_WARN("[TelemetryAggregator] Cannot rename robot {} to {} - old ID not found", old_id, new_id);
        return false;
    }

    // Check if new ID already exists
    if (std::find(robot_ids_.begin(), robot_ids_.end(), new_id) != robot_ids_.end()) {
        LOG_WARN("[TelemetryAggregator] Cannot rename robot {} to {} - new ID already exists", old_id, new_id);
        return false;
    }

    // Update the robot ID in the list
    *it = new_id;

    // Update tracking maps
    if (last_sequences_.count(old_id)) {
        last_sequences_[new_id] = last_sequences_[old_id];
        last_sequences_.erase(old_id);
    }
    if (last_sent_.count(old_id)) {
        last_sent_[new_id] = last_sent_[old_id];
        last_sent_.erase(old_id);
    }

    LOG_INFO("[TelemetryAggregator] Renamed robot {} to {}", old_id, new_id);
    return true;
}

size_t TelemetryAggregator::robot_count() const {
    std::lock_guard<std::mutex> lock(robots_mutex_);
    return robot_ids_.size();
}

void TelemetryAggregator::set_interval(std::chrono::milliseconds interval) {
    interval_ = interval;
}

void TelemetryAggregator::set_delta_enabled(bool enabled) {
    delta_enabled_ = enabled;
}

void TelemetryAggregator::set_telemetry_enabled(bool enabled) {
    telemetry_enabled_ = enabled;
}

void TelemetryAggregator::set_agent_id(const std::string& agent_id) {
    agent_id_ = agent_id;
    LOG_INFO("[TelemetryAggregator] Updated agent ID to: {}", agent_id_);
}

void TelemetryAggregator::aggregation_loop() {
    while (running_) {
        auto start = std::chrono::steady_clock::now();

        // Create and send heartbeat
        auto heartbeat = create_quic_heartbeat();
        if (heartbeat.message) {
            quic_queue_.push(std::move(heartbeat));
        }

        // Sleep for remaining interval
        auto elapsed = std::chrono::steady_clock::now() - start;
        auto sleep_time = interval_ - std::chrono::duration_cast<std::chrono::milliseconds>(elapsed);
        if (sleep_time > std::chrono::milliseconds(0)) {
            std::this_thread::sleep_for(sleep_time);
        }
    }
}

OutboundMessage TelemetryAggregator::create_quic_heartbeat() {
    auto agent_msg = std::make_shared<fleet::v1::AgentMessage>();
    agent_msg->set_agent_id(agent_id_);
    agent_msg->set_timestamp_ms(now_ms());

    auto* heartbeat = agent_msg->mutable_heartbeat();
    heartbeat->set_agent_id(agent_id_);

    // Get first robot's state for the agent heartbeat
    // In 1:1 model, agent_id == robot_id
    {
        std::lock_guard<std::mutex> lock(robots_mutex_);
        if (!robot_ids_.empty()) {
            auto snapshot = get_snapshot(robot_ids_[0]);
            if (snapshot.has_value()) {
                heartbeat->set_is_executing(snapshot->is_executing);
                heartbeat->set_current_action(snapshot->current_action_type);
                heartbeat->set_current_task_id(snapshot->current_task_id);
                heartbeat->set_current_step_id(snapshot->current_step_id);

                // Set state based on robot state
                switch (snapshot->robot_state) {
                    case 0: heartbeat->set_state("idle"); break;
                    case 1: heartbeat->set_state("executing"); break;
                    case 2: heartbeat->set_state("waiting"); break;
                    case 3: heartbeat->set_state("error"); break;
                    default: heartbeat->set_state("unknown"); break;
                }

                // Add telemetry if enabled
                if (telemetry_enabled_ && snapshot->has_data()) {
                    auto* telemetry = heartbeat->mutable_telemetry();
                    build_telemetry_payload(robot_ids_[0], *snapshot, telemetry);
                }
            }
        }
    }

    OutboundMessage msg;
    msg.message = agent_msg;
    msg.created_at = std::chrono::steady_clock::now();
    msg.priority = 0;

    return msg;
}

void TelemetryAggregator::build_telemetry_payload(
    const std::string& robot_id,
    const TelemetrySnapshot& snapshot,
    void* pb_payload_ptr
) {
    auto* payload = static_cast<fleet::v1::TelemetryPayload*>(pb_payload_ptr);
    payload->set_robot_id(robot_id);

    auto now_ns = std::chrono::duration_cast<std::chrono::nanoseconds>(
        std::chrono::system_clock::now().time_since_epoch()
    ).count();
    payload->set_collected_at_ns(now_ns);

    // JointState
    if (snapshot.joint_state.has_value()) {
        auto* js = payload->mutable_joint_state();
        const auto& joint_state = *snapshot.joint_state;

        for (const auto& name : joint_state.name) {
            js->add_name(name);
        }
        for (const auto& pos : joint_state.position) {
            js->add_position(pos);
        }
        for (const auto& vel : joint_state.velocity) {
            js->add_velocity(vel);
        }
        for (const auto& eff : joint_state.effort) {
            js->add_effort(eff);
        }

        // Convert ROS timestamp to nanoseconds
        auto stamp_ns = static_cast<int64_t>(joint_state.header.stamp.sec) * 1000000000LL +
                        joint_state.header.stamp.nanosec;
        js->set_timestamp_ns(stamp_ns);

        // Set topic name for visualization
        if (!snapshot.joint_state_topic.empty()) {
            js->set_topic_name(snapshot.joint_state_topic);
        }
    }

    // Odometry
    if (snapshot.odometry.has_value()) {
        auto* odom = payload->mutable_odometry();
        const auto& odometry = *snapshot.odometry;

        odom->set_frame_id(odometry.header.frame_id);
        odom->set_child_frame_id(odometry.child_frame_id);

        // Pose
        auto* pose = odom->mutable_pose();
        auto* position = pose->mutable_position();
        position->set_x(odometry.pose.pose.position.x);
        position->set_y(odometry.pose.pose.position.y);
        position->set_z(odometry.pose.pose.position.z);

        auto* orientation = pose->mutable_orientation();
        orientation->set_x(odometry.pose.pose.orientation.x);
        orientation->set_y(odometry.pose.pose.orientation.y);
        orientation->set_z(odometry.pose.pose.orientation.z);
        orientation->set_w(odometry.pose.pose.orientation.w);

        // Twist
        auto* twist = odom->mutable_twist();
        auto* linear = twist->mutable_linear();
        linear->set_x(odometry.twist.twist.linear.x);
        linear->set_y(odometry.twist.twist.linear.y);
        linear->set_z(odometry.twist.twist.linear.z);

        auto* angular = twist->mutable_angular();
        angular->set_x(odometry.twist.twist.angular.x);
        angular->set_y(odometry.twist.twist.angular.y);
        angular->set_z(odometry.twist.twist.angular.z);

        // Convert ROS timestamp to nanoseconds
        auto stamp_ns = static_cast<int64_t>(odometry.header.stamp.sec) * 1000000000LL +
                        odometry.header.stamp.nanosec;
        odom->set_timestamp_ns(stamp_ns);

        // Set topic name for visualization
        if (!snapshot.odometry_topic.empty()) {
            odom->set_topic_name(snapshot.odometry_topic);
        }
    }

    // Transforms
    for (const auto& [child_frame_id, tf] : snapshot.transforms) {
        auto* transform = payload->add_transforms();
        transform->set_frame_id(tf.header.frame_id);
        transform->set_child_frame_id(tf.child_frame_id);

        auto* translation = transform->mutable_translation();
        translation->set_x(tf.transform.translation.x);
        translation->set_y(tf.transform.translation.y);
        translation->set_z(tf.transform.translation.z);

        auto* rotation = transform->mutable_rotation();
        rotation->set_x(tf.transform.rotation.x);
        rotation->set_y(tf.transform.rotation.y);
        rotation->set_z(tf.transform.rotation.z);
        rotation->set_w(tf.transform.rotation.w);

        // Convert ROS timestamp to nanoseconds
        auto stamp_ns = static_cast<int64_t>(tf.header.stamp.sec) * 1000000000LL +
                        tf.header.stamp.nanosec;
        transform->set_timestamp_ns(stamp_ns);
    }
}

bool TelemetryAggregator::has_changed(
    const std::string& robot_id,
    const TelemetrySnapshot& snapshot
) {
    if (!delta_enabled_) {
        return true;
    }

    auto it = last_sequences_.find(robot_id);
    if (it == last_sequences_.end()) {
        last_sequences_[robot_id] = snapshot.sequence;
        return true;
    }

    if (it->second != snapshot.sequence) {
        it->second = snapshot.sequence;
        return true;
    }

    return false;
}

std::optional<TelemetrySnapshot> TelemetryAggregator::get_snapshot(
    const std::string& robot_id
) {
    return store_.get(robot_id);
}

size_t TelemetryAggregator::cleanup_orphaned_entries() {
    std::lock_guard<std::mutex> lock(robots_mutex_);

    size_t cleaned = 0;

    // Find and remove orphaned entries from last_sequences_
    for (auto it = last_sequences_.begin(); it != last_sequences_.end(); ) {
        if (std::find(robot_ids_.begin(), robot_ids_.end(), it->first) == robot_ids_.end()) {
            it = last_sequences_.erase(it);
            cleaned++;
        } else {
            ++it;
        }
    }

    // Find and remove orphaned entries from last_sent_
    for (auto it = last_sent_.begin(); it != last_sent_.end(); ) {
        if (std::find(robot_ids_.begin(), robot_ids_.end(), it->first) == robot_ids_.end()) {
            it = last_sent_.erase(it);
            cleaned++;
        } else {
            ++it;
        }
    }

    if (cleaned > 0) {
        LOG_DEBUG("[TelemetryAggregator] Cleaned up {} orphaned delta entries", cleaned);
    }

    return cleaned;
}

}  // namespace telemetry
}  // namespace fleet_agent
