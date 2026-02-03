// Copyright 2026 Multi-Robot Supervision System
// Graceful Shutdown Handler Implementation

#include "robot_agent/core/shutdown.hpp"

#include <algorithm>
#include <csignal>
#include <cstring>
#include <iostream>

namespace robot_agent {

namespace {
// Global pointer for signal handler (signals can't access member functions)
ShutdownHandler* g_handler = nullptr;
}  // namespace

ShutdownHandler::ShutdownHandler() {
    g_handler = this;
    install_signal_handlers();
}

ShutdownHandler::~ShutdownHandler() {
    g_handler = nullptr;
}

ShutdownHandler& ShutdownHandler::instance() {
    static ShutdownHandler instance;
    return instance;
}

void ShutdownHandler::install_signal_handlers() {
    std::lock_guard<std::mutex> lock(mutex_);

    if (signals_installed_) {
        return;
    }

    // Install signal handlers
    struct sigaction sa;
    sa.sa_handler = signal_handler;
    sigemptyset(&sa.sa_mask);
    sa.sa_flags = 0;

    sigaction(SIGINT, &sa, nullptr);
    sigaction(SIGTERM, &sa, nullptr);

    signals_installed_ = true;
}

void ShutdownHandler::signal_handler(int signal) {
    const char* signal_name = (signal == SIGINT) ? "SIGINT" : "SIGTERM";

    // Write to stderr (signal-safe)
    const char* msg_start = "\n[ShutdownHandler] Received ";
    const char* msg_end = ", initiating shutdown...\n";
    write(STDERR_FILENO, msg_start, strlen(msg_start));
    write(STDERR_FILENO, signal_name, strlen(signal_name));
    write(STDERR_FILENO, msg_end, strlen(msg_end));

    if (g_handler) {
        g_handler->request_shutdown();
    }
}

void ShutdownHandler::register_callback(std::function<void()> callback) {
    std::lock_guard<std::mutex> lock(mutex_);

    if (shutdown_requested_) {
        // Already shutting down, execute immediately
        try {
            callback();
        } catch (const std::exception& e) {
            std::cerr << "[ShutdownHandler] Callback exception: " << e.what() << std::endl;
        }
        return;
    }

    callbacks_.push_back(std::move(callback));
}

bool ShutdownHandler::should_shutdown() const {
    return shutdown_requested_.load(std::memory_order_acquire);
}

void ShutdownHandler::request_shutdown() {
    bool expected = false;
    if (!shutdown_requested_.compare_exchange_strong(expected, true,
                                                      std::memory_order_acq_rel)) {
        // Already shutting down
        return;
    }

    // Notify any threads waiting on cv_
    cv_.notify_all();

    // Execute callbacks in a separate context to avoid blocking signal handler
    // In production, this should be done in a dedicated thread
    // For simplicity, we execute synchronously here
    execute_callbacks();
}

void ShutdownHandler::execute_callbacks() {
    std::vector<std::function<void()>> callbacks_copy;

    {
        std::lock_guard<std::mutex> lock(mutex_);
        if (shutdown_complete_) {
            return;  // Already executed
        }
        callbacks_copy = callbacks_;
    }

    // Execute callbacks in reverse order (LIFO)
    // This ensures that later-registered components are cleaned up first
    for (auto it = callbacks_copy.rbegin(); it != callbacks_copy.rend(); ++it) {
        try {
            (*it)();
        } catch (const std::exception& e) {
            std::cerr << "[ShutdownHandler] Callback exception: " << e.what() << std::endl;
        } catch (...) {
            std::cerr << "[ShutdownHandler] Unknown callback exception" << std::endl;
        }
    }

    {
        std::lock_guard<std::mutex> lock(mutex_);
        shutdown_complete_ = true;
    }

    cv_.notify_all();
}

void ShutdownHandler::wait_for_shutdown() {
    std::unique_lock<std::mutex> lock(mutex_);

    // Wait for shutdown to be requested
    cv_.wait(lock, [this] {
        return shutdown_requested_.load(std::memory_order_acquire);
    });

    // If callbacks haven't been executed yet (e.g., in multi-threaded scenario),
    // wait for them to complete
    if (!shutdown_complete_) {
        // Execute callbacks if not done yet
        lock.unlock();
        execute_callbacks();
        lock.lock();
    }

    // Wait for completion
    cv_.wait(lock, [this] {
        return shutdown_complete_.load(std::memory_order_acquire);
    });
}

bool ShutdownHandler::is_shutdown_complete() const {
    return shutdown_complete_.load(std::memory_order_acquire);
}

}  // namespace robot_agent
