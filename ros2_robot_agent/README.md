# Robot Agent C++ (gRPC over QUIC)

C++ 기반 Robot Agent로, ROS2 로봇들을 관리하고 Central Server와 **gRPC over QUIC** 프로토콜로 통신합니다.

## 주요 특징

- **gRPC over QUIC**: 0-RTT 재연결, 연결 마이그레이션, 멀티플렉싱
- **MsQuic**: Microsoft의 고성능 QUIC 라이브러리 사용
- **Zero-Config**: ROS2 Action Server 자동 탐지
- **Hybrid Control**: 로컬/서버 조건 평가 지원
- **mTLS**: 상호 TLS 인증

## 시스템 요구사항

- Ubuntu 22.04 (Jammy)
- ROS2 Humble
- CMake 3.14+
- C++17 컴파일러
- **MsQuic 2.x** (필수 - QUIC 통신에 필요)

> ⚠️ **Prerequisites**: MsQuic 라이브러리는 **필수**입니다. 설치하지 않으면 빌드가 실패합니다.
> 아래 "의존성 설치 > 3. MsQuic 설치" 섹션을 반드시 먼저 완료하세요.

## 의존성 설치

### 1. ROS2 Humble 설치

```bash
# ROS2 저장소 추가
sudo apt update && sudo apt install -y software-properties-common
sudo add-apt-repository universe
sudo apt update && sudo apt install curl -y
sudo curl -sSL https://raw.githubusercontent.com/ros/rosdistro/master/ros.key -o /usr/share/keyrings/ros-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/ros-archive-keyring.gpg] http://packages.ros.org/ros2/ubuntu $(. /etc/os-release && echo $UBUNTU_CODENAME) main" | sudo tee /etc/apt/sources.list.d/ros2.list > /dev/null

# ROS2 설치
sudo apt update
sudo apt install -y ros-humble-ros-base ros-humble-nav2-msgs
```

### 2. 기본 빌드 의존성 설치

```bash
sudo apt update
sudo apt install -y \
    build-essential \
    cmake \
    git \
    pkg-config \
    libgrpc++-dev \
    libprotobuf-dev \
    protobuf-compiler \
    protobuf-compiler-grpc \
    libtbb-dev \
    libssl-dev \
    libyaml-cpp-dev \
    libspdlog-dev \
    nlohmann-json3-dev
```

### 3. MsQuic 설치 ⭐

MsQuic는 Microsoft에서 개발한 크로스 플랫폼 QUIC 프로토콜 구현입니다.

#### 방법 1: Microsoft 패키지 저장소 (권장)

```bash
# Microsoft 패키지 저장소 추가
wget -q https://packages.microsoft.com/config/ubuntu/22.04/packages-microsoft-prod.deb
sudo dpkg -i packages-microsoft-prod.deb
rm packages-microsoft-prod.deb

# MsQuic 설치
sudo apt update
sudo apt install -y libmsquic

# 설치 확인
dpkg -l | grep msquic
# 출력: ii  libmsquic  2.x.x  amd64  Microsoft implementation of the QUIC protocol
```

#### 방법 2: 소스에서 빌드

```bash
# 의존성 설치
sudo apt install -y cmake ninja-build

# 소스 클론
git clone --recursive https://github.com/microsoft/msquic.git
cd msquic

# 빌드
mkdir build && cd build
cmake -G Ninja \
    -DCMAKE_BUILD_TYPE=Release \
    -DQUIC_BUILD_SHARED=ON \
    -DQUIC_TLS=openssl \
    ..
ninja

# 설치
sudo ninja install
sudo ldconfig
```

#### 방법 3: GitHub Release에서 다운로드

```bash
# 최신 릴리즈 다운로드 (예: v2.3.0)
MSQUIC_VERSION="2.3.0"
wget https://github.com/microsoft/msquic/releases/download/v${MSQUIC_VERSION}/libmsquic_${MSQUIC_VERSION}_amd64.deb

# 설치
sudo dpkg -i libmsquic_${MSQUIC_VERSION}_amd64.deb
sudo apt install -f  # 의존성 해결
rm libmsquic_${MSQUIC_VERSION}_amd64.deb
```

