// Package service hosts sketches behind HTTP and MCP. The Registry owns the
// sketches; each one has its own lock, and all serialization happens under
// that lock. Persistence is write-behind: writes are applied in memory and,
// when LogWrites is on, appended to a per-sketch log; dirty sketches are
// snapshotted every FlushInterval and the log is trimmed. On startup the
// snapshot is loaded and the log replayed. The loss window is FlushInterval
// with LogWrites off, and zero with it on.
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/dippendicular/cardinex/core/build"
	"github.com/dippendicular/cardinex/core/classic"
	"github.com/dippendicular/cardinex/core/semantic"
	"github.com/dippendicular/cardinex/core/sketch"
)

// Errors with stable codes, mapped to HTTP statuses and MCP errors.
var (
	ErrNotFound     = &Error{Code: "not_found", Msg: "sketch not found"}
	ErrNameTaken    = &Error{Code: "name_taken", Msg: "a sketch with that name exists"}
	ErrWrongKind    = &Error{Code: "wrong_kind", Msg: "operation not supported for this sketch kind"}
	ErrIncompatible = &Error{Code: "incompatible", Msg: "sketches have different parameters"}
	ErrUnavailable  = &Error{Code: "unavailable", Msg: "sketch persistence outcome is unresolved; reload before retrying"}
)

// Error is a client-facing error.
type Error struct {
	Code string `json:"code"`
	Msg  string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Msg }

func badRequest(format string, a ...any) error {
	return &Error{Code: "bad_request", Msg: fmt.Sprintf(format, a...)}
}

const batchMemory = 10000

// batchSet remembers the most recent batch IDs, in arrival order.
type batchSet struct {
	order []string
	seen  map[string]bool
}

func newBatchSet(ids []string) *batchSet {
	b := &batchSet{seen: map[string]bool{}}
	for _, id := range ids {
		b.add(id)
	}
	return b
}

func (b *batchSet) add(id string) {
	if id == "" || b.seen[id] {
		return
	}
	b.seen[id] = true
	b.order = append(b.order, id)
	if len(b.order) > batchMemory {
		delete(b.seen, b.order[0])
		b.order = b.order[1:]
	}
}

// Entry is one hosted sketch.
type Entry struct {
	ID   string
	Name string
	Kind sketch.Kind

	mu      sync.Mutex
	sk      sketch.Sketch
	dirty   bool
	logLen  int64
	batches *batchSet
	// blocked is set after a store error whose remote commit outcome cannot be
	// known from the Store interface.  Fail closed rather than continue from a
	// state that may disagree with the durable log.
	blocked bool
}

// Registry owns the hosted sketches.
type Registry struct {
	mu     sync.RWMutex
	byID   map[string]*Entry
	byName map[string]*Entry

	store     Store
	logWrites bool
	log       *slog.Logger
}

// NewRegistry builds a registry on store. logWrites appends every write to the
// store's log before acknowledging it.
func NewRegistry(store Store, logWrites bool, log *slog.Logger) *Registry {
	if log == nil {
		log = slog.Default()
	}
	return &Registry{byID: map[string]*Entry{}, byName: map[string]*Entry{}, store: store, logWrites: logWrites, log: log}
}

func kindName(k sketch.Kind) string {
	switch k {
	case sketch.KindHLL:
		return "hll"
	case sketch.KindCMS:
		return "cms"
	case sketch.KindBloom:
		return "bloom"
	case sketch.KindSemantic:
		return "semantic"
	}
	return "unknown"
}

func newID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Build constructs a sketch of the named kind from JSON params.
func Build(kind string, params json.RawMessage) (sketch.Sketch, error) {
	sk, err := build.New(kind, params)
	if err != nil {
		return nil, badRequest("%v", err)
	}
	return sk, nil
}

// Decode turns CDNX bytes into a sketch of any kind.
func Decode(b []byte) (sketch.Sketch, error) { return build.Decode(b) }

// Create registers a new sketch.
func (r *Registry) Create(ctx context.Context, kind, name string, params json.RawMessage) (*Entry, error) {
	sk, err := Build(kind, params)
	if err != nil {
		return nil, err
	}
	return r.add(ctx, name, sk)
}

// Import registers a sketch decoded from CDNX bytes.
func (r *Registry) Import(ctx context.Context, name string, b []byte) (*Entry, error) {
	sk, err := Decode(b)
	if err != nil {
		return nil, badRequest("import: %v", err)
	}
	return r.add(ctx, name, sk)
}

func (r *Registry) add(ctx context.Context, name string, sk sketch.Sketch) (*Entry, error) {
	e := &Entry{ID: newID(), Name: name, Kind: sk.Kind(), sk: sk, batches: newBatchSet(nil)}
	r.mu.Lock()
	if name != "" && r.byName[name] != nil {
		r.mu.Unlock()
		return nil, ErrNameTaken
	}
	r.byID[e.ID] = e
	if name != "" {
		r.byName[name] = e
	}
	r.mu.Unlock()
	// Save immediately so log appends always follow a snapshot.
	if err := r.flushEntry(ctx, e); err != nil {
		r.remove(e)
		return nil, err
	}
	return e, nil
}

