package classify

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

const classifyMaxTokens = 32

// batchClassifyMaxTokens caps the batch classification reply. Enough for
// hundreds of short key assignments.
const batchClassifyMaxTokens = 4096

// batchChunkSize limits how many epics go into a single AI prompt to stay
// within token limits.
const batchChunkSize = 50

// AnthropicClassifier asks Claude to map an epic onto the best-fitting
// criterion of a rubric when deterministic rules are inconclusive.
type AnthropicClassifier struct {
	client *anthropic.Client
}

// NewAnthropicClassifier builds an AI classifier from config. Returns nil if
// neither Anthropic nor Bedrock is configured, letting the caller skip AI
// fallback.
func NewAnthropicClassifier(cfg *config.Anthropic, bedrockCfg *config.Bedrock) *AnthropicClassifier {
	client := anthropic.New(cfg, bedrockCfg)
	if client == nil {
		return nil
	}
	return &AnthropicClassifier{client: client}
}

// Map returns the criterion key the epic best serves, chosen from the rubric.
// It returns an empty key (no error) when the model judges that none applies.
// The model's answer is resolved case-insensitively back to the exact
// rubric-declared key so casing differences do not degrade to unmapped.
func (ac *AnthropicClassifier) Map(ctx context.Context, epic *ingest.RawEpic, rubric domain.Rubric) (string, error) {
	raw, err := ac.client.Complete(ctx, buildMapPrompt(epic, rubric), classifyMaxTokens)
	if err != nil {
		return "", err
	}

	answer := strings.TrimSpace(raw)
	if answer == "" || strings.EqualFold(answer, "none") {
		return "", nil
	}
	if key, ok := resolveKey(rubric, answer); ok {
		return key, nil
	}
	return "", fmt.Errorf("classify: ai returned unknown criterion %q", raw)
}

// MapAll maps a batch of epics to criteria in a single AI call. Returns a map
// of epic index → criterion key. Epics that the model can't place are omitted.
// Chunks into groups of batchChunkSize to stay within token limits. On partial
// failure, mappings from successful chunks are still returned alongside the
// error.
func (ac *AnthropicClassifier) MapAll(ctx context.Context, epics []*ingest.RawEpic, rubric domain.Rubric) (map[int]string, error) {
	out := make(map[int]string, len(epics))
	var firstErr error
	for start := 0; start < len(epics); start += batchChunkSize {
		end := start + batchChunkSize
		if end > len(epics) {
			end = len(epics)
		}
		mapping, err := ac.mapBatch(ctx, epics[start:end], rubric)
		if err != nil {
			firstErr = setIfNil(firstErr, err)
			continue
		}
		mergeBatchMapping(out, mapping, start)
	}
	return out, firstErr
}

func mergeBatchMapping(out map[int]string, mapping map[int]string, offset int) {
	for localIdx, key := range mapping {
		out[offset+localIdx] = key
	}
}

func setIfNil(current, val error) error {
	if current == nil {
		return val
	}
	return current
}

func (ac *AnthropicClassifier) mapBatch(ctx context.Context, epics []*ingest.RawEpic, rubric domain.Rubric) (map[int]string, error) {
	prompt := buildBatchMapPrompt(epics, rubric)
	raw, err := ac.client.Complete(ctx, prompt, batchClassifyMaxTokens)
	if err != nil {
		return nil, err
	}
	return decodeBatchMap(raw, rubric, len(epics))
}

// resolveKey matches an answer to a rubric key case-insensitively and returns
// the exact declared key.
func resolveKey(rubric domain.Rubric, answer string) (string, bool) {
	for _, c := range rubric.Criteria {
		if strings.EqualFold(c.Key, answer) {
			return c.Key, true
		}
	}
	return "", false
}

func buildMapPrompt(epic *ingest.RawEpic, rubric domain.Rubric) string {
	var b strings.Builder
	b.WriteString("Map this work item to the single goal it best advances.\n\nGoals:\n")
	for _, c := range rubric.Criteria {
		fmt.Fprintf(&b, "- %s: %s\n", c.Key, c.Title)
	}
	fmt.Fprintf(&b,
		"\nWork item:\n%s\n\n"+
			"Answer with exactly one goal key from the list above, or \"none\" "+
			"if no goal fits. Reply with the key only.",
		epic.Text(),
	)
	return b.String()
}

// buildBatchMapPrompt asks the AI to classify multiple epics in one call.
func buildBatchMapPrompt(epics []*ingest.RawEpic, rubric domain.Rubric) string {
	var b strings.Builder
	b.WriteString("Map each work item to the single goal it best advances.\n\nGoals:\n")
	for _, c := range rubric.Criteria {
		fmt.Fprintf(&b, "- %s: %s\n", c.Key, c.Title)
	}
	b.WriteString("\nWork items:\n")
	for i, e := range epics {
		fmt.Fprintf(&b, "%d. %s\n", i, e.Text())
	}
	b.WriteString("\nReply with JSON only: {\"mappings\":[{\"index\":0,\"criterion\":\"key\"},...]}. " +
		"Use \"none\" for items that fit no goal.")
	return b.String()
}

type batchMapReply struct {
	Mappings []struct {
		Index     int    `json:"index"`
		Criterion string `json:"criterion"`
	} `json:"mappings"`
}

func decodeBatchMap(raw string, rubric domain.Rubric, count int) (map[int]string, error) {
	var parsed batchMapReply
	jsonStr := extractJSON(raw)
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return nil, fmt.Errorf("classify: decode batch reply: %w", err)
	}
	out := make(map[int]string, len(parsed.Mappings))
	for _, m := range parsed.Mappings {
		if m.Index < 0 || m.Index >= count {
			continue
		}
		if strings.EqualFold(m.Criterion, "none") {
			continue
		}
		if key, ok := resolveKey(rubric, m.Criterion); ok {
			out[m.Index] = key
		}
	}
	return out, nil
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
