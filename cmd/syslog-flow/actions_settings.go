package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

type actionRuleView struct {
	actionRule
	Status actionStatus
}

func (rule actionRule) HasSeverity(level string) bool {
	for _, selected := range rule.Severities {
		if selected == level {
			return true
		}
	}
	return false
}

func populateActionsSettings(data *settingsData) {
	actions.mu.Lock()
	encoded, _ := json.Marshal(actions.config.Rules)
	data.ActionRulesJSON = template.JS(encoded)
	data.ActionDraft = actionRule{Enabled: true, DeviceMode: "include", ContainsMode: "include", Severities: []string{"emerg", "alert", "crit", "err"}, CooldownSeconds: 300, Type: "shoutrrr"}
	for _, rule := range actions.config.Rules {
		data.ActionRules = append(data.ActionRules, actionRuleView{actionRule: rule, Status: actions.statuses[rule.ID]})
		if rule.ID == data.EditAction {
			data.ActionDraft = rule
		}
	}
	data.ActionsError = actions.configError
	if actions.readerError != "" {
		if data.ActionsError != "" {
			data.ActionsError += "; "
		}
		data.ActionsError += actions.readerError
	}
	actions.mu.Unlock()
	data.SeverityNames = severityNames
	names := make(map[string]bool)
	stateIndex.mu.Lock()
	for _, file := range stateIndex.files {
		names[strings.TrimSuffix(file.name, ".log")] = true
	}
	stateIndex.mu.Unlock()
	for name := range names {
		data.ActionDevices = append(data.ActionDevices, name)
	}
	sort.Strings(data.ActionDevices)
	for name, value := range data.ActionDraft.Headers {
		data.ActionHeaders += name + ": " + value + "\n"
	}
}

func handleActionsSave(w http.ResponseWriter, r *http.Request) {
	id := r.Form.Get("id")
	operation := r.Form.Get("action")
	if operation == "test" {
		if err := actions.test(id); err != nil {
			writeSettingsError(w, r, http.StatusBadRequest, err.Error())
			return
		}
		if wantsSettingsJSON(r) {
			writeSettingsJSON(w, http.StatusOK, map[string]any{"ok": true, "queued": true})
			return
		}
		http.Redirect(w, r, "/settings?section=actions", http.StatusSeeOther)
		return
	}
	var rule actionRule
	if operation == "save" {
		cooldown, err := strconv.Atoi(r.Form.Get("cooldown_seconds"))
		if err != nil {
			writeSettingsError(w, r, http.StatusBadRequest, "Cooldown must be a whole number of seconds")
			return
		}
		rule = actionRule{ID: id, Name: r.Form.Get("name"), Enabled: r.Form.Get("enabled") == "on", Device: r.Form.Get("device"), Contains: r.Form.Get("contains"), Severities: r.Form["severities"], CooldownSeconds: cooldown, Type: r.Form.Get("type"), URL: r.Form.Get("url")}
		rule.DeviceMode = r.Form.Get("device_mode")
		rule.ContainsMode = r.Form.Get("contains_mode")
		for _, device := range strings.Split(r.Form.Get("devices"), "\n") {
			if device = strings.TrimSpace(device); device != "" {
				rule.Devices = append(rule.Devices, device)
			}
		}
		switch r.Form.Get("payload_mode") {
		case "", "log":
		case "custom":
			payload := r.Form.Get("payload")
			rule.Payload = &payload
		default:
			writeSettingsError(w, r, http.StatusBadRequest, "Choose log event or custom payload")
			return
		}
		if rule.ID == "" {
			rule.ID = newSettingsCSRFToken()
		}
		if rule.Type == "webhook" {
			rule.Headers = make(map[string]string)
			for _, line := range strings.Split(r.Form.Get("headers"), "\n") {
				if strings.TrimSpace(line) == "" {
					continue
				}
				name, value, ok := strings.Cut(line, ":")
				name = http.CanonicalHeaderKey(strings.TrimSpace(name))
				if !ok || !validActionHeader(name) {
					writeSettingsError(w, r, http.StatusBadRequest, "Enter webhook headers as Name: value, one per line")
					return
				}
				if _, exists := rule.Headers[name]; exists {
					writeSettingsError(w, r, http.StatusBadRequest, "Webhook header names must be unique")
					return
				}
				rule.Headers[name] = strings.TrimSpace(value)
			}
		}
	} else if operation != "delete" && operation != "toggle" {
		writeSettingsError(w, r, http.StatusBadRequest, "Invalid action operation")
		return
	}
	if operation == "toggle" && r.Form.Get("enabled") != "on" && r.Form.Get("enabled") != "off" {
		writeSettingsError(w, r, http.StatusBadRequest, "Choose an enabled or disabled state")
		return
	}
	actions.mu.Lock()
	config := actionsConfig{Rules: append([]actionRule{}, actions.config.Rules...)}
	found := false
	for i := range config.Rules {
		if config.Rules[i].ID == id {
			found = true
			switch operation {
			case "save":
				config.Rules[i] = rule
			case "delete":
				config.Rules = append(config.Rules[:i], config.Rules[i+1:]...)
			case "toggle":
				config.Rules[i].Enabled = r.Form.Get("enabled") == "on"
			}
			break
		}
	}
	var err error
	if !found {
		if operation == "save" && id == "" {
			config.Rules = append(config.Rules, rule)
		} else {
			err = fmt.Errorf("Action not found; reload Settings")
		}
	}
	if err == nil {
		err = actions.saveLocked(config)
	}
	actions.mu.Unlock()
	if err != nil {
		writeSettingsError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if wantsSettingsJSON(r) {
		writeSettingsJSON(w, http.StatusOK, map[string]any{"ok": true, "reload": true})
		return
	}
	http.Redirect(w, r, "/settings?section=actions", http.StatusSeeOther)
}

func handleActionsStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actions.mu.Lock()
	defer actions.mu.Unlock()
	writeSettingsJSON(w, http.StatusOK, map[string]any{"statuses": actions.statuses, "config_error": actions.configError, "reader_error": actions.readerError})
}

