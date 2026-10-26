package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nicholas-fedor/shoutrrr"
	"github.com/nicholas-fedor/shoutrrr/pkg/types"
)

const actionsConfigPath = "/config/actions.json"
const actionTimeout = 10 * time.Second

var severityNames = []string{"emerg", "alert", "crit", "err", "warning", "notice", "info", "debug"}
var actions = newActionEngine(actionsConfigPath)

type actionRule struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Enabled         bool              `json:"enabled"`
	Device          string            `json:"device,omitempty"` // Legacy single-device rules are normalized when loaded.
	DeviceMode      string            `json:"device_mode,omitempty"`
	Devices         []string          `json:"devices,omitempty"`
	ContainsMode    string            `json:"contains_mode,omitempty"`
	Contains        string            `json:"contains,omitempty"`
	Severities      []string          `json:"severities"`
	CooldownSeconds int               `json:"cooldown_seconds"`
	Type            string            `json:"type"`
	URL             string            `json:"url"`
	Headers         map[string]string `json:"headers,omitempty"`
	Payload         *string           `json:"payload,omitempty"`
}

type actionsConfig struct {
	Rules []actionRule `json:"rules"`
}

type actionEvent struct {
	Rule       string    `json:"rule"`
	Device     string    `json:"device"`
	Severity   string    `json:"severity"`
	Timestamp  time.Time `json:"timestamp"`
	ReportedAt string    `json:"reported_at"`
	Message    string    `json:"message"`
	Raw        string    `json:"raw"`
	Test       bool      `json:"test"`
}

type actionStatus struct {
	LastAttempt time.Time `json:"last_attempt"`
	LastSuccess time.Time `json:"last_success"`
	Error       string    `json:"error"`
	Pending     int       `json:"pending"`
	Dropped     uint64    `json:"dropped"`
}

type actionJob struct {
	rule     actionRule
	event    actionEvent
	revision uint64
}

type actionEngine struct {
	mu          sync.Mutex
	path        string
	config      actionsConfig
	fileInfo    os.FileInfo
	revision    uint64
	statuses    map[string]actionStatus
	cooldowns   map[string]map[string]time.Time
	configError string
	readerError string
	queue       chan actionJob
}

func newActionEngine(path string) *actionEngine {
	return &actionEngine{path: path, config: actionsConfig{Rules: []actionRule{}}, statuses: make(map[string]actionStatus), cooldowns: make(map[string]map[string]time.Time), queue: make(chan actionJob, 64)}
}

