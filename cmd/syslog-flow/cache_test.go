package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanDayCacheFileSummarizesLogs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "router.log")
	contents := "2026-05-05T10:00:00Z | first\n\n2026-05-05T10:02:00Z | second\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	summary, err := scanDayCacheFile(discoveredFile{path: path, name: "router.log", size: info.Size(), modTime: info.ModTime()})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Lines != 2 || summary.Size != int64(len(contents)) {
		t.Fatalf("summary = %#v", summary)
	}
	wantLastSeen, err := time.Parse(time.RFC3339, "2026-05-05T10:02:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if !summary.LastSeen.Equal(wantLastSeen) {
		t.Fatalf("last seen = %s, want %s", summary.LastSeen, wantLastSeen)
	}
}

func TestApplyDayCacheReplacesOnlyTheCachedDay(t *testing.T) {
	index := &logIndex{files: map[string]*indexedFile{
		"old":   {path: "old", day: "2026/05/04", name: "old.log"},
		"today": {path: "today", day: "2026/05/05", name: "today.log", tail: []indexedLine{{seq: 1}}},
	}}
	modTime := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	index.applyDayCache(dayCache{Day: "2026/05/04", Files: []dayCacheFile{{Name: "router.log", Size: 42, ModTime: modTime, Lines: 7, LastSeen: modTime}}})

	if len(index.files) != 2 || index.files["today"].tail[0].seq != 1 {
		t.Fatalf("unrelated index entries changed: %#v", index.files)
	}
	for _, file := range index.files {
		if file.day == "2026/05/04" && (file.name != "router.log" || file.lineCount != 7) {
			t.Fatalf("cached entry = %#v", file)
		}
	}
}
