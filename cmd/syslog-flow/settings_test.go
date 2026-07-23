package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatSettingsJSON(t *testing.T) {
	formatted, err := formatSettingsJSON(`{"live_refresh_seconds":2}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"live_refresh_seconds\": 2\n}\n"
	if string(formatted) != want {
		t.Fatalf("formatted JSON = %q, want %q", formatted, want)
	}
}

func TestValidateSettingsFormatRejectsInvalidValues(t *testing.T) {
	if err := validateSettingsFormat("status-colors", []byte(`{"info":123}`)); err == nil {
		t.Fatal("expected invalid status color value to be rejected")
	}
	if err := validateSettingsFormat("system", []byte(`[]`)); err == nil {
		t.Fatal("expected non-object system settings to be rejected")
	}
}

func TestWriteSettingsFileAtomicallyReplacesContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeSettingsFile(path, []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new\n" {
		t.Fatalf("settings file = %q", data)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".syslog-flow-settings-") {
			t.Fatalf("temporary settings file %q was left behind", entry.Name())
		}
	}
}
