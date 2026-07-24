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
	"github.com/makarski/teamscope/align"
	"github.com/makarski/teamscope/config"
	"github.com/makarski/teamscope/domain"
	"github.com/makarski/teamscope/github"
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
	ScoreAll(ctx context.Context, items []align.ScoreItem) ([]align.ScoreResult, error)
}

// Store persists snapshots.
type Store interface {
	Save(ctx context.Context, snap domain.Snapshot) (int64, error)
}

// GitHubFetcher fetches contribution activity for a team's repos. May be nil
// to skip the activity signal.
type GitHubFetcher interface {
	FetchAttributedActivity(ctx context.Context, repos []string, since time.Time, epics []github.EpicRef) (map[string]domain.Activity, error)
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

// Deps bundles the pipeline collaborators. aligner, drift, github, and
// narrator may be nil to skip those stages.
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
	GitHub      GitHubFetcher
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
	if len(refs) != len(epics) {
		slog.Warn("batch classify returned wrong length, falling back", "team", team.Name, "got", len(refs), "want", len(epics))
		refs = make([]domain.CriterionRef, len(epics))
		for i := range epics {
			refs[i] = rc.classifier.Classify(ctx, &epics[i])
		}
	}

	// Batch score advancement for all epics that mapped to a criterion.
	scoreResults := r.batchScore(ctx, epics, refs, rc.rubric)

	classified := make([]domain.ClassifiedEpic, 0, len(epics))
	for i := range epics {
		ref := applyScore(refs[i], scoreResults[i], rc.rubric)
		classified = append(classified, r.enrichWithRef(ctx, &epics[i], ref, rc))
	}
	slog.Info("enriched epics", "team", team.Name, "count", len(classified))

	classified = r.enrichActivity(ctx, team, classified)

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
	all = deduplicateEpics(all)
	return all, nil
}

// deduplicateEpics removes duplicate epics by key, keeping the first occurrence
// (which has child issues populated). Standalone issues don't have children, so
// the epic version with children always wins.
func deduplicateEpics(epics []ingest.RawEpic) []ingest.RawEpic {
	seen := make(map[string]bool, len(epics))
	out := make([]ingest.RawEpic, 0, len(epics))
	for _, e := range epics {
		key := e.Epic.Key
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, e)
	}
	if dupes := len(epics) - len(out); dupes > 0 {
		slog.Info("deduplicated epics", "removed", dupes, "kept", len(out))
	}
	return out
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

// batchScore collects all epics that mapped to a criterion and scores them
// in a single batched AI call. Returns results indexed by epic position.
func (r *Runner) batchScore(ctx context.Context, epics []ingest.RawEpic, refs []domain.CriterionRef, rubric domain.Rubric) []align.ScoreResult {
	results := make([]align.ScoreResult, len(epics))
	for i := range results {
		results[i] = align.ScoreResult{Advances: domain.AdvUnscored}
	}
	if r.deps.Aligner == nil {
		return results
	}

	items, indices := collectScoreItems(epics, refs, rubric)
	if len(items) == 0 {
		return results
	}

	scored, err := r.deps.Aligner.ScoreAll(ctx, items)
	if err != nil {
		return results
	}
	for i, idx := range indices {
		if i < len(scored) {
			results[idx] = scored[i]
		}
	}
	return results
}

func collectScoreItems(epics []ingest.RawEpic, refs []domain.CriterionRef, rubric domain.Rubric) ([]align.ScoreItem, []int) {
	var items []align.ScoreItem
	var indices []int
	for i, ref := range refs {
		c, ok := rubric.Find(ref.Key)
		if !ok {
			continue
		}
		items = append(items, align.ScoreItem{Epic: &epics[i], Criterion: c})
		indices = append(indices, i)
	}
	return items, indices
}

