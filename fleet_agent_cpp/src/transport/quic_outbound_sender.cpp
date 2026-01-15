// Copyright 2026 Multi-Robot Supervision System
// QUIC Outbound Sender Implementation

#include "fleet_agent/transport/quic_outbound_sender.hpp"
#include "fleet_agent/core/logger.hpp"
#include "fleet_agent/core/shutdown.hpp"

#include "fleet/v1/service.pb.h"

#include <algorithm>
#include <cstring>

namespace fleet_agent {
namespace transport {

namespace {
logging::ComponentLogger log("QUICOutboundSender");
}

QUICOutboundSender::QUICOutboundSender(
    QUICClient* quic_client,
    QuicOutboundQueue& outbound_queue,
    const std::string& agent_id)
    : QUICOutboundSender(quic_client, outbound_queue, agent_id, Config()) {}

QUICOutboundSender::QUICOutboundSender(
    QUICClient* quic_client,
    QuicOutboundQueue& outbound_queue,
    const std::string& agent_id,
    const Config& config)
    : quic_client_(quic_client)
    , outbound_queue_(outbound_queue)
    , agent_id_(agent_id)
    , config_(config)
{
    log.info("QUICOutboundSender created for agent {}", agent_id_);
}

QUICOutboundSender::~QUICOutboundSender() {
    stop();
}

bool QUICOutboundSender::start() {
    if (running_.load()) {
        log.warn("QUICOutboundSender already running");
        return true;
    }

    log.info("Starting QUICOutboundSender...");

    stop_requested_.store(false);
    running_.store(true);

    sender_thread_ = std::thread(&QUICOutboundSender::sender_loop, this);

    log.info("QUICOutboundSender started");
    return true;
}

void QUICOutboundSender::stop() {
    if (!running_.load()) {
        return;
    }

    log.info("Stopping QUICOutboundSender...");

    stop_requested_.store(true);
    running_.store(false);

    // Wake up the sender thread if waiting on connection or queue
    {
        std::lock_guard<std::mutex> lock(connect_mutex_);
        connect_cv_.notify_all();
    }
    outbound_queue_.notify_all();

    if (sender_thread_.joinable()) {
        sender_thread_.join();
    }

    // Close stream if open
    {
        std::lock_guard<std::mutex> lock(stream_mutex_);
        if (send_stream_) {
            send_stream_->close();
            send_stream_.reset();
        }
    }

    log.info("QUICOutboundSender stopped");
}

void QUICOutboundSender::set_connection_callback(ConnectionCallback cb) {
    std::lock_guard<std::mutex> lock(callback_mutex_);
    connection_callback_ = std::move(cb);
}

void QUICOutboundSender::notify_connected() {
    log.info("QUIC connection available");
    connected_.store(true);
    connect_cv_.notify_all();
}

void QUICOutboundSender::notify_disconnected() {
    log.warn("QUIC connection lost");
    connected_.store(false);

    // Close current stream
    std::lock_guard<std::mutex> lock(stream_mutex_);
    if (send_stream_) {
        send_stream_->close();
        send_stream_.reset();
    }
}

void QUICOutboundSender::reset_stats() {
    stats_.messages_sent.store(0);
    stats_.messages_failed.store(0);
    stats_.bytes_sent.store(0);
    stats_.send_errors.store(0);
    stats_.reconnects.store(0);
    stats_.last_send_time_ms.store(0);
    stats_.queue_depth.store(0);
}

void QUICOutboundSender::sender_loop() {
    log.info("Sender loop started");

    while (!stop_requested_.load() && FLEET_AGENT_RUNNING) {
        // Wait for connection if not connected
        if (!connected_.load()) {
            std::unique_lock<std::mutex> lock(connect_mutex_);
            connect_cv_.wait_for(lock, std::chrono::seconds(1), [this] {
                return connected_.load() || stop_requested_.load();
            });

            if (stop_requested_.load()) {
                break;
            }

            if (!connected_.load()) {
                continue;
            }
        }

        // Wait for message with condition variable notification
        // Uses NotifiableQueue::wait_pop() for low-latency (<1ms) wake-up
        OutboundMessage msg;
        if (outbound_queue_.wait_pop(msg, std::chrono::milliseconds(100), running_)) {
            // Update queue depth stat
            stats_.queue_depth.store(static_cast<uint32_t>(outbound_queue_.unsafe_size()));

            // Send the message
            size_t retries = 0;
            while (retries < config_.max_retries && !stop_requested_.load()) {
                if (send_message(msg)) {
                    break;
                }

                retries++;
                if (retries < config_.max_retries) {
                    log.warn("Send failed, retry {}/{}", retries, config_.max_retries);
                    std::this_thread::sleep_for(config_.retry_delay);
                }
            }

            if (retries >= config_.max_retries) {
                log.error("Message dropped after {} retries", config_.max_retries);
                stats_.messages_failed.fetch_add(1);
            }
        }
        // No sleep needed - wait_pop handles efficient waiting
    }

    log.info("Sender loop exited");
}

bool QUICOutboundSender::ensure_stream() {
    std::lock_guard<std::mutex> lock(stream_mutex_);

    // Check if existing stream is still valid
    if (send_stream_ && send_stream_->is_open()) {
        return true;
    }

    // Need to open a new stream
    if (!quic_client_ || !quic_client_->is_connected()) {
        log.warn("Cannot open stream: QUIC client not connected");
        connected_.store(false);
        return false;
    }

    send_stream_ = quic_client_->get_stream();
    if (!send_stream_) {
        log.error("Failed to get QUIC stream");
        stats_.send_errors.fetch_add(1);
        return false;
    }

    log.info("Opened new QUIC stream (ID: {})", send_stream_->id());
    stats_.reconnects.fetch_add(1);
    return true;
}

bool QUICOutboundSender::send_message(const OutboundMessage& msg) {
    if (!msg.message) {
        log.warn("Attempted to send null message");
        return true;  // Consider it "sent" to avoid retries
    }

    // Ensure we have a valid stream
    if (!ensure_stream()) {
        return false;
    }

    // Serialize message with length prefix
    std::vector<uint8_t> buffer;
    if (!serialize_message(msg, buffer)) {
        log.error("Failed to serialize message");
        stats_.messages_failed.fetch_add(1);
        return true;  // Don't retry serialization failures
    }

    // Send via stream
    std::lock_guard<std::mutex> lock(stream_mutex_);
    if (!send_stream_ || !send_stream_->is_open()) {
        log.warn("Stream closed during send");
        handle_send_failure();
        return false;
    }

    bool sent = send_stream_->write(buffer.data(), buffer.size(), false);
    if (!sent) {
        log.error("Stream write failed");
        handle_send_failure();
        return false;
    }

    // Update statistics
    stats_.messages_sent.fetch_add(1);
    stats_.bytes_sent.fetch_add(buffer.size());
    stats_.last_send_time_ms.store(now_ms());

    // Log heartbeat at debug level (they're frequent)
    if (msg.message->has_heartbeat()) {
        log.debug("Sent heartbeat ({} bytes)", buffer.size());
    } else {
        log.info("Sent message type={} ({} bytes)",
            static_cast<int>(msg.message->payload_case()), buffer.size());
    }

    return true;
}

bool QUICOutboundSender::serialize_message(
    const OutboundMessage& msg,
    std::vector<uint8_t>& buffer)
{
    if (!msg.message) {
        return false;
    }

    // Serialize to protobuf
    std::string serialized;
    if (!msg.message->SerializeToString(&serialized)) {
        log.error("Protobuf serialization failed");
        return false;
    }

    // Allocate buffer: 4 bytes length prefix + payload
    size_t payload_size = serialized.size();
    buffer.resize(4 + payload_size);

    // Write 4-byte big-endian length prefix (compatible with Go server)
    buffer[0] = static_cast<uint8_t>((payload_size >> 24) & 0xFF);
    buffer[1] = static_cast<uint8_t>((payload_size >> 16) & 0xFF);
    buffer[2] = static_cast<uint8_t>((payload_size >> 8) & 0xFF);
    buffer[3] = static_cast<uint8_t>(payload_size & 0xFF);

    // Copy payload
    std::memcpy(buffer.data() + 4, serialized.data(), payload_size);

    return true;
}

void QUICOutboundSender::handle_send_failure() {
    stats_.send_errors.fetch_add(1);

    // Close current stream
    {
        std::lock_guard<std::mutex> lock(stream_mutex_);
        if (send_stream_) {
            send_stream_->close();
            send_stream_.reset();
        }
    }

    // Check if connection is still alive
    if (!quic_client_ || !quic_client_->is_connected()) {
        connected_.store(false);

        // Notify callback
        std::lock_guard<std::mutex> cb_lock(callback_mutex_);
        if (connection_callback_) {
            connection_callback_(false);
        }
    }
}

int64_t QUICOutboundSender::now_ms() {
    auto now = std::chrono::system_clock::now();
    return std::chrono::duration_cast<std::chrono::milliseconds>(
        now.time_since_epoch()
    ).count();
}

}  // namespace transport
}  // namespace fleet_agent
