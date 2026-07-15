// Package align scores epics against a team's declared goals prompt.
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

// Scorer judges how well an epic serves the declared goals.
type Scorer struct {
	client *anthropic.Client
	prompt string
}

// NewScorer builds a goals scorer. Returns nil when either the AI client or
// the goals prompt is absent, so callers can skip alignment scoring.
func NewScorer(cfg *config.Anthropic, goals *config.Goals) *Scorer {
	client := anthropic.New(cfg)
	if client == nil || !hasGoals(goals) {
		return nil
	}
	return &Scorer{client: client, prompt: goals.Prompt}
}

func hasGoals(goals *config.Goals) bool {
	return goals != nil && strings.TrimSpace(goals.Prompt) != ""
}

type scoreReply struct {
	Alignment string `json:"alignment"`
	Note      string `json:"note"`
}

// Score returns the epic's alignment and a short explanatory note.
func (s *Scorer) Score(ctx context.Context, epic *ingest.RawEpic) (domain.Alignment, string, error) {
	raw, err := s.client.Complete(ctx, s.buildPrompt(epic), alignMaxTokens)
	if err != nil {
		return "", "", err
	}

	reply, err := parseReply(raw)
	if err != nil {
		return "", "", err
	}

	alignment, err := normalizeAlignment(reply.Alignment)
	if err != nil {
		return "", "", err
	}
	return alignment, strings.TrimSpace(reply.Note), nil
}

func (s *Scorer) buildPrompt(epic *ingest.RawEpic) string {
	return fmt.Sprintf(
		"Team goals:\n%s\n\n"+
			"Work item:\n%s\n\n"+
			"Judge how well the work item serves the goals. Reply with JSON only: "+
			`{"alignment":"aligned|partial|off_track","note":"<max 12 words>"}.`,
		s.prompt, epic.Text(),
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

func normalizeAlignment(v string) (domain.Alignment, error) {
	switch domain.Alignment(strings.ToLower(strings.TrimSpace(v))) {
	case domain.AlignAligned:
		return domain.AlignAligned, nil
	case domain.AlignPartial:
		return domain.AlignPartial, nil
	case domain.AlignOffTrack:
		return domain.AlignOffTrack, nil
	default:
		return "", fmt.Errorf("align: invalid alignment %q", v)
	}
}
