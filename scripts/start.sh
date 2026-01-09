#!/bin/bash

# Fleet Management System - Start Script
# 백엔드와 프론트엔드를 백그라운드로 실행합니다.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
LOG_DIR="$PROJECT_DIR/logs"
PID_DIR="$PROJECT_DIR/.pids"

# 색상 정의
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 로그 및 PID 디렉토리 생성
mkdir -p "$LOG_DIR" "$PID_DIR"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  Fleet Management System 시작${NC}"
echo -e "${BLUE}========================================${NC}"

# 이미 실행 중인지 확인
check_running() {
    local name=$1
    local pid_file="$PID_DIR/${name}.pid"

    if [ -f "$pid_file" ]; then
        local pid=$(cat "$pid_file")
        if ps -p "$pid" > /dev/null 2>&1; then
            echo -e "${YELLOW}⚠ $name 이미 실행 중 (PID: $pid)${NC}"
            return 0
        else
            rm -f "$pid_file"
        fi
    fi
    return 1
}

# 백엔드 시작
start_backend() {
    echo -e "\n${GREEN}▶ Backend 시작 중...${NC}"

    if check_running "backend"; then
        return
    fi

    cd "$PROJECT_DIR/central_server/backend"

    USE_SYSTEM_PYTHON=0

    # 가상환경 확인 및 생성
    if [ ! -d "venv" ]; then
        echo -e "${YELLOW}  가상환경 생성 중...${NC}"
        if ! python3 -m venv venv 2>/dev/null; then
            echo -e "${RED}  ✗ 가상환경 생성 실패${NC}"
            echo -e "${YELLOW}  → 시스템 Python을 직접 사용합니다${NC}"
            echo -e "${YELLOW}  (권장: sudo apt install python3.10-venv 설치 후 재시도)${NC}"
            USE_SYSTEM_PYTHON=1
        fi
    fi

    if [ "$USE_SYSTEM_PYTHON" = "0" ] && [ -f "venv/bin/activate" ]; then
        source venv/bin/activate

        # 의존성 설치 확인
        if [ ! -f "venv/.installed" ]; then
            echo -e "${YELLOW}  의존성 설치 중...${NC}"
            pip install -q fastapi uvicorn sqlalchemy aiosqlite python-multipart pydantic pydantic-settings httpx websockets
            touch venv/.installed
        fi

        # 서버 시작
        nohup python -m uvicorn app.main:app --host 0.0.0.0 --port 8080 \
            > "$LOG_DIR/backend.log" 2>&1 &

        echo $! > "$PID_DIR/backend.pid"
        deactivate
    else
        # 시스템 Python 사용
        echo -e "${YELLOW}  의존성 설치 중 (시스템 Python)...${NC}"
        pip3 install --user -q fastapi uvicorn sqlalchemy aiosqlite python-multipart pydantic pydantic-settings httpx websockets 2>/dev/null || true

        # 서버 시작
        nohup python3 -m uvicorn app.main:app --host 0.0.0.0 --port 8080 \
            > "$LOG_DIR/backend.log" 2>&1 &

        echo $! > "$PID_DIR/backend.pid"
    fi

    echo -e "${GREEN}  ✓ Backend 시작됨 (PID: $(cat $PID_DIR/backend.pid))${NC}"
    echo -e "${GREEN}    API: http://localhost:8080${NC}"
    echo -e "${GREEN}    Docs: http://localhost:8080/docs${NC}"
}

# 프론트엔드 시작
start_frontend() {
    echo -e "\n${GREEN}▶ Frontend 시작 중...${NC}"

    if check_running "frontend"; then
        return
    fi

    cd "$PROJECT_DIR/central_server/frontend"

    # node_modules 확인
    if [ ! -d "node_modules" ]; then
        echo -e "${YELLOW}  의존성 설치 중...${NC}"
        npm install --silent
    fi

    # 개발 서버 시작
    nohup npm run dev > "$LOG_DIR/frontend.log" 2>&1 &

    echo $! > "$PID_DIR/frontend.pid"
    echo -e "${GREEN}  ✓ Frontend 시작됨 (PID: $!)${NC}"
    echo -e "${GREEN}    UI: http://localhost:5173${NC}"
}

# 상태 확인
check_status() {
    echo -e "\n${BLUE}서비스 상태:${NC}"

    for service in backend frontend; do
        local pid_file="$PID_DIR/${service}.pid"
        if [ -f "$pid_file" ]; then
            local pid=$(cat "$pid_file")
            if ps -p "$pid" > /dev/null 2>&1; then
                echo -e "  ${GREEN}● $service: 실행 중 (PID: $pid)${NC}"
            else
                echo -e "  ${RED}● $service: 중지됨${NC}"
            fi
        else
            echo -e "  ${RED}● $service: 중지됨${NC}"
        fi
    done
}

# 메인 실행
main() {
    start_backend
    sleep 2  # 백엔드가 먼저 시작되도록 대기
    start_frontend

    echo -e "\n${BLUE}========================================${NC}"
    check_status
    echo -e "${BLUE}========================================${NC}"

    echo -e "\n${YELLOW}로그 확인:${NC}"
    echo -e "  tail -f $LOG_DIR/backend.log"
    echo -e "  tail -f $LOG_DIR/frontend.log"
    echo -e "\n${YELLOW}종료하려면:${NC}"
    echo -e "  $SCRIPT_DIR/stop.sh"
}

main
