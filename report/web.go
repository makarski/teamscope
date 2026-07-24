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
<script defer src="https://unpkg.com/alpinejs@3.14.1/dist/cdn.min.js"></script>
</head>
<body>
<div class="container">
  <div class="header">
    <h1>Team<span>scope</span></h1>
    <span class="meta">goal alignment &middot; drift &middot; focus</span>
  </div>

  <nav class="nav-tabs">
    <a href="/" class="active">Current</a>
    <a href="/trends">Trends</a>
  </nav>

{{if not .Teams}}
  <div class="empty">No snapshots yet. Run <code>teamscope snapshot</code>.</div>
{{end}}

{{range .Teams}}
  <section class="team" x-data="{ open: true }">
    <div class="team-head" role="button" tabindex="0" :aria-expanded="open" @click="open = !open" @keydown.enter="open = !open" @keydown.space.prevent="open = !open">
      <h2>{{.Team}}</h2>
      <div class="badges">
        <span class="badge badge-blue">{{.EpicCount}} epics</span>
        <span class="badge badge-green">focus {{.BlockerFocus}}%</span>
        {{if .Drift}}<span class="badge badge-red">{{len .Drift}} drift</span>{{else}}<span class="badge badge-green">on track</span>{{end}}
        {{if .Unmapped}}<span class="badge badge-yellow">{{len .Unmapped}} unmapped</span>{{end}}
        {{if .GitHubPRs}}<span class="badge badge-purple">{{.GitHubPRs}} PRs</span>{{end}}
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
        <div class="pillar{{if eq .Drift "optimistic"}} pillar-drift{{else if eq .Drift "stale"}} pillar-stale{{end}}">
          <div class="pillar-head">
            <span class="pillar-title">{{.Title}}</span>
            {{if eq .Drift "optimistic"}}<span class="badge badge-yellow">optimistic</span>
            {{else if eq .Drift "stale"}}<span class="badge badge-purple">stale</span>
            {{else if eq .Status "done"}}<span class="badge badge-green">done</span>
            {{else}}<span class="badge badge-dim">open</span>{{end}}
          </div>
          <div class="pillar-stats">
            {{if and (eq .DoneCount 0) (eq .OpenCount 0)}}
            {{.Advancing}} advancing / {{.Total}} epics
            {{else}}
            {{.DoneCount}} done / {{.OpenCount}} open
            {{end}}
            {{if .Tickets}} &middot; {{len .Tickets}} tickets{{end}}
            {{if .PRs}} &middot; {{.PRs}} PRs{{end}}
          </div>
          {{if .Tickets}}
          <div class="pillar-tickets">
            {{range .Tickets}}{{if $.JiraBaseURL}}<a href="{{$.JiraBaseURL}}/browse/{{.Key}}">{{.Key}}</a>{{else}}{{.Key}}{{end}} {{.Summary}}<br>{{end}}
          </div>
          {{end}}
        </div>
        {{end}}
      </div>
      {{end}}

      {{if .Unmapped}}
      <div class="unmapped-section" x-data="{ show: false }">
        <div class="unmapped-header" role="button" tabindex="0" :aria-expanded="show" @click="show = !show" @keydown.enter="show = !show" @keydown.space.prevent="show = !show">
          <span class="badge badge-yellow">{{len .Unmapped}} unmapped epics</span>
          <span class="unmapped-hint" x-show="!show">click to show</span>
          <span class="unmapped-hint" x-show="show" x-cloak>click to hide</span>
        </div>
        <template x-if="show">
          <div class="unmapped-list">
            {{range .Unmapped}}
            <div class="unmapped-item">
              {{if $.JiraBaseURL}}<a href="{{$.JiraBaseURL}}/browse/{{.Key}}">{{.Key}}</a>{{else}}{{.Key}}{{end}}
              {{.Summary}}
              <span class="st-{{.Status}}">{{.Status}}</span>
            </div>
            {{end}}
          </div>
        </template>
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
          {{range $i, $e := .Epics}}
          <tr x-data="{ tickets: false }">
            <td class="epic-key">
              {{if $.JiraBaseURL}}<a href="{{$.JiraBaseURL}}/browse/{{$e.Key}}">{{$e.Key}}</a>{{else}}{{$e.Key}}{{end}} {{$e.Summary}}
              {{if $e.Tickets}}<span class="badge badge-dim" style="cursor:pointer" @click="tickets = !tickets">{{len $e.Tickets}} tickets</span>{{end}}
            </td>
            <td>{{if $e.Criterion}}<span class="badge badge-dim">{{$e.Criterion}}</span>{{else}}&mdash;{{end}}</td>
            <td>{{if eq $e.Advances "advances"}}<span class="st-done">yes</span>{{else if eq $e.Advances "stalled"}}<span class="st-overdue">no</span>{{else}}&mdash;{{end}}</td>
            <td class="st-{{$e.Status}}">{{$e.Status}}</td>
            <td><span class="progress-bar"><span class="progress-fill" style="width: {{$e.Progress}}%"></span></span> {{$e.Progress}}%</td>
          </tr>
          {{if $e.Tickets}}
          <template x-if="tickets">
            <tr>
              <td colspan="5" style="padding:0">
                <table style="margin:0">
                  <tbody>
                    {{range $e.Tickets}}
                    <tr>
                      <td class="epic-key" style="padding-left:2rem">{{if $.JiraBaseURL}}<a href="{{$.JiraBaseURL}}/browse/{{.Key}}">{{.Key}}</a>{{else}}{{.Key}}{{end}} {{.Summary}}</td>
                      <td colspan="4" class="st-{{.Status}}">{{.Status}}</td>
                    </tr>
                    {{end}}
                  </tbody>
                </table>
              </td>
            </tr>
          </template>
          {{end}}
          {{end}}
        </tbody>
      </table>
    </div>
  </section>
{{end}}
</div>
</body>
</html>`
