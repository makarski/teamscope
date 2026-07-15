// Package pipeline orchestrates ingest, classification, alignment scoring and
// persistence into a stored team snapshot.
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

// Classifier decides an epic's work type and the source of that decision.
type Classifier interface {
	Classify(ctx context.Context, epic *ingest.RawEpic) (domain.WorkType, domain.ClassSource)
}

// Aligner scores an epic against declared goals. A nil Aligner disables scoring.
type Aligner interface {
	Score(ctx context.Context, epic *ingest.RawEpic) (domain.Alignment, string, error)
}

// Store persists snapshots.
type Store interface {
	Save(ctx context.Context, snap domain.Snapshot) (int64, error)
}

// Runner builds and stores a snapshot per team.
type Runner struct {
	fetcher     Fetcher
	classifier  Classifier
	aligner     Aligner
	store       Store
	statusNames config.StatusNames
	goalsHash   string
	now         func() time.Time
}

// NewRunner wires the pipeline collaborators. aligner may be nil to skip
// alignment scoring (e.g. when no goals prompt or AI token is configured).
func NewRunner(
	fetcher Fetcher,
	classifier Classifier,
	aligner Aligner,
	store Store,
	statusNames config.StatusNames,
	goalsHash string,
) *Runner {
	return &Runner{
		fetcher:     fetcher,
		classifier:  classifier,
		aligner:     aligner,
		store:       store,
		statusNames: statusNames,
		goalsHash:   goalsHash,
		now:         time.Now,
	}
}

// Run builds a snapshot for one team across its Jira projects, persists it,
// and returns the stored snapshot id.
func (r *Runner) Run(ctx context.Context, team config.Team) (int64, error) {
	epics, err := r.collectEpics(team)
	if err != nil {
		return 0, err
	}

	now := r.now()
	classified := make([]domain.ClassifiedEpic, 0, len(epics))
	for i := range epics {
		classified = append(classified, r.enrich(ctx, &epics[i], now))
	}

	snap := domain.Snapshot{
		Team:      team.Name,
		TakenAt:   now,
		GoalsHash: r.goalsHash,
		Epics:     classified,
	}
	return r.store.Save(ctx, snap)
}

func (r *Runner) collectEpics(team config.Team) ([]ingest.RawEpic, error) {
	var all []ingest.RawEpic
	for _, project := range team.JiraProjects {
		epics, err := r.fetcher.FetchEpics(project)
		if err != nil {
			return nil, fmt.Errorf("pipeline: team %q: %w", team.Name, err)
		}
		all = append(all, epics...)
	}
	return all, nil
}

// enrich classifies, scores and computes progress for a single epic.
func (r *Runner) enrich(ctx context.Context, epic *ingest.RawEpic, now time.Time) domain.ClassifiedEpic {
	workType, source := r.classifier.Classify(ctx, epic)
	status, progress := ingest.ProgressOf(epic, r.statusNames, now)
	alignment, note := r.scoreAlignment(ctx, epic)

	return domain.ClassifiedEpic{
		Key:         epic.Epic.Key,
		Summary:     epic.Epic.Fields.Summary,
		WorkType:    workType,
		ClassSource: source,
		Alignment:   alignment,
		AlignNote:   note,
		Progress:    progress,
		Status:      status,
	}
}

// scoreAlignment returns the alignment verdict, defaulting to empty when no
// aligner is configured or the AI call fails (alignment is best-effort).
func (r *Runner) scoreAlignment(ctx context.Context, epic *ingest.RawEpic) (domain.Alignment, string) {
	if r.aligner == nil {
		return "", ""
	}
	alignment, note, err := r.aligner.Score(ctx, epic)
	if err != nil {
		return "", ""
	}
	return alignment, note
}
