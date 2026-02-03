// Copyright 2026 Multi-Robot Supervision System
// QUIC Transport for gRPC using MsQuic

#pragma once

#include "robot_agent/transport/tls_credentials.hpp"

#include <atomic>
#include <chrono>
#include <functional>
#include <memory>
#include <mutex>
#include <string>
#include <thread>
#include <unordered_map>
#include <vector>
#include <condition_variable>

// MsQuic forward declarations (MsQuic is REQUIRED)
typedef struct QUIC_API_TABLE QUIC_API_TABLE;
typedef struct QUIC_HANDLE QUIC_HANDLE;
typedef QUIC_HANDLE* HQUIC;

namespace robot_agent {
namespace transport {

// ============================================================
// QUIC Configuration
// ============================================================

/**
 * QUIC transport configuration
 */
struct QUICConfig {
    // Connection settings
    std::chrono::milliseconds idle_timeout{30000};
    std::chrono::milliseconds keepalive_interval{10000};
    std::chrono::milliseconds handshake_timeout{10000};
    std::chrono::milliseconds disconnect_timeout{5000};

    // Stream settings
    uint16_t max_bidi_streams{1000};
    uint16_t max_uni_streams{100};

    // Flow control
    uint64_t stream_recv_window{1 * 1024 * 1024};   // 1MB
    uint64_t conn_recv_window{2 * 1024 * 1024};     // 2MB

    // 0-RTT (Resumption)
    bool enable_resumption{true};
    std::string resumption_ticket_path;

    // Datagrams (QUIC unreliable datagrams)
    bool enable_datagrams{true};
    uint16_t max_datagram_size{1200};

    // ALPN protocol identifier (must match server: "fleet-agent-raw")
    std::string alpn{"fleet-agent-raw"};

    // Congestion control
    uint16_t congestion_control{0};  // 0=Cubic, 1=BBR

    // Connection migration
    bool disable_migration{false};

    // MTU
    uint16_t max_udp_payload{1500};
    uint16_t min_mtu{1248};
    uint16_t max_mtu{1500};
};

// ============================================================
// QUIC Stream
// ============================================================

/**
 * Bidirectional QUIC stream for gRPC RPCs
 */
class QUICStream {
public:
    enum class State {
        STARTING,
        OPEN,
        SEND_SHUTDOWN,
        RECV_SHUTDOWN,
        CLOSED
    };

    using DataCallback = std::function<void(const uint8_t*, size_t, bool fin)>;
    using CloseCallback = std::function<void(uint64_t error_code)>;
    using CleanupCallback = std::function<void(uint64_t stream_id)>;

    QUICStream(const QUIC_API_TABLE* msquic, HQUIC stream_handle, uint64_t stream_id);
    ~QUICStream();

    // Stream ID
    uint64_t id() const { return stream_id_; }

    // State
    State state() const { return state_.load(); }
    bool is_open() const { return state_ == State::OPEN; }

    // Blocking write
    bool write(const uint8_t* data, size_t len, bool fin = false);

    // Non-blocking write (queues data)
    bool write_async(const uint8_t* data, size_t len, bool fin = false);

    // Read (returns data from internal buffer)
    size_t read(uint8_t* buf, size_t len);

    // Check if data available
    size_t available() const;

    // Shutdown directions
    void shutdown_send();
    void shutdown_recv();
    void close(uint64_t error_code = 0);

    // Callbacks
    void set_data_callback(DataCallback cb);
    void set_close_callback(CloseCallback cb);
    void set_cleanup_callback(CleanupCallback cb);

    // Internal - called by MsQuic callbacks
    void on_data_received(const uint8_t* data, size_t len, bool fin);
    void on_send_complete(bool cancelled);
    void on_shutdown(uint64_t error_code);

private:
    const QUIC_API_TABLE* msquic_;
    HQUIC handle_;
    uint64_t stream_id_;
    std::atomic<State> state_{State::STARTING};

    // Receive buffer
    std::vector<uint8_t> recv_buffer_;
    mutable std::mutex recv_mutex_;
    std::condition_variable recv_cv_;

    // Send tracking
    std::atomic<bool> send_pending_{false};
    std::condition_variable send_cv_;
    std::mutex send_mutex_;

    DataCallback data_callback_;
    CloseCallback close_callback_;
    CleanupCallback cleanup_callback_;
};

// ============================================================
// QUIC Connection
// ============================================================

/**
 * QUIC connection with stream multiplexing
 */
class QUICConnection {
public:
    enum class State {
        NONE,
        CONNECTING,
        CONNECTED,
        SHUTDOWN_INITIATED,
        SHUTDOWN_COMPLETE,
        CLOSED
    };

    using ConnectedCallback = std::function<void(bool success, const std::string& error)>;
    using StreamCallback = std::function<void(std::shared_ptr<QUICStream>)>;
    using MigrationCallback = std::function<void(const std::string& new_addr)>;
    using DatagramCallback = std::function<void(const uint8_t*, size_t)>;

    QUICConnection(
        const QUIC_API_TABLE* msquic,
        HQUIC registration,
        HQUIC configuration,
        const std::string& server_addr,
        uint16_t server_port,
        const QUICConfig& config
    );
    ~QUICConnection();

    // Connect (async)
    bool connect();

