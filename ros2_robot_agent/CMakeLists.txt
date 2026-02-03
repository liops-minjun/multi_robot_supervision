cmake_minimum_required(VERSION 3.14)
project(ros2_robot_agent VERSION 1.0.0 LANGUAGES CXX)

# C++ Standard
set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

# Build type
if(NOT CMAKE_BUILD_TYPE)
  set(CMAKE_BUILD_TYPE Release)
endif()

# Compile options
add_compile_options(-Wall -Wextra -Wpedantic)
if(CMAKE_BUILD_TYPE STREQUAL "Release")
  add_compile_options(-O3)
endif()

# Find ROS2 packages
find_package(ament_cmake REQUIRED)
find_package(rclcpp REQUIRED)
find_package(rclcpp_action REQUIRED)
find_package(std_msgs REQUIRED)
find_package(geometry_msgs REQUIRED)
find_package(nav_msgs REQUIRED)
find_package(sensor_msgs REQUIRED)
find_package(nav2_msgs REQUIRED)
find_package(rcl_interfaces REQUIRED)
find_package(rosidl_runtime_cpp REQUIRED)
find_package(rosidl_typesupport_cpp REQUIRED)
find_package(rosidl_typesupport_introspection_cpp REQUIRED)
find_package(action_msgs REQUIRED)
find_package(tf2_msgs REQUIRED)
find_package(lifecycle_msgs REQUIRED)

# Find external dependencies
find_package(Protobuf REQUIRED)
find_package(PkgConfig REQUIRED)
pkg_check_modules(GRPC REQUIRED grpc++ grpc)
find_package(TBB REQUIRED)
find_package(OpenSSL REQUIRED)
find_package(yaml-cpp REQUIRED)
find_package(spdlog REQUIRED)
find_package(nlohmann_json REQUIRED)

# gRPC plugin for protoc
find_program(GRPC_CPP_PLUGIN grpc_cpp_plugin REQUIRED)

# Find MsQuic for QUIC transport (REQUIRED)
find_library(MSQUIC_LIBRARY NAMES msquic PATHS /usr/lib /usr/local/lib REQUIRED)
find_path(MSQUIC_INCLUDE_DIR NAMES msquic.h PATHS /usr/include /usr/local/include REQUIRED)
message(STATUS "Found MsQuic: ${MSQUIC_LIBRARY}")
message(STATUS "MsQuic include: ${MSQUIC_INCLUDE_DIR}")

# ==============================================================================
# Proto Generation
# ==============================================================================

set(PROTO_DIR ${CMAKE_CURRENT_SOURCE_DIR}/proto)
set(PROTO_GEN_DIR ${CMAKE_CURRENT_BINARY_DIR}/generated)

file(MAKE_DIRECTORY ${PROTO_GEN_DIR})

# Proto files
set(PROTO_FILES
  ${PROTO_DIR}/fleet/v1/common.proto
  ${PROTO_DIR}/fleet/v1/commands.proto
  ${PROTO_DIR}/fleet/v1/graphs.proto
  ${PROTO_DIR}/fleet/v1/service.proto
)

# Get protobuf plugin path (grpc plugin already found above)
find_program(PROTOC_PATH protoc REQUIRED)
set(GRPC_CPP_PLUGIN_PATH ${GRPC_CPP_PLUGIN})

# Generate proto sources
set(PROTO_SRCS)
set(PROTO_HDRS)
set(GRPC_SRCS)
set(GRPC_HDRS)

foreach(PROTO_FILE ${PROTO_FILES})
  get_filename_component(PROTO_NAME ${PROTO_FILE} NAME_WE)
  get_filename_component(PROTO_PATH ${PROTO_FILE} DIRECTORY)
  file(RELATIVE_PATH PROTO_REL_PATH ${PROTO_DIR} ${PROTO_PATH})

  file(MAKE_DIRECTORY ${PROTO_GEN_DIR}/${PROTO_REL_PATH})

  set(PROTO_SRC ${PROTO_GEN_DIR}/${PROTO_REL_PATH}/${PROTO_NAME}.pb.cc)
  set(PROTO_HDR ${PROTO_GEN_DIR}/${PROTO_REL_PATH}/${PROTO_NAME}.pb.h)
  set(GRPC_SRC ${PROTO_GEN_DIR}/${PROTO_REL_PATH}/${PROTO_NAME}.grpc.pb.cc)
  set(GRPC_HDR ${PROTO_GEN_DIR}/${PROTO_REL_PATH}/${PROTO_NAME}.grpc.pb.h)

  add_custom_command(
    OUTPUT ${PROTO_SRC} ${PROTO_HDR} ${GRPC_SRC} ${GRPC_HDR}
    COMMAND ${PROTOC_PATH}
      --experimental_allow_proto3_optional
      --proto_path=${PROTO_DIR}
      --cpp_out=${PROTO_GEN_DIR}
      --grpc_out=${PROTO_GEN_DIR}
      --plugin=protoc-gen-grpc=${GRPC_CPP_PLUGIN_PATH}
      ${PROTO_FILE}
    DEPENDS ${PROTO_FILE}
    COMMENT "Generating protobuf and gRPC code for ${PROTO_NAME}"
  )

  list(APPEND PROTO_SRCS ${PROTO_SRC})
  list(APPEND PROTO_HDRS ${PROTO_HDR})
  list(APPEND GRPC_SRCS ${GRPC_SRC})
  list(APPEND GRPC_HDRS ${GRPC_HDR})
