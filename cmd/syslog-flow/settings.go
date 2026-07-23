package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type settingsSection struct {
	ID          string
	Title       string
	Description string
	Path        string
	Caches      bool
}

type settingsData struct {
	Sections settingsSectionList
	Section  settingsSection
	Value    string
	Error    string
	CSRF     string
	Months   []cacheMonth
	CacheJob cacheRefreshJob
}

type settingsSectionList []settingsSection

func (sections settingsSectionList) selected(id string) settingsSection {
	for _, section := range sections {
		if section.ID == id {
			return section
		}
	}
	return sections[0]
}

var settingsSections = settingsSectionList{
	{ID: "system", Title: "System", Description: "Refresh intervals and in-memory tail limits from app.json.", Path: appConfigPath},
	{ID: "device-colors", Title: "Device Colors", Description: "Optional exact and contains rules from device-colors.json.", Path: deviceColorPath},
	{ID: "status-colors", Title: "Status Colors", Description: "Syslog severity colors from status-colors.json.", Path: statusColorPath},
	{ID: "interface-theme", Title: "Interface Theme", Description: "Light and dark interface colors from interface-colors.json.", Path: interfaceColorPath},
	{ID: "json-caches", Title: "JSON Caches", Description: "Daily log summaries. Existing valid caches are used until you refresh their month.", Caches: true},
}

var settingsCSRFToken = newSettingsCSRFToken()

