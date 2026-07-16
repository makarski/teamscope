package report

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"io"

	"github.com/makarski/teamscope/domain"
)

// snapshotSource supplies the data the web page renders.
type snapshotSource interface {
	Teams(ctx context.Context) ([]string, error)
	Latest(ctx context.Context, team string) (domain.Snapshot, error)
}

// WebRenderer renders the at-a-glance dashboard as static HTML.
type WebRenderer struct {
	source snapshotSource
	tmpl   *template.Template
}

// NewWebRenderer builds a renderer backed by the given snapshot source.
func NewWebRenderer(source snapshotSource) (*WebRenderer, error) {
	tmpl, err := template.New("dashboard").Parse(dashboardTemplate)
	if err != nil {
		return nil, fmt.Errorf("report: parse template: %w", err)
	}
	return &WebRenderer{source: source, tmpl: tmpl}, nil
}

// Render writes the dashboard for all teams with snapshots to w.
func (wr *WebRenderer) Render(ctx context.Context, w io.Writer) error {
	teams, err := wr.source.Teams(ctx)
	if err != nil {
		return err
	}

	views, err := wr.collectViews(ctx, teams)
	if err != nil {
		return err
	}

	if err := wr.tmpl.Execute(w, views); err != nil {
		return fmt.Errorf("report: execute template: %w", err)
	}
	return nil
}

func (wr *WebRenderer) collectViews(ctx context.Context, teams []string) ([]TeamView, error) {
	views := make([]TeamView, 0, len(teams))
	for _, team := range teams {
		snap, err := wr.source.Latest(ctx, team)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("report: load latest for %q: %w", team, err)
		}
		views = append(views, NewTeamView(snap))
	}
	return views, nil
}

const dashboardTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Teamscope</title>
<style>
  :root { color-scheme: light dark; }
  body { font-family: system-ui, sans-serif; margin: 0; padding: 2rem; background: #0f1117; color: #e6e8ee; }
  h1 { margin: 0 0 1.5rem; font-size: 1.4rem; }
  .team { background: #171a23; border: 1px solid #262b38; border-radius: 12px; padding: 1.25rem 1.5rem; margin-bottom: 1.5rem; }
  .team-head { display: flex; justify-content: space-between; align-items: baseline; margin-bottom: .75rem; }
  .team-head h2 { margin: 0; font-size: 1.1rem; }
  .meta { font-size: .8rem; color: #8a90a2; }
  .bar { display: flex; height: 14px; border-radius: 7px; overflow: hidden; margin: .5rem 0 1rem; background: #262b38; }
  .seg-advancing { background: #4f8cff; }
  .seg-mapped { background: #f0a83c; }
  .align { display: flex; gap: 1rem; font-size: .82rem; margin-bottom: 1rem; flex-wrap: wrap; }
  .pill { padding: .15rem .55rem; border-radius: 999px; background: #262b38; }
  .pill.off { background: #5a1f23; color: #ffb4ba; }
  .pill.ok { background: #1f3a24; color: #7ee787; }
  .cov { width: 100%; border-collapse: collapse; font-size: .85rem; margin-bottom: 1rem; }
  .cov td, .cov th { padding: .35rem .5rem; border-bottom: 1px solid #262b38; text-align: left; }
  .cov .minibar { display: inline-block; height: 8px; border-radius: 4px; background: #4f8cff; vertical-align: middle; }
  .cov .track { display: inline-block; width: 90px; height: 8px; border-radius: 4px; background: #262b38; vertical-align: middle; margin-right: .5rem; }
  .st-done { color: #7ee787; }
  .st-open { color: #ffcf7a; }
  table { width: 100%; border-collapse: collapse; font-size: .85rem; }
  th, td { text-align: left; padding: .45rem .5rem; border-bottom: 1px solid #262b38; }
  th { color: #8a90a2; font-weight: 600; }
  .tag { font-size: .72rem; padding: .1rem .45rem; border-radius: 5px; background: #262b38; }
  .status-overdue { color: #ffb4ba; }
  .status-done { color: #7ee787; }
  .off-track { margin-bottom: 1rem; }
  .off-track h3 { font-size: .85rem; color: #ffb4ba; margin: 0 0 .4rem; }
  .off-track li { font-size: .82rem; margin-bottom: .2rem; }
  .empty { color: #8a90a2; }
</style>
</head>
<body>
<h1>Teamscope &mdash; goal alignment &amp; focus</h1>
{{if not .}}<p class="empty">No snapshots yet. Run <code>teamscope snapshot</code>.</p>{{end}}
{{range .}}
<section class="team">
  <div class="team-head">
    <h2>{{.Team}}</h2>
    <span class="meta">rubric: {{.Rubric}} &middot; {{.TakenAt}} &middot; {{.EpicCount}} epics</span>
  </div>

  <div class="align">
    <span class="pill ok">blocker focus {{.BlockerFocus}}%</span>
    {{if .Drift}}<span class="pill off">drift: {{len .Drift}} uncovered</span>{{else}}<span class="pill ok">no drift</span>{{end}}
    {{if .Unmapped}}<span class="pill off">unmapped {{len .Unmapped}}</span>{{end}}
    {{range .Lenses}}<span class="pill">{{if .Lens}}{{.Lens}}{{else}}unlensed{{end}} {{.Percent}}%</span>{{end}}
  </div>

  <table class="cov">
    <thead><tr><th>Criterion</th><th>Status</th><th>Advancing</th><th>Coverage</th></tr></thead>
    <tbody>
      {{range .Coverage}}
      <tr>
        <td><strong>{{.Key}}</strong> {{.Title}}</td>
        <td class="st-{{.Status}}">{{if .Status}}{{.Status}}{{else}}&mdash;{{end}}</td>
        <td>{{.Advancing}} / {{.Total}}</td>
        <td><span class="track"><span class="minibar" style="width: {{.Share}}%"></span></span>{{.Share}}%</td>
      </tr>
      {{end}}
      {{if not .Coverage}}<tr><td colspan="4" class="empty">No rubric criteria resolved.</td></tr>{{end}}
    </tbody>
  </table>

  {{if .Drift}}
  <div class="off-track">
    <h3>Drift &mdash; open goals nobody is advancing</h3>
    <ul>
      {{range .Drift}}<li><strong>{{.Key}}</strong> {{.Title}}</li>{{end}}
    </ul>
  </div>
  {{end}}

  {{if .Unmapped}}
  <div class="off-track">
    <h3>Unmapped epics &mdash; work serving no declared goal</h3>
    <ul>
      {{range .Unmapped}}<li><strong>{{.Key}}</strong> {{.Summary}}</li>{{end}}
    </ul>
  </div>
  {{end}}

  <table>
    <thead><tr><th>Epic</th><th>Criterion</th><th>Advances</th><th>Status</th><th>Progress</th></tr></thead>
    <tbody>
      {{range .Epics}}
      <tr>
        <td><strong>{{.Key}}</strong> {{.Summary}}</td>
        <td>{{if .Criterion}}<span class="tag">{{.Criterion}}</span>{{else}}&mdash;{{end}}</td>
        <td>{{if .Advances}}<span class="st-done">yes</span>{{else}}&mdash;{{end}}</td>
        <td class="status-{{.Status}}">{{.Status}}</td>
        <td>{{.Progress}}%</td>
      </tr>
      {{end}}
    </tbody>
  </table>
</section>
{{end}}
</body>
</html>`
