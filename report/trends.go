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
		"sparkHeight": sparkHeight,
		"sub":         func(a, b int) int { return a - b },
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
		trends = append(trends, NewTeamTrend(team, points))
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
  .sparkline-row { display: flex; align-items: center; gap: 1rem; margin-bottom: .75rem; }
  .trend-team { background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius); margin-bottom: 1.5rem; overflow: hidden; }
  .trend-team h2 { font-size: 1.1rem; font-weight: 600; padding: 1rem 1.5rem; border-bottom: 1px solid var(--border); }
  .trend-body { padding: 1.25rem 1.5rem; }

  .sparkline-row { display: flex; align-items: center; gap: 1rem; margin-bottom: .75rem; }
  .sparkline-label { width: 120px; font-size: .82rem; color: var(--text-dim); }
  .sparkline-track { flex: 1; display: flex; align-items: flex-end; gap: 2px; height: 32px; }
  .sparkline-bar { flex: 1; min-width: 3px; border-radius: 2px 2px 0 0; transition: opacity .2s; }
  .sparkline-bar:hover { opacity: .7; }
  .bar-blue { background: var(--accent); }
  .bar-green { background: var(--green); }
  .bar-red { background: var(--red); }
  .bar-yellow { background: var(--yellow); }
  .sparkline-value { width: 40px; text-align: right; font-size: .82rem; font-weight: 600; }

  .trend-dates { display: flex; gap: 2px; margin-left: 120px; margin-top: .25rem; }
  .trend-date { flex: 1; min-width: 3px; text-align: center; font-size: .65rem; color: var(--text-dim); overflow: hidden; }
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
      {{if $team.HasData}}
      <div class="sparkline-row">
        <span class="sparkline-label">Epics tracked</span>
        <div class="sparkline-track">
          {{range $team.Points}}<div class="sparkline-bar bar-blue" style="height: {{sparkHeight .EpicCount nil $team.MaxEpics}}%" title="{{.Date}}: {{.EpicCount}} epics"></div>{{end}}
        </div>
        <span class="sparkline-value">{{(index $team.Points (sub (len $team.Points) 1)).EpicCount}}</span>
      </div>

      <div class="sparkline-row">
        <span class="sparkline-label">Blocker focus %</span>
        <div class="sparkline-track">
          {{range $team.Points}}<div class="sparkline-bar bar-green" style="height: {{.BlockerFocus}}%" title="{{.Date}}: {{.BlockerFocus}}%"></div>{{end}}
        </div>
        <span class="sparkline-value">{{(index $team.Points (sub (len $team.Points) 1)).BlockerFocus}}%</span>
      </div>

      <div class="sparkline-row">
        <span class="sparkline-label">Drift count</span>
        <div class="sparkline-track">
          {{range $team.Points}}<div class="sparkline-bar bar-red" style="height: {{sparkHeight .DriftCount nil 10}}%" title="{{.Date}}: {{.DriftCount}} drift"></div>{{end}}
        </div>
        <span class="sparkline-value">{{(index $team.Points (sub (len $team.Points) 1)).DriftCount}}</span>
      </div>

      <div class="sparkline-row">
        <span class="sparkline-label">Unmapped</span>
        <div class="sparkline-track">
          {{range $team.Points}}<div class="sparkline-bar bar-yellow" style="height: {{sparkHeight .UnmappedCount nil 10}}%" title="{{.Date}}: {{.UnmappedCount}} unmapped"></div>{{end}}
        </div>
        <span class="sparkline-value">{{(index $team.Points (sub (len $team.Points) 1)).UnmappedCount}}</span>
      </div>

      <div class="trend-dates">
        {{range $team.Points}}<div class="trend-date">{{.Date}}</div>{{end}}
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

// sparkHeight computes the bar height percentage for a sparkline value.
// It scales value against max, with a minimum of 2% so small values are visible.
func sparkHeight(value int, _ any, max int) int {
	if max <= 0 {
		max = 1
	}
	h := int(float64(value) / float64(max) * 100)
	if h > 100 {
		h = 100
	}
	if h < 2 && value > 0 {
		h = 2
	}
	return h
}
