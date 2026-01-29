#!/usr/bin/env python3
"""
Test Action Server B (Lifecycle Node)

A lifecycle-managed action server that:
- Waits randomly 5-10 seconds
- Returns Success with 90% probability
- Returns Failure with 10% probability

Lifecycle States:
- UNCONFIGURED: Node created, no resources allocated
- INACTIVE: Configured but action server not accepting goals
- ACTIVE: Action server is running and accepting goals
- FINALIZED: Shutting down

Used for testing Action Graph workflows and lifecycle state monitoring.
"""

import rclpy
from rclpy.lifecycle import LifecycleNode, LifecycleState, TransitionCallbackReturn
from rclpy.action import ActionServer, GoalResponse, CancelResponse
from rclpy.callback_groups import ReentrantCallbackGroup
from rclpy.executors import MultiThreadedExecutor

import random
import time

from test_action_server.action import TestAction


class TestActionServerB(LifecycleNode):
    def __init__(self):
        super().__init__('test_b_action_server')

        self._action_server = None
        self._callback_group = ReentrantCallbackGroup()

        # Configuration
        self.min_duration = 5.0   # Minimum execution time (seconds)
        self.max_duration = 10.0  # Maximum execution time (seconds)
        self.success_rate = 0.9   # 90% success rate

        self.get_logger().info('Test Action Server B created (UNCONFIGURED state)')
        self.get_logger().info('Use "ros2 lifecycle set /test_b_action_server configure" to configure')
        self.get_logger().info('Use "ros2 lifecycle set /test_b_action_server activate" to activate')

    # ============================================================
    # Lifecycle Callbacks
    # ============================================================

    def on_configure(self, state: LifecycleState) -> TransitionCallbackReturn:
        """Configure the node - allocate resources."""
        self.get_logger().info('Configuring...')
        self.get_logger().info('Configured successfully (INACTIVE state)')
        return TransitionCallbackReturn.SUCCESS

    def on_activate(self, state: LifecycleState) -> TransitionCallbackReturn:
        """Activate the node - start the action server."""
        self.get_logger().info('Activating...')

        self._action_server = ActionServer(
            self,
            TestAction,
            'test_B_action',
            execute_callback=self.execute_callback,
            goal_callback=self.goal_callback,
            cancel_callback=self.cancel_callback,
            callback_group=self._callback_group
        )

        self.get_logger().info('Test Action Server B activated on /test_B_action (ACTIVE state)')
        return TransitionCallbackReturn.SUCCESS

    def on_deactivate(self, state: LifecycleState) -> TransitionCallbackReturn:
        """Deactivate the node - stop accepting new goals."""
        self.get_logger().info('Deactivating...')

        if self._action_server:
            self._action_server.destroy()
            self._action_server = None

        self.get_logger().info('Deactivated (INACTIVE state)')
        return TransitionCallbackReturn.SUCCESS

    def on_cleanup(self, state: LifecycleState) -> TransitionCallbackReturn:
        """Clean up resources - return to unconfigured state."""
        self.get_logger().info('Cleaning up...')

        if self._action_server:
            self._action_server.destroy()
            self._action_server = None

        self.get_logger().info('Cleaned up (UNCONFIGURED state)')
        return TransitionCallbackReturn.SUCCESS

    def on_shutdown(self, state: LifecycleState) -> TransitionCallbackReturn:
        """Shutdown the node."""
        self.get_logger().info('Shutting down...')

        if self._action_server:
            self._action_server.destroy()
            self._action_server = None

        self.get_logger().info('Shutdown complete (FINALIZED state)')
        return TransitionCallbackReturn.SUCCESS

    def on_error(self, state: LifecycleState) -> TransitionCallbackReturn:
        """Handle error state."""
        self.get_logger().error(f'Error occurred in state {state.label}')
        return TransitionCallbackReturn.SUCCESS

    # ============================================================
    # Action Server Callbacks
    # ============================================================

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

        execution_time = random.uniform(self.min_duration, self.max_duration)
        start_time = time.time()

        feedback_msg = TestAction.Feedback()

        while True:
            elapsed = time.time() - start_time
            progress = min(elapsed / execution_time, 1.0)

            if goal_handle.is_cancel_requested:
                goal_handle.canceled()
                self.get_logger().info('Goal canceled')
                result = TestAction.Result()
                result.success = False
                result.message = 'Action was canceled'
                result.execution_time = elapsed
                return result

            feedback_msg.progress = progress
            feedback_msg.status = f'Processing... ({int(progress * 100)}%)'
            feedback_msg.elapsed_time = elapsed
            goal_handle.publish_feedback(feedback_msg)

            if elapsed >= execution_time:
                break

            time.sleep(0.1)

        actual_execution_time = time.time() - start_time
        success = random.random() < self.success_rate

        result = TestAction.Result()
        result.execution_time = actual_execution_time

        if success:
            goal_handle.succeed()
            result.success = True
            result.message = f'Test B completed successfully in {actual_execution_time:.2f}s'
            self.get_logger().info(f'Goal succeeded: {result.message}')
        else:
            goal_handle.abort()
            result.success = False
            result.message = f'Test B failed after {actual_execution_time:.2f}s (simulated failure)'
            self.get_logger().warn(f'Goal failed: {result.message}')

        return result


def main(args=None):
    rclpy.init(args=args)

    node = TestActionServerB()

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
