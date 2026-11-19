package main

import (
	"bytes"
	"fmt"
	"html"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSearchDateParameters(t *testing.T) {
	days := []Day{{Name: "2026/10/20"}, {Name: "2026/10/19"}, {Name: "2026/10/18"}}
	for _, tc := range []struct {
		query string
		want  string
	}{
		{"", "2026/10/20,2026/10/19,2026/10/18"},
		{"start=2026-10-19&end=2026-10-20", "2026/10/20,2026/10/19"},
		{"start=2026-10-19&end=2026-10-19", "2026/10/19"},
		{"end=2026-10-19", "2026/10/19,2026/10/18"},
		{"start=2026-10-20", "2026/10/20"},
		{"start=2026-11-01", ""},
	} {
		values, _ := url.ParseQuery(tc.query)
		data, err := searchParameters(values)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, day := range searchDateDays(days, data) {
			names = append(names, day.Name)
		}
		if got := strings.Join(names, ","); got != tc.want {
			t.Fatalf("%s: got %s, want %s", tc.query, got, tc.want)
		}
	}
	for _, raw := range []string{"start=2026-02-30", "end=garbage", "start=2026-10-20&end=2026-10-19", "file=../router.log", "level=bogus", "scope=bogus"} {
		response := httptest.NewRecorder()
		handleSearch(response, httptest.NewRequest("GET", "/search?"+raw, nil))
		if response.Code != 400 {
			t.Fatalf("%s should be rejected, got %d", raw, response.Code)
		}
	}
	data, err := searchParameters(url.Values{"scope": {"week", "all"}})
	if err != nil || data.SearchScope != "all" {
		t.Fatal("legacy search buttons must use their last scope parameter")
	}
	var many []Day
	for i := 20; i >= 10; i-- {
		many = append(many, Day{Name: fmt.Sprintf("2026/10/%02d", i)})
	}
	if got := searchDateDays(many, PageData{SearchScope: "week"}); len(got) != 7 || got[6].Name != "2026/10/14" {
		t.Fatal("legacy week scope should retain seven available days")
	}
}

func TestSearchDialogDefaultDates(t *testing.T) {
	now := appNow()
	data := (PageData{}).SearchDialogData()
	if data.SearchStart != now.AddDate(0, 0, -7).Format("2006-01-02") || data.SearchEnd != now.Format("2006-01-02") {
		t.Fatalf("default dates should run from seven days ago through today: %+v", data)
	}
	for _, selected := range []PageData{{Global: true}, {Global: true, SearchStart: "2026-10-01", SearchEnd: "2026-10-03"}} {
		dialog := selected.SearchDialogData()
		if dialog.SearchStart != selected.SearchStart || dialog.SearchEnd != selected.SearchEnd {
			t.Fatal("reopening search should preserve selected dates, including unbounded searches")
		}
	}
}

func TestSearchCombinesDatesDeviceSeverityAndText(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2026, 10, 18, 12, 0, 0, 0, appLocation)
	var days []Day
	for i := 2; i >= 0; i-- {
		at := base.AddDate(0, 0, i)
		day := Day{Name: at.Format("2006/01/02"), Files: []LogFile{{Name: "router.log"}, {Name: "nas.log"}}}
		for _, device := range []string{"router", "nas"} {
			appendActionLog(t, filepath.Join(root, day.Name, device+".log"), actionLine(at, device, "err", "disk unavailable")+actionLine(at.Add(time.Second), device, "info", "disk available")+actionLine(at.Add(2*time.Second), device, "err", "network unavailable"))
		}
		days = append(days, day)
	}
	for _, tc := range []struct {
		query string
		count int
	}{
		{"start=2026-10-19&end=2026-10-20&file=router.log&level=err&q=DISK", 2},
		{"start=2026-10-19&end=2026-10-19&file=nas.log&level=info", 1},
		{"start=2026-10-19&end=2026-10-19", 6},
		{"level=err&q=disk", 6},
		{"level=err&q=DISK%7Cnetwork&regex=1", 12},
		{"level=err&q=DISK%7Cnetwork", 0},
		{"file=missing.log", 0},
	} {
		values, _ := url.ParseQuery(tc.query)
		data, err := searchParameters(values)
		if err != nil {
			t.Fatal(err)
		}
		selected := searchDateDays(days, data)
		filter, err := newLogFilter(data.Query, data.Severity, data.Regex)
		if err != nil {
			t.Fatal(err)
		}
		results, err := searchDays(root, selected, filter, data.File)
		if err != nil || len(results.lines) != tc.count {
			t.Fatalf("%s: got %d lines, want %d: %v", tc.query, len(results.lines), tc.count, err)
		}
		for _, line := range results.lines {
			if data.File != "" && !strings.Contains(line, "/"+data.File+"  ") {
				t.Fatalf("device filter leaked another file: %s", line)
			}
		}
	}
	devices := searchDevices(days)
	if len(devices) != 2 || devices[0].Name != "nas.log" || devices[1].Name != "router.log" {
		t.Fatalf("device choices must be deduplicated and sorted: %+v", devices)
	}
}