// enrich maps, scores advancement and computes progress for a single epic.
func (r *Runner) enrich(ctx context.Context, epic *ingest.RawEpic, rc runContext) domain.ClassifiedEpic {
	ref := rc.classifier.Classify(ctx, epic)
	status, progress := ingest.ProgressOf(epic, r.deps.StatusNames, rc.now)

	lens := domain.LensNone
	var advances domain.Advancement
	var note string
	if c, ok := rc.rubric.Find(ref.Key); ok {
		lens = c.Lens
		advances, note = r.scoreAdvancement(ctx, epic, c)
	}

	return domain.ClassifiedEpic{
		Key:       epic.Epic.Key,
		Summary:   epic.Epic.Fields.Summary,
		Criterion: domain.CriterionRef{Key: ref.Key, Advances: advances, Source: ref.Source, Note: note},
		Lens:      lens,
		Progress:  progress,
		Status:    status,
		Tickets:   epicTickets(epic, r.deps.StatusNames),
	}
}

// applyScore merges a score result into a criterion ref, setting the
// advancement verdict and note when the ref maps to a rubric criterion.
func applyScore(ref domain.CriterionRef, score align.ScoreResult, rubric domain.Rubric) domain.CriterionRef {
	if _, ok := rubric.Find(ref.Key); ok {
		ref.Advances = score.Advances
		ref.Note = score.Note
	}
	return ref
}

// enrichWithRef takes a pre-computed criterion ref (with score already applied)
// and completes the enrichment: progress, lens, tickets.
func (r *Runner) enrichWithRef(ctx context.Context, epic *ingest.RawEpic, ref domain.CriterionRef, rc runContext) domain.ClassifiedEpic {
	status, progress := ingest.ProgressOf(epic, r.deps.StatusNames, rc.now)

	lens := domain.LensNone
	if c, ok := rc.rubric.Find(ref.Key); ok {
		lens = c.Lens
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

// fetchGitHubActivity fetches merged PRs for the team's repos in the last
// 90 days and attributes them to epics by Jira key (with epic-summary token
// fallback). Returns a map of epic key → Activity.
func (r *Runner) fetchGitHubActivity(ctx context.Context, team config.Team, epics []domain.ClassifiedEpic) map[string]domain.Activity {
	since := r.now().AddDate(0, 0, -90)
	refs := make([]github.EpicRef, 0, len(epics))
	for _, e := range epics {
		refs = append(refs, github.EpicRef{Key: e.Key, Summary: e.Summary})
	}
	attributed, err := r.deps.GitHub.FetchAttributedActivity(ctx, team.GitHubRepos, since, refs)
	if err != nil {
		slog.Warn("github activity fetch failed", "team", team.Name, "err", err)
		return nil
	}
	return attributed
}

// enrichActivity fetches GitHub activity for the team's repos and attributes
// it to individual epics by key matching (with summary-token fallback).
// Epics without matching GitHub activity get zero activity. No-op when
// GitHub is not configured, the team has no repos, or there are no epics.
func (r *Runner) enrichActivity(ctx context.Context, team config.Team, epics []domain.ClassifiedEpic) []domain.ClassifiedEpic {
	if !shouldEnrichGitHub(r.deps.GitHub, team, epics) {
		return epics
	}
	attributed := r.fetchGitHubActivity(ctx, team, epics)
	totalPRs := 0
	for i := range epics {
		if a, ok := attributed[epics[i].Key]; ok {
			epics[i].Activity = a
			totalPRs += a.PullRequests
		}
	}
	slog.Info("fetched github activity", "team", team.Name, "repos", len(team.GitHubRepos),
		"prs", totalPRs, "attributed", len(attributed))
	return epics
}

// shouldEnrichGitHub reports whether the GitHub activity stage should run:
// GitHub must be configured, the team must have repos, and there must be
// at least one epic to attribute activity to.
func shouldEnrichGitHub(gh GitHubFetcher, team config.Team, epics []domain.ClassifiedEpic) bool {
	return gh != nil && len(team.GitHubRepos) > 0 && len(epics) > 0
}
