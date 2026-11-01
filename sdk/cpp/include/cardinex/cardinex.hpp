// cardinex.hpp — header-only C++17 RAII wrapper over cardinex.h.
#pragma once
#include <cstdint>
#include <stdexcept>
#include <string>
#include <string_view>
#include <utility>

extern "C" {
#include "cardinex.h"
}

namespace cardinex {

class Error : public std::runtime_error {
 public:
  Error(int32_t code, const std::string& msg, int64_t index = -1)
      : std::runtime_error(msg), code(code), index(index) {}
  int32_t code;
  int64_t index;  // first failing batch item, or -1
};

inline void check(int32_t st, int64_t index = -1) {
  if (st != CDNX_OK) {
    char buf[1024];
    cdnx_last_error(buf, sizeof buf);
    throw Error(st, buf, index);
  }
}

struct Estimate {
  double value, lower, upper;
  bool empty;
};

inline Estimate from(const cdnx_estimate_t& e) { return {e.value, e.lower, e.upper, e.empty != 0}; }

inline std::string take(uint8_t* p, size_t n) {
  std::string s(reinterpret_cast<char*>(p), n);
  cdnx_bytes_free(p);
  return s;
}

// Sketch owns a handle; move-only.
class Sketch {
 public:
  explicit Sketch(std::string_view kind, const std::string& params_json = "{}") {
    check(cdnx_new(std::string(kind).c_str(), params_json.c_str(), &h_));
  }
  static Sketch load(const std::string& bytes) {
    uint64_t h = 0;
    check(cdnx_unmarshal(reinterpret_cast<const uint8_t*>(bytes.data()), bytes.size(), &h));
    return Sketch(h);
  }
  Sketch(Sketch&& o) noexcept : h_(std::exchange(o.h_, 0)) {}
  Sketch& operator=(Sketch&& o) noexcept {
    if (this != &o) {
      reset();
      h_ = std::exchange(o.h_, 0);
    }
    return *this;
  }
  Sketch(const Sketch&) = delete;
  Sketch& operator=(const Sketch&) = delete;
  ~Sketch() { reset(); }

  uint16_t kind() const {
    uint16_t k = 0;
    check(cdnx_kind(h_, &k));
    return k;
  }
  void merge(const Sketch& other) { check(cdnx_merge(h_, other.h_)); }
  std::string bytes() const { return out(cdnx_marshal); }
  std::string stats_json() const { return out(cdnx_stats_json); }

  // Classic sketches.
  void add_key(std::string_view key, uint64_t n = 1) {
    check(cdnx_add_key(h_, reinterpret_cast<const uint8_t*>(key.data()), key.size(), n));
  }
  Estimate hll_estimate() const {
    cdnx_estimate_t e;
    check(cdnx_hll_estimate(h_, &e));
    return from(e);
  }
  Estimate cms_count(std::string_view key) const {
    cdnx_estimate_t e;
    check(cdnx_cms_count(h_, reinterpret_cast<const uint8_t*>(key.data()), key.size(), &e));
    return from(e);
  }
  bool bloom_contains(std::string_view key) const {
    int32_t r = 0;
    check(cdnx_bloom_contains(h_, reinterpret_cast<const uint8_t*>(key.data()), key.size(), &r));
    return r != 0;
  }

  // Semantic sketch. Batch input is a row-major n×dim float buffer, read in place.
  void add(const float* vec, size_t dim, double value, uint64_t id) {
    check(cdnx_semantic_add(h_, vec, dim, value, id));
  }
  void add_batch(const float* vecs, size_t n, size_t dim, const double* values, const uint64_t* ids) {
    int64_t fail = -1;
    int32_t st = cdnx_semantic_add_batch(h_, vecs, n, dim, values, ids, &fail);
    check(st, fail);
  }
  std::string topk_json(int32_t k, std::string_view by = "count") const {
    uint8_t* p = nullptr;
    size_t n = 0;
    check(cdnx_semantic_topk_json(h_, k, std::string(by).c_str(), &p, &n));
    return take(p, n);
  }
  std::string lookup_json(const float* vec, size_t dim) const {
    uint8_t* p = nullptr;
    size_t n = 0;
    check(cdnx_semantic_lookup_json(h_, vec, dim, &p, &n));
    return take(p, n);
  }
  std::string diff_json(const Sketch& other) const {
    uint8_t* p = nullptr;
    size_t n = 0;
    check(cdnx_semantic_diff_json(h_, other.h_, &p, &n));
    return take(p, n);
  }

  uint64_t handle() const { return h_; }

 private:
  explicit Sketch(uint64_t h) : h_(h) {}
  void reset() {
    if (h_) {
      cdnx_free(h_);
      h_ = 0;
    }
  }
  std::string out(int32_t (*fn)(uint64_t, uint8_t**, size_t*)) const {
    uint8_t* p = nullptr;
    size_t n = 0;
    check(fn(h_, &p, &n));
    return take(p, n);
  }
  uint64_t h_ = 0;
};

}  // namespace cardinex
