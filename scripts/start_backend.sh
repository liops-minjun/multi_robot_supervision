#!/bin/bash
# Start Go Backend Server (Local Development)

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

# Load environment
source "$SCRIPT_DIR/env_local.sh"

cd "$PROJECT_DIR/central_server_go"

# Build if needed
if [ ! -f fleet-server ] || [ main.go -nt fleet-server ]; then
    echo "Building fleet-server..."
    go build -o fleet-server ./cmd/server/main.go
fi

# Run
echo ""
echo "Starting Fleet Server..."
echo "  API: http://localhost:$HTTP_PORT"
echo "  Health: http://localhost:$HTTP_PORT/health"
echo ""
./fleet-server
