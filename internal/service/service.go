package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/dippendicular/cardinex/pkg/bloom"
	"github.com/dippendicular/cardinex/pkg/cms"
	"github.com/dippendicular/cardinex/pkg/hll"
	"github.com/dippendicular/cardinex/pkg/sketch"
	"github.com/dippendicular/cardinex/pkg/tuner"
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

type Server struct {
	rdb      *redis.Client
	mu       sync.RWMutex
	sketches map[string]sketch.Sketch
}

type CreateRequest struct {
	Precision        uint8   `json:"precision"`
	Width            uint32  `json:"width"`
	Depth            uint32  `json:"depth"`
	ExpectedElements int     `json:"expected_elements"`
	FalsePositive    float64 `json:"false_positive_rate"`
	MemoryBytes      int     `json:"memory_bytes"`
	ErrorRate        float64 `json:"error_rate"`
}

func New(redisAddr string) (*Server, error) {
	s := &Server{sketches: map[string]sketch.Sketch{}}
	if redisAddr != "" {
		s.rdb = redis.NewClient(&redis.Options{Addr: redisAddr})
		if err := s.load(context.Background()); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Post("/sketches/{type}", s.create)
	r.Post("/sketches/{id}/add", s.add)
	r.Get("/sketches/{id}/query", s.query)
	r.Post("/sketches/merge", s.merge)
	r.Get("/sketches/{id}/stats", s.stats)
	r.Delete("/sketches/{id}", s.delete)
	return r
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	typ := strings.ToLower(chi.URLParam(r, "type"))
	if req.MemoryBytes > 0 && req.ErrorRate > 0 && req.ExpectedElements > 0 {
		tuned, err := tuner.Tune(req.MemoryBytes, req.ErrorRate, req.ExpectedElements)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		switch typ {
		case "hll", "hyperloglog":
			req.Precision = tuned.HyperLogLog.Precision
		case "cms", "countmin":
			req.Width, req.Depth = tuned.CountMin.Width, tuned.CountMin.Depth
		case "bloom":
			req.ExpectedElements, req.FalsePositive = req.ExpectedElements, tuned.Bloom.ErrorRate
		}
	}
	sk, err := build(typ, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	id := newID()
	s.mu.Lock()
	s.sketches[id] = sk
	s.mu.Unlock()
	if err := s.persist(r.Context(), id, sk); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "type": typ, "stats": statsFor(sk)})
}

func (s *Server) add(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Item string `json:"item"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Item == "" {
		writeError(w, http.StatusBadRequest, errors.New("item is required"))
		return
	}
	id := chi.URLParam(r, "id")
	s.mu.Lock()
	sk := s.sketches[id]
	if sk != nil {
		sk.Add([]byte(req.Item))
	}
	s.mu.Unlock()
	if sk == nil {
		writeError(w, http.StatusNotFound, errors.New("sketch not found"))
		return
	}
	if err := s.persist(r.Context(), id, sk); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) query(w http.ResponseWriter, r *http.Request) {
	id, item := chi.URLParam(r, "id"), r.URL.Query().Get("item")
	s.mu.RLock()
	sk := s.sketches[id]
	s.mu.RUnlock()
	if sk == nil {
		writeError(w, http.StatusNotFound, errors.New("sketch not found"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": sk.Query([]byte(item))})
}

func (s *Server) merge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Into string `json:"into"`
		From string `json:"from"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.mu.Lock()
	into, from := s.sketches[req.Into], s.sketches[req.From]
	if into != nil && from != nil {
		err := into.Merge(from)
		if err != nil {
			s.mu.Unlock()
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	s.mu.Unlock()
	if into == nil || from == nil {
		writeError(w, http.StatusNotFound, errors.New("sketch not found"))
		return
	}
	if err := s.persist(r.Context(), req.Into, into); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": req.Into, "stats": statsFor(into)})
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s.mu.RLock()
	sk := s.sketches[id]
	s.mu.RUnlock()
	if sk == nil {
		writeError(w, http.StatusNotFound, errors.New("sketch not found"))
		return
	}
	writeJSON(w, http.StatusOK, statsFor(sk))
}

func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s.mu.Lock()
	delete(s.sketches, id)
	s.mu.Unlock()
	if s.rdb != nil {
		_ = s.rdb.Del(r.Context(), "sketch:"+id).Err()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) persist(ctx context.Context, id string, sk sketch.Sketch) error {
	if s.rdb == nil {
		return nil
	}
	return s.rdb.Set(ctx, "sketch:"+id, sk.Serialize(), 0).Err()
}

func (s *Server) load(ctx context.Context) error {
	if s.rdb == nil {
		return nil
	}
	keys, err := s.rdb.Keys(ctx, "sketch:*").Result()
	if err != nil {
		return err
	}
	for _, k := range keys {
		b, err := s.rdb.Get(ctx, k).Bytes()
		if err != nil {
			continue
		}
		sk, err := deserializeAny(b)
		if err == nil {
			s.sketches[strings.TrimPrefix(k, "sketch:")] = sk
		}
	}
	return nil
}

func build(typ string, req CreateRequest) (sketch.Sketch, error) {
	switch typ {
	case "hll", "hyperloglog":
		if req.Precision == 0 {
			req.Precision = 14
		}
		return hll.New(req.Precision)
	case "cms", "countmin":
		if req.Width == 0 {
			req.Width = 2720
		}
		if req.Depth == 0 {
			req.Depth = 5
		}
		return cms.New(req.Width, req.Depth)
	case "bloom":
		if req.ExpectedElements == 0 {
			req.ExpectedElements = 100000
		}
		if req.FalsePositive == 0 {
			req.FalsePositive = 0.01
		}
		return bloom.New(req.ExpectedElements, req.FalsePositive)
	default:
		return nil, errors.New("unknown sketch type")
	}
}

func deserializeAny(b []byte) (sketch.Sketch, error) {
	if len(b) < 4 {
		return nil, errors.New("payload too small")
	}
	switch string(b[:4]) {
	case "PDH1":
		var h hll.HyperLogLog
		return &h, h.Deserialize(b)
	case "PDC1":
		var c cms.CountMinSketch
		return &c, c.Deserialize(b)
	case "PDB1":
		var bf bloom.BloomFilter
		return &bf, bf.Deserialize(b)
	default:
		return nil, errors.New("unknown sketch payload")
	}
}

func statsFor(sk sketch.Sketch) map[string]any {
	switch v := sk.(type) {
	case *hll.HyperLogLog:
		est := v.Count()
		err := v.ErrorRate()
		return map[string]any{"type": "hll", "memory_bytes": v.MemoryBytes(), "element_count_estimate": est, "error_bound": err, "confidence_interval": []uint64{uint64(float64(est) * (1 - err)), uint64(float64(est) * (1 + err))}}
	case *cms.CountMinSketch:
		return map[string]any{"type": "cms", "memory_bytes": v.MemoryBytes(), "element_count_estimate": v.Total(), "error_bound": v.Epsilon(), "confidence": v.Confidence(), "confidence_interval": []uint64{v.Total(), v.Total() + uint64(v.Epsilon()*float64(v.Total()))}}
	case *bloom.BloomFilter:
		return map[string]any{"type": "bloom", "memory_bytes": v.MemoryBytes(), "element_count_estimate": v.Inserts(), "error_bound": v.FalsePositiveRate(), "confidence": 1 - v.FalsePositiveRate(), "confidence_interval": []uint64{v.Inserts(), v.Inserts()}}
	default:
		return map[string]any{}
	}
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
