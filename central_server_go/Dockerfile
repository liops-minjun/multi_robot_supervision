# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies including protobuf
RUN apk add --no-cache git ca-certificates tzdata protobuf protobuf-dev

# Install Go protobuf plugins
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.2 && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1

WORKDIR /app

# Copy go mod files first (for better caching)
COPY go.mod ./
# Create empty go.sum if it doesn't exist
RUN touch go.sum

# Copy proto files and generate code
COPY pkg/proto/ ./pkg/proto/

# Generate protobuf Go code
RUN protoc \
    --proto_path=./pkg/proto \
    --go_out=./pkg/proto \
    --go_opt=paths=source_relative \
    --go-grpc_out=./pkg/proto \
    --go-grpc_opt=paths=source_relative \
    ./pkg/proto/fleet.proto

# Download dependencies (after proto generation so imports work)
RUN go mod download || true

# Copy remaining source code
COPY . .

# Tidy modules
RUN go mod tidy

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /fleet-server ./cmd/server

# Runtime stage
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /fleet-server /app/fleet-server

# Create non-root user
RUN adduser -D -g '' appuser

# Copy configuration with correct ownership
COPY --chown=appuser:appuser config.yaml /app/config.yaml

# Copy certificates (in production, use secrets or volume mounts)
COPY --chown=appuser:appuser certs/ /app/certs/

USER appuser

# Expose ports
EXPOSE 9090
EXPOSE 8081

# Health check (using gRPC health check would be better in production)
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8081/health || exit 1

# Run
ENTRYPOINT ["/app/fleet-server"]
CMD ["-config", "/app/config.yaml"]
