// Package align scores whether an epic advances the criterion it maps to.
package align

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/makarski/teamscope/anthropic"
	"github.com/makarski/teamscope/config"
	"github.com/makarski/teamscope/domain"
	"github.com/makarski/teamscope/ingest"
)

const alignMaxTokens = 128

// Scorer judges whether an epic advances a given criterion.
type Scorer struct {
	client *anthropic.Client
}

// NewScorer builds an advancement scorer. Returns nil when no AI client is
// configured, so callers can skip advancement scoring.
func NewScorer(cfg *config.Anthropic, bedrockCfg *config.Bedrock) *Scorer {
	client := anthropic.New(cfg, bedrockCfg)
	if client == nil {
		return nil
	}
	return &Scorer{client: client}
}

type scoreReply struct {
	Advances bool   `json:"advances"`
	Note     string `json:"note"`
}

// Score returns whether the epic advances the criterion, plus a short note.
func (s *Scorer) Score(ctx context.Context, epic *ingest.RawEpic, criterion domain.Criterion) (bool, string, error) {
	raw, err := s.client.Complete(ctx, s.buildPrompt(epic, criterion), alignMaxTokens)
	if err != nil {
		return false, "", err
	}

	reply, err := parseReply(raw)
	if err != nil {
		return false, "", err
	}
	return reply.Advances, strings.TrimSpace(reply.Note), nil
}

func (s *Scorer) buildPrompt(epic *ingest.RawEpic, criterion domain.Criterion) string {
	return fmt.Sprintf(
		"Goal:\n%s: %s\n\n"+
			"Work item:\n%s\n\n"+
			"Does the work item meaningfully advance the goal? Reply with JSON only: "+
			`{"advances":true|false,"note":"<max 12 words>"}.`,
		criterion.Key, criterion.Title, epic.Text(),
	)
}

func parseReply(raw string) (scoreReply, error) {
	jsonPart := extractJSON(raw)
	var reply scoreReply
	if err := json.Unmarshal([]byte(jsonPart), &reply); err != nil {
		return scoreReply{}, fmt.Errorf("align: decode reply %q: %w", raw, err)
	}
	return reply, nil
}

// extractJSON pulls the first {...} object out of a reply, tolerating any
// prose the model wraps around it.
func extractJSON(raw string) string {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if !validJSONBounds(start, end) {
		return raw
	}
	return raw[start : end+1]
}

func validJSONBounds(start, end int) bool {
	return start != -1 && end != -1 && end >= start
}
