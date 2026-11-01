package service

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// MaxBody bounds request bodies (vector batches are large).
const MaxBody = 64 << 20

// Server is the HTTP API.
type Server struct {
	reg    *Registry
	apiKey string
	log    *slog.Logger

	requests, failures atomic.Uint64
}

// NewServer wraps reg. A non-empty apiKey requires "Authorization: Bearer <key>".
func NewServer(reg *Registry, apiKey string, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{reg: reg, apiKey: apiKey, log: log}
}

// Routes returns the router. Classic routes keep their original paths.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer, s.count)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.Get("/metrics", s.metrics)

	r.Group(func(r chi.Router) {
		r.Use(s.auth, func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				req.Body = http.MaxBytesReader(w, req.Body, MaxBody)
				next.ServeHTTP(w, req)
			})
		})
		// Shared
		r.Get("/sketches", s.list)
		r.Post("/sketches/import", s.importSketch)
		r.Post("/sketches/merge", s.merge)
		r.Get("/sketches/{id}/stats", s.stats)
		r.Get("/sketches/{id}/export", s.export)
		r.Delete("/sketches/{id}", s.delete)
		// Classic
		r.Post("/sketches/{type}", s.create)
		r.Post("/sketches/{id}/add", s.add)
		r.Get("/sketches/{id}/query", s.query)
		// Semantic
		r.Post("/semantic", s.createSemantic)
		r.Post("/semantic/merge", s.merge)
		r.Post("/semantic/{id}/add-batch", s.add)
		r.Get("/semantic/{id}/topk", s.topk)
		r.Post("/semantic/{id}/lookup", s.lookup)
		r.Post("/semantic/{id}/diff", s.diff)
		r.Get("/semantic/{id}/exemplars/{entity}", s.exemplars)
		r.Get("/semantic/{id}/stats", s.stats)
	})
	return r
}

