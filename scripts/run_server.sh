#!/bin/bash

# Multi-Robot Supervision System - Server Start Script
# Docker Compose로 전체 스택 실행:
#   - Neo4j (database)
#   - Go Backend (REST API + gRPC/QUIC)
#   - React Frontend (:3000)

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
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
echo -e "║           Multi-Robot Fleet Management Server                 ║"
echo -e "╚═══════════════════════════════════════════════════════════════╝${NC}"

# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
    echo -e "${RED}Error: Docker is not running!${NC}"
    echo -e "${YELLOW}Please start Docker and try again.${NC}"
    exit 1
fi

# Parse arguments
BUILD_FLAG=""
DETACH_FLAG="-d"
LOGS_FLAG=""
CLEAN_FLAG=""

for arg in "$@"; do
    case $arg in
        --build)
            BUILD_FLAG="--build"
            ;;
        --foreground|-f)
            DETACH_FLAG=""
            ;;
        --logs|-l)
            LOGS_FLAG="true"
            ;;
        --clean|-c)
            CLEAN_FLAG="true"
            ;;
        --help|-h)
            echo "Usage: ./run_server.sh [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --build        Rebuild Docker images"
            echo "  --clean, -c    Clean up containers before starting"
            echo "  --foreground   Run in foreground (not detached)"
            echo "  --logs, -l     Show logs after starting"
            echo "  --help, -h     Show this help"
            exit 0
            ;;
    esac
done

# Clean up stale containers if requested or if there might be issues
cleanup_containers() {
    echo -e "${YELLOW}Cleaning up stale containers...${NC}"
    docker ps -a --filter "name=fleet" -q | xargs -r docker rm -f 2>/dev/null || true
    docker network prune -f 2>/dev/null || true
    echo -e "${GREEN}[✓]${NC} Cleanup complete"
}

if [ -n "$CLEAN_FLAG" ]; then
    cleanup_containers
fi

echo -e "\n${GREEN}Starting services...${NC}"

# Start with docker-compose
if [ -n "$BUILD_FLAG" ]; then
    echo -e "${YELLOW}Building images...${NC}"
fi

# Try to start, if it fails due to ContainerConfig error, clean and retry
if ! docker-compose up $DETACH_FLAG $BUILD_FLAG 2>&1; then
    echo -e "${YELLOW}Container issue detected, cleaning up and retrying...${NC}"
    cleanup_containers
    docker-compose up $DETACH_FLAG $BUILD_FLAG
fi

if [ -n "$DETACH_FLAG" ]; then
    # Wait for services to be ready
    sleep 2

    echo -e "\n${GREEN}╔═══════════════════════════════════════════════════════════════╗"
    echo -e "║              Server started successfully!                      ║"
    echo -e "╚═══════════════════════════════════════════════════════════════╝${NC}"
    echo ""
    echo -e "${CYAN}Services:${NC}"
    echo -e "  ${GREEN}●${NC} Frontend UI:     http://localhost:3000"
    echo -e "  ${GREEN}●${NC} Backend API:     http://localhost:8081"
    echo -e "  ${GREEN}●${NC} gRPC (TCP):      localhost:9090"
    echo -e "  ${GREEN}●${NC} QUIC (UDP):      localhost:9443"
    echo -e "  ${GREEN}●${NC} Raw QUIC (UDP):  localhost:9444"
    echo -e "  ${GREEN}●${NC} Neo4j Browser:   http://localhost:7474"
    echo ""
    echo -e "${CYAN}Connect Fleet Agent:${NC}"
    echo -e "  cd ros2_robot_agent && colcon build"
    echo ""
    echo -e "${YELLOW}Commands:${NC}"
    echo -e "  View logs:       docker-compose logs -f [service]"
    echo -e "  Go backend logs: docker-compose logs -f go-backend"
    echo -e "  Stop server:     $SCRIPT_DIR/stop_server.sh"
    echo -e "  Status:          docker-compose ps"
    echo ""

    if [ -n "$LOGS_FLAG" ]; then
        echo -e "${CYAN}Showing logs... (Ctrl+C to exit)${NC}\n"
        docker-compose logs -f
    fi
fi
