# basicstation-analytics: bridge from basicstation UNIX DGRAM socket to analytics gRPC
# SPDX-License-Identifier: MIT
# Copyright 2026 Kudzu Technologies
#
# Build: cmake -B build -DCMAKE_BUILD_TYPE=Release && cmake --build build
# Cross-compile: cmake -B build-<platform> -DCMAKE_TOOLCHAIN_FILE=cmake/toolchain-<platform>.cmake
#
# Expects client-cc at ../client-cc.

cmake_minimum_required(VERSION 3.24)
project(basicstation_analytics LANGUAGES CXX C)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_POSITION_INDEPENDENT_CODE ON)

option(BS_ANALYTICS_BUILD_TESTS "Build unit tests" OFF)

if(NOT CMAKE_BUILD_TYPE AND NOT CMAKE_CONFIGURATION_TYPES)
  set(CMAKE_BUILD_TYPE Release CACHE STRING "Build type" FORCE)
endif()
if(CMAKE_BUILD_TYPE STREQUAL "Release")
  set(CMAKE_CXX_FLAGS_RELEASE "${CMAKE_CXX_FLAGS_RELEASE} -O2" CACHE STRING "" FORCE)
endif()

execute_process(
  COMMAND git rev-parse --short HEAD
  WORKING_DIRECTORY ${CMAKE_CURRENT_SOURCE_DIR}/..
  OUTPUT_VARIABLE BS_ANALYTICS_GIT_HASH
  OUTPUT_STRIP_TRAILING_WHITESPACE
  ERROR_QUIET
)
if(NOT BS_ANALYTICS_GIT_HASH)
  set(BS_ANALYTICS_GIT_HASH "unknown")
endif()
set(BS_ANALYTICS_VERSION "dev" CACHE STRING "Release version")

# -----------------------------------------------------------------------------
# client-cc
# -----------------------------------------------------------------------------
set(CLIENT_CC_DIR "${CMAKE_CURRENT_SOURCE_DIR}/../client-cc")
if(NOT EXISTS "${CLIENT_CC_DIR}/CMakeLists.txt")
  message(FATAL_ERROR "client-cc not found at ${CLIENT_CC_DIR}")
endif()
add_subdirectory(${CLIENT_CC_DIR} ${CMAKE_BINARY_DIR}/client-cc)

# -----------------------------------------------------------------------------
# Core library (testable without main)
# -----------------------------------------------------------------------------
add_library(bs-analytics-core STATIC
  receiver.cc
  dedupe.cc
  config.cc
  gateway_sync.cc
  pairing.cc
  logging.cc
  sender.cc
)
target_include_directories(bs-analytics-core PUBLIC
  ${CMAKE_CURRENT_SOURCE_DIR}
)
target_compile_definitions(bs-analytics-core PUBLIC
  BS_ANALYTICS_VERSION="${BS_ANALYTICS_VERSION}"
  BS_ANALYTICS_GIT_HASH="${BS_ANALYTICS_GIT_HASH}"
)
target_link_libraries(bs-analytics-core PUBLIC client-cc)
find_package(OpenSSL REQUIRED)
target_link_libraries(bs-analytics-core PUBLIC OpenSSL::Crypto OpenSSL::SSL)
find_package(Threads QUIET)
if(Threads_FOUND)
  target_link_libraries(bs-analytics-core PUBLIC Threads::Threads)
endif()
if(CMAKE_CXX_COMPILER_ID MATCHES "GNU|Clang")
  target_compile_options(bs-analytics-core PRIVATE -Wall -Wextra -Wno-unused-parameter)
endif()

# -----------------------------------------------------------------------------
# Executable
# -----------------------------------------------------------------------------
add_executable(basicstation-analytics main.cc)
target_link_libraries(basicstation-analytics PRIVATE bs-analytics-core)
if(CMAKE_CXX_COMPILER_ID MATCHES "GNU|Clang")
  target_compile_options(bs-analytics-core PRIVATE -ffunction-sections -fdata-sections)
  if(APPLE)
    target_link_options(basicstation-analytics PRIVATE -Wl,-dead_strip)
  else()
    target_link_options(basicstation-analytics PRIVATE -Wl,--gc-sections)
  endif()
endif()
install(TARGETS basicstation-analytics RUNTIME DESTINATION bin)

# -----------------------------------------------------------------------------
# Tests
# -----------------------------------------------------------------------------
if(BS_ANALYTICS_BUILD_TESTS)
  enable_testing()
  function(bs_add_test name src)
    add_executable(${name} ${src})
    target_link_libraries(${name} PRIVATE bs-analytics-core)
    add_test(NAME ${name} COMMAND ${name})
  endfunction()
  bs_add_test(protocol_test tests/protocol_test.cc)
  bs_add_test(receiver_test tests/receiver_test.cc)
  bs_add_test(config_test tests/config_test.cc)
  bs_add_test(pairing_test tests/pairing_test.cc)
  bs_add_test(gateway_sync_test tests/gateway_sync_test.cc)
  bs_add_test(batch_queue_test tests/batch_queue_test.cc)
  bs_add_test(dedupe_test tests/dedupe_test.cc)
  bs_add_test(sender_test tests/sender_test.cc)
  bs_add_test(socket_test tests/socket_test.cc)
  bs_add_test(radio_golden_test tests/radio_golden_test.cc)
  target_link_libraries(radio_golden_test PRIVATE nlohmann_json::nlohmann_json)
  target_compile_definitions(radio_golden_test PRIVATE BS_RADIO_FIXTURE="${CMAKE_CURRENT_SOURCE_DIR}/../testdata/basicstation-radio.json")
endif()
