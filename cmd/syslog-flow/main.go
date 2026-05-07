package main

import (
	"bufio"
	"encoding/json"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	logRoot            = "/logs"
	addr               = ":2200"
	deviceColorPath    = "/config/device-colors.json"
	statusColorPath    = "/config/status-colors.json"
	interfaceColorPath = "/config/interface-colors.json"
	appConfigPath      = "/config/app.json"
	faviconPath        = "/resources/favicon.ico"
	appleIconPath      = "/resources/apple-touch-icon.png"
	dayChunkSize       = 500
)

var appLocation = loadAppLocation()

type Day struct {
	Name  string
	Files []LogFile
}

type LogFile struct {
	Name string
	Size int64
}

type PageData struct {
	Days              []Day
	Selected          string
	Files             []LogFile
	File              string
	Query             string
	Severity          string
	Lines             []string
	Error             string
	Global            bool
	Live              bool
	Overview          bool
	ResultInfo        string
	SyslogEndpoint    string
	Critical5m        string
	TodayLines        string
	AllLines          string
	TotalLogSize      string
	LinesPerSecond    string
	RefreshURL        string
	LiveRefreshMS     int
	StatsRefreshMS    int
	OverviewRefreshMS int
	OlderURL          string
	ChunkStart        int
	TotalLogLines     int
	HasOlder          bool
	LatestDay         string
	DayCount          string
	DeviceCount       string
	Devices           []DeviceSummary
	RecentLines       []string
}

type DeviceSummary struct {
	Name     string
	Day      string
	Link     string
	Lines    string
	LineInfo string
	LastSeen string
	IP       string
	Color    string
}

type DashboardData struct {
	LatestDay   string
	DayCount    string
	DeviceCount string
	Devices     []DeviceSummary
}

type deviceRecord struct {
	name  string
	day   string
	lines int
	mod   time.Time
}

type recentRecord struct {
	line string
	at   time.Time
}

type logRecord struct {
	line string
	at   time.Time
}

type lineStats struct {
	critical5m     int
	todayDay       string
	todayLines     int
	allLines       int
	linesPerSecond float64
	expires        time.Time
}

type lineSample struct {
	at       time.Time
	allLines int
}

type logPayload struct {
	Lines         []string `json:"lines"`
	Start         int      `json:"start,omitempty"`
	Total         int      `json:"total,omitempty"`
	HasMoreBefore bool     `json:"hasMoreBefore,omitempty"`
}

type statsPayload struct {
	Critical5m     string `json:"critical5m"`
	TodayLines     string `json:"todayLines"`
	AllLines       string `json:"allLines"`
	LinesPerSecond string `json:"linesPerSecond"`
	DayCount       string `json:"dayCount"`
	DeviceCount    string `json:"deviceCount"`
}

type apiStatsPayload struct {
	Critical5m     int     `json:"critical_5m"`
	TodayLines     int     `json:"today_lines"`
	AllLines       int     `json:"all_lines"`
	LogBytes       int64   `json:"log_bytes"`
	LinesPerSecond float64 `json:"lines_per_second"`
	LogDays        int     `json:"log_days"`
	Devices        int     `json:"devices"`
}

type statsSnapshot struct {
	critical5m     int
	todayLines     int
	allLines       int
	totalLogSize   int64
	linesPerSecond float64
	dayCount       int
	deviceCount    int
}

type deviceColorConfig struct {
	modTime time.Time
	colors  map[string]string
}

type statusColorConfig struct {
	modTime time.Time
	colors  map[string]string
}

type interfaceColorsConfig struct {
	modTime time.Time
	light   map[string]string
	dark    map[string]string
}

type appConfig struct {
	modTime                time.Time
	LiveRefreshSeconds     int `json:"live_refresh_seconds"`
	StatsRefreshSeconds    int `json:"stats_refresh_seconds"`
	OverviewRefreshSeconds int `json:"overview_refresh_seconds"`
}

type fileSummary struct {
	modTime   time.Time
	size      int64
	lineCount int
	tail      []string
	tailLimit int
}

type storedLogLine struct {
	visible string
	ingest  time.Time
}

var statsCache struct {
	sync.Mutex
	lineStats
	samples []lineSample
}

var fileCache = struct {
	sync.Mutex
	files map[string]fileSummary
}{
	files: make(map[string]fileSummary),
}