const actionsSettingsTemplate = `
{{define "actions"}}
<style>
 .actions-toolbar,.action-card-head,.action-buttons,.action-dialog-head,.action-dialog-footer,.action-footer-buttons,.action-switch-label,.action-url-label {display:flex;align-items:center;gap:.75rem;}
 .actions-toolbar,.action-card-head,.action-dialog-head,.action-dialog-footer,.action-url-label {justify-content:space-between;}
 .actions-toolbar {margin:1.25rem 0;} .actions-toolbar p {margin:0;max-width:35rem;} .actions-toolbar button {margin:0;flex-shrink:0;}
 .action-card {background:var(--panel);border:1px solid var(--line);border-radius:.75rem;margin:1rem 0;padding:1.1rem 1.25rem;}
 .action-card-head {align-items:flex-start;} .action-card h3 {margin:0;font-size:1rem;overflow-wrap:anywhere;} .action-card p {margin:.6rem 0;overflow-wrap:anywhere;}
 .action-card .action-status {font-size:.85rem;color:var(--muted);padding-top:.75rem;border-top:1px solid var(--line);}
 .action-card .action-contains {white-space:pre-line;}
 .action-buttons {flex-wrap:wrap;margin-top:.9rem;} .action-buttons form,.action-card-head form {margin:0;} .action-buttons button {margin:0;}
 .action-secondary {background:var(--panel);color:var(--ink);border:1px solid var(--line);box-shadow:none;} .action-danger {background:transparent;color:var(--error-ink);border:1px solid var(--error-line);box-shadow:none;}
 .action-switch-label {white-space:nowrap;font-size:.88rem;cursor:pointer;}
 input.action-switch {appearance:none;position:relative;flex-shrink:0;width:38px;height:22px;margin:0;border:0;border-radius:12px;padding:0;background:var(--muted);cursor:pointer;}
 input.action-switch::before {content:"";position:absolute;width:18px;height:18px;top:2px;left:2px;background:var(--panel);border-radius:50%;transition:transform .15s;}
 input.action-switch:checked {background:var(--accent);} input.action-switch:checked::before {transform:translateX(16px);} input.action-switch:disabled {opacity:.6;cursor:wait;}
 input.action-switch:focus-visible,.action-dialog button:focus-visible {outline:2px solid var(--accent);outline-offset:3px;}
 .actions-empty {border:1px dashed var(--line);border-radius:.75rem;padding:2.5rem 1rem;text-align:center;color:var(--muted);}
 .action-dialog {background:var(--panel);color:var(--ink);border:1px solid var(--line);border-radius:12px;width:min(680px,calc(100% - 24px));max-height:90dvh;padding:0;overflow:hidden;box-shadow:0 20px 70px rgba(0,0,0,.3);}
 .action-dialog::backdrop {background:rgba(0,0,0,.55);}
 .action-dialog form {display:flex;flex-direction:column;height:min(820px,90dvh);margin:0;min-height:0;}
 .action-dialog button {margin:0;}
 .action-dialog-head {padding:1rem 1.25rem;border-bottom:1px solid var(--line);flex-shrink:0;} .action-dialog-head h3 {display:flex;align-items:center;gap:.5rem;flex:1;min-width:0;font-size:1.1rem;margin:0;} .action-dialog-head #action-name {flex:1;min-width:0;width:100%;border:1px solid transparent;border-radius:0;background:transparent;color:var(--ink);font:inherit;padding:.35rem .1rem;} .action-dialog-head #action-name:hover,.action-dialog-head #action-name:focus {border-bottom-color:var(--accent);outline:none;} .action-dialog-head #action-name::placeholder {color:var(--muted);} .action-close {font-size:1.1rem;line-height:1;padding:.5rem .65rem;}
 .action-editor-content {padding:1.25rem;overflow-y:auto;min-height:0;flex:1;background:var(--bg);display:flex;flex-direction:column;gap:1rem;}
 .action-section {background:var(--panel);border:1px solid var(--line);border-radius:.6rem;padding:1.15rem;}
 .action-section h4 {font-size:.95rem;margin:0 0 1rem;} .action-section .section-hint {font-size:.85rem;color:var(--muted);margin:-.5rem 0 1rem;}
 .action-field {display:flex;flex-direction:column;gap:.4rem;min-width:0;} .action-field + .action-field,.action-field + .action-field-grid,.action-field-grid + .action-field,.action-section details {margin-top:1rem;}
 .action-field[hidden] {display:none;}
 .action-field label,.action-url-label label,.severity-field legend {font-size:.88rem;font-weight:650;}
 .action-field input,.action-field select,.action-field textarea {background:var(--input-bg);color:var(--ink);border:1px solid var(--line);border-radius:.4rem;padding:.65rem .75rem;width:100%;font:inherit;min-width:0;}
 .action-field input:focus,.action-field select:focus,.action-field textarea:focus {outline:2px solid var(--accent);outline-offset:1px;}
 .action-field small {color:var(--muted);font-size:.8rem;} .action-field textarea {min-height:5rem;font:13px/1.5 ui-monospace,monospace;}
 .action-field-grid {display:grid;grid-template-columns:1fr 1fr;gap:1rem;} .action-field-grid .action-field {margin:0;}
 .action-device-suggestions {background:var(--panel);border:1px solid var(--line);border-radius:.4rem;max-height:9rem;overflow-y:auto;padding:.25rem;} .action-device-suggestions[hidden],.action-device-suggestions button[hidden] {display:none;} .action-device-suggestions button {display:block;text-align:left;width:100%;border:0;border-radius:.25rem;padding:.45rem .65rem;} .action-device-suggestions button:hover {background:var(--active-bg);color:var(--active-ink);}
 .action-url-label button {font-size:.8rem;padding:.1rem .3rem;background:transparent;color:var(--accent-strong);box-shadow:none;}
 .severity-field {padding:0;border:0;margin:1.2rem 0 0;min-width:0;} .severity-field legend {padding:0;margin-bottom:.6rem;}
 .severity-options {display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:.5rem;} .severity-options label {display:flex;align-items:center;gap:.4rem;background:var(--panel-soft);border:1px solid var(--line);border-radius:.4rem;padding:.55rem .5rem;font-size:.84rem;cursor:pointer;}
 .severity-options input {margin:0;accent-color:var(--accent);} .severity-options label:has(input:checked) {border-color:var(--accent);background:var(--active-bg);color:var(--active-ink);}
 .action-section details summary {font-size:.88rem;font-weight:650;cursor:pointer;} .action-section details .action-field {margin-top:.75rem;}
 .action-dialog-footer {padding:1rem 1.25rem;border-top:1px solid var(--line);flex-shrink:0;} .action-footer-buttons {justify-content:flex-end;flex-shrink:0;margin-left:auto;} .action-footer-buttons button {white-space:nowrap;}
 .action-editor-content .error {margin:0;overflow-wrap:anywhere;}
 @media(max-width:600px) {.actions-toolbar {align-items:flex-start;} .action-dialog-head,.action-dialog-footer,.action-editor-content {padding:1rem;} .action-section {padding:1rem;} .action-field-grid {grid-template-columns:1fr;} .severity-options {grid-template-columns:repeat(2,minmax(0,1fr));} .action-dialog-footer {gap:.6rem;flex-wrap:wrap;} .action-footer-buttons {gap:.4rem;} .action-footer-buttons button {padding:.6rem .7rem;font-size:.85rem;}}
 @media(prefers-reduced-motion:reduce) {input.action-switch::before {transition:none;}}
</style>
<div class="actions-toolbar">
 <button type="button" data-new-action aria-haspopup="dialog">+ Add action</button>
</div>
<p id="actions-monitor-error" class="error" role="alert" {{if not .ActionsError}}hidden{{end}}>{{.ActionsError}}</p>
<p id="actions-page-error" class="error" role="alert" hidden></p>
{{range .ActionRules}}
 <article class="action-card">
  <div class="action-card-head">
   <h3>{{.Name}}</h3>
   <form method="post" action="/settings" data-settings-form>
    <input type="hidden" name="section" value="actions"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="id" value="{{.ID}}"><input type="hidden" name="action" value="toggle">
    <label class="action-switch-label"><input type="checkbox" name="enabled" value="on" role="switch" class="action-switch" data-action-toggle aria-label="Enable {{.Name}}" {{if .Enabled}}checked{{end}}> Enabled</label>
    <button type="submit" hidden>Update</button>
   </form>
  </div>
  <p>{{if .Devices}}{{if eq .DeviceMode "exclude"}}Exclude{{else}}Include{{end}}: {{range $i,$device := .Devices}}{{if $i}}, {{end}}{{$device}}{{end}}{{else}}Any device{{end}} · {{range $i,$level := .Severities}}{{if $i}}, {{end}}{{$level}}{{end}}{{if .Contains}} · <span class="action-contains">{{if eq .ContainsMode "exclude"}}excludes messages containing any of:{{else}}contains any of:{{end}} {{.Contains}}</span>{{end}}</p>
  <p class="muted">{{if eq .Type "shoutrrr"}}Shoutrrr{{else}}Webhook{{end}} · {{.CooldownSeconds}} second cooldown</p>
  <p class="action-status" data-action-status="{{.ID}}">{{if .Status.Pending}}Queued: {{.Status.Pending}}. {{end}}{{if .Status.Error}}{{.Status.Error}}{{else if not .Status.LastSuccess.IsZero}}Last delivered {{.Status.LastSuccess.Format "2006-01-02 15:04:05 MST"}}{{else}}No deliveries yet{{end}}{{if .Status.Dropped}} · Dropped: {{.Status.Dropped}}{{end}}</p>
  <div class="action-buttons">
   <button class="action-secondary" type="button" data-edit-action="{{.ID}}" aria-haspopup="dialog">Edit</button>
   <form method="post" action="/settings" data-settings-form><input type="hidden" name="section" value="actions"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="id" value="{{.ID}}"><button class="action-secondary" name="action" value="test" type="submit">Test action</button></form>
   <form method="post" action="/settings" data-settings-form><input type="hidden" name="section" value="actions"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="id" value="{{.ID}}"><button class="action-danger" name="action" value="delete" type="submit">Delete</button></form>
  </div>
 </article>
{{else}}<div class="actions-empty">No actions yet. Use Add action to create your first rule.</div>{{end}}
<dialog id="action-dialog" class="action-dialog" aria-labelledby="action-editor-title" data-edit-id="{{.ActionDraft.ID}}">
 <form id="action-editor" method="post" action="/settings" data-settings-form>
  <div class="action-dialog-head"><h3 id="action-editor-title"><span id="action-editor-verb">{{if .ActionDraft.ID}}Edit{{else}}New{{end}}</span><input id="action-name" name="name" aria-label="Action name" maxlength="100" required value="{{.ActionDraft.Name}}" placeholder="Action"></h3><button type="button" class="action-close action-secondary" data-close-action aria-label="Close action editor">×</button></div>
  <div class="action-editor-content">
   <p class="error" data-form-error role="alert" hidden></p>
   <input type="hidden" name="section" value="actions"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="id" value="{{.ActionDraft.ID}}">
   <section class="action-section" aria-labelledby="action-delivery-heading">
    <h4 id="action-delivery-heading">Delivery</h4>
    <div class="action-field"><label for="action-type">Action type</label><select id="action-type" name="type"><option value="shoutrrr" {{if eq .ActionDraft.Type "shoutrrr"}}selected{{end}}>Shoutrrr</option><option value="webhook" {{if eq .ActionDraft.Type "webhook"}}selected{{end}}>Webhook (JSON POST)</option></select></div>
    <div class="action-field">
     <div class="action-url-label"><label for="action-url">Destination URL</label><button type="button" id="action-show-url" aria-pressed="false" aria-controls="action-url" {{if eq .ActionDraft.Type "webhook"}}hidden{{end}}>Show</button></div>
     <input id="action-url" name="url" type="{{if eq .ActionDraft.Type "webhook"}}text{{else}}password{{end}}" autocomplete="{{if eq .ActionDraft.Type "webhook"}}off{{else}}new-password{{end}}" spellcheck="false" required value="{{.ActionDraft.URL}}" aria-describedby="action-url-hint"><small id="action-url-hint"></small>
    </div>
    <div class="action-field"><label for="action-payload-mode">Payload</label><select id="action-payload-mode" name="payload_mode"><option value="log" {{if not .ActionDraft.Payload}}selected{{end}}>Log event</option><option value="custom" {{if .ActionDraft.Payload}}selected{{end}}>Custom payload</option></select><small id="action-payload-mode-hint"></small></div>
    <div id="action-custom-payload" class="action-field" {{if not .ActionDraft.Payload}}hidden{{end}}><label id="action-payload-label" for="action-payload">Custom payload</label><textarea id="action-payload" name="payload" spellcheck="false" maxlength="65536" aria-describedby="action-payload-hint">{{with .ActionDraft.Payload}}{{.}}{{end}}</textarea><small id="action-payload-hint"></small></div>
    <details id="webhook-headers"><summary>Custom headers (optional)</summary><div class="action-field"><label for="action-headers">Headers</label><textarea id="action-headers" name="headers" spellcheck="false" placeholder="Authorization: Bearer your-token">{{.ActionHeaders}}</textarea><small>One header per line, as Name: value. Content-Type defaults to application/json.</small></div></details>
   </section>
   <section class="action-section" aria-labelledby="action-conditions-heading">
    <h4 id="action-conditions-heading">Match log entries</h4><p class="section-hint">All conditions must match. Any selected severity can trigger the action.</p>
    <div class="action-field-grid">
     <div class="action-field"><label for="action-device-mode">Device filter</label><select id="action-device-mode" name="device_mode"><option value="include" {{if ne .ActionDraft.DeviceMode "exclude"}}selected{{end}}>Include</option><option value="exclude" {{if eq .ActionDraft.DeviceMode "exclude"}}selected{{end}}>Exclude</option></select><label for="action-devices">Devices (optional)</label><div id="action-device-input"><textarea id="action-devices" name="devices" rows="3" placeholder="One device per line" aria-controls="action-device-suggestions" aria-describedby="action-devices-hint">{{join .ActionDraft.Devices "\n"}}</textarea><div id="action-device-suggestions" class="action-device-suggestions" role="group" aria-label="Suggested devices" hidden>{{range .ActionDevices}}<button type="button" class="action-secondary" data-action-device="{{.}}">{{.}}</button>{{end}}</div></div><small id="action-devices-hint">Type a device name and choose a suggestion below, or enter one device name per line. Leave empty for any device.</small></div>
     <div class="action-field"><label for="action-contains-mode">Message filter</label><select id="action-contains-mode" name="contains_mode"><option value="include" {{if ne .ActionDraft.ContainsMode "exclude"}}selected{{end}}>Include</option><option value="exclude" {{if eq .ActionDraft.ContainsMode "exclude"}}selected{{end}}>Exclude</option></select><label for="action-contains">Contains phrases (optional)</label><textarea id="action-contains" name="contains" maxlength="1024" rows="3" placeholder="One phrase per line" aria-describedby="action-contains-hint">{{.ActionDraft.Contains}}</textarea><small id="action-contains-hint">One phrase per line, ignoring case. Include matches messages containing any phrase; Exclude skips them. Blank lines are ignored. Leave empty for any message.</small></div>
    </div>
    <fieldset class="severity-field"><legend>Trigger on these severities</legend><div class="severity-options">{{range .SeverityNames}}<label><input type="checkbox" name="severities" value="{{.}}" {{if $.ActionDraft.HasSeverity .}}checked{{end}}> {{.}}</label>{{end}}</div></fieldset>
   </section>
   <section class="action-section" aria-labelledby="action-cooldown-heading">
    <h4 id="action-cooldown-heading">Cooldown</h4><div class="action-field"><label for="action-cooldown">Wait between notifications per device (seconds)</label><input id="action-cooldown" name="cooldown_seconds" type="number" min="0" max="86400" required value="{{.ActionDraft.CooldownSeconds}}"><small>300 = five minutes. Use 0 to send every matching entry.</small></div>
   </section>
  </div>
  <div class="action-dialog-footer">
   <label class="action-switch-label"><input id="action-enabled" type="checkbox" name="enabled" role="switch" class="action-switch" {{if .ActionDraft.Enabled}}checked{{end}}> Enabled</label>
   <div class="action-footer-buttons"><button name="action" value="save" type="submit">Save action</button></div>
  </div>
 </form>
</dialog>
<script id="action-rules" type="application/json">{{.ActionRulesJSON}}</script>
<script>
 const actionDialog = document.getElementById("action-dialog");
 const actionForm = document.getElementById("action-editor");
 const savedActions = JSON.parse(document.getElementById("action-rules").textContent);
 const actionType = document.getElementById("action-type");
 const actionURL = document.getElementById("action-url");
 const actionPayloadMode = document.getElementById("action-payload-mode");
 const actionPayload = document.getElementById("action-payload");
 const showActionURL = document.getElementById("action-show-url");
 const actionFormError = actionForm.querySelector("[data-form-error]");
 const actionDevices = document.getElementById("action-devices");
 const actionDeviceSuggestions = document.getElementById("action-device-suggestions");
 const actionDeviceButtons = Array.from(actionDeviceSuggestions.querySelectorAll("[data-action-device]"));
 function actionDeviceLine() {
  const start = actionDevices.value.slice(0, actionDevices.selectionStart).lastIndexOf("\n") + 1;
  const next = actionDevices.value.indexOf("\n", actionDevices.selectionStart);
  return {start, end: next < 0 ? actionDevices.value.length : next};
 }
 function updateDeviceSuggestions() {
  const {start, end} = actionDeviceLine();
  const query = actionDevices.value.slice(start, end).trim().toLowerCase();
  const selected = actionDevices.value.split("\n").map(value => value.trim());
  for (const button of actionDeviceButtons) {
   const device = button.dataset.actionDevice;
   button.hidden = selected.includes(device) || !device.toLowerCase().includes(query);
  }
  actionDeviceSuggestions.hidden = !actionDeviceButtons.some(button => !button.hidden);
 }
 // Keep textarea focus so Safari does not hide suggestions before the click.
 for (const button of actionDeviceButtons) button.addEventListener("pointerdown", event => event.preventDefault());
 for (const button of actionDeviceButtons) button.addEventListener("click", () => {
  const {start, end} = actionDeviceLine();
  const suffix = actionDevices.value.slice(end);
  actionDevices.value = actionDevices.value.slice(0, start) + button.dataset.actionDevice + (suffix || "\n");
  const caret = start + button.dataset.actionDevice.length + (suffix ? 0 : 1);
  actionDevices.focus();
  actionDevices.setSelectionRange(caret, caret);
  updateDeviceSuggestions();
 });
 for (const event of ["input", "focus", "click"]) actionDevices.addEventListener(event, updateDeviceSuggestions);
 actionDevices.addEventListener("keyup", event => { if (event.key !== "Escape") updateDeviceSuggestions(); });
 actionDevices.addEventListener("keydown", event => {
  if (event.key === "Escape" && !actionDeviceSuggestions.hidden) {
   event.preventDefault(); event.stopPropagation(); actionDeviceSuggestions.hidden = true;
  }
 });
 document.getElementById("action-device-input").addEventListener("focusout", event => {
  if (!event.currentTarget.contains(event.relatedTarget)) actionDeviceSuggestions.hidden = true;
 });
 function updateActionFields() {
  const webhook = actionType.value === "webhook";
  if (webhook) actionURL.type = "text";
  else if (showActionURL.hidden) { actionURL.type = "password"; showActionURL.textContent = "Show"; showActionURL.setAttribute("aria-pressed","false"); }
  showActionURL.hidden = webhook;
  actionURL.autocomplete = webhook ? "off" : "new-password";
  document.getElementById("webhook-headers").hidden = !webhook;
  document.getElementById("action-headers").disabled = !webhook;
  actionURL.placeholder = webhook ? "https://example.com/webhook" : "ntfy://your-server/topic";
  document.getElementById("action-url-hint").textContent = webhook ? "Sends a JSON POST from the container to this URL." : "Enter a Shoutrrr service URL for your notification provider.";
  const custom = actionPayloadMode.value === "custom";
  document.getElementById("action-custom-payload").hidden = !custom;
  actionPayload.disabled = !custom;
  actionPayload.required = custom && !webhook;
  document.getElementById("action-payload-mode-hint").textContent = webhook ? "Use the log-event JSON or replace it with your own request body." : "Use the log-event message or replace it with your own message text.";
  document.getElementById("action-payload-label").textContent = webhook ? "JSON request body" : "Message text";
  document.getElementById("action-payload-hint").textContent = webhook ? "Sent exactly as entered, without log fields. Use valid JSON, or leave empty for no body. Test action sends this payload too." : "Sent as the message without log details. The notification title uses the action name. Test action sends this message too.";
  actionPayload.placeholder = webhook ? '{"entity_id":"light.kitchen_pendants"}' : "Disk alert from the firewall";
 }
 function openActionEditor(rule) {
  if (actionForm.dataset.submitting === "true") return;
  const draft = rule || {enabled:true, type:"shoutrrr", severities:["emerg","alert","crit","err"], cooldown_seconds:300};
  actionForm.reset();
  for (const name of ["id","name","contains","url"]) actionForm.elements.namedItem(name).value = draft[name] || "";
  actionForm.elements.namedItem("device_mode").value = draft.device_mode || "include";
  actionForm.elements.namedItem("contains_mode").value = draft.contains_mode || "include";
  actionForm.elements.namedItem("devices").value = (draft.devices || []).join("\n");
  actionDeviceSuggestions.hidden = true;
  actionForm.elements.namedItem("enabled").checked = draft.enabled;
  actionForm.elements.namedItem("cooldown_seconds").value = draft.cooldown_seconds;
  actionType.value = draft.type;
  actionPayloadMode.value = draft.payload == null ? "log" : "custom";
  actionPayload.value = draft.payload ?? "";
  for (const checkbox of actionForm.querySelectorAll('[name="severities"]')) checkbox.checked = draft.severities.includes(checkbox.value);
  document.getElementById("action-headers").value = Object.entries(draft.headers || {}).map(([name,value]) => name+": "+value).join("\n");
  document.getElementById("webhook-headers").open = false;
  actionURL.type = "password"; showActionURL.textContent = "Show"; showActionURL.setAttribute("aria-pressed","false");
  actionFormError.hidden = true; actionFormError.textContent = "";
  document.getElementById("action-editor-verb").textContent = rule ? "Edit" : "New";
  updateActionFields();
  actionDialog.showModal();
  actionDialog.querySelector(".action-editor-content").scrollTop = 0;
  document.getElementById("action-name").focus({preventScroll:true});
 }
 document.querySelector("[data-new-action]").addEventListener("click", () => openActionEditor());
 for (const button of document.querySelectorAll("[data-edit-action]")) button.addEventListener("click", () => openActionEditor(savedActions.find(rule => rule.id === button.dataset.editAction)));
 for (const button of document.querySelectorAll("[data-close-action]")) button.addEventListener("click", () => actionDialog.close());
 actionType.addEventListener("change", updateActionFields); updateActionFields();
 actionPayloadMode.addEventListener("change", updateActionFields);
 showActionURL.addEventListener("click", () => {
  const visible = actionURL.type === "password";
  actionURL.type = visible ? "text" : "password"; showActionURL.textContent = visible ? "Hide" : "Show"; showActionURL.setAttribute("aria-pressed",String(visible));
 });
 actionForm.addEventListener("submit", event => {
  if (!actionForm.querySelector('[name="severities"]:checked')) {
   event.preventDefault(); event.stopImmediatePropagation();
   actionFormError.textContent = "Select at least one severity."; actionFormError.hidden = false;
   actionFormError.scrollIntoView({block:"nearest"});
  }
 });
 if (actionDialog.dataset.editId) openActionEditor(savedActions.find(rule => rule.id === actionDialog.dataset.editId));
 async function refreshActionStatuses() {
  try {
   const response=await fetch("/api/settings/actions-status", {cache:"no-store"}); if (!response.ok) return;
   const result=await response.json();
   const error=document.getElementById("actions-monitor-error"); error.textContent=[result.config_error,result.reader_error].filter(Boolean).join("; "); error.hidden=!error.textContent;
   for (const node of document.querySelectorAll("[data-action-status]")) {
    const status=result.statuses[node.dataset.actionStatus]; if (!status) continue;
    const success=Date.parse(status.last_success); const attempted=Date.parse(status.last_attempt);
    let message=status.error || (success>0 ? "Last delivered "+new Date(success).toLocaleString() : attempted>0 ? "Sending…" : "No deliveries yet");
    if (status.pending) message="Queued/sending: "+status.pending+". "+message;
    if (status.dropped) message+=" · Dropped: "+status.dropped;
    node.textContent=message;
   }
  } catch { /* Keep the last known delivery results. */ }
 }
 setInterval(refreshActionStatuses,3000);
</script>
{{end}}`
