// Package ai is CodeAudit's AI Security Investigator: it asks a local model
// (Ollama, Gemma by default) to explain a finding or a path from a limited
// context built from the analysis's evidence, and validates every reference
// of the answer against that context. The model never creates, confirms or
// closes a finding; the scanner works without it.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

// Config of the model provider. It comes from flags and the user's
// environment only: never from the analysed repository, whose files are
// not trusted.
type Config struct {
	Endpoint    string        // default http://127.0.0.1:11434
	Model       string        // empty: the smallest installed Gemma model
	Timeout     time.Duration // whole request; default 10 minutes (CPU inference is slow)
	AllowRemote bool          // an endpoint outside this machine needs explicit permission
	MaxTokens   int           // budget of the context sent; default 6000 (estimated)
	NumCtx      int           // context window asked from the model; default 8192
	Temperature float64       // default 0.1
}

// DefaultEndpoint is Ollama's local address.
const DefaultEndpoint = "http://127.0.0.1:11434"

// FromEnv fills unset fields from GITVIEW_OLLAMA_URL (or OLLAMA_HOST) and
// GITVIEW_AI_MODEL.
func (c Config) FromEnv() Config {
	if c.Endpoint == "" {
		c.Endpoint = os.Getenv("GITVIEW_OLLAMA_URL")
	}
	if c.Endpoint == "" {
		if h := os.Getenv("OLLAMA_HOST"); h != "" {
			c.Endpoint = h
		}
	}
	if c.Model == "" {
		c.Model = os.Getenv("GITVIEW_AI_MODEL")
	}
	return c
}

func (c Config) withDefaults() Config {
	if c.Endpoint == "" {
		c.Endpoint = DefaultEndpoint
	}
	if c.Timeout <= 0 {
		c.Timeout = 10 * time.Minute
	}
	if c.MaxTokens <= 0 {
		c.MaxTokens = 6000
	}
	if c.NumCtx <= 0 {
		c.NumCtx = 8192
	}
	if c.Temperature <= 0 {
		c.Temperature = 0.1
	}
	return c
}

// ErrRemote is returned for endpoints outside this machine without
// AllowRemote.
var ErrRemote = errors.New("the model endpoint is not on this machine: pass --ai-allow-remote to send code to it")

// Client talks to Ollama's HTTP API.
type Client struct {
	cfg  Config
	base *url.URL
	http *http.Client
}

// NewClient validates the endpoint. Only loopback addresses are accepted
// unless cfg.AllowRemote; redirects to another host are refused.
func NewClient(cfg Config) (*Client, error) {
	cfg = cfg.withDefaults()
	ep := cfg.Endpoint
	if !strings.Contains(ep, "://") {
		ep = "http://" + ep
	}
	u, err := url.Parse(ep)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid model endpoint %q", cfg.Endpoint)
	}
	if !cfg.AllowRemote && !IsLoopback(u.Hostname()) {
		return nil, ErrRemote
	}
	host := u.Host
	hc := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Host != host {
			return errors.New("redirect to another host refused")
		}
		return nil
	}}
	return &Client{cfg: cfg, base: u, http: hc}, nil
}

// IsLoopback reports whether a host name is this machine.
func IsLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Endpoint is the address the client uses.
func (c *Client) Endpoint() string { return c.base.String() }

// Model is an installed model.
type Model struct {
	Name     string `json:"name"`
	Digest   string `json:"digest"`
	Size     int64  `json:"size"`
	Family   string `json:"family,omitempty"`
	Params   string `json:"parameters,omitempty"`
	Modified string `json:"modified,omitempty"`
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(data)
	}
	u := *c.base
	u.Path = strings.TrimRight(u.Path, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return fmt.Errorf("ollama: %s", e.Error)
		}
		return fmt.Errorf("ollama: %s", resp.Status)
	}
	return json.Unmarshal(data, out)
}

// Models lists the installed models (GET /api/tags). Nothing is
// downloaded.
func (c *Client) Models(ctx context.Context) ([]Model, error) {
	var tags struct {
		Models []struct {
			Name       string `json:"name"`
			Model      string `json:"model"`
			Digest     string `json:"digest"`
			Size       int64  `json:"size"`
			ModifiedAt string `json:"modified_at"`
			Details    struct {
				Family        string `json:"family"`
				ParameterSize string `json:"parameter_size"`
			} `json:"details"`
		} `json:"models"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/tags", nil, &tags); err != nil {
		return nil, err
	}
	var out []Model
	for _, m := range tags.Models {
		name := m.Name
		if name == "" {
			name = m.Model
		}
		out = append(out, Model{Name: name, Digest: m.Digest, Size: m.Size, Family: m.Details.Family, Params: m.Details.ParameterSize, Modified: m.ModifiedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ResolveModel picks the configured model, which must be installed, or
// the smallest installed Gemma model: it is the one most likely to fit
// the machine's memory and answer in reasonable time (a larger one is
// chosen with --model or GITVIEW_AI_MODEL).
func (c *Client) ResolveModel(ctx context.Context, requested string) (Model, error) {
	models, err := c.Models(ctx)
	if err != nil {
		return Model{}, err
	}
	if requested == "" {
		requested = c.cfg.Model
	}
	if requested != "" {
		for _, m := range models {
			if m.Name == requested || strings.TrimSuffix(m.Name, ":latest") == requested {
				return m, nil
			}
		}
		return Model{}, fmt.Errorf("model %q is not installed (GitView does not download models: run ollama pull %s)", requested, requested)
	}
	var gemma []Model
	for _, m := range models {
		if strings.HasPrefix(strings.ToLower(m.Name), "gemma") || strings.EqualFold(m.Family, "gemma3") || strings.EqualFold(m.Family, "gemma") {
			gemma = append(gemma, m)
		}
	}
	if len(gemma) == 0 {
		return Model{}, errors.New("no Gemma model is installed (run ollama pull gemma3, or choose another model with --model)")
	}
	sort.SliceStable(gemma, func(i, j int) bool { return gemma[i].Size < gemma[j].Size })
	return gemma[0], nil
}

// Status is what the TUI and the page show about the provider.
type Status struct {
	Endpoint  string  `json:"endpoint"`
	Reachable bool    `json:"reachable"`
	Models    []Model `json:"models,omitempty"`
	Selected  string  `json:"selected,omitempty"`
	Error     string  `json:"error,omitempty"`
}

// Probe checks whether the provider answers and which model would be used.
func (c *Client) Probe(ctx context.Context) Status {
	st := Status{Endpoint: c.Endpoint()}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	models, err := c.Models(ctx)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.Reachable, st.Models = true, models
	if m, err := c.ResolveModel(ctx, ""); err == nil {
		st.Selected = m.Name
	} else {
		st.Error = err.Error()
	}
	return st
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chat sends one non-streamed chat request with a JSON schema as format.
func (c *Client) chat(ctx context.Context, model string, msgs []message, schema any) (string, error) {
	req := map[string]any{
		"model":    model,
		"messages": msgs,
		"stream":   false,
		"format":   schema,
		"options":  map[string]any{"temperature": c.cfg.Temperature, "num_ctx": c.cfg.NumCtx},
	}
	var resp struct {
		Message message `json:"message"`
		Done    bool    `json:"done"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/chat", req, &resp); err != nil {
		return "", err
	}
	return resp.Message.Content, nil
}
