package classify

import (
	"context"
	"errors"
	"testing"

	"github.com/andygrunwald/go-jira"

	"github.com/makarski/teamscope/config"
	"github.com/makarski/teamscope/domain"
	"github.com/makarski/teamscope/ingest"
)

func epic(summary, desc string, labels, components []string) *ingest.RawEpic {
	comps := make([]*jira.Component, 0, len(components))
	for _, c := range components {
		comps = append(comps, &jira.Component{Name: c})
	}
	return &ingest.RawEpic{
		Epic: jira.Issue{Fields: &jira.IssueFields{
			Summary:     summary,
			Description: desc,
			Labels:      labels,
			Components:  comps,
		}},
	}
}

var sampleClassify = &config.Classify{
	Business: []string{"billing", "revenue"},
	Chore:    []string{"refactor", "tech debt"},
	RnD:      []string{"spike", "experiment"},
}

func TestRuleEnginePriority(t *testing.T) {
	re := NewRuleEngine(sampleClassify)

	tests := []struct {
		name       string
		epic       *ingest.RawEpic
		wantType   domain.WorkType
		wantSource domain.ClassSource
	}{
		{
			name:       "label wins over keyword",
			epic:       epic("refactor the billing engine", "", []string{"business"}, nil),
			wantType:   domain.WorkBusiness,
			wantSource: domain.SourceLabel,
		},
		{
			name:       "component when no label",
			epic:       epic("some work", "", nil, []string{"rnd"}),
			wantType:   domain.WorkRnD,
			wantSource: domain.SourceComponent,
		},
		{
			name:       "keyword when no label or component",
			epic:       epic("spike on new queue", "", nil, nil),
			wantType:   domain.WorkRnD,
			wantSource: domain.SourceKeyword,
		},
		{
			name:       "keyword in description",
			epic:       epic("misc", "we must refactor this", nil, nil),
			wantType:   domain.WorkChore,
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
			gotType, gotSource := re.Classify(tt.epic)
			if gotSource != tt.wantSource {
				t.Errorf("source = %q, want %q", gotSource, tt.wantSource)
			}
			if tt.wantSource != domain.SourceUnknown && gotType != tt.wantType {
				t.Errorf("type = %q, want %q", gotType, tt.wantType)
			}
		})
	}
}

type stubAI struct {
	result domain.WorkType
	err    error
	called bool
}

func (s *stubAI) Classify(context.Context, *ingest.RawEpic) (domain.WorkType, error) {
	s.called = true
	return s.result, s.err
}

func TestClassifierRulesShortCircuitAI(t *testing.T) {
	ai := &stubAI{result: domain.WorkRnD}
	c := NewClassifier(NewRuleEngine(sampleClassify), ai)

	wt, src := c.Classify(context.Background(), epic("billing work", "", nil, nil))
	if wt != domain.WorkBusiness || src != domain.SourceKeyword {
		t.Errorf("got %q/%q, want business/keyword", wt, src)
	}
	if ai.called {
		t.Error("AI should not be called when a rule matches")
	}
}

func TestClassifierAIFallback(t *testing.T) {
	ai := &stubAI{result: domain.WorkRnD}
	c := NewClassifier(NewRuleEngine(sampleClassify), ai)

	wt, src := c.Classify(context.Background(), epic("do the thing", "nothing", nil, nil))
	if wt != domain.WorkRnD || src != domain.SourceAI {
		t.Errorf("got %q/%q, want rnd/ai", wt, src)
	}
	if !ai.called {
		t.Error("AI should be called when no rule matches")
	}
}

func TestClassifierAIErrorDefaultsChore(t *testing.T) {
	ai := &stubAI{err: errors.New("boom")}
	c := NewClassifier(NewRuleEngine(sampleClassify), ai)

	wt, src := c.Classify(context.Background(), epic("do the thing", "nothing", nil, nil))
	if wt != domain.WorkChore || src != domain.SourceUnknown {
		t.Errorf("got %q/%q, want chore/unknown", wt, src)
	}
}

func TestClassifierNoAIDefaultsChore(t *testing.T) {
	c := NewClassifier(NewRuleEngine(sampleClassify), nil)

	wt, src := c.Classify(context.Background(), epic("do the thing", "nothing", nil, nil))
	if wt != domain.WorkChore || src != domain.SourceUnknown {
		t.Errorf("got %q/%q, want chore/unknown", wt, src)
	}
}
