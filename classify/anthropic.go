package classify

import (
	"context"
	"fmt"
	"strings"

	"github.com/makarski/teamscope/anthropic"
	"github.com/makarski/teamscope/config"
	"github.com/makarski/teamscope/domain"
	"github.com/makarski/teamscope/ingest"
)

const classifyMaxTokens = 16

// AnthropicClassifier asks Claude to pick a work type when rules are inconclusive.
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

// Classify returns a work type inferred from the epic's text.
func (ac *AnthropicClassifier) Classify(ctx context.Context, epic *ingest.RawEpic) (domain.WorkType, error) {
	raw, err := ac.client.Complete(ctx, buildClassifyPrompt(epic), classifyMaxTokens)
	if err != nil {
		return "", err
	}

	wt := domain.WorkType(strings.ToLower(strings.TrimSpace(raw)))
	if !wt.Valid() {
		return "", fmt.Errorf("classify: ai returned invalid work type %q", raw)
	}
	return wt, nil
}

func buildClassifyPrompt(epic *ingest.RawEpic) string {
	return fmt.Sprintf(
		"Classify this software work item into exactly one category: "+
			"business (customer/revenue value), chore (maintenance/tech-debt/upgrades), "+
			"or rnd (research/experiments/spikes).\n\n"+
			"Title and description:\n%s\n\n"+
			"Answer with a single word: business, chore, or rnd.",
		epic.Text(),
	)
}
