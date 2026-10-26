package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormDialogsIgnoreOutsideClicks(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is needed for dialog interaction checks")
	}
	if output, err := exec.Command(node, "testdata/dialogs_test.cjs").CombinedOutput(); err != nil {
		t.Fatalf("dialog interaction checks: %v\n%s", err, output)
	}
}

func TestSettingsActionRequests(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is needed for settings request checks")
	}
	if output, err := exec.Command(node, "testdata/settings_test.cjs").CombinedOutput(); err != nil {
		t.Fatalf("settings request checks: %v\n%s", err, output)
	}
}

func TestDefaultSettingsJSONUsesConfigGeneratorDefaults(t *testing.T) {
	data, err := defaultSettingsJSON("status-colors")
	if err != nil {
		t.Fatal(err)
	}
	var colors map[string]string
	if err := json.Unmarshal(data, &colors); err != nil {
		t.Fatal(err)
	}
	if colors["info"] != "#A4AFA9" || colors["debug"] != "#747C8C" {
		t.Fatalf("status defaults = %#v", colors)
	}
}

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
	for _, section := range settingsSections {
		if section.Caches {
			continue
		}
		for _, value := range []string{"null", "[]", "123", `"text"`} {
			if err := validateSettingsFormat(section.ID, []byte(value)); err == nil {
				t.Errorf("%s accepted %s", section.ID, value)
			}
		}
	}
}

func TestMergeConfigFileRepairsNull(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.json")
	if err := os.WriteFile(path, []byte("null"), 0o600); err != nil {
		t.Fatal(err)
	}
	defaults := defaultAppConfigFile()
	if err := mergeConfigFileMissingKeys(path, defaults); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var actual map[string]int
	if err := json.Unmarshal(data, &actual); err != nil {
		t.Fatal(err)
	}
	for key, want := range defaults {
		if actual[key] != want {
			t.Errorf("%s = %d, want %d", key, actual[key], want)
		}
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
