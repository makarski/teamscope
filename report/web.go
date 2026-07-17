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
	source   snapshotSource
	tmpl     *template.Template
	jiraBase string
}

// NewWebRenderer builds a renderer backed by the given snapshot source.
// jiraBaseURL is used to hyperlink ticket keys (may be empty).
func NewWebRenderer(source snapshotSource, jiraBaseURL string) (*WebRenderer, error) {
	tmpl, err := template.New("dashboard").Parse(dashboardTemplate)
	if err != nil {
		return nil, fmt.Errorf("report: parse template: %w", err)
	}
	return &WebRenderer{source: source, tmpl: tmpl, jiraBase: jiraBaseURL}, nil
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

	data := dashboardData{
		Teams:       views,
		JiraBaseURL: wr.jiraBase,
	}

	if err := wr.tmpl.Execute(w, data); err != nil {
		return fmt.Errorf("report: execute template: %w", err)
	}
	return nil
}

type dashboardData struct {
	Teams       []TeamView
	JiraBaseURL string
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
<style>` + dashboardCSS + `</style>
<script defer src="https://unpkg.com/alpinejs@3.x.x/dist/cdn.min.js"></script>
</head>
<body>
<div class="container">
  <div class="header">
    <h1>Team<span>scope</span></h1>
    <span class="meta">goal alignment &middot; drift &middot; focus</span>
  </div>

{{if not .Teams}}
  <div class="empty">No snapshots yet. Run <code>teamscope snapshot</code>.</div>
{{end}}

{{range .Teams}}
  <section class="team" x-data="{ open: true }">
    <div class="team-head" @click="open = !open">
      <h2>{{.Team}}</h2>
      <div class="badges">
        <span class="badge badge-blue">{{.EpicCount}} epics</span>
        <span class="badge badge-green">focus {{.BlockerFocus}}%</span>
        {{if .Drift}}<span class="badge badge-red">{{len .Drift}} drift</span>{{else}}<span class="badge badge-green">on track</span>{{end}}
        {{if .Unmapped}}<span class="badge badge-yellow">{{len .Unmapped}} unmapped</span>{{end}}
        <span style="color:var(--text-dim);font-size:1.2rem" x-show="!open">&#9660;</span>
        <span style="color:var(--text-dim);font-size:1.2rem" x-show="open" x-cloak>&#9650;</span>
      </div>
    </div>

    <div class="team-body" x-show="open" x-cloak>
      {{if .Narrative}}
      <div class="narrative">
        <div class="label">PO Brief</div>
        <p>{{.Narrative}}</p>
      </div>
      {{end}}

      {{if .States}}
      <div class="pillars">
        {{range .States}}
        <div class="pillar">
          <div class="pillar-head">
            <span class="pillar-title">{{.Title}}</span>
            {{if eq .Drift "optimistic"}}<span class="badge badge-yellow">optimistic</span>
            {{else if eq .Drift "stale"}}<span class="badge badge-purple">stale</span>
            {{else if eq .Status "done"}}<span class="badge badge-green">done</span>
            {{else}}<span class="badge badge-dim">open</span>{{end}}
          </div>
          <div class="pillar-stats">
            {{.DoneCount}} done / {{.OpenCount}} open
            {{if .Tickets}} &middot; {{len .Tickets}} tickets{{end}}
          </div>
          {{if .Tickets}}
          <div class="pillar-tickets">
            {{range .Tickets}}<a href="{{$.JiraBaseURL}}/browse/{{.Key}}">{{.Key}}</a> {{end}}
          </div>
          {{end}}
        </div>
        {{end}}
      </div>
      {{end}}

      {{if .Coverage}}
      <table>
        <thead><tr><th>Criterion</th><th>Status</th><th>Advancing</th><th>Coverage</th></tr></thead>
        <tbody>
          {{range .Coverage}}
          <tr>
            <td><strong>{{.Key}}</strong> {{.Title}}</td>
            <td class="st-{{.Status}}">{{if .Status}}{{.Status}}{{else}}&mdash;{{end}}</td>
            <td>{{.Advancing}} / {{.Total}}</td>
            <td>{{.Share}}%</td>
          </tr>
          {{end}}
        </tbody>
      </table>
      {{end}}

      <table>
        <thead><tr><th>Epic</th><th>Criterion</th><th>Advances</th><th>Status</th><th>Progress</th></tr></thead>
        <tbody>
          {{range .Epics}}
          <tr>
            <td class="epic-key"><a href="{{$.JiraBaseURL}}/browse/{{.Key}}">{{.Key}}</a> {{.Summary}}</td>
            <td>{{if .Criterion}}<span class="badge badge-dim">{{.Criterion}}</span>{{else}}&mdash;{{end}}</td>
            <td>{{if eq .Advances "advances"}}<span class="st-done">yes</span>{{else if eq .Advances "stalled"}}<span class="st-overdue">no</span>{{else}}&mdash;{{end}}</td>
            <td class="st-{{.Status}}">{{.Status}}</td>
            <td><span class="progress-bar"><span class="progress-fill" style="width: {{.Progress}}%"></span></span> {{.Progress}}%</td>
          </tr>
          {{end}}
        </tbody>
      </table>
    </div>
  </section>
{{end}}
</div>
</body>
</html>`
