package report

// dashboardCSS is the handcrafted dark-theme stylesheet for the dashboard.
const dashboardCSS = `
:root {
  --bg: #0d1117;
  --surface: #161b22;
  --surface-hover: #1c2330;
  --border: #30363d;
  --text: #e6edf3;
  --text-dim: #8b949e;
  --accent: #58a6ff;
  --green: #3fb950;
  --yellow: #d29922;
  --red: #f85149;
  --purple: #bc8cff;
  --orange: #db6d28;
  --radius: 10px;
}

* { box-sizing: border-box; margin: 0; padding: 0; }

body {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif;
  background: var(--bg);
  color: var(--text);
  line-height: 1.6;
  padding: 0;
}

.container { max-width: 1200px; margin: 0 auto; padding: 2rem 1.5rem; }

/* Header */
.header {
  display: flex; justify-content: space-between; align-items: center;
  margin-bottom: 2rem; padding-bottom: 1rem; border-bottom: 1px solid var(--border);
}
.header h1 { font-size: 1.5rem; font-weight: 700; }
.header h1 span { color: var(--accent); }
.header .meta { font-size: .82rem; color: var(--text-dim); }

/* Executive summary */
.exec-summary {
  background: linear-gradient(135deg, var(--surface) 0%, #1a2332 100%);
  border: 1px solid var(--border); border-radius: var(--radius);
  padding: 1.5rem; margin-bottom: 2rem;
}
.exec-summary h2 { font-size: 1rem; color: var(--accent); margin-bottom: .75rem; }
.exec-summary p { color: var(--text); font-size: .92rem; white-space: pre-wrap; }

/* Team card */
.team {
  background: var(--surface); border: 1px solid var(--border);
  border-radius: var(--radius); margin-bottom: 1.5rem; overflow: hidden;
}
.team-head {
  display: flex; justify-content: space-between; align-items: center;
  padding: 1.25rem 1.5rem; cursor: pointer;
}
.team-head:hover { background: var(--surface-hover); }
.team-head h2 { font-size: 1.15rem; font-weight: 600; }
.team-head .badges { display: flex; gap: .5rem; align-items: center; }
.team-body { padding: 0 1.5rem 1.5rem; }

/* Badges */
.badge {
  font-size: .72rem; font-weight: 600; padding: .2rem .6rem;
  border-radius: 999px; display: inline-flex; align-items: center; gap: .3rem;
}
.badge-green { background: rgba(63,185,80,.15); color: var(--green); }
.badge-yellow { background: rgba(210,153,34,.15); color: var(--yellow); }
.badge-red { background: rgba(248,81,73,.15); color: var(--red); }
.badge-blue { background: rgba(88,166,255,.15); color: var(--accent); }
.badge-purple { background: rgba(188,140,255,.15); color: var(--purple); }
.badge-dim { background: rgba(139,148,158,.12); color: var(--text-dim); }

/* Narrative panel */
.narrative {
  background: rgba(88,166,255,.05); border-left: 3px solid var(--accent);
  border-radius: 0 8px 8px 0; padding: 1rem 1.25rem; margin-bottom: 1.25rem;
}
.narrative .label { font-size: .72rem; color: var(--accent); font-weight: 600; text-transform: uppercase; letter-spacing: .05em; margin-bottom: .4rem; }
.narrative p { font-size: .9rem; color: var(--text); white-space: pre-wrap; }

/* Pillar grid */
.pillars { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: .75rem; margin-bottom: 1.5rem; }
.pillar {
  background: var(--bg); border: 1px solid var(--border); border-radius: 8px;
  padding: .85rem 1rem; transition: border-color .2s;
}
.pillar:hover { border-color: var(--accent); }
.pillar-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: .5rem; }
.pillar-title { font-size: .88rem; font-weight: 600; }
.pillar-stats { font-size: .78rem; color: var(--text-dim); }
.pillar-tickets { font-size: .78rem; margin-top: .4rem; }
.pillar-tickets a { color: var(--accent); text-decoration: none; }
.pillar-tickets a:hover { text-decoration: underline; }

/* Progress ring */
.ring { width: 28px; height: 28px; }
.ring circle { fill: none; stroke-width: 3; }
.ring .track { stroke: var(--border); }
.ring .fill { stroke: var(--accent); stroke-linecap: round; transition: stroke-dashoffset .3s; }

/* Coverage table */
table { width: 100%; border-collapse: collapse; font-size: .85rem; margin-bottom: 1rem; }
th { text-align: left; padding: .5rem; color: var(--text-dim); font-weight: 600; border-bottom: 1px solid var(--border); }
td { padding: .5rem; border-bottom: 1px solid var(--border); }
tr:hover td { background: var(--surface-hover); }

/* Epic link */
.epic-key a { color: var(--accent); text-decoration: none; font-weight: 600; }
.epic-key a:hover { text-decoration: underline; }

/* Progress bar */
.progress-bar { display: inline-block; width: 80px; height: 6px; background: var(--border); border-radius: 3px; overflow: hidden; vertical-align: middle; }
.progress-fill { height: 100%; background: var(--accent); border-radius: 3px; }

/* Status colors */
.st-done { color: var(--green); }
.st-ongoing { color: var(--yellow); }
.st-overdue { color: var(--red); }
.st-todo { color: var(--text-dim); }

/* Drift badges */
.drift-optimistic { color: var(--orange); }
.drift-stale { color: var(--purple); }

/* Collapsed state */
[x-cloak] { display: none; }

/* Empty state */
.empty { text-align: center; padding: 3rem; color: var(--text-dim); }
.empty code { background: var(--surface); padding: .2rem .4rem; border-radius: 4px; font-size: .85rem; }

.nav-tabs { display: flex; gap: 0; margin-bottom: 1.5rem; border-bottom: 1px solid var(--border); }
.nav-tabs a { padding: .6rem 1.2rem; text-decoration: none; color: var(--text-dim); font-size: .9rem; font-weight: 600; border-bottom: 2px solid transparent; }
.nav-tabs a:hover { color: var(--text); }
.nav-tabs a.active { color: var(--accent); border-bottom-color: var(--accent); }
`