var colorCache struct {
	sync.Mutex
	deviceColorConfig
}

var statusCache struct {
	sync.Mutex
	statusColorConfig
}

var interfaceColorCache struct {
	sync.Mutex
	interfaceColorsConfig
}

var appConfigCache struct {
	sync.Mutex
	appConfig
}

func main() {
	if err := ensureConfigFiles(); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(logRoot, 0o755); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/api/overview", handleAPIOverview)
	mux.HandleFunc("/api/stats", handleAPIStats)
	mux.HandleFunc("/apple-touch-icon.png", serveAppleTouchIcon)
	mux.HandleFunc("/statistics", handleOverview)
	mux.HandleFunc("/day/", handleDay)
	mux.HandleFunc("/favicon.ico", serveFavicon)
	mux.HandleFunc("/search", handleSearch)

	log.Printf("syslog-flow listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func dashboardData(days []Day) (DashboardData, error) {
	data := DashboardData{
		LatestDay:   days[0].Name,
		DayCount:    formatInt(len(days)),
		DeviceCount: "0",
	}

	devices := make(map[string]deviceRecord)
	for _, day := range days {
		files, err := listFiles(day.Name)
		if err != nil {
			return data, err
		}
		for _, file := range files {
			path := filepath.Join(logRoot, day.Name, file.Name)
			summary, err := summarizeFile(path, 12)
			if err != nil {
				return data, err
			}
			lastSeen := summary.modTime
			if len(summary.tail) > 0 {
				lastSeen = internalLineTime(summary.tail[len(summary.tail)-1], summary.modTime)
			}

			record := deviceRecord{
				name:  strings.TrimSuffix(file.Name, ".log"),
				day:   day.Name,
				lines: summary.lineCount,
				mod:   lastSeen,
			}
			if existing, ok := devices[record.name]; !ok || record.mod.After(existing.mod) {
				devices[record.name] = record
			}
		}
	}

	records := make([]deviceRecord, 0, len(devices))
	for _, record := range devices {
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].mod.After(records[j].mod)
	})

	data.DeviceCount = formatInt(len(devices))
	for _, record := range records {
		data.Devices = append(data.Devices, DeviceSummary{
			Name:     record.name,
			Day:      record.day,
			Link:     deviceDayLink(record.day, record.name),
			Lines:    formatInt(record.lines),
			LineInfo: formatLineInfo(record.day, record.lines),
			LastSeen: formatSeen(record.mod),
			Color:    deviceColor(record.name),
		})
	}

	return data, nil
}

func liveLines(days []Day, limit int) ([]string, error) {
	var records []recentRecord
	for _, day := range days {
		files, err := listFiles(day.Name)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			path := filepath.Join(logRoot, day.Name, file.Name)
			summary, err := summarizeFile(path, limit)
			if err != nil {
				return nil, err
			}
			for _, line := range summary.tail {
				records = append(records, recentRecord{
					line: day.Name + "/" + file.Name + "  " + line,
					at:   internalLineTime(line, summary.modTime),
				})
			}
		}
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].at.Before(records[j].at)
	})
	if len(records) > limit {
		records = records[len(records)-limit:]
	}

	lines := make([]string, 0, len(records))
	for _, record := range records {
		lines = append(lines, record.line)
	}
	return lines, nil
}

func addStats(r *http.Request, data *PageData) {
	snapshot, err := buildStatsSnapshot(data.Days)
	if err != nil && data.Error == "" {
		data.Error = err.Error()
	}
	config := currentAppConfig()
	data.SyslogEndpoint = syslogEndpoint(r)
	data.LiveRefreshMS = config.LiveRefreshSeconds * 1000
	data.StatsRefreshMS = config.StatsRefreshSeconds * 1000
	data.OverviewRefreshMS = config.OverviewRefreshSeconds * 1000
	applyStatsSnapshot(data, snapshot)
	if data.Query == "" && (data.Live || data.Selected != "") {
		data.RefreshURL = logRefreshURL(r)
	}
}

func buildStatsSnapshot(days []Day) (statsSnapshot, error) {
	stats, err := currentLineStats()
	snapshot := statsSnapshot{
		critical5m:     stats.critical5m,
		todayLines:     stats.todayLines,
		allLines:       stats.allLines,
		totalLogSize:   totalLogBytes(days),
		linesPerSecond: stats.linesPerSecond,
		dayCount:       len(days),
		deviceCount:    deviceCount(days),
	}
	return snapshot, err
}

