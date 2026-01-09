#!/bin/bash

# Local Development Script (Minimal Docker)
# - Docker: Neo4j only
# - Local: Go Backend + React Frontend

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
cd "$PROJECT_DIR"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

echo -e "${CYAN}╔═══════════════════════════════════════════════════════════════╗"
echo -e "║         Local Development (Minimal Docker)                    ║"
echo -e "╚═══════════════════════════════════════════════════════════════╝${NC}"

# Cleanup on exit
PIDS=""
cleanup() {
    echo -e "\n${YELLOW}Shutting down...${NC}"
    # Kill background processes
    for pid in $PIDS; do
        kill $pid 2>/dev/null || true
    done
    # Stop Docker containers
    docker stop fleet-neo4j 2>/dev/null || true
    echo -e "${GREEN}Cleanup complete${NC}"
}
trap cleanup EXIT INT TERM

# ============================================================
# Step 1: Check dependencies
# ============================================================
echo -e "\n${BLUE}[1/4] Checking dependencies...${NC}"

if ! command -v docker &> /dev/null; then
    echo -e "${RED}Error: Docker not found${NC}"
    exit 1
fi

if ! command -v go &> /dev/null; then
    echo -e "${YELLOW}Go not found. Installing...${NC}"
    # Install Go
    GO_VERSION="1.22.0"
    wget -q "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -O /tmp/go.tar.gz
    sudo rm -rf /usr/local/go
    sudo tar -C /usr/local -xzf /tmp/go.tar.gz
    export PATH=$PATH:/usr/local/go/bin
    echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
    echo -e "${GREEN}Go installed: $(go version)${NC}"
fi

if ! command -v node &> /dev/null; then
    echo -e "${RED}Error: Node.js not found${NC}"
    exit 1
fi

echo -e "${GREEN}[✓] Dependencies OK${NC}"
echo -e "    Go: $(go version 2>/dev/null | cut -d' ' -f3 || echo 'will install')"
echo -e "    Node: $(node --version)"
echo -e "    Docker: $(docker --version | cut -d' ' -f3)"

# ============================================================
# Step 2: Start Neo4j
# ============================================================
echo -e "\n${BLUE}[2/4] Starting Neo4j...${NC}"

# Stop if already running
docker stop fleet-neo4j 2>/dev/null || true
docker rm fleet-neo4j 2>/dev/null || true

docker run -d \
    --name fleet-neo4j \
    -e NEO4J_AUTH=neo4j/neo4j123 \
    -p 7474:7474 \
    -p 7687:7687 \
    neo4j:5

# Wait for Neo4j
echo -n "    Waiting for Neo4j"
for i in {1..30}; do
    if docker exec fleet-neo4j cypher-shell -u neo4j -p neo4j123 "RETURN 1" &>/dev/null; then
        echo -e " ${GREEN}[✓]${NC}"
        break
    fi
    echo -n "."
    sleep 1
done

# ============================================================
# Step 3: Start Go Backend
# ============================================================
echo -e "\n${BLUE}[3/4] Starting Go Backend...${NC}"

cd "$PROJECT_DIR/central_server_go"

# Set environment variables
export NEO4J_URI=neo4j://localhost:7687
export NEO4J_USER=neo4j
export NEO4J_PASSWORD=neo4j123
export NEO4J_DATABASE=neo4j
export HTTP_PORT=8081
export GRPC_PORT=9090

# Build and run
go build -o fleet-server ./cmd/server/main.go
./fleet-server &
BACKEND_PID=$!
PIDS="$PIDS $BACKEND_PID"

# Wait for backend
echo -n "    Waiting for Backend"
for i in {1..30}; do
    if curl -s http://localhost:8081/health &>/dev/null; then
        echo -e " ${GREEN}[✓]${NC}"
        break
    fi
    echo -n "."
    sleep 1
done

# ============================================================
# Step 4: Start React Frontend
# ============================================================
echo -e "\n${BLUE}[4/4] Starting React Frontend...${NC}"

cd "$PROJECT_DIR/central_server/frontend"

# Install dependencies if needed
if [ ! -d "node_modules" ]; then
    echo "    Installing npm dependencies..."
    npm install
fi

# Start frontend
npm run dev &
FRONTEND_PID=$!
PIDS="$PIDS $FRONTEND_PID"

sleep 3
echo -e "    ${GREEN}[✓] Frontend starting${NC}"

# ============================================================
# Done!
# ============================================================
echo -e "\n${GREEN}╔═══════════════════════════════════════════════════════════════╗"
echo -e "║              Development Environment Ready!                    ║"
echo -e "╚═══════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "${CYAN}Services:${NC}"
echo -e "  ${GREEN}●${NC} Frontend:      http://localhost:5173"
echo -e "  ${GREEN}●${NC} Backend API:   http://localhost:8081"
echo -e "  ${GREEN}●${NC} Health Check:  http://localhost:8081/health"
echo -e "  ${GREEN}●${NC} gRPC (TCP):   localhost:9090"
echo -e "  ${GREEN}●${NC} QUIC (UDP):   localhost:9443"
echo -e "  ${GREEN}●${NC} Raw QUIC:     localhost:9444"
echo -e "  ${GREEN}●${NC} Neo4j Browser: http://localhost:7474"
echo -e "  ${GREEN}●${NC} Neo4j Bolt:    localhost:7687"
echo ""
echo -e "${CYAN}To test Fleet Agent (C++):${NC}"
echo -e "  cd fleet_agent_cpp && colcon build"
echo -e "  source install/setup.bash"
echo -e "  ros2 run fleet_agent fleet_agent_node"
echo ""
echo -e "${YELLOW}Press Ctrl+C to stop all services${NC}"
echo ""

# Wait
wait
