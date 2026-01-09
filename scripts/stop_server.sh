#!/bin/bash

# Multi-Robot Supervision System - Server Stop Script

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
cd "$PROJECT_DIR"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${YELLOW}Stopping Multi-Robot Supervision System...${NC}"

docker-compose down

echo -e "${GREEN}Server stopped.${NC}"
