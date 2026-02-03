// Copyright 2026 Multi-Robot Supervision System
// QUIC Transport Adapter Implementation

#include "robot_agent/transport/quic_transport_adapter.hpp"
#include "robot_agent/core/logger.hpp"

namespace robot_agent::transport {

QUICTransportAdapter::QUICTransportAdapter(
    std::shared_ptr<QUICClient> client,
    const std::string& ca_cert_path,
    const std::string& client_cert_path,
    const std::string& client_key_path)
    : client_(std::move(client))
    , ca_cert_path_(ca_cert_path)
    , client_cert_path_(client_cert_path)
    , client_key_path_(client_key_path)
{
    // Default config - query client for actual values
    config_.enable_resumption = true;
    config_.enable_datagrams = true;
}

QUICTransportAdapter::QUICTransportAdapter(
    std::shared_ptr<QUICClient> client,
    const QUICConfig& config,
    const std::string& ca_cert_path,
    const std::string& client_cert_path,
    const std::string& client_key_path)
    : client_(std::move(client))
    , config_(config)
    , ca_cert_path_(ca_cert_path)
    , client_cert_path_(client_cert_path)
    , client_key_path_(client_key_path)
{
}

QUICTransportAdapter::~QUICTransportAdapter() {
    disconnect();
}

bool QUICTransportAdapter::connect(const std::string& address, uint16_t port) {
    if (!client_) {
        LOG_ERROR("QUICTransportAdapter: No client provided");
        return false;
    }

    // Initialize TLS if not done
    if (!initialized_) {
        if (!client_->initialize(ca_cert_path_, client_cert_path_, client_key_path_)) {
            LOG_ERROR("QUICTransportAdapter: Failed to initialize TLS");
            return false;
        }
        initialized_ = true;

        // Set up datagram callback
        client_->set_datagram_callback(
            [this](const uint8_t* data, size_t len) {
                on_datagram_received(data, len);
            });

        // Set up connection handler
        client_->set_connection_handler(
            [this](bool connected) {
                std::lock_guard<std::mutex> lock(callback_mutex_);
                if (connection_callback_) {
                    connection_callback_(connected);
                }
            });
    }

    // Connect to server
    if (!client_->connect(address, port)) {
        LOG_ERROR("QUICTransportAdapter: Failed to connect to {}:{}", address, port);
        return false;
    }

    LOG_INFO("QUICTransportAdapter: Connected to {}:{}", address, port);
    return true;
}

void QUICTransportAdapter::disconnect() {
    if (client_ && client_->is_connected()) {
        // Release current stream
        {
            std::lock_guard<std::mutex> lock(stream_mutex_);
            if (current_stream_) {
                client_->release_stream(current_stream_);
                current_stream_.reset();
            }
        }

        client_->disconnect();
        LOG_INFO("QUICTransportAdapter: Disconnected");
    }
}

bool QUICTransportAdapter::is_connected() const {
    return client_ && client_->is_connected();
}

bool QUICTransportAdapter::send(const std::vector<uint8_t>& data) {
    if (!is_connected()) {
        LOG_WARN("QUICTransportAdapter: Cannot send - not connected");
        return false;
    }

    auto stream = ensure_stream();
    if (!stream) {
        LOG_ERROR("QUICTransportAdapter: Failed to get stream for sending");
        return false;
    }

    // Write data to stream
    if (!stream->write(data.data(), data.size(), false)) {
        LOG_ERROR("QUICTransportAdapter: Failed to write to stream");
        // Stream may be broken, release it
        {
            std::lock_guard<std::mutex> lock(stream_mutex_);
            if (current_stream_ == stream) {
                client_->release_stream(current_stream_);
                current_stream_.reset();
            }
        }
        return false;
    }

    return true;
}

bool QUICTransportAdapter::send_datagram(const std::vector<uint8_t>& data) {
    if (!is_connected()) {
        LOG_WARN("QUICTransportAdapter: Cannot send datagram - not connected");
        return false;
    }

    if (!supports_datagrams()) {
        // Fall back to reliable send
        return send(data);
    }

    return client_->send_datagram(data.data(), data.size());
}

void QUICTransportAdapter::set_receive_callback(ReceiveCallback cb) {
    std::lock_guard<std::mutex> lock(callback_mutex_);
    receive_callback_ = std::move(cb);
}

void QUICTransportAdapter::set_connection_callback(ConnectionCallback cb) {
    std::lock_guard<std::mutex> lock(callback_mutex_);
    connection_callback_ = std::move(cb);

    // Also set on client if already initialized
    if (client_) {
        client_->set_connection_handler(
            [this](bool connected) {
                std::lock_guard<std::mutex> lock(callback_mutex_);
                if (connection_callback_) {
                    connection_callback_(connected);
                }
            });
    }
}

bool QUICTransportAdapter::supports_0rtt() const {
    return config_.enable_resumption;
}

bool QUICTransportAdapter::supports_datagrams() const {
    return config_.enable_datagrams;
}

QUICConnection::Stats QUICTransportAdapter::get_stats() const {
    if (client_) {
        return client_->get_stats();
    }
    return {};
}

std::shared_ptr<QUICStream> QUICTransportAdapter::ensure_stream() {
    std::lock_guard<std::mutex> lock(stream_mutex_);

    // Check if current stream is still valid
    if (current_stream_ && current_stream_->is_open()) {
        return current_stream_;
    }

    // Get a new stream
    current_stream_ = client_->get_stream();
    if (current_stream_) {
        // Set up data callback for the stream
        current_stream_->set_data_callback(
            [this](const uint8_t* data, size_t len, bool fin) {
                on_stream_data(data, len, fin);
            });

        // Set up close callback
        current_stream_->set_close_callback(
            [this](uint64_t error_code) {
                LOG_DEBUG("QUICTransportAdapter: Stream closed with error {}", error_code);
                std::lock_guard<std::mutex> lock(stream_mutex_);
                current_stream_.reset();
            });
    }

    return current_stream_;
}

void QUICTransportAdapter::on_datagram_received(const uint8_t* data, size_t len) {
    std::lock_guard<std::mutex> lock(callback_mutex_);
    if (receive_callback_) {
        receive_callback_(data, len);
    }
}

void QUICTransportAdapter::on_stream_data(const uint8_t* data, size_t len, bool /*fin*/) {
    std::lock_guard<std::mutex> lock(callback_mutex_);
    if (receive_callback_) {
        receive_callback_(data, len);
    }
}

}  // namespace robot_agent::transport