func parseActionsConfig(data []byte) (actionsConfig, error) {
	var config actionsConfig
	if len(data) > 1024*1024 {
		return config, fmt.Errorf("Actions configuration is too large")
	}
	if err := json.Unmarshal(data, &config); err != nil || config.Rules == nil {
		return config, fmt.Errorf("Actions must contain a rules array")
	}
	if len(config.Rules) > 100 {
		return config, fmt.Errorf("Use at most 100 action rules")
	}
	seen := make(map[string]bool)
	for i := range config.Rules {
		rule := &config.Rules[i]
		rule.Name = strings.TrimSpace(rule.Name)
		rule.Device = strings.TrimSpace(rule.Device)
		rule.URL = strings.TrimSpace(rule.URL)
		if rule.DeviceMode == "" {
			rule.DeviceMode = "include"
		}
		if rule.DeviceMode != "include" && rule.DeviceMode != "exclude" {
			return config, fmt.Errorf("Choose Include or Exclude for %s", rule.Name)
		}
		if rule.ContainsMode == "" {
			rule.ContainsMode = "include"
		}
		if rule.ContainsMode != "include" && rule.ContainsMode != "exclude" {
			return config, fmt.Errorf("Choose Include or Exclude for the message filter in %s", rule.Name)
		}
		if rule.Device != "" {
			if len(rule.Devices) > 0 || rule.DeviceMode == "exclude" {
				return config, fmt.Errorf("Use devices instead of the legacy device field for %s", rule.Name)
			}
			rule.Devices = []string{rule.Device}
			rule.Device = ""
		}
		if !validName(rule.ID) || seen[rule.ID] {
			return config, fmt.Errorf("Each action needs a unique ID")
		}
		seen[rule.ID] = true
		if rule.Name == "" || len(rule.Name) > 100 {
			return config, fmt.Errorf("Action names must contain 1–100 characters")
		}
		devices := make(map[string]bool, len(rule.Devices))
		for j, device := range rule.Devices {
			device = strings.TrimSpace(device)
			if !validName(device) || devices[device] {
				return config, fmt.Errorf("Device names must be valid and unique for %s", rule.Name)
			}
			rule.Devices[j] = device
			devices[device] = true
		}
		if len(rule.Contains) > 1024 {
			return config, fmt.Errorf("Message condition is too long for %s", rule.Name)
		}
		if rule.CooldownSeconds < 0 || rule.CooldownSeconds > 86400 {
			return config, fmt.Errorf("Cooldown must be between 0 and 86400 seconds")
		}
		if len(rule.Severities) == 0 || len(rule.Severities) > len(severityNames) {
			return config, fmt.Errorf("Select at least one severity for %s", rule.Name)
		}
		levels := make(map[string]bool)
		for j, level := range rule.Severities {
			normalized, ok := normalizeSeverityName(level)
			if !ok || levels[normalized] {
				return config, fmt.Errorf("Invalid or repeated severity for %s", rule.Name)
			}
			rule.Severities[j] = normalized
			levels[normalized] = true
		}
		if len(rule.URL) > 8192 {
			return config, fmt.Errorf("Destination URL is too long")
		}
		if rule.Payload != nil {
			if len(*rule.Payload) > 64*1024 {
				return config, fmt.Errorf("Custom payload must be no larger than 64 KiB")
			}
			if rule.Type == "webhook" && *rule.Payload != "" && !json.Valid([]byte(*rule.Payload)) {
				return config, fmt.Errorf("Custom webhook payload must be valid JSON or empty")
			}
			if rule.Type == "shoutrrr" && strings.TrimSpace(*rule.Payload) == "" {
				return config, fmt.Errorf("Custom Shoutrrr message must not be empty")
			}
		}
		switch rule.Type {
		case "webhook":
			u, err := url.Parse(rule.URL)
			if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
				return config, fmt.Errorf("Webhooks need an HTTP or HTTPS URL without embedded credentials or fragments")
			}
		case "shoutrrr":
			sender, err := shoutrrr.CreateSenderWithOptions(types.SenderOptions{}, rule.URL)
			if err != nil {
				return config, fmt.Errorf("Invalid Shoutrrr URL for %s; check the service URL format", rule.Name)
			}
			_ = sender.Close()
		default:
			return config, fmt.Errorf("Choose Shoutrrr or webhook for %s", rule.Name)
		}
		if len(rule.Headers) > 32 {
			return config, fmt.Errorf("Use at most 32 webhook headers")
		}
		for name, value := range rule.Headers {
			if rule.Type != "webhook" || !validActionHeader(name) || len(value) > 8192 || strings.ContainsAny(value, "\r\n\x00") {
				return config, fmt.Errorf("Invalid webhook header")
			}
		}
	}
	return config, nil
}

func validActionHeader(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", r)) {
			return false
		}
	}
	switch strings.ToLower(name) {
	case "host", "content-length", "transfer-encoding", "connection":
		return false
	}
	return true
}

// The caller holds mu, so saves, reloads and queued-job revisions stay in order.
func (engine *actionEngine) apply(config actionsConfig, info os.FileInfo) {
	engine.config = config
	engine.fileInfo = info
	engine.revision++
	engine.configError = ""
	engine.cooldowns = make(map[string]map[string]time.Time)
	statuses := make(map[string]actionStatus, len(config.Rules))
	for _, rule := range config.Rules {
		status := engine.statuses[rule.ID]
		status.Pending = 0
		statuses[rule.ID] = status
	}
	engine.statuses = statuses
}