func applyStatsSnapshot(data *PageData, snapshot statsSnapshot) {
	data.Critical5m = formatInt(snapshot.critical5m)
	data.TodayLines = formatInt(snapshot.todayLines)
	data.AllLines = formatInt(snapshot.allLines)
	data.TotalLogSize = formatBytes(snapshot.totalLogSize)
	data.LinesPerSecond = formatRate(snapshot.linesPerSecond)
	data.DayCount = formatInt(snapshot.dayCount)
	data.DeviceCount = formatInt(snapshot.deviceCount)
}

func statsPayloadFromSnapshot(snapshot statsSnapshot) statsPayload {
	return statsPayload{
		Critical5m:     formatInt(snapshot.critical5m),
		TodayLines:     formatInt(snapshot.todayLines),
		AllLines:       formatInt(snapshot.allLines),
		LinesPerSecond: formatRate(snapshot.linesPerSecond),
		DayCount:       formatInt(snapshot.dayCount),
		DeviceCount:    formatInt(snapshot.deviceCount),
	}
}

func overviewPayloadFromData(snapshot statsSnapshot, dashboard DashboardData) overviewPayload {
	payload := overviewPayload{
		AllLines:     formatInt(snapshot.allLines),
		TotalLogSize: formatBytes(snapshot.totalLogSize),
		DayCount:     dashboard.DayCount,
		DeviceCount:  dashboard.DeviceCount,
		Devices:      make([]devicePayload, 0, len(dashboard.Devices)),
	}
	for _, device := range dashboard.Devices {
		payload.Devices = append(payload.Devices, devicePayload{
			Name:     device.Name,
			Link:     device.Link,
			LineInfo: device.LineInfo,
			LastSeen: device.LastSeen,
			IP:       device.IP,
			Color:    device.Color,
		})
	}
	return payload
}

func deviceCount(days []Day) int {
	devices := make(map[string]struct{})
	for _, day := range days {
		for _, file := range day.Files {
			devices[strings.TrimSuffix(file.Name, ".log")] = struct{}{}
		}
	}
	return len(devices)
}

func totalLogBytes(days []Day) int64 {
	var total int64
	for _, day := range days {
		for _, file := range day.Files {
			total += file.Size
		}
	}
	return total
}

func deviceDayLink(day, device string) string {
	values := url.Values{
		"file": []string{device + ".log"},
	}
	return "/day/" + day + "?" + values.Encode()
}

func currentLineStats() (lineStats, error) {
	now := appNow()
	today := now.Format("2006/01/02")
	refreshInterval := time.Duration(currentAppConfig().StatsRefreshSeconds) * time.Second

	statsCache.Lock()
	cached := statsCache.lineStats
	if now.Before(cached.expires) && cached.todayDay == today {
		statsCache.Unlock()
		return cached, nil
	}
	statsCache.Unlock()

	stats, err := countLineStats(today)
	stats.linesPerSecond = updateLineRate(now, stats.allLines)
	stats.expires = now.Add(refreshInterval)
	stats.todayDay = today

	statsCache.Lock()
	statsCache.lineStats = stats
	statsCache.Unlock()

	return stats, err
}

func countLineStats(today string) (lineStats, error) {
	stats := lineStats{todayDay: today}
	cutoff := appNow().Add(-5 * time.Minute)
	err := filepath.WalkDir(logRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			return nil
		}
		summary, err := summarizeFile(path, 2048)
		if err != nil {
			return err
		}
		stats.allLines += summary.lineCount
		rel, err := filepath.Rel(logRoot, path)
		if err != nil {
			return err
		}
		if strings.HasPrefix(filepath.ToSlash(rel), today+"/") {
			stats.todayLines += summary.lineCount
		}
		if summary.modTime.After(cutoff) || summary.modTime.Equal(cutoff) {
			criticalCount, err := countCriticalSince(path, cutoff)
			if err != nil {
				return err
			}
			stats.critical5m += criticalCount
		}
		return nil
	})
	return stats, err
}

func countCriticalSince(path string, cutoff time.Time) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	var count int
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if internalLineTime(line, time.Time{}).Before(cutoff) {
			continue
		}
		if isCriticalSeverity(line) {
			count++
		}
	}
	return count, scanner.Err()
}

