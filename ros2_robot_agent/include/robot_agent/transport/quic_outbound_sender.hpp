// Copyright 2026 Multi-Robot Supervision System
// QUIC Outbound Sender - Sends queued messages to server

#pragma once

#include "robot_agent/core/types.hpp"
#include "robot_agent/transport/quic_transport.hpp"

#include <atomic>
#include <chrono>
#include <condition_variable>
#include <functional>
#include <memory>
#include <mutex>
#include <string>
#include <thread>

namespace robot_agent {
namespace transport {

/**
 * QUICOutboundSender - Dedicated sender for outbound QUIC messages
 *
 * Runs a background thread that:
 * 1. Polls QuicOutboundQueue for pending messages
 * 2. Serializes messages to protobuf format
 * 3. Sends via persistent QUIC stream with length-prefix framing
 *
 * Thread-safe and handles reconnection gracefully.
 *
 * Message framing (compatible with Go server):
 * +----------------+------------------+
 * | Length (4B BE) | Protobuf payload |
 * +----------------+------------------+
 */
class QUICOutboundSender {
public:
    /**
     * Configuration for the sender
     */
    struct Config {
        // Polling interval when queue is empty
        std::chrono::milliseconds poll_interval{10};

        // Maximum messages to batch per send cycle
        size_t max_batch_size{100};

        // Timeout for stream operations
        std::chrono::milliseconds stream_timeout{5000};

        // Retry delay on send failure
        std::chrono::milliseconds retry_delay{100};

        // Maximum retry attempts before dropping message
        size_t max_retries{3};

        // Enable statistics tracking
        bool enable_stats{true};
    };

    /**
     * Statistics for monitoring
     */
    struct Stats {
        std::atomic<uint64_t> messages_sent{0};
        std::atomic<uint64_t> messages_failed{0};
        std::atomic<uint64_t> bytes_sent{0};
        std::atomic<uint64_t> send_errors{0};
        std::atomic<uint64_t> reconnects{0};
        std::atomic<int64_t> last_send_time_ms{0};
        std::atomic<uint32_t> queue_depth{0};
    };

    /**
     * Connection state callback
     */
    using ConnectionCallback = std::function<void(bool connected)>;

    /**
     * Constructor
     *
     * @param quic_client QUIC client for transport
     * @param outbound_queue Queue to poll for messages
     * @param agent_id Agent identifier for logging
     * @param config Sender configuration
     */
    QUICOutboundSender(
        QUICClient* quic_client,
        QuicOutboundQueue& outbound_queue,
        const std::string& agent_id
    );

    QUICOutboundSender(
        QUICClient* quic_client,
        QuicOutboundQueue& outbound_queue,
        const std::string& agent_id,
        const Config& config
    );

    ~QUICOutboundSender();

    // Non-copyable
    QUICOutboundSender(const QUICOutboundSender&) = delete;
    QUICOutboundSender& operator=(const QUICOutboundSender&) = delete;

    // ============================================================
    // Lifecycle
    // ============================================================

    /**
     * Start the sender thread.
     *
     * @return true if started successfully
     */
    bool start();

    /**
     * Stop the sender thread.
     *
     * Waits for current send to complete before returning.
     */
    void stop();

    /**
     * Check if sender is running.
     */
    bool is_running() const { return running_.load(); }

    // ============================================================
    // Configuration
    // ============================================================

    /**
     * Set connection state callback.
     *
     * Called when stream connection state changes.
     */
    void set_connection_callback(ConnectionCallback cb);

    /**
     * Notify sender that connection is available.
     *
     * Call this after QUIC connection is established.
     */
    void notify_connected();

    /**
     * Notify sender that connection is lost.
     *
     * Call this when QUIC connection is lost.
     */
    void notify_disconnected();

    // ============================================================
    // Statistics
    // ============================================================

    /**
     * Get current statistics.
     */
    const Stats& stats() const { return stats_; }

    /**
     * Reset statistics.
     */
    void reset_stats();

private:
    QUICClient* quic_client_;
    QuicOutboundQueue& outbound_queue_;
    std::string agent_id_;
    Config config_;

    // Thread control
    std::thread sender_thread_;
    std::atomic<bool> running_{false};
    std::atomic<bool> stop_requested_{false};

    // Connection state
    std::atomic<bool> connected_{false};
    std::mutex connect_mutex_;
    std::condition_variable connect_cv_;

    // Persistent stream for sending
    std::shared_ptr<QUICStream> send_stream_;
    std::mutex stream_mutex_;

    // Callbacks
    ConnectionCallback connection_callback_;
    std::mutex callback_mutex_;

    // Statistics
    Stats stats_;

    // ============================================================
    // Internal Methods
    // ============================================================

    /**
     * Main sender loop (runs in sender_thread_)
     */
    void sender_loop();

    /**
     * Ensure send stream is available.
     *
     * Opens a new stream if current one is closed.
     *
     * @return true if stream is available
     */
    bool ensure_stream();

    /**
     * Send a single message via stream.
     *
     * @param msg Message to send
     * @return true if sent successfully
     */
    bool send_message(const OutboundMessage& msg);

    /**
     * Serialize and frame a message.
     *
     * Adds 4-byte big-endian length prefix.
     *
     * @param msg Message to serialize
     * @param buffer Output buffer
     * @return true if serialization successful
     */
    bool serialize_message(const OutboundMessage& msg, std::vector<uint8_t>& buffer);

    /**
     * Handle send failure.
     *
     * May close stream and trigger reconnection.
     */
    void handle_send_failure();

    /**
     * Get current timestamp in milliseconds.
     */
    static int64_t now_ms();
};

}  // namespace transport
}  // namespace robot_agent
