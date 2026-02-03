# Robot Agent C++ Dockerfile
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
COPY . ros2_robot_agent/

# Build
WORKDIR /ros2_ws
RUN . /opt/ros/humble/setup.sh && \
    colcon build \
        --packages-select ros2_robot_agent \
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
RUN useradd -m -s /bin/bash robot_agent

# Create directories
RUN mkdir -p /etc/robot_agent/certs \
    /var/lib/robot_agent/graphs \
    /var/log/robot_agent && \
    chown -R robot_agent:robot_agent /etc/robot_agent /var/lib/robot_agent /var/log/robot_agent

# Copy built artifacts
COPY --from=builder /ros2_ws/install /opt/ros2_ws/install

# Copy example config
COPY config/agent.example.yaml /etc/robot_agent/agent.yaml.example

# Setup entrypoint
COPY docker-entrypoint.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# Set environment
ENV FLEET_AGENT_CONFIG=/etc/robot_agent/agent.yaml
ENV ROS_DOMAIN_ID=0
ENV RMW_IMPLEMENTATION=rmw_fastrtps_cpp

USER robot_agent
WORKDIR /home/robot_agent

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["robot_agent_node"]
