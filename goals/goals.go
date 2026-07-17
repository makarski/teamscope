// Package goals resolves the Rubric a team is measured against. A Rubric is a
// named set of criteria produced by a pluggable Source, so the observability
// engine stays agnostic to whether goals come from a static list, Jira labels
// or (later) a parsed Confluence page.
package goals

import (
	"context"
	"fmt"

	"github.com/makarski/teamscope/domain"
)

// Source resolves the rubric for a given team. Implementations may ignore the
// team argument when the rubric is shared across teams.
type Source interface {
	Rubric(ctx context.Context, team string) (domain.Rubric, error)
}

const defaultWeight = 1.0

// StaticSource serves a fixed rubric defined inline (e.g. in config). It is
// deterministic and needs no external calls, making it the safe default and
// the backward-compatible home for the business/chore/rnd taxonomy.
type StaticSource struct {
	rubric domain.Rubric
}

// NewStaticSource builds a StaticSource from a rubric definition, defaulting
// any zero weight to 1.0 so callers need not set it.
func NewStaticSource(rubric domain.Rubric) *StaticSource {
	normalized := make([]domain.Criterion, len(rubric.Criteria))
	for i, c := range rubric.Criteria {
		if c.Weight == 0 {
			c.Weight = defaultWeight
		}
		normalized[i] = c
	}
	return &StaticSource{rubric: domain.Rubric{Name: rubric.Name, Criteria: normalized}}
}

// Rubric returns the fixed rubric, ignoring the team.
func (s *StaticSource) Rubric(_ context.Context, _ string) (domain.Rubric, error) {
	return s.rubric, nil
}

// LabelFetcher fetches the epics carrying a given label within a project.
type LabelFetcher interface {
	FetchByLabel(project, label string) ([]LabelledEpic, error)
}

// LabelledEpic is the minimal projection of a Jira epic the JiraLabelSource
// needs to synthesize a criterion.
type LabelledEpic struct {
	Key     string
	Summary string
	Done    bool
}

// NewJiraLabelSource wires a label source for one project under binding b
// (which names the rubric and sets the shared lens).
func NewJiraLabelSource(fetcher LabelFetcher, project, label string, b Binding) Source {
	return newDynamicSource(b, func() ([]goalItem, error) {
		epics, err := fetcher.FetchByLabel(project, label)
		if err != nil {
			return nil, fmt.Errorf("goals: fetch label %q in %s: %w", label, project, err)
		}
		return labelledItems(epics), nil
	})
}

func labelledItems(epics []LabelledEpic) []goalItem {
	items := make([]goalItem, 0, len(epics))
	for _, e := range epics {
		items = append(items, goalItem{Key: e.Key, Title: e.Summary, Done: e.Done})
	}
	return items
}

// Binding names the rubric a dynamic source produces and sets the lens applied
// to every criterion. Grouping these avoids threading them as loose arguments.
type Binding struct {
	Name string
	Lens domain.Lens
}

// goalItem is one unit a dynamic source yields: it becomes a single criterion.
type goalItem struct {
	Key   string
	Title string
	Done  bool
}

// dynamicSource resolves a rubric by fetching goal items and mapping each onto
// a criterion. It unifies every non-static source (Jira label, Confluence
// readiness, and future ones): each supplies only how to fetch its items, and
// the shared criterion shape lives here in one place.
type dynamicSource struct {
	binding Binding
	items   func() ([]goalItem, error)
}

// newDynamicSource builds a source from a binding and an item fetcher. It is the
// single construction path every dynamic source (label, Confluence) funnels
// through, so the wiring lives in one place.
func newDynamicSource(b Binding, items func() ([]goalItem, error)) Source {
	return &dynamicSource{binding: b, items: items}
}

// Rubric fetches the source's goal items and maps each to a criterion, applying
// the shared shape: status from done-state, default weight, and shared lens.
func (s *dynamicSource) Rubric(_ context.Context, _ string) (domain.Rubric, error) {
	items, err := s.items()
	if err != nil {
		return domain.Rubric{}, err
	}

	criteria := make([]domain.Criterion, 0, len(items))
	for _, it := range items {
		criteria = append(criteria, domain.Criterion{
			Key:    it.Key,
			Title:  it.Title,
			Status: statusOf(it.Done),
			Weight: defaultWeight,
			Lens:   s.binding.Lens,
		})
	}
	return domain.Rubric{Name: s.binding.Name, Criteria: criteria}, nil
}

func statusOf(done bool) domain.Status {
	if done {
		return domain.CriterionDone
	}
	return domain.CriterionOpen
}

// Pillar is the minimal projection of a readiness page's pillar the
// Confluence source needs to synthesize a criterion.
type Pillar struct {
	Key   string
	Title string
	Done  bool
}

// PageFetcher parses a readiness Confluence page into its pillars.
type PageFetcher interface {
	FetchReadinessPillars(pageID string) ([]Pillar, error)
}

// NewConfluenceSource turns each pillar of a product-readiness page into a
// criterion: the pillar becomes the criterion, its RAG state the criterion
// status. This drives readiness-style rubrics straight from the page the team
// already maintains. Pillar extraction is delegated to the PageFetcher; the
// default implementation uses an AI backend to read the page. name labels the
// resulting rubric; lens (optional) is applied to every criterion.
func NewConfluenceSource(fetcher PageFetcher, pageID string, b Binding) Source {
	return newDynamicSource(b, func() ([]goalItem, error) {
		pillars, err := fetcher.FetchReadinessPillars(pageID)
		if err != nil {
			return nil, fmt.Errorf("goals: fetch readiness page %s: %w", pageID, err)
		}
		return pillarItems(pillars), nil
	})
}

func pillarItems(pillars []Pillar) []goalItem {
	items := make([]goalItem, 0, len(pillars))
	for _, p := range pillars {
		items = append(items, goalItem{Key: p.Key, Title: p.Title, Done: p.Done})
	}
	return items
}
