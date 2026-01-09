// Copyright 2026 Multi-Robot Supervision System
// TLS Credentials Manager

#pragma once

#include <filesystem>
#include <memory>
#include <optional>
#include <string>

#include <grpcpp/security/credentials.h>

namespace fleet_agent {
namespace transport {

/**
 * TlsCredentials - Manages mTLS certificates for gRPC.
 *
 * Handles loading and validating TLS certificates for
 * mutual TLS authentication with the central server.
 *
 * Certificate files:
 *   - CA certificate (ca.crt)
 *   - Client certificate (client.crt)
 *   - Client private key (client.key)
 */
class TlsCredentials {
public:
    /**
     * TLS configuration.
     */
    struct Config {
        std::filesystem::path ca_cert_path;
        std::filesystem::path client_cert_path;
        std::filesystem::path client_key_path;
        bool verify_server{true};
    };

    /**
     * Constructor.
     *
     * @param config TLS configuration
     */
    explicit TlsCredentials(const Config& config);

    ~TlsCredentials();

    /**
     * Check if credentials are valid and loaded.
     */
    bool is_valid() const;

    /**
     * Get gRPC channel credentials.
     *
     * Returns TLS credentials for secure channel creation.
     *
     * @return Channel credentials, or nullptr if invalid
     */
    std::shared_ptr<grpc::ChannelCredentials> get_channel_credentials() const;

    /**
     * Get error message if credentials failed to load.
     */
    std::string get_error() const;

    /**
     * Create insecure credentials for testing.
     *
     * WARNING: Only use for local development/testing.
     */
    static std::shared_ptr<grpc::ChannelCredentials> create_insecure();

    /**
     * Reload certificates from disk.
     *
     * Useful for certificate rotation without restart.
     *
     * @return true if reload successful
     */
    bool reload();

private:
    Config config_;
    std::shared_ptr<grpc::ChannelCredentials> credentials_;
    std::string error_;
    bool valid_{false};

    /**
     * Load file contents.
     */
    std::optional<std::string> load_file(const std::filesystem::path& path);

    /**
     * Initialize credentials from loaded certificates.
     */
    bool initialize();
};

}  // namespace transport
}  // namespace fleet_agent
