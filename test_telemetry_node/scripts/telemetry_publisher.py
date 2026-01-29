#!/usr/bin/env python3
"""
Test Telemetry Publisher Node

Publishes test telemetry data for:
- JointState (sensor_msgs/msg/JointState)
- Odometry (nav_msgs/msg/Odometry)
- TF transforms (tf2_msgs/msg/TFMessage)

Usage:
  ros2 run test_telemetry_node telemetry_publisher.py --ros-args -p robot_namespace:=/robot_001
"""

import math
import rclpy
from rclpy.node import Node
from rclpy.qos import QoSProfile, ReliabilityPolicy, DurabilityPolicy

from sensor_msgs.msg import JointState
from nav_msgs.msg import Odometry
from geometry_msgs.msg import TransformStamped
from tf2_msgs.msg import TFMessage
from builtin_interfaces.msg import Time


class TelemetryPublisher(Node):
    def __init__(self):
        super().__init__('telemetry_publisher')

        # Parameters
        self.declare_parameter('robot_namespace', '/robot_001')
        self.declare_parameter('joint_names', ['joint_1', 'joint_2', 'joint_3', 'joint_4', 'joint_5', 'joint_6'])
        self.declare_parameter('publish_rate', 10.0)  # Hz

        self.robot_ns = self.get_parameter('robot_namespace').value
        self.joint_names = self.get_parameter('joint_names').value
        self.publish_rate = self.get_parameter('publish_rate').value

        # Clean namespace
        if not self.robot_ns.startswith('/'):
            self.robot_ns = '/' + self.robot_ns

        self.get_logger().info(f'Starting telemetry publisher for namespace: {self.robot_ns}')

        # QoS for reliable publishing
        qos = QoSProfile(
            reliability=ReliabilityPolicy.RELIABLE,
            durability=DurabilityPolicy.VOLATILE,
            depth=10
        )

        # Publishers
        self.joint_state_pub = self.create_publisher(
            JointState,
            f'{self.robot_ns}/joint_states',
            qos
        )

        self.odom_pub = self.create_publisher(
            Odometry,
            f'{self.robot_ns}/odom',
            qos
        )

        # TF publisher (global /tf topic)
        tf_qos = QoSProfile(
            reliability=ReliabilityPolicy.RELIABLE,
            durability=DurabilityPolicy.VOLATILE,
            depth=100
        )
        self.tf_pub = self.create_publisher(
            TFMessage,
            '/tf',
            tf_qos
        )

        # State variables for simulation
        self.time_counter = 0.0
        self.odom_x = 0.0
        self.odom_y = 0.0
        self.odom_theta = 0.0

        # Timer
        period = 1.0 / self.publish_rate
        self.timer = self.create_timer(period, self.publish_telemetry)

        self.get_logger().info(f'Publishing at {self.publish_rate} Hz')
        self.get_logger().info(f'JointState topic: {self.robot_ns}/joint_states')
        self.get_logger().info(f'Odometry topic: {self.robot_ns}/odom')
        self.get_logger().info(f'TF topic: /tf')

    def get_current_time(self) -> Time:
        now = self.get_clock().now()
        time_msg = Time()
        time_msg.sec = int(now.nanoseconds // 1_000_000_000)
        time_msg.nanosec = int(now.nanoseconds % 1_000_000_000)
        return time_msg

    def publish_telemetry(self):
        current_time = self.get_current_time()
        self.time_counter += 1.0 / self.publish_rate

        # Publish JointState
        self.publish_joint_state(current_time)

        # Publish Odometry
        self.publish_odometry(current_time)

        # Publish TF
        self.publish_tf(current_time)

    def publish_joint_state(self, timestamp: Time):
        msg = JointState()
        msg.header.stamp = timestamp
        msg.header.frame_id = f'{self.robot_ns.lstrip("/")}_base_link'

        msg.name = list(self.joint_names)

        # Simulate sinusoidal joint motion
        positions = []
        velocities = []
        efforts = []

        for i, _ in enumerate(self.joint_names):
            # Each joint oscillates with different phase
            phase = i * 0.5
            pos = math.sin(self.time_counter + phase) * 1.57  # +/- 90 degrees
            vel = math.cos(self.time_counter + phase) * 1.57  # derivative
            eff = math.sin(self.time_counter * 0.5 + phase) * 10.0  # effort

            positions.append(pos)
            velocities.append(vel)
            efforts.append(eff)

        msg.position = positions
        msg.velocity = velocities
        msg.effort = efforts

        self.joint_state_pub.publish(msg)

    def publish_odometry(self, timestamp: Time):
        msg = Odometry()
        msg.header.stamp = timestamp
        msg.header.frame_id = 'odom'
        msg.child_frame_id = f'{self.robot_ns.lstrip("/")}_base_link'

        # Simulate circular motion
        radius = 2.0
        angular_speed = 0.1

        self.odom_theta = self.time_counter * angular_speed
        self.odom_x = radius * math.cos(self.odom_theta)
        self.odom_y = radius * math.sin(self.odom_theta)

        # Position
        msg.pose.pose.position.x = self.odom_x
        msg.pose.pose.position.y = self.odom_y
        msg.pose.pose.position.z = 0.0

        # Orientation (quaternion from yaw)
        msg.pose.pose.orientation.x = 0.0
        msg.pose.pose.orientation.y = 0.0
        msg.pose.pose.orientation.z = math.sin(self.odom_theta / 2)
        msg.pose.pose.orientation.w = math.cos(self.odom_theta / 2)

        # Velocity
        linear_speed = radius * angular_speed
        msg.twist.twist.linear.x = -linear_speed * math.sin(self.odom_theta)
        msg.twist.twist.linear.y = linear_speed * math.cos(self.odom_theta)
        msg.twist.twist.linear.z = 0.0
        msg.twist.twist.angular.x = 0.0
        msg.twist.twist.angular.y = 0.0
        msg.twist.twist.angular.z = angular_speed

        self.odom_pub.publish(msg)

    def publish_tf(self, timestamp: Time):
        tf_msg = TFMessage()

        # Transform: odom -> base_link
        t1 = TransformStamped()
        t1.header.stamp = timestamp
        t1.header.frame_id = 'odom'
        t1.child_frame_id = f'{self.robot_ns.lstrip("/")}_base_link'
        t1.transform.translation.x = self.odom_x
        t1.transform.translation.y = self.odom_y
        t1.transform.translation.z = 0.0
        t1.transform.rotation.x = 0.0
        t1.transform.rotation.y = 0.0
        t1.transform.rotation.z = math.sin(self.odom_theta / 2)
        t1.transform.rotation.w = math.cos(self.odom_theta / 2)
        tf_msg.transforms.append(t1)

        # Transform: base_link -> tool0 (end effector)
        t2 = TransformStamped()
        t2.header.stamp = timestamp
        t2.header.frame_id = f'{self.robot_ns.lstrip("/")}_base_link'
        t2.child_frame_id = f'{self.robot_ns.lstrip("/")}_tool0'
        t2.transform.translation.x = 0.5
        t2.transform.translation.y = 0.0
        t2.transform.translation.z = 0.8
        # Small oscillation on tool
        tool_angle = math.sin(self.time_counter * 2) * 0.1
        t2.transform.rotation.x = 0.0
        t2.transform.rotation.y = math.sin(tool_angle / 2)
        t2.transform.rotation.z = 0.0
        t2.transform.rotation.w = math.cos(tool_angle / 2)
        tf_msg.transforms.append(t2)

        self.tf_pub.publish(tf_msg)


def main(args=None):
    rclpy.init(args=args)

    node = TelemetryPublisher()

    try:
        rclpy.spin(node)
    except KeyboardInterrupt:
        pass
    finally:
        node.destroy_node()
        rclpy.shutdown()


if __name__ == '__main__':
    main()
