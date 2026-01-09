// Copyright 2026 Multi-Robot Supervision System
// Persistent Message Queue for reliable delivery

#pragma once

#include <atomic>
#include <chrono>
#include <condition_variable>
#include <deque>
#include <filesystem>
#include <fstream>
#include <functional>
#include <memory>
#include <mutex>
#include <optional>
#include <string>
#include <thread>
#include <unordered_map>

namespace fleet_agent {
namespace transport {

// ============================================================
// PersistentMessage - Message with retry metadata
// ============================================================

struct PersistentMessage {
    std::string id;                                         // Unique message ID
    std::string payload;                                    // Serialized message
    std::string message_type;                               // For routing/handling
    std::chrono::steady_clock::time_point created_at;
    std::chrono::steady_clock::time_point next_retry_at;
    int retry_count{0};
    int max_retries{3};
    int priority{0};                                        // Higher = more urgent
    bool requires_ack{true};                                // If false, fire-and-forget

    // Persistence fields
    uint64_t sequence_num{0};                               // Monotonic sequence for ordering
    bool persisted{false};                                  // True if written to disk

    bool is_expired() const {
        return retry_count >= max_retries;
    }

    bool ready_for_retry() const {
        return std::chrono::steady_clock::now() >= next_retry_at;
    }
};

// ============================================================
// RetryPolicy - Configurable retry behavior
// ============================================================

struct RetryPolicy {
    int max_retries{3};
    std::chrono::milliseconds initial_delay{1000};
    std::chrono::milliseconds max_delay{30000};
    double backoff_multiplier{2.0};
    bool jitter_enabled{true};

    std::chrono::milliseconds get_delay(int retry_count) const {
        if (retry_count <= 0) return initial_delay;

        int64_t delay_ms = initial_delay.count();
        for (int i = 0; i < retry_count && delay_ms < max_delay.count(); i++) {
            delay_ms = static_cast<int64_t>(delay_ms * backoff_multiplier);
        }
        delay_ms = std::min(delay_ms, max_delay.count());

        // Add jitter (±10%)
        if (jitter_enabled && delay_ms > 0) {
            int64_t jitter = delay_ms / 10;
            delay_ms += (rand() % (2 * jitter + 1)) - jitter;
        }

        return std::chrono::milliseconds(std::max(delay_ms, int64_t(0)));
    }
};

// ============================================================
// PersistentQueue - Disk-backed message queue with retry
// ============================================================

class PersistentQueue {
public:
    using SendCallback = std::function<bool(const PersistentMessage&)>;
    using FailureCallback = std::function<void(const PersistentMessage&, const std::string&)>;

    struct Config {
        std::string storage_path{"/var/lib/fleet_agent/queue"};
        size_t max_queue_size{10000};
        size_t max_memory_queue{1000};     // Keep this many in memory
        bool sync_writes{true};            // fsync after each write
        RetryPolicy retry_policy;
    };

    explicit PersistentQueue(const Config& config);
    ~PersistentQueue();

    // Non-copyable, non-movable
    PersistentQueue(const PersistentQueue&) = delete;
    PersistentQueue& operator=(const PersistentQueue&) = delete;

    // Start/stop the retry processor
    bool start(SendCallback send_cb);
    void stop();

    // Queue a message for delivery
    bool enqueue(PersistentMessage msg);

    // Acknowledge successful delivery (removes from queue)
    void acknowledge(const std::string& message_id);

    // Mark message as failed (will retry if policy allows)
    void mark_failed(const std::string& message_id, const std::string& error);

    // Set failure callback
    void set_failure_callback(FailureCallback cb) { failure_callback_ = std::move(cb); }

    // Stats
    size_t pending_count() const;
    size_t retry_count() const;
    size_t failed_count() const { return failed_count_; }

private:
    void process_loop();
    void load_from_disk();
    bool persist_message(const PersistentMessage& msg);
    void remove_from_disk(const std::string& message_id);
    std::string get_message_path(const std::string& message_id) const;
    uint64_t next_sequence();

    Config config_;
    std::atomic<bool> running_{false};
    std::thread processor_thread_;

    // In-memory queue (priority + FIFO within priority)
    mutable std::mutex queue_mutex_;
    std::deque<PersistentMessage> pending_queue_;
    std::unordered_map<std::string, PersistentMessage> retry_queue_;
    std::condition_variable queue_cv_;

    // Callbacks
    SendCallback send_callback_;
    FailureCallback failure_callback_;

    // Sequence number for ordering
    std::atomic<uint64_t> sequence_counter_{0};

    // Stats
    std::atomic<uint64_t> failed_count_{0};
};

// ============================================================
// AckTracker - Track pending acknowledgments
// ============================================================

class AckTracker {
public:
    struct PendingAck {
        std::string message_id;
        std::chrono::steady_clock::time_point sent_at;
        std::chrono::steady_clock::time_point deadline;
        std::function<void()> on_timeout;
    };

    explicit AckTracker(std::chrono::milliseconds default_timeout = std::chrono::seconds(30));
    ~AckTracker();

    // Start timeout checker
    void start();
    void stop();

    // Track a pending ack
    void track(const std::string& message_id,
               std::function<void()> on_timeout = nullptr,
               std::optional<std::chrono::milliseconds> timeout = std::nullopt);

    // Mark as acknowledged (returns true if was pending)
    bool acknowledge(const std::string& message_id);

    // Check if pending
    bool is_pending(const std::string& message_id) const;

    // Stats
    size_t pending_count() const;

private:
    void check_timeouts();

    std::chrono::milliseconds default_timeout_;
    std::atomic<bool> running_{false};
    std::thread checker_thread_;

    mutable std::mutex pending_mutex_;
    std::unordered_map<std::string, PendingAck> pending_;
    std::condition_variable check_cv_;
};

}  // namespace transport
}  // namespace fleet_agent
