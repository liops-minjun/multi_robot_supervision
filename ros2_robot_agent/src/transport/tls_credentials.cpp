// Copyright 2026 Multi-Robot Supervision System
// TLS Credentials Implementation

#include "robot_agent/transport/tls_credentials.hpp"
#include "robot_agent/core/logger.hpp"

#include <fstream>
#include <sstream>

namespace robot_agent {
namespace transport {

namespace {
logging::ComponentLogger log("TlsCredentials");
}

TlsCredentials::TlsCredentials(const Config& config)
    : config_(config) {

    initialize();
}

TlsCredentials::~TlsCredentials() = default;

bool TlsCredentials::is_valid() const {
    return valid_;
}

std::shared_ptr<grpc::ChannelCredentials> TlsCredentials::get_channel_credentials() const {
    return credentials_;
}

std::string TlsCredentials::get_error() const {
    return error_;
}

std::shared_ptr<grpc::ChannelCredentials> TlsCredentials::create_insecure() {
    return grpc::InsecureChannelCredentials();
}

bool TlsCredentials::reload() {
    valid_ = false;
    credentials_.reset();
    error_.clear();
    return initialize();
}

std::optional<std::string> TlsCredentials::load_file(const std::filesystem::path& path) {
    if (!std::filesystem::exists(path)) {
        error_ = "File not found: " + path.string();
        log.error("{}", error_);
        return std::nullopt;
    }

    std::ifstream file(path);
    if (!file.is_open()) {
        error_ = "Failed to open file: " + path.string();
        log.error("{}", error_);
        return std::nullopt;
    }

    std::stringstream buffer;
    buffer << file.rdbuf();
    return buffer.str();
}

bool TlsCredentials::initialize() {
    // Load CA certificate
    auto ca_cert = load_file(config_.ca_cert_path);
    if (!ca_cert) {
        return false;
    }

    // Load client certificate
    auto client_cert = load_file(config_.client_cert_path);
    if (!client_cert) {
        return false;
    }

    // Load client private key
    auto client_key = load_file(config_.client_key_path);
    if (!client_key) {
        return false;
    }

    // Build SSL credentials options
    grpc::SslCredentialsOptions ssl_opts;
    ssl_opts.pem_root_certs = *ca_cert;
    ssl_opts.pem_cert_chain = *client_cert;
    ssl_opts.pem_private_key = *client_key;

    // Create credentials
    credentials_ = grpc::SslCredentials(ssl_opts);

    if (!credentials_) {
        error_ = "Failed to create SSL credentials";
        log.error("{}", error_);
        return false;
    }

    valid_ = true;
    log.info("TLS credentials loaded successfully");
    log.debug("  CA cert: {}", config_.ca_cert_path.string());
    log.debug("  Client cert: {}", config_.client_cert_path.string());
    log.debug("  Client key: {}", config_.client_key_path.string());

    return true;
}

}  // namespace transport
}  // namespace robot_agent
