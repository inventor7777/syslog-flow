package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIndexResetChangesGenerationWithNumbering(t *testing.T) {
	index := &logIndex{}
	index.reset()
	startup := index.generation
	if startup == "" {
		t.Fatal("startup must create a generation")
	}
	now := appNow()
	index.lastRefresh = now
	if _, err := index.currentStats(now); err != nil {
		t.Fatal(err)
	}
	if index.generation != startup {
		t.Fatal("ordinary polling must preserve the generation")
	}
	index.files["old"] = &indexedFile{}
	index.nextSeq = 100
	index.reset() // Midnight uses this same reset as startup.
	if index.generation == startup || index.generation == "" || index.nextSeq != 0 || len(index.files) != 0 {
		t.Fatal("a numbering reset must replace the generation and clear the old index")
	}
	restarted := &logIndex{}
	restarted.reset()
	if restarted.generation == startup || restarted.generation == index.generation {
		t.Fatal("a restarted process must have a different generation")
	}
}

func TestRefreshFileWaitsForCompleteRecords(t *testing.T) {
	now := appNow()
	path := filepath.Join(t.TempDir(), "router.log")
	first := "2026-10-20T10:00:00Z | first\r\n"
	second := "2026-10-20T10:00:01Z | second\n"
	contents := first + "\n" + second
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	index := &logIndex{files: make(map[string]*indexedFile)}
	for step, size := range []int{len(first) - 1, len(first) + 1, len(contents) - 3, len(contents)} {
		file := discoveredFile{path: path, name: "router.log", day: now.Format("2006/01/02"), size: int64(size), modTime: now.Add(time.Duration(step) * time.Second)}
		if err := index.refreshFile(file, defaultAppConfig(), now); err != nil {
			t.Fatal(err)
		}
		want := []int{0, 1, 1, 2}[step]
		if got := index.files[path].lineCount; got != want {
			t.Fatalf("step %d: indexed %d lines, want %d", step, got, want)
		}
	}
	file := index.files[path]
	if file.readOffset != int64(len(contents)) || file.size != int64(len(contents)) {
		t.Fatalf("offset = %d, size = %d", file.readOffset, file.size)
	}
	if len(file.tail) != 2 || !strings.HasSuffix(file.tail[0].line, " | first") || !strings.HasSuffix(file.tail[1].line, " | second") {
		t.Fatalf("tail = %#v", file.tail)
	}
	if file.physicalLines != 3 || file.tail[0].lineNumber != 1 || file.tail[1].lineNumber != 3 {
		t.Fatalf("physical line positions lost across partial reads: %#v", file.tail)
	}
}

func TestDaySnapshotKeepsWindowAndCursorTogether(t *testing.T) {
	for _, query := range []string{"", "UPS", "nut|ups"} {
		for _, file := range []string{"", "router.log"} {
			for _, appendBeforeRead := range []bool{false, true} {
				now := appNow()
				day := now.Format("2006/01/02")
				path := filepath.Join(t.TempDir(), "router.log")
				first := actionLine(now, "router", "err", "UPS first")
				second := actionLine(now.Add(time.Second), "router", "err", "UPS second")
				contents := first + strings.TrimSuffix(second, "\n")
				index := &logIndex{generation: "snapshot-test", lastRefresh: now, files: make(map[string]*indexedFile)}
				refresh := func(step int) {
					t.Helper()
					if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
						t.Fatal(err)
					}
					if err := index.refreshFile(discoveredFile{path: path, day: day, name: "router.log", size: int64(len(contents)), modTime: now.Add(time.Duration(step) * time.Second)}, defaultAppConfig(), now); err != nil {
						t.Fatal(err)
					}
				}
				refresh(0)
				filter, err := newLogFilter(query, "", query == "nut|ups")
				if err != nil {
					t.Fatal(err)
				}
				files, snapshot, err := index.dayScanSnapshot(now, day, file)
				if err != nil {
					t.Fatal(err)
				}
				if len(files) != 1 || files[0].size != int64(len(first)) || snapshot.cursor != 1 {
					t.Fatalf("snapshot must stop at the indexed complete line: %+v %+v", files, snapshot)
				}
				appendLines := func() {
					contents += "\n" + actionLine(now.Add(2*time.Second), "router", "err", "UPS third")
					refresh(1)
				}
				if appendBeforeRead {
					appendLines()
				}
				window, err := readDaySnapshot(files, snapshot, filter, -1, dayChunkSize)
				if err != nil {
					t.Fatal(err)
				}
				if !appendBeforeRead {
					appendLines()
				}
				if len(window.lines) != 1 || !strings.Contains(window.lines[0], "UPS first") || window.cursor != 1 || window.total != 1 || window.generation != "snapshot-test" {
					t.Fatalf("later writes changed the window or cursor: %+v", window)
				}
				payload, err := index.dayAppends(now, day, file, filter, window.cursor)
				if err != nil || payload.Cursor != 3 || len(payload.Lines) != 2 || !strings.Contains(payload.Lines[0], "UPS second") || !strings.Contains(payload.Lines[1], "UPS third") {
					t.Fatalf("later writes must arrive exactly once on the next poll: %+v, %v", payload, err)
				}
				caughtUp, err := index.dayAppends(now, day, file, filter, payload.Cursor)
				if err != nil || len(caughtUp.Lines) != 0 {
					t.Fatalf("caught-up poll repeated entries: %+v, %v", caughtUp, err)
				}
			}
		}
	}
}