func (r *Registry) remove(e *Entry) {
	r.mu.Lock()
	delete(r.byID, e.ID)
	if e.Name != "" && r.byName[e.Name] == e {
		delete(r.byName, e.Name)
	}
	r.mu.Unlock()
}

// Get finds a sketch by ID or name.
func (r *Registry) Get(ref string) (*Entry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if e := r.byID[ref]; e != nil {
		return e, nil
	}
	if e := r.byName[ref]; e != nil {
		return e, nil
	}
	return nil, ErrNotFound
}

// List returns all entries sorted by ID.
func (r *Registry) List() []*Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Entry, 0, len(r.byID))
	for _, e := range r.byID {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Delete removes a sketch and its persisted state.
func (r *Registry) Delete(ctx context.Context, ref string) error {
	e, err := r.Get(ref)
	if err != nil {
		return err
	}
	r.remove(e)
	return r.store.Delete(ctx, e.ID)
}

// writeEntry is the log format for replay.
type writeEntry struct {
	BatchID string      `json:"batch_id,omitempty"`
	Keys    []string    `json:"keys,omitempty"`
	Counts  []uint64    `json:"counts,omitempty"`
	Vectors [][]float32 `json:"vectors,omitempty"`
	Values  []float64   `json:"values,omitempty"`
	IDs     []uint64    `json:"ids,omitempty"`
}

// WriteResult reports what a write did.
type WriteResult struct {
	Applied   bool   `json:"applied"`
	Duplicate bool   `json:"duplicate,omitempty"`
	Items     int    `json:"items"`
	BatchID   string `json:"batch_id,omitempty"`
}

// Write prepares a batch on an isolated snapshot.  When durable logging is
// enabled, its record is appended before the prepared state replaces the live
// sketch. A repeated non-empty batch ID is acknowledged without being applied
// again.
func (r *Registry) Write(ctx context.Context, ref string, w writeEntry) (WriteResult, error) {
	e, err := r.Get(ref)
	if err != nil {
		return WriteResult{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.blocked {
		return WriteResult{}, ErrUnavailable
	}
	if w.BatchID != "" && e.batches.seen[w.BatchID] {
		return WriteResult{Duplicate: true, BatchID: w.BatchID}, nil
	}
	prepared, err := cloneSketch(e.sk)
	if err != nil {
		return WriteResult{}, fmt.Errorf("prepare write: %w", err)
	}
	n, err := apply(prepared, w)
	if err != nil {
		return WriteResult{}, err
	}
	if r.logWrites {
		b, err := json.Marshal(w)
		if err != nil {
			return WriteResult{}, fmt.Errorf("encode write log: %w", err)
		}
		if err := r.store.Append(ctx, e.ID, b); err != nil {
			e.blocked = true
			return WriteResult{}, fmt.Errorf("durably append write: %w", err)
		}
	}
	// Assignment and bookkeeping cannot fail while the entry lock is held.
	// A durable record (when configured) therefore always precedes the
	// visible state transition.
	e.sk = prepared
	e.batches.add(w.BatchID)
	e.dirty = true
	if r.logWrites {
		e.logLen++
	}
	return WriteResult{Applied: true, Items: n, BatchID: w.BatchID}, nil
}

// cloneSketch uses the CDNX round trip so this registry-level transaction
// works uniformly for every supported sketch without widening the public
// sketch interface. Decode validates the prepared snapshot as a side effect.
func cloneSketch(sk sketch.Sketch) (sketch.Sketch, error) {
	b, err := sk.MarshalBinary()
	if err != nil {
		return nil, err
	}
	return Decode(b)
}

func apply(sk sketch.Sketch, w writeEntry) (int, error) {
	switch s := sk.(type) {
	case *semantic.Sketch:
		if len(w.Vectors) == 0 || len(w.Keys) > 0 {
			return 0, badRequest("semantic writes take vectors, values and ids")
		}
		vals, ids := w.Values, w.IDs
		if vals == nil {
			vals = make([]float64, len(w.Vectors))
			for i := range vals {
				vals[i] = 1
			}
		}
		if err := s.AddBatch(w.Vectors, vals, ids); err != nil {
			return 0, badRequest("%v", err)
		}
		return len(w.Vectors), nil
	case *classic.CMS:
		if len(w.Counts) != 0 && len(w.Counts) != len(w.Keys) {
			return 0, badRequest("counts must match keys")
		}
		for i, k := range w.Keys {
			n := uint64(1)
			if len(w.Counts) > 0 {
				n = w.Counts[i]
			}
			s.AddKey([]byte(k), n)
		}
		return len(w.Keys), nil
	case sketch.Cardinality:
		for _, k := range w.Keys {
			s.AddKey([]byte(k))
		}
		return len(w.Keys), nil
	case sketch.Membership:
		for _, k := range w.Keys {
			s.AddKey([]byte(k))
		}
		return len(w.Keys), nil
	}
	return 0, ErrWrongKind
}

// lockPair locks two entries in ID order.
func lockPair(a, b *Entry) func() {
	if b.ID < a.ID {
		a, b = b, a
	}
	a.mu.Lock()
	b.mu.Lock()
	return func() { b.mu.Unlock(); a.mu.Unlock() }
}

// Merge folds from into into.
func (r *Registry) Merge(ctx context.Context, ref, fromRef string) (*Entry, error) {
	into, err := r.Get(ref)
	if err != nil {
		return nil, err
	}
	from, err := r.Get(fromRef)
	if err != nil {
		return nil, err
	}
	if into == from {
		return nil, badRequest("cannot merge a sketch into itself")
	}
	unlock := lockPair(into, from)
	defer unlock()
	if into.blocked || from.blocked {
		return nil, ErrUnavailable
	}
	prepared, err := cloneSketch(into.sk)
	if err != nil {
		return nil, fmt.Errorf("prepare merge: %w", err)
	}
	if err := prepared.Merge(from.sk); err != nil {
		if errors.Is(err, sketch.ErrIncompatible) {
			return nil, ErrIncompatible
		}
		return nil, badRequest("%v", err)
	}
	if r.logWrites {
		snap, err := prepared.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("encode merged snapshot: %w", err)
		}
		meta := Meta{Name: into.Name, Kind: uint16(into.Kind), Batches: append([]string(nil), into.batches.order...)}
		trim := into.logLen
		if err := r.store.Save(ctx, into.ID, meta, snap, trim); err != nil {
			into.blocked = true
			return nil, fmt.Errorf("durably save merge: %w", err)
		}
		into.logLen -= trim
		into.dirty = false
	}
	into.sk = prepared
	into.dirty = true
	if r.logWrites {
		into.dirty = false
	}
	return into, nil
}

// With runs f with the entry locked.
func (e *Entry) With(f func(sketch.Sketch) error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.blocked {
		return ErrUnavailable
	}
	return f(e.sk)
}

// Export returns the CDNX bytes.
func (e *Entry) Export() ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.blocked {
		return nil, ErrUnavailable
	}
	return e.sk.MarshalBinary()
}

// flushEntry snapshots one entry and trims the log entries it covers.
func (r *Registry) flushEntry(ctx context.Context, e *Entry) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.blocked {
		return ErrUnavailable
	}
	snap, err := e.sk.MarshalBinary()
	trim := e.logLen
	meta := Meta{Name: e.Name, Kind: uint16(e.Kind), Batches: append([]string(nil), e.batches.order...)}
	if err == nil {
		err = r.store.Save(ctx, e.ID, meta, snap, trim)
	}
	if err != nil {
		// Save may have committed remotely before its response failed.  Do not
		// permit later mutations until recovery reconciles the durable state.
		e.blocked = true
	} else {
		e.logLen -= trim
		e.dirty = false
	}
	return err
}

