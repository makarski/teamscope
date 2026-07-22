// Package report turns stored snapshots into presentation view models and
// renders them to Slack and a static web page.
package report

import (
	"math"
	"sort"

	"github.com/makarski/teamscope/domain"
)

// TeamView is the display model for a single team's latest snapshot.
type TeamView struct {
	Team          string
	Rubric        string
	TakenAt       string
	EpicCount     int
	Coverage      []CriterionCoverage
	Lenses        []LensShare
	Drift         []CriterionCoverage // criteria with no active epic advancing them
	Unmapped      []EpicView          // epics that mapped to no criterion
	OffTrack      []EpicView          // overdue epics needing attention
	Epics         []EpicView
	BlockerFocus  int // % of active epics working an open criterion
	States        []PillarStateView
	Narrative     string
	GitHubPRs     int
	GitHubCommits int
}

// PillarStateView is the display model for a criterion's drift state.
type PillarStateView struct {
	Key       string
	Title     string
	Status    domain.Status
	Drift     domain.Drift
	DoneCount int
	OpenCount int
	Tickets   []TicketView
	Advancing int // epics that advance this criterion
	Total     int // epics mapped to this criterion
	PRs       int // GitHub PRs attributed to this criterion's epics
	Commits   int // GitHub commits attributed to this criterion's epics
}

// TicketView is a Jira ticket with its live status. Used for both pillar
// drift tickets and epic child tickets.
type TicketView struct {
	Key     string
	Summary string
	Status  domain.ProgressStatus
}

// CriterionCoverage reports how much active work advances one criterion.
type CriterionCoverage struct {
	Key       string
	Title     string
	Status    domain.Status
	Lens      domain.Lens
	Advancing int // epics that advance this criterion
	Total     int // epics mapped to this criterion
	Share     int // % of all epics mapped here
	PRs       int // GitHub PRs attributed to this criterion's epics
	Commits   int // GitHub commits attributed to this criterion's epics
}

// LensShare is the share of epics viewed through one lens.
type LensShare struct {
	Lens    domain.Lens
	Percent int
}

// EpicView is a single epic row.
type EpicView struct {
	Key       string
	Summary   string
	Criterion string
	Advances  domain.Advancement
	AlignNote string
	Lens      domain.Lens
	Status    domain.ProgressStatus
	Progress  int // completion percentage
	Tickets   []TicketView
}

// TicketView is a child ticket of an epic — see TicketView above.

const timeDisplayLayout = "2006-01-02 15:04 MST"

// NewTeamView builds a display model from a snapshot.
func NewTeamView(snap domain.Snapshot) TeamView {
	epics := epicViews(snap.Epics)
	coverage := criterionCoverage(snap)

	return TeamView{
		Team:          snap.Team,
		Rubric:        snap.Rubric.Name,
		TakenAt:       snap.TakenAt.Format(timeDisplayLayout),
		EpicCount:     len(snap.Epics),
		Coverage:      coverage,
		Lenses:        lensShares(snap.Epics),
		Drift:         driftCriteria(coverage),
		Unmapped:      filterUnmapped(epics),
		OffTrack:      filterOffTrack(epics),
		Epics:         epics,
		BlockerFocus:  blockerFocus(snap),
		States:        pillarStates(snap.States, coverage),
		Narrative:     snap.Narrative,
		GitHubPRs:     teamPRs(snap.Epics),
		GitHubCommits: teamCommits(snap.Epics),
	}
}

// pillarStates converts domain CriterionStates to display views, enriching
// them with coverage data (advancing/total epics) and GitHub activity from
// the criterion coverage.
func pillarStates(states []domain.CriterionState, coverage []CriterionCoverage) []PillarStateView {
	covByKey := map[string]CriterionCoverage{}
	for _, c := range coverage {
		covByKey[c.Key] = c
	}

	views := make([]PillarStateView, 0, len(states))
	for _, s := range states {
		tickets := make([]TicketView, 0, len(s.LinkedKeys))
		for _, t := range s.LinkedKeys {
			tickets = append(tickets, TicketView{
				Key:    t.Key,
				Status: t.Status,
			})
		}
		cov := covByKey[s.Criterion.Key]
		views = append(views, PillarStateView{
			Key:       s.Criterion.Key,
			Title:     s.Criterion.Title,
			Status:    s.Criterion.Status,
			Drift:     s.Drift,
			DoneCount: s.DoneCount,
			OpenCount: s.OpenCount,
			Tickets:   tickets,
			Advancing: cov.Advancing,
			Total:     cov.Total,
			PRs:       cov.PRs,
			Commits:   cov.Commits,
		})
	}
	return views
}

