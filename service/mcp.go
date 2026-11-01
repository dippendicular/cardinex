package service

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// MCP serves the Model Context Protocol over newline-delimited JSON-RPC 2.0
// (stdio transport). Tools: create, add, merge, topk, query, diff,
// exemplars, stats. Every numeric answer carries its interval and, for
// semantic sketches, ambiguous mass; tolerance turns wide intervals into an
// explicit insufficient_confidence status. Writes are idempotent by batch_id.
type MCP struct {
	reg     *Registry
	version string
	mu      sync.Mutex // serialises writes to out
}

func NewMCP(reg *Registry, version string) *MCP { return &MCP{reg: reg, version: version} }

const mcpProtocol = "2025-06-18"

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

// Serve reads requests from in and writes responses to out until EOF or ctx ends.
func (m *MCP) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), MaxBody)
	enc := json.NewEncoder(out)
	for sc.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcReq
		if err := json.Unmarshal(line, &req); err != nil {
			m.send(enc, rpcResp{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcErr{-32700, "parse error"}})
			continue
		}
		if len(req.ID) == 0 {
			continue // notification
		}
		res, rerr := m.handle(ctx, req)
		m.send(enc, rpcResp{JSONRPC: "2.0", ID: req.ID, Result: res, Error: rerr})
	}
	return sc.Err()
}

func (m *MCP) send(enc *json.Encoder, r rpcResp) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_ = enc.Encode(r)
}

func (m *MCP) handle(ctx context.Context, req rpcReq) (any, *rpcErr) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": mcpProtocol,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "cardinex", "version": m.version},
			"instructions": "Cardinex hosts mergeable sketches. Semantic sketches answer per-entity count, sum and mean " +
				"over embeddings with guaranteed error intervals. Act on an answer only when its status is ok; " +
				"on insufficient_confidence, fetch exemplars or exact data instead.",
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": mcpTools}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcErr{-32602, "invalid params"}
		}
		out, err := m.call(ctx, p.Name, p.Arguments)
		if err != nil {
			var e *Error
			if !errors.As(err, &e) {
				e = &Error{Code: "internal", Msg: err.Error()}
			}
			b, _ := json.Marshal(map[string]*Error{"error": e})
			return map[string]any{"isError": true, "content": []map[string]string{{"type": "text", "text": string(b)}}}, nil
		}
		b, _ := json.Marshal(out)
		return map[string]any{"content": []map[string]string{{"type": "text", "text": string(b)}}, "structuredContent": out}, nil
	}
	return nil, &rpcErr{-32601, "method not found: " + req.Method}
}

func args[T any](raw json.RawMessage) (T, error) {
	var v T
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, badRequest("arguments: %v", err)
	}
	return v, nil
}

func (m *MCP) call(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	switch name {
	case "create":
		a, err := args[struct {
			Kind   string          `json:"kind"`
			Name   string          `json:"name"`
			Params json.RawMessage `json:"params"`
		}](raw)
		if err != nil {
			return nil, err
		}
		e, err := m.reg.Create(ctx, a.Kind, a.Name, a.Params)
		if err != nil {
			return nil, err
		}
		return Describe(e), nil
	case "add":
		a, err := args[struct {
			Sketch string `json:"sketch"`
			writeEntry
		}](raw)
		if err != nil {
			return nil, err
		}
		return m.reg.Write(ctx, a.Sketch, a.writeEntry)
	case "merge":
		a, err := args[struct{ Into, From string }](raw)
		if err != nil {
			return nil, err
		}
		e, err := m.reg.Merge(a.Into, a.From)
		if err != nil {
			return nil, err
		}
		return Describe(e), nil
	case "topk":
		a, err := args[struct {
			Sketch    string  `json:"sketch"`
			K         int     `json:"k"`
			By        string  `json:"by"`
			Tolerance float64 `json:"tolerance"`
		}](raw)
		if err != nil {
			return nil, err
		}
		e, err := m.reg.Get(a.Sketch)
		if err != nil {
			return nil, err
		}
		if a.K <= 0 {
			a.K = 10
		}
		return TopK(e, a.K, a.By, a.Tolerance)
	case "query":
		a, err := args[struct {
			Sketch    string    `json:"sketch"`
			Key       string    `json:"key"`
			Vector    []float32 `json:"vector"`
			Metric    string    `json:"metric"`
			Tolerance float64   `json:"tolerance"`
		}](raw)
		if err != nil {
			return nil, err
		}
		e, err := m.reg.Get(a.Sketch)
		if err != nil {
			return nil, err
		}
		if len(a.Vector) > 0 {
			return Lookup(e, a.Vector, a.Metric, a.Tolerance)
		}
		return QueryKey(e, a.Key, a.Tolerance)
	case "diff":
		a, err := args[struct {
			Sketch    string  `json:"sketch"`
			Other     string  `json:"other"`
			Tolerance float64 `json:"tolerance"`
		}](raw)
		if err != nil {
			return nil, err
		}
		x, err := m.reg.Get(a.Sketch)
		if err != nil {
			return nil, err
		}
		y, err := m.reg.Get(a.Other)
		if err != nil {
			return nil, err
		}
		d, err := Diff(x, y, a.Tolerance)
		if err != nil {
			return nil, err
		}
		return map[string]any{"deltas": d}, nil
	case "exemplars":
		a, err := args[struct {
			Sketch string `json:"sketch"`
			Entity uint64 `json:"entity"`
		}](raw)
		if err != nil {
			return nil, err
		}
		e, err := m.reg.Get(a.Sketch)
		if err != nil {
			return nil, err
		}
		ex, err := Exemplars(e, a.Entity)
		if err != nil {
			return nil, err
		}
		return map[string]any{"entity": a.Entity, "exemplars": ex}, nil
	case "stats":
		a, err := args[struct {
			Sketch string `json:"sketch"`
		}](raw)
		if err != nil {
			return nil, err
		}
		if a.Sketch == "" {
			out := []Info{}
			for _, e := range m.reg.List() {
				out = append(out, Describe(e))
			}
			return map[string]any{"sketches": out}, nil
		}
		e, err := m.reg.Get(a.Sketch)
		if err != nil {
			return nil, err
		}
		return Describe(e), nil
	}
	return nil, badRequest("unknown tool %q", name)
}