// Flush snapshots every dirty entry.
func (r *Registry) Flush(ctx context.Context) error {
	var errs []error
	for _, e := range r.List() {
		e.mu.Lock()
		d := e.dirty
		e.mu.Unlock()
		if d {
			if err := r.flushEntry(ctx, e); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", e.ID, err))
			}
		}
	}
	return errors.Join(errs...)
}

// Run flushes every interval until ctx is done, then flushes once more.
func (r *Registry) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			fctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := r.Flush(fctx); err != nil {
				r.log.Error("final flush", "err", err)
			}
			cancel()
			return
		case <-t.C:
			if err := r.Flush(ctx); err != nil {
				r.log.Warn("flush", "err", err)
			}
		}
	}
}

// Load restores snapshots and replays their logs. Corrupt records are logged
// and counted, never silently skipped.
func (r *Registry) Load(ctx context.Context) (loaded, corrupt int, err error) {
	recs, err := r.store.Load(ctx)
	if err != nil {
		return 0, 0, err
	}
	for _, rec := range recs {
		sk, err := Decode(rec.Snapshot)
		if err != nil {
			r.log.Error("corrupt snapshot", "id", rec.ID, "err", err)
			corrupt++
			continue
		}
		e := &Entry{ID: rec.ID, Name: rec.Meta.Name, Kind: sk.Kind(), sk: sk, batches: newBatchSet(rec.Meta.Batches)}
		replayOK := true
		for i, l := range rec.Log {
			var w writeEntry
			if err := json.Unmarshal(l, &w); err != nil {
				r.log.Error("corrupt log entry", "id", rec.ID, "index", i, "err", err)
				corrupt++
				replayOK = false
				break
			}
			if w.BatchID != "" && e.batches.seen[w.BatchID] {
				continue
			}
			if _, err := apply(e.sk, w); err != nil {
				r.log.Error("log replay failed", "id", rec.ID, "index", i, "err", err)
				corrupt++
				replayOK = false
				break
			}
			e.batches.add(w.BatchID)
			e.dirty = true
		}
		if !replayOK {
			r.log.Error("record unavailable after failed recovery", "id", rec.ID)
			continue
		}
		e.logLen = int64(len(rec.Log))
		r.mu.Lock()
		r.byID[e.ID] = e
		if e.Name != "" {
			r.byName[e.Name] = e
		}
		r.mu.Unlock()
		loaded++
	}
	return loaded, corrupt, nil
}
