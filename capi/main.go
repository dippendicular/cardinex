// Command capi builds libcardinex, the C ABI over the Go core:
//
//	go build -buildmode=c-shared -o libcardinex.so ./capi
//
// The public header is capi/cardinex.h (not the cgo-generated one).
package main

/*
#include <stdlib.h>
#include "cdnx_types.h"
void cdnx_set_err(const char *s);
*/
import "C"

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/dippendicular/cardinex/core/build"
	"github.com/dippendicular/cardinex/core/classic"
	"github.com/dippendicular/cardinex/core/semantic"
	"github.com/dippendicular/cardinex/core/serial"
	"github.com/dippendicular/cardinex/core/sketch"
)

func main() {}

type handle struct {
	id uint64
	mu sync.Mutex
	sk sketch.Sketch
}

var (
	handles sync.Map // uint64 -> *handle
	nextID  atomic.Uint64
)

const (
	okStatus     = C.CDNX_OK
	eInval       = C.CDNX_EINVAL
	eNotFound    = C.CDNX_ENOTFOUND
	eIncompat    = C.CDNX_EINCOMPATIBLE
	eCorrupt     = C.CDNX_ECORRUPT
	eKind        = C.CDNX_EKIND
	eInternal    = C.CDNX_EINTERNAL
	errNoHandle  = "cardinex: unknown handle"
	errWrongKind = "cardinex: operation not supported for this sketch kind"
)

func setErr(msg string) {
	cs := C.CString(msg)
	C.cdnx_set_err(cs)
	C.free(unsafe.Pointer(cs))
}

func fail(code C.int32_t, err error) C.int32_t {
	setErr(err.Error())
	return code
}

func classify(err error) C.int32_t {
	switch {
	case errors.Is(err, sketch.ErrIncompatible):
		return fail(eIncompat, err)
	case errors.Is(err, serial.ErrCorrupt), errors.Is(err, serial.ErrCRC), errors.Is(err, serial.ErrMagic),
		errors.Is(err, serial.ErrTruncated), errors.Is(err, serial.ErrVersion), errors.Is(err, serial.ErrKind):
		return fail(eCorrupt, err)
	}
	return fail(eInval, err)
}

// guard converts panics into CDNX_EINTERNAL.
func guard(status *C.int32_t) {
	if r := recover(); r != nil {
		*status = fail(eInternal, fmt.Errorf("cardinex: internal error: %v", r))
	}
}

func register(sk sketch.Sketch) C.uint64_t {
	h := &handle{id: nextID.Add(1), sk: sk}
	handles.Store(h.id, h)
	return C.uint64_t(h.id)
}

func lookup(h C.uint64_t) (*handle, bool) {
	v, ok := handles.Load(uint64(h))
	if !ok {
		setErr(errNoHandle)
		return nil, false
	}
	return v.(*handle), true
}

func goBytes(p *C.uint8_t, n C.size_t) []byte {
	if p == nil || n == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(p)), int(n))
}

func emit(b []byte, out **C.uint8_t, outLen *C.size_t) C.int32_t {
	if out == nil || outLen == nil {
		return fail(eInval, errors.New("cardinex: null output pointer"))
	}
	p := (*C.uint8_t)(C.malloc(C.size_t(max(len(b), 1))))
	copy(unsafe.Slice((*byte)(unsafe.Pointer(p)), len(b)), b)
	*out, *outLen = p, C.size_t(len(b))
	return okStatus
}

func emitJSON(v any, out **C.uint8_t, outLen *C.size_t) C.int32_t {
	b, err := json.Marshal(v)
	if err != nil {
		return fail(eInternal, err)
	}
	return emit(b, out, outLen)
}

//export cdnx_bytes_free
func cdnx_bytes_free(p *C.uint8_t) { C.free(unsafe.Pointer(p)) }

//export cdnx_new
func cdnx_new(kind *C.char, params *C.char, out *C.uint64_t) (status C.int32_t) {
	defer guard(&status)
	if kind == nil || out == nil {
		return fail(eInval, errors.New("cardinex: null argument"))
	}
	var raw json.RawMessage
	if params != nil {
		raw = json.RawMessage(C.GoString(params))
	}
	sk, err := build.New(C.GoString(kind), raw)
	if err != nil {
		return fail(eInval, err)
	}
	*out = register(sk)
	return okStatus
}

//export cdnx_unmarshal
func cdnx_unmarshal(buf *C.uint8_t, n C.size_t, out *C.uint64_t) (status C.int32_t) {
	defer guard(&status)
	if out == nil {
		return fail(eInval, errors.New("cardinex: null output pointer"))
	}
	sk, err := build.Decode(goBytes(buf, n))
	if err != nil {
		return classify(err)
	}
	*out = register(sk)
	return okStatus
}

