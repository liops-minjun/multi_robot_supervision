#!/bin/bash

# Fleet Management System - Stop Script
# 백엔드와 프론트엔드를 종료합니다.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
PID_DIR="$PROJECT_DIR/.pids"

# 색상 정의
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  Fleet Management System 종료${NC}"
echo -e "${BLUE}========================================${NC}"

stop_service() {
    local name=$1
    local pid_file="$PID_DIR/${name}.pid"

    if [ -f "$pid_file" ]; then
        local pid=$(cat "$pid_file")
        if ps -p "$pid" > /dev/null 2>&1; then
            echo -e "${YELLOW}▶ $name 종료 중 (PID: $pid)...${NC}"
            kill "$pid" 2>/dev/null

            # 종료 대기
            local count=0
            while ps -p "$pid" > /dev/null 2>&1 && [ $count -lt 10 ]; do
                sleep 0.5
                count=$((count + 1))
            done

            # 강제 종료
            if ps -p "$pid" > /dev/null 2>&1; then
                kill -9 "$pid" 2>/dev/null
            fi

            echo -e "${GREEN}  ✓ $name 종료됨${NC}"
        else
            echo -e "${YELLOW}  $name 이미 종료됨${NC}"
        fi
        rm -f "$pid_file"
    else
        echo -e "${YELLOW}  $name PID 파일 없음${NC}"
    fi
}

# 자식 프로세스도 종료 (npm, uvicorn 등)
cleanup_processes() {
    # uvicorn 프로세스 정리
    pkill -f "uvicorn app.main:app" 2>/dev/null || true

    # vite 개발 서버 정리
    pkill -f "vite" 2>/dev/null || true
}

echo ""
stop_service "frontend"
stop_service "backend"

echo -e "\n${YELLOW}▶ 잔여 프로세스 정리 중...${NC}"
cleanup_processes

echo -e "\n${GREEN}========================================${NC}"
echo -e "${GREEN}  모든 서비스가 종료되었습니다${NC}"
echo -e "${GREEN}========================================${NC}"
