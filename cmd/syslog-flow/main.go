package main

import (
	"bufio"
	"container/heap"
	"encoding/json"
	"html/template"
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
	maxSearchResults   = 5000
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
	RefreshCursor     int64
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

type searchResults struct {
	lines   []string
	limited bool
}

type logRecordHeap []logRecord

func (h logRecordHeap) Len() int { return len(h) }
func (h logRecordHeap) Less(i, j int) bool {
	if h[i].at.Equal(h[j].at) {
		return h[i].line < h[j].line
	}
	return h[i].at.Before(h[j].at)
}
func (h logRecordHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *logRecordHeap) Push(value any) {
	*h = append(*h, value.(logRecord))
}
func (h *logRecordHeap) Pop() any {
	old := *h
	value := old[len(old)-1]
	*h = old[:len(old)-1]
	return value
}

type dayRecordStream struct {
	file    *os.File
	scanner *bufio.Scanner
	label   string
	filter  logFilter
	record  logRecord
	err     error
}

type dayRecordStreamHeap []*dayRecordStream

func (h dayRecordStreamHeap) Len() int { return len(h) }
func (h dayRecordStreamHeap) Less(i, j int) bool {
	if h[i].record.at.Equal(h[j].record.at) {
		return h[i].record.line < h[j].record.line
	}
	return h[i].record.at.Before(h[j].record.at)
}
func (h dayRecordStreamHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *dayRecordStreamHeap) Push(value any) {
	*h = append(*h, value.(*dayRecordStream))
}
func (h *dayRecordStreamHeap) Pop() any {
	old := *h
	value := old[len(old)-1]
	*h = old[:len(old)-1]
	return value
}

func newDayRecordStream(path, label string, filter logFilter) (*dayRecordStream, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &dayRecordStream{file: file, scanner: scanner, label: label, filter: filter}, nil
}

func (stream *dayRecordStream) next() bool {
	for stream.scanner.Scan() {
		line := stream.scanner.Text()
		visible := visibleLogText(line)
		if !matchesLogFilter(visible, stream.filter) {
			continue
		}
		stream.record = logRecord{
			line: stream.label + "  " + line,
			at:   internalLineTime(line, time.Time{}),
		}
		return true
	}
	stream.err = stream.scanner.Err()
	return false
}

func closeDayRecordStreams(streams []*dayRecordStream) {
	for _, stream := range streams {
		_ = stream.file.Close()
	}
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

type criticalWindowState struct {
	modTime    time.Time
	size       int64
	timestamps []time.Time
}

type logPayload struct {
	Lines         []string `json:"lines"`
	Start         int      `json:"start,omitempty"`
	Total         int      `json:"total,omitempty"`
	HasMoreBefore bool     `json:"hasMoreBefore,omitempty"`
	Cursor        int64    `json:"cursor,omitempty"`
	Replace       bool     `json:"replace,omitempty"`
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
	set     deviceColorSet
}

type deviceColorSet struct {
	Exact    map[string]string    `json:"exact"`
	Contains []deviceContainsRule `json:"contains"`
}

type deviceContainsRule struct {
	Match string `json:"match"`
	Color string `json:"color"`
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
	StatsTailLines         int `json:"stats_tail_lines"`
	StatsTailMaxAgeHours   int `json:"stats_tail_max_age_hours"`
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

var criticalCache = struct {
	sync.Mutex
	files map[string]criticalWindowState
}{
	files: make(map[string]criticalWindowState),
}

func main() {
	if err := ensureConfigFiles(); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(logRoot, 0o755); err != nil {
		log.Fatal(err)
	}
	if err := stateIndex.refresh(appNow()); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/api/overview", handleAPIOverview)
	mux.HandleFunc("/api/stats", handleAPIStats)
	mux.HandleFunc("/api/settings/cache-status", handleCacheRefreshStatus)
	mux.HandleFunc("/settings", handleSettings)
	mux.HandleFunc("/apple-touch-icon.png", serveAppleTouchIcon)
	mux.HandleFunc("/statistics", handleOverview)
	mux.HandleFunc("/day/", handleDay)
	mux.HandleFunc("/favicon.ico", serveFavicon)
	mux.HandleFunc("/search", handleSearch)

	log.Printf("syslog-flow listening on %s", addr)
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}
	log.Fatal(server.ListenAndServe())
}

func dashboardData(days []Day) (DashboardData, error) {
	return stateIndex.dashboard(appNow())
}

func liveLines(days []Day, limit int) ([]string, error) {
	window, err := stateIndex.liveWindow(appNow(), limit)
	if err != nil {
		return nil, err
	}
	return window.lines, nil
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
	_, snapshot, err := stateIndex.currentStats(appNow())
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
	stats, _, err := stateIndex.currentStats(appNow())
	stats.todayDay = today
	return stats, err
}

func countCriticalSince(path string, cutoff time.Time) (int, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}

	criticalCache.Lock()
	cached, ok := criticalCache.files[path]
	criticalCache.Unlock()

	if ok && cached.size == info.Size() && cached.modTime.Equal(info.ModTime()) {
		trimmed := trimCriticalTimestamps(cached.timestamps, cutoff)
		cached.timestamps = trimmed
		criticalCache.Lock()
		criticalCache.files[path] = cached
		criticalCache.Unlock()
		return len(trimmed), nil
	}

	var startOffset int64
	timestamps := make([]time.Time, 0)
	if ok && info.Size() >= cached.size && info.ModTime().After(cached.modTime) {
		startOffset = cached.size
		timestamps = trimCriticalTimestamps(cached.timestamps, cutoff)
	}

	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	if startOffset > 0 {
		if _, err := file.Seek(startOffset, 0); err != nil {
			return 0, err
		}
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		at := internalLineTime(line, time.Time{})
		if at.Before(cutoff) {
			continue
		}
		if isCriticalSeverity(line) {
			timestamps = append(timestamps, at)
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}

	timestamps = trimCriticalTimestamps(timestamps, cutoff)
	criticalCache.Lock()
	criticalCache.files[path] = criticalWindowState{
		modTime:    info.ModTime(),
		size:       info.Size(),
		timestamps: timestamps,
	}
	criticalCache.Unlock()
	return len(timestamps), nil
}

func trimCriticalTimestamps(values []time.Time, cutoff time.Time) []time.Time {
	if len(values) == 0 {
		return nil
	}
	trimmed := values[:0]
	for _, at := range values {
		if at.Before(cutoff) {
			continue
		}
		trimmed = append(trimmed, at)
	}
	return trimmed
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

	retainTail := shouldRetainFileTail(info.ModTime(), currentAppConfig())

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

	returned := summary
	if !retainTail {
		summary.tail = nil
		summary.tailLimit = 0
	}

	fileCache.Lock()
	fileCache.files[path] = summary
	fileCache.Unlock()

	return sliceSummaryTail(returned, tailLimit), nil
}

func shouldRetainFileTail(modTime time.Time, config appConfig) bool {
	maxAge := time.Duration(config.StatsTailMaxAgeHours) * time.Hour
	if maxAge <= 0 {
		return false
	}
	cutoff := appNow().Add(-maxAge)
	return modTime.After(cutoff) || modTime.Equal(cutoff)
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
	return stateIndex.daysSnapshot(appNow())
}

func listFiles(day string) ([]LogFile, error) {
	return stateIndex.filesSnapshot(appNow(), day)
}

func readDayWindow(day, file string, filter logFilter, before, limit int) ([]string, int, int, error) {
	paths := []string{file}
	if file == "" {
		files, err := listFiles(day)
		if err != nil {
			return nil, 0, 0, err
		}
		paths = make([]string, 0, len(files))
		for _, candidate := range files {
			paths = append(paths, candidate.Name)
		}
	}

	total := 0
	if filter.Query == "" && filter.Severity == "" {
		count, err := stateIndex.dayLineCount(appNow(), day, file)
		if err != nil {
			return nil, 0, 0, err
		}
		total = count
	} else {
		for _, name := range paths {
			count, err := countFileRecords(filepath.Join(logRoot, day, name), filter)
			if err != nil {
				return nil, 0, total, err
			}
			total += count
		}
	}
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

	// rsyslog writes each device file in receive-time order. Merge those ordered
	// streams so a page retains only its requested chunk instead of every line.
	streams := make([]*dayRecordStream, 0, len(paths))
	queue := make(dayRecordStreamHeap, 0, len(paths))
	for _, name := range paths {
		stream, err := newDayRecordStream(filepath.Join(logRoot, day, name), day+"/"+name, filter)
		if err != nil {
			closeDayRecordStreams(streams)
			return nil, 0, total, err
		}
		streams = append(streams, stream)
		if stream.next() {
			queue = append(queue, stream)
		} else if err := stream.err; err != nil {
			closeDayRecordStreams(streams)
			return nil, 0, total, err
		}
	}
	heap.Init(&queue)
	lines := make([]string, 0, end-start)
	for position := 0; queue.Len() > 0 && position < end; position++ {
		stream := heap.Pop(&queue).(*dayRecordStream)
		if position >= start {
			lines = append(lines, stream.record.line)
		}
		if stream.next() {
			heap.Push(&queue, stream)
		} else if err := stream.err; err != nil {
			closeDayRecordStreams(streams)
			return nil, 0, total, err
		}
	}
	closeDayRecordStreams(streams)
	return lines, start, total, nil
}

func searchAll(query string) (searchResults, error) {
	if query == "" {
		return searchResults{}, nil
	}

	days, err := listDays()
	if err != nil {
		return searchResults{}, err
	}
	return searchDays(days, query)
}

func searchRecentDays(query string, limit int) (searchResults, error) {
	if query == "" {
		return searchResults{}, nil
	}

	days, err := listDays()
	if err != nil {
		return searchResults{}, err
	}
	if limit > 0 && len(days) > limit {
		days = days[:limit]
	}
	return searchDays(days, query)
}

func searchDays(days []Day, query string) (searchResults, error) {
	records := make(logRecordHeap, 0, maxSearchResults)
	heap.Init(&records)
	limited := false
	for _, day := range days {
		files, err := listFiles(day.Name)
		if err != nil {
			return searchResults{lines: recordLines(records), limited: limited}, err
		}
		for _, file := range files {
			label := day.Name + "/" + file.Name
			path := filepath.Join(logRoot, day.Name, file.Name)
			err := visitFileRecords(path, logFilter{Query: query}, label, func(record logRecord) {
				if records.Len() < maxSearchResults {
					heap.Push(&records, record)
					return
				}
				limited = true
				if records[0].at.Before(record.at) || (records[0].at.Equal(record.at) && records[0].line < record.line) {
					records[0] = record
					heap.Fix(&records, 0)
				}
			})
			if err != nil {
				return searchResults{lines: recordLines(records), limited: limited}, err
			}
		}
	}
	sortLogRecords([]logRecord(records))
	return searchResults{lines: recordLines([]logRecord(records)), limited: limited}, nil
}

func countFileRecords(path string, filter logFilter) (int, error) {
	count := 0
	err := visitFileRecords(path, filter, "", func(logRecord) { count++ })
	return count, err
}

func visitFileRecords(path string, filter logFilter, label string, visit func(logRecord)) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		visible := visibleLogText(line)
		if matchesLogFilter(visible, filter) {
			visit(logRecord{
				line: label + "  " + line,
				at:   internalLineTime(line, time.Time{}),
			})
		}
	}
	return scanner.Err()
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
	if color := config.Exact[name]; color != "" {
		return color
	}
	for _, rule := range config.Contains {
		if rule.Match != "" && strings.Contains(name, rule.Match) {
			return rule.Color
		}
	}
	return ""
}

