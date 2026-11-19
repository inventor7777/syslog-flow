package main

import (
	"bytes"
	"encoding/json"
	"html"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFilteredDayLiveUpdates(t *testing.T) {
	previous := stateIndex
	t.Cleanup(func() { stateIndex = previous })
	for _, file := range []string{"", "router.log"} {
		for _, regex := range []bool{false, true} {
			now := appNow()
			day := now.Format("2006/01/02")
			stateIndex = &logIndex{generation: "filtered-live", lastRefresh: now, files: make(map[string]*indexedFile)}
			path := filepath.Join(t.TempDir(), "router.log")
			contents := actionLine(now, "router", "err", "initial entry")
			step := 0
			refresh := func() {
				t.Helper()
				if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
				step++
				if err := stateIndex.refreshFile(discoveredFile{path: path, day: day, name: "router.log", size: int64(len(contents)), modTime: now.Add(time.Duration(step) * time.Second)}, defaultAppConfig(), now); err != nil {
					t.Fatal(err)
				}
				stateIndex.lastRefresh = appNow()
			}
			refresh()
			query := "UPS"
			if regex {
				query = "nut|ups"
			}
			data := PageData{Selected: day, Query: query, Regex: regex, File: file, Severity: "err"}
			request := httptest.NewRequest("GET", data.DayURL(file, "err"), nil)
			response := httptest.NewRecorder()
			render(response, request, data)
			refreshURL := logRefreshURL(request)
			if !strings.Contains(html.UnescapeString(response.Body.String()), `data-refresh-url="`+refreshURL+`"`) {
				t.Fatalf("filtered page missing polling URL: file=%q regex=%v", file, regex)
			}
			values, _ := url.Parse(refreshURL)
			params := values.Query()
			params.Set("since", "1")
			contents += actionLine(now.Add(time.Second), "router", "info", "UPS wrong severity") +
				actionLine(now.Add(2*time.Second), "router", "err", "unrelated entry") +
				actionLine(now.Add(3*time.Second), "router", "err", "UPS on battery")
			refresh()
			poll := func() logPayload {
				t.Helper()
				response := httptest.NewRecorder()
				handleDay(response, httptest.NewRequest("GET", values.Path+"?"+params.Encode(), nil))
				var payload logPayload
				if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || response.Code != 200 {
					t.Fatalf("poll failed: %d %s: %v", response.Code, response.Body.String(), err)
				}
				return payload
			}
			payload := poll()
			if len(payload.Lines) != 1 || !strings.Contains(payload.Lines[0], "UPS on battery") || payload.Cursor != 4 || payload.Replace {
				t.Fatalf("filtered append failed: %+v", payload)
			}
			params.Set("since", "4")
			contents += actionLine(now.Add(4*time.Second), "router", "err", "unrelated new entry")
			refresh()
			payload = poll()
			if len(payload.Lines) != 0 || payload.Cursor != 5 {
				t.Fatalf("nonmatching entries must advance cursor without replaying matches: %+v", payload)
			}
		}
	}
}

func TestRegexControlsAndValidation(t *testing.T) {
	for _, data := range []PageData{{}, {Selected: "2026/11/18"}, {Selected: "2026/11/18", File: "router.log"}, {Global: true}} {
		for _, regex := range []bool{false, true} {
			data.Regex = regex
			var output bytes.Buffer
			if err := page.Execute(&output, data); err != nil {
				t.Fatal(err)
			}
			body := output.String()
			if strings.Count(body, `name="q"`) != strings.Count(body, `type="checkbox" name="regex" value="1"`) {
				t.Fatal("every search field must have a regex checkbox")
			}
			checked := `name="regex" value="1" checked`
			if strings.Contains(body, checked) != regex {
				t.Fatal("regex checkbox must preserve selected mode")
			}
		}
	}
	for _, path := range []string{"/search?q=%5B&regex=1", "/day/2026/11/18?q=%5B&regex=1", "/day/2026/11/18?q=%5B&regex=1&partial=logs&format=json&since=1"} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest("GET", path, nil)
		if request.URL.Path == "/search" {
			handleSearch(response, request)
		} else {
			handleDay(response, request)
		}
		if response.Code != 400 || !strings.Contains(response.Body.String(), "invalid regular expression") {
			t.Fatalf("invalid pattern must return a clear error: %d %s", response.Code, response.Body.String())
		}
	}
}

