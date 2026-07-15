// Package anthropic provides a minimal client for the Anthropic Messages API,
// shared by the classify and align stages.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/makarski/teamscope/config"
)

const (
	defaultBaseURL = "https://api.anthropic.com"
	apiVersion     = "2023-06-01"
)

// Client is a minimal Anthropic Messages API client.
type Client struct {
	httpClient *http.Client
	baseURL    string
	token      string
	model      string
}

// New builds a client from config. Returns nil if no token is configured,
// letting callers cleanly skip the AI stage.
func New(cfg *config.Anthropic) *Client {
	if cfg == nil || cfg.Token == "" {
		return nil
	}
	base := cfg.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	return &Client{
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
	body, err := json.Marshal(request{
		Model:     c.model,
		MaxTokens: maxTokens,
		Messages:  []message{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return "", fmt.Errorf("anthropic: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("anthropic: build request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", c.token)
	req.Header.Set("anthropic-version", apiVersion)

	return c.do(req)
}

func (c *Client) do(req *http.Request) (string, error) {
	resp, err := c.httpClient.Do(req)
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

	var out response
	if err := json.Unmarshal(payload, &out); err != nil {
		return "", fmt.Errorf("anthropic: decode response: %w", err)
	}
	if len(out.Content) == 0 {
		return "", fmt.Errorf("anthropic: empty response")
	}
	return strings.TrimSpace(out.Content[0].Text), nil
}
