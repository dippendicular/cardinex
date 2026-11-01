"""Cardinex: mergeable sketches with guaranteed error intervals.

The Go core does all the math; this package is a thin binding over the C ABI.
NumPy float32 C-contiguous arrays cross into the library without copying.
"""
from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Any, Iterable, Optional

import numpy as np

from ._ffi import ffi, lib

__all__ = ["CardinexError", "Estimate", "Semantic", "HLL", "CMS", "Bloom", "load"]
__version__ = "0.1.0"

_CODES = {1: "invalid", 2: "not_found", 3: "incompatible", 4: "corrupt", 5: "wrong_kind", 6: "internal"}


class CardinexError(Exception):
    def __init__(self, code: int, message: str, index: Optional[int] = None):
        super().__init__(f"{_CODES.get(code, code)}: {message}")
        self.code, self.message, self.index = code, message, index


def _check(status: int, index: Optional[int] = None) -> None:
    if status:
        buf = ffi.new("char[1024]")
        lib.cdnx_last_error(buf, 1024)
        raise CardinexError(status, ffi.string(buf).decode(), index)


def _take(out, n) -> bytes:
    try:
        return bytes(ffi.buffer(out[0], n[0]))
    finally:
        lib.cdnx_bytes_free(out[0])


def _json_call(fn, *args) -> Any:
    out, n = ffi.new("uint8_t **"), ffi.new("size_t *")
    _check(fn(*args, out, n))
    return json.loads(_take(out, n))


@dataclass(frozen=True)
class Estimate:
    value: float
    lower: float
    upper: float
    empty: bool = False

    @classmethod
    def _from(cls, e) -> "Estimate":
        return cls(e.value, e.lower, e.upper, bool(e.empty))


def _key(k) -> bytes:
    return bytes(k) if isinstance(k, (bytes, bytearray)) else str(k).encode()


class _Sketch:
    _kind = ""

    def __init__(self, _handle: Optional[int] = None, **params):
        if _handle is None:
            out = ffi.new("uint64_t *")
            _check(lib.cdnx_new(self._kind.encode(), json.dumps(params).encode(), out))
            _handle = out[0]
        self._h = _handle

    def close(self) -> None:
        if getattr(self, "_h", 0):
            lib.cdnx_free(self._h)
            self._h = 0

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        self.close()

    def __del__(self):
        try:
            self.close()
        except Exception:
            pass

    def merge(self, other: "_Sketch") -> None:
        _check(lib.cdnx_merge(self._h, other._h))

    def to_bytes(self) -> bytes:
        out, n = ffi.new("uint8_t **"), ffi.new("size_t *")
        _check(lib.cdnx_marshal(self._h, out, n))
        return _take(out, n)

    def stats(self) -> dict:
        return _json_call(lib.cdnx_stats_json, self._h)


class HLL(_Sketch):
    """HyperLogLog: HLL(precision=14)."""
    _kind = "hll"

    def add(self, key) -> None:
        k = _key(key)
        _check(lib.cdnx_add_key(self._h, k, len(k), 0))

    def update(self, keys: Iterable) -> None:
        for k in keys:
            self.add(k)

    def estimate(self) -> Estimate:
        e = ffi.new("cdnx_estimate_t *")
        _check(lib.cdnx_hll_estimate(self._h, e))
        return Estimate._from(e)


class CMS(_Sketch):
    """Count-Min: CMS(width=2720, depth=5, conservative=False) or CMS(epsilon=, delta=)."""
    _kind = "cms"

    def add(self, key, n: int = 1) -> None:
        k = _key(key)
        _check(lib.cdnx_add_key(self._h, k, len(k), n))

    def count(self, key) -> Estimate:
        k, e = _key(key), ffi.new("cdnx_estimate_t *")
        _check(lib.cdnx_cms_count(self._h, k, len(k), e))
        return Estimate._from(e)


class Bloom(_Sketch):
    """Bloom: Bloom(expected_elements=, false_positive_rate=) or Bloom(bits=, hashes=)."""
    _kind = "bloom"

    def add(self, key) -> None:
        k = _key(key)
        _check(lib.cdnx_add_key(self._h, k, len(k), 0))

    def __contains__(self, key) -> bool:
        k, out = _key(key), ffi.new("int32_t *")
        _check(lib.cdnx_bloom_contains(self._h, k, len(k), out))
        return bool(out[0])


class Semantic(_Sketch):
    """Semantic GROUP BY sketch: Semantic(dim=, tau=, seed=, capacity=4096, ...)."""
    _kind = "semantic"

    def add(self, vec, value: float = 1.0, item_id: int = 0) -> None:
        v = np.ascontiguousarray(vec, dtype=np.float32)
        _check(lib.cdnx_semantic_add(self._h, ffi.from_buffer("float[]", v), v.shape[0], float(value), int(item_id)))

    def add_batch(self, vecs: np.ndarray, values=None, ids=None) -> None:
        """All-or-nothing. vecs is (n, dim) float32; C-contiguous input is not copied."""
        v = np.ascontiguousarray(vecs, dtype=np.float32)
        if v.ndim != 2:
            raise ValueError("vecs must be 2-D (n, dim)")
        n, d = v.shape
        vals = np.ascontiguousarray(np.ones(n) if values is None else values, dtype=np.float64)
        idv = np.ascontiguousarray(np.arange(1, n + 1) if ids is None else ids, dtype=np.uint64)
        if vals.shape != (n,) or idv.shape != (n,):
            raise ValueError("values and ids must have length n")
        fail = ffi.new("int64_t *")
        st = lib.cdnx_semantic_add_batch(self._h, ffi.from_buffer("float[]", v), n, d,
                                         ffi.from_buffer("double[]", vals), ffi.from_buffer("uint64_t[]", idv), fail)
        _check(st, fail[0] if fail[0] >= 0 else None)

    def topk(self, k: int = 10, by: str = "count") -> dict:
        return _json_call(lib.cdnx_semantic_topk_json, self._h, k, by.encode())

    def lookup(self, vec) -> dict:
        v = np.ascontiguousarray(vec, dtype=np.float32)
        return _json_call(lib.cdnx_semantic_lookup_json, self._h, ffi.from_buffer("float[]", v), v.shape[0])

    def diff(self, other: "Semantic") -> dict:
        return _json_call(lib.cdnx_semantic_diff_json, self._h, other._h)


_BY_KIND = {1: HLL, 2: CMS, 3: Bloom, 4: Semantic}


def load(data: bytes) -> _Sketch:
    """Decode CDNX bytes into the matching sketch class."""
    out, kind = ffi.new("uint64_t *"), ffi.new("uint16_t *")
    _check(lib.cdnx_unmarshal(ffi.from_buffer(data), len(data), out))
    lib.cdnx_kind(out[0], kind)
    return _BY_KIND[kind[0]](_handle=out[0])