func isCriticalSeverity(line string) bool {
	fields := strings.Fields(visibleLogText(line))
	if len(fields) < 3 {
		return false
	}
	severity, ok := normalizeSeverityName(fields[2])
	if !ok {
		return false
	}
	return severity == "emerg" || severity == "alert" || severity == "crit"
}

func updateLineRate(now time.Time, allLines int) float64 {
	statsCache.Lock()
	defer statsCache.Unlock()

	samples := append(statsCache.samples, lineSample{at: now, allLines: allLines})
	cutoff := now.Add(-60 * time.Second)

	trimmed := make([]lineSample, 0, len(samples))
	for _, sample := range samples {
		if sample.at.Before(cutoff) {
			continue
		}
		trimmed = append(trimmed, sample)
	}
	if len(trimmed) == 0 {
		trimmed = append(trimmed, lineSample{at: now, allLines: allLines})
	}
	statsCache.samples = trimmed

	if len(trimmed) < 2 {
		return 0
	}

	oldest := trimmed[0]
	newest := trimmed[len(trimmed)-1]
	seconds := newest.at.Sub(oldest.at).Seconds()
	if seconds <= 0 {
		return 0
	}

	delta := newest.allLines - oldest.allLines
	if delta < 0 {
		return 0
	}
	return float64(delta) / seconds
}

func summarizeFile(path string, tailLimit int) (fileSummary, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileSummary{}, err
	}

	fileCache.Lock()
	cached, ok := fileCache.files[path]
	fileCache.Unlock()

	if ok && cached.size == info.Size() && cached.modTime.Equal(info.ModTime()) && cached.tailLimit >= tailLimit {
		return sliceSummaryTail(cached, tailLimit), nil
	}

	file, err := os.Open(path)
	if err != nil {
		return fileSummary{}, err
	}
	defer file.Close()

	summary := fileSummary{
		modTime:   info.ModTime(),
		size:      info.Size(),
		tailLimit: tailLimit,
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		summary.lineCount++
		if tailLimit == 0 {
			continue
		}
		line := scanner.Text()
		if line == "" {
			continue
		}
		summary.tail = append(summary.tail, line)
		if len(summary.tail) > tailLimit {
			copy(summary.tail, summary.tail[1:])
			summary.tail = summary.tail[:tailLimit]
		}
	}
	if err := scanner.Err(); err != nil {
		return fileSummary{}, err
	}

	fileCache.Lock()
	fileCache.files[path] = summary
	fileCache.Unlock()

	return sliceSummaryTail(summary, tailLimit), nil
}

func sliceSummaryTail(summary fileSummary, tailLimit int) fileSummary {
	if tailLimit <= 0 || len(summary.tail) <= tailLimit {
		return summary
	}

	trimmed := summary
	trimmed.tail = append([]string(nil), summary.tail[len(summary.tail)-tailLimit:]...)
	trimmed.tailLimit = tailLimit
	return trimmed
}

func parseStoredLogLine(line string) storedLogLine {
	prefix, rest, ok := strings.Cut(line, " | ")
	if !ok {
		return storedLogLine{visible: line}
	}

	parsed := storedLogLine{visible: rest}
	if ingest, err := time.Parse(time.RFC3339, strings.TrimSpace(prefix)); err == nil {
		parsed.ingest = ingest
	}
	return parsed
}

func visibleLogText(line string) string {
	return parseStoredLogLine(line).visible
}

func internalLineTime(line string, fallback time.Time) time.Time {
	parsed := parseStoredLogLine(line)
	if !parsed.ingest.IsZero() {
		return parsed.ingest
	}
	return fallback
}

func appNow() time.Time {
	return time.Now().In(appLocation)
}

func loadAppLocation() *time.Location {
	name := strings.TrimSpace(os.Getenv("TZ"))
	if name == "" {
		return time.Local
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		log.Printf("invalid TZ %q, falling back to system local time: %v", name, err)
		return time.Local
	}
	return location
}

func listDays() ([]Day, error) {
	var days []Day
	err := filepath.WalkDir(logRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() || path == logRoot {
			return nil
		}

		rel, err := filepath.Rel(logRoot, path)
		if err != nil {
			return err
		}
		day := filepath.ToSlash(rel)
		if len(strings.Split(day, "/")) < 3 {
			return nil
		}
		if !validDayPath(day) {
			return filepath.SkipDir
		}
		files, err := listFiles(day)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			return nil
		}
		days = append(days, Day{Name: day, Files: files})
		return filepath.SkipDir
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(days, func(i, j int) bool {
		return days[i].Name > days[j].Name
	})
	return days, nil
}

