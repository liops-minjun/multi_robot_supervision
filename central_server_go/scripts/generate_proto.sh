#!/bin/bash
# Generate Go code from Protocol Buffer definitions
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
PROTO_DIR="$PROJECT_ROOT/pkg/proto"

echo "Generating Go code from Protocol Buffers..."
echo "Proto directory: $PROTO_DIR"

# Check if protoc is available
if ! command -v protoc &> /dev/null; then
    echo "Error: protoc is not installed"
    echo "Install with: apt-get install -y protobuf-compiler"
    exit 1
fi

# Check if Go protobuf plugins are available
if ! command -v protoc-gen-go &> /dev/null; then
    echo "Installing protoc-gen-go..."
    go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
fi

if ! command -v protoc-gen-go-grpc &> /dev/null; then
    echo "Installing protoc-gen-go-grpc..."
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
fi

# Ensure GOPATH/bin is in PATH
export PATH="$PATH:$(go env GOPATH)/bin"

# Generate Go code
protoc \
    --proto_path="$PROTO_DIR" \
    --go_out="$PROTO_DIR" \
    --go_opt=paths=source_relative \
    --go-grpc_out="$PROTO_DIR" \
    --go-grpc_opt=paths=source_relative \
    "$PROTO_DIR/fleet.proto"

echo "Generated files:"
ls -la "$PROTO_DIR"/*.pb.go 2>/dev/null || echo "No .pb.go files found"

echo "Done!"
