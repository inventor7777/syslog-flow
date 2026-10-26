package main

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

func searchParameters(values url.Values) (PageData, error) {
	data := PageData{Global: true, Query: strings.TrimSpace(values.Get("q")), File: strings.TrimSpace(values.Get("file")), SearchStart: values.Get("start"), SearchEnd: values.Get("end")}
	for _, date := range []string{data.SearchStart, data.SearchEnd} {
		if date != "" {
			if _, err := time.Parse("2006-01-02", date); err != nil {
				return data, fmt.Errorf("invalid search date; use YYYY-MM-DD")
			}
		}
	}
	if data.SearchStart != "" && data.SearchEnd != "" && data.SearchStart > data.SearchEnd {
		return data, fmt.Errorf("start date must be on or before end date")
	}
	if data.File != "" && !validName(data.File) {
		return data, fmt.Errorf("invalid device file")
	}
	if raw := strings.TrimSpace(values.Get("level")); raw != "" {
		var ok bool
		data.Severity, ok = normalizeSeverityName(raw)
		if !ok {
			return data, fmt.Errorf("invalid severity level")
		}
	}
	// Older search forms submitted scope twice; the clicked button was last.
	if scopes := values["scope"]; len(scopes) > 0 {
		data.SearchScope = strings.TrimSpace(scopes[len(scopes)-1])
	}
	if data.SearchScope != "" && data.SearchScope != "all" && data.SearchScope != "week" {
		return data, fmt.Errorf("invalid search scope")
	}
	return data, nil
}

func searchDateDays(days []Day, data PageData) []Day {
	if data.SearchScope == "week" && len(days) > 7 {
		days = days[:7]
	}
	selected := make([]Day, 0, len(days))
	for _, day := range days {
		date := strings.ReplaceAll(day.Name, "/", "-")
		if (data.SearchStart == "" || date >= data.SearchStart) && (data.SearchEnd == "" || date <= data.SearchEnd) {
			selected = append(selected, day)
		}
	}
	return selected
}

func searchDevices(days []Day) []LogFile {
	names := make(map[string]bool)
	for _, day := range days {
		for _, file := range day.Files {
			names[file.Name] = true
		}
	}
	devices := make([]LogFile, 0, len(names))
	for name := range names {
		devices = append(devices, LogFile{Name: name})
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].Name < devices[j].Name })
	return devices
}

func (data PageData) SearchURL(file, severity string) string {
	values := url.Values{}
	for key, value := range map[string]string{"q": data.Query, "start": data.SearchStart, "end": data.SearchEnd, "scope": data.SearchScope, "file": file, "level": severity} {
		if value != "" {
			values.Set(key, value)
		}
	}
	return "/search?" + values.Encode()
}

func (data PageData) SearchDialogData() PageData {
	if !data.Global {
		now := appNow()
		data.SearchStart = now.AddDate(0, 0, -7).Format("2006-01-02")
		data.SearchEnd = now.Format("2006-01-02")
	}
	return data
}

const searchDialogTemplate = `
{{define "search-dialog"}}
<style>
  .open-search { align-items: center; background: var(--accent); border: 0; border-radius: .55rem; color: white; cursor: pointer; display: inline-flex; font: inherit; font-weight: 700; gap: .5rem; justify-content: center; padding: .65rem .9rem; width: 100%; }
  .open-search svg { fill: currentColor; height: 1.45rem; width: 1.45rem; }
  .search-dialog { background: var(--panel); border: 1px solid var(--line); border-radius: 12px; box-shadow: 0 20px 70px rgba(0,0,0,.3); color: var(--ink); max-height: 90dvh; padding: 0; width: min(560px,calc(100% - 24px)); }
  .search-dialog::backdrop { background: rgba(0,0,0,.55); }
  .search-dialog form { display: block; margin: 0; }
  .search-dialog-head,.search-dialog-footer { align-items: center; display: flex; gap: .75rem; padding: 1rem 1.25rem; }
  .search-dialog-head { border-bottom: 1px solid var(--line); }
  .search-dialog-head h3 { flex: 1; margin: 0; }
  .search-dialog-body { background: var(--bg); padding: 1.25rem; }
  .search-dialog label { display: block; font-weight: 650; margin: 0 0 .4rem; }
  .search-dialog input { display: block; font: inherit; min-width: 0; width: 100%; }
  .search-date-fields { border-top: 1px solid var(--line); display: grid; gap: 1rem; grid-template-columns: repeat(2,minmax(0,1fr)); margin-top: 1.25rem; padding-top: 1.25rem; }
  .search-dialog-footer { border-top: 1px solid var(--line); justify-content: flex-end; }
  .search-dialog .search-cancel { background: var(--panel-soft); border: 1px solid var(--line); color: var(--ink); }
  .search-dialog button:focus-visible,.open-search:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
  @media(max-width: 450px) { .search-date-fields { grid-template-columns: 1fr; } }
</style>
<dialog class="search-dialog" id="search-dialog" aria-labelledby="search-dialog-title">
  <form action="/search" method="get" data-loading-submit>
    <div class="search-dialog-head"><h3 id="search-dialog-title">Search logs</h3><button type="button" class="search-cancel" data-close-search aria-label="Close search">×</button></div>
    <div class="search-dialog-body">
      <label for="search-query">Contains text</label>
      <input id="search-query" name="q" value="{{.Query}}" placeholder="Any message" autofocus>
      <div class="search-date-fields">
        <div><label for="search-start">From date</label><input id="search-start" type="date" name="start" value="{{.SearchStart}}"></div>
        <div><label for="search-end">Through date</label><input id="search-end" type="date" name="end" value="{{.SearchEnd}}"></div>
      </div>
      {{if .Global}}{{if .File}}<input type="hidden" name="file" value="{{.File}}">{{end}}{{if .Severity}}<input type="hidden" name="level" value="{{.Severity}}">{{end}}{{end}}
    </div>
    <div class="search-dialog-footer"><button type="submit">Search</button></div>
  </form>
</dialog>
<script>
(() => {
  const dialog = document.getElementById("search-dialog");
  document.querySelectorAll("[data-open-search]").forEach(button => button.addEventListener("click", () => dialog.showModal()));
  dialog.querySelectorAll("[data-close-search]").forEach(button => button.addEventListener("click", () => dialog.close()));
  const start = document.getElementById("search-start");
  const end = document.getElementById("search-end");
  const validateDates = () => end.setCustomValidity(start.value && end.value && start.value > end.value ? "Choose an end date on or after the start date." : "");
  start.addEventListener("input", validateDates);
  end.addEventListener("input", validateDates);
})();
</script>
{{end}}
`