var settingsPage = template.Must(template.New("settings").Funcs(template.FuncMap{
	"interfaceTheme": interfaceThemeDeclarations,
}).Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Settings · syslog-flow</title>
  <link rel="icon" href="/favicon.ico" sizes="any">
  <style>
    :root { color-scheme: light dark; {{interfaceTheme "light"}} }
    @media (prefers-color-scheme: dark) { :root { {{interfaceTheme "dark"}} } }
    * { box-sizing: border-box; }
    body { margin: 0; background: var(--bg); color: var(--ink); font: 15px/1.45 ui-sans-serif, "Aptos", "Segoe UI", sans-serif; }
    header { align-items: center; background: var(--panel-strong); backdrop-filter: blur(8px); border-bottom: 1px solid var(--line); display: flex; gap: 1rem; min-height: 64px; padding: 1rem 1.25rem; position: sticky; top: 0; z-index: 2; }
    h1 { font-size: 1.1rem; letter-spacing: 0.03em; margin: 0; }
    a { color: var(--accent-strong); text-decoration: none; }
    a:hover { text-decoration: underline; }
    h1 a { color: var(--ink); }
    .top-link { align-items: center; background: var(--panel); border: 1px solid var(--line); border-radius: 999px; box-shadow: var(--glow-soft); color: var(--ink); display: inline-flex; font-size: 0.82rem; font-weight: 700; padding: 0.32rem 0.7rem; text-decoration: none; white-space: nowrap; }
    .top-link.active { background: var(--active-bg); border-color: var(--accent); color: var(--active-ink); }
    .layout { display: grid; grid-template-columns: 15rem minmax(0, 1fr); margin: 0 auto; max-width: 72rem; min-height: calc(100vh - 3.8rem); }
    aside { background: var(--panel-soft); border-right: 1px solid var(--line); padding: 1rem; }
    main { max-width: 54rem; padding: 1.5rem; }
    .section-link { border-radius: 0.55rem; color: var(--ink); display: block; font-weight: 650; margin-bottom: 0.25rem; padding: 0.55rem 0.65rem; }
    .section-link.active { background: var(--active-bg); color: var(--active-ink); }
    .muted { color: var(--muted); }
    h2 { margin: 0 0 0.35rem; }
    textarea { background: var(--code); border: 1px solid var(--line); border-radius: 0.65rem; color: var(--code-ink); font: 13px/1.45 ui-monospace, SFMono-Regular, Consolas, monospace; min-height: 28rem; padding: 0.85rem; resize: vertical; width: 100%; }
    button { background: var(--accent); border: 0; border-radius: 0.55rem; color: white; cursor: pointer; font: inherit; font-weight: 700; margin-top: 0.85rem; padding: 0.65rem 0.9rem; }
    button:disabled { cursor: wait; opacity: 0.8; }
    button.loading::before { animation: spin 0.7s linear infinite; border: 2px solid currentColor; border-right-color: transparent; border-radius: 50%; content: ""; display: inline-block; height: 0.8em; margin-right: 0.45rem; vertical-align: -0.08em; width: 0.8em; }
    button.success { background: #16803c; }
    button.failure { background: #b42318; }
    @keyframes spin { to { transform: rotate(360deg); } }
    .error { border: 1px solid; border-radius: 0.65rem; margin: 1rem 0; padding: 0.75rem 0.85rem; }
    .error { background: var(--error-bg); border-color: var(--error-line); color: var(--error-ink); }
    .cache-month { align-items: center; background: var(--panel); border: 1px solid var(--line); border-radius: 0.65rem; display: flex; gap: 0.8rem; justify-content: space-between; margin: 0.6rem 0; padding: 0.75rem 0.85rem; }
    .cache-month button { margin: 0; }
    @media (max-width: 700px) { header { flex-wrap: wrap; } .layout { grid-template-columns: 1fr; } aside { border-bottom: 1px solid var(--line); border-right: 0; } main { padding: 1rem; } }
  </style>
</head>
<body>
  <header>
    <h1><a href="/">syslog-flow</a></h1>
    <a class="top-link active" href="/settings">Settings</a>
    <a class="top-link" href="/statistics">Statistics</a>
  </header>
  <div class="layout">
    <aside>
      {{range .Sections}}
        <a class="section-link {{if eq $.Section.ID .ID}}active{{end}}" href="/settings?section={{.ID}}">{{.Title}}</a>
      {{end}}
    </aside>
    <main>
      <h2>{{.Section.Title}}</h2>
      <p class="muted">{{.Section.Description}}</p>
      {{if .Error}}<div class="error">{{.Error}}</div>{{end}}
      {{if .Section.Caches}}
        {{range .Months}}
          <div class="cache-month">
            <div><strong>{{.Label}}</strong><br><span class="muted">{{.Cached}}/{{.Days}} completed days cached</span></div>
            <form method="post" action="/settings" data-settings-form data-cache-form><input type="hidden" name="section" value="json-caches"><input type="hidden" name="action" value="refresh-month"><input type="hidden" name="month" value="{{.Month}}"><input type="hidden" name="csrf" value="{{$.CSRF}}"><button class="cache-refresh-button" data-month="{{.Month}}" type="submit"{{if $.CacheJob.Running}} disabled{{end}}>Refresh {{.Label}}</button></form>
          </div>
        {{else}}
          <p class="muted">Completed log days will appear here after logs span more than one day.</p>
        {{end}}
      {{else}}
      <form method="post" action="/settings" data-settings-form>
        <input type="hidden" name="section" value="{{.Section.ID}}">
        <input type="hidden" name="csrf" value="{{.CSRF}}">
        <textarea name="value" spellcheck="false" aria-label="{{.Section.Title}} JSON">{{.Value}}</textarea>
        <button class="save-button" type="submit">Save {{.Section.Title}}</button>
      </form>
      {{end}}
    </main>
  </div>
  <script>
    const settingForms = document.querySelectorAll("[data-settings-form]");
    function setButtonState(button, label, state) {
      button.disabled = state === "loading";
      button.classList.toggle("loading", state === "loading");
      button.classList.toggle("success", state === "success");
      button.classList.toggle("failure", state === "failure");
      button.textContent = label;
    }

    async function pollCacheRefresh(form, button) {
      const response = await fetch("/api/settings/cache-status", { cache: "no-store" });
      if (!response.ok) throw new Error("Unable to check cache refresh status");
      const job = await response.json();
      if (job.running) {
        setButtonState(button, "Refreshing " + job.completed + "/" + job.total + "…", "loading");
        window.setTimeout(() => pollCacheRefresh(form, button).catch(error => {
          form.dataset.submitting = "";
          setButtonState(button, error.message, "failure");
        }), 1000);
        return;
      }
      if (job.error) {
        form.dataset.submitting = "";
        setButtonState(button, "Failed", "failure");
        return;
      }
      setButtonState(button, "Complete ✓", "success");
      window.setTimeout(() => window.location.reload(), 1200);
    }

    for (const form of settingForms) {
      form.addEventListener("submit", async event => {
        event.preventDefault();
        if (form.dataset.submitting === "true") return;
        form.dataset.submitting = "true";
        const button = form.querySelector("button[type=submit]");
        const originalLabel = button.textContent;
        const body = new URLSearchParams();
        for (const [name, value] of new FormData(form).entries()) {
          body.append(name, value);
        }
        setButtonState(button, form.matches("[data-cache-form]") ? "Starting…" : "Saving…", "loading");
        try {
          const response = await fetch(form.getAttribute("action") || window.location.pathname, {
            method: "POST",
            body,
            headers: { Accept: "application/json" },
          });
          const result = await response.json();
          if (!response.ok || !result.ok) throw new Error(result.error || "Save failed");
          if (form.matches("[data-cache-form]")) {
            pollCacheRefresh(form, button).catch(error => {
              form.dataset.submitting = "";
              setButtonState(button, error.message, "failure");
            });
          } else {
            setButtonState(button, "Saved ✓", "success");
            window.setTimeout(() => {
              form.dataset.submitting = "";
              setButtonState(button, originalLabel, "idle");
            }, 1600);
          }
        } catch (error) {
          setButtonState(button, error.message || "Save failed", "failure");
          window.setTimeout(() => {
            form.dataset.submitting = "";
            setButtonState(button, originalLabel, "idle");
          }, 2500);
        }
      });
    }
  </script>
</body>
</html>`))

func handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/settings" {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		section := settingsSections.selected(r.URL.Query().Get("section"))
		renderSettings(w, settingsData{
			Sections: settingsSections,
			Section:  section,
			CSRF:     settingsCSRFToken,
		})
	case http.MethodPost:
		handleSettingsSave(w, r)
	default:
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleSettingsSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeSettingsError(w, r, http.StatusBadRequest, "invalid form")
		return
	}
	section := settingsSections.selected(r.Form.Get("section"))
	if r.Form.Get("section") != section.ID || r.Form.Get("csrf") != settingsCSRFToken {
		writeSettingsError(w, r, http.StatusBadRequest, "invalid settings request")
		return
	}
	if section.Caches {
		if r.Form.Get("action") != "refresh-month" {
			writeSettingsError(w, r, http.StatusBadRequest, "invalid cache action")
			return
		}
		if err := startMonthCacheRefresh(r.Form.Get("month")); err != nil {
			if wantsSettingsJSON(r) {
				writeSettingsError(w, r, http.StatusBadRequest, err.Error())
				return
			}
			renderSettings(w, settingsData{Sections: settingsSections, Section: section, Error: err.Error(), CSRF: settingsCSRFToken})
			return
		}
		if wantsSettingsJSON(r) {
			writeSettingsJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
		http.Redirect(w, r, "/settings?section=json-caches", http.StatusSeeOther)
		return
	}

	value := r.Form.Get("value")
	formatted, err := formatSettingsJSON(value)
	if err != nil {
		if wantsSettingsJSON(r) {
			writeSettingsError(w, r, http.StatusBadRequest, err.Error())
			return
		}
		renderSettings(w, settingsData{Sections: settingsSections, Section: section, Value: value, Error: err.Error(), CSRF: settingsCSRFToken})
		return
	}
	if err := validateSettingsFormat(section.ID, formatted); err != nil {
		if wantsSettingsJSON(r) {
			writeSettingsError(w, r, http.StatusBadRequest, err.Error())
			return
		}
		renderSettings(w, settingsData{Sections: settingsSections, Section: section, Value: value, Error: err.Error(), CSRF: settingsCSRFToken})
		return
	}
	if err := writeSettingsFile(section.Path, formatted); err != nil {
		if wantsSettingsJSON(r) {
			writeSettingsError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		renderSettings(w, settingsData{Sections: settingsSections, Section: section, Value: value, Error: err.Error(), CSRF: settingsCSRFToken})
		return
	}
	clearSettingsCache(section.ID)
	if wantsSettingsJSON(r) {
		writeSettingsJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	http.Redirect(w, r, "/settings?section="+section.ID, http.StatusSeeOther)
}

func handleCacheRefreshStatus(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/settings/cache-status" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeSettingsJSON(w, http.StatusOK, currentCacheRefreshJob())
}

func wantsSettingsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}

func writeSettingsJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeSettingsError(w http.ResponseWriter, r *http.Request, status int, message string) {
	if wantsSettingsJSON(r) {
		writeSettingsJSON(w, status, map[string]any{"ok": false, "error": message})
		return
	}
	http.Error(w, message, status)
}

func renderSettings(w http.ResponseWriter, data settingsData) {
	if data.Section.Caches {
		months, err := cacheMonths(appNow())
		if err != nil {
			data.Error = err.Error()
		} else {
			data.Months = months
		}
		data.CacheJob = currentCacheRefreshJob()
	}
	if !data.Section.Caches && data.Value == "" && data.Error == "" {
		value, err := readSettingsFile(data.Section.Path)
		if err != nil {
			data.Error = err.Error()
		} else {
			data.Value = value
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := settingsPage.Execute(w, data); err != nil {
		log.Printf("settings render error: %v", err)
	}
}

func readSettingsFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	formatted, err := formatSettingsJSON(string(data))
	if err != nil {
		return "", err
	}
	return string(formatted), nil
}

func formatSettingsJSON(value string) ([]byte, error) {
	if !json.Valid([]byte(value)) {
		return nil, fmt.Errorf("settings must contain valid JSON")
	}
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, []byte(value), "", "  "); err != nil {
		return nil, err
	}
	formatted.WriteByte('\n')
	return formatted.Bytes(), nil
}

func validateSettingsFormat(section string, value []byte) error {
	switch section {
	case "system":
		if _, err := parseAppConfig(value); err != nil {
			return fmt.Errorf("invalid app.json: %w", err)
		}
	case "device-colors":
		if _, err := parseDeviceColors(value); err != nil {
			return fmt.Errorf("invalid device-colors.json: %w", err)
		}
	case "status-colors":
		if _, err := parseStatusColors(value); err != nil {
			return fmt.Errorf("invalid status-colors.json: %w", err)
		}
	case "interface-theme":
		if _, err := parseInterfaceColors(value); err != nil {
			return fmt.Errorf("invalid interface-colors.json: %w", err)
		}
	}
	return nil
}

func writeSettingsFile(path string, value []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".syslog-flow-settings-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(value); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func clearSettingsCache(section string) {
	switch section {
	case "system":
		appConfigCache.Lock()
		appConfigCache.appConfig = appConfig{}
		appConfigCache.Unlock()
	case "device-colors":
		colorCache.Lock()
		colorCache.deviceColorConfig = deviceColorConfig{}
		colorCache.Unlock()
	case "status-colors":
		statusCache.Lock()
		statusCache.statusColorConfig = statusColorConfig{}
		statusCache.Unlock()
	case "interface-theme":
		interfaceColorCache.Lock()
		interfaceColorCache.interfaceColorsConfig = interfaceColorsConfig{}
		interfaceColorCache.Unlock()
	}
}

func newSettingsCSRFToken() string {
	value := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(value)
}
