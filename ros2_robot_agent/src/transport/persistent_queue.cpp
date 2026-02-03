// Copyright 2026 Multi-Robot Supervision System
// Persistent Message Queue Implementation

#include "robot_agent/transport/persistent_queue.hpp"
#include "robot_agent/core/logger.hpp"

#include <nlohmann/json.hpp>

#include <algorithm>
#include <random>

namespace robot_agent {
namespace transport {

namespace {
logging::ComponentLogger log("PersistentQueue");

// Serialize message to JSON for disk storage
std::string serialize_message(const PersistentMessage& msg) {
    nlohmann::json j;
    j["id"] = msg.id;
    j["payload"] = msg.payload;
    j["message_type"] = msg.message_type;
    j["created_at_ms"] = std::chrono::duration_cast<std::chrono::milliseconds>(
        msg.created_at.time_since_epoch()).count();
    j["retry_count"] = msg.retry_count;
    j["max_retries"] = msg.max_retries;
    j["priority"] = msg.priority;
    j["requires_ack"] = msg.requires_ack;
    j["sequence_num"] = msg.sequence_num;
    return j.dump();
}

// Deserialize message from JSON
std::optional<PersistentMessage> deserialize_message(const std::string& json_str) {
    try {
        auto j = nlohmann::json::parse(json_str);
        PersistentMessage msg;
        msg.id = j["id"].get<std::string>();
        msg.payload = j["payload"].get<std::string>();
        msg.message_type = j.value("message_type", "");
        msg.retry_count = j.value("retry_count", 0);
        msg.max_retries = j.value("max_retries", 3);
        msg.priority = j.value("priority", 0);
        msg.requires_ack = j.value("requires_ack", true);
        msg.sequence_num = j.value("sequence_num", uint64_t(0));
        msg.persisted = true;

        // Reconstruct time points
        int64_t created_ms = j.value("created_at_ms", int64_t(0));
        msg.created_at = std::chrono::steady_clock::time_point(
            std::chrono::milliseconds(created_ms));
        msg.next_retry_at = std::chrono::steady_clock::now();

        return msg;
    } catch (const std::exception& e) {
        log.error("Failed to deserialize message: {}", e.what());
        return std::nullopt;
    }
}

}  // namespace

// ============================================================
// PersistentQueue Implementation
// ============================================================

PersistentQueue::PersistentQueue(const Config& config)
    : config_(config) {

    // Ensure storage directory exists
    std::error_code ec;
    std::filesystem::create_directories(config_.storage_path, ec);
    if (ec) {
        log.error("Failed to create storage directory {}: {}",
                 config_.storage_path, ec.message());
    }

    // Load any persisted messages
    load_from_disk();
}

PersistentQueue::~PersistentQueue() {
    stop();
}

bool PersistentQueue::start(SendCallback send_cb) {
    if (running_.exchange(true)) {
        log.warn("PersistentQueue already running");
        return false;
    }

    send_callback_ = std::move(send_cb);

    // Start processor thread
    processor_thread_ = std::thread(&PersistentQueue::process_loop, this);

    log.info("PersistentQueue started with {} pending messages", pending_count());
    return true;
}

void PersistentQueue::stop() {
    if (!running_.exchange(false)) {
        return;
    }

    queue_cv_.notify_all();

    if (processor_thread_.joinable()) {
        processor_thread_.join();
    }

    log.info("PersistentQueue stopped");
}

bool PersistentQueue::enqueue(PersistentMessage msg) {
    std::lock_guard<std::mutex> lock(queue_mutex_);

    if (pending_queue_.size() + retry_queue_.size() >= config_.max_queue_size) {
        log.warn("Queue full, dropping message {}", msg.id);
        return false;
    }

    msg.sequence_num = next_sequence();
    msg.created_at = std::chrono::steady_clock::now();
    msg.next_retry_at = msg.created_at;

    // Persist to disk first
    if (!persist_message(msg)) {
        log.error("Failed to persist message {}", msg.id);
        // Continue anyway - we'll lose it on crash but can still try to send
    }

    pending_queue_.push_back(std::move(msg));

    queue_cv_.notify_one();
    return true;
}

void PersistentQueue::acknowledge(const std::string& message_id) {
    std::lock_guard<std::mutex> lock(queue_mutex_);

    // Remove from retry queue
    auto it = retry_queue_.find(message_id);
    if (it != retry_queue_.end()) {
        remove_from_disk(message_id);
        retry_queue_.erase(it);
        log.debug("Acknowledged message {}", message_id);
        return;
    }

    // Remove from pending queue
    auto pit = std::find_if(pending_queue_.begin(), pending_queue_.end(),
        [&](const PersistentMessage& m) { return m.id == message_id; });
    if (pit != pending_queue_.end()) {
        remove_from_disk(message_id);
        pending_queue_.erase(pit);
        log.debug("Acknowledged pending message {}", message_id);
    }
}

void PersistentQueue::mark_failed(const std::string& message_id, const std::string& error) {
    std::lock_guard<std::mutex> lock(queue_mutex_);

    auto it = retry_queue_.find(message_id);
    if (it == retry_queue_.end()) {
        return;
    }

    auto& msg = it->second;
    msg.retry_count++;

    if (msg.is_expired()) {
        // Max retries exceeded - permanent failure
        log.error("Message {} failed permanently after {} retries: {}",
                 message_id, msg.retry_count, error);

        if (failure_callback_) {
            failure_callback_(msg, error);
        }

        remove_from_disk(message_id);
        retry_queue_.erase(it);
        failed_count_++;
    } else {
        // Schedule retry
        auto delay = config_.retry_policy.get_delay(msg.retry_count);
        msg.next_retry_at = std::chrono::steady_clock::now() + delay;

        log.warn("Message {} failed (attempt {}), retrying in {}ms: {}",
                 message_id, msg.retry_count, delay.count(), error);

        // Update persisted state
        persist_message(msg);

        queue_cv_.notify_one();
    }
}

size_t PersistentQueue::pending_count() const {
    std::lock_guard<std::mutex> lock(queue_mutex_);
    return pending_queue_.size();
}

size_t PersistentQueue::retry_count() const {
    std::lock_guard<std::mutex> lock(queue_mutex_);
    return retry_queue_.size();
}

void PersistentQueue::process_loop() {
    log.debug("Processor thread started");

    while (running_) {
        PersistentMessage msg_to_send;
        bool has_message = false;

        {
            std::unique_lock<std::mutex> lock(queue_mutex_);

            // Wait for messages or retry time
            queue_cv_.wait_for(lock, std::chrono::milliseconds(100), [this] {
                return !running_ || !pending_queue_.empty() ||
                       std::any_of(retry_queue_.begin(), retry_queue_.end(),
                           [](const auto& p) { return p.second.ready_for_retry(); });
            });

            if (!running_) break;

            // First, try to send pending messages
            if (!pending_queue_.empty()) {
                msg_to_send = std::move(pending_queue_.front());
                pending_queue_.pop_front();
                has_message = true;
            }
            // Then, check retry queue
            else {
                for (auto& [id, msg] : retry_queue_) {
                    if (msg.ready_for_retry()) {
                        msg_to_send = msg;  // Copy
                        has_message = true;
                        break;
                    }
                }
            }
        }

        if (has_message && send_callback_) {
            bool success = false;
            try {
                success = send_callback_(msg_to_send);
            } catch (const std::exception& e) {
                log.error("Send callback threw exception: {}", e.what());
            }

            if (success) {
                if (!msg_to_send.requires_ack) {
                    // Fire-and-forget, remove immediately
                    acknowledge(msg_to_send.id);
                } else {
                    // Move to retry queue to wait for ack
                    std::lock_guard<std::mutex> lock(queue_mutex_);
                    retry_queue_[msg_to_send.id] = std::move(msg_to_send);
                }
            } else {
                // Send failed, schedule retry
                mark_failed(msg_to_send.id, "Send failed");
            }
        }
    }

    log.debug("Processor thread stopped");
}

void PersistentQueue::load_from_disk() {
    std::error_code ec;
    for (const auto& entry : std::filesystem::directory_iterator(config_.storage_path, ec)) {
        if (!entry.is_regular_file()) continue;
        if (entry.path().extension() != ".msg") continue;

        std::ifstream file(entry.path());
        if (!file.is_open()) continue;

        std::string content((std::istreambuf_iterator<char>(file)),
                            std::istreambuf_iterator<char>());

        auto msg = deserialize_message(content);
        if (msg) {
            msg->persisted = true;

            // Update sequence counter
            if (msg->sequence_num >= sequence_counter_) {
                sequence_counter_ = msg->sequence_num + 1;
            }

            pending_queue_.push_back(std::move(*msg));
            log.debug("Loaded persisted message {}", msg->id);
        }
    }

    // Sort by sequence number
    std::sort(pending_queue_.begin(), pending_queue_.end(),
        [](const PersistentMessage& a, const PersistentMessage& b) {
            return a.sequence_num < b.sequence_num;
        });

    if (!pending_queue_.empty()) {
        log.info("Loaded {} persisted messages from disk", pending_queue_.size());
    }
}

bool PersistentQueue::persist_message(const PersistentMessage& msg) {
    std::string path = get_message_path(msg.id);

    std::ofstream file(path);
    if (!file.is_open()) {
        log.error("Failed to open {} for writing", path);
        return false;
    }

    file << serialize_message(msg);

    if (config_.sync_writes) {
        file.flush();
        // Note: fsync would require platform-specific code or std::filesystem
    }

    return file.good();
}

void PersistentQueue::remove_from_disk(const std::string& message_id) {
    std::string path = get_message_path(message_id);
    std::error_code ec;
    std::filesystem::remove(path, ec);
    if (ec) {
        log.warn("Failed to remove {}: {}", path, ec.message());
    }
}

std::string PersistentQueue::get_message_path(const std::string& message_id) const {
    return config_.storage_path + "/" + message_id + ".msg";
}

uint64_t PersistentQueue::next_sequence() {
    return sequence_counter_.fetch_add(1, std::memory_order_relaxed);
}

// ============================================================
// AckTracker Implementation
// ============================================================

AckTracker::AckTracker(std::chrono::milliseconds default_timeout)
    : default_timeout_(default_timeout) {
}

AckTracker::~AckTracker() {
    stop();
}

void AckTracker::start() {
    if (running_.exchange(true)) {
        return;
    }

    checker_thread_ = std::thread([this] {
        while (running_) {
            check_timeouts();

            std::unique_lock<std::mutex> lock(pending_mutex_);
            check_cv_.wait_for(lock, std::chrono::milliseconds(500), [this] {
                return !running_;
            });
        }
    });
}

void AckTracker::stop() {
    if (!running_.exchange(false)) {
        return;
    }

    check_cv_.notify_all();

    if (checker_thread_.joinable()) {
        checker_thread_.join();
    }
}

void AckTracker::track(const std::string& message_id,
                       std::function<void()> on_timeout,
                       std::optional<std::chrono::milliseconds> timeout) {
    std::lock_guard<std::mutex> lock(pending_mutex_);

    auto now = std::chrono::steady_clock::now();
    auto actual_timeout = timeout.value_or(default_timeout_);

    pending_[message_id] = PendingAck{
        message_id,
        now,
        now + actual_timeout,
        std::move(on_timeout)
    };
}

bool AckTracker::acknowledge(const std::string& message_id) {
    std::lock_guard<std::mutex> lock(pending_mutex_);
    return pending_.erase(message_id) > 0;
}

bool AckTracker::is_pending(const std::string& message_id) const {
    std::lock_guard<std::mutex> lock(pending_mutex_);
    return pending_.find(message_id) != pending_.end();
}

size_t AckTracker::pending_count() const {
    std::lock_guard<std::mutex> lock(pending_mutex_);
    return pending_.size();
}

void AckTracker::check_timeouts() {
    auto now = std::chrono::steady_clock::now();
    std::vector<std::function<void()>> timeout_callbacks;

    {
        std::lock_guard<std::mutex> lock(pending_mutex_);

        for (auto it = pending_.begin(); it != pending_.end(); ) {
            if (now >= it->second.deadline) {
                if (it->second.on_timeout) {
                    timeout_callbacks.push_back(it->second.on_timeout);
                }
                it = pending_.erase(it);
            } else {
                ++it;
            }
        }
    }

    // Call callbacks outside lock
    for (auto& cb : timeout_callbacks) {
        try {
            cb();
        } catch (...) {
            // Ignore callback exceptions
        }
    }
}

}  // namespace transport
}  // namespace robot_agent
