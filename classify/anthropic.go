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

const classifyMaxTokens = 32

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
func (ac *AnthropicClassifier) Map(ctx context.Context, epic *ingest.RawEpic, rubric domain.Rubric) (string, error) {
	raw, err := ac.client.Complete(ctx, buildMapPrompt(epic, rubric), classifyMaxTokens)
	if err != nil {
		return "", err
	}

	answer := strings.TrimSpace(raw)
	if strings.EqualFold(answer, "none") || answer == "" {
		return "", nil
	}
	if _, ok := rubric.Find(answer); !ok {
		return "", fmt.Errorf("classify: ai returned unknown criterion %q", raw)
	}
	return answer, nil
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