//export cdnx_marshal
func cdnx_marshal(h C.uint64_t, out **C.uint8_t, outLen *C.size_t) (status C.int32_t) {
	defer guard(&status)
	x, ok := lookup(h)
	if !ok {
		return eNotFound
	}
	x.mu.Lock()
	b, err := x.sk.MarshalBinary()
	x.mu.Unlock()
	if err != nil {
		return fail(eInternal, err)
	}
	return emit(b, out, outLen)
}

//export cdnx_merge
func cdnx_merge(into, from C.uint64_t) (status C.int32_t) {
	defer guard(&status)
	a, ok := lookup(into)
	if !ok {
		return eNotFound
	}
	b, ok := lookup(from)
	if !ok {
		return eNotFound
	}
	if a == b {
		return fail(eInval, errors.New("cardinex: cannot merge a sketch into itself"))
	}
	first, second := a, b
	if b.id < a.id {
		first, second = b, a
	}
	first.mu.Lock()
	second.mu.Lock()
	err := a.sk.Merge(b.sk)
	second.mu.Unlock()
	first.mu.Unlock()
	if err != nil {
		return classify(err)
	}
	return okStatus
}

//export cdnx_kind
func cdnx_kind(h C.uint64_t, out *C.uint16_t) (status C.int32_t) {
	defer guard(&status)
	x, ok := lookup(h)
	if !ok {
		return eNotFound
	}
	*out = C.uint16_t(x.sk.Kind())
	return okStatus
}

