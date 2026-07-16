package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/makarski/teamscope/anthropic"
	"github.com/makarski/teamscope/config"
	"github.com/makarski/teamscope/goals"
)

// extractMaxTokens caps the readiness-extraction reply. A readiness page has a
// handful of pillars, each a short key/title/status, so this is ample.
const extractMaxTokens = 1024

// completer performs a single AI completion. *anthropic.Client satisfies it;
// tests supply a stub.
type completer interface {
	Complete(ctx context.Context, prompt string, maxTokens int) (string, error)
}

// ConfluenceClient reads a readiness page and asks an AI to extract its pillars
// as rubric criteria. It reuses the Atlassian basic-auth credentials to fetch
// the page and the shared Claude client to interpret it, so it tolerates the
// free-form structure real readiness pages have.
type ConfluenceClient struct {
	fetch func(pageID string) (json.RawMessage, error)
	ai    completer
}

// NewConfluenceClient builds a Confluence client from the Jira credentials
// (Atlassian Cloud shares one auth domain) and the configured AI backend.
// Returns nil when no AI is configured, so callers can require AI for readiness
// rubrics and fail loudly rather than silently degrade.
func NewConfluenceClient(jiraCfg *config.Jira, aiCfg *config.Anthropic, bedrockCfg *config.Bedrock) *ConfluenceClient {
	ai := anthropic.New(aiCfg, bedrockCfg)
	if ai == nil {
		return nil
	}
	return &ConfluenceClient{
		fetch: newPageFetcher(jiraCfg.BaseURL, jiraCfg.User, jiraCfg.Token),
		ai:    ai,
	}
}

// FetchReadinessPillars fetches the page, flattens it to text, and asks the AI
// to extract its readiness pillars.
func (c *ConfluenceClient) FetchReadinessPillars(pageID string) ([]goals.Pillar, error) {
	body, err := c.fetch(pageID)
	if err != nil {
		return nil, fmt.Errorf("ingest: fetch confluence page %s: %w", pageID, err)
	}

	text, err := pageText(body)
	if err != nil {
		return nil, fmt.Errorf("ingest: read confluence page %s: %w", pageID, err)
	}

	pillars, err := c.extract(text)
	if err != nil {
		return nil, fmt.Errorf("ingest: extract pillars from page %s: %w", pageID, err)
	}
	return pillars, nil
}

// extract prompts the AI to read the page text and return its pillars.
func (c *ConfluenceClient) extract(pageText string) ([]goals.Pillar, error) {
	reply, err := c.ai.Complete(context.Background(), extractPrompt(pageText), extractMaxTokens)
	if err != nil {
		return nil, err
	}
	return decodePillars(reply)
}

func extractPrompt(pageText string) string {
	return "You are reading a product-readiness assessment page. It defines several " +
		"readiness pillars (e.g. Feature Readiness, Security & Compliance), each with a " +
		"RAG status (red/amber/green) or an equivalent verdict.\n\n" +
		"Extract every pillar. For each, return:\n" +
		`  "key":   a short lowercase slug (e.g. "feature-readiness")` + "\n" +
		`  "title": the pillar's name` + "\n" +
		`  "done":  true only if the pillar is fully met (all green / no open gaps), else false` + "\n\n" +
		`Reply with JSON only: {"pillars":[{"key":"...","title":"...","done":true|false}]}.` + "\n\n" +
		"Page:\n" + pageText
}

type pillarReply struct {
	Pillars []struct {
		Key   string `json:"key"`
		Title string `json:"title"`
		Done  bool   `json:"done"`
	} `json:"pillars"`
}

func decodePillars(reply string) ([]goals.Pillar, error) {
	var parsed pillarReply
	if err := json.Unmarshal([]byte(extractJSON(reply)), &parsed); err != nil {
		return nil, fmt.Errorf("decode reply %q: %w", reply, err)
	}
	if len(parsed.Pillars) == 0 {
		return nil, fmt.Errorf("no pillars found in reply %q", reply)
	}

	pillars := make([]goals.Pillar, 0, len(parsed.Pillars))
	for _, p := range parsed.Pillars {
		if p.Key == "" || p.Title == "" {
			continue
		}
		pillars = append(pillars, goals.Pillar{Key: p.Key, Title: p.Title, Done: p.Done})
	}
	if len(pillars) == 0 {
		return nil, fmt.Errorf("no usable pillars in reply %q", reply)
	}
	return pillars, nil
}

// extractJSON pulls the first {...} object out of a reply, tolerating any prose
// the model wraps around it.
func extractJSON(raw string) string {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if !validJSONBounds(start, end) {
		return raw
	}
	return raw[start : end+1]
}

// validJSONBounds reports whether start/end delimit a non-empty {...} span.
func validJSONBounds(start, end int) bool {
	if start == -1 || end == -1 {
		return false
	}
	return end >= start
}
