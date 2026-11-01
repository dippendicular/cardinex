cmake_minimum_required(VERSION 3.16)
project(cardinex CXX)

# CARDINEX_LIB_DIR holds libcardinex + cardinex.h (scripts/build-lib.sh output).
set(CARDINEX_LIB_DIR "${CMAKE_CURRENT_SOURCE_DIR}/../../dist" CACHE PATH "libcardinex directory")
find_library(CARDINEX_LIBRARY NAMES cardinex libcardinex PATHS ${CARDINEX_LIB_DIR} NO_DEFAULT_PATH REQUIRED)

add_library(cardinex INTERFACE)
add_library(cardinex::cardinex ALIAS cardinex)
target_compile_features(cardinex INTERFACE cxx_std_17)
target_include_directories(cardinex INTERFACE
  $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
  $<BUILD_INTERFACE:${CARDINEX_LIB_DIR}>
  $<INSTALL_INTERFACE:include>)
target_link_libraries(cardinex INTERFACE ${CARDINEX_LIBRARY})

install(DIRECTORY include/ DESTINATION include)
install(TARGETS cardinex EXPORT cardinexTargets)
install(EXPORT cardinexTargets NAMESPACE cardinex:: DESTINATION lib/cmake/cardinex)

option(CARDINEX_BUILD_TESTS "Build the conformance runner and example" ON)
if(CARDINEX_BUILD_TESTS)
  include(FetchContent)
  FetchContent_Declare(json URL https://github.com/nlohmann/json/releases/download/v3.11.3/json.tar.xz)
  FetchContent_MakeAvailable(json)
  add_executable(conformance tests/conformance.cpp)
  target_link_libraries(conformance PRIVATE cardinex::cardinex nlohmann_json::nlohmann_json)
  add_executable(example_batch examples/batch.cpp)
  target_link_libraries(example_batch PRIVATE cardinex::cardinex)
endif()
