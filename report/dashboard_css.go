package report

// dashboardCSS is the handcrafted light-theme stylesheet for the dashboard.
const dashboardCSS = `
:root {
  --bg: #f8f9fa;
  --surface: #ffffff;
  --surface-hover: #f0f2f5;
  --border: #e4e7eb;
  --text: #1f2329;
  --text-dim: #8a9099;
  --text-muted: #b0b6bf;
  --accent: #4a7fff;
  --accent-light: rgba(74,127,255,.08);
  --green: #22c55e;
  --green-light: rgba(34,197,94,.08);
  --yellow: #f59e0b;
  --yellow-light: rgba(245,158,11,.08);
  --red: #ef4444;
  --red-light: rgba(239,68,68,.08);
  --purple: #8b5cf6;
  --purple-light: rgba(139,92,246,.08);
  --orange: #f97316;
  --shadow: 0 1px 3px rgba(0,0,0,.06), 0 1px 2px rgba(0,0,0,.04);
  --shadow-lg: 0 4px 12px rgba(0,0,0,.08);
  --radius: 10px;
}

* { box-sizing: border-box; margin: 0; padding: 0; }

body {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'Inter', system-ui, sans-serif;
  background: var(--bg);
  color: var(--text);
  line-height: 1.6;
  padding: 0;
  -webkit-font-smoothing: antialiased;
}

.container { max-width: 1200px; margin: 0 auto; padding: 2rem 1.5rem; }

/* Header */
.header {
  display: flex; justify-content: space-between; align-items: center;
  margin-bottom: 1.5rem;
}
.header h1 { font-size: 1.5rem; font-weight: 700; letter-spacing: -.02em; }
.header h1 span { color: var(--accent); }
.header .meta { font-size: .82rem; color: var(--text-dim); }

/* Nav tabs */
.nav-tabs { display: flex; gap: 0; margin-bottom: 1.5rem; border-bottom: 1px solid var(--border); }
.nav-tabs a { padding: .6rem 1.2rem; text-decoration: none; color: var(--text-dim); font-size: .88rem; font-weight: 600; border-bottom: 2px solid transparent; transition: all .15s; }
.nav-tabs a:hover { color: var(--text); }
.nav-tabs a.active { color: var(--accent); border-bottom-color: var(--accent); }

/* Executive summary */
.exec-summary {
  background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius);
  box-shadow: var(--shadow);
  padding: 1.5rem; margin-bottom: 1.5rem;
}
.exec-summary h2 { font-size: .82rem; color: var(--accent); margin-bottom: .5rem; text-transform: uppercase; letter-spacing: .05em; font-weight: 700; }
.exec-summary p { color: var(--text); font-size: .92rem; white-space: pre-wrap; line-height: 1.7; }

/* Team card */
.team {
  background: var(--surface); border: 1px solid var(--border);
  border-radius: var(--radius); box-shadow: var(--shadow);
  margin-bottom: 1.5rem; overflow: hidden;
}
.team-head {
  display: flex; justify-content: space-between; align-items: center;
  padding: 1.1rem 1.5rem; cursor: pointer; transition: background .15s;
}
.team-head:hover { background: var(--surface-hover); }
.team-head h2 { font-size: 1.1rem; font-weight: 650; letter-spacing: -.01em; }
.team-head .badges { display: flex; gap: .4rem; align-items: center; }
.team-body { padding: 0 1.5rem 1.5rem; }

/* Badges */
.badge {
  font-size: .7rem; font-weight: 600; padding: .2rem .55rem;
  border-radius: 999px; display: inline-flex; align-items: center; gap: .3rem;
}
.badge-green { background: var(--green-light); color: var(--green); }
.badge-yellow { background: var(--yellow-light); color: var(--yellow); }
.badge-red { background: var(--red-light); color: var(--red); }
.badge-blue { background: var(--accent-light); color: var(--accent); }
.badge-purple { background: var(--purple-light); color: var(--purple); }
.badge-dim { background: rgba(138,144,153,.1); color: var(--text-dim); }

/* Narrative panel */
.narrative {
  background: var(--accent-light); border-left: 3px solid var(--accent);
  border-radius: 0 8px 8px 0; padding: 1rem 1.25rem; margin-bottom: 1.25rem;
}
.narrative .label { font-size: .68rem; color: var(--accent); font-weight: 700; text-transform: uppercase; letter-spacing: .06em; margin-bottom: .35rem; }
.narrative p { font-size: .9rem; color: var(--text); white-space: pre-wrap; line-height: 1.65; }

/* Pillar grid */
.pillars { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: .75rem; margin-bottom: 1.5rem; }
.pillar {
  background: var(--bg); border: 1px solid var(--border); border-radius: 8px;
  padding: .85rem 1rem; transition: border-color .15s, box-shadow .15s;
}
.pillar:hover { border-color: var(--accent); box-shadow: var(--shadow); }
.pillar-drift { border-color: var(--yellow); background: var(--yellow-light); }
.pillar-stale { border-color: var(--purple); background: var(--purple-light); }
.pillar-drift:hover { border-color: var(--yellow); box-shadow: 0 0 0 2px var(--yellow-light); }
.pillar-stale:hover { border-color: var(--purple); box-shadow: 0 0 0 2px var(--purple-light); }
.pillar-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: .4rem; }
.pillar-title { font-size: .86rem; font-weight: 600; }
.pillar-stats { font-size: .76rem; color: var(--text-dim); }
.pillar-tickets { font-size: .76rem; margin-top: .35rem; line-height: 1.5; }
.pillar-tickets a { color: var(--accent); text-decoration: none; }
.pillar-tickets a:hover { text-decoration: underline; }

/* Tables */
table { width: 100%; border-collapse: collapse; font-size: .84rem; margin-bottom: 1rem; }
th { text-align: left; padding: .5rem; color: var(--text-dim); font-weight: 600; border-bottom: 2px solid var(--border); font-size: .76rem; text-transform: uppercase; letter-spacing: .03em; }
td { padding: .5rem; border-bottom: 1px solid var(--border); }
tr:hover td { background: var(--surface-hover); }
tr:last-child td { border-bottom: none; }

/* Epic link */
.epic-key a { color: var(--accent); text-decoration: none; font-weight: 600; }
.epic-key a:hover { text-decoration: underline; }

/* Progress bar */
.progress-bar { display: inline-block; width: 80px; height: 6px; background: var(--border); border-radius: 3px; overflow: hidden; vertical-align: middle; }
.progress-fill { height: 100%; background: var(--accent); border-radius: 3px; transition: width .3s; }

/* Status colors */
.st-done { color: var(--green); }
.st-ongoing { color: var(--yellow); }
.st-overdue { color: var(--red); }
.st-todo { color: var(--text-muted); }

/* Drift */
.drift-optimistic { color: var(--orange); }
.drift-stale { color: var(--purple); }

/* Collapsed state */
[x-cloak] { display: none; }

/* Empty state */
.empty { text-align: center; padding: 3rem; color: var(--text-dim); }
.empty code { background: var(--surface); padding: .2rem .4rem; border-radius: 4px; font-size: .85rem; border: 1px solid var(--border); }

/* Unmapped section */
.unmapped-section { margin-bottom: 1rem; }
.unmapped-header { display: flex; align-items: center; gap: .5rem; cursor: pointer; padding: .5rem 0; }
.unmapped-hint { font-size: .76rem; color: var(--text-dim); }
.unmapped-list { background: var(--bg); border: 1px solid var(--border); border-radius: 8px; padding: .5rem .75rem; margin-top: .25rem; }
.unmapped-item { font-size: .82rem; padding: .3rem 0; border-bottom: 1px solid var(--border); }
.unmapped-item:last-child { border-bottom: none; }
.unmapped-item a { color: var(--accent); text-decoration: none; font-weight: 600; }
.unmapped-item a:hover { text-decoration: underline; }

/* SVG charts */
.chart-container { margin-bottom: 1rem; }
.chart-label { font-size: .78rem; color: var(--text-dim); font-weight: 600; margin-bottom: .25rem; }
.chart-svg { width: 100%; height: 60px; display: block; }
.chart-value { font-size: .82rem; font-weight: 700; color: var(--text); }
.chart-row { display: flex; align-items: center; gap: .75rem; margin-bottom: .5rem; }
.chart-row-label { width: 110px; font-size: .78rem; color: var(--text-dim); flex-shrink: 0; }
.chart-row-body { flex: 1; }
.chart-row-value { width: 50px; text-align: right; font-size: .82rem; font-weight: 700; flex-shrink: 0; }
.chart-dates { display: flex; justify-content: space-between; margin-left: 110px; margin-top: .15rem; font-size: .65rem; color: var(--text-muted); }
`