#### MsQuic 설치 확인

```bash
# 라이브러리 확인
ls -la /usr/lib/libmsquic.so*

# 헤더 확인
ls -la /usr/include/msquic.h

# 버전 확인 (빌드 후)
./robot_agent_node --version
```

### 4. 선택적: 개발 헤더 설치

MsQuic 개발 헤더가 별도로 필요한 경우:

```bash
# 헤더 파일만 복사
sudo wget -O /usr/include/msquic.h \
    https://raw.githubusercontent.com/microsoft/msquic/main/src/inc/msquic.h
sudo wget -O /usr/include/msquic_posix.h \
    https://raw.githubusercontent.com/microsoft/msquic/main/src/inc/msquic_posix.h
```

## 빌드

### ROS2 워크스페이스에서 빌드

```bash
# 워크스페이스 생성
mkdir -p ~/ros2_ws/src
cd ~/ros2_ws/src

# 소스 복사 또는 심볼릭 링크
ln -s /path/to/multi-robot-supervision/ros2_robot_agent .

# 빌드
cd ~/ros2_ws
source /opt/ros/humble/setup.bash
colcon build --packages-select ros2_robot_agent

# 환경 설정
source install/setup.bash
```

### 빌드 옵션

```bash
# Release 빌드 (최적화)
colcon build --packages-select ros2_robot_agent \
    --cmake-args -DCMAKE_BUILD_TYPE=Release

# Debug 빌드
colcon build --packages-select ros2_robot_agent \
    --cmake-args -DCMAKE_BUILD_TYPE=Debug
```

> **Note**: MsQuic는 필수 의존성입니다. 설치되어 있지 않으면 CMake 구성 단계에서 빌드가 실패합니다.

## 설정

### 설정 파일 생성

```bash
sudo mkdir -p /etc/robot_agent
sudo cp config/agent.example.yaml /etc/robot_agent/agent.yaml
sudo chown $USER:$USER /etc/robot_agent/agent.yaml
```

### 설정 파일 편집

```yaml
# /etc/robot_agent/agent.yaml

agent:
  id: "agent_01"
  name: "Factory Floor Agent"

robots:
  - id: "robot_001"
    name: "AMR Robot 1"
    namespace: "/robot_001"

server:
  # QUIC 설정 (필수)
  quic:
    server_address: "192.168.0.100"  # Central Server IP
    server_port: 9444                # Raw QUIC 포트 (UDP)
    ca_cert: "/etc/robot_agent/certs/ca.crt"
    client_cert: "/etc/robot_agent/certs/client.crt"
    client_key: "/etc/robot_agent/certs/client.key"
    # 성능 설정
    idle_timeout_ms: 30000
    keepalive_interval_ms: 10000
    enable_0rtt: true                # 빠른 재연결
    enable_datagrams: true           # 저지연 텔레메트리
```

### TLS 인증서 설정

```bash
# 인증서 디렉토리 생성
sudo mkdir -p /etc/robot_agent/certs

# 인증서 복사 (CA, 클라이언트 인증서, 키)
sudo cp ca.crt /etc/robot_agent/certs/
sudo cp client.crt /etc/robot_agent/certs/
sudo cp client.key /etc/robot_agent/certs/

# 권한 설정
sudo chmod 600 /etc/robot_agent/certs/client.key
```

## 실행

### 직접 실행

```bash
source /opt/ros/humble/setup.bash
source ~/ros2_ws/install/setup.bash

# 기본 설정으로 실행
ros2 run ros2_robot_agent robot_agent_node

# 설정 파일 지정
ros2 run ros2_robot_agent robot_agent_node -c /etc/robot_agent/agent.yaml

# 환경 변수로 설정
FLEET_AGENT_CONFIG=/etc/robot_agent/agent.yaml ros2 run ros2_robot_agent robot_agent_node
```

### Launch 파일 사용

