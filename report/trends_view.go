package report

import (
	"time"

	"github.com/makarski/teamscope/domain"
)

// TeamTrend is the display model for one team's trend data.
type TeamTrend struct {
	Team   string
	Points []TrendPointView
}

// TrendPointView is a single point in a team's trend chart.
type TrendPointView struct {
	Date          string
	EpicCount     int
	BlockerFocus  int
	DriftCount    int
	UnmappedCount int
}

const trendDateLayout = "01-02"

// NewTeamTrend builds a display model from trend points.
func NewTeamTrend(team string, points []domain.TrendPoint) TeamTrend {
	views := make([]TrendPointView, 0, len(points))
	// Reverse to oldest-first for charting.
	for i := len(points) - 1; i >= 0; i-- {
		p := points[i]
		views = append(views, TrendPointView{
			Date:          p.TakenAt.Format(trendDateLayout),
			EpicCount:     p.EpicCount,
			BlockerFocus:  p.BlockerFocus,
			DriftCount:    p.DriftCount,
			UnmappedCount: p.UnmappedCount,
		})
	}
	return TeamTrend{Team: team, Points: views}
}

// MaxEpics returns the highest epic count across all points, for scaling the
// sparkline. Returns 1 if empty to avoid division by zero.
func (t TeamTrend) MaxEpics() int {
	max := 1
	for _, p := range t.Points {
		if p.EpicCount > max {
			max = p.EpicCount
		}
	}
	return max
}

// HasData reports whether the team has at least one trend point.
func (t TeamTrend) HasData() bool {
	return len(t.Points) > 0
}

// SnapshotCount returns the number of data points.
func (t TeamTrend) SnapshotCount() int {
	return len(t.Points)
}

// LastUpdated returns the date of the most recent snapshot, or empty string.
func (t TeamTrend) LastUpdated() string {
	if len(t.Points) == 0 {
		return ""
	}
	return t.Points[len(t.Points)-1].Date
}

// Unused but keeps time import for future use.
var _ = time.Now
