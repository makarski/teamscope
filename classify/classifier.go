package classify

import (
	"context"

	"github.com/makarski/teamscope/domain"
	"github.com/makarski/teamscope/ingest"
)

// aiClassifier is the subset of AnthropicClassifier used for fallback,
// kept as an interface so it can be stubbed in tests.
type aiClassifier interface {
	Classify(ctx context.Context, epic *ingest.RawEpic) (domain.WorkType, error)
}

// Classifier decides an epic's work type: rules first, AI fallback when
// rules are inconclusive. When no AI is configured, it defaults to chore.
type Classifier struct {
	rules *RuleEngine
	ai    aiClassifier
}

// NewClassifier wires the rule engine with an optional AI fallback.
// Pass a nil ai to disable the AI stage.
func NewClassifier(rules *RuleEngine, ai aiClassifier) *Classifier {
	return &Classifier{rules: rules, ai: ai}
}

// Classify returns the work type and the source that decided it.
func (c *Classifier) Classify(ctx context.Context, epic *ingest.RawEpic) (domain.WorkType, domain.ClassSource) {
	if wt, src := c.rules.Classify(epic); src != domain.SourceUnknown {
		return wt, src
	}

	if c.ai != nil {
		if wt, err := c.ai.Classify(ctx, epic); err == nil {
			return wt, domain.SourceAI
		}
	}

	// Safe default: unclassified maintenance work counts as chore.
	return domain.WorkChore, domain.SourceUnknown
}
