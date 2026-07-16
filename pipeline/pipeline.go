// Package pipeline orchestrates ingest, rubric resolution, criterion mapping,
// advancement scoring and persistence into a stored team snapshot.
package pipeline

import (
	"context"
	"fmt"
	"time"

	"github.com/makarski/teamscope/config"
	"github.com/makarski/teamscope/domain"
	"github.com/makarski/teamscope/ingest"
)

// Fetcher fetches epics for a Jira project.
type Fetcher interface {
	FetchEpics(project string) ([]ingest.RawEpic, error)
}

// RubricSource resolves the rubric a team is measured against.
type RubricSource interface {
	Rubric(ctx context.Context, team string) (domain.Rubric, error)
}

// ClassifierFactory builds a criterion mapper bound to a specific rubric.
type ClassifierFactory interface {
	For(rubric domain.Rubric) Classifier
}

// Classifier maps an epic onto its team's rubric.
type Classifier interface {
	Classify(ctx context.Context, epic *ingest.RawEpic) domain.CriterionRef
}

// Aligner scores whether an epic advances a criterion. A nil Aligner disables
// advancement scoring.
type Aligner interface {
	Score(ctx context.Context, epic *ingest.RawEpic, criterion domain.Criterion) (bool, string, error)
}

// Store persists snapshots.
type Store interface {
	Save(ctx context.Context, snap domain.Snapshot) (int64, error)
}

// Runner builds and stores a snapshot per team.
type Runner struct {
	deps Deps
	now  func() time.Time
}

// Deps bundles the pipeline collaborators. aligner may be nil to skip
// advancement scoring.
type Deps struct {
	Fetcher     Fetcher
	Sources     map[string]RubricSource // keyed by team name
	Factory     ClassifierFactory
	Aligner     Aligner
	Store       Store
	StatusNames config.StatusNames
	GoalsHash   string
}

// NewRunner wires the pipeline collaborators from Deps.
func NewRunner(d Deps) *Runner {
	return &Runner{deps: d, now: time.Now}
}

// runContext carries the per-team values shared while enriching each epic.
type runContext struct {
	rubric     domain.Rubric
	classifier Classifier
	now        time.Time
}

// Run builds a snapshot for one team across its Jira projects, persists it,
// and returns the stored snapshot id.
func (r *Runner) Run(ctx context.Context, team config.Team) (int64, error) {
	rubric, err := r.resolveRubric(ctx, team)
	if err != nil {
		return 0, err
	}

	epics, err := r.collectEpics(team)
	if err != nil {
		return 0, err
	}

	rc := runContext{rubric: rubric, classifier: r.deps.Factory.For(rubric), now: r.now()}
	classified := make([]domain.ClassifiedEpic, 0, len(epics))
	for i := range epics {
		classified = append(classified, r.enrich(ctx, &epics[i], rc))
	}

	snap := domain.Snapshot{
		Team:      team.Name,
		Rubric:    rubric,
		TakenAt:   rc.now,
		GoalsHash: r.deps.GoalsHash,
		Epics:     classified,
	}
	return r.deps.Store.Save(ctx, snap)
}

func (r *Runner) resolveRubric(ctx context.Context, team config.Team) (domain.Rubric, error) {
	src, ok := r.deps.Sources[team.Name]
	if !ok {
		return domain.Rubric{}, fmt.Errorf("pipeline: team %q: no rubric source configured", team.Name)
	}
	rubric, err := src.Rubric(ctx, team.Name)
	if err != nil {
		return domain.Rubric{}, fmt.Errorf("pipeline: team %q: %w", team.Name, err)
	}
	return rubric, nil
}

func (r *Runner) collectEpics(team config.Team) ([]ingest.RawEpic, error) {
	var all []ingest.RawEpic
	for _, project := range team.JiraProjects {
		epics, err := r.deps.Fetcher.FetchEpics(project)
		if err != nil {
			return nil, fmt.Errorf("pipeline: team %q: %w", team.Name, err)
		}
		all = append(all, epics...)
	}
	return all, nil
}

// enrich maps, scores advancement and computes progress for a single epic.
func (r *Runner) enrich(ctx context.Context, epic *ingest.RawEpic, rc runContext) domain.ClassifiedEpic {
	ref := rc.classifier.Classify(ctx, epic)
	status, progress := ingest.ProgressOf(epic, r.deps.StatusNames, rc.now)

	lens := domain.LensNone
	if c, ok := rc.rubric.Find(ref.Key); ok {
		lens = c.Lens
		ref.Advances, ref.Note = r.scoreAdvancement(ctx, epic, c)
	}

	return domain.ClassifiedEpic{
		Key:       epic.Epic.Key,
		Summary:   epic.Epic.Fields.Summary,
		Criterion: ref,
		Lens:      lens,
		Progress:  progress,
		Status:    status,
	}
}

// scoreAdvancement returns whether the epic advances its criterion, defaulting
// to false when no aligner is configured or the AI call fails (best-effort).
func (r *Runner) scoreAdvancement(ctx context.Context, epic *ingest.RawEpic, criterion domain.Criterion) (bool, string) {
	if r.deps.Aligner == nil {
		return false, ""
	}
	advances, note, err := r.deps.Aligner.Score(ctx, epic, criterion)
	if err != nil {
		return false, ""
	}
	return advances, note
}
