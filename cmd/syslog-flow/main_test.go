package main

import (
	"container/heap"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDayRecordStreamsMergeInReceiveOrder(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.log")
	second := filepath.Join(dir, "second.log")
	if err := os.WriteFile(first, []byte("2026-01-01T00:00:01Z | one\n2026-01-01T00:00:03Z | three\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("2026-01-01T00:00:02Z | two\n2026-01-01T00:00:04Z | four\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	paths := []struct {
		path  string
		label string
	}{{first, "first"}, {second, "second"}}
	streams := make([]*dayRecordStream, 0, len(paths))
	queue := make(dayRecordStreamHeap, 0, len(paths))
	for _, path := range paths {
		stream, err := newDayRecordStream(path.path, path.label, logFilter{})
		if err != nil {
			t.Fatal(err)
		}
		streams = append(streams, stream)
		if stream.next() {
			queue = append(queue, stream)
		}
	}
	defer closeDayRecordStreams(streams)

	heap.Init(&queue)
	var got []string
	for queue.Len() > 0 {
		stream := heap.Pop(&queue).(*dayRecordStream)
		got = append(got, visibleLogText(strings.TrimPrefix(stream.record.line, stream.label+"  ")))
		if stream.next() {
			heap.Push(&queue, stream)
		} else if stream.err != nil {
			t.Fatal(stream.err)
		}
	}

	want := []string{"one", "two", "three", "four"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged records = %q, want %q", got, want)
	}
}

func TestLogRecordHeapKeepsNewestRecords(t *testing.T) {
	records := make(logRecordHeap, 0, 2)
	heap.Init(&records)
	parseTime := func(value string) time.Time {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	for _, record := range []logRecord{
		{line: "old", at: parseTime("2026-01-01T00:00:01Z")},
		{line: "middle", at: parseTime("2026-01-01T00:00:02Z")},
		{line: "new", at: parseTime("2026-01-01T00:00:03Z")},
	} {
		if records.Len() < 2 {
			heap.Push(&records, record)
			continue
		}
		if records[0].at.Before(record.at) {
			records[0] = record
			heap.Fix(&records, 0)
		}
	}
	sortLogRecords([]logRecord(records))
	if got := recordLines([]logRecord(records)); !reflect.DeepEqual(got, []string{"middle", "new"}) {
		t.Fatalf("bounded records = %q", got)
	}
}

func TestDaySummaryUsesAllFiles(t *testing.T) {
	files := []LogFile{{Name: "router.log", Size: 10 * 1024 * 1024}, {Name: "switch.log", Size: 2 * 1024 * 1024}}
	if got, want := daySummary(files, 68859), "68,859 lines - 12.0MB"; got != want {
		t.Fatalf("day summary = %q, want %q", got, want)
	}
}

func TestTopDaySummariesReturnsBusiestDays(t *testing.T) {
	got := topDaySummaries(map[string]int{
		"2026/05/01": 2,
		"2026/05/02": 9,
		"2026/05/03": 6,
		"2026/05/04": 8,
		"2026/05/05": 3,
		"2026/05/06": 7,
	}, 5)
	if names := []string{got[0].Name, got[1].Name, got[2].Name, got[3].Name, got[4].Name}; !reflect.DeepEqual(names, []string{"2026/05/02", "2026/05/04", "2026/05/06", "2026/05/03", "2026/05/05"}) {
		t.Fatalf("top days = %q", names)
	}
}

func TestClampHomepageLines(t *testing.T) {
	if got := clampHomepageLines(0, 200); got != 200 {
		t.Fatalf("zero homepage lines = %d", got)
	}
	if got := clampHomepageLines(6000, 200); got != 5000 {
		t.Fatalf("large homepage lines = %d", got)
	}
}
