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
  .bar { display: flex; height: 14px; border-radius: 7px; overflow: hidden; margin: .5rem 0 1rem; }
  .seg-business { background: #4f8cff; }
  .seg-chore { background: #f0a83c; }
  .seg-rnd { background: #8b5cf6; }
  .legend { display: flex; gap: 1.25rem; font-size: .8rem; margin-bottom: 1rem; flex-wrap: wrap; }
  .legend span::before { content: ""; display: inline-block; width: 10px; height: 10px; border-radius: 3px; margin-right: .4rem; vertical-align: middle; }
  .lg-business::before { background: #4f8cff; }
  .lg-chore::before { background: #f0a83c; }
  .lg-rnd::before { background: #8b5cf6; }
  .align { display: flex; gap: 1rem; font-size: .82rem; margin-bottom: 1rem; }
  .pill { padding: .15rem .55rem; border-radius: 999px; background: #262b38; }
  .pill.off { background: #5a1f23; color: #ffb4ba; }
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
<h1>Teamscope &mdash; goal alignment &amp; work mix</h1>
{{if not .}}<p class="empty">No snapshots yet. Run <code>teamscope snapshot</code>.</p>{{end}}
{{range .}}
<section class="team">
  <div class="team-head">
    <h2>{{.Team}}</h2>
    <span class="meta">{{.TakenAt}} &middot; {{.EpicCount}} epics</span>
  </div>

  <div class="bar">
    {{range .Mix}}<div class="seg-{{.WorkType}}" style="width: {{.Percent}}%" title="{{.WorkType}} {{.Percent}}%"></div>{{end}}
  </div>
  <div class="legend">
    {{range .Mix}}<span class="lg-{{.WorkType}}">{{.WorkType}} {{.Percent}}%</span>{{end}}
  </div>

  <div class="align">
    <span class="pill">aligned {{.Alignment.Aligned}}</span>
    <span class="pill">partial {{.Alignment.Partial}}</span>
    <span class="pill off">off-track {{.Alignment.OffTrack}}</span>
    <span class="pill">unscored {{.Alignment.Unknown}}</span>
  </div>

  {{if .OffTrack}}
  <div class="off-track">
    <h3>Needs attention</h3>
    <ul>
      {{range .OffTrack}}<li><strong>{{.Key}}</strong> {{.Summary}} &mdash; {{if .AlignNote}}{{.AlignNote}}{{else}}{{.Status}}{{end}}</li>{{end}}
    </ul>
  </div>
  {{end}}

  <table>
    <thead><tr><th>Epic</th><th>Type</th><th>Alignment</th><th>Status</th><th>Progress</th></tr></thead>
    <tbody>
      {{range .Epics}}
      <tr>
        <td><strong>{{.Key}}</strong> {{.Summary}}</td>
        <td><span class="tag">{{.WorkType}}</span></td>
        <td>{{if .Alignment}}{{.Alignment}}{{else}}&mdash;{{end}}</td>
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
