package report

import (
	"html/template"

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

// NewTeamTrend builds a display model from trend points. Points are expected
// to be in oldest-first order (as returned by the store).
func NewTeamTrend(team string, points []domain.TrendPoint) TeamTrend {
	views := make([]TrendPointView, 0, len(points))
	for _, p := range points {
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

// FirstDate returns the date of the oldest snapshot, or empty string.
func (t TeamTrend) FirstDate() string {
	if len(t.Points) == 0 {
		return ""
	}
	return t.Points[0].Date
}

// LastDate returns the date of the most recent snapshot, or empty string.
func (t TeamTrend) LastDate() string {
	return t.LastUpdated()
}

// EpicChart returns an SVG filled chart for epic count over time.
func (t TeamTrend) EpicChart() template.HTML {
	return template.HTML(t.chart(func(p TrendPointView) float64 { return float64(p.EpicCount) }, "#4a7fff"))
}

// FocusChart returns an SVG filled chart for blocker focus % over time.
func (t TeamTrend) FocusChart() template.HTML {
	return template.HTML(t.chart(func(p TrendPointView) float64 { return float64(p.BlockerFocus) }, "#22c55e"))
}

// DriftChart returns an SVG filled chart for drift count over time.
func (t TeamTrend) DriftChart() template.HTML {
	return template.HTML(t.chart(func(p TrendPointView) float64 { return float64(p.DriftCount) }, "#ef4444"))
}

// UnmappedChart returns an SVG filled chart for unmapped count over time.
func (t TeamTrend) UnmappedChart() template.HTML {
	return template.HTML(t.chart(func(p TrendPointView) float64 { return float64(p.UnmappedCount) }, "#f59e0b"))
}

func (t TeamTrend) chart(valueFn func(TrendPointView) float64, color string) string {
	vals := make([]float64, len(t.Points))
	for i, p := range t.Points {
		vals[i] = valueFn(p)
	}
	return svgFilledChart([]chartSeries{{values: vals, color: color}}, 300, 60)
}
