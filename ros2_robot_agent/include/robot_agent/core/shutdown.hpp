// Copyright 2026 Multi-Robot Supervision System
// Graceful Shutdown Handler

#pragma once

#include <atomic>
#include <condition_variable>
#include <functional>
#include <mutex>
#include <vector>

namespace robot_agent {

/**
 * ShutdownHandler - Manages graceful shutdown on SIGINT/SIGTERM.
 *
 * Singleton class that:
 * - Registers signal handlers for SIGINT and SIGTERM
 * - Maintains a list of cleanup callbacks
 * - Provides thread-safe shutdown coordination
 *
 * Usage:
 *   // Register cleanup callback
 *   ShutdownHandler::instance().register_callback([&] {
 *       agent.stop();
 *   });
 *
 *   // Check in loops
 *   while (!ShutdownHandler::instance().should_shutdown()) {
 *       // ... do work ...
 *   }
 *
 *   // Wait for shutdown completion
 *   ShutdownHandler::instance().wait_for_shutdown();
 */
class ShutdownHandler {
public:
    /**
     * Get singleton instance.
     * Thread-safe initialization (C++11 magic statics).
     */
    static ShutdownHandler& instance();

    // Delete copy/move operations
    ShutdownHandler(const ShutdownHandler&) = delete;
    ShutdownHandler& operator=(const ShutdownHandler&) = delete;
    ShutdownHandler(ShutdownHandler&&) = delete;
    ShutdownHandler& operator=(ShutdownHandler&&) = delete;

    /**
     * Register a shutdown callback.
     *
     * Callbacks are invoked in reverse registration order (LIFO).
     * This ensures that later-registered components are cleaned up first.
     *
     * @param callback Function to call during shutdown
     */
    void register_callback(std::function<void()> callback);

    /**
     * Check if shutdown has been requested.
     *
     * @return true if shutdown signal received
     */
    bool should_shutdown() const;

    /**
     * Request shutdown programmatically.
     *
     * Triggers the same behavior as receiving SIGINT/SIGTERM.
     */
    void request_shutdown();

    /**
     * Wait for all shutdown callbacks to complete.
     *
     * Blocks until:
     * - Shutdown is requested, AND
     * - All registered callbacks have been executed
     */
    void wait_for_shutdown();

    /**
     * Execute shutdown callbacks.
     *
     * Called automatically by signal handler, but can be called
     * manually for testing or special shutdown scenarios.
     */
    void execute_callbacks();

    /**
     * Check if callbacks have been executed.
     *
     * @return true if shutdown callbacks completed
     */
    bool is_shutdown_complete() const;

    /**
     * Install signal handlers.
     *
     * Called automatically on first instance() call.
     * Can be called again to reinstall handlers.
     */
    void install_signal_handlers();

private:
    ShutdownHandler();
    ~ShutdownHandler();

    static void signal_handler(int signal);

    std::atomic<bool> shutdown_requested_{false};
    std::atomic<bool> shutdown_complete_{false};
    std::vector<std::function<void()>> callbacks_;
    mutable std::mutex mutex_;
    std::condition_variable cv_;
    bool signals_installed_{false};
};

// ============================================================
// Convenience Macros
// ============================================================

/**
 * Check shutdown flag in loop condition.
 *
 * Usage:
 *   while (FLEET_AGENT_RUNNING) {
 *       // ... loop body ...
 *   }
 */
#define FLEET_AGENT_RUNNING (!robot_agent::ShutdownHandler::instance().should_shutdown())

/**
 * Register shutdown callback.
 *
 * Usage:
 *   FLEET_AGENT_ON_SHUTDOWN([&] { cleanup(); });
 */
#define FLEET_AGENT_ON_SHUTDOWN(callback) \
    robot_agent::ShutdownHandler::instance().register_callback(callback)

}  // namespace robot_agent
