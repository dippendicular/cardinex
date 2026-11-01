"""cffi ABI-mode binding to libcardinex (no compiler needed at install)."""
import os
import sys

from cffi import FFI

ffi = FFI()
ffi.cdef("""
typedef struct { double value; double lower; double upper; int32_t empty; } cdnx_estimate_t;
int32_t cdnx_last_error(char *buf, size_t len);
void cdnx_bytes_free(uint8_t *p);
int32_t cdnx_new(const char *kind, const char *params_json, uint64_t *out);
int32_t cdnx_unmarshal(const uint8_t *buf, size_t len, uint64_t *out);
int32_t cdnx_marshal(uint64_t h, uint8_t **out, size_t *out_len);
int32_t cdnx_merge(uint64_t into, uint64_t from);
int32_t cdnx_kind(uint64_t h, uint16_t *out);
int32_t cdnx_stats_json(uint64_t h, uint8_t **out, size_t *out_len);
int32_t cdnx_free(uint64_t h);
int32_t cdnx_add_key(uint64_t h, const uint8_t *key, size_t len, uint64_t n);
int32_t cdnx_hll_estimate(uint64_t h, cdnx_estimate_t *out);
int32_t cdnx_cms_count(uint64_t h, const uint8_t *key, size_t len, cdnx_estimate_t *out);
int32_t cdnx_bloom_contains(uint64_t h, const uint8_t *key, size_t len, int32_t *out);
int32_t cdnx_semantic_add(uint64_t h, const float *vec, size_t dim, double value, uint64_t id);
int32_t cdnx_semantic_add_batch(uint64_t h, const float *vecs, size_t n, size_t dim,
                                const double *values, const uint64_t *ids, int64_t *fail_index);
int32_t cdnx_semantic_topk_json(uint64_t h, int32_t k, const char *by, uint8_t **out, size_t *out_len);
int32_t cdnx_semantic_lookup_json(uint64_t h, const float *vec, size_t dim, uint8_t **out, size_t *out_len);
int32_t cdnx_semantic_diff_json(uint64_t a, uint64_t b, uint8_t **out, size_t *out_len);
""")


def _libname():
    if sys.platform == "win32":
        return "cardinex.dll"
    if sys.platform == "darwin":
        return "libcardinex.dylib"
    return "libcardinex.so"


lib = ffi.dlopen(os.environ.get("CARDINEX_LIB") or os.path.join(os.path.dirname(__file__), _libname()))
