package report

import (
	"context"
	"fmt"
	"html/template"
	"io"

	"github.com/makarski/teamscope/domain"
)

// trendSource supplies the data the trends page renders.
type trendSource interface {
	Teams(ctx context.Context) ([]string, error)
	TrendMetrics(ctx context.Context, team string, n int) ([]domain.TrendPoint, error)
	Latest(ctx context.Context, team string) (domain.Snapshot, error)
}

// TrendRenderer renders the trends dashboard as static HTML.
type TrendRenderer struct {
	source   trendSource
	tmpl     *template.Template
	jiraBase string
}

// NewTrendRenderer builds a renderer backed by the given trend source.
func NewTrendRenderer(source trendSource, jiraBaseURL string) (*TrendRenderer, error) {
	tmpl, err := template.New("trends").Funcs(template.FuncMap{
		"sub": func(a, b int) int { return a - b },
	}).Parse(trendsTemplate)
	if err != nil {
		return nil, fmt.Errorf("report: parse trends template: %w", err)
	}
	return &TrendRenderer{source: source, tmpl: tmpl, jiraBase: jiraBaseURL}, nil
}

// Render writes the trends dashboard for all teams to w.
func (tr *TrendRenderer) Render(ctx context.Context, w io.Writer) error {
	teams, err := tr.source.Teams(ctx)
	if err != nil {
		return err
	}

	trends := make([]TeamTrend, 0, len(teams))
	for _, team := range teams {
		points, err := tr.source.TrendMetrics(ctx, team, 30)
		if err != nil {
			return fmt.Errorf("report: load trend for %q: %w", team, err)
		}
		trend := NewTeamTrend(team, points)
		// Load the latest snapshot's narrative for the trend brief.
		if snap, err := tr.source.Latest(ctx, team); err == nil {
			trend.Narrative = snap.Narrative
		}
		trends = append(trends, trend)
	}

	data := trendPageData{
		Trends:      trends,
		JiraBaseURL: tr.jiraBase,
	}

	if err := tr.tmpl.Execute(w, data); err != nil {
		return fmt.Errorf("report: execute trends template: %w", err)
	}
	return nil
}

type trendPageData struct {
	Trends      []TeamTrend
	JiraBaseURL string
}

const trendsTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Teamscope — Trends</title>
<style>` + dashboardCSS + `
  .trend-team { background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius); box-shadow: var(--shadow); margin-bottom: 1.5rem; overflow: hidden; }
  .trend-team h2 { font-size: 1.1rem; font-weight: 650; padding: 1.1rem 1.5rem; border-bottom: 1px solid var(--border); }
  .trend-body { padding: 1.25rem 1.5rem; }
</style>
</head>
<body>
<div class="container">
  <div class="header">
    <h1>Team<span>scope</span></h1>
    <span class="meta">trends &middot; progress over time</span>
  </div>

  <nav class="nav-tabs">
    <a href="/">Current</a>
    <a href="/trends" class="active">Trends</a>
  </nav>

{{if not .Trends}}
  <div class="empty">No snapshots yet. Run <code>teamscope snapshot</code>.</div>
{{end}}

{{range $team := .Trends}}
  <section class="trend-team">
    <h2>{{$team.Team}} <span class="badge badge-dim" style="margin-left:.5rem">{{$team.SnapshotCount}} snapshots</span></h2>
    <div class="trend-body">
      {{if $team.Narrative}}
      <div class="narrative">
        <div class="label">PO Trend Brief</div>
        <p>{{$team.Narrative}}</p>
      </div>
      {{end}}
      {{if $team.HasData}}
      <div class="chart-row">
        <span class="chart-row-label">Epics tracked</span>
        <div class="chart-row-body">{{$team.EpicChart}}</div>
        <span class="chart-row-value">{{(index $team.Points (sub (len $team.Points) 1)).EpicCount}}</span>
      </div>

      <div class="chart-row">
        <span class="chart-row-label">Blocker focus</span>
        <div class="chart-row-body">{{$team.FocusChart}}</div>
        <span class="chart-row-value">{{(index $team.Points (sub (len $team.Points) 1)).BlockerFocus}}%</span>
      </div>

      <div class="chart-row">
        <span class="chart-row-label">Drift count</span>
        <div class="chart-row-body">{{$team.DriftChart}}</div>
        <span class="chart-row-value">{{(index $team.Points (sub (len $team.Points) 1)).DriftCount}}</span>
      </div>

      <div class="chart-row">
        <span class="chart-row-label">Unmapped</span>
        <div class="chart-row-body">{{$team.UnmappedChart}}</div>
        <span class="chart-row-value">{{(index $team.Points (sub (len $team.Points) 1)).UnmappedCount}}</span>
      </div>

      <div class="chart-dates">
        <span>{{$team.FirstDate}}</span>
        <span>{{$team.LastDate}}</span>
      </div>
      {{else}}
      <p class="empty">No trend data yet.</p>
      {{end}}
    </div>
  </section>
{{end}}
</div>
</body>
</html>`
