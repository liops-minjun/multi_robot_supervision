# Fleet Agent C++ Dockerfile
# Multi-stage build for minimal runtime image
# Includes MsQuic for gRPC over QUIC transport

# ==============================================================================
# Stage 1: Build environment
# ==============================================================================
FROM ros:humble-ros-base AS builder

# Install build dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    cmake \
    git \
    pkg-config \
    wget \
    gnupg2 \
    # gRPC and Protobuf
    libgrpc++-dev \
    libprotobuf-dev \
    protobuf-compiler \
    protobuf-compiler-grpc \
    # Other dependencies
    libtbb-dev \
    libssl-dev \
    libyaml-cpp-dev \
    libspdlog-dev \
    nlohmann-json3-dev \
    libmosquitto-dev \
    # ROS2 packages
    ros-humble-nav2-msgs \
    && rm -rf /var/lib/apt/lists/*

# Install MsQuic from Microsoft packages
RUN wget -q https://packages.microsoft.com/config/ubuntu/22.04/packages-microsoft-prod.deb && \
    dpkg -i packages-microsoft-prod.deb && \
    rm packages-microsoft-prod.deb && \
    apt-get update && \
    apt-get install -y --no-install-recommends libmsquic && \
    rm -rf /var/lib/apt/lists/*

# Create workspace
WORKDIR /ros2_ws/src

# Copy source
COPY . fleet_agent_cpp/

# Build
WORKDIR /ros2_ws
RUN . /opt/ros/humble/setup.sh && \
    colcon build \
        --packages-select fleet_agent_cpp \
        --cmake-args \
            -DCMAKE_BUILD_TYPE=Release \
            -DBUILD_TESTING=OFF

# ==============================================================================
# Stage 2: Runtime environment
# ==============================================================================
FROM ros:humble-ros-base AS runtime

# Install runtime dependencies including MsQuic
RUN apt-get update && apt-get install -y --no-install-recommends \
    wget \
    gnupg2 \
    libgrpc++1 \
    libprotobuf23 \
    libtbb12 \
    libssl3 \
    libyaml-cpp0.7 \
    libspdlog1 \
    libmosquitto1 \
    ros-humble-nav2-msgs \
    && rm -rf /var/lib/apt/lists/*

# Install MsQuic runtime
RUN wget -q https://packages.microsoft.com/config/ubuntu/22.04/packages-microsoft-prod.deb && \
    dpkg -i packages-microsoft-prod.deb && \
    rm packages-microsoft-prod.deb && \
    apt-get update && \
    apt-get install -y --no-install-recommends libmsquic && \
    apt-get remove -y wget gnupg2 && \
    apt-get autoremove -y && \
    rm -rf /var/lib/apt/lists/*

# Create user
RUN useradd -m -s /bin/bash fleet_agent

# Create directories
RUN mkdir -p /etc/fleet_agent/certs \
    /var/lib/fleet_agent/graphs \
    /var/log/fleet_agent && \
    chown -R fleet_agent:fleet_agent /etc/fleet_agent /var/lib/fleet_agent /var/log/fleet_agent

# Copy built artifacts
COPY --from=builder /ros2_ws/install /opt/ros2_ws/install

# Copy example config
COPY config/agent.example.yaml /etc/fleet_agent/agent.yaml.example

# Setup entrypoint
COPY docker-entrypoint.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# Set environment
ENV FLEET_AGENT_CONFIG=/etc/fleet_agent/agent.yaml
ENV ROS_DOMAIN_ID=0
ENV RMW_IMPLEMENTATION=rmw_fastrtps_cpp

USER fleet_agent
WORKDIR /home/fleet_agent

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["fleet_agent_node"]
