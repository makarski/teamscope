// Package anthropic provides a minimal client for Claude, shared by the
// classify and align stages. It talks either to the Anthropic Messages API
// directly or to Amazon Bedrock, depending on which is configured.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/makarski/teamscope/config"
)

const (
	defaultBaseURL = "https://api.anthropic.com"
	apiVersion     = "2023-06-01"
)

// backend performs a single completion call against a specific transport
// (raw HTTPS to Anthropic, or Bedrock's InvokeModel).
type backend interface {
	complete(ctx context.Context, prompt string, maxTokens int) (string, error)
}

// Client is a minimal Claude client. Which backend it uses is decided once,
// at construction time, by New.
type Client struct {
	backend backend
}

// New builds a client from config. Bedrock takes priority when configured;
// otherwise it falls back to the Anthropic API. Returns nil if neither is
// configured (or Bedrock fails to initialize), letting callers cleanly skip
// the AI stage.
func New(cfg *config.Anthropic, bedrockCfg *config.Bedrock) *Client {
	if bedrockCfg != nil && bedrockCfg.Model != "" {
		b, err := newBedrockBackend(bedrockCfg)
		if err != nil {
			slog.Warn("anthropic: bedrock init failed, disabling AI stage", "err", err)
			return nil
		}
		return &Client{backend: b}
	}

	if cfg == nil || cfg.Token == "" {
		return nil
	}
	return &Client{backend: newHTTPBackend(cfg)}
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type response struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
}

// Complete sends a single user prompt and returns the model's text reply,
// trimmed of surrounding whitespace.
func (c *Client) Complete(ctx context.Context, prompt string, maxTokens int) (string, error) {
	return c.backend.complete(ctx, prompt, maxTokens)
}

// httpBackend calls the Anthropic Messages API directly over HTTPS.
type httpBackend struct {
	httpClient *http.Client
	baseURL    string
	token      string
	model      string
}

func newHTTPBackend(cfg *config.Anthropic) *httpBackend {
	base := cfg.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	return &httpBackend{
		httpClient: http.DefaultClient,
		baseURL:    strings.TrimRight(base, "/"),
		token:      cfg.Token,
		model:      cfg.Model,
	}
}

type request struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	Messages  []message `json:"messages"`
}

func (b *httpBackend) complete(ctx context.Context, prompt string, maxTokens int) (string, error) {
	body, err := json.Marshal(request{
		Model:     b.model,
		MaxTokens: maxTokens,
		Messages:  []message{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return "", fmt.Errorf("anthropic: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("anthropic: build request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", b.token)
	req.Header.Set("anthropic-version", apiVersion)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic: call: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("anthropic: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anthropic: status %d: %s", resp.StatusCode, payload)
	}

	return decodeReply(payload)
}

func decodeReply(payload []byte) (string, error) {
	var out response
	if err := json.Unmarshal(payload, &out); err != nil {
		return "", fmt.Errorf("anthropic: decode response: %w", err)
	}
	if len(out.Content) == 0 {
		return "", fmt.Errorf("anthropic: empty response")
	}
	return strings.TrimSpace(out.Content[0].Text), nil
}
