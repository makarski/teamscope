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

// batchScoreMaxTokens caps the batch advancement reply.
const batchScoreMaxTokens = 4096

// batchScoreChunkSize limits how many epics go into a single AI prompt.
const batchScoreChunkSize = 50

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
	Advances *bool  `json:"advances"`
	Note     string `json:"note"`
}

// Score returns whether the epic advances the criterion, plus a short note.
func (s *Scorer) Score(ctx context.Context, epic *ingest.RawEpic, criterion domain.Criterion) (domain.Advancement, string, error) {
	raw, err := s.client.Complete(ctx, s.buildPrompt(epic, criterion), alignMaxTokens)
	if err != nil {
		return domain.AdvUnscored, "", err
	}

	reply, err := parseReply(raw)
	if err != nil {
		return domain.AdvUnscored, "", err
	}
	return advancementOf(*reply.Advances), strings.TrimSpace(reply.Note), nil
}

// ScoreItem pairs an epic with the criterion it was mapped to.
type ScoreItem struct {
	Epic      *ingest.RawEpic
	Criterion domain.Criterion
}

// ScoreResult is the outcome for one epic.
type ScoreResult struct {
	Advances domain.Advancement
	Note     string
}

// ScoreAll scores a batch of epic/criterion pairs in chunked AI calls.
// Returns results indexed by input position.
func (s *Scorer) ScoreAll(ctx context.Context, items []ScoreItem) ([]ScoreResult, error) {
	results := make([]ScoreResult, len(items))
	for i := range results {
		results[i] = ScoreResult{Advances: domain.AdvUnscored}
	}

	for start := 0; start < len(items); start += batchScoreChunkSize {
		end := start + batchScoreChunkSize
		if end > len(items) {
			end = len(items)
		}
		batch := items[start:end]
		if err := s.scoreBatch(ctx, batch, results, start); err != nil {
			return nil, err
		}
	}
	return results, nil
}

func (s *Scorer) scoreBatch(ctx context.Context, batch []ScoreItem, results []ScoreResult, offset int) error {
	prompt := s.buildBatchPrompt(batch)
	raw, err := s.client.Complete(ctx, prompt, batchScoreMaxTokens)
	if err != nil {
		return err
	}
	return decodeBatchScore(raw, batch, results, offset)
}

func advancementOf(advances bool) domain.Advancement {
	if advances {
		return domain.AdvAdvances
	}
	return domain.AdvStalled
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

// buildBatchPrompt asks the AI to score multiple epic/criterion pairs in one call.
func (s *Scorer) buildBatchPrompt(items []ScoreItem) string {
	var b strings.Builder
	b.WriteString("For each work item, judge whether it meaningfully advances the stated goal.\n\n")
	for i, item := range items {
		fmt.Fprintf(&b, "%d. Goal: %s: %s\n   Work item: %s\n\n", i, item.Criterion.Key, item.Criterion.Title, item.Epic.Text())
	}
	b.WriteString(`Reply with JSON only: {"results":[{"index":0,"advances":true|false,"note":"<max 12 words>"},...]}.`)
	return b.String()
}

type batchScoreReply struct {
	Results []struct {
		Index    int    `json:"index"`
		Advances *bool  `json:"advances"`
		Note     string `json:"note"`
	} `json:"results"`
}

func decodeBatchScore(raw string, batch []ScoreItem, results []ScoreResult, offset int) error {
	var parsed batchScoreReply
	jsonStr := extractJSON(raw)
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return fmt.Errorf("align: decode batch reply: %w", err)
	}
	for _, r := range parsed.Results {
		if r.Index < 0 || r.Index >= len(batch) {
			continue
		}
		if r.Advances == nil {
			continue
		}
		results[offset+r.Index] = ScoreResult{
			Advances: advancementOf(*r.Advances),
			Note:     strings.TrimSpace(r.Note),
		}
	}
	return nil
}

func parseReply(raw string) (scoreReply, error) {
	jsonPart := extractJSON(raw)
	var reply scoreReply
	if err := json.Unmarshal([]byte(jsonPart), &reply); err != nil {
		return scoreReply{}, fmt.Errorf("align: decode reply %q: %w", raw, err)
	}
	if reply.Advances == nil {
		return scoreReply{}, fmt.Errorf("align: reply missing advances field")
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
