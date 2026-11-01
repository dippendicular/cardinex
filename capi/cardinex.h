/*
 * cardinex.h — C ABI for libcardinex (docs: CAPI.md).
 *
 * Rules:
 *  - Sketches are opaque uint64_t handles. Free each with cdnx_free.
 *  - Every function returns int32_t status (CDNX_OK on success). On failure,
 *    cdnx_last_error copies a thread-local message.
 *  - Callers own input buffers; they are read during the call only and never
 *    retained. Vector batches are read in place (zero copy).
 *  - Output byte buffers (*out, *out_len) are allocated by the library; free
 *    them with cdnx_bytes_free.
 *  - JSON outputs use the same shape as the HTTP API.
 *  - One Go runtime per process: do not load two different Go c-shared
 *    libraries into the same process.
 */
#ifndef CARDINEX_H
#define CARDINEX_H
#include "cdnx_types.h"

#ifdef __cplusplus
extern "C" {
#endif

int32_t cdnx_last_error(char *buf, size_t len);
void cdnx_bytes_free(uint8_t *p);

/* kind: "semantic", "hll", "cms", "bloom"; params_json may be NULL. */
int32_t cdnx_new(const char *kind, const char *params_json, uint64_t *out);
int32_t cdnx_unmarshal(const uint8_t *buf, size_t len, uint64_t *out);
int32_t cdnx_marshal(uint64_t h, uint8_t **out, size_t *out_len);
int32_t cdnx_merge(uint64_t into, uint64_t from);
int32_t cdnx_kind(uint64_t h, uint16_t *out); /* 1 hll, 2 cms, 3 bloom, 4 semantic */
int32_t cdnx_stats_json(uint64_t h, uint8_t **out, size_t *out_len);
int32_t cdnx_free(uint64_t h);

/* Classic sketches. n is the occurrence count for CMS and ignored otherwise. */
int32_t cdnx_add_key(uint64_t h, const uint8_t *key, size_t len, uint64_t n);
int32_t cdnx_hll_estimate(uint64_t h, cdnx_estimate_t *out);
int32_t cdnx_cms_count(uint64_t h, const uint8_t *key, size_t len, cdnx_estimate_t *out);
int32_t cdnx_bloom_contains(uint64_t h, const uint8_t *key, size_t len, int32_t *out);

/* Semantic sketch. vecs is n×dim row-major float32. AddBatch is
 * all-or-nothing; on failure *fail_index is the first bad item (else -1). */
int32_t cdnx_semantic_add(uint64_t h, const float *vec, size_t dim, double value, uint64_t id);
int32_t cdnx_semantic_add_batch(uint64_t h, const float *vecs, size_t n, size_t dim,
                                const double *values, const uint64_t *ids, int64_t *fail_index);
/* by: "count", "sum" or "mean"; k <= 0 returns all. */
int32_t cdnx_semantic_topk_json(uint64_t h, int32_t k, const char *by, uint8_t **out, size_t *out_len);
int32_t cdnx_semantic_lookup_json(uint64_t h, const float *vec, size_t dim, uint8_t **out, size_t *out_len);
int32_t cdnx_semantic_diff_json(uint64_t a, uint64_t b, uint8_t **out, size_t *out_len);

#ifdef __cplusplus
}
#endif
#endif
