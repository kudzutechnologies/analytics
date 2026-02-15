# CMakeLists.txt for client-cc
#
# Usage:
#   mkdir build && cd build
#   cmake ..
#   make
#
# For cross-compiling or embedded builds, use:
#   cmake -DCMAKE_TOOLCHAIN_FILE=your-toolchain.cmake ..
#
# Options:
#   -DCLIENT_CC_BUILD_SHARED=ON/OFF (default: OFF)
#   -DCLIENT_CC_USE_OPENSSL=ON/OFF (default: ON)
#   -DProtobuf_ROOT=/path/to/protobuf
#   -DgRPC_ROOT=/path/to/grpc
#   -DOPENSSL_ROOT_DIR=/path/to/openssl
#   -DCLIENT_CC_EXTRA_SOURCES="/path/to/sha256_fallback.c"
#
# Requires: gRPC, Protobuf, OpenSSL (unless disabled)

cmake_minimum_required(VERSION 3.14)
project(client_cc LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_POSITION_INDEPENDENT_CODE ON)

option(CLIENT_CC_BUILD_SHARED "Build shared library" OFF)
option(CLIENT_CC_USE_OPENSSL "Use OpenSSL for SHA256" ON)
set(CLIENT_CC_EXTRA_SOURCES "" CACHE STRING "Extra user-supplied sources (e.g., fallback crypto)")

# Allow user to specify custom dependency roots
set(Protobuf_ROOT "${Protobuf_ROOT}" CACHE PATH "Protobuf root directory")
set(gRPC_ROOT "${gRPC_ROOT}" CACHE PATH "gRPC root directory")
set(OPENSSL_ROOT_DIR "${OPENSSL_ROOT_DIR}" CACHE PATH "OpenSSL root directory")

# Workaround for Homebrew Protobuf CMake target issues
set(Protobuf_MODULE_COMPATIBLE ON)
set(Protobuf_USE_STATIC_LIBS ON)
set(Protobuf_USE_MODULES OFF)

# Find Protobuf: try CONFIG first (Protobuf 5.x / Homebrew uses this and does not set Protobuf_LIBRARIES)
find_package(Protobuf CONFIG QUIET)
if(NOT TARGET protobuf::libprotobuf)
    find_package(Protobuf REQUIRED)
endif()
if(TARGET protobuf::libprotobuf)
    set(CLIENT_CC_PROTOBUF_LIBS protobuf::libprotobuf)
    set(CLIENT_CC_PROTOBUF_INCLUDES)
elseif(Protobuf_LIBRARIES)
    set(CLIENT_CC_PROTOBUF_LIBS ${Protobuf_LIBRARIES})
    set(CLIENT_CC_PROTOBUF_INCLUDES ${Protobuf_INCLUDE_DIRS})
else()
    message(FATAL_ERROR "Protobuf: need protobuf::libprotobuf (CONFIG) or Protobuf_LIBRARIES. Homebrew Protobuf 5.x requires CONFIG.")
endif()

# Find gRPC
find_package(gRPC REQUIRED)

# OpenSSL is optional
if(CLIENT_CC_USE_OPENSSL)
    find_package(OpenSSL)
    if(NOT OpenSSL_FOUND)
        message(WARNING "OpenSSL not found, but CLIENT_CC_USE_OPENSSL=ON. You may need to provide a fallback SHA256 implementation.")
    endif()
endif()

# Include directories (src so that "protobuf/api.pb.h" resolves to src/protobuf/api.pb.h)
set(CLIENT_CC_INCLUDE_DIRS
    ${CMAKE_CURRENT_SOURCE_DIR}/include
    ${CMAKE_CURRENT_SOURCE_DIR}/src
    ${CMAKE_CURRENT_SOURCE_DIR}/src/protobuf
    ${CLIENT_CC_PROTOBUF_INCLUDES}
    ${gRPC_INCLUDE_DIRS}
)
if(CLIENT_CC_USE_OPENSSL AND OpenSSL_FOUND)
    list(APPEND CLIENT_CC_INCLUDE_DIRS ${OPENSSL_INCLUDE_DIR})
endif()

# Source files (include generated protobuf and gRPC stubs)
set(CLIENT_CC_SOURCES
    src/Client.cc
    src/protobuf/api.pb.cc
    src/protobuf/analytics.pb.cc
    src/protobuf/api.grpc.pb.cc
    ${CLIENT_CC_EXTRA_SOURCES}
)

# Platform/architecture-specific sources or definitions (user can edit here)
# Example: add_definitions(-DPLATFORM_EMBEDDED)
# Example: list(APPEND CLIENT_CC_SOURCES src/sha256_fallback.c)

# Library type
if(CLIENT_CC_BUILD_SHARED)
    set(CLIENT_CC_LIB_TYPE SHARED)
else()
    set(CLIENT_CC_LIB_TYPE STATIC)
endif()

add_library(client-cc ${CLIENT_CC_LIB_TYPE} ${CLIENT_CC_SOURCES})

# Modern CMake: target includes and definitions
# (User can override/add more flags for embedded/cross builds)
target_include_directories(client-cc
    PUBLIC
        $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
        $<INSTALL_INTERFACE:include>
        ${CLIENT_CC_INCLUDE_DIRS}
)

# Link dependencies
set(THREADS_PREFER_PTHREAD_FLAG ON)
find_package(Threads REQUIRED)

target_link_libraries(client-cc
    PUBLIC
        gRPC::grpc++
        ${CLIENT_CC_PROTOBUF_LIBS}
        Threads::Threads
)
if(CLIENT_CC_USE_OPENSSL AND OpenSSL_FOUND)
    target_link_libraries(client-cc PUBLIC OpenSSL::SSL OpenSSL::Crypto)
    target_compile_definitions(client-cc PUBLIC CLIENT_CC_USE_OPENSSL)
else()
    target_compile_definitions(client-cc PUBLIC CLIENT_CC_NO_OPENSSL)
endif()

# Public headers
set_target_properties(client-cc PROPERTIES PUBLIC_HEADER "include/Client.h")

# Install rules (both static and shared)
install(TARGETS client-cc
    LIBRARY DESTINATION lib
    ARCHIVE DESTINATION lib
    PUBLIC_HEADER DESTINATION include
)

# Print summary
message(STATUS "client-cc build type: ${CLIENT_CC_LIB_TYPE}")
message(STATUS "OpenSSL enabled: ${CLIENT_CC_USE_OPENSSL}")
message(STATUS "Extra sources: ${CLIENT_CC_EXTRA_SOURCES}")
message(STATUS "Protobuf: ${CLIENT_CC_PROTOBUF_LIBS}")
message(STATUS "gRPC include: ${gRPC_INCLUDE_DIRS}") 