func TestArchivedDaySnapshotUsesCachedFileSize(t *testing.T) {
	now := appNow()
	day := now.AddDate(0, 0, -1).Format("2006/01/02")
	path := filepath.Join(t.TempDir(), "router.log")
	contents := actionLine(now.AddDate(0, 0, -1), "router", "err", "UPS archived")
	appendActionLog(t, path, contents)
	index := &logIndex{generation: "archive-test", lastRefresh: now, files: map[string]*indexedFile{
		path: {path: path, day: day, name: "router.log", size: int64(len(contents)), lineCount: 1},
	}}
	files, snapshot, err := index.dayScanSnapshot(now, day, "")
	if err != nil {
		t.Fatal(err)
	}
	window, err := readDaySnapshot(files, snapshot, logFilter{Query: "UPS"}, -1, dayChunkSize)
	if err != nil || len(window.lines) != 1 || window.total != 1 || window.cursor != 0 {
		t.Fatalf("archive metadata must retain readable logs without a live cursor: %+v, %v", window, err)
	}
}

func TestDayAppendsDetectsBusyDeviceGap(t *testing.T) {
	now := appNow()
	day := now.Format("2006/01/02")
	index := &logIndex{lastRefresh: now, files: make(map[string]*indexedFile)}
	dir := t.TempDir()
	refresh := func(name, contents string, step int) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		file := discoveredFile{path: path, day: day, name: name, size: int64(len(contents)), modTime: now.Add(time.Duration(step) * time.Second)}
		if err := index.refreshFile(file, defaultAppConfig(), now); err != nil {
			t.Fatal(err)
		}
	}
	refresh("quiet.log", "quiet\n", 0)
	refresh("busy.log", "busy\n", 0)
	since := index.nextSeq
	refresh("busy.log", strings.Repeat("busy\n", 2102), 1)
	busy := index.files[filepath.Join(dir, "busy.log")]
	if busy.discardedThrough <= since {
		t.Fatal("test did not overflow the busy device tail")
	}
	for _, tc := range []struct {
		name    string
		file    string
		since   int64
		replace bool
	}{
		{"all devices with gap", "", since, true},
		{"busy device with gap", "busy.log", since, true},
		{"quiet device unaffected", "quiet.log", since, false},
		{"last discarded record already seen", "", busy.discardedThrough, false},
		{"caught up", "", index.nextSeq, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := index.dayAppends(now, day, tc.file, logFilter{}, tc.since)
			if err != nil || payload.Replace != tc.replace {
				t.Fatalf("replace = %t, want %t; error = %v", payload.Replace, tc.replace, err)
			}
		})
	}
}
