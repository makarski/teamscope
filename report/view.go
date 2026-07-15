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
	Team      string
	TakenAt   string
	EpicCount int
	Mix       []MixSlice
	Alignment AlignmentBreakdown
	OffTrack  []EpicView
	Epics     []EpicView
}

// MixSlice is one work-type share, as a rounded percentage.
type MixSlice struct {
	WorkType domain.WorkType
	Percent  int
}

// AlignmentBreakdown counts epics per alignment verdict.
type AlignmentBreakdown struct {
	Aligned  int
	Partial  int
	OffTrack int
	Unknown  int
}

// EpicView is a single epic row.
type EpicView struct {
	Key       string
	Summary   string
	WorkType  domain.WorkType
	Alignment domain.Alignment
	AlignNote string
	Status    domain.ProgressStatus
	Progress  int // completion percentage
}

const timeDisplayLayout = "2006-01-02 15:04 MST"

// NewTeamView builds a display model from a snapshot.
func NewTeamView(snap domain.Snapshot) TeamView {
	epics := epicViews(snap.Epics)
	return TeamView{
		Team:      snap.Team,
		TakenAt:   snap.TakenAt.Format(timeDisplayLayout),
		EpicCount: len(snap.Epics),
		Mix:       mixSlices(snap.Mix()),
		Alignment: alignmentBreakdown(snap.Epics),
		OffTrack:  filterOffTrack(epics),
		Epics:     epics,
	}
}

func mixSlices(mix map[domain.WorkType]float64) []MixSlice {
	slices := make([]MixSlice, 0, len(mix))
	for _, wt := range domain.AllWorkTypes() {
		slices = append(slices, MixSlice{
			WorkType: wt,
			Percent:  int(math.Round(mix[wt] * 100)),
		})
	}
	return slices
}

func alignmentBreakdown(epics []domain.ClassifiedEpic) AlignmentBreakdown {
	var b AlignmentBreakdown
	for _, e := range epics {
		switch e.Alignment {
		case domain.AlignAligned:
			b.Aligned++
		case domain.AlignPartial:
			b.Partial++
		case domain.AlignOffTrack:
			b.OffTrack++
		default:
			b.Unknown++
		}
	}
	return b
}

func epicViews(epics []domain.ClassifiedEpic) []EpicView {
	views := make([]EpicView, 0, len(epics))
	for _, e := range epics {
		views = append(views, EpicView{
			Key:       e.Key,
			Summary:   e.Summary,
			WorkType:  e.WorkType,
			Alignment: e.Alignment,
			AlignNote: e.AlignNote,
			Status:    e.Status,
			Progress:  int(math.Round(e.Progress * 100)),
		})
	}
	sort.SliceStable(views, func(i, j int) bool {
		return views[i].Key < views[j].Key
	})
	return views
}

func filterOffTrack(epics []EpicView) []EpicView {
	var out []EpicView
	for _, e := range epics {
		if e.Alignment == domain.AlignOffTrack || e.Status == domain.StatusOverdue {
			out = append(out, e)
		}
	}
	return out
}