func TestPagesAndPollsCarryIndexGeneration(t *testing.T) {
	previous := stateIndex
	t.Cleanup(func() { stateIndex = previous })
	now := appNow()
	stateIndex = &logIndex{generation: "current-generation", lastRefresh: now, files: make(map[string]*indexedFile)}
	day := now.Format("2006/01/02")
	for _, path := range []string{"/", "/statistics", "/day/" + day} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest("GET", path, nil)
		switch path {
		case "/":
			handleIndex(response, request)
		case "/statistics":
			handleOverview(response, request)
		default:
			handleDay(response, request)
		}
		if response.Code != 200 || !strings.Contains(response.Body.String(), `data-generation="current-generation"`) {
			t.Fatalf("%s: page missing generation (status %d)", path, response.Code)
		}
	}
	for _, path := range []string{
		"/?partial=logs&format=json",
		"/?partial=stats&format=json",
		"/day/" + day + "?partial=logs&format=json&since=1",
		"/day/" + day + "?partial=logs&format=json&before=0",
		"/api/overview",
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest("GET", path, nil)
		switch request.URL.Path {
		case "/":
			handleIndex(response, request)
		case "/api/overview":
			handleAPIOverview(response, request)
		default:
			handleDay(response, request)
		}
		var payload struct{ Generation string }
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatalf("%s: %v; body: %s", path, err, response.Body.String())
		}
		if response.Code != 200 || payload.Generation != "current-generation" {
			t.Fatalf("%s: generation = %q (status %d)", path, payload.Generation, response.Code)
		}
	}
}

func TestEmptyHomepageRendersLiveViewer(t *testing.T) {
	var output bytes.Buffer
	if err := page.Execute(&output, PageData{Live: true, RefreshURL: "/?partial=logs&format=json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `id="log-viewer"`) || !strings.Contains(output.String(), `data-refresh-url=`) {
		t.Fatal("empty homepage must render a viewer with polling enabled")
	}
}

func TestViewerLiveUpdates(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is needed to run the viewer JavaScript regression checks")
	}
	command := exec.Command(node, "testdata/viewer_test.cjs")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("viewer checks: %v\n%s", err, output)
	}
}

func TestDayDeviceLinksPreserveSeverityAndQuery(t *testing.T) {
	var output bytes.Buffer
	if err := page.Execute(&output, PageData{
		Selected: "2026/10/20", File: "router.log", Severity: "crit", Query: "disk & error",
		Files: []LogFile{{Name: "router.log"}, {Name: "2001:db8::1.log"}},
	}); err != nil {
		t.Fatal(err)
	}
	body := html.UnescapeString(output.String())
	for _, link := range []string{
		"/day/2026/10/20?level=crit&q=disk+%26+error",
		"/day/2026/10/20?file=router.log&level=crit&q=disk+%26+error",
		"/day/2026/10/20?file=2001%3Adb8%3A%3A1.log&level=crit&q=disk+%26+error",
	} {
		if !strings.Contains(body, `href="`+link+`"`) {
			t.Fatalf("device link lost a filter: %s", link)
		}
	}
}

func TestDayLinksPreserveRegexMode(t *testing.T) {
	for _, query := range []string{"nut|ups", ""} {
		data := PageData{Selected: "2026/11/18", Query: query, Regex: true, File: "router.log", Severity: "err", Files: []LogFile{{Name: "router.log"}, {Name: "nas.log"}}}
		var output bytes.Buffer
		if err := page.Execute(&output, data); err != nil {
			t.Fatal(err)
		}
		body := html.UnescapeString(output.String())
		for _, selection := range [][2]string{{"", "err"}, {"nas.log", "err"}, {"router.log", ""}, {"router.log", "crit"}} {
			link, err := url.Parse(data.DayURL(selection[0], selection[1]))
			if err != nil {
				t.Fatal(err)
			}
			values := link.Query()
			if values.Get("q") != query || values.Get("regex") != "1" || values.Get("file") != selection[0] || values.Get("level") != selection[1] || !strings.Contains(body, `href="`+link.String()+`"`) {
				t.Fatalf("day link lost filters: %s", link)
			}
		}
	}
}
