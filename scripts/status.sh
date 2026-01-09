#!/bin/bash

# Fleet Management System - Status Script
# 서비스 상태를 확인합니다.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
PID_DIR="$PROJECT_DIR/.pids"
LOG_DIR="$PROJECT_DIR/logs"

# 색상 정의
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  Fleet Management System 상태${NC}"
echo -e "${BLUE}========================================${NC}"

check_service() {
    local name=$1
    local port=$2
    local pid_file="$PID_DIR/${name}.pid"

    echo -e "\n${BLUE}[$name]${NC}"

    # PID 확인
    if [ -f "$pid_file" ]; then
        local pid=$(cat "$pid_file")
        if ps -p "$pid" > /dev/null 2>&1; then
            echo -e "  상태: ${GREEN}● 실행 중${NC}"
            echo -e "  PID: $pid"
        else
            echo -e "  상태: ${RED}● 중지됨 (PID 파일 존재하나 프로세스 없음)${NC}"
        fi
    else
        echo -e "  상태: ${RED}● 중지됨${NC}"
    fi

    # 포트 확인
    if command -v lsof &> /dev/null; then
        local port_check=$(lsof -i :$port -t 2>/dev/null)
        if [ -n "$port_check" ]; then
            echo -e "  포트 $port: ${GREEN}사용 중${NC}"
        else
            echo -e "  포트 $port: ${YELLOW}미사용${NC}"
        fi
    fi

    # 로그 파일
    local log_file="$LOG_DIR/${name}.log"
    if [ -f "$log_file" ]; then
        local log_size=$(du -h "$log_file" | cut -f1)
        echo -e "  로그: $log_file ($log_size)"
        echo -e "  최근 로그:"
        tail -3 "$log_file" 2>/dev/null | sed 's/^/    /'
    fi
}

check_service "backend" 8080
check_service "frontend" 3000

echo -e "\n${BLUE}========================================${NC}"
echo -e "\n${YELLOW}명령어:${NC}"
echo -e "  $SCRIPT_DIR/start.sh  - 서비스 시작"
echo -e "  $SCRIPT_DIR/stop.sh   - 서비스 종료"
echo -e "  $SCRIPT_DIR/status.sh - 상태 확인"
