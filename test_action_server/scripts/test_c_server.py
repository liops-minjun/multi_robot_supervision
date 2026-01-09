#!/usr/bin/env python3
"""
Test Action Server C

A simple action server that:
- Waits randomly 5-10 seconds
- Returns Success with 90% probability
- Returns Failure with 10% probability

Used for testing Action Graph workflows.
"""

import rclpy
from rclpy.node import Node
from rclpy.action import ActionServer, GoalResponse, CancelResponse
from rclpy.callback_groups import ReentrantCallbackGroup
from rclpy.executors import MultiThreadedExecutor

import random
import time

from test_action_server.action import TestAction


class TestActionServerC(Node):
    def __init__(self):
        super().__init__('test_c_action_server')

        self._action_server = ActionServer(
            self,
            TestAction,
            'test_C_action',
            execute_callback=self.execute_callback,
            goal_callback=self.goal_callback,
            cancel_callback=self.cancel_callback,
            callback_group=ReentrantCallbackGroup()
        )

        self.get_logger().info('Test Action Server C started on /test_C_action')

        # Configuration
        self.min_duration = 5.0   # Minimum execution time (seconds)
        self.max_duration = 10.0  # Maximum execution time (seconds)
        self.success_rate = 0.9   # 90% success rate

    def goal_callback(self, goal_request):
        """Accept or reject a goal request."""
        self.get_logger().info(f'Received goal request: task_name={goal_request.task_name}')
        return GoalResponse.ACCEPT

    def cancel_callback(self, goal_handle):
        """Accept or reject a cancel request."""
        self.get_logger().info('Received cancel request')
        return CancelResponse.ACCEPT

    async def execute_callback(self, goal_handle):
        """Execute the action."""
        self.get_logger().info(f'Executing goal: {goal_handle.request.task_name}')

        # Determine random execution time
        execution_time = random.uniform(self.min_duration, self.max_duration)
        start_time = time.time()

        feedback_msg = TestAction.Feedback()

        # Execute with progress feedback
        while True:
            elapsed = time.time() - start_time
            progress = min(elapsed / execution_time, 1.0)

            # Check for cancellation
            if goal_handle.is_cancel_requested:
                goal_handle.canceled()
                self.get_logger().info('Goal canceled')
                result = TestAction.Result()
                result.success = False
                result.message = 'Action was canceled'
                result.execution_time = elapsed
                return result

            # Send feedback
            feedback_msg.progress = progress
            feedback_msg.status = f'Processing... ({int(progress * 100)}%)'
            feedback_msg.elapsed_time = elapsed
            goal_handle.publish_feedback(feedback_msg)

            # Check if done
            if elapsed >= execution_time:
                break

            # Sleep briefly
            time.sleep(0.1)

        # Determine success or failure
        actual_execution_time = time.time() - start_time
        success = random.random() < self.success_rate

        result = TestAction.Result()
        result.execution_time = actual_execution_time

        if success:
            goal_handle.succeed()
            result.success = True
            result.message = f'Test C completed successfully in {actual_execution_time:.2f}s'
            self.get_logger().info(f'Goal succeeded: {result.message}')
        else:
            goal_handle.abort()
            result.success = False
            result.message = f'Test C failed after {actual_execution_time:.2f}s (simulated failure)'
            self.get_logger().warn(f'Goal failed: {result.message}')

        return result


def main(args=None):
    rclpy.init(args=args)

    node = TestActionServerC()

    executor = MultiThreadedExecutor()
    executor.add_node(node)

    try:
        executor.spin()
    except KeyboardInterrupt:
        pass
    finally:
        node.destroy_node()
        rclpy.shutdown()


if __name__ == '__main__':
    main()