func listFiles(day string) ([]LogFile, error) {
	path := filepath.Join(logRoot, day)
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	files := make([]LogFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") || !validName(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, LogFile{Name: entry.Name(), Size: info.Size()})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name < files[j].Name
	})
	return files, nil
}

func readDayRecords(day, file string, filter logFilter) ([]logRecord, error) {
	if file != "" {
		records, err := scanFileRecords(filepath.Join(logRoot, day, file), filter, day+"/"+file)
		sortLogRecords(records)
		return records, err
	}

	files, err := listFiles(day)
	if err != nil {
		return nil, err
	}

	var records []logRecord
	for _, f := range files {
		found, err := scanFileRecords(filepath.Join(logRoot, day, f.Name), filter, day+"/"+f.Name)
		if err != nil {
			return records, err
		}
		records = append(records, found...)
	}
	sortLogRecords(records)
	return records, nil
}

func readDayWindow(day, file string, filter logFilter, before, limit int) ([]string, int, int, error) {
	records, err := readDayRecords(day, file, filter)
	total := len(records)
	if limit <= 0 || limit > total {
		limit = total
	}

	end := total
	if before >= 0 && before < end {
		end = before
	}
	start := end - limit
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}

	return recordLines(records[start:end]), start, total, err
}

func searchAll(query string) ([]string, error) {
	if query == "" {
		return nil, nil
	}

	var records []logRecord
	err := filepath.WalkDir(logRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			return nil
		}
		rel, err := filepath.Rel(logRoot, path)
		if err != nil {
			return err
		}
		found, err := scanFileRecords(path, logFilter{Query: query}, rel)
		if err != nil {
			return err
		}
		records = append(records, found...)
		return nil
	})
	sortLogRecords(records)
	return recordLines(records), err
}

func scanFileRecords(path string, filter logFilter, label string) ([]logRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var records []logRecord
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		visible := visibleLogText(line)
		if matchesLogFilter(visible, filter) {
			records = append(records, logRecord{
				line: label + "  " + line,
				at:   internalLineTime(line, time.Time{}),
			})
		}
	}
	return records, scanner.Err()
}

func sortLogRecords(records []logRecord) {
	sort.SliceStable(records, func(i, j int) bool {
		return records[i].at.Before(records[j].at)
	})
}

func recordLines(records []logRecord) []string {
	lines := make([]string, 0, len(records))
	for _, record := range records {
		lines = append(lines, record.line)
	}
	return lines
}

func resultInfo(count int, filter logFilter) string {
	if filter.Query == "" && filter.Severity == "" {
		return ""
	}
	if count == 1 {
		return "1 matching line"
	}
	return strconv.Itoa(count) + " matching lines"
}

func formatInt(n int) string {
	raw := strconv.Itoa(n)
	if len(raw) <= 3 {
		return raw
	}

	var b strings.Builder
	first := len(raw) % 3
	if first == 0 {
		first = 3
	}
	b.WriteString(raw[:first])
	for i := first; i < len(raw); i += 3 {
		b.WriteByte(',')
		b.WriteString(raw[i : i+3])
	}
	return b.String()
}

func formatBytes(size int64) string {
	const unit = 1024
	if size < unit {
		return strconv.FormatInt(size, 10) + "B"
	}

	value := float64(size)
	suffixes := []string{"KB", "MB", "GB", "TB"}
	suffix := suffixes[0]
	for i := 0; i < len(suffixes) && value >= unit; i++ {
		value /= unit
		suffix = suffixes[i]
	}

	decimals := 2
	if value >= 10 {
		decimals = 1
	}
	if value >= 100 {
		decimals = 0
	}

	return strconv.FormatFloat(value, 'f', decimals, 64) + suffix
}

func formatSeen(t time.Time) string {
	now := appNow()
	seen := t.In(appLocation)
	if seen.Format("2006-01-02") == now.Format("2006-01-02") {
		return "today " + seen.Format("15:04")
	}
	return seen.Format("2006/01/02 15:04")
}

