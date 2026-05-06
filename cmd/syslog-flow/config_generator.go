package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func ensureConfigFiles() error {
	if err := os.MkdirAll(filepath.Dir(appConfigPath), 0o755); err != nil {
		return err
	}

	if err := writeConfigFileIfMissing(appConfigPath, defaultAppConfigFile()); err != nil {
		return err
	}
	if err := writeConfigFileIfMissing(deviceColorPath, map[string]string{}); err != nil {
		return err
	}
	if err := writeConfigFileIfMissing(interfaceColorPath, defaultInterfaceColorsFile()); err != nil {
		return err
	}
	if err := writeConfigFileIfMissing(statusColorPath, defaultStatusColors()); err != nil {
		return err
	}
	return nil
}

func writeConfigFileIfMissing(path string, value any) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func defaultAppConfigFile() map[string]int {
	defaults := defaultAppConfig()
	return map[string]int{
		"live_refresh_seconds":     defaults.LiveRefreshSeconds,
		"stats_refresh_seconds":    defaults.StatsRefreshSeconds,
		"overview_refresh_seconds": defaults.OverviewRefreshSeconds,
	}
}

func defaultStatusColors() map[string]string {
	return map[string]string{
		"emerg":   "#FF4D4D",
		"alert":   "#FF4D4D",
		"crit":    "#FF4D4D",
		"err":     "#FF6B6B",
		"warning": "#FFD166",
		"notice":  "#7BDFF2",
		"info":    "#9AA89F",
		"debug":   "#8E9AAF",
	}
}

type interfaceColorsFile struct {
	Light map[string]string `json:"light"`
	Dark  map[string]string `json:"dark"`
}

func defaultInterfaceColorsFile() interfaceColorsFile {
	return interfaceColorsFile{
		Light: map[string]string{
			"bg":            "#ffffff",
			"panel":         "#ffffff",
			"panel-soft":    "#fbfdff",
			"panel-strong":  "rgba(255, 255, 255, 0.92)",
			"panel-card":    "#ffffff",
			"ink":           "#172336",
			"muted":         "#64748b",
			"line":          "rgba(0, 0, 0, 0.18)",
			"accent":        "#0078ff",
			"accent-strong": "#005ed6",
			"active-bg":     "#d9ecff",
			"active-ink":    "#004aab",
			"input-bg":      "#ffffff",
			"code":          "#111827",
			"code-ink":      "#dbeafe",
			"error-bg":      "#fff0ed",
			"error-line":    "#f3b3a8",
			"error-ink":     "#7f1d1d",
			"glow-soft":     "none",
			"glow-card":     "none",
			"shadow":        "rgba(15, 23, 42, 0.06)",
		},
		Dark: map[string]string{
			"bg":            "#111517",
			"panel":         "#182024",
			"panel-soft":    "rgba(24, 32, 36, 0.68)",
			"panel-strong":  "rgba(19, 26, 30, 0.9)",
			"panel-card":    "rgba(24, 32, 36, 0.86)",
			"ink":           "#e7eee8",
			"muted":         "#9aa89f",
			"line":          "#2c3b36",
			"accent":        "#0078ff",
			"accent-strong": "#8ec5ff",
			"active-bg":     "#0f2f59",
			"active-ink":    "#cfe6ff",
			"input-bg":      "#0f1715",
			"code":          "#07110f",
			"code-ink":      "#cef7df",
			"error-bg":      "#311b1b",
			"error-line":    "#7f3434",
			"error-ink":     "#ffd7d7",
			"glow-soft":     "0 0 10px rgba(0, 120, 255, 0.3), 0 0 28px rgba(0, 120, 255, 0.18)",
			"glow-card":     "0 0 10px rgba(0, 120, 255, 0.42), 0 0 30px rgba(0, 120, 255, 0.21), 0 8px 25px rgba(0, 0, 0, 0.42)",
			"shadow":        "rgba(0, 0, 0, 0.28)",
		},
	}
}
