// Package pipeline orchestrates ingest, rubric resolution, criterion mapping,
// advancement scoring, drift checking, narrative generation and persistence
// into a stored team snapshot.
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/andygrunwald/go-jira"
	"github.com/makarski/teamscope/config"
	"github.com/makarski/teamscope/domain"
	"github.com/makarski/teamscope/ingest"
)

// Fetcher fetches epics and standalone issues for a Jira project.
type Fetcher interface {
	FetchEpics(project string) ([]ingest.RawEpic, error)
	FetchStandaloneIssues(project string, excludeKeys map[string]bool) ([]ingest.RawEpic, error)
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
	ClassifyAll(ctx context.Context, epics []*ingest.RawEpic) []domain.CriterionRef
}

// Aligner scores whether an epic advances a criterion. A nil Aligner disables
// advancement scoring.
type Aligner interface {
	Score(ctx context.Context, epic *ingest.RawEpic, criterion domain.Criterion) (domain.Advancement, string, error)
}

// Store persists snapshots.
type Store interface {
	Save(ctx context.Context, snap domain.Snapshot) (int64, error)
}

// TicketFetcher fetches live Jira ticket status by key, for drift checking.
// May be nil to skip drift.
type TicketFetcher interface {
	FetchByKeys(keys []string) ([]domain.TicketLink, error)
}

// DriftChecker reconciles claimed readiness against live ticket status.
// May be nil to skip drift.
type DriftChecker interface {
	Compute(ctx context.Context, rubric domain.Rubric, texts []string) ([]domain.CriterionState, error)
}

// Narrator generates a plain-language progress brief from a snapshot.
// May be nil to skip narration.
type Narrator interface {
	Brief(ctx context.Context, snap domain.Snapshot) (string, error)
}

// Runner builds and stores a snapshot per team.
type Runner struct {
	deps Deps
	now  func() time.Time
}

// Deps bundles the pipeline collaborators. aligner, drift, and narrator may
// be nil to skip those stages.
type Deps struct {
	Fetcher     Fetcher
	Sources     map[string]RubricSource // keyed by team name
	Factory     ClassifierFactory
	Aligner     Aligner
	Store       Store
	StatusNames config.StatusNames
	GoalsHash   string
	Drift       DriftChecker
	DriftTexts  func(team string, epics []ingest.RawEpic) []string
	Narrator    Narrator
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
	slog.Info("fetched issues", "team", team.Name, "epics", len(epics))

	rc := runContext{rubric: rubric, classifier: r.deps.Factory.For(rubric), now: r.now()}

	// Batch classify all epics in one pass to avoid N+1 AI calls.
	epicPtrs := make([]*ingest.RawEpic, len(epics))
	for i := range epics {
		epicPtrs[i] = &epics[i]
	}
	refs := rc.classifier.ClassifyAll(ctx, epicPtrs)

	classified := make([]domain.ClassifiedEpic, 0, len(epics))
	for i := range epics {
		classified = append(classified, r.enrichWithRef(ctx, &epics[i], refs[i], rc))
	}
	slog.Info("enriched epics", "team", team.Name, "count", len(classified))

	states := r.computeDrift(ctx, rubric, team, epics)
	slog.Info("computed drift", "team", team.Name, "states", len(states))

	snap := domain.Snapshot{
		Team:      team.Name,
		Rubric:    rubric,
		TakenAt:   rc.now,
		GoalsHash: r.deps.GoalsHash,
		Epics:     classified,
		States:    states,
	}

	snap.Narrative = r.generateNarrative(ctx, snap)

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
		slog.Info("fetching epics", "team", team.Name, "project", project)
		epics, err := r.deps.Fetcher.FetchEpics(project)
		if err != nil {
			return nil, fmt.Errorf("pipeline: team %q: %w", team.Name, err)
		}
		all = append(all, epics...)

		slog.Info("fetching standalone issues", "team", team.Name, "project", project)
		standalone, err := r.deps.Fetcher.FetchStandaloneIssues(project, childKeys(epics))
		if err != nil {
			return nil, fmt.Errorf("pipeline: team %q: standalone: %w", team.Name, err)
		}
		all = append(all, standalone...)
	}
	return all, nil
}

