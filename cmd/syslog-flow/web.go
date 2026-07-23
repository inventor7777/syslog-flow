package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
)

var page = template.Must(template.New("page").Funcs(template.FuncMap{
	"logHead":          logHeading,
	"logHeadColor":     logHeadingColor,
	"renderLogBody":    renderLogBody,
	"deviceColorsJSON": deviceColorsJSON,
	"interfaceTheme":   interfaceThemeDeclarations,
	"statusColor":      statusColor,
	"statusColorsJSON": statusColorsJSON,
}).Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>syslog-flow</title>
  <link rel="icon" href="/favicon.ico" sizes="any">
  <link rel="apple-touch-icon" href="/apple-touch-icon.png">
  <style>
    :root {
      color-scheme: light dark;
      {{interfaceTheme "light"}}
    }
    @media (prefers-color-scheme: dark) {
      :root {
        {{interfaceTheme "dark"}}
      }
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      background: var(--bg);
      color: var(--ink);
      font: 15px/1.45 ui-sans-serif, "Aptos", "Segoe UI", sans-serif;
      overflow: hidden;
    }
    body.overview-page {
      overflow: auto;
    }
    header {
      align-items: center;
      border-bottom: 1px solid var(--line);
      display: flex;
      gap: 1rem;
      padding: 1rem 1.25rem;
      background: var(--panel-strong);
      backdrop-filter: blur(8px);
      position: sticky;
      top: 0;
      z-index: 2;
    }
    h1 { margin: 0; font-size: 1.1rem; letter-spacing: 0.03em; }
    h1 a {
      color: var(--ink);
      text-decoration: none;
    }
    h1 a:hover { color: var(--accent-strong); text-decoration: none; }
    .top-stats {
      align-items: center;
      display: flex;
      gap: 0.55rem;
      justify-content: flex-end;
      margin-left: auto;
      min-width: 0;
    }
    .top-link {
      align-items: center;
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 999px;
      box-shadow: var(--glow-soft);
      color: var(--ink);
      display: inline-flex;
      font-size: 0.82rem;
      font-weight: 700;
      padding: 0.32rem 0.7rem;
      text-decoration: none;
      white-space: nowrap;
    }
    .top-link:hover {
      border-color: var(--accent);
      color: var(--accent-strong);
      text-decoration: none;
    }
    .top-link.active {
      background: var(--active-bg);
      border-color: var(--accent);
      color: var(--active-ink);
    }
    .jump-controls {
      display: none;
    }
    .stat {
      align-items: center;
      border: 1px solid var(--line);
      border-radius: 999px;
      background: var(--panel);
      box-shadow: var(--glow-soft);
      color: var(--muted);
      display: inline-flex;
      font-size: 0.82rem;
      gap: 0.25rem;
      padding: 0.32rem 0.58rem;
      white-space: nowrap;
    }
    button.stat {
      font: inherit;
      cursor: pointer;
    }
    button.stat:hover {
      border-color: var(--accent);
      color: var(--accent-strong);
    }
    button.stat:focus-visible {
      outline: 2px solid var(--accent);
      outline-offset: 2px;
    }
    .stat strong {
      color: var(--ink);
      font-weight: 750;
    }
    .layout {
      display: grid;
      grid-template-columns: 18rem minmax(0, 1fr);
      height: calc(100vh - 3.75rem);
      min-height: 0;
    }
    aside {
      border-right: 1px solid var(--line);
      padding: 1rem;
      background: var(--panel-soft);
      box-shadow: inset -1px 0 0 var(--line);
      overflow: auto;
    }
    main { min-height: 0; min-width: 0; overflow: hidden; padding: 1rem; }
    a { color: var(--accent-strong); text-decoration: none; }
    a:hover { text-decoration: underline; }
    .day {
      display: block;
      padding: 0.45rem 0.6rem;
      border-radius: 0.55rem;
      margin-bottom: 0.2rem;
      font-weight: 650;
    }
    .day.active { background: var(--active-bg); color: var(--active-ink); }
    .files {
      display: flex;
      flex-wrap: wrap;
      gap: 0.5rem;
      margin: 1rem 0;
    }
    .chip {
      display: inline-block;
      padding: 0.4rem 0.6rem;
      border: 1px solid var(--line);
      border-radius: 999px;
      background: var(--panel);
      box-shadow: var(--glow-soft);
    }
    .chip.active { border-color: var(--accent); background: var(--active-bg); color: var(--active-ink); }
    .severity-chip {
      border-color: var(--chip-accent, var(--line));
      color: var(--chip-accent, inherit);
    }
    @media (prefers-color-scheme: dark) {
      .severity-chip {
        box-shadow: 0 0 12px var(--chip-accent, rgba(0, 120, 255, 0.25));
      }
    }
    form {
      display: flex;
      gap: 0.5rem;
      margin: 0.75rem 0 1rem;
      flex-wrap: wrap;
    }
    .search-form {
      align-items: stretch;
      flex-wrap: nowrap;
    }
    .search-form input {
      min-width: 0;
    }
    .search-form button {
      align-items: center;
      display: inline-flex;
      flex: 0 0 auto;
      height: 2.35rem;
      justify-content: center;
      padding: 0;
      width: 2.35rem;
    }
    .search-form button svg {
      fill: currentColor;
      height: 1.45rem;
      width: 1.45rem;
    }
    input {
      min-width: min(28rem, 100%);
      flex: 1;
      padding: 0.65rem 0.75rem;
      border: 1px solid var(--line);
      border-radius: 0.55rem;
      background: var(--input-bg);
      color: var(--ink);
      box-shadow: var(--glow-soft);
    }
    form button {
      padding: 0.65rem 0.9rem;
      border: 0;
      border-radius: 0.55rem;
      background: var(--accent);
      color: white;
      font-weight: 700;
      cursor: pointer;
      box-shadow: var(--glow-soft);
    }
    .panel {
      display: flex;
      flex-direction: column;
      height: 100%;
      min-height: 0;
      overflow: visible;
      padding: 0;
      background: transparent;
      border: 0;
      border-radius: 0;
      box-shadow: none;
    }
    h2 { margin: 0 0 0.8rem; }
    h3 { margin: 0 0 0.55rem; }
    .dashboard {
      display: flex;
      flex: 1;
      flex-direction: column;
      gap: 1rem;
      min-height: 0;
    }
    .stats-grid {
      display: grid;
      gap: 0.9rem;
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    .stat-tile {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 1rem;
      box-shadow: var(--glow-card);
      padding: 1rem 1.1rem;
    }
    .stat-tile-label {
      color: var(--muted);
      display: block;
      font-size: 0.9rem;
      font-weight: 700;
      letter-spacing: 0.02em;
      margin-bottom: 0.45rem;
      text-transform: uppercase;
    }
    .stat-tile-value {
      color: var(--ink);
      display: block;
      font-size: clamp(1.65rem, 3vw, 2.35rem);
      font-weight: 800;
      letter-spacing: -0.03em;
      line-height: 1.05;
    }
    .stat-tile-note {
      color: var(--muted);
      display: block;
      font-size: 0.95rem;
      margin-top: 0.35rem;
    }
    .dashboard-section {
      min-height: 0;
    }
    .dashboard-section:last-child {
      display: flex;
      flex: 1;
      flex-direction: column;
    }
    .device-list {
      display: grid;
      gap: 0.6rem;
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    .device-list.overview {
      grid-template-columns: 1fr;
    }
    .device-row {
      align-items: center;
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 0.7rem;
      color: inherit;
      display: flex;
      gap: 0.4rem;
      min-width: 0;
      padding: 0.62rem 0.7rem;
      text-decoration: none;
      white-space: nowrap;
    }
    .device-row:hover {
      border-color: var(--accent);
      text-decoration: none;
    }
    .device-row strong {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .device-row span { color: var(--muted); font-size: 0.84rem; white-space: nowrap; }
    .device-row .sep {
      color: var(--muted);
      flex: 0 0 auto;
    }
    .device-row .spacer {
      flex: 1 1 auto;
      min-width: 0;
    }
    .muted { color: var(--muted); }
    .overview-page .layout {
      grid-template-columns: minmax(0, 1fr);
      height: auto;
      min-height: calc(100vh - 3.75rem);
    }
    .overview-page main {
      overflow: auto;
    }
    .overview-page .panel {
      height: auto;
      min-height: calc(100vh - 5.75rem);
      overflow: visible;
    }
    .overview-page .dashboard {
      min-height: auto;
    }
    .overview-page .dashboard-section:last-child {
      flex: none;
    }
    .overview-link-wrap {
      display: flex;
      justify-content: center;
      margin-top: 1.1rem;
    }
    .overview-link {
      align-items: center;
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 999px;
      box-shadow: var(--glow-soft);
      color: var(--muted);
      display: inline-flex;
      gap: 0.45rem;
      padding: 0.55rem 0.9rem;
      text-decoration: none;
      white-space: nowrap;
    }
    .overview-link:hover {
      border-color: var(--accent);
      color: var(--accent-strong);
      text-decoration: none;
    }
    .overview-link svg {
      fill: currentColor;
      height: 0.95rem;
      width: 0.95rem;
    }
    pre {
      margin: 0;
      overflow: auto;
      white-space: pre-wrap;
      word-break: break-word;
      color: var(--code-ink);
      background: var(--code);
      flex: 1;
      border-radius: 0.65rem;
      padding: 1rem;
      min-height: 0;
    }
    .log-line {
      display: block;
    }
    .log-head {
      font-weight: 700;
    }
    .log-tag {
      font-weight: 600;
    }
    .error {
      border: 1px solid var(--error-line);
      background: var(--error-bg);
      color: var(--error-ink);
      padding: 0.75rem;
      border-radius: 0.55rem;
      margin-bottom: 1rem;
    }
    @media (max-width: 1194px) and (min-width: 761px) {
      .files {
        align-content: flex-start;
        max-height: 5.9rem;
        overflow-y: auto;
        padding-right: 0.15rem;
      }
    }
    @media (max-width: 760px) {
      body { overflow: auto; }
      header { align-items: center; flex-direction: row; flex-wrap: wrap; }
      .jump-controls {
        display: flex;
        flex-basis: 100%;
        gap: 0.5rem;
        width: 100%;
      }
      .jump-controls .top-link {
        flex: 1 1 0;
        justify-content: center;
      }
      .top-stats {
        flex-basis: 100%;
        justify-content: flex-start;
        margin-left: 0;
        overflow-x: auto;
        width: 100%;
      }
      .stats-grid { grid-template-columns: 1fr; }
      .device-list { grid-template-columns: 1fr; }
      .device-row {
        display: grid;
        gap: 0.2rem;
        white-space: normal;
      }
      .layout { grid-template-columns: 1fr; height: auto; }
      main { overflow: visible; }
      .panel { height: auto; overflow: visible; }
      pre { flex: none; min-height: 24rem; }
      aside { border-right: 0; border-bottom: 1px solid var(--line); }
    }
  </style>
</head>
<body{{if .Overview}} class="overview-page"{{end}} data-stats-refresh-ms="{{.StatsRefreshMS}}" data-overview-refresh-ms="{{.OverviewRefreshMS}}">
  <header>
    <h1><a href="/">syslog-flow</a></h1>
    <a class="top-link" href="/statistics">Statistics</a>
    <button class="top-link active" type="button" data-live-toggle>Live</button>
    {{if or .Lines .Selected}}
      <div class="jump-controls">
        <button class="top-link" type="button" data-jump-top>Jump to Top</button>
        <button class="top-link" type="button" data-jump-bottom>Jump to Bottom</button>
      </div>
    {{end}}
    <div class="top-stats" aria-label="Syslog statistics">
      <span class="stat">Crit 5m <strong data-stat-value="critical5m">{{.Critical5m}}</strong></span>
      <span class="stat">Today <strong data-stat-value="todayLines">{{.TodayLines}}</strong></span>
      <span class="stat">Lines/s <strong data-stat-value="linesPerSecond">{{.LinesPerSecond}}</strong></span>
      <button class="stat" type="button" data-copy="{{.SyslogEndpoint}}">
        <span data-copy-label>Syslog</span> <strong>{{.SyslogEndpoint}}</strong>
      </button>
    </div>
  </header>
  <div class="layout">
    {{if not .Overview}}
      <aside>
        <form class="search-form" action="/search" method="get">
          <input name="q" value="{{.Query}}" placeholder="Global Search">
          <button type="submit" name="scope" value="all" aria-label="Search all logs" title="Search all logs">
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path d="M9.5,3A6.5,6.5 0 0,1 16,9.5C16,11.11 15.41,12.59 14.44,13.73L14.71,14H15.5L20.5,19L19,20.5L14,15.5V14.71L13.73,14.44C12.59,15.41 11.11,16 9.5,16A6.5,6.5 0 0,1 3,9.5A6.5,6.5 0 0,1 9.5,3M9.5,5C7,5 5,7 5,9.5C5,12 7,14 9.5,14C12,14 14,12 14,9.5C14,7 12,5 9.5,5Z"/>
            </svg>
          </button>
          <button type="submit" name="scope" value="week" aria-label="Search last 7 days" title="Search last 7 days">
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path d="M15.5,12C18,12 20,14 20,16.5C20,17.38 19.75,18.21 19.31,18.9L22.39,22L21,23.39L17.88,20.32C17.19,20.75 16.37,21 15.5,21C13,21 11,19 11,16.5C11,14 13,12 15.5,12M15.5,14A2.5,2.5 0 0,0 13,16.5A2.5,2.5 0 0,0 15.5,19A2.5,2.5 0 0,0 18,16.5A2.5,2.5 0 0,0 15.5,14M19,8H5V19H9.5C9.81,19.75 10.26,20.42 10.81,21H5C3.89,21 3,20.1 3,19V5C3,3.89 3.89,3 5,3H6V1H8V3H16V1H18V3H19A2,2 0 0,1 21,5V13.03C20.5,12.22 19.8,11.54 19,11V8Z"/>
            </svg>
          </button>
        </form>
        <p class="muted">Days</p>
        {{range .Days}}
          <a class="day {{if eq $.Selected .Name}}active{{end}}" href="/day/{{.Name}}">{{.Name}}</a>
        {{else}}
          <p class="muted">No logs yet.</p>
        {{end}}
      </aside>
    {{end}}
    <main>
      {{if .Error}}<div class="error">{{.Error}}</div>{{end}}
      <section class="panel">
        {{if .Global}}
          <h2>Global Search</h2>
        {{else if .Selected}}
          <h2>{{.Selected}}</h2>
        {{else if .Overview}}
          <h2>Statistics</h2>
        {{else if not .Live}}
          <h2>Waiting for logs</h2>
          <p class="muted">Point devices at this host on syslog UDP/TCP port 514.</p>
        {{end}}

        {{if .Overview}}
          {{if .Days}}
            <div class="dashboard">
              <div class="dashboard-section">
                <div class="stats-grid">
                  <div class="stat-tile">
                    <span class="stat-tile-label">All time</span>
                    <span class="stat-tile-value" data-overview-value="allLines">{{.AllLines}}</span>
                    <span class="stat-tile-note">total lines stored</span>
                  </div>
                  <div class="stat-tile">
                    <span class="stat-tile-label">Storage</span>
                    <span class="stat-tile-value" data-overview-value="totalLogSize">{{.TotalLogSize}}</span>
                    <span class="stat-tile-note">size of stored log files</span>
                  </div>
                  <div class="stat-tile">
                    <span class="stat-tile-label">Devices</span>
                    <span class="stat-tile-value" data-overview-value="deviceCount">{{.DeviceCount}}</span>
                    <span class="stat-tile-note">devices with stored logs</span>
                  </div>
                  <div class="stat-tile">
                    <span class="stat-tile-label">Log days</span>
                    <span class="stat-tile-value" data-overview-value="dayCount">{{.DayCount}}</span>
                    <span class="stat-tile-note">days with stored logs</span>
                  </div>
                </div>
              </div>
              <div class="dashboard-section">
                <h3>Devices</h3>
                <div class="device-list overview" data-overview-devices>
                  {{range .Devices}}
                    <a class="device-row" href="{{.Link}}">
                      <strong{{with .Color}} style="color: {{.}}"{{end}}>{{.Name}}</strong>
                      <span class="sep">-</span>
                      <span>{{.LineInfo}}</span>
                      <span class="sep">-</span>
                      <span>{{.LastSeen}}</span>
                      {{if .IP}}
                        <span class="spacer"></span>
                        <span>{{.IP}}</span>
                      {{end}}
                    </a>
                  {{else}}
                    <p class="muted">No device logs yet.</p>
                  {{end}}
                </div>
              </div>
            </div>
            <div class="overview-link-wrap">
              <a class="overview-link" href="https://github.com/inventor7777/syslog-flow" target="_blank" rel="noreferrer">
                GitHub / Documentation
                <svg viewBox="0 0 24 24" aria-hidden="true">
                  <path d="M14 3h7v7h-2V6.41l-9.29 9.3-1.42-1.42 9.3-9.29H14V3zM5 5h6v2H7v10h10v-4h2v6H5V5z"/>
                </svg>
              </a>
            </div>
          {{else}}
            <p class="muted">Point devices at this host on syslog UDP/TCP port 514.</p>
          {{end}}
        {{end}}

        {{if and .Live (not .Lines)}}
          <p class="muted">Point devices at this host on syslog UDP/TCP port 514.</p>
        {{end}}

        {{if .Files}}
          <div class="files">
            <a class="chip {{if not .File}}active{{end}}" href="/day/{{.Selected}}{{if .Query}}?q={{.Query | urlquery}}{{end}}">All files</a>
            {{range .Files}}
              <a class="chip {{if eq $.File .Name}}active{{end}}" href="/day/{{$.Selected}}?file={{.Name | urlquery}}{{if $.Query}}&q={{$.Query | urlquery}}{{end}}">{{.Name}}</a>
            {{end}}
          </div>
        {{end}}

        {{if and .Selected (not .Global)}}
          <form action="/day/{{.Selected}}" method="get">
            {{if .File}}<input type="hidden" name="file" value="{{.File}}">{{end}}
            {{if .Severity}}<input type="hidden" name="level" value="{{.Severity}}">{{end}}
            <input name="q" value="{{.Query}}" placeholder="Filter this day{{if .File}} / file{{end}}">
            <button type="submit">Filter</button>
          </form>
          <div class="files">
            <a class="chip {{if eq .Severity ""}}active{{end}}" href="/day/{{.Selected}}{{if .File}}?file={{.File | urlquery}}{{if .Query}}&q={{.Query | urlquery}}{{end}}{{else if .Query}}?q={{.Query | urlquery}}{{end}}">All events</a>
            <a class="chip severity-chip {{if eq .Severity "emerg"}}active{{end}}" style="--chip-accent: {{statusColor "emerg"}};" href="/day/{{.Selected}}?level=emerg{{if .File}}&file={{.File | urlquery}}{{end}}{{if .Query}}&q={{.Query | urlquery}}{{end}}">Emerg</a>
            <a class="chip severity-chip {{if eq .Severity "alert"}}active{{end}}" style="--chip-accent: {{statusColor "alert"}};" href="/day/{{.Selected}}?level=alert{{if .File}}&file={{.File | urlquery}}{{end}}{{if .Query}}&q={{.Query | urlquery}}{{end}}">Alert</a>
            <a class="chip severity-chip {{if eq .Severity "crit"}}active{{end}}" style="--chip-accent: {{statusColor "crit"}};" href="/day/{{.Selected}}?level=crit{{if .File}}&file={{.File | urlquery}}{{end}}{{if .Query}}&q={{.Query | urlquery}}{{end}}">Crit</a>
            <a class="chip severity-chip {{if eq .Severity "err"}}active{{end}}" style="--chip-accent: {{statusColor "err"}};" href="/day/{{.Selected}}?level=err{{if .File}}&file={{.File | urlquery}}{{end}}{{if .Query}}&q={{.Query | urlquery}}{{end}}">Err</a>
            <a class="chip severity-chip {{if eq .Severity "warning"}}active{{end}}" style="--chip-accent: {{statusColor "warning"}};" href="/day/{{.Selected}}?level=warning{{if .File}}&file={{.File | urlquery}}{{end}}{{if .Query}}&q={{.Query | urlquery}}{{end}}">Warning</a>
            <a class="chip severity-chip {{if eq .Severity "notice"}}active{{end}}" style="--chip-accent: {{statusColor "notice"}};" href="/day/{{.Selected}}?level=notice{{if .File}}&file={{.File | urlquery}}{{end}}{{if .Query}}&q={{.Query | urlquery}}{{end}}">Notice</a>
            <a class="chip severity-chip {{if eq .Severity "info"}}active{{end}}" style="--chip-accent: {{statusColor "info"}};" href="/day/{{.Selected}}?level=info{{if .File}}&file={{.File | urlquery}}{{end}}{{if .Query}}&q={{.Query | urlquery}}{{end}}">Info</a>
            <a class="chip severity-chip {{if eq .Severity "debug"}}active{{end}}" style="--chip-accent: {{statusColor "debug"}};" href="/day/{{.Selected}}?level=debug{{if .File}}&file={{.File | urlquery}}{{end}}{{if .Query}}&q={{.Query | urlquery}}{{end}}">Debug</a>
          </div>
        {{end}}

        {{if .ResultInfo}}<p class="muted">{{.ResultInfo}}</p>{{end}}
        {{if or .Lines .Selected}}<pre id="log-viewer" {{if .RefreshURL}}data-refresh-url="{{.RefreshURL}}" data-refresh-ms="{{.LiveRefreshMS}}"{{end}}{{if gt .RefreshCursor 0}} data-refresh-cursor="{{.RefreshCursor}}"{{end}}{{if .OlderURL}} data-older-url="{{.OlderURL}}" data-start="{{.ChunkStart}}" data-total="{{.TotalLogLines}}" data-has-older="{{.HasOlder}}"{{end}}>{{range .Lines}}<span class="log-line" data-raw="{{.}}"><span class="log-head"{{with logHeadColor .}} style="color: {{.}}"{{end}}>{{logHead .}}</span>{{renderLogBody .}}</span>{{end}}</pre>{{end}}
      </section>
    </main>
  </div>
  <script id="device-colors" type="application/json">{{deviceColorsJSON}}</script>
  <script id="status-colors" type="application/json">{{statusColorsJSON}}</script>
  <script>
    async function copyText(text) {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(text);
        return;
      }

      const textarea = document.createElement("textarea");
      textarea.value = text;
      textarea.setAttribute("readonly", "");
      textarea.style.position = "fixed";
      textarea.style.left = "-9999px";
      textarea.style.top = "0";
      document.body.appendChild(textarea);
      textarea.select();
      textarea.setSelectionRange(0, textarea.value.length);

      try {
        if (!document.execCommand("copy")) {
          throw new Error("copy command failed");
        }
      } finally {
        textarea.remove();
      }
    }

    for (const button of document.querySelectorAll("[data-copy]")) {
      button.addEventListener("click", async () => {
        const label = button.querySelector("[data-copy-label]");
        const original = label.textContent;
        try {
          await copyText(button.dataset.copy);
          label.textContent = "Copied";
          setTimeout(() => { label.textContent = original; }, 1200);
        } catch {
          label.textContent = "Copy failed";
          setTimeout(() => { label.textContent = original; }, 1200);
        }
      });
    }

    const logViewer = document.getElementById("log-viewer");
    const liveToggle = document.querySelector("[data-live-toggle]");
    const jumpTop = document.querySelector("[data-jump-top]");
    const jumpBottom = document.querySelector("[data-jump-bottom]");
    const deviceColors = JSON.parse(document.getElementById("device-colors")?.textContent || "{}");
    const statusColors = JSON.parse(document.getElementById("status-colors")?.textContent || "{}");
    const statNodes = Object.fromEntries(
      Array.from(document.querySelectorAll("[data-stat-value]"), (node) => [node.dataset.statValue, node])
    );
    const overviewNodes = Object.fromEntries(
      Array.from(document.querySelectorAll("[data-overview-value]"), (node) => [node.dataset.overviewValue, node])
    );
    const overviewDevices = document.querySelector("[data-overview-devices]");
    let liveEnabled = true;
    const olderUrl = logViewer?.dataset.olderUrl || "";
    let refreshCursor = Number.parseInt(logViewer?.dataset.refreshCursor || "0", 10);
    const liveRefreshMs = Number.parseInt(logViewer?.dataset.refreshMs || "10000", 10);
    const statsRefreshMs = Number.parseInt(document.body.dataset.statsRefreshMs || "10000", 10);
    const overviewRefreshMs = Number.parseInt(document.body.dataset.overviewRefreshMs || "10000", 10);
    const preservePrefix = olderUrl !== "";
    let loadedStart = Number.parseInt(logViewer?.dataset.start || "0", 10);
    let hasOlder = logViewer?.dataset.hasOlder === "true";
    let loadingOlder = false;

    function deviceColorForName(name) {
      if (!name) {
        return "";
      }
      if (deviceColors && typeof deviceColors === "object" && !Array.isArray(deviceColors)) {
        if (deviceColors.exact || deviceColors.contains) {
          const exact = deviceColors.exact || {};
          if (exact[name]) {
            return exact[name];
          }
          const contains = Array.isArray(deviceColors.contains) ? deviceColors.contains : [];
          for (const rule of contains) {
            if (rule && typeof rule.match === "string" && rule.match && typeof rule.color === "string" && name.includes(rule.match)) {
              return rule.color;
            }
          }
          return "";
        }
        return deviceColors[name] || "";
      }
      return "";
    }

    function hasViewerSelection() {
      const selection = window.getSelection();
      if (!selection || selection.rangeCount === 0 || selection.isCollapsed) {
        return false;
      }
      return logViewer.contains(selection.getRangeAt(0).commonAncestorContainer);
    }

    function viewerLines() {
      return Array.from(logViewer.querySelectorAll(".log-line"), (node) => node.dataset.raw || node.textContent);
    }

    function appendLineNode(line) {
      const node = document.createElement("span");
      node.className = "log-line";
      node.dataset.raw = line;
      const parts = splitLogLine(line);
      const head = document.createElement("span");
      head.className = "log-head";
      head.textContent = parts.head;
      const deviceColor = deviceColorForName(parts.device);
      if (deviceColor) {
        head.style.color = deviceColor;
      }
      node.appendChild(head);
      if (parts.tag) {
        node.appendChild(document.createTextNode(" "));
        const tag = document.createElement("span");
        tag.className = "log-tag";
        tag.textContent = parts.tag;
        if (parts.severity && statusColors[parts.severity]) {
          tag.style.color = statusColors[parts.severity];
        }
        node.appendChild(tag);
        if (parts.message) {
          node.appendChild(document.createTextNode(parts.message));
        }
      } else if (parts.tail) {
        node.appendChild(document.createTextNode(parts.tail));
      }
      return node;
    }

    function visibleLogText(line) {
      const separator = line.indexOf(" | ");
      if (separator < 0) {
        return line;
      }
      return line.slice(separator + 3);
    }

    function splitLogLine(line) {
      const sep = line.indexOf("  ");
      if (sep < 0) {
        return { head: line, tail: "", device: "", severity: "", tag: "", message: "" };
      }

      const rest = visibleLogText(line.slice(sep + 2).trimStart());
      const firstSpace = rest.indexOf(" ");
      if (firstSpace < 0) {
        return { head: rest, tail: "", device: "", severity: "", tag: "", message: "" };
      }

      const timestamp = rest.slice(0, firstSpace);
      const afterTimestamp = rest.slice(firstSpace + 1).trimStart();
      const secondSpace = afterTimestamp.indexOf(" ");
      if (secondSpace < 0) {
        return { head: afterTimestamp + " " + timestamp, tail: "", device: afterTimestamp, severity: "", tag: "", message: "" };
      }

      const device = afterTimestamp.slice(0, secondSpace);
      const tail = afterTimestamp.slice(secondSpace);
      const body = parseLogBody(tail);
      return { head: device + " " + timestamp, tail, device, severity: body.severity, tag: body.tag, message: body.message };
    }

    function parseLogBody(tail) {
      const trimmed = tail.trimStart();
      if (!trimmed) {
        return { severity: "", tag: "", message: "" };
      }

      const firstSpace = trimmed.indexOf(" ");
      if (firstSpace < 0) {
        return { severity: "", tag: "", message: "" };
      }

      const severity = trimmed.slice(0, firstSpace).toLowerCase();
      if (!statusColors[severity]) {
        return { severity: "", tag: "", message: "" };
      }

      const rest = trimmed.slice(firstSpace + 1).trimStart();
      const secondSpace = rest.indexOf(" ");
      if (secondSpace < 0) {
        return { severity, tag: rest, message: "" };
      }

      return {
        severity,
        tag: rest.slice(0, secondSpace),
        message: rest.slice(secondSpace),
      };
    }

    function linesEqual(left, right) {
      if (left.length !== right.length) {
        return false;
      }
      for (let i = 0; i < left.length; i++) {
        if (left[i] !== right[i]) {
          return false;
        }
      }
      return true;
    }

    function overlapEnd(currentLines, nextLines) {
      const maxOverlap = Math.min(nextLines.length, currentLines.length);
      for (let overlap = maxOverlap; overlap > 0; overlap--) {
        let matches = true;
        for (let i = 0; i < overlap; i++) {
          if (currentLines[currentLines.length - overlap + i] !== nextLines[i]) {
            matches = false;
            break;
          }
        }
        if (matches) {
          return overlap;
        }
      }
      return 0;
    }

    function replaceViewerLines(lines) {
      const fragment = document.createDocumentFragment();
      for (const line of lines) {
        fragment.appendChild(appendLineNode(line));
      }
      logViewer.replaceChildren(fragment);
    }

    function prependViewerLines(lines) {
      const fragment = document.createDocumentFragment();
      for (const line of lines) {
        fragment.appendChild(appendLineNode(line));
      }
      logViewer.insertBefore(fragment, logViewer.firstChild);
    }

    function applyIncrementalUpdate(nextLines) {
      const currentLines = viewerLines();
      if (linesEqual(nextLines, currentLines)) {
        return true;
      }

      const overlap = overlapEnd(currentLines, nextLines);
      if (overlap === 0 && currentLines.length > 0 && nextLines.length > 0) {
        return false;
      }

      const newSuffix = nextLines.slice(overlap);
      if (!preservePrefix) {
        const keepCount = overlap;
        const dropCount = Math.max(0, currentLines.length - keepCount);
        const currentNodes = Array.from(logViewer.querySelectorAll(".log-line"));

        if (dropCount > 0) {
          for (let i = 0; i < dropCount; i++) {
            const node = currentNodes[i];
            if (node) {
              node.remove();
            }
          }
        }
      }

      for (const line of newSuffix) {
        logViewer.appendChild(appendLineNode(line));
      }

      return true;
    }

    function buildOverviewDeviceNode(device) {
      const row = document.createElement("a");
      row.className = "device-row";
      row.href = device.link || "#";

      const name = document.createElement("strong");
      name.textContent = device.name || "";
      if (device.color) {
        name.style.color = device.color;
      }
      row.appendChild(name);

      const sepOne = document.createElement("span");
      sepOne.className = "sep";
      sepOne.textContent = "-";
      row.appendChild(sepOne);

      const lineInfo = document.createElement("span");
      lineInfo.textContent = device.lineInfo || "";
      row.appendChild(lineInfo);

      const sepTwo = document.createElement("span");
      sepTwo.className = "sep";
      sepTwo.textContent = "-";
      row.appendChild(sepTwo);

      const lastSeen = document.createElement("span");
      lastSeen.textContent = device.lastSeen || "";
      row.appendChild(lastSeen);

      if (device.ip) {
        const spacer = document.createElement("span");
        spacer.className = "spacer";
        row.appendChild(spacer);

        const ip = document.createElement("span");
        ip.textContent = device.ip;
        row.appendChild(ip);
      }

      return row;
    }

    function replaceOverviewDevices(devices) {
      if (!overviewDevices) {
        return;
      }
      const fragment = document.createDocumentFragment();
      if (!Array.isArray(devices) || devices.length === 0) {
        const empty = document.createElement("p");
        empty.className = "muted";
        empty.textContent = "No device logs yet.";
        fragment.appendChild(empty);
      } else {
        for (const device of devices) {
          fragment.appendChild(buildOverviewDeviceNode(device));
        }
      }
      overviewDevices.replaceChildren(fragment);
    }

    if (liveToggle) {
      liveToggle.addEventListener("click", () => {
        liveEnabled = !liveEnabled;
        liveToggle.classList.toggle("active", liveEnabled);
        liveToggle.textContent = liveEnabled ? "Live" : "Paused";
      });
    }

    if (logViewer) {
      const scrollToTop = () => {
        logViewer.scrollTop = 0;
      };

      const scrollToBottom = () => {
        logViewer.scrollTop = logViewer.scrollHeight;
      };

      const jumpToTop = () => {
        scrollToTop();
        requestAnimationFrame(() => {
          logViewer.firstElementChild?.scrollIntoView({ block: "start" });
          window.scrollTo({ top: 0, behavior: "smooth" });
        });
      };

      const jumpToBottom = () => {
        scrollToBottom();
        requestAnimationFrame(() => {
          logViewer.lastElementChild?.scrollIntoView({ block: "end" });
          window.scrollTo({ top: document.documentElement.scrollHeight, behavior: "smooth" });
        });
      };

      if (jumpTop) {
        jumpTop.addEventListener("click", jumpToTop);
      }

      if (jumpBottom) {
        jumpBottom.addEventListener("click", jumpToBottom);
      }

      async function loadOlder() {
        if (!preservePrefix || !hasOlder || loadingOlder) {
          return;
        }
        loadingOlder = true;
        const previousHeight = logViewer.scrollHeight;
        const previousTop = logViewer.scrollTop;
        try {
          const separator = olderUrl.includes("?") ? "&" : "?";
          const response = await fetch(olderUrl + separator + "before=" + loadedStart, { cache: "no-store" });
          if (!response.ok) {
            return;
          }
          const payload = await response.json();
          const olderLines = Array.isArray(payload.lines) ? payload.lines : [];
          if (olderLines.length === 0) {
            hasOlder = false;
            return;
          }
          prependViewerLines(olderLines);
          loadedStart = typeof payload.start === "number" ? payload.start : Math.max(0, loadedStart - olderLines.length);
          hasOlder = !!payload.hasMoreBefore;
          logViewer.scrollTop = logViewer.scrollHeight - previousHeight + previousTop;
        } catch {
          // Keep the current view if loading older lines fails.
        } finally {
          loadingOlder = false;
        }
      }

      if (logViewer.dataset.refreshUrl) {
        requestAnimationFrame(scrollToBottom);
      }

      logViewer.addEventListener("scroll", () => {
        if (logViewer.scrollTop <= 40) {
          void loadOlder();
        }
      });

      setInterval(async () => {
        if (!liveEnabled) {
          return;
        }
        if (hasViewerSelection()) {
          return;
        }
        const distanceFromBottom = logViewer.scrollHeight - logViewer.clientHeight - logViewer.scrollTop;
        const wasAtBottom = distanceFromBottom <= 4;
        if (!wasAtBottom) {
          return;
        }
        try {
          const refreshUrl = new URL(logViewer.dataset.refreshUrl, window.location.origin);
          if (refreshCursor > 0 && preservePrefix) {
            refreshUrl.searchParams.set("since", String(refreshCursor));
          }
          const response = await fetch(refreshUrl, { cache: "no-store" });
          if (response.ok) {
            const payload = await response.json();
            const nextLines = Array.isArray(payload.lines) ? payload.lines : [];
            if (typeof payload.cursor === "number" && payload.cursor > 0) {
              refreshCursor = payload.cursor;
              logViewer.dataset.refreshCursor = String(payload.cursor);
            }
            if (payload.replace) {
              replaceViewerLines(nextLines);
              scrollToBottom();
              return;
            }
            if (preservePrefix && refreshCursor > 0) {
              if (nextLines.length === 0) {
                return;
              }
              for (const line of nextLines) {
                logViewer.appendChild(appendLineNode(line));
              }
              scrollToBottom();
              return;
            }
            if (linesEqual(nextLines, viewerLines())) {
              return;
            }
            if (!applyIncrementalUpdate(nextLines)) {
              replaceViewerLines(nextLines);
            }
            scrollToBottom();
          }
        } catch {
          // Keep the current view if a refresh fails.
        }
      }, Number.isFinite(liveRefreshMs) && liveRefreshMs > 0 ? liveRefreshMs : 10000);
    }

    setInterval(async () => {
      try {
        const response = await fetch("/?partial=stats&format=json", { cache: "no-store" });
        if (!response.ok) {
          return;
        }
        const payload = await response.json();
        for (const [key, node] of Object.entries(statNodes)) {
          if (typeof payload[key] === "string") {
            node.textContent = payload[key];
          }
        }
      } catch {
        // Keep the current stats if refresh fails.
      }
    }, Number.isFinite(statsRefreshMs) && statsRefreshMs > 0 ? statsRefreshMs : 10000);

    if (overviewDevices) {
      setInterval(async () => {
        try {
          const response = await fetch("/api/overview", { cache: "no-store" });
          if (!response.ok) {
            return;
          }
          const payload = await response.json();
          for (const [key, node] of Object.entries(overviewNodes)) {
            if (typeof payload[key] === "string") {
              node.textContent = payload[key];
            }
          }
          replaceOverviewDevices(payload.devices);
        } catch {
          // Keep the current overview if refresh fails.
        }
      }, Number.isFinite(overviewRefreshMs) && overviewRefreshMs > 0 ? overviewRefreshMs : 10000);
    }
  </script>
</body>
</html>`))

type overviewPayload struct {
	AllLines     string          `json:"allLines"`
	TotalLogSize string          `json:"totalLogSize"`
	DayCount     string          `json:"dayCount"`
	DeviceCount  string          `json:"deviceCount"`
	Devices      []devicePayload `json:"devices"`
}

type devicePayload struct {
	Name     string `json:"name"`
	Link     string `json:"link"`
	LineInfo string `json:"lineInfo"`
	LastSeen string `json:"lastSeen"`
	IP       string `json:"ip,omitempty"`
	Color    string `json:"color,omitempty"`
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	days, err := listDays()
	data := PageData{Days: days, Live: true}
	if wantsStatsPartial(r) {
		snapshot, statsErr := buildStatsSnapshot(days)
		if err == nil && statsErr != nil {
			err = statsErr
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeStatsJSON(w, snapshot)
		return
	}
	if err != nil {
		data.Error = err.Error()
	}
	if len(days) > 0 {
		window, windowErr := stateIndex.liveWindow(appNow(), 200)
		lines := window.lines
		liveErr := windowErr
		if liveErr != nil && data.Error == "" {
			data.Error = liveErr.Error()
		}
		data.Lines = lines
		data.RefreshCursor = window.cursor
	}
	if wantsLogPartial(r) {
		if wantsJSONPartial(r) {
			writeLinesJSON(w, data.Lines)
		} else {
			writeLines(w, data.Lines)
		}
		return
	}
	render(w, r, data)
}

func handleOverview(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/statistics" {
		http.NotFound(w, r)
		return
	}

	days, err := listDays()
	data := PageData{Days: days, Overview: true}
	if err != nil {
		data.Error = err.Error()
	}
	if len(days) > 0 {
		dashboard, dashErr := dashboardData(days)
		if dashErr != nil && data.Error == "" {
			data.Error = dashErr.Error()
		}
		data.LatestDay = dashboard.LatestDay
		data.DayCount = dashboard.DayCount
		data.DeviceCount = dashboard.DeviceCount
		data.Devices = dashboard.Devices
	}
	render(w, r, data)
}

func handleAPIStats(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/stats" {
		http.NotFound(w, r)
		return
	}

	days, err := listDays()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	snapshot, err := buildStatsSnapshot(days)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeAPIStatsJSON(w, snapshot)
}

func handleAPIOverview(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/overview" {
		http.NotFound(w, r)
		return
	}

	days, err := listDays()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	snapshot, err := buildStatsSnapshot(days)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	dashboard := DashboardData{
		DayCount:    formatInt(snapshot.dayCount),
		DeviceCount: formatInt(snapshot.deviceCount),
	}
	if len(days) > 0 {
		dashboard, err = dashboardData(days)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	writeOverviewJSON(w, overviewPayloadFromData(snapshot, dashboard))
}

func serveFavicon(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/favicon.ico" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, faviconPath)
}

func serveAppleTouchIcon(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/apple-touch-icon.png" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, appleIconPath)
}

func handleDay(w http.ResponseWriter, r *http.Request) {
	day := strings.TrimPrefix(r.URL.Path, "/day/")
	if !validDayPath(day) {
		http.NotFound(w, r)
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	severity, severityOK := normalizeSeverityName(r.URL.Query().Get("level"))
	if raw := strings.TrimSpace(r.URL.Query().Get("level")); raw != "" && !severityOK {
		http.Error(w, "invalid severity level", http.StatusBadRequest)
		return
	}
	file := strings.TrimSpace(r.URL.Query().Get("file"))
	if file != "" && !validName(file) {
		http.NotFound(w, r)
		return
	}

	before, beforeErr := parseBeforeOffset(r)
	since, sinceErr := parseSinceCursor(r)
	filter := logFilter{Query: query, Severity: severity}
	if wantsLogPartial(r) {
		if beforeErr != nil {
			http.Error(w, beforeErr.Error(), http.StatusBadRequest)
			return
		}
		if sinceErr != nil {
			http.Error(w, sinceErr.Error(), http.StatusBadRequest)
			return
		}
		if wantsJSONPartial(r) && before < 0 && since > 0 {
			appended, cursor, replace, appendErr := stateIndex.dayAppends(appNow(), day, file, filter, since)
			if appendErr != nil {
				http.Error(w, appendErr.Error(), http.StatusInternalServerError)
				return
			}
			if replace {
				lines, start, total, scanErr := readDayWindow(day, file, filter, before, dayChunkSize)
				if scanErr != nil {
					http.Error(w, scanErr.Error(), http.StatusInternalServerError)
					return
				}
				writeLogPayloadJSON(w, logPayload{
					Lines:         lines,
					Start:         start,
					Total:         total,
					HasMoreBefore: start > 0,
					Cursor:        cursor,
					Replace:       true,
				})
				return
			}
			writeLogPayloadJSON(w, logPayload{
				Lines:  appended,
				Cursor: cursor,
			})
			return
		}
		lines, start, total, scanErr := readDayWindow(day, file, filter, before, dayChunkSize)
		if scanErr != nil {
			http.Error(w, scanErr.Error(), http.StatusInternalServerError)
			return
		}
		if wantsJSONPartial(r) {
			writeLogPayloadJSON(w, logPayload{
				Lines:         lines,
				Start:         start,
				Total:         total,
				HasMoreBefore: start > 0,
			})
		} else {
			writeLines(w, lines)
		}
		return
	}

	days, err := listDays()
	files, fileErr := listFiles(day)
	lines, start, total, scanErr := readDayWindow(day, file, filter, before, dayChunkSize)

	data := PageData{
		Days:          days,
		Selected:      day,
		Files:         files,
		File:          file,
		Query:         query,
		Severity:      severity,
		Lines:         lines,
		OlderURL:      logRefreshURL(r),
		ChunkStart:    start,
		TotalLogLines: total,
		HasOlder:      start > 0,
	}
	if cursor, cursorErr := stateIndex.dayCursor(appNow(), day, file); cursorErr == nil {
		data.RefreshCursor = cursor
	} else if data.Error == "" {
		data.Error = cursorErr.Error()
	}
	if err != nil {
		data.Error = err.Error()
	} else if fileErr != nil {
		data.Error = fileErr.Error()
	} else if beforeErr != nil {
		data.Error = beforeErr.Error()
	} else if sinceErr != nil {
		data.Error = sinceErr.Error()
	} else if scanErr != nil {
		data.Error = scanErr.Error()
	}
	data.ResultInfo = resultInfo(total, filter)
	render(w, r, data)
}

func handleSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	days, err := listDays()
	results := searchResults{}
	var scanErr error
	if scope == "week" {
		results, scanErr = searchRecentDays(query, 7)
	} else {
		results, scanErr = searchAll(query)
	}

	data := PageData{
		Days:       days,
		Query:      query,
		Lines:      results.lines,
		Global:     true,
		ResultInfo: resultInfo(len(results.lines), logFilter{Query: query}),
	}
	if results.limited {
		data.ResultInfo += " (showing newest results; limit 5,000)"
	}
	if err != nil {
		data.Error = err.Error()
	} else if scanErr != nil {
		data.Error = scanErr.Error()
	}
	render(w, r, data)
}

func render(w http.ResponseWriter, r *http.Request, data PageData) {
	addStats(r, &data)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := page.Execute(w, data); err != nil {
		log.Printf("render error: %v", err)
	}
}

func wantsLogPartial(r *http.Request) bool {
	return r.URL.Query().Get("partial") == "logs"
}

func wantsStatsPartial(r *http.Request) bool {
	return r.URL.Query().Get("partial") == "stats" && r.URL.Query().Get("format") == "json"
}

func wantsJSONPartial(r *http.Request) bool {
	return wantsLogPartial(r) && r.URL.Query().Get("format") == "json"
}

func parseBeforeOffset(r *http.Request) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("before"))
	if raw == "" {
		return -1, nil
	}
	before, err := strconv.Atoi(raw)
	if err != nil || before < 0 {
		return 0, fmt.Errorf("invalid before offset")
	}
	return before, nil
}

func parseSinceCursor(r *http.Request) (int64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("since"))
	if raw == "" {
		return 0, nil
	}
	since, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || since < 0 {
		return 0, fmt.Errorf("invalid since cursor")
	}
	return since, nil
}

func writeLines(w http.ResponseWriter, lines []string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	for _, line := range lines {
		_, _ = w.Write([]byte(line + "\n"))
	}
}

func writeLinesJSON(w http.ResponseWriter, lines []string) {
	writeLogPayloadJSON(w, logPayload{Lines: lines})
}

func writeLogPayloadJSON(w http.ResponseWriter, payload logPayload) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeStatsJSON(w http.ResponseWriter, snapshot statsSnapshot) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	payload := statsPayloadFromSnapshot(snapshot)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeAPIStatsJSON(w http.ResponseWriter, snapshot statsSnapshot) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	payload := apiStatsPayload{
		Critical5m:     snapshot.critical5m,
		TodayLines:     snapshot.todayLines,
		AllLines:       snapshot.allLines,
		LogBytes:       snapshot.totalLogSize,
		LinesPerSecond: snapshot.linesPerSecond,
		LogDays:        snapshot.dayCount,
		Devices:        snapshot.deviceCount,
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeOverviewJSON(w http.ResponseWriter, payload overviewPayload) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func logRefreshURL(r *http.Request) string {
	values := r.URL.Query()
	values.Set("partial", "logs")
	values.Set("format", "json")
	if encoded := values.Encode(); encoded != "" {
		return r.URL.Path + "?" + encoded
	}
	return r.URL.Path + "?partial=logs&format=json"
}

func syslogEndpoint(r *http.Request) string {
	host := r.Host
	if host == "" {
		return ":514"
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "" {
		return ":514"
	}
	return host + ":514"
}