//export cdnx_stats_json
func cdnx_stats_json(h C.uint64_t, out **C.uint8_t, outLen *C.size_t) (status C.int32_t) {
	defer guard(&status)
	x, ok := lookup(h)
	if !ok {
		return eNotFound
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	var v any
	switch s := x.sk.(type) {
	case *semantic.Sketch:
		v = struct {
			Params semantic.Params `json:"params"`
			semantic.Stats
		}{s.Params(), s.Stats()}
	case *classic.HLL:
		v = map[string]any{"kind": "hll", "precision": s.Precision(), "estimate": s.Estimate(), "relative_error": s.RelativeError(), "memory_bytes": s.MemoryBytes()}
	case *classic.CMS:
		v = map[string]any{"kind": "cms", "width": s.Width(), "depth": s.Depth(), "conservative": s.Conservative(), "total": s.Total(), "epsilon": s.Epsilon(), "confidence": s.Confidence(), "memory_bytes": s.MemoryBytes()}
	case *classic.Bloom:
		v = map[string]any{"kind": "bloom", "bits": s.Bits(), "hashes": s.Hashes(), "set_bits": s.SetBits(), "false_positive_rate": s.FalsePositiveRate(), "cardinality": s.EstimateCardinality(), "memory_bytes": s.MemoryBytes()}
	}
	return emitJSON(v, out, outLen)
}

//export cdnx_free
func cdnx_free(h C.uint64_t) C.int32_t {
	if _, ok := handles.LoadAndDelete(uint64(h)); !ok {
		setErr(errNoHandle)
		return eNotFound
	}
	return okStatus
}

//export cdnx_add_key
func cdnx_add_key(h C.uint64_t, key *C.uint8_t, n C.size_t, count C.uint64_t) (status C.int32_t) {
	defer guard(&status)
	x, ok := lookup(h)
	if !ok {
		return eNotFound
	}
	k := goBytes(key, n)
	x.mu.Lock()
	defer x.mu.Unlock()
	switch s := x.sk.(type) {
	case *classic.CMS:
		if count == 0 {
			count = 1
		}
		s.AddKey(k, uint64(count))
	case sketch.Cardinality:
		s.AddKey(k)
	case sketch.Membership:
		s.AddKey(k)
	default:
		return fail(eKind, errors.New(errWrongKind))
	}
	return okStatus
}

func putEstimate(out *C.cdnx_estimate_t, e sketch.Estimate) {
	out.value, out.lower, out.upper = C.double(e.Value), C.double(e.Lower), C.double(e.Upper)
	out.empty = 0
	if e.Empty {
		out.empty = 1
	}
}

//export cdnx_hll_estimate
func cdnx_hll_estimate(h C.uint64_t, out *C.cdnx_estimate_t) (status C.int32_t) {
	defer guard(&status)
	x, ok := lookup(h)
	if !ok {
		return eNotFound
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	s, ok := x.sk.(*classic.HLL)
	if !ok {
		return fail(eKind, errors.New(errWrongKind))
	}
	putEstimate(out, s.Estimate())
	return okStatus
}

//export cdnx_cms_count
func cdnx_cms_count(h C.uint64_t, key *C.uint8_t, n C.size_t, out *C.cdnx_estimate_t) (status C.int32_t) {
	defer guard(&status)
	x, ok := lookup(h)
	if !ok {
		return eNotFound
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	s, ok := x.sk.(*classic.CMS)
	if !ok {
		return fail(eKind, errors.New(errWrongKind))
	}
	putEstimate(out, s.Count(goBytes(key, n)))
	return okStatus
}

//export cdnx_bloom_contains
func cdnx_bloom_contains(h C.uint64_t, key *C.uint8_t, n C.size_t, out *C.int32_t) (status C.int32_t) {
	defer guard(&status)
	x, ok := lookup(h)
	if !ok {
		return eNotFound
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	s, ok := x.sk.(*classic.Bloom)
	if !ok {
		return fail(eKind, errors.New(errWrongKind))
	}
	*out = 0
	if s.Contains(goBytes(key, n)) {
		*out = 1
	}
	return okStatus
}

func semanticOf(h C.uint64_t) (*semantic.Sketch, C.int32_t) {
	x, ok := lookup(h)
	if !ok {
		return nil, eNotFound
	}
	s, ok := x.sk.(*semantic.Sketch)
	if !ok {
		return nil, fail(eKind, errors.New(errWrongKind))
	}
	return s, okStatus
}

// floats views caller memory without copying; it is only used during the call.
func floats(p *C.float, n int) []float32 {
	if p == nil || n == 0 {
		return nil
	}
	return unsafe.Slice((*float32)(unsafe.Pointer(p)), n)
}

//export cdnx_semantic_add
func cdnx_semantic_add(h C.uint64_t, vec *C.float, dim C.size_t, value C.double, id C.uint64_t) (status C.int32_t) {
	defer guard(&status)
	s, st := semanticOf(h)
	if s == nil {
		return st
	}
	if err := s.Add(floats(vec, int(dim)), float64(value), uint64(id)); err != nil {
		return fail(eInval, err)
	}
	return okStatus
}

//export cdnx_semantic_add_batch
func cdnx_semantic_add_batch(h C.uint64_t, vecs *C.float, n, dim C.size_t, values *C.double, ids *C.uint64_t, failIndex *C.int64_t) (status C.int32_t) {
	defer guard(&status)
	if failIndex != nil {
		*failIndex = -1
	}
	s, st := semanticOf(h)
	if s == nil {
		return st
	}
	if n == 0 {
		return okStatus
	}
	if vecs == nil || values == nil || ids == nil {
		return fail(eInval, errors.New("cardinex: null batch pointer"))
	}
	flat := floats(vecs, int(n*dim))
	rows := make([][]float32, int(n))
	for i := range rows {
		rows[i] = flat[i*int(dim) : (i+1)*int(dim) : (i+1)*int(dim)]
	}
	vals := unsafe.Slice((*float64)(unsafe.Pointer(values)), int(n))
	idv := unsafe.Slice((*uint64)(unsafe.Pointer(ids)), int(n))
	if err := s.AddBatch(rows, vals, idv); err != nil {
		var be *semantic.BatchError
		if errors.As(err, &be) && failIndex != nil {
			*failIndex = C.int64_t(be.Index)
		}
		return fail(eInval, err)
	}
	return okStatus
}

func order(by *C.char) (semantic.Order, error) {
	if by == nil {
		return semantic.ByCount, nil
	}
	switch C.GoString(by) {
	case "", "count":
		return semantic.ByCount, nil
	case "sum":
		return semantic.BySum, nil
	case "mean":
		return semantic.ByMean, nil
	}
	return 0, errors.New("cardinex: by must be count, sum or mean")
}

//export cdnx_semantic_topk_json
func cdnx_semantic_topk_json(h C.uint64_t, k C.int32_t, by *C.char, out **C.uint8_t, outLen *C.size_t) (status C.int32_t) {
	defer guard(&status)
	s, st := semanticOf(h)
	if s == nil {
		return st
	}
	o, err := order(by)
	if err != nil {
		return fail(eInval, err)
	}
	uc, _ := s.Unmonitored()
	stats := s.Stats()
	return emitJSON(map[string]any{"entities": s.TopK(int(k), o), "w": stats.W, "w_q": stats.Wq, "unmonitored_count": uc}, out, outLen)
}

//export cdnx_semantic_lookup_json
func cdnx_semantic_lookup_json(h C.uint64_t, vec *C.float, dim C.size_t, out **C.uint8_t, outLen *C.size_t) (status C.int32_t) {
	defer guard(&status)
	s, st := semanticOf(h)
	if s == nil {
		return st
	}
	ent, found := s.Lookup(floats(vec, int(dim)))
	if !found {
		uc, _ := s.Unmonitored()
		return emitJSON(map[string]any{"found": false, "unmonitored_count": uc}, out, outLen)
	}
	return emitJSON(map[string]any{"found": true, "entity": ent}, out, outLen)
}

//export cdnx_semantic_diff_json
func cdnx_semantic_diff_json(a, b C.uint64_t, out **C.uint8_t, outLen *C.size_t) (status C.int32_t) {
	defer guard(&status)
	sa, st := semanticOf(a)
	if sa == nil {
		return st
	}
	sb, st := semanticOf(b)
	if sb == nil {
		return st
	}
	d, err := sa.Diff(sb)
	if err != nil {
		return classify(err)
	}
	return emitJSON(map[string]any{"deltas": d}, out, outLen)
}
