#!/bin/bash
# mTLS Certificate Generation Script for Fleet Server
# This script generates CA, Server, and Agent certificates for mutual TLS authentication

set -e

CERT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DAYS_VALID=365

echo "=== Fleet mTLS Certificate Generator ==="
echo "Output directory: $CERT_DIR"
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m' # No Color

# ============================================================
# 1. Generate CA (Certificate Authority)
# ============================================================
echo -e "${GREEN}[1/3] Generating CA certificate...${NC}"

openssl genrsa -out "$CERT_DIR/ca.key" 4096

openssl req -x509 -new -nodes \
    -key "$CERT_DIR/ca.key" \
    -sha256 \
    -days $DAYS_VALID \
    -out "$CERT_DIR/ca.crt" \
    -subj "/CN=FleetCA/O=Fleet/C=KR"

echo "  Created: ca.key, ca.crt"

# ============================================================
# 2. Generate Server Certificate
# ============================================================
echo -e "${GREEN}[2/3] Generating server certificate...${NC}"

# Create server extension file for SAN (Subject Alternative Names)
cat > "$CERT_DIR/server.ext" << EOF
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = localhost
DNS.2 = fleet-server
DNS.3 = go-backend
DNS.4 = *.local
IP.1 = 127.0.0.1
IP.2 = 0.0.0.0
IP.3 = 192.168.0.200
EOF

openssl genrsa -out "$CERT_DIR/server.key" 2048

openssl req -new \
    -key "$CERT_DIR/server.key" \
    -out "$CERT_DIR/server.csr" \
    -subj "/CN=fleet-server/O=Fleet/C=KR"

openssl x509 -req \
    -in "$CERT_DIR/server.csr" \
    -CA "$CERT_DIR/ca.crt" \
    -CAkey "$CERT_DIR/ca.key" \
    -CAcreateserial \
    -out "$CERT_DIR/server.crt" \
    -days $DAYS_VALID \
    -sha256 \
    -extfile "$CERT_DIR/server.ext"

echo "  Created: server.key, server.crt"

# ============================================================
# 3. Generate Agent Certificate (for Python agent)
# ============================================================
echo -e "${GREEN}[3/3] Generating agent certificate...${NC}"

# Create agent extension file
cat > "$CERT_DIR/agent.ext" << EOF
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth
EOF

openssl genrsa -out "$CERT_DIR/agent.key" 2048

openssl req -new \
    -key "$CERT_DIR/agent.key" \
    -out "$CERT_DIR/agent.csr" \
    -subj "/CN=fleet-agent/O=Fleet/C=KR"

openssl x509 -req \
    -in "$CERT_DIR/agent.csr" \
    -CA "$CERT_DIR/ca.crt" \
    -CAkey "$CERT_DIR/ca.key" \
    -CAcreateserial \
    -out "$CERT_DIR/agent.crt" \
    -days $DAYS_VALID \
    -sha256 \
    -extfile "$CERT_DIR/agent.ext"

echo "  Created: agent.key, agent.crt"

# ============================================================
# Cleanup temporary files
# ============================================================
rm -f "$CERT_DIR"/*.csr "$CERT_DIR"/*.ext "$CERT_DIR"/*.srl

# ============================================================
# Summary
# ============================================================
echo ""
echo -e "${GREEN}=== Certificate Generation Complete ===${NC}"
echo ""
echo "Files generated:"
echo "  CA:     ca.key, ca.crt"
echo "  Server: server.key, server.crt"
echo "  Agent:  agent.key, agent.crt"
echo ""
echo "Usage:"
echo "  - Go Server: Use server.key, server.crt, ca.crt (for client verification)"
echo "  - Python Agent: Use agent.key, agent.crt, ca.crt (for server verification)"
echo ""
echo "Copy agent certificates to robot agent:"
echo "  scp agent.key agent.crt ca.crt robot@<robot-ip>:/etc/fleet_agent/certs/"
echo ""

# Verify certificates
echo "Verifying certificates..."
openssl verify -CAfile "$CERT_DIR/ca.crt" "$CERT_DIR/server.crt"
openssl verify -CAfile "$CERT_DIR/ca.crt" "$CERT_DIR/agent.crt"

echo ""
echo -e "${GREEN}All certificates verified successfully!${NC}"