func (engine *actionEngine) reload() error {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	info, err := os.Stat(engine.path)
	if err != nil {
		engine.configError = "Unable to read actions.json"
		return fmt.Errorf("%s", engine.configError)
	}
	if engine.configError == "" && engine.fileInfo != nil && os.SameFile(info, engine.fileInfo) && info.ModTime().Equal(engine.fileInfo.ModTime()) && info.Size() == engine.fileInfo.Size() {
		return nil
	}
	file, err := os.Open(engine.path)
	if err != nil {
		engine.configError = "Unable to read actions.json"
		return fmt.Errorf("%s", engine.configError)
	}
	data, err := io.ReadAll(io.LimitReader(file, 1024*1024+1))
	file.Close()
	if err != nil {
		engine.configError = "Unable to read actions.json"
		return fmt.Errorf("%s", engine.configError)
	}
	config, err := parseActionsConfig(data)
	if err != nil {
		engine.configError = err.Error()
		return err
	}
	engine.apply(config, info)
	return nil
}

func (engine *actionEngine) saveLocked(config actionsConfig) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	config, err = parseActionsConfig(data)
	if err != nil {
		return err
	}
	if err = writeSettingsFileMode(engine.path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("Unable to save actions.json")
	}
	info, err := os.Stat(engine.path)
	if err != nil {
		return err
	}
	engine.apply(config, info)
	return nil
}

func (rule actionRule) matches(event actionEvent) bool {
	if !rule.Enabled {
		return false
	}
	if len(rule.Devices) > 0 {
		deviceMatch := slices.Contains(rule.Devices, event.Device)
		if rule.DeviceMode == "exclude" {
			deviceMatch = !deviceMatch
		}
		if !deviceMatch {
			return false
		}
	}
	levelMatch := false
	for _, level := range rule.Severities {
		if level == event.Severity {
			levelMatch = true
			break
		}
	}
	if !levelMatch || rule.Contains == "" {
		return levelMatch
	}
	message := strings.ToLower(event.Message)
	hasPhrases := false
	for _, phrase := range strings.Split(rule.Contains, "\n") {
		phrase = strings.TrimSuffix(phrase, "\r")
		if strings.TrimSpace(phrase) == "" {
			continue
		}
		hasPhrases = true
		if strings.Contains(message, strings.ToLower(phrase)) {
			return rule.ContainsMode != "exclude"
		}
	}
	return !hasPhrases || rule.ContainsMode == "exclude"
}

func (engine *actionEngine) active() bool {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	for _, rule := range engine.config.Rules {
		if rule.Enabled {
			return true
		}
	}
	return false
}

func parseActionEvent(line string, now time.Time) (actionEvent, bool) {
	stored := parseStoredLogLine(line)
	parts := strings.SplitN(stored.visible, " ", 4)
	if len(parts) != 4 {
		return actionEvent{}, false
	}
	severity, ok := normalizeSeverityName(parts[2])
	if !ok {
		return actionEvent{}, false
	}
	at := stored.ingest
	if at.IsZero() {
		at = now
	}
	return actionEvent{Device: parts[1], Severity: severity, Timestamp: at, ReportedAt: parts[0], Message: parts[3], Raw: stored.visible}, true
}

