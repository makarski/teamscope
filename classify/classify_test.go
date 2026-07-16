package classify

import (
	"context"
	"errors"
	"testing"

	"github.com/andygrunwald/go-jira"

	"github.com/makarski/teamscope/domain"
	"github.com/makarski/teamscope/ingest"
)

func epic(summary, desc string, labels, components []string) *ingest.RawEpic {
	comps := make([]*jira.Component, 0, len(components))
	for _, c := range components {
		comps = append(comps, &jira.Component{Name: c})
	}
	issue := jira.Issue{Fields: &jira.IssueFields{
		Summary:    summary,
		Labels:     labels,
		Components: comps,
	}}
	re := ingest.NewRawEpic(issue, nil, desc)
	return &re
}

// workRubric is the business/chore/rnd taxonomy expressed as a static rubric.
var workRubric = domain.Rubric{Name: "work", Criteria: []domain.Criterion{
	{Key: "business", Title: "Customer/revenue value"},
	{Key: "chore", Title: "Maintenance and tech debt"},
	{Key: "rnd", Title: "Research and experiments"},
}}

var workHints = []KeywordHint{
	{Keyword: "billing", CriterionKey: "business"},
	{Keyword: "revenue", CriterionKey: "business"},
	{Keyword: "refactor", CriterionKey: "chore"},
	{Keyword: "tech debt", CriterionKey: "chore"},
	{Keyword: "spike", CriterionKey: "rnd"},
	{Keyword: "experiment", CriterionKey: "rnd"},
}

func TestRuleEnginePriority(t *testing.T) {
	re := NewRuleEngine(workRubric, workHints)

	tests := []struct {
		name       string
		epic       *ingest.RawEpic
		wantKey    string
		wantSource domain.ClassSource
	}{
		{
			name:       "label wins over keyword",
			epic:       epic("refactor the billing engine", "", []string{"business"}, nil),
			wantKey:    "business",
			wantSource: domain.SourceLabel,
		},
		{
			name:       "component when no label",
			epic:       epic("some work", "", nil, []string{"rnd"}),
			wantKey:    "rnd",
			wantSource: domain.SourceComponent,
		},
		{
			name:       "keyword when no label or component",
			epic:       epic("spike on new queue", "", nil, nil),
			wantKey:    "rnd",
			wantSource: domain.SourceKeyword,
		},
		{
			name:       "keyword in description",
			epic:       epic("misc", "we must refactor this", nil, nil),
			wantKey:    "chore",
			wantSource: domain.SourceKeyword,
		},
		{
			name:       "no match is unknown",
			epic:       epic("do the thing", "nothing notable", nil, nil),
			wantSource: domain.SourceUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotKey, gotSource := re.Map(tt.epic)
			if gotSource != tt.wantSource {
				t.Errorf("source = %q, want %q", gotSource, tt.wantSource)
			}
			if tt.wantSource != domain.SourceUnknown && gotKey != tt.wantKey {
				t.Errorf("key = %q, want %q", gotKey, tt.wantKey)
			}
		})
	}
}

type stubAI struct {
	result string
	err    error
	called bool
}

func (s *stubAI) Map(context.Context, *ingest.RawEpic, domain.Rubric) (string, error) {
	s.called = true
	return s.result, s.err
}

func classifierFor(ai AIMapper) *Classifier {
	return NewFactory(ai, workHints).For(workRubric)
}

func TestClassifierRulesShortCircuitAI(t *testing.T) {
	ai := &stubAI{result: "rnd"}
	c := classifierFor(ai)

	ref := c.Classify(context.Background(), epic("billing work", "", nil, nil))
	if ref.Key != "business" || ref.Source != domain.SourceKeyword {
		t.Errorf("got %q/%q, want business/keyword", ref.Key, ref.Source)
	}
	if ai.called {
		t.Error("AI should not be called when a rule matches")
	}
}

func TestClassifierAIFallback(t *testing.T) {
	ai := &stubAI{result: "rnd"}
	c := classifierFor(ai)

	ref := c.Classify(context.Background(), epic("do the thing", "nothing", nil, nil))
	if ref.Key != "rnd" || ref.Source != domain.SourceAI {
		t.Errorf("got %q/%q, want rnd/ai", ref.Key, ref.Source)
	}
	if !ai.called {
		t.Error("AI should be called when no rule matches")
	}
}

func TestClassifierAIErrorIsUnmapped(t *testing.T) {
	ai := &stubAI{err: errors.New("boom")}
	c := classifierFor(ai)

	ref := c.Classify(context.Background(), epic("do the thing", "nothing", nil, nil))
	if ref.Key != "" || ref.Source != domain.SourceUnknown {
		t.Errorf("got %q/%q, want empty/unknown", ref.Key, ref.Source)
	}
}

func TestClassifierNoAIIsUnmapped(t *testing.T) {
	c := classifierFor(nil)

	ref := c.Classify(context.Background(), epic("do the thing", "nothing", nil, nil))
	if ref.Key != "" || ref.Source != domain.SourceUnknown {
		t.Errorf("got %q/%q, want empty/unknown", ref.Key, ref.Source)
	}
}
