package main

import (
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"time"

	"stasher/internal/label"
)

type dashVolume struct {
	ID       string
	Path     string
	Schedule string
	Keep     string
	Next     time.Time
}

type dashContainer struct {
	ShortID          string
	Name             string
	Image            string
	StopDuringBackup bool
	Volumes          []dashVolume
}

type dashData struct {
	Now        time.Time
	Containers []dashContainer
}

func (m *Manager) dashboardData() dashData {
	data := dashData{Now: time.Now()}
	for id, meta := range m.meta {
		dc := dashContainer{
			ShortID:          id[:12],
			Name:             meta.name,
			Image:            meta.image,
			StopDuringBackup: meta.config.Options.StopDuringBackup,
		}
		for volID, vol := range meta.config.Volumes {
			dv := dashVolume{
				ID:       volID,
				Path:     vol.Path,
				Schedule: vol.Schedule,
				Keep:     formatKeep(vol.Keep),
			}
			if jobs, ok := m.jobs[id]; ok {
				if j, ok := jobs[volID]; ok {
					dv.Next = j.Next()
				}
			}
			dc.Volumes = append(dc.Volumes, dv)
		}
		sort.Slice(dc.Volumes, func(i, j int) bool { return dc.Volumes[i].ID < dc.Volumes[j].ID })
		data.Containers = append(data.Containers, dc)
	}
	sort.Slice(data.Containers, func(i, j int) bool { return data.Containers[i].Name < data.Containers[j].Name })
	return data
}

func formatKeep(policy label.KeepPolicy) string {
	type entry struct {
		period label.Period
		name   string
	}
	order := []entry{
		{label.Days, "days"},
		{label.Weeks, "weeks"},
		{label.Months, "months"},
		{label.Years, "years"},
	}
	var parts []string
	for _, e := range order {
		if n, ok := policy[e.period]; ok && n > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", e.name, n))
		}
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, " ")
}

func (m *Manager) dashboardHandler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	data := m.dashboardData()
	m.mu.Unlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err := dashTmpl.Execute(w, data)
	if err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

var dashTmpl = template.Must(template.New("dashboard").Funcs(template.FuncMap{
	"formatTime": func(t time.Time) string {
		if t.IsZero() {
			return "—"
		}
		return t.Format("2006-01-02 15:04:05 UTC")
	},
}).Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta http-equiv="refresh" content="30">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>stasher</title>
  <style>
    *, *::before, *::after { box-sizing: border-box; }
    body { font-family: system-ui, sans-serif; margin: 0; padding: 1.5rem 2rem; background: #f8f9fa; color: #212529; }
    h1 { margin: 0 0 0.25rem; font-size: 1.5rem; }
    .meta { color: #6c757d; font-size: 0.85rem; margin-bottom: 1.5rem; }
    table { border-collapse: collapse; width: 100%; background: #fff; border-radius: 6px; overflow: hidden; box-shadow: 0 1px 3px rgba(0,0,0,.08); }
    th { background: #343a40; color: #fff; text-align: left; padding: 0.6rem 0.9rem; font-size: 0.8rem; font-weight: 600; letter-spacing: .03em; text-transform: uppercase; white-space: nowrap; }
    td { padding: 0.6rem 0.9rem; border-bottom: 1px solid #e9ecef; font-size: 0.875rem; vertical-align: middle; }
    tr:last-child td { border-bottom: none; }
    tr:hover td { background: #f1f3f5; }
    .id { color: #6c757d; font-family: monospace; font-size: 0.8em; }
    .badge { display: inline-block; padding: 0.15em 0.5em; border-radius: 3px; font-size: 0.75em; font-weight: 600; }
    .badge-yes { background: #fff3cd; color: #856404; }
    .badge-no  { background: #e9ecef; color: #6c757d; }
    .empty { text-align: center; padding: 3rem; color: #6c757d; }
  </style>
</head>
<body>
  <h1>stasher</h1>
  <p class="meta">Last updated: {{formatTime .Now}} &nbsp;·&nbsp; auto-refresh every 30 s</p>
  {{if .Containers}}
  <table>
    <thead>
      <tr>
        <th>Container</th>
        <th>Image</th>
        <th>Volume</th>
        <th>Path</th>
        <th>Schedule</th>
        <th>Next backup</th>
        <th>Keep</th>
        <th>Stop during backup</th>
      </tr>
    </thead>
    <tbody>
      {{range .Containers}}{{$c := .}}
      {{range .Volumes}}
      <tr>
        <td>{{$c.Name}}<br><span class="id">{{$c.ShortID}}</span></td>
        <td>{{$c.Image}}</td>
        <td>{{.ID}}</td>
        <td><code>{{.Path}}</code></td>
        <td>{{.Schedule}}</td>
        <td>{{formatTime .Next}}</td>
        <td>{{.Keep}}</td>
        <td>{{if $c.StopDuringBackup}}<span class="badge badge-yes">yes</span>{{else}}<span class="badge badge-no">no</span>{{end}}</td>
      </tr>
      {{end}}
      {{end}}
    </tbody>
  </table>
  {{else}}
  <div class="empty">No containers with <code>stasher.enabled=true</code> discovered yet.</div>
  {{end}}
</body>
</html>
`))
