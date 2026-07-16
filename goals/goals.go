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

// JiraLabelSource turns every epic carrying a configured label into a
// criterion: the epic key is the criterion key, its summary the title, and its
// Jira done-state the criterion status. This automates readiness-style rubrics
// straight from Jira with no AI.
type JiraLabelSource struct {
	fetcher LabelFetcher
	project string
	label   string
	name    string
	lens    domain.Lens
}

// NewJiraLabelSource wires a label source for one project. name labels the
// resulting rubric; lens (optional) is applied to every criterion.
func NewJiraLabelSource(fetcher LabelFetcher, project, label, name string, lens domain.Lens) *JiraLabelSource {
	return &JiraLabelSource{fetcher: fetcher, project: project, label: label, name: name, lens: lens}
}

// Rubric queries the label and maps each epic to a criterion.
func (s *JiraLabelSource) Rubric(_ context.Context, _ string) (domain.Rubric, error) {
	epics, err := s.fetcher.FetchByLabel(s.project, s.label)
	if err != nil {
		return domain.Rubric{}, fmt.Errorf("goals: fetch label %q in %s: %w", s.label, s.project, err)
	}

	criteria := make([]domain.Criterion, 0, len(epics))
	for _, e := range epics {
		criteria = append(criteria, domain.Criterion{
			Key:    e.Key,
			Title:  e.Summary,
			Status: statusOf(e.Done),
			Weight: defaultWeight,
			Lens:   s.lens,
		})
	}
	return domain.Rubric{Name: s.name, Criteria: criteria}, nil
}

func statusOf(done bool) domain.Status {
	if done {
		return domain.CriterionDone
	}
	return domain.CriterionOpen
}
