package dashboard

import (
	"bytes"
	"fmt"
	"html/template"
	"sort"
	"time"
)

// statEntry is a key-value pair used by the HTML template stats table.
type statEntry struct {
	Key string
	Val string
}

// dashboardTmpl is the HTML template for the root `/` endpoint.
// It uses only Go standard library html/template — no external JS.
var dashboardTmpl = template.Must(template.New("dashboard").Funcs(template.FuncMap{
	"formatTime": func(t time.Time) string {
		return t.UTC().Format("2006-01-02 15:04:05 UTC")
	},
	"healthClass": func(h HealthStatus) string {
		switch h {
		case HealthOK:
			return "health-ok"
		case HealthDegraded:
			return "health-degraded"
		case HealthFailed:
			return "health-failed"
		default:
			return "health-unknown"
		}
	},
	// sortedStats returns the stats map as a sorted slice of statEntry so the
	// template can iterate without needing nested key lookups.
	"sortedStats": func(m map[string]any) []statEntry {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		entries := make([]statEntry, len(keys))
		for i, k := range keys {
			entries[i] = statEntry{Key: k, Val: fmt.Sprintf("%v", m[k])}
		}
		return entries
	},
}).Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{{.Title}}</title>
<style>
body { font-family: monospace; background: #0f0f0f; color: #d4d4d4; margin: 0; padding: 16px; }
h1 { color: #4ec9b0; margin-bottom: 4px; }
.subtitle { color: #888; font-size: 0.85em; margin-bottom: 24px; }
.components { display: flex; flex-wrap: wrap; gap: 16px; margin-bottom: 24px; }
.card { background: #1e1e1e; border: 1px solid #333; border-radius: 6px; padding: 16px; min-width: 280px; max-width: 480px; flex: 1; }
.card-title { font-size: 1.1em; font-weight: bold; color: #9cdcfe; margin-bottom: 6px; }
.card-type { font-size: 0.8em; color: #888; margin-bottom: 8px; }
.health-ok { display: inline-block; padding: 2px 8px; border-radius: 4px; background: #1e3a1e; color: #4caf50; font-size: 0.85em; margin-bottom: 10px; }
.health-degraded { display: inline-block; padding: 2px 8px; border-radius: 4px; background: #3a2e1e; color: #ff9800; font-size: 0.85em; margin-bottom: 10px; }
.health-failed { display: inline-block; padding: 2px 8px; border-radius: 4px; background: #3a1e1e; color: #f44336; font-size: 0.85em; margin-bottom: 10px; }
table { width: 100%; border-collapse: collapse; font-size: 0.85em; }
td { padding: 3px 6px; border-bottom: 1px solid #2a2a2a; }
td:first-child { color: #888; width: 55%; }
td:last-child { color: #ce9178; word-break: break-all; }
.events-section { margin-bottom: 24px; }
.events-section h2 { color: #4ec9b0; font-size: 1em; margin-bottom: 8px; }
.event { background: #1a1a1a; border-left: 3px solid #333; padding: 6px 10px; margin-bottom: 4px; font-size: 0.82em; }
.event-time { color: #888; }
.event-source { color: #9cdcfe; }
.event-action { color: #dcdcaa; }
.event-msg { color: #d4d4d4; }
.footer { color: #555; font-size: 0.75em; margin-top: 32px; border-top: 1px solid #222; padding-top: 10px; }
.no-events { color: #555; font-size: 0.85em; font-style: italic; }
</style>
</head>
<body>
<h1>{{.Title}}</h1>
<div class="subtitle">Generated: {{formatTime .GeneratedAt}} &nbsp;|&nbsp; {{len .Components}} component(s)</div>

<div class="components">
{{range .Components}}
<div class="card">
  <div class="card-title">{{.Name}}</div>
  <div class="card-type">type: {{.Type}}</div>
  <span class="{{healthClass .Health}}">{{.Health}}</span>
  <table>
  {{range (sortedStats .Stats)}}
    <tr><td>{{.Key}}</td><td>{{.Val}}</td></tr>
  {{end}}
  </table>
</div>
{{end}}
</div>

<div class="events-section">
<h2>Timeline Events ({{len .Events}})</h2>
{{if .Events}}
{{range .Events}}
<div class="event">
  <span class="event-time">{{formatTime .Time}}</span>
  &nbsp;<span class="event-source">[{{.Source}}]</span>
  &nbsp;<span class="event-action">{{.Action}}</span>
  &nbsp;<span class="event-msg">{{.Message}}</span>
</div>
{{end}}
{{else}}
<div class="no-events">No events recorded.</div>
{{end}}
</div>

<div class="footer">
  Local dashboard only — no networking, no Raft, no consensus, no distributed cluster.
</div>
</body>
</html>
`))

// renderHTML executes the dashboard HTML template against snap and returns
// the rendered bytes.
func renderHTML(snap Snapshot) ([]byte, error) {
	var buf bytes.Buffer
	if err := dashboardTmpl.Execute(&buf, snap); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
