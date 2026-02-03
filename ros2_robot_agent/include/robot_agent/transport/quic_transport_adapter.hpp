// Copyright 2026 Multi-Robot Supervision System
// QUIC Transport Adapter - Adapts QUICClient to ITransport interface

#pragma once

#include "robot_agent/interfaces/transport.hpp"
#include "robot_agent/transport/quic_transport.hpp"

#include <memory>
#include <mutex>
#include <string>

namespace robot_agent::transport {

/**
 * QUICTransportAdapter - Adapts QUICClient to ITransport interface.
 *
 * This adapter wraps the existing QUICClient implementation
 * to conform to the ITransport interface, enabling:
 *   - Dependency injection in Agent class
 *   - Unit testing with mock transports
 *   - Consistent transport API across implementations
 *
 * Usage:
 *   auto quic_client = std::make_shared<QUICClient>(config);
 *   auto transport = std::make_unique<QUICTransportAdapter>(quic_client, creds);
 *   transport->connect("192.168.0.100", 9443);
 */
class QUICTransportAdapter : public interfaces::ITransport {
public:
    /**
     * Constructor.
     *
     * @param client Shared pointer to QUICClient (ownership shared)
     * @param ca_cert_path Path to CA certificate for TLS
     * @param client_cert_path Optional path to client certificate
     * @param client_key_path Optional path to client private key
     */
    QUICTransportAdapter(
        std::shared_ptr<QUICClient> client,
        const std::string& ca_cert_path,
        const std::string& client_cert_path = "",
        const std::string& client_key_path = ""
    );

    /**
     * Constructor with QUICConfig for feature queries.
     *
     * @param client Shared pointer to QUICClient
     * @param config QUIC configuration for feature flags
     * @param ca_cert_path Path to CA certificate
     * @param client_cert_path Optional client certificate path
     * @param client_key_path Optional client key path
     */
    QUICTransportAdapter(
        std::shared_ptr<QUICClient> client,
        const QUICConfig& config,
        const std::string& ca_cert_path,
        const std::string& client_cert_path = "",
        const std::string& client_key_path = ""
    );

    ~QUICTransportAdapter() override;

    // ============================================================
    // ITransport Implementation
    // ============================================================

    /**
     * Connect to server.
     * Initializes TLS and establishes QUIC connection.
     */
    bool connect(const std::string& address, uint16_t port) override;

    /**
     * Disconnect from server.
     */
    void disconnect() override;

    /**
     * Check connection status.
     */
    bool is_connected() const override;

    /**
     * Send data via reliable stream.
     * Opens a stream if needed and writes data.
     */
    bool send(const std::vector<uint8_t>& data) override;

    /**
     * Send data via unreliable datagram.
     * Falls back to reliable send if datagrams not supported.
     */
    bool send_datagram(const std::vector<uint8_t>& data) override;

    /**
     * Set callback for received data.
     * Receives data from both streams and datagrams.
     */
    void set_receive_callback(ReceiveCallback cb) override;

    /**
     * Set callback for connection state changes.
     */
    void set_connection_callback(ConnectionCallback cb) override;

    /**
     * Get protocol name.
     */
    std::string protocol_name() const override { return "QUIC"; }

    /**
     * Check 0-RTT support.
     */
    bool supports_0rtt() const override;

    /**
     * Check datagram support.
     */
    bool supports_datagrams() const override;

    // ============================================================
    // Additional Methods
    // ============================================================

    /**
     * Get underlying QUICClient for advanced operations.
     */
    QUICClient* client() const { return client_.get(); }

    /**
     * Get connection statistics.
     */
    QUICConnection::Stats get_stats() const;

private:
    std::shared_ptr<QUICClient> client_;
    QUICConfig config_;

    std::string ca_cert_path_;
    std::string client_cert_path_;
    std::string client_key_path_;

    bool initialized_{false};

    // Current stream for reliable messaging
    std::shared_ptr<QUICStream> current_stream_;
    mutable std::mutex stream_mutex_;

    // Callbacks
    ReceiveCallback receive_callback_;
    ConnectionCallback connection_callback_;
    mutable std::mutex callback_mutex_;

    /**
     * Ensure we have an open stream for sending.
     */
    std::shared_ptr<QUICStream> ensure_stream();

    /**
     * Handle incoming datagram data.
     */
    void on_datagram_received(const uint8_t* data, size_t len);

    /**
     * Handle stream data received.
     */
    void on_stream_data(const uint8_t* data, size_t len, bool fin);
};

}  // namespace robot_agent::transport