func logHeadingColor(line string) string {
	return deviceColor(logDevice(line))
}

func deviceColorsJSON() template.JS {
	colors, err := loadDeviceColors()
	if err != nil || (len(colors.Exact) == 0 && len(colors.Contains) == 0) {
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

func loadDeviceColors() (deviceColorSet, error) {
	info, err := os.Stat(deviceColorPath)
	if err != nil {
		if os.IsNotExist(err) {
			return deviceColorSet{}, nil
		}
		return deviceColorSet{}, err
	}

	colorCache.Lock()
	cached := colorCache.deviceColorConfig
	colorCache.Unlock()

	if (cached.set.Exact != nil || cached.set.Contains != nil) && cached.modTime.Equal(info.ModTime()) {
		return cached.set, nil
	}

	data, err := os.ReadFile(deviceColorPath)
	if err != nil {
		return deviceColorSet{}, err
	}

	set, err := parseDeviceColors(data)
	if err != nil {
		return deviceColorSet{}, err
	}

	colorCache.Lock()
	colorCache.deviceColorConfig = deviceColorConfig{
		modTime: info.ModTime(),
		set:     set,
	}
	colorCache.Unlock()

	return set, nil
}

func parseDeviceColors(data []byte) (deviceColorSet, error) {
	set := deviceColorSet{
		Exact: make(map[string]string),
	}
	if len(strings.TrimSpace(string(data))) > 0 {
		var structured deviceColorSet
		if err := json.Unmarshal(data, &structured); err == nil && (structured.Exact != nil || structured.Contains != nil) {
			for name, color := range structured.Exact {
				if !validName(name) {
					continue
				}
				if normalized, ok := normalizeHexColor(color); ok {
					set.Exact[name] = normalized
				}
			}
			for _, rule := range structured.Contains {
				match := strings.TrimSpace(rule.Match)
				if match == "" {
					continue
				}
				if normalized, ok := normalizeHexColor(rule.Color); ok {
					set.Contains = append(set.Contains, deviceContainsRule{
						Match: match,
						Color: normalized,
					})
				}
			}
		} else {
			raw := make(map[string]string)
			if err := json.Unmarshal(data, &raw); err != nil {
				return deviceColorSet{}, err
			}
			for name, color := range raw {
				if !validName(name) {
					continue
				}
				if normalized, ok := normalizeHexColor(color); ok {
					set.Exact[name] = normalized
				}
			}
		}
	}
	return set, nil
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

	colors, err := parseStatusColors(data)
	if err != nil {
		return nil, err
	}

	statusCache.Lock()
	statusCache.statusColorConfig = statusColorConfig{
		modTime: info.ModTime(),
		colors:  colors,
	}
	statusCache.Unlock()

	return colors, nil
}

func parseStatusColors(data []byte) (map[string]string, error) {
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

	colors, err := parseInterfaceColors(data)
	if err != nil {
		return defaults, err
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

func parseInterfaceColors(data []byte) (interfaceColorsFile, error) {
	defaults := defaultInterfaceColorsFile()
	raw := interfaceColorsFile{}
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &raw); err != nil {
			return defaults, err
		}
	}
	return interfaceColorsFile{
		Light: mergeInterfaceTheme(defaults.Light, raw.Light),
		Dark:  mergeInterfaceTheme(defaults.Dark, raw.Dark),
	}, nil
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
		StatsTailLines:         1024,
		StatsTailMaxAgeHours:   24,
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

	if cached.LiveRefreshSeconds > 0 && cached.StatsRefreshSeconds > 0 && cached.OverviewRefreshSeconds > 0 && cached.StatsTailLines > 0 && cached.StatsTailMaxAgeHours > 0 && cached.modTime.Equal(info.ModTime()) {
		return cached, nil
	}

	data, err := os.ReadFile(appConfigPath)
	if err != nil {
		return defaults, err
	}

	config, err := parseAppConfig(data)
	if err != nil {
		return defaults, err
	}

	config.modTime = info.ModTime()
	config.LiveRefreshSeconds = clampRefreshSeconds(config.LiveRefreshSeconds, defaults.LiveRefreshSeconds)
	config.StatsRefreshSeconds = clampRefreshSeconds(config.StatsRefreshSeconds, defaults.StatsRefreshSeconds)
	config.OverviewRefreshSeconds = clampRefreshSeconds(config.OverviewRefreshSeconds, defaults.OverviewRefreshSeconds)
	config.StatsTailLines = clampStatsTailLines(config.StatsTailLines, defaults.StatsTailLines)
	config.StatsTailMaxAgeHours = clampStatsTailMaxAgeHours(config.StatsTailMaxAgeHours, defaults.StatsTailMaxAgeHours)

	appConfigCache.Lock()
	appConfigCache.appConfig = config
	appConfigCache.Unlock()

	return config, nil
}

func parseAppConfig(data []byte) (appConfig, error) {
	defaults := defaultAppConfig()
	config := defaults
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &config); err != nil {
			return defaults, err
		}
	}
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

func clampStatsTailLines(value, fallback int) int {
	if value < 1 {
		return fallback
	}
	if value > 16384 {
		return 16384
	}
	return value
}

func clampStatsTailMaxAgeHours(value, fallback int) int {
	if value < 1 {
		return fallback
	}
	if value > 24*365 {
		return 24 * 365
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