func (engine *actionEngine) consider(line string, now time.Time) {
	event, ok := parseActionEvent(line, now)
	if !ok {
		return
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	for _, rule := range engine.config.Rules {
		if !rule.matches(event) {
			continue
		}
		if last := engine.cooldowns[rule.ID][event.Device]; !last.IsZero() && now.Sub(last) < time.Duration(rule.CooldownSeconds)*time.Second {
			continue
		}
		if engine.enqueueLocked(rule, event) {
			if engine.cooldowns[rule.ID] == nil {
				engine.cooldowns[rule.ID] = make(map[string]time.Time)
			}
			engine.cooldowns[rule.ID][event.Device] = now
		}
	}
}

func (engine *actionEngine) enqueueLocked(rule actionRule, event actionEvent) bool {
	event.Rule = rule.Name
	status := engine.statuses[rule.ID]
	select {
	case engine.queue <- actionJob{rule: rule, event: event, revision: engine.revision}:
		status.Pending++
	default:
		status.Dropped++
		status.Error = "Delivery queue full; notification dropped"
		engine.statuses[rule.ID] = status
		return false
	}
	engine.statuses[rule.ID] = status
	return true
}

func (engine *actionEngine) test(id string) error {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	for _, rule := range engine.config.Rules {
		if rule.ID == id {
			device := "syslog-flow"
			if rule.DeviceMode != "exclude" && len(rule.Devices) > 0 {
				device = rule.Devices[0]
			}
			now := appNow()
			event := actionEvent{Device: device, Severity: rule.Severities[0], Timestamp: now, ReportedAt: now.Format(time.RFC3339), Message: "Test notification from syslog-flow", Raw: "Test notification from syslog-flow", Test: true}
			if !engine.enqueueLocked(rule, event) {
				return fmt.Errorf("Delivery queue is full; try again shortly")
			}
			return nil
		}
	}
	return fmt.Errorf("Action not found")
}

func (engine *actionEngine) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-engine.queue:
			engine.mu.Lock()
			if job.revision != engine.revision {
				engine.mu.Unlock()
				continue
			}
			status := engine.statuses[job.rule.ID]
			status.LastAttempt = appNow()
			engine.statuses[job.rule.ID] = status
			engine.mu.Unlock()
			deliveryCtx, cancel := context.WithTimeout(ctx, actionTimeout)
			err := sendAction(deliveryCtx, job.rule, job.event)
			cancel()
			engine.mu.Lock()
			if job.revision == engine.revision {
				status = engine.statuses[job.rule.ID]
				status.Pending--
				status.Error = ""
				if err != nil {
					status.Error = err.Error()
				} else {
					status.LastSuccess = appNow()
				}
				engine.statuses[job.rule.ID] = status
			}
			engine.mu.Unlock()
		}
	}
}

func actionPayload(rule actionRule, event actionEvent) ([]byte, error) {
	if rule.Payload != nil {
		return []byte(*rule.Payload), nil
	}
	if rule.Type == "shoutrrr" {
		return []byte(fmt.Sprintf("[%s] %s\n%s\n%s", strings.ToUpper(event.Severity), event.Device, event.Timestamp.Format(time.RFC3339), event.Message)), nil
	}
	return json.Marshal(event)
}

func sendAction(ctx context.Context, rule actionRule, event actionEvent) error {
	data, err := actionPayload(rule, event)
	if err != nil {
		return err
	}
	if rule.Type == "shoutrrr" {
		params := types.Params{}
		params.SetTitle("syslog-flow: " + rule.Name)
		if err := shoutrrr.SendContext(ctx, rule.URL, string(data), &params); err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("Shoutrrr delivery timed out or was cancelled")
			}
			// Service errors can include credentials in response text; don't expose them.
			return fmt.Errorf("Shoutrrr delivery failed; check the URL, credentials and destination")
		}
		return nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, rule.URL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("Invalid webhook URL")
	}
	request.Header.Set("Content-Type", "application/json")
	for name, value := range rule.Headers {
		request.Header.Set(name, value)
	}
	client := &http.Client{Timeout: actionTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("Webhook delivery timed out or was cancelled")
		}
		return fmt.Errorf("Webhook connection failed; check the destination and network")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Webhook returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (engine *actionEngine) setReaderError(err error) {
	message := ""
	if err != nil {
		message = "Unable to monitor logs: " + err.Error()
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if message != "" && message != engine.readerError {
		log.Print(message)
	}
	engine.readerError = message
}
