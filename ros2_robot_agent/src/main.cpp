// Copyright 2026 Multi-Robot Supervision System
// Robot Agent Entry Point

#include "robot_agent/agent.hpp"
#include "robot_agent/core/config_loader.hpp"
#include "robot_agent/core/logger.hpp"

#include <rclcpp/rclcpp.hpp>

#include <iostream>
#include <string>

namespace {

void print_usage(const char* prog_name) {
    std::cout << "Usage: " << prog_name << " [options]\n"
              << "\n"
              << "Options:\n"
              << "  -c, --config <file>  Configuration file path\n"
              << "                       Default: /etc/robot_agent/agent.yaml\n"
              << "                       Or: FLEET_AGENT_CONFIG env var\n"
              << "  -h, --help           Show this help message\n"
              << "  -v, --version        Show version information\n"
              << "\n"
              << "Environment variables:\n"
              << "  FLEET_AGENT_CONFIG   Config file path\n"
              << "  FLEET_AGENT_ID       Override agent ID\n"
              << "  FLEET_SERVER_URL     Override server URL\n"
              << "\n"
              << "Example:\n"
              << "  " << prog_name << " -c /opt/robot_agent/config/agent.yaml\n"
              << std::endl;
}

void print_version() {
    std::cout << "Robot Agent C++ v1.0.0\n"
              << "Multi-Robot Supervision System\n"
              << "Built with ROS2 Humble, QUIC (MsQuic)\n"
              << std::endl;
}

std::string get_config_path(int argc, char* argv[]) {
    // Check command line arguments
    for (int i = 1; i < argc; ++i) {
        std::string arg = argv[i];
        if ((arg == "-c" || arg == "--config") && i + 1 < argc) {
            return argv[i + 1];
        }
    }

    // Check environment variable
    const char* env_config = std::getenv("FLEET_AGENT_CONFIG");
    if (env_config && env_config[0] != '\0') {
        return env_config;
    }

    // Default path
    return "/etc/robot_agent/agent.yaml";
}

}  // namespace

int main(int argc, char* argv[]) {
    // Parse command line
    for (int i = 1; i < argc; ++i) {
        std::string arg = argv[i];
        if (arg == "-h" || arg == "--help") {
            print_usage(argv[0]);
            return 0;
        }
        if (arg == "-v" || arg == "--version") {
            print_version();
            return 0;
        }
    }

    // Initialize ROS2 (passing original args for remapping)
    rclcpp::init(argc, argv);

    // Get config path
    std::string config_path = get_config_path(argc, argv);

    // Load configuration
    robot_agent::AgentConfig config;
    try {
        config = robot_agent::load_config(config_path);
    } catch (const robot_agent::ConfigLoadError& e) {
        std::cerr << "Failed to load configuration: " << config_path << std::endl;
        std::cerr << "Error: " << e.what() << std::endl;
        rclcpp::shutdown();
        return 1;
    } catch (const robot_agent::ConfigValidationError& e) {
        std::cerr << "Configuration validation failed: " << config_path << std::endl;
        std::cerr << "Error: " << e.what() << std::endl;
        rclcpp::shutdown();
        return 1;
    }

    // Initialize logger early for startup messages
    robot_agent::logging::init(config.logging.level, config.logging.file);

    std::cout << "Robot Agent starting..." << std::endl;
    std::cout << "Agent ID: " << config.agent_id << std::endl;
    std::cout << "Config: " << config_path << std::endl;
    std::cout << "Robots: " << config.robots.size() << std::endl;

    // Create and run agent
    robot_agent::Agent agent(config);
    int result = agent.run();

    // Cleanup
    std::cout << "Robot Agent exiting with code: " << result << std::endl;
    rclcpp::shutdown();

    return result;
}
