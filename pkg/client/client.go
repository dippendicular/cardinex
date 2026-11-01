package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func New(baseURL string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: http.DefaultClient}
}

func (c *Client) Create(typ string, params map[string]any) (map[string]any, error) {
	return c.do(http.MethodPost, "/sketches/"+url.PathEscape(typ), params)
}

func (c *Client) Add(id, item string) error {
	_, err := c.do(http.MethodPost, "/sketches/"+url.PathEscape(id)+"/add", map[string]any{"item": item})
	return err
}

func (c *Client) Query(id, item string) (map[string]any, error) {
	return c.do(http.MethodGet, "/sketches/"+url.PathEscape(id)+"/query?item="+url.QueryEscape(item), nil)
}

func (c *Client) Merge(into, from string) (map[string]any, error) {
	return c.do(http.MethodPost, "/sketches/merge", map[string]any{"into": into, "from": from})
}

func (c *Client) Stats(id string) (map[string]any, error) {
	return c.do(http.MethodGet, "/sketches/"+url.PathEscape(id)+"/stats", nil)
}

func (c *Client) Delete(id string) error {
	_, err := c.do(http.MethodDelete, "/sketches/"+url.PathEscape(id), nil)
	return err
}

func (c *Client) do(method, path string, body any) (map[string]any, error) {
	var rbody *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rbody = bytes.NewReader(b)
	} else {
		rbody = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, rbody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%v", out["error"])
	}
	return out, nil
}