func TestSearchKeepsNewestMatchingResults(t *testing.T) {
	root := t.TempDir()
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, appLocation)
	var contents strings.Builder
	for i := 0; i < maxSearchResults+5; i++ {
		contents.WriteString(actionLine(at.Add(time.Duration(i)*time.Second), "router", "err", fmt.Sprintf("match %04d", i)))
	}
	contents.WriteString(actionLine(at.Add(24*time.Hour), "router", "info", "filtered out"))
	appendActionLog(t, filepath.Join(root, "2026/10/20/router.log"), contents.String())
	results, err := searchDays(root, []Day{{Name: "2026/10/20", Files: []LogFile{{Name: "router.log"}}}}, logFilter{Severity: "err"}, "")
	if err != nil || !results.limited || len(results.lines) != maxSearchResults || !strings.Contains(results.lines[0], "match 0005") || !strings.Contains(results.lines[len(results.lines)-1], "match 5004") {
		t.Fatalf("newest-result limit or filtering failed: count=%d, limited=%v, err=%v", len(results.lines), results.limited, err)
	}
	link, err := url.Parse(results.links[0])
	if err != nil || len(results.links) != len(results.lines) || link.Query().Get("line") != "6" {
		t.Fatal("source links must stay aligned with capped and sorted search results")
	}
}

func TestSearchLimitUsesPhysicalOrderForEqualTimestamps(t *testing.T) {
	root := t.TempDir()
	day, file := "2026/10/20", "2001:db8::1.log"
	raw := actionLine(appNow(), "2001:db8::1", "err", "identical event")
	appendActionLog(t, filepath.Join(root, day, file), strings.Repeat(raw, maxSearchResults+5))
	data, err := searchParameters(url.Values{"file": {file}})
	if err != nil {
		t.Fatal(err)
	}
	results, err := searchDays(root, []Day{{Name: day, Files: []LogFile{{Name: file}}}}, logFilter{}, data.File)
	if err != nil || !results.limited || len(results.links) != maxSearchResults {
		t.Fatalf("search limit failed: %v", err)
	}
	for _, i := range []int{0, len(results.links) - 1} {
		link, err := url.Parse(results.links[i])
		if err != nil || link.Query().Get("line") != fmt.Sprint(i+6) || link.Query().Get("file") != file {
			t.Fatalf("equal-timestamp source order lost: %s, %v", results.links[i], err)
		}
		if _, err := parseLogTarget(link.Query()); err != nil {
			t.Fatalf("IPv6 source link rejected: %v", err)
		}
	}
}

func TestSearchPagePreservesFilters(t *testing.T) {
	data := PageData{Global: true, Query: "disk & error", Regex: true, File: "router.log", Severity: "err", SearchStart: "2026-10-18", SearchEnd: "2026-10-20", SearchDevices: []LogFile{{Name: "nas.log"}, {Name: "router.log"}}}
	for _, selection := range [][2]string{{"nas.log", "err"}, {"router.log", "crit"}, {"", "err"}, {"router.log", ""}} {
		link, err := url.Parse(data.SearchURL(selection[0], selection[1]))
		if err != nil {
			t.Fatal(err)
		}
		values := link.Query()
		if values.Get("q") != data.Query || values.Get("regex") != "1" || values.Get("start") != data.SearchStart || values.Get("end") != data.SearchEnd || values.Get("file") != selection[0] || values.Get("level") != selection[1] {
			t.Fatalf("filter link lost selected values: %s", link)
		}
	}
	var output bytes.Buffer
	if err := page.Execute(&output, data); err != nil {
		t.Fatal(err)
	}
	body := html.UnescapeString(output.String())
	for _, expected := range []string{`id="search-dialog"`, `type="date" name="start"`, `type="date" name="end"`, `data-open-search`, `id="log-viewer"`, `aria-label="Filter by device"`, `aria-label="Filter by severity"`, data.SearchURL("nas.log", "err"), data.SearchURL("router.log", "crit")} {
		if !strings.Contains(body, expected) {
			t.Fatalf("search page missing %s", expected)
		}
	}
}

func TestSearchHandlerAllowsDateOnlySearch(t *testing.T) {
	previous := stateIndex
	stateIndex = &logIndex{generation: "search-test", lastRefresh: appNow(), files: make(map[string]*indexedFile)}
	t.Cleanup(func() { stateIndex = previous })
	response := httptest.NewRecorder()
	handleSearch(response, httptest.NewRequest("GET", "/search?start=2026-10-19&end=2026-10-20&file=router.log&level=err", nil))
	if response.Code != 200 || !strings.Contains(response.Body.String(), "0 matching lines") || !strings.Contains(response.Body.String(), "Global Search (2026-10-19 to 2026-10-20)") {
		t.Fatalf("date-only search should render results without redirecting: %d: %s", response.Code, response.Body.String())
	}
}