```bash
ros2 launch ros2_robot_agent robot_agent.launch.py

# 설정 파일 지정
ros2 launch ros2_robot_agent robot_agent.launch.py config:=/etc/robot_agent/agent.yaml
```

### Docker 실행

```bash
# 이미지 빌드
docker build -t ros2_robot_agent:latest .

# 실행 (host 네트워크 필요 - ROS2 DDS)
docker run -d \
    --name robot_agent \
    --network host \
    -v /etc/robot_agent:/etc/robot_agent:ro \
    -e FLEET_QUIC_SERVER=192.168.0.100 \
    -e FLEET_QUIC_PORT=9443 \
    ros2_robot_agent:latest
```

### Docker Compose 실행

```bash
# 설정 파일 준비
cp config/agent.example.yaml config/agent.yaml
# agent.yaml 편집...

# 실행
docker-compose up -d

# 로그 확인
docker-compose logs -f robot_agent
```

## QUIC 장점

| 특성 | QUIC (UDP) | 기존 TCP |
|------|------------|----------|
| 연결 설정 | 0-RTT (재연결 시) | 3-way handshake |
| Head-of-line blocking | 없음 | 있음 |
| 연결 마이그레이션 | 지원 (IP 변경 가능) | 미지원 |
| TLS | 내장 (TLS 1.3) | 별도 설정 |
| 멀티플렉싱 | 스트림 레벨 | 연결 레벨 |
| 무선 환경 | 우수 | 보통 |

### QUIC가 적합한 환경

- 무선 네트워크 환경 (WiFi, 5G)
- 이동 로봇 (IP 변경 가능성)
- 불안정한 네트워크
- 빠른 재연결이 중요한 경우

> **Note**: 이 Robot Agent는 QUIC만 지원합니다. 방화벽이 UDP:9443을 허용하는지 확인하세요.

## 문제 해결

### MsQuic 관련

```bash
# 라이브러리를 찾을 수 없는 경우
sudo ldconfig
export LD_LIBRARY_PATH=/usr/lib:$LD_LIBRARY_PATH

# 빌드 시 MsQuic를 찾지 못하는 경우
colcon build --cmake-args \
    -DMSQUIC_LIBRARY=/usr/lib/libmsquic.so \
    -DMSQUIC_INCLUDE_DIR=/usr/include
```

### 연결 문제

```bash
# UDP 포트 확인
sudo netstat -ulnp | grep 9443

# 방화벽 설정 (UFW)
sudo ufw allow 9443/udp

# QUIC 연결 테스트
# Central Server 로그에서 QUIC 연결 확인
```

### 인증서 문제

```bash
# 인증서 검증
openssl verify -CAfile ca.crt client.crt

# 인증서 정보 확인
openssl x509 -in client.crt -text -noout

# 키 매칭 확인
openssl x509 -noout -modulus -in client.crt | openssl md5
openssl rsa -noout -modulus -in client.key | openssl md5
# 두 해시가 일치해야 함
```

## 아키텍처

```
┌─────────────────────────────────────────────────────────────┐
│                     Robot Agent C++                         │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │   ROS2      │  │  Capability │  │    Heartbeat        │  │
│  │  Executor   │  │   Scanner   │  │   Collector         │  │
│  └──────┬──────┘  └──────┬──────┘  └──────────┬──────────┘  │
│         │                │                     │            │
│  ┌──────┴────────────────┴─────────────────────┴──────────┐ │
│  │                    Command Processor                    │ │
│  │              (Graph Executor, Preconditions)            │ │
│  └──────────────────────────┬──────────────────────────────┘ │
│                             │                                │
│  ┌──────────────────────────┴──────────────────────────────┐ │
│  │                    Transport Layer                       │ │
│  │                ┌─────────────────┐                       │ │
│  │                │  QUIC Client    │                       │ │
│  │                │  (MsQuic)       │                       │ │
│  │                └────────┬────────┘                       │ │
│  └─────────────────────────┼────────────────────────────────┘ │
└────────────────────────────┼──────────────────────────────────┘
                             │
                             ▼
             ┌─────────────────────────────────────┐
             │        Central Server (Go)          │
             │           QUIC:9443                 │
             └─────────────────────────────────────┘
```

