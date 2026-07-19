# forwarder-cc: C++ Semtech UDP proxy and analytics forwarder
#
# Build: cmake -S . -B build && cmake --build build
# Tests:  cmake -S . -B build -DFORWARDER_CC_BUILD_TESTS=ON && cmake --build build && ctest --test-dir build --output-on-failure
# Size:   cmake -S . -B build-size -DCMAKE_BUILD_TYPE=MinSizeRel && cmake --build build-size
#
# Uses client-cc as a sub-project (same repo). No -march=native for portability.

cmake_minimum_required(VERSION 3.14)
project(forwarder_cc LANGUAGES CXX C)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_POSITION_INDEPENDENT_CODE ON)

option(FORWARDER_CC_BUILD_TESTS "Build forwarder-cc unit tests" OFF)

if(NOT CMAKE_BUILD_TYPE AND NOT CMAKE_CONFIGURATION_TYPES)
  set(CMAKE_BUILD_TYPE Release CACHE STRING "Build type" FORCE)
endif()

# Optional git hash for --version
execute_process(
  COMMAND git rev-parse --short HEAD
  WORKING_DIRECTORY ${CMAKE_CURRENT_SOURCE_DIR}
  OUTPUT_VARIABLE FORWARDER_CC_GIT_HASH
  OUTPUT_STRIP_TRAILING_WHITESPACE
  ERROR_QUIET
)
if(NOT FORWARDER_CC_GIT_HASH)
  set(FORWARDER_CC_GIT_HASH "unknown")
endif()

# -----------------------------------------------------------------------------
# nlohmann_json (used by Semtech UDP JSON parsing)
# -----------------------------------------------------------------------------
include(FetchContent)
FetchContent_Declare(
  json
  URL https://github.com/nlohmann/json/releases/download/v3.11.3/json.tar.xz
  DOWNLOAD_EXTRACT_TIMESTAMP TRUE
)
set(JSON_BuildTests OFF CACHE BOOL "" FORCE)
FetchContent_MakeAvailable(json)

# -----------------------------------------------------------------------------
# client-cc (analytics gRPC client + pairing) as sub-project
# -----------------------------------------------------------------------------
set(CLIENT_CC_DIR "${CMAKE_CURRENT_SOURCE_DIR}/../client-cc")
if(NOT EXISTS "${CLIENT_CC_DIR}/CMakeLists.txt")
  message(FATAL_ERROR "client-cc not found at ${CLIENT_CC_DIR}. Expect repo layout: analytics/forwarder-cc and analytics/client-cc")
endif()
add_subdirectory(${CLIENT_CC_DIR} ${CMAKE_BINARY_DIR}/client-cc)

# -----------------------------------------------------------------------------
# Core static library (testable without main)
# -----------------------------------------------------------------------------
set(FORWARDER_CC_CORE_SOURCES
  config.cc
  pairing.cc
  logging.cc
  gateway_sync.cc
  semtech_udp.cc
  lora_mac.cc
  dedupe.cc
  udp_proxy.cc
  forwarder.cc
)

add_library(forwarder-cc-core STATIC ${FORWARDER_CC_CORE_SOURCES})

target_include_directories(forwarder-cc-core PUBLIC
  ${CMAKE_CURRENT_SOURCE_DIR}
)

target_compile_definitions(forwarder-cc-core PUBLIC
  FORWARDER_CC_GIT_HASH="${FORWARDER_CC_GIT_HASH}"
)

target_link_libraries(forwarder-cc-core PUBLIC
  client-cc
  nlohmann_json::nlohmann_json
)

find_package(Threads QUIET)
if(Threads_FOUND)
  target_link_libraries(forwarder-cc-core PUBLIC Threads::Threads)
endif()

# Size-conscious flags for GNU/Clang
if(CMAKE_CXX_COMPILER_ID MATCHES "GNU|Clang")
  target_compile_options(forwarder-cc-core PRIVATE -ffunction-sections -fdata-sections)
  if(APPLE)
    target_link_options(forwarder-cc-core INTERFACE -Wl,-dead_strip)
  else()
    target_link_options(forwarder-cc-core INTERFACE -Wl,--gc-sections)
  endif()
endif()

# -----------------------------------------------------------------------------
# Executable
# -----------------------------------------------------------------------------
add_executable(forwarder-cc main.cc)
target_link_libraries(forwarder-cc PRIVATE forwarder-cc-core)

install(TARGETS forwarder-cc RUNTIME DESTINATION bin)

# -----------------------------------------------------------------------------
# Tests (never linked into the release binary)
# -----------------------------------------------------------------------------
if(FORWARDER_CC_BUILD_TESTS)
  enable_testing()

  function(forwarder_cc_add_test name src)
    add_executable(${name} ${src})
    target_link_libraries(${name} PRIVATE forwarder-cc-core)
    add_test(NAME ${name} COMMAND ${name})
  endfunction()

  forwarder_cc_add_test(gateway_sync_test tests/gateway_sync_test.cc)
  forwarder_cc_add_test(config_test tests/config_test.cc)
  forwarder_cc_add_test(semtech_udp_test tests/semtech_udp_test.cc)
  forwarder_cc_add_test(forwarder_test tests/forwarder_test.cc)
  forwarder_cc_add_test(dedupe_test tests/dedupe_test.cc)
  forwarder_cc_add_test(udp_proxy_test tests/udp_proxy_test.cc)
  forwarder_cc_add_test(pairing_test tests/pairing_test.cc)
endif()
