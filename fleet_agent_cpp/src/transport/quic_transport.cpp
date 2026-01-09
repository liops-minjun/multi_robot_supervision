// Copyright 2026 Multi-Robot Supervision System
// QUIC Transport Implementation using MsQuic

#include "fleet_agent/transport/quic_transport.hpp"
#include "fleet_agent/core/logger.hpp"

#include <msquic.h>

#include <cstring>
#include <fstream>

namespace fleet_agent {
namespace transport {

namespace {
logging::ComponentLogger log("QUICTransport");

// Forward declarations for MsQuic callbacks
QUIC_STATUS QUIC_API InternalConnectionCallback(
    HQUIC connection,
    void* context,
    QUIC_CONNECTION_EVENT* event);

QUIC_STATUS QUIC_API InternalStreamCallback(
    HQUIC stream,
    void* context,
    QUIC_STREAM_EVENT* event);

// Callback function pointers
QUIC_STREAM_CALLBACK_HANDLER g_stream_callback = InternalStreamCallback;
QUIC_CONNECTION_CALLBACK_HANDLER g_connection_callback = InternalConnectionCallback;

}  // namespace

// ============================================================
// QUICStream Implementation
// ============================================================

QUICStream::QUICStream(const QUIC_API_TABLE* msquic, HQUIC stream_handle, uint64_t stream_id)
    : msquic_(msquic)
    , handle_(stream_handle)
    , stream_id_(stream_id) {

    state_ = State::OPEN;
    log.debug("Stream {} created", stream_id_);
}

QUICStream::~QUICStream() {
    close();
}

bool QUICStream::write(const uint8_t* data, size_t len, bool fin) {
    if (!is_open() || !handle_ || !msquic_) {
        log.error("Stream write failed: stream not ready (open={}, handle={}, msquic={})",
                  is_open(), handle_ != nullptr, msquic_ != nullptr);
        return false;
    }

    // Allocate buffer that MsQuic will own
    QUIC_BUFFER* buffer = new QUIC_BUFFER;
    buffer->Length = static_cast<uint32_t>(len);
    buffer->Buffer = new uint8_t[len];
    std::memcpy(buffer->Buffer, data, len);

    QUIC_SEND_FLAGS flags = fin ? QUIC_SEND_FLAG_FIN : QUIC_SEND_FLAG_NONE;

    // Mark send pending
    send_pending_ = true;

    // Send data via MsQuic
    QUIC_STATUS status = msquic_->StreamSend(handle_, buffer, 1, flags, buffer);
    if (QUIC_FAILED(status)) {
        log.error("StreamSend failed: 0x{:x}", status);
        delete[] buffer->Buffer;
        delete buffer;
        send_pending_ = false;
        return false;
    }

    log.debug("Stream {} sent {} bytes (fin={})", stream_id_, len, fin);

    // Wait for send complete callback
    {
        std::unique_lock<std::mutex> lock(send_mutex_);
        if (!send_cv_.wait_for(lock, std::chrono::seconds(10), [this] { return !send_pending_; })) {
            log.warn("Stream {} send timeout", stream_id_);
            return false;
        }
    }

    return true;
}

bool QUICStream::write_async(const uint8_t* data, size_t len, bool fin) {
    if (!is_open() || !handle_ || !msquic_) {
        return false;
    }

    QUIC_BUFFER* buffer = new QUIC_BUFFER;
    buffer->Length = static_cast<uint32_t>(len);
    buffer->Buffer = new uint8_t[len];
    std::memcpy(buffer->Buffer, data, len);

    QUIC_SEND_FLAGS flags = fin ? QUIC_SEND_FLAG_FIN : QUIC_SEND_FLAG_NONE;

    // Non-blocking send
    QUIC_STATUS status = msquic_->StreamSend(handle_, buffer, 1, flags, buffer);
    if (QUIC_FAILED(status)) {
        log.error("StreamSend (async) failed: 0x{:x}", status);
        delete[] buffer->Buffer;
        delete buffer;
        return false;
    }

    return true;
}

size_t QUICStream::read(uint8_t* buf, size_t len) {
    std::lock_guard<std::mutex> lock(recv_mutex_);

    if (recv_buffer_.empty()) {
        return 0;
    }

    size_t to_read = std::min(len, recv_buffer_.size());
    std::memcpy(buf, recv_buffer_.data(), to_read);
    recv_buffer_.erase(recv_buffer_.begin(), recv_buffer_.begin() + to_read);

    return to_read;
}

size_t QUICStream::available() const {
    std::lock_guard<std::mutex> lock(recv_mutex_);
    return recv_buffer_.size();
}

void QUICStream::shutdown_send() {
    if (handle_) {
        // msquic->StreamShutdown(handle_, QUIC_STREAM_SHUTDOWN_FLAG_GRACEFUL, 0);
    }
    if (state_ == State::OPEN) {
        state_ = State::SEND_SHUTDOWN;
    }
}

void QUICStream::shutdown_recv() {
    if (handle_) {
        // msquic->StreamShutdown(handle_, QUIC_STREAM_SHUTDOWN_FLAG_ABORT_RECEIVE, 0);
    }
    if (state_ == State::OPEN) {
        state_ = State::RECV_SHUTDOWN;
    }
}

void QUICStream::close(uint64_t error_code) {
    if (state_ == State::CLOSED) {
        return;
    }

    if (handle_) {
        // msquic->StreamClose(handle_);
        handle_ = nullptr;
    }

    state_ = State::CLOSED;
    log.debug("Stream {} closed", stream_id_);

    if (close_callback_) {
        close_callback_(error_code);
    }
}

void QUICStream::set_data_callback(DataCallback cb) {
    data_callback_ = std::move(cb);
}

void QUICStream::set_close_callback(CloseCallback cb) {
    close_callback_ = std::move(cb);
}

void QUICStream::on_data_received(const uint8_t* data, size_t len, bool fin) {
    {
        std::lock_guard<std::mutex> lock(recv_mutex_);
        recv_buffer_.insert(recv_buffer_.end(), data, data + len);
    }
    recv_cv_.notify_all();

    if (data_callback_) {
        data_callback_(data, len, fin);
    }

    if (fin) {
        state_ = State::RECV_SHUTDOWN;
    }
}

void QUICStream::on_send_complete(bool cancelled) {
    send_pending_ = false;
    send_cv_.notify_all();

    if (cancelled) {
        log.warn("Stream {} send cancelled", stream_id_);
    }
}

void QUICStream::on_shutdown(uint64_t error_code) {
    state_ = State::CLOSED;
    if (close_callback_) {
        close_callback_(error_code);
    }
}

// ============================================================
// QUICConnection Implementation
// ============================================================

QUICConnection::QUICConnection(
    const QUIC_API_TABLE* msquic,
    HQUIC registration,
    HQUIC configuration,
    const std::string& server_addr,
    uint16_t server_port,
    const QUICConfig& config)
    : msquic_(msquic)
    , registration_(registration)
    , configuration_(configuration)
    , server_addr_(server_addr)
    , server_port_(server_port)
    , config_(config) {

    log.info("Creating QUIC connection to {}:{}", server_addr, server_port);
}

QUICConnection::~QUICConnection() {
    disconnect();
}

bool QUICConnection::connect() {
    if (!msquic_ || !configuration_) {
        log.error("MsQuic not initialized");
        return false;
    }

    state_ = State::CONNECTING;

    // Open connection
    QUIC_STATUS status = msquic_->ConnectionOpen(
        registration_,
        g_connection_callback,
        this,
        &handle_);

    if (QUIC_FAILED(status)) {
        log.error("ConnectionOpen failed: 0x{:x}", status);
        state_ = State::CLOSED;
        return false;
    }

    // Enable datagrams if configured
    if (config_.enable_datagrams) {
        BOOLEAN datagram_enabled = TRUE;
        msquic_->SetParam(
            handle_,
            QUIC_PARAM_CONN_DATAGRAM_RECEIVE_ENABLED,
            sizeof(datagram_enabled),
            &datagram_enabled);
    }

    // Start connection
    status = msquic_->ConnectionStart(
        handle_,
        configuration_,
        QUIC_ADDRESS_FAMILY_UNSPEC,
        server_addr_.c_str(),
        server_port_);

    if (QUIC_FAILED(status)) {
        log.error("ConnectionStart failed: 0x{:x}", status);
        msquic_->ConnectionClose(handle_);
        handle_ = nullptr;
        state_ = State::CLOSED;
        return false;
    }

    log.info("QUIC connection initiated to {}:{}", server_addr_, server_port_);
    return true;
}

bool QUICConnection::wait_connected(std::chrono::milliseconds timeout) {
    std::unique_lock<std::mutex> lock(connect_mutex_);

    bool result = connect_cv_.wait_for(lock, timeout, [this] {
        return state_ == State::CONNECTED ||
               state_ == State::CLOSED ||
               state_ == State::SHUTDOWN_COMPLETE;
    });

    if (!result) {
        log.error("Connection timeout");
        return false;
    }

    if (state_ != State::CONNECTED) {
        log.error("Connection failed: {}", connect_error_);
        return false;
    }

    return true;
}

void QUICConnection::disconnect(uint64_t error_code) {
    if (state_ == State::CLOSED || state_ == State::SHUTDOWN_COMPLETE) {
        return;
    }

    state_ = State::SHUTDOWN_INITIATED;

    if (handle_ && msquic_) {
        msquic_->ConnectionShutdown(
            handle_,
            QUIC_CONNECTION_SHUTDOWN_FLAG_NONE,
            error_code);
    }
}

std::shared_ptr<QUICStream> QUICConnection::open_stream() {
    if (!is_connected() || !handle_) {
        return nullptr;
    }

    HQUIC stream_handle = nullptr;

    QUIC_STATUS status = msquic_->StreamOpen(
        handle_,
        QUIC_STREAM_OPEN_FLAG_NONE,
        g_stream_callback,
        nullptr,  // Context set after creation
        &stream_handle);

    if (QUIC_FAILED(status)) {
        log.error("StreamOpen failed: 0x{:x}", status);
        return nullptr;
    }

    // Get stream ID
    uint64_t stream_id = 0;
    uint32_t buf_len = sizeof(stream_id);
    msquic_->GetParam(
        stream_handle,
        QUIC_PARAM_STREAM_ID,
        &buf_len,
        &stream_id);

    auto stream = std::make_shared<QUICStream>(msquic_, stream_handle, stream_id);

    // Set stream context for callbacks
    msquic_->SetCallbackHandler(stream_handle, reinterpret_cast<void*>(g_stream_callback), stream.get());

    // Start stream
    status = msquic_->StreamStart(
        stream_handle,
        QUIC_STREAM_START_FLAG_NONE);

    if (QUIC_FAILED(status)) {
        log.error("StreamStart failed: 0x{:x}", status);
        msquic_->StreamClose(stream_handle);
        return nullptr;
    }

    // Store stream
    {
        std::lock_guard<std::mutex> lock(streams_mutex_);
        streams_[stream_id] = stream;
        stats_.streams_opened++;
    }

    log.debug("Opened stream {}", stream_id);
    return stream;
}

bool QUICConnection::send_datagram(const uint8_t* data, size_t len) {
    if (!is_connected() || !handle_ || !config_.enable_datagrams) {
        return false;
    }

    QUIC_BUFFER buffer;
    buffer.Length = static_cast<uint32_t>(len);
    buffer.Buffer = const_cast<uint8_t*>(data);

    QUIC_STATUS status = msquic_->DatagramSend(
        handle_,
        &buffer,
        1,
        QUIC_SEND_FLAG_NONE,
        nullptr);

    if (QUIC_SUCCEEDED(status)) {
        std::lock_guard<std::mutex> lock(stats_mutex_);
        stats_.datagrams_sent++;
    }

    return QUIC_SUCCEEDED(status);
}

void QUICConnection::set_connected_callback(ConnectedCallback cb) {
    connected_callback_ = std::move(cb);
}

void QUICConnection::set_stream_callback(StreamCallback cb) {
    stream_callback_ = std::move(cb);
}

void QUICConnection::set_migration_callback(MigrationCallback cb) {
    migration_callback_ = std::move(cb);
}

void QUICConnection::set_datagram_callback(DatagramCallback cb) {
    datagram_callback_ = std::move(cb);
}

QUICConnection::Stats QUICConnection::get_stats() const {
    std::lock_guard<std::mutex> lock(stats_mutex_);

    if (handle_ && msquic_) {
        QUIC_STATISTICS stats;
        uint32_t stats_len = sizeof(stats);
        if (QUIC_SUCCEEDED(msquic_->GetParam(
                handle_,
                QUIC_PARAM_CONN_STATISTICS,
                &stats_len,
                &stats))) {
            Stats result = stats_;
            result.bytes_sent = stats.Send.TotalBytes;
            result.bytes_received = stats.Recv.TotalBytes;
            result.rtt_us = stats.Rtt;  // MsQuic 2.2: Rtt is at top level, not Timing.SmoothedRtt
            result.cwnd = 0;  // MsQuic 2.2: CongestionWindow not in QUIC_STATISTICS
            return result;
        }
    }

    return stats_;
}

std::string QUICConnection::local_address() const {
    if (!handle_ || !msquic_) return "";

    QUIC_ADDR addr;
    uint32_t addr_len = sizeof(addr);
    if (QUIC_FAILED(msquic_->GetParam(
            handle_,
            QUIC_PARAM_CONN_LOCAL_ADDRESS,
            &addr_len,
            &addr))) {
        return "";
    }

    char buf[64];
    // Convert address to string
    // QuicAddrToString(&addr, buf, sizeof(buf));
    return std::string(buf);
}

std::string QUICConnection::remote_address() const {
    return server_addr_ + ":" + std::to_string(server_port_);
}

void QUICConnection::on_connected() {
    state_ = State::CONNECTED;
    log.info("QUIC connection established");

    connect_cv_.notify_all();

    if (connected_callback_) {
        connected_callback_(true, "");
    }
}

void QUICConnection::on_shutdown_initiated(uint64_t error_code) {
    state_ = State::SHUTDOWN_INITIATED;
    log.info("QUIC shutdown initiated: {}", error_code);

    connect_error_ = "Shutdown: " + std::to_string(error_code);
    connect_cv_.notify_all();
}

void QUICConnection::on_shutdown_complete() {
    state_ = State::SHUTDOWN_COMPLETE;

    if (handle_ && msquic_) {
        msquic_->ConnectionClose(handle_);
        handle_ = nullptr;
    }

    state_ = State::CLOSED;
    log.info("QUIC connection closed");

    connect_cv_.notify_all();

    if (connected_callback_) {
        connected_callback_(false, "Connection closed");
    }
}

void QUICConnection::on_new_stream(HQUIC stream_handle) {
    uint64_t stream_id = 0;
    uint32_t buf_len = sizeof(stream_id);
    msquic_->GetParam(
        stream_handle,
        QUIC_PARAM_STREAM_ID,
        &buf_len,
        &stream_id);

    auto stream = std::make_shared<QUICStream>(msquic_, stream_handle, stream_id);
    msquic_->SetCallbackHandler(stream_handle, reinterpret_cast<void*>(g_stream_callback), stream.get());

    {
        std::lock_guard<std::mutex> lock(streams_mutex_);
        streams_[stream_id] = stream;
    }

    if (stream_callback_) {
        stream_callback_(stream);
    }
}

void QUICConnection::on_datagram_received(const uint8_t* data, size_t len) {
    if (datagram_callback_) {
        datagram_callback_(data, len);
    }
}

void QUICConnection::on_resumption_ticket(const uint8_t* ticket, size_t len) {
    if (!config_.resumption_ticket_path.empty()) {
        std::ofstream file(config_.resumption_ticket_path, std::ios::binary);
        if (file.is_open()) {
            file.write(reinterpret_cast<const char*>(ticket), len);
            log.debug("Resumption ticket saved");
        }
    }
}

// ============================================================
// MsQuic Callbacks (inside anonymous namespace)
// ============================================================

namespace {

QUIC_STATUS QUIC_API InternalConnectionCallback(
    HQUIC connection,
    void* context,
    QUIC_CONNECTION_EVENT* event) {

    auto* conn = static_cast<QUICConnection*>(context);

    switch (event->Type) {
        case QUIC_CONNECTION_EVENT_CONNECTED:
            conn->on_connected();
            break;

        case QUIC_CONNECTION_EVENT_SHUTDOWN_INITIATED_BY_TRANSPORT:
            conn->on_shutdown_initiated(event->SHUTDOWN_INITIATED_BY_TRANSPORT.Status);
            break;

        case QUIC_CONNECTION_EVENT_SHUTDOWN_INITIATED_BY_PEER:
            conn->on_shutdown_initiated(event->SHUTDOWN_INITIATED_BY_PEER.ErrorCode);
            break;

        case QUIC_CONNECTION_EVENT_SHUTDOWN_COMPLETE:
            conn->on_shutdown_complete();
            break;

        case QUIC_CONNECTION_EVENT_PEER_STREAM_STARTED:
            conn->on_new_stream(event->PEER_STREAM_STARTED.Stream);
            // Accept the stream
            return QUIC_STATUS_SUCCESS;

        case QUIC_CONNECTION_EVENT_DATAGRAM_RECEIVED:
            conn->on_datagram_received(
                event->DATAGRAM_RECEIVED.Buffer->Buffer,
                event->DATAGRAM_RECEIVED.Buffer->Length);
            break;

        case QUIC_CONNECTION_EVENT_RESUMPTION_TICKET_RECEIVED:
            conn->on_resumption_ticket(
                event->RESUMPTION_TICKET_RECEIVED.ResumptionTicket,
                event->RESUMPTION_TICKET_RECEIVED.ResumptionTicketLength);
            break;

        default:
            break;
    }

    return QUIC_STATUS_SUCCESS;
}

QUIC_STATUS QUIC_API InternalStreamCallback(
    HQUIC stream,
    void* context,
    QUIC_STREAM_EVENT* event) {

    auto* s = static_cast<QUICStream*>(context);

    switch (event->Type) {
        case QUIC_STREAM_EVENT_RECEIVE:
            for (uint32_t i = 0; i < event->RECEIVE.BufferCount; i++) {
                s->on_data_received(
                    event->RECEIVE.Buffers[i].Buffer,
                    event->RECEIVE.Buffers[i].Length,
                    event->RECEIVE.Flags & QUIC_RECEIVE_FLAG_FIN);
            }
            break;

        case QUIC_STREAM_EVENT_SEND_COMPLETE:
            s->on_send_complete(event->SEND_COMPLETE.Canceled);
            // Free the buffer
            if (event->SEND_COMPLETE.ClientContext) {
                auto* buffer = static_cast<QUIC_BUFFER*>(event->SEND_COMPLETE.ClientContext);
                delete[] buffer->Buffer;
                delete buffer;
            }
            break;

        case QUIC_STREAM_EVENT_PEER_SEND_SHUTDOWN:
            s->on_data_received(nullptr, 0, true);
            break;

        case QUIC_STREAM_EVENT_PEER_SEND_ABORTED:
        case QUIC_STREAM_EVENT_PEER_RECEIVE_ABORTED:
            s->on_shutdown(event->PEER_SEND_ABORTED.ErrorCode);
            break;

        case QUIC_STREAM_EVENT_SHUTDOWN_COMPLETE:
            s->on_shutdown(0);
            break;

        default:
            break;
    }

    return QUIC_STATUS_SUCCESS;
}

}  // namespace (callbacks)

// ============================================================
// QUICClient Implementation
// ============================================================

QUICClient::QUICClient(const QUICConfig& config)
    : config_(config) {
}

QUICClient::~QUICClient() {
    cleanup();
}

bool QUICClient::initialize(
    const std::string& ca_cert_path,
    const std::string& client_cert_path,
    const std::string& client_key_path) {

    // Open MsQuic library
    QUIC_STATUS status = MsQuicOpen2(&msquic_);
    if (QUIC_FAILED(status)) {
        log.error("MsQuicOpen2 failed: 0x{:x}", status);
        return false;
    }

    // Create registration
    QUIC_REGISTRATION_CONFIG reg_config = {
        "FleetAgent",
        QUIC_EXECUTION_PROFILE_LOW_LATENCY
    };

    status = msquic_->RegistrationOpen(&reg_config, &registration_);
    if (QUIC_FAILED(status)) {
        log.error("RegistrationOpen failed: 0x{:x}", status);
        cleanup();
        return false;
    }

    // Initialize configuration
    if (!init_configuration(ca_cert_path, client_cert_path, client_key_path)) {
        cleanup();
        return false;
    }

    log.info("MsQuic initialized successfully");
    return true;
}

bool QUICClient::init_configuration(
    const std::string& ca_cert,
    const std::string& client_cert,
    const std::string& client_key) {

    // ALPN
    QUIC_BUFFER alpn;
    alpn.Length = static_cast<uint32_t>(config_.alpn.size());
    alpn.Buffer = reinterpret_cast<uint8_t*>(const_cast<char*>(config_.alpn.c_str()));

    // Settings
    QUIC_SETTINGS settings = {};
    settings.IsSet.IdleTimeoutMs = TRUE;
    settings.IdleTimeoutMs = static_cast<uint64_t>(config_.idle_timeout.count());

    settings.IsSet.PeerBidiStreamCount = TRUE;
    settings.PeerBidiStreamCount = config_.max_bidi_streams;

    settings.IsSet.PeerUnidiStreamCount = TRUE;
    settings.PeerUnidiStreamCount = config_.max_uni_streams;

    settings.IsSet.DatagramReceiveEnabled = TRUE;
    settings.DatagramReceiveEnabled = config_.enable_datagrams;

    settings.IsSet.KeepAliveIntervalMs = TRUE;
    settings.KeepAliveIntervalMs = static_cast<uint32_t>(config_.keepalive_interval.count());

    settings.IsSet.CongestionControlAlgorithm = TRUE;
    settings.CongestionControlAlgorithm = static_cast<QUIC_CONGESTION_CONTROL_ALGORITHM>(config_.congestion_control);

    // Open configuration
    QUIC_STATUS status = msquic_->ConfigurationOpen(
        registration_,
        &alpn,
        1,
        &settings,
        sizeof(settings),
        nullptr,
        &configuration_);

    if (QUIC_FAILED(status)) {
        log.error("ConfigurationOpen failed: 0x{:x}", status);
        return false;
    }

    // TLS credentials
    QUIC_CREDENTIAL_CONFIG cred_config = {};
    cred_config.Type = QUIC_CREDENTIAL_TYPE_NONE;
    cred_config.Flags = QUIC_CREDENTIAL_FLAG_CLIENT;

    // IMPORTANT: cert_file must persist until after ConfigurationLoadCredential
    QUIC_CERTIFICATE_FILE cert_file = {};

    if (!ca_cert.empty()) {
        // Use certificate validation
        cred_config.Flags |= QUIC_CREDENTIAL_FLAG_SET_CA_CERTIFICATE_FILE;
        cred_config.CaCertificateFile = ca_cert.c_str();
        log.info("QUIC TLS: Server certificate validation enabled");
    } else {
        // No server validation (for testing only!)
        cred_config.Flags |= QUIC_CREDENTIAL_FLAG_NO_CERTIFICATE_VALIDATION;
        log.warn("QUIC TLS: Server certificate validation DISABLED - "
                 "SECURITY RISK! Provide CA certificate for production use.");
    }

    if (!client_cert.empty() && !client_key.empty()) {
        // Client certificate for mTLS
        cred_config.Type = QUIC_CREDENTIAL_TYPE_CERTIFICATE_FILE;
        cert_file.CertificateFile = client_cert.c_str();
        cert_file.PrivateKeyFile = client_key.c_str();
        cred_config.CertificateFile = &cert_file;
        log.info("QUIC TLS: Client certificate for mTLS: {}", client_cert);
    }

    log.debug("Loading QUIC credentials (type={}, flags=0x{:x})",
              static_cast<int>(cred_config.Type), cred_config.Flags);
    status = msquic_->ConfigurationLoadCredential(configuration_, &cred_config);
    if (QUIC_FAILED(status)) {
        log.error("ConfigurationLoadCredential failed: 0x{:x}", status);
        return false;
    }

    return true;
}

bool QUICClient::connect(const std::string& server_addr, uint16_t server_port) {
    if (!msquic_ || !configuration_) {
        log.error("MsQuic not initialized");
        return false;
    }

    // Create connection
    connection_ = std::make_unique<QUICConnection>(
        msquic_,
        registration_,
        configuration_,
        server_addr,
        server_port,
        config_);

    // Set callbacks
    connection_->set_connected_callback([this](bool success, const std::string& error) {
        if (connection_handler_) {
            connection_handler_(success);
        }
    });

    // Connect
    if (!connection_->connect()) {
        connection_.reset();
        return false;
    }

    // Wait for connection
    if (!connection_->wait_connected(config_.handshake_timeout)) {
        connection_.reset();
        return false;
    }

    return true;
}

void QUICClient::disconnect() {
    if (connection_) {
        connection_->disconnect();
        connection_.reset();
    }
}

bool QUICClient::is_connected() const {
    return connection_ && connection_->is_connected();
}

std::shared_ptr<QUICStream> QUICClient::get_stream() {
    // Try pool first
    {
        std::lock_guard<std::mutex> lock(pool_mutex_);
        if (!stream_pool_.empty()) {
            auto stream = stream_pool_.back();
            stream_pool_.pop_back();
            if (stream->is_open()) {
                return stream;
            }
        }
    }

    // Open new stream
    if (connection_) {
        return connection_->open_stream();
    }

    return nullptr;
}

void QUICClient::release_stream(std::shared_ptr<QUICStream> stream) {
    if (!stream || !stream->is_open()) {
        return;
    }

    std::lock_guard<std::mutex> lock(pool_mutex_);
    if (stream_pool_.size() < MAX_POOL_SIZE) {
        stream_pool_.push_back(stream);
    }
}

bool QUICClient::send_datagram(const uint8_t* data, size_t len) {
    if (connection_) {
        return connection_->send_datagram(data, len);
    }
    return false;
}

void QUICClient::set_datagram_callback(std::function<void(const uint8_t*, size_t)> cb) {
    if (connection_) {
        connection_->set_datagram_callback(cb);
    }
}

void QUICClient::set_connection_handler(ConnectionHandler handler) {
    connection_handler_ = std::move(handler);
}

QUICConnection::Stats QUICClient::get_stats() const {
    if (connection_) {
        return connection_->get_stats();
    }
    return {};
}

void QUICClient::cleanup() {
    disconnect();

    if (configuration_ && msquic_) {
        msquic_->ConfigurationClose(configuration_);
        configuration_ = nullptr;
    }

    if (registration_ && msquic_) {
        msquic_->RegistrationClose(registration_);
        registration_ = nullptr;
    }

    if (msquic_) {
        MsQuicClose(msquic_);
        msquic_ = nullptr;
    }
}

// ============================================================
// QUICGrpcConn Implementation
// ============================================================

QUICGrpcConn::QUICGrpcConn(std::shared_ptr<QUICStream> stream)
    : stream_(stream) {
}

QUICGrpcConn::~QUICGrpcConn() {
    close();
}

ssize_t QUICGrpcConn::read(uint8_t* buf, size_t len) {
    if (!stream_ || !stream_->is_open()) {
        return -1;
    }

    // TODO: Implement deadline support
    return static_cast<ssize_t>(stream_->read(buf, len));
}

ssize_t QUICGrpcConn::write(const uint8_t* data, size_t len) {
    if (!stream_ || !stream_->is_open()) {
        return -1;
    }

    if (stream_->write(data, len)) {
        return static_cast<ssize_t>(len);
    }
    return -1;
}

void QUICGrpcConn::close() {
    if (stream_) {
        stream_->close();
    }
}

void QUICGrpcConn::set_read_deadline(std::chrono::steady_clock::time_point deadline) {
    read_deadline_ = deadline;
}

void QUICGrpcConn::set_write_deadline(std::chrono::steady_clock::time_point deadline) {
    write_deadline_ = deadline;
}

std::string QUICGrpcConn::local_addr() const {
    return "";  // Would need connection reference
}

std::string QUICGrpcConn::remote_addr() const {
    return "";  // Would need connection reference
}

}  // namespace transport
}  // namespace fleet_agent