---

## Development Principles (개발 원칙)

### 1. Interface-First Design (인터페이스 우선 설계)

모든 주요 컴포넌트는 인터페이스(IXxx)를 먼저 정의하고 구현합니다.

```
┌─────────────────────────────────────────────────────────────────┐
│                         Agent (Orchestrator)                     │
├─────────────────────────────────────────────────────────────────┤
│  ┌──────────────────┐  ┌──────────────────┐  ┌───────────────┐  │
│  │   ITransport*    │  │ICapabilityScanner*│  │IActionExecutor*│ │
│  └────────┬─────────┘  └────────┬─────────┘  └───────┬───────┘  │
│  ┌────────▼─────────┐  ┌───────▼──────────┐  ┌──────▼────────┐  │
│  │QUICTransport     │  │CapabilityScanner │  │ROS2Action     │  │
│  │Adapter           │  │Adapter           │  │Executor       │  │
│  └──────────────────┘  └──────────────────┘  └───────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

**핵심 인터페이스:**
- `interfaces/transport.hpp` - ITransport
- `interfaces/capability_scanner.hpp` - ICapabilityScanner
- `interfaces/action_executor.hpp` - IActionExecutor

### 2. Dependency Injection (의존성 주입)

```cpp
// Production
auto agent = AgentFactory::create_agent("config.yaml");

// Testing (mock 주입)
AgentComponents components;
components.transport = std::make_unique<MockTransport>();
auto agent = AgentFactory::create_agent_with_components(config, std::move(components));
```

### 3. Single Responsibility (단일 책임)

| 클래스 | 책임 | 목표 라인 수 |
|--------|------|-------------|
| Agent | 오케스트레이션 | ~500 lines |
| GraphExecutor | 그래프 탐색 | ~300 lines |
| IActionExecutor | 액션 실행 | ~200 lines |

### 4. Adapter Pattern (어댑터 패턴)

기존 구현체 수정 없이 인터페이스 적용:

```cpp
class QUICTransportAdapter : public ITransport {
    std::shared_ptr<QUICClient> client_;
public:
    bool connect(const std::string& addr, uint16_t port) override {
        return client_->connect(addr, port);
    }
};
```

### 5. File Organization (파일 구성)

```
include/robot_agent/
├── interfaces/              # 인터페이스 (순수 가상 클래스)
│   ├── transport.hpp
│   ├── capability_scanner.hpp
│   └── action_executor.hpp
├── factory/                 # 팩토리
│   └── agent_factory.hpp
├── transport/               # Transport 구현
│   ├── quic_transport.hpp   # 구체 클래스
│   └── quic_transport_adapter.hpp # 어댑터
├── capability/              # Capability 구현
└── executor/                # Executor 구현
```

### 6. When Adding New Components (새 컴포넌트 추가)

1. 인터페이스 정의 (`interfaces/i_xxx.hpp`)
2. 구현체/어댑터 생성
3. 팩토리 메서드 추가 (`AgentFactory::create_default_xxx()`)
4. CMakeLists.txt 업데이트
5. 테스트 작성 (Mock 사용)

### 7. Naming Conventions (네이밍 규칙)

| 타입 | 패턴 | 예시 |
|------|------|------|
| 인터페이스 | `I` + PascalCase | `ITransport` |
| 어댑터 | Concrete + `Adapter` | `QUICTransportAdapter` |
| 팩토리 메서드 | `create_` + snake_case | `create_default_transport()` |
| 콜백 타입 | PascalCase + `Callback` | `ResultCallback` |

---

## 라이선스

Apache-2.0

## 참고 자료

- [MsQuic GitHub](https://github.com/microsoft/msquic)
- [MsQuic Documentation](https://github.com/microsoft/msquic/tree/main/docs)
- [QUIC RFC 9000](https://datatracker.ietf.org/doc/html/rfc9000)
- [gRPC over QUIC](https://grpc.io/blog/grpc-on-http2/)