// criterionCoverage aggregates epics onto every rubric criterion, including
// criteria with zero epics so drift is visible.
func criterionCoverage(snap domain.Snapshot) []CriterionCoverage {
	total := len(snap.Epics)
	byKey := map[string]*CriterionCoverage{}
	order := make([]string, 0, len(snap.Rubric.Criteria))

	for _, c := range snap.Rubric.Criteria {
		byKey[c.Key] = &CriterionCoverage{Key: c.Key, Title: c.Title, Status: c.Status, Lens: c.Lens}
		order = append(order, c.Key)
	}

	for _, e := range snap.Epics {
		cov, ok := byKey[e.Criterion.Key]
		if !ok {
			continue // unmapped or stale key; surfaced separately
		}
		cov.Total++
		cov.PRs += e.Activity.PullRequests
		cov.Commits += e.Activity.Commits
		if e.Criterion.Advances == domain.AdvAdvances {
			cov.Advancing++
		}
	}

	out := make([]CriterionCoverage, 0, len(order))
	for _, key := range order {
		cov := byKey[key]
		cov.Share = pct(cov.Total, total)
		out = append(out, *cov)
	}
	return out
}

func lensShares(epics []domain.ClassifiedEpic) []LensShare {
	counts := map[domain.Lens]int{}
	for _, e := range epics {
		counts[e.Lens]++
	}
	total := len(epics)

	lenses := make([]LensShare, 0, len(counts))
	for _, l := range []domain.Lens{domain.LensProduct, domain.LensBusiness, domain.LensOperations, domain.LensNone} {
		if counts[l] == 0 {
			continue
		}
		lenses = append(lenses, LensShare{Lens: l, Percent: pct(counts[l], total)})
	}
	return lenses
}

// driftCriteria are open criteria that no active epic advances.
func driftCriteria(coverage []CriterionCoverage) []CriterionCoverage {
	var out []CriterionCoverage
	for _, c := range coverage {
		if c.Status != domain.CriterionDone && c.Advancing == 0 {
			out = append(out, c)
		}
	}
	return out
}

// blockerFocus is the share of epics whose criterion is still open (i.e. work
// aimed at unfinished goals rather than already-done ones).
func blockerFocus(snap domain.Snapshot) int {
	statusByKey := map[string]domain.Status{}
	for _, c := range snap.Rubric.Criteria {
		statusByKey[c.Key] = c.Status
	}

	onOpen := 0
	for _, e := range snap.Epics {
		if e.Criterion.Key != "" && statusByKey[e.Criterion.Key] != domain.CriterionDone {
			onOpen++
		}
	}
	return pct(onOpen, len(snap.Epics))
}

func epicViews(epics []domain.ClassifiedEpic) []EpicView {
	views := make([]EpicView, 0, len(epics))
	for _, e := range epics {
		views = append(views, EpicView{
			Key:       e.Key,
			Summary:   e.Summary,
			Criterion: e.Criterion.Key,
			Advances:  e.Criterion.Advances,
			AlignNote: e.Criterion.Note,
			Lens:      e.Lens,
			Status:    e.Status,
			Progress:  int(math.Round(e.Progress * 100)),
			Tickets:   ticketViews(e.Tickets),
		})
	}
	sort.SliceStable(views, func(i, j int) bool {
		return views[i].Key < views[j].Key
	})
	return views
}

func ticketViews(tickets []domain.EpicTicket) []TicketView {
	if len(tickets) == 0 {
		return nil
	}
	views := make([]TicketView, 0, len(tickets))
	for _, t := range tickets {
		views = append(views, TicketView{
			Key:     t.Key,
			Summary: t.Summary,
			Status:  t.Status,
		})
	}
	return views
}

func filterUnmapped(epics []EpicView) []EpicView {
	var out []EpicView
	for _, e := range epics {
		if e.Criterion == "" {
			out = append(out, e)
		}
	}
	return out
}

func filterOffTrack(epics []EpicView) []EpicView {
	var out []EpicView
	for _, e := range epics {
		if e.Status == domain.StatusOverdue {
			out = append(out, e)
		}
	}
	return out
}

func pct(n, total int) int {
	if total == 0 {
		return 0
	}
	return int(math.Round(float64(n) / float64(total) * 100))
}

// teamPRs sums GitHub PRs across all epics.
func teamPRs(epics []domain.ClassifiedEpic) int {
	total := 0
	for _, e := range epics {
		total += e.Activity.PullRequests
	}
	return total
}

// teamCommits sums GitHub commits across all epics.
func teamCommits(epics []domain.ClassifiedEpic) int {
	total := 0
	for _, e := range epics {
		total += e.Activity.Commits
	}
	return total
}
