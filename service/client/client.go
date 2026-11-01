// Package client is a Go client for the Cardinex HTTP API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Client talks to one Cardinex server.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

func New(baseURL string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: http.DefaultClient}
}

// APIError is the server's error envelope.
type APIError struct {
	Status  int
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("cardinex: %d %s: %s", e.Status, e.Code, e.Message)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	ctype := "application/json"
	switch b := body.(type) {
	case nil:
	case []byte:
		rd, ctype = bytes.NewReader(b), "application/octet-stream"
	default:
		buf, err := json.Marshal(b)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", ctype)
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var env struct {
			Error APIError `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&env)
		env.Error.Status = resp.StatusCode
		return &env.Error
	}
	switch o := out.(type) {
	case nil:
		return nil
	case *[]byte:
		*o, err = io.ReadAll(resp.Body)
		return err
	default:
		return json.NewDecoder(resp.Body).Decode(out)
	}
}

// Create makes a sketch: kind is semantic, hll, cms or bloom.
func (c *Client) Create(ctx context.Context, kind, name string, params any) (map[string]any, error) {
	var out map[string]any
	path := "/sketches/" + url.PathEscape(kind)
	if kind == "semantic" {
		path = "/semantic"
	}
	err := c.do(ctx, http.MethodPost, path, map[string]any{"name": name, "params": params}, &out)
	return out, err
}

// AddKeys adds keys to a classic sketch; counts may be nil.
func (c *Client) AddKeys(ctx context.Context, id, batchID string, keys []string, counts []uint64) (map[string]any, error) {
	var out map[string]any
	err := c.do(ctx, http.MethodPost, "/sketches/"+url.PathEscape(id)+"/add",
		map[string]any{"batch_id": batchID, "keys": keys, "counts": counts}, &out)
	return out, err
}

// AddBatch adds vectors to a semantic sketch. Retrying a batchID is a no-op.
func (c *Client) AddBatch(ctx context.Context, id, batchID string, vectors [][]float32, values []float64, ids []uint64) (map[string]any, error) {
	var out map[string]any
	err := c.do(ctx, http.MethodPost, "/semantic/"+url.PathEscape(id)+"/add-batch",
		map[string]any{"batch_id": batchID, "vectors": vectors, "values": values, "ids": ids}, &out)
	return out, err
}

// Query answers a classic query (key is ignored for HLL).
func (c *Client) Query(ctx context.Context, id, key string, tolerance float64) (map[string]any, error) {
	var out map[string]any
	q := url.Values{"key": {key}}
	if tolerance > 0 {
		q.Set("tolerance", strconv.FormatFloat(tolerance, 'g', -1, 64))
	}
	err := c.do(ctx, http.MethodGet, "/sketches/"+url.PathEscape(id)+"/query?"+q.Encode(), nil, &out)
	return out, err
}

// TopK ranks semantic entities by count, sum or mean.
func (c *Client) TopK(ctx context.Context, id string, k int, by string, tolerance float64) (map[string]any, error) {
	var out map[string]any
	q := url.Values{"k": {strconv.Itoa(k)}, "by": {by}, "tolerance": {strconv.FormatFloat(tolerance, 'g', -1, 64)}}
	err := c.do(ctx, http.MethodGet, "/semantic/"+url.PathEscape(id)+"/topk?"+q.Encode(), nil, &out)
	return out, err
}

// Lookup returns the entity a vector belongs to.
func (c *Client) Lookup(ctx context.Context, id string, vector []float32, metric string, tolerance float64) (map[string]any, error) {
	var out map[string]any
	err := c.do(ctx, http.MethodPost, "/semantic/"+url.PathEscape(id)+"/lookup",
		map[string]any{"vector": vector, "metric": metric, "tolerance": tolerance}, &out)
	return out, err
}

// Diff returns id − other per entity.
func (c *Client) Diff(ctx context.Context, id, other string, tolerance float64) (map[string]any, error) {
	var out map[string]any
	err := c.do(ctx, http.MethodPost, "/semantic/"+url.PathEscape(id)+"/diff",
		map[string]any{"other": other, "tolerance": tolerance}, &out)
	return out, err
}

// Merge folds from into into.
func (c *Client) Merge(ctx context.Context, into, from string) (map[string]any, error) {
	var out map[string]any
	err := c.do(ctx, http.MethodPost, "/sketches/merge", map[string]any{"into": into, "from": from}, &out)
	return out, err
}

// Stats describes a sketch.
func (c *Client) Stats(ctx context.Context, id string) (map[string]any, error) {
	var out map[string]any
	err := c.do(ctx, http.MethodGet, "/sketches/"+url.PathEscape(id)+"/stats", nil, &out)
	return out, err
}

// Export returns the sketch's CDNX bytes.
func (c *Client) Export(ctx context.Context, id string) ([]byte, error) {
	var out []byte
	err := c.do(ctx, http.MethodGet, "/sketches/"+url.PathEscape(id)+"/export", nil, &out)
	return out, err
}

// Import uploads CDNX bytes as a new sketch.
func (c *Client) Import(ctx context.Context, name string, b []byte) (map[string]any, error) {
	var out map[string]any
	err := c.do(ctx, http.MethodPost, "/sketches/import?name="+url.QueryEscape(name), b, &out)
	return out, err
}

// Delete removes a sketch.
func (c *Client) Delete(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/sketches/"+url.PathEscape(id), nil, nil)
}
