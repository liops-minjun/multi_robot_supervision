#!/bin/bash
# Start React Frontend (Local Development)

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_DIR/central_server/frontend"

# Install dependencies if needed
if [ ! -d "node_modules" ]; then
    echo "Installing npm dependencies..."
    npm install
fi

echo ""
echo "Starting Frontend..."
echo "  URL: http://localhost:5173"
echo ""
npm run dev