func formatLineInfo(day string, lines int) string {
	label := formatInt(lines) + " lines"
	today := appNow().Format("2006/01/02")
	if day == today {
		return label + " today"
	}
	return label + " on " + day
}

func formatRate(rate float64) string {
	if rate >= 10 {
		return strconv.FormatFloat(rate, 'f', 0, 64)
	}
	if rate >= 1 {
		return strconv.FormatFloat(rate, 'f', 1, 64)
	}
	return strconv.FormatFloat(rate, 'f', 2, 64)
}

func deviceColor(name string) string {
	config, err := loadDeviceColors()
	if err != nil {
		return ""
	}
	return config[name]
}

func logHeadingColor(line string) string {
	return deviceColor(logDevice(line))
}

func deviceColorsJSON() template.JS {
	colors, err := loadDeviceColors()
	if err != nil || len(colors) == 0 {
		return template.JS("{}")
	}
	data, err := json.Marshal(colors)
	if err != nil {
		return template.JS("{}")
	}
	return template.JS(data)
}

func statusColorsJSON() template.JS {
	colors, err := loadStatusColors()
	if err != nil || len(colors) == 0 {
		return template.JS("{}")
	}
	data, err := json.Marshal(colors)
	if err != nil {
		return template.JS("{}")
	}
	return template.JS(data)
}

func interfaceThemeDeclarations(mode string) template.CSS {
	theme, err := loadInterfaceColors()
	if err != nil {
		theme = defaultInterfaceColorsFile()
	}

	values := theme.Light
	if mode == "dark" {
		values = theme.Dark
	}

	var b strings.Builder
	for _, key := range interfaceThemeKeys() {
		b.WriteString("--")
		b.WriteString(key)
		b.WriteString(": ")
		b.WriteString(values[key])
		b.WriteString(";\n      ")
	}
	return template.CSS(strings.TrimSpace(b.String()))
}

func loadDeviceColors() (map[string]string, error) {
	info, err := os.Stat(deviceColorPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	colorCache.Lock()
	cached := colorCache.deviceColorConfig
	colorCache.Unlock()

	if cached.colors != nil && cached.modTime.Equal(info.ModTime()) {
		return cached.colors, nil
	}

	data, err := os.ReadFile(deviceColorPath)
	if err != nil {
		return nil, err
	}

	raw := make(map[string]string)
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
	}

	colors := make(map[string]string, len(raw))
	for name, color := range raw {
		if !validName(name) {
			continue
		}
		if normalized, ok := normalizeHexColor(color); ok {
			colors[name] = normalized
		}
	}

	colorCache.Lock()
	colorCache.deviceColorConfig = deviceColorConfig{
		modTime: info.ModTime(),
		colors:  colors,
	}
	colorCache.Unlock()

	return colors, nil
}

func loadStatusColors() (map[string]string, error) {
	info, err := os.Stat(statusColorPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	statusCache.Lock()
	cached := statusCache.statusColorConfig
	statusCache.Unlock()

	if cached.colors != nil && cached.modTime.Equal(info.ModTime()) {
		return cached.colors, nil
	}

	data, err := os.ReadFile(statusColorPath)
	if err != nil {
		return nil, err
	}

	raw := make(map[string]string)
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
	}

	colors := make(map[string]string, len(raw))
	for severity, color := range raw {
		if normalizedSeverity, ok := normalizeSeverityName(severity); ok {
			if normalizedColor, ok := normalizeHexColor(color); ok {
				colors[normalizedSeverity] = normalizedColor
			}
		}
	}

	statusCache.Lock()
	statusCache.statusColorConfig = statusColorConfig{
		modTime: info.ModTime(),
		colors:  colors,
	}
	statusCache.Unlock()

	return colors, nil
}

func loadInterfaceColors() (interfaceColorsFile, error) {
	defaults := defaultInterfaceColorsFile()

	info, err := os.Stat(interfaceColorPath)
	if err != nil {
		if os.IsNotExist(err) {
			return defaults, nil
		}
		return defaults, err
	}

	interfaceColorCache.Lock()
	cached := interfaceColorCache.interfaceColorsConfig
	interfaceColorCache.Unlock()

	if cached.light != nil && cached.dark != nil && cached.modTime.Equal(info.ModTime()) {
		return interfaceColorsFile{
			Light: cached.light,
			Dark:  cached.dark,
		}, nil
	}

	data, err := os.ReadFile(interfaceColorPath)
	if err != nil {
		return defaults, err
	}

	raw := interfaceColorsFile{}
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &raw); err != nil {
			return defaults, err
		}
	}

	colors := interfaceColorsFile{
		Light: mergeInterfaceTheme(defaults.Light, raw.Light),
		Dark:  mergeInterfaceTheme(defaults.Dark, raw.Dark),
	}

	interfaceColorCache.Lock()
	interfaceColorCache.interfaceColorsConfig = interfaceColorsConfig{
		modTime: info.ModTime(),
		light:   colors.Light,
		dark:    colors.Dark,
	}
	interfaceColorCache.Unlock()

	return colors, nil
}

