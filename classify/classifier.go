package classify

import (
	"context"

	"github.com/makarski/teamscope/domain"
	"github.com/makarski/teamscope/ingest"
)

// AIMapper is the subset of AnthropicClassifier used for fallback, kept as an
// interface so it can be stubbed in tests and left nil to disable AI.
type AIMapper interface {
	Map(ctx context.Context, epic *ingest.RawEpic, rubric domain.Rubric) (string, error)
}

// Classifier maps epics onto one team's rubric: deterministic rules first, AI
// mapping when rules are inconclusive. An unmapped epic yields an empty key.
type Classifier struct {
	rubric domain.Rubric
	rules  *RuleEngine
	ai     AIMapper
}

// Classify maps an epic to a criterion of the rubric and records how it was
// decided.
func (c *Classifier) Classify(ctx context.Context, epic *ingest.RawEpic) domain.CriterionRef {
	if key, src := c.rules.Map(epic); src != domain.SourceUnknown {
		return domain.CriterionRef{Key: key, Source: src}
	}

	if c.ai != nil {
		if key, err := c.ai.Map(ctx, epic, c.rubric); err == nil && key != "" {
			return domain.CriterionRef{Key: key, Source: domain.SourceAI}
		}
	}

	return domain.CriterionRef{Key: "", Source: domain.SourceUnknown}
}

// Factory builds rubric-bound classifiers. It carries the shared AI mapper and
// per-rubric keyword hints so the pipeline can spin up a Classifier once the
// rubric is resolved at runtime.
type Factory struct {
	ai    AIMapper
	hints []KeywordHint
}

// NewFactory builds a classifier factory. Pass a nil ai to disable AI mapping;
// hints supply optional keyword→criterion mappings applied to every rubric.
func NewFactory(ai AIMapper, hints []KeywordHint) *Factory {
	return &Factory{ai: ai, hints: hints}
}

// For returns a Classifier bound to the given rubric.
func (f *Factory) For(rubric domain.Rubric) *Classifier {
	return &Classifier{
		rubric: rubric,
		rules:  NewRuleEngine(rubric, f.hints),
		ai:     f.ai,
	}
}
