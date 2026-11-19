package main

import "testing"

func TestLogFilterRegexAndLiteral(t *testing.T) {
	for _, tc := range []struct {
		query, severity, line string
		regex, want           bool
	}{
		{"nut|ups", "", "UPS on battery", true, true},
		{"nut|ups", "", "nut disconnected", true, true},
		{"nut|ups", "", "router connected", true, false},
		{"nut|ups", "", "UPS on battery", false, false},
		{"nut|ups", "", "literal NUT|UPS", false, true},
		{"192.168", "", "192x168", false, false},
		{"192.168", "", "192x168", true, true},
		{"[error]", "", "literal [ERROR]", false, true},
		{"disk?", "", "disk", false, false},
		{"^ups$", "", "UPS", true, true},
		{"^ups$", "", "UPS on battery", true, false},
		{"", "", "anything", true, true},
		{"nut|ups", "err", "2026-11-18T12:00:00Z router err UPS on battery", true, true},
		{"nut|ups", "err", "2026-11-18T12:00:00Z router info UPS on battery", true, false},
	} {
		filter, err := newLogFilter(tc.query, tc.severity, tc.regex)
		if err != nil {
			t.Fatal(err)
		}
		if got := matchesLogFilter(tc.line, filter); got != tc.want {
			t.Errorf("query=%q regex=%v severity=%q line=%q: got %v, want %v", tc.query, tc.regex, tc.severity, tc.line, got, tc.want)
		}
	}
	if _, err := newLogFilter("[", "", true); err == nil {
		t.Fatal("invalid regular expression accepted")
	}
	if _, err := newLogFilter("[", "", false); err != nil {
		t.Fatal("literal punctuation must remain valid")
	}
}