func mergeInterfaceTheme(defaults, overrides map[string]string) map[string]string {
	merged := make(map[string]string, len(defaults))
	for _, key := range interfaceThemeKeys() {
		value := defaults[key]
		if override, ok := overrides[key]; ok && validInterfaceThemeValue(override) {
			value = strings.TrimSpace(override)
		}
		merged[key] = value
	}
	return merged
}

func interfaceThemeKeys() []string {
	return []string{
		"bg",
		"panel",
		"panel-soft",
		"panel-strong",
		"panel-card",
		"ink",
		"muted",
		"line",
		"accent",
		"accent-strong",
		"active-bg",
		"active-ink",
		"input-bg",
		"code",
		"code-ink",
		"error-bg",
		"error-line",
		"error-ink",
		"glow-soft",
		"glow-card",
		"shadow",
	}
}

func validInterfaceThemeValue(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case strings.ContainsRune("#(),.% -", r):
		default:
			return false
		}
	}
	return true
}

func normalizeHexColor(color string) (string, bool) {
	color = strings.TrimSpace(color)
	if len(color) != 7 || !strings.HasPrefix(color, "#") {
		return "", false
	}
	for _, r := range color[1:] {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			continue
		}
		return "", false
	}
	return strings.ToUpper(color), true
}

func normalizeSeverityName(name string) (string, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "emerg", "alert", "crit", "err", "warning", "notice", "info", "debug":
		return name, true
	default:
		return "", false
	}
}

func statusColor(severity string) string {
	colors, err := loadStatusColors()
	if err != nil {
		return ""
	}
	return colors[severity]
}

func currentAppConfig() appConfig {
	config, err := loadAppConfig()
	if err != nil {
		return defaultAppConfig()
	}
	return config
}

func defaultAppConfig() appConfig {
	return appConfig{
		LiveRefreshSeconds:     2,
		StatsRefreshSeconds:    10,
		OverviewRefreshSeconds: 10,
	}
}

func loadAppConfig() (appConfig, error) {
	defaults := defaultAppConfig()

	info, err := os.Stat(appConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return defaults, nil
		}
		return defaults, err
	}

	appConfigCache.Lock()
	cached := appConfigCache.appConfig
	appConfigCache.Unlock()

	if cached.LiveRefreshSeconds > 0 && cached.StatsRefreshSeconds > 0 && cached.OverviewRefreshSeconds > 0 && cached.modTime.Equal(info.ModTime()) {
		return cached, nil
	}

	data, err := os.ReadFile(appConfigPath)
	if err != nil {
		return defaults, err
	}

	config := defaults
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &config); err != nil {
			return defaults, err
		}
	}

	config.modTime = info.ModTime()
	config.LiveRefreshSeconds = clampRefreshSeconds(config.LiveRefreshSeconds, defaults.LiveRefreshSeconds)
	config.StatsRefreshSeconds = clampRefreshSeconds(config.StatsRefreshSeconds, defaults.StatsRefreshSeconds)
	config.OverviewRefreshSeconds = clampRefreshSeconds(config.OverviewRefreshSeconds, defaults.OverviewRefreshSeconds)

	appConfigCache.Lock()
	appConfigCache.appConfig = config
	appConfigCache.Unlock()

	return config, nil
}

func clampRefreshSeconds(value, fallback int) int {
	if value < 1 {
		return fallback
	}
	if value > 3600 {
		return 3600
	}
	return value
}

func validName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func validDayPath(day string) bool {
	parts := strings.Split(day, "/")
	if len(parts) != 3 {
		return false
	}
	if len(parts[0]) != 4 || len(parts[1]) != 2 || len(parts[2]) != 2 {
		return false
	}
	for _, part := range parts {
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}