    // Wait for connection (blocking)
    bool wait_connected(std::chrono::milliseconds timeout = std::chrono::milliseconds(10000));

    // Disconnect
    void disconnect(uint64_t error_code = 0);

    // Wait for shutdown to complete (blocking)
    bool wait_shutdown(std::chrono::milliseconds timeout = std::chrono::milliseconds(5000));

    // State
    State state() const { return state_.load(); }
    bool is_connected() const { return state_ == State::CONNECTED; }

    // Open new stream
    std::shared_ptr<QUICStream> open_stream();

    // Datagrams (unreliable)
    bool send_datagram(const uint8_t* data, size_t len);

    // Callbacks
    void set_connected_callback(ConnectedCallback cb);
    void set_stream_callback(StreamCallback cb);
    void set_migration_callback(MigrationCallback cb);
    void set_datagram_callback(DatagramCallback cb);

    // Stats
    struct Stats {
        uint64_t bytes_sent{0};
        uint64_t bytes_received{0};
        uint64_t streams_opened{0};
        uint64_t datagrams_sent{0};
        uint32_t rtt_us{0};
        uint32_t cwnd{0};
    };
    Stats get_stats() const;

    // Address
    std::string local_address() const;
    std::string remote_address() const;

    // Internal - MsQuic callbacks
    void on_connected();
    void on_shutdown_initiated(uint64_t error_code);
    void on_shutdown_complete();
    void on_new_stream(HQUIC stream_handle);
    void on_datagram_received(const uint8_t* data, size_t len);
    void on_resumption_ticket(const uint8_t* ticket, size_t len);

private:
    const QUIC_API_TABLE* msquic_;
    HQUIC registration_;
    HQUIC configuration_;
    HQUIC handle_{nullptr};

    std::string server_addr_;
    uint16_t server_port_;
    QUICConfig config_;

    std::atomic<State> state_{State::NONE};

    // Connection ready signal
    std::mutex connect_mutex_;
    std::condition_variable connect_cv_;
    std::string connect_error_;

    // Streams
    std::unordered_map<uint64_t, std::shared_ptr<QUICStream>> streams_;
    std::mutex streams_mutex_;

    // Callbacks
    ConnectedCallback connected_callback_;
    StreamCallback stream_callback_;
    MigrationCallback migration_callback_;
    DatagramCallback datagram_callback_;

    // Stats
    mutable std::mutex stats_mutex_;
    Stats stats_;
};

// ============================================================
// QUIC Client (MsQuic wrapper)
// ============================================================

/**
 * MsQuic-based QUIC client for gRPC transport
 */
class QUICClient {
public:
    using ConnectionHandler = std::function<void(bool connected)>;

    QUICClient(const QUICConfig& config);
    ~QUICClient();

    // Initialize MsQuic
    bool initialize(
        const std::string& ca_cert_path,
        const std::string& client_cert_path = "",
        const std::string& client_key_path = ""
    );

    // Connect to server
    bool connect(const std::string& server_addr, uint16_t server_port);

    // Disconnect
    void disconnect();

    // Check connection
    bool is_connected() const;

    // Get stream for gRPC communication
    std::shared_ptr<QUICStream> get_stream();

    // Release stream back to pool
    void release_stream(std::shared_ptr<QUICStream> stream);

    // Datagrams
    bool send_datagram(const uint8_t* data, size_t len);
    void set_datagram_callback(std::function<void(const uint8_t*, size_t)> cb);

    // Connection handler
    void set_connection_handler(ConnectionHandler handler);

    // Get underlying connection
    QUICConnection* connection() { return connection_.get(); }

    // Stats
    QUICConnection::Stats get_stats() const;

private:
    QUICConfig config_;

    // MsQuic handles
    const QUIC_API_TABLE* msquic_{nullptr};
    HQUIC registration_{nullptr};
    HQUIC configuration_{nullptr};

    // Connection
    std::unique_ptr<QUICConnection> connection_;

    // Stream pool
    std::vector<std::shared_ptr<QUICStream>> stream_pool_;
    std::mutex pool_mutex_;
    static constexpr size_t MAX_POOL_SIZE = 10;

    // Callbacks
    ConnectionHandler connection_handler_;

    // Initialize configuration
    bool init_configuration(
        const std::string& ca_cert,
        const std::string& client_cert,
        const std::string& client_key
    );

    void cleanup();
};

// ============================================================
// gRPC QUIC Transport Wrapper
// ============================================================

/**
 * Provides net::Conn-like interface for gRPC over QUIC
 */
class QUICGrpcConn {
public:
    QUICGrpcConn(std::shared_ptr<QUICStream> stream);
    ~QUICGrpcConn();

    // net.Conn-like interface
    ssize_t read(uint8_t* buf, size_t len);
    ssize_t write(const uint8_t* data, size_t len);
    void close();

    // Timeout support
    void set_read_deadline(std::chrono::steady_clock::time_point deadline);
    void set_write_deadline(std::chrono::steady_clock::time_point deadline);

    // Address info
    std::string local_addr() const;
    std::string remote_addr() const;

private:
    std::shared_ptr<QUICStream> stream_;
    std::chrono::steady_clock::time_point read_deadline_;
    std::chrono::steady_clock::time_point write_deadline_;
};

}  // namespace transport
}  // namespace robot_agent