// childKeys builds a set of all child issue keys across a set of epics, so
// standalone issue fetching can exclude them.
func childKeys(epics []ingest.RawEpic) map[string]bool {
	keys := make(map[string]bool)
	for _, e := range epics {
		for _, issue := range e.Issues {
			keys[issue.Key] = true
		}
	}
	return keys
}

// enrich maps, scores advancement and computes progress for a single epic.
func (r *Runner) enrich(ctx context.Context, epic *ingest.RawEpic, rc runContext) domain.ClassifiedEpic {
	ref := rc.classifier.Classify(ctx, epic)
	return r.enrichWithRef(ctx, epic, ref, rc)
}

// enrichWithRef takes a pre-computed criterion ref (from batch classification)
// and completes the enrichment: progress, lens, advancement scoring.
func (r *Runner) enrichWithRef(ctx context.Context, epic *ingest.RawEpic, ref domain.CriterionRef, rc runContext) domain.ClassifiedEpic {
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
		Tickets:   epicTickets(epic, r.deps.StatusNames),
	}
}

// epicTickets converts an epic's child issues into EpicTicket views, classified
// into the configured status buckets.
func epicTickets(epic *ingest.RawEpic, sn config.StatusNames) []domain.EpicTicket {
	if len(epic.Issues) == 0 {
		return nil
	}
	tickets := make([]domain.EpicTicket, 0, len(epic.Issues))
	for _, issue := range epic.Issues {
		tickets = append(tickets, domain.EpicTicket{
			Key:     issue.Key,
			Summary: issue.Fields.Summary,
			Status:  ticketProgressStatus(issue.Fields.Status, sn),
		})
	}
	return tickets
}

// ticketProgressStatus buckets a Jira issue status name into a ProgressStatus
// using the configured status names.
func ticketProgressStatus(status *jira.Status, sn config.StatusNames) domain.ProgressStatus {
	if status == nil {
		return domain.StatusToDo
	}
	name := status.Name
	if contains(sn.Done, name) {
		return domain.StatusDone
	}
	if contains(sn.InProgress, name) {
		return domain.StatusOngoing
	}
	if contains(sn.ToDo, name) {
		return domain.StatusToDo
	}
	return domain.StatusToDo
}

func contains(s []string, v string) bool {
	for _, item := range s {
		if item == v {
			return true
		}
	}
	return false
}

// scoreAdvancement returns whether the epic advances its criterion, defaulting
// to unscored when no aligner is configured or the AI call fails (best-effort).
func (r *Runner) scoreAdvancement(ctx context.Context, epic *ingest.RawEpic, criterion domain.Criterion) (domain.Advancement, string) {
	if r.deps.Aligner == nil {
		return domain.AdvUnscored, ""
	}
	advances, note, err := r.deps.Aligner.Score(ctx, epic, criterion)
	if err != nil {
		return domain.AdvUnscored, ""
	}
	return advances, note
}

// computeDrift reconciles the rubric's claimed readiness against live Jira
// ticket status. Returns empty when no drift checker is configured.
func (r *Runner) computeDrift(ctx context.Context, rubric domain.Rubric, team config.Team, epics []ingest.RawEpic) []domain.CriterionState {
	if r.deps.Drift == nil {
		return nil
	}
	texts := r.driftTexts(team, epics)
	states, err := r.deps.Drift.Compute(ctx, rubric, texts)
	if err != nil {
		return nil
	}
	return states
}

// driftTexts collects the text sources to scan for referenced ticket keys.
func (r *Runner) driftTexts(team config.Team, epics []ingest.RawEpic) []string {
	if r.deps.DriftTexts != nil {
		return r.deps.DriftTexts(team.Name, epics)
	}
	// Default: scan epic descriptions and summaries.
	texts := make([]string, 0, len(epics))
	for i := range epics {
		texts = append(texts, epics[i].Text())
	}
	return texts
}

// generateNarrative produces a PO-style brief for the snapshot. Returns empty
// when no narrator is configured.
func (r *Runner) generateNarrative(ctx context.Context, snap domain.Snapshot) string {
	if r.deps.Narrator == nil {
		return ""
	}
	brief, err := r.deps.Narrator.Brief(ctx, snap)
	if err != nil {
		return ""
	}
	return brief
}
