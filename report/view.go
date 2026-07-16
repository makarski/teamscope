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
	Team         string
	Rubric       string
	TakenAt      string
	EpicCount    int
	Coverage     []CriterionCoverage
	Lenses       []LensShare
	Drift        []CriterionCoverage // criteria with no active epic advancing them
	Unmapped     []EpicView          // epics that mapped to no criterion
	OffTrack     []EpicView          // overdue or non-advancing active epics
	Epics        []EpicView
	BlockerFocus int // % of active epics working an open criterion
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
	Advances  bool
	AlignNote string
	Lens      domain.Lens
	Status    domain.ProgressStatus
	Progress  int // completion percentage
}

const timeDisplayLayout = "2006-01-02 15:04 MST"

// NewTeamView builds a display model from a snapshot.
func NewTeamView(snap domain.Snapshot) TeamView {
	epics := epicViews(snap.Epics)
	coverage := criterionCoverage(snap)

	return TeamView{
		Team:         snap.Team,
		Rubric:       snap.Rubric.Name,
		TakenAt:      snap.TakenAt.Format(timeDisplayLayout),
		EpicCount:    len(snap.Epics),
		Coverage:     coverage,
		Lenses:       lensShares(snap.Epics),
		Drift:        driftCriteria(coverage),
		Unmapped:     filterUnmapped(epics),
		OffTrack:     filterOffTrack(epics),
		Epics:        epics,
		BlockerFocus: blockerFocus(snap),
	}
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
		if e.Criterion.Advances {
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
		})
	}
	sort.SliceStable(views, func(i, j int) bool {
		return views[i].Key < views[j].Key
	})
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