func schema(props string, required ...string) json.RawMessage {
	req, _ := json.Marshal(required)
	return json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{%s},"required":%s}`, props, req))
}

const (
	pSketch    = `"sketch":{"type":"string","description":"sketch ID or name"}`
	pTolerance = `"tolerance":{"type":"number","description":"max acceptable interval width; wider answers return status insufficient_confidence"}`
)

var mcpTools = []map[string]any{
	{"name": "create", "description": "Create a sketch. kind: semantic (params: dim, tau, seed required; capacity, band, bits, value_min, value_max, exemplars, probation optional), hll, cms or bloom.",
		"inputSchema": schema(`"kind":{"type":"string","enum":["semantic","hll","cms","bloom"]},"name":{"type":"string"},"params":{"type":"object"}`, "kind")},
	{"name": "add", "description": "Add a batch. Semantic: vectors, values, ids. Classic: keys (and counts for cms). Retrying with the same batch_id is a no-op.",
		"inputSchema": schema(pSketch+`,"batch_id":{"type":"string"},"vectors":{"type":"array","items":{"type":"array","items":{"type":"number"}}},"values":{"type":"array","items":{"type":"number"}},"ids":{"type":"array","items":{"type":"integer"}},"keys":{"type":"array","items":{"type":"string"}},"counts":{"type":"array","items":{"type":"integer"}}`, "sketch")},
	{"name": "merge", "description": "Merge sketch 'from' into sketch 'into'. Parameters must match.",
		"inputSchema": schema(`"into":{"type":"string"},"from":{"type":"string"}`, "into", "from")},
	{"name": "topk", "description": "Top-k entities of a semantic sketch by count, sum or mean, each with guaranteed intervals, ambiguous mass and exemplar IDs.",
		"inputSchema": schema(pSketch+`,"k":{"type":"integer"},"by":{"type":"string","enum":["count","sum","mean"]},`+pTolerance, "sketch")},
	{"name": "query", "description": "Semantic: the entity a vector belongs to (vector, metric). Classic: hll cardinality, cms count of key, bloom membership of key.",
		"inputSchema": schema(pSketch+`,"vector":{"type":"array","items":{"type":"number"}},"metric":{"type":"string","enum":["count","sum","mean"]},"key":{"type":"string"},`+pTolerance, "sketch")},
	{"name": "diff", "description": "Per-entity change between two semantic sketches (sketch minus other), with intervals and a significance flag.",
		"inputSchema": schema(pSketch+`,"other":{"type":"string"},`+pTolerance, "sketch", "other")},
	{"name": "exemplars", "description": "Exemplar item IDs of one entity, to fetch real examples before acting.",
		"inputSchema": schema(pSketch+`,"entity":{"type":"integer"}`, "sketch", "entity")},
	{"name": "stats", "description": "Sketch statistics; omit sketch to list all sketches.",
		"inputSchema": schema(pSketch)},
}