endforeach()

# Proto library
add_library(fleet_proto STATIC
  ${PROTO_SRCS}
  ${GRPC_SRCS}
)

target_include_directories(fleet_proto PUBLIC
  ${PROTO_GEN_DIR}
  ${Protobuf_INCLUDE_DIRS}
  ${GRPC_INCLUDE_DIRS}
)

target_link_libraries(fleet_proto PUBLIC
  ${Protobuf_LIBRARIES}
  ${GRPC_LIBRARIES}
)

# ==============================================================================
# Robot Agent Library
# ==============================================================================

set(ROBOT_AGENT_SOURCES
  # Core
  src/core/config_loader.cpp
  src/core/shutdown.cpp
  src/core/logger.cpp
  src/core/agent_id_storage.cpp

  # Capability
  src/capability/type_loader.cpp
  src/capability/schema_extractor.cpp
  src/capability/scanner.cpp
  src/capability/capability_scanner_adapter.cpp

  # Executor
  src/executor/precondition.cpp
  src/executor/action_executor.cpp
  src/executor/dynamic_action_client.cpp
  src/executor/command_processor.cpp
  src/executor/task_log_sender.cpp
  src/executor/task_executor.cpp
  src/executor/ros2_action_executor.cpp

  # Graph
  src/graph/storage.cpp
  src/graph/executor.cpp

  # State Management
  src/state/state_definition.cpp
  src/state/state_tracker.cpp
  src/state/graph_state.cpp

  # Protocol
  src/protocol/message_handler.cpp

  # Transport
  src/transport/tls_credentials.cpp
  src/transport/quic_transport.cpp
  src/transport/quic_transport_adapter.cpp
  src/transport/quic_outbound_sender.cpp
  src/transport/persistent_queue.cpp

  # Telemetry
  src/telemetry/collector.cpp
  src/telemetry/aggregator.cpp

  # Factory
  src/factory/agent_factory.cpp

  # Agent
  src/agent.cpp
)

add_library(robot_agent_lib STATIC ${ROBOT_AGENT_SOURCES})

target_include_directories(robot_agent_lib PUBLIC
  $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
  $<INSTALL_INTERFACE:include>
  ${PROTO_GEN_DIR}
  ${MSQUIC_INCLUDE_DIR}
)

target_link_libraries(robot_agent_lib PUBLIC
  fleet_proto
  TBB::tbb
  OpenSSL::SSL
  OpenSSL::Crypto
  yaml-cpp
  spdlog::spdlog
  nlohmann_json::nlohmann_json
  ${MSQUIC_LIBRARY}
  dl  # For dlopen/dlsym
)

ament_target_dependencies(robot_agent_lib PUBLIC
  rclcpp
  rclcpp_action
  std_msgs
  geometry_msgs
  nav_msgs
  sensor_msgs
  nav2_msgs
  rcl_interfaces
  rosidl_runtime_cpp
  rosidl_typesupport_cpp
  rosidl_typesupport_introspection_cpp
  action_msgs
  tf2_msgs
  lifecycle_msgs
)

# ==============================================================================
# Robot Agent Executable
# ==============================================================================

add_executable(robot_agent_node src/main.cpp)

target_link_libraries(robot_agent_node PRIVATE
  robot_agent_lib
)

# ==============================================================================
# Installation
# ==============================================================================

install(TARGETS robot_agent_node
  DESTINATION lib/${PROJECT_NAME}
)

# ==============================================================================
# Test Dynamic Action Client
# ==============================================================================

add_executable(test_dynamic_action src/test_dynamic_action.cpp)

target_link_libraries(test_dynamic_action PRIVATE
  robot_agent_lib
)

install(TARGETS test_dynamic_action
  DESTINATION lib/${PROJECT_NAME}
)

install(TARGETS robot_agent_lib fleet_proto
  ARCHIVE DESTINATION lib
  LIBRARY DESTINATION lib
  RUNTIME DESTINATION bin
)

install(DIRECTORY include/
  DESTINATION include
)

install(DIRECTORY ${PROTO_GEN_DIR}/fleet
  DESTINATION include
)

install(DIRECTORY config/
  DESTINATION share/${PROJECT_NAME}/config
)

install(DIRECTORY certs/
  DESTINATION share/${PROJECT_NAME}/certs
  OPTIONAL
)

install(DIRECTORY launch/
  DESTINATION share/${PROJECT_NAME}/launch
  OPTIONAL
)

# ==============================================================================
# Testing
# ==============================================================================

if(BUILD_TESTING)
  find_package(ament_lint_auto REQUIRED)
  find_package(ament_cmake_gtest REQUIRED)

  ament_lint_auto_find_test_dependencies()

  # Add tests here
  # ament_add_gtest(test_config test/test_config.cpp)
  # target_link_libraries(test_config robot_agent_lib)
endif()

# ==============================================================================
# Ament package
# ==============================================================================

ament_export_include_directories(include)
ament_export_libraries(robot_agent_lib fleet_proto)
ament_export_dependencies(
  rclcpp
  rclcpp_action
  std_msgs
  geometry_msgs
  nav_msgs
  sensor_msgs
  nav2_msgs
)

ament_package()
