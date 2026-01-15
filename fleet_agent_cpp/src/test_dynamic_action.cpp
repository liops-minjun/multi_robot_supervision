// Simple test for DynamicActionClient
#include <rclcpp/rclcpp.hpp>
#include "fleet_agent/executor/typed_action_client.hpp"
#include <chrono>
#include <thread>

using namespace fleet_agent::executor;

int main(int argc, char** argv) {
    rclcpp::init(argc, argv);

    auto node = std::make_shared<rclcpp::Node>("test_dynamic_action");

    RCLCPP_INFO(node->get_logger(), "Creating DynamicActionClient for /test_A_action...");

    auto client = ActionClientFactory::create(
        node,
        "/test_A_action",
        "test_action_server/action/TestAction"
    );

    if (!client) {
        RCLCPP_ERROR(node->get_logger(), "Failed to create action client");
        return 1;
    }

    RCLCPP_INFO(node->get_logger(), "Waiting for action server...");
    if (!client->wait_for_server(std::chrono::seconds(5))) {
        RCLCPP_ERROR(node->get_logger(), "Action server not available");
        return 1;
    }

    RCLCPP_INFO(node->get_logger(), "Sending goal...");

    std::atomic<bool> done{false};

    auto handle = client->send_goal(
        R"({"task_name": "dynamic_test", "timeout_sec": 5.0})",
        [&done, &node](bool success, const std::string& result) {
            RCLCPP_INFO(node->get_logger(), "=== RESULT CALLBACK ===");
            RCLCPP_INFO(node->get_logger(), "Success: %s", success ? "true" : "false");
            RCLCPP_INFO(node->get_logger(), "Result: %s", result.c_str());
            done.store(true);
        },
        [&node](const std::string& feedback) {
            RCLCPP_INFO(node->get_logger(), "Feedback: %s", feedback.c_str());
        }
    );

    if (!handle) {
        RCLCPP_ERROR(node->get_logger(), "Failed to send goal");
        return 1;
    }

    RCLCPP_INFO(node->get_logger(), "Goal sent, waiting for result...");

    // Spin until done
    rclcpp::executors::SingleThreadedExecutor executor;
    executor.add_node(node);

    while (!done.load() && rclcpp::ok()) {
        executor.spin_some(std::chrono::milliseconds(100));
    }

    RCLCPP_INFO(node->get_logger(), "Test complete");
    rclcpp::shutdown();
    return 0;
}
