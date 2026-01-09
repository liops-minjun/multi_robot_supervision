#!/bin/bash

# Quick development server startup script
# Runs Go backend and React frontend without Docker

set -e

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

echo -e "${BLUE}Starting development servers...${NC}"

# Cleanup function
cleanup() {
    echo -e "\n${YELLOW}Shutting down...${NC}"
    kill $BACKEND_PID $FRONTEND_PID 2>/dev/null
    exit 0
}
trap cleanup SIGINT SIGTERM

# Start Go backend
echo -e "${GREEN}[1/2] Starting Go backend (port 8081)...${NC}"
cd "$PROJECT_DIR/central_server_go"
go run cmd/server/main.go &
BACKEND_PID=$!

# Start React frontend
echo -e "${GREEN}[2/2] Starting React frontend (port 5173)...${NC}"
cd "$PROJECT_DIR/central_server/frontend"
npm run dev &
FRONTEND_PID=$!

echo -e "\n${GREEN}========================================${NC}"
echo -e "${GREEN}Development servers running:${NC}"
echo -e "  Backend:  http://localhost:8081"
echo -e "  Frontend: http://localhost:5173"
echo -e "${GREEN}========================================${NC}"
echo -e "${YELLOW}Press Ctrl+C to stop all servers${NC}\n"

# Wait for both processes
wait $BACKEND_PID $FRONTEND_PID