func (s *Server) count(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.apiKey != "" {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if subtle.ConstantTimeCompare([]byte(got), []byte(s.apiKey)) != 1 {
				s.fail(w, r, http.StatusUnauthorized, &Error{Code: "unauthorized", Msg: "missing or bad API key"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) metrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# TYPE cardinex_requests_total counter\ncardinex_requests_total %d\n", s.requests.Load())
	fmt.Fprintf(w, "# TYPE cardinex_request_failures_total counter\ncardinex_request_failures_total %d\n", s.failures.Load())
	fmt.Fprintf(w, "# TYPE cardinex_sketches gauge\ncardinex_sketches %d\n", len(s.reg.List()))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, code int, err error) {
	s.failures.Add(1)
	var e *Error
	if !errors.As(err, &e) {
		e = &Error{Code: "internal", Msg: err.Error()}
	}
	if code >= 500 {
		s.log.Error("request failed", "path", r.URL.Path, "req", middleware.GetReqID(r.Context()), "err", err)
	}
	writeJSON(w, code, map[string]*Error{"error": e})
}

func (s *Server) err(w http.ResponseWriter, r *http.Request, err error) {
	var e *Error
	var mb *http.MaxBytesError
	switch {
	case errors.As(err, &mb):
		s.fail(w, r, http.StatusRequestEntityTooLarge, &Error{Code: "too_large", Msg: err.Error()})
	case errors.As(err, &e):
		code := map[string]int{"not_found": 404, "name_taken": 409, "incompatible": 409, "wrong_kind": 400, "bad_request": 400}[e.Code]
		if code == 0 {
			code = 400
		}
		s.fail(w, r, code, e)
	default:
		s.fail(w, r, http.StatusInternalServerError, err)
	}
}

func decode(r *http.Request, v any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		var mb *http.MaxBytesError
		if errors.As(err, &mb) {
			return err
		}
		return badRequest("body: %v", err)
	}
	return nil
}

func floatParam(r *http.Request, name string) (float64, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return 0, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 {
		return 0, badRequest("%s must be a non-negative number", name)
	}
	return f, nil
}

func (s *Server) list(w http.ResponseWriter, _ *http.Request) {
	out := []Info{}
	for _, e := range s.reg.List() {
		out = append(out, Describe(e))
	}
	writeJSON(w, http.StatusOK, out)
}

type createBody struct {
	Name   string          `json:"name"`
	Params json.RawMessage `json:"params"`
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var b createBody
	if r.ContentLength != 0 {
		if err := decode(r, &b); err != nil {
			s.err(w, r, err)
			return
		}
	}
	e, err := s.reg.Create(r.Context(), chi.URLParam(r, "type"), b.Name, b.Params)
	if err != nil {
		s.err(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, Describe(e))
}

func (s *Server) createSemantic(w http.ResponseWriter, r *http.Request) {
	var b createBody
	if err := decode(r, &b); err != nil {
		s.err(w, r, err)
		return
	}
	e, err := s.reg.Create(r.Context(), "semantic", b.Name, b.Params)
	if err != nil {
		s.err(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, Describe(e))
}

// add accepts {"batch_id","keys","counts"} for classic sketches,
// {"item": "..."} for the original single-key form, and
// {"batch_id","vectors","values","ids"} for semantic sketches.
func (s *Server) add(w http.ResponseWriter, r *http.Request) {
	var b struct {
		writeEntry
		Item string `json:"item"`
	}
	if err := decode(r, &b); err != nil {
		s.err(w, r, err)
		return
	}
	if b.Item != "" {
		b.Keys = append(b.Keys, b.Item)
	}
	res, err := s.reg.Write(r.Context(), chi.URLParam(r, "id"), b.writeEntry)
	if err != nil {
		s.err(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) query(w http.ResponseWriter, r *http.Request) {
	e, err := s.reg.Get(chi.URLParam(r, "id"))
	if err != nil {
		s.err(w, r, err)
		return
	}
	tol, err := floatParam(r, "tolerance")
	if err != nil {
		s.err(w, r, err)
		return
	}
	key := r.URL.Query().Get("key")
	if key == "" {
		key = r.URL.Query().Get("item")
	}
	ans, err := QueryKey(e, key, tol)
	if err != nil {
		s.err(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ans)
}

func (s *Server) merge(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Into string `json:"into"`
		From string `json:"from"`
	}
	if err := decode(r, &b); err != nil {
		s.err(w, r, err)
		return
	}
	e, err := s.reg.Merge(b.Into, b.From)
	if err != nil {
		s.err(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, Describe(e))
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	e, err := s.reg.Get(chi.URLParam(r, "id"))
	if err != nil {
		s.err(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, Describe(e))
}

func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	e, err := s.reg.Get(chi.URLParam(r, "id"))
	if err != nil {
		s.err(w, r, err)
		return
	}
	b, err := e.Export()
	if err != nil {
		s.err(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(b)
}

func (s *Server) importSketch(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		s.err(w, r, err)
		return
	}
	e, err := s.reg.Import(r.Context(), r.URL.Query().Get("name"), b)
	if err != nil {
		s.err(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, Describe(e))
}

func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	if err := s.reg.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.err(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) topk(w http.ResponseWriter, r *http.Request) {
	e, err := s.reg.Get(chi.URLParam(r, "id"))
	if err != nil {
		s.err(w, r, err)
		return
	}
	tol, err := floatParam(r, "tolerance")
	if err != nil {
		s.err(w, r, err)
		return
	}
	k, _ := strconv.Atoi(r.URL.Query().Get("k"))
	if k <= 0 {
		k = 10
	}
	res, err := TopK(e, k, r.URL.Query().Get("by"), tol)
	if err != nil {
		s.err(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) lookup(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Vector    []float32 `json:"vector"`
		Metric    string    `json:"metric"`
		Tolerance float64   `json:"tolerance"`
	}
	if err := decode(r, &b); err != nil {
		s.err(w, r, err)
		return
	}
	e, err := s.reg.Get(chi.URLParam(r, "id"))
	if err != nil {
		s.err(w, r, err)
		return
	}
	res, err := Lookup(e, b.Vector, b.Metric, b.Tolerance)
	if err != nil {
		s.err(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) diff(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Other     string  `json:"other"`
		Tolerance float64 `json:"tolerance"`
	}
	if err := decode(r, &b); err != nil {
		s.err(w, r, err)
		return
	}
	a, err := s.reg.Get(chi.URLParam(r, "id"))
	if err != nil {
		s.err(w, r, err)
		return
	}
	o, err := s.reg.Get(b.Other)
	if err != nil {
		s.err(w, r, err)
		return
	}
	res, err := Diff(a, o, b.Tolerance)
	if err != nil {
		s.err(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deltas": res})
}

func (s *Server) exemplars(w http.ResponseWriter, r *http.Request) {
	e, err := s.reg.Get(chi.URLParam(r, "id"))
	if err != nil {
		s.err(w, r, err)
		return
	}
	id, err := strconv.ParseUint(chi.URLParam(r, "entity"), 10, 64)
	if err != nil {
		s.err(w, r, badRequest("entity must be an unsigned integer"))
		return
	}
	ex, err := Exemplars(e, id)
	if err != nil {
		s.err(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entity": id, "exemplars": ex})
}

// ListenAndServe runs the HTTP server until ctx is done, then shuts down
// gracefully.
func ListenAndServe(ctx context.Context, addr string, h http.Handler) error {
	srv := &http.Server{
		Addr: addr, Handler: h,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second,
		WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return srv.Shutdown(sctx)
	}
}
