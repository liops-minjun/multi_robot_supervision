// Copyright 2026 Multi-Robot Supervision System
// Transport Interface - Abstraction for network communication

#pragma once

#include <cstdint>
#include <functional>
#include <memory>
#include <string>
#include <vector>

namespace robot_agent::interfaces {

/**
 * ITransport - Abstract interface for network transport layer.
 *
 * This interface decouples the agent logic from the specific transport
 * implementation (QUIC, TCP, etc.), enabling:
 *   - Unit testing with mock transports
 *   - Easy switching between transport protocols
 *   - Better separation of concerns
 */
class ITransport {
public:
    virtual ~ITransport() = default;

    // ============================================================
    // Connection Management
    // ============================================================

    /**
     * Connect to the server at the given address and port.
     * @param address Server hostname or IP address
     * @param port Server port number
     * @return true if connection initiated successfully
     */
    virtual bool connect(const std::string& address, uint16_t port) = 0;

    /**
     * Disconnect from the server.
     */
    virtual void disconnect() = 0;

    /**
     * Check if currently connected to the server.
     * @return true if connected
     */
    virtual bool is_connected() const = 0;

    // ============================================================
    // Data Transmission
    // ============================================================

    /**
     * Send data to the server (reliable stream).
     * @param data Buffer containing data to send
     * @return true if data was queued for sending
     */
    virtual bool send(const std::vector<uint8_t>& data) = 0;

    /**
     * Send data to the server (unreliable datagram for telemetry).
     * Falls back to reliable send if datagrams not supported.
     * @param data Buffer containing data to send
     * @return true if data was sent/queued
     */
    virtual bool send_datagram(const std::vector<uint8_t>& data) = 0;

    // ============================================================
    // Callbacks
    // ============================================================

    using ReceiveCallback = std::function<void(const uint8_t* data, size_t len)>;
    using ConnectionCallback = std::function<void(bool connected)>;

    /**
     * Set callback for received data.
     * @param cb Callback function receiving raw data
     */
    virtual void set_receive_callback(ReceiveCallback cb) = 0;

    /**
     * Set callback for connection state changes.
     * @param cb Callback function receiving connection status
     */
    virtual void set_connection_callback(ConnectionCallback cb) = 0;

    // ============================================================
    // Transport Info
    // ============================================================

    /**
     * Get the transport protocol name (e.g., "QUIC", "TCP").
     * @return Protocol name string
     */
    virtual std::string protocol_name() const = 0;

    /**
     * Check if 0-RTT connection resumption is supported and enabled.
     * @return true if 0-RTT is available
     */
    virtual bool supports_0rtt() const = 0;

    /**
     * Check if unreliable datagrams are supported.
     * @return true if datagrams are available
     */
    virtual bool supports_datagrams() const = 0;
};

}  // namespace robot_agent::interfaces
