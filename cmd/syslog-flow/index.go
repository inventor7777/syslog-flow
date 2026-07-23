package main

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	indexRefreshMinInterval = time.Second
	archiveRefreshInterval  = 15 * time.Minute
)

type indexedLine struct {
	seq  int64
	text string
	at   time.Time
}

type indexedFile struct {
	path          string
	day           string
	name          string
	size          int64
	modTime       time.Time
	lineCount     int
	lastSeen      time.Time
	tailRetained  bool
	tailLimit     int
	tail          []indexedLine
	criticalTimes []time.Time
}

type logIndex struct {
	mu              sync.Mutex
	refreshMu       sync.Mutex
	lastRefresh     time.Time
	lastFullRefresh time.Time
	currentDay      string
	nextSeq         int64
	files           map[string]*indexedFile
}

type discoveredFile struct {
	path    string
	day     string
	name    string
	size    int64
	modTime time.Time
}

type indexDayWindow struct {
	lines  []string
	cursor int64
}

var stateIndex = &logIndex{
	files: make(map[string]*indexedFile),
}

func (idx *logIndex) refresh(now time.Time) error {
	idx.refreshMu.Lock()
	defer idx.refreshMu.Unlock()

	today := now.Format("2006/01/02")

	idx.mu.Lock()
	rebuildAll := idx.currentDay != "" && idx.currentDay != today
	if !rebuildAll && !idx.lastRefresh.IsZero() && now.Sub(idx.lastRefresh) < indexRefreshMinInterval {
		idx.mu.Unlock()
		return nil
	}
	fullRefresh := rebuildAll || idx.lastFullRefresh.IsZero() || now.Sub(idx.lastFullRefresh) >= archiveRefreshInterval
	if rebuildAll {
		idx.files = make(map[string]*indexedFile)
		idx.nextSeq = 0
	}
	idx.mu.Unlock()

	config := currentAppConfig()
	current := make(map[string]struct{})
	if fullRefresh {
		days, err := discoverLogDays()
		if err != nil {
			return err
		}
		for _, day := range days {
			if day != today {
				cache, err := readDayCache(day)
				if err != nil {
					cache, err = buildDayCache(day, now)
					if err != nil {
						return err
					}
				}
				idx.applyDayCache(cache)
				for _, file := range cache.Files {
					current[filepath.Join(logRoot, filepath.FromSlash(day), file.Name)] = struct{}{}
				}
				continue
			}
			files, err := discoverDayLogFiles(day)
			if err != nil {
				return err
			}
			for _, file := range files {
				if err := idx.refreshFile(file, config, now); err != nil {
					return err
				}
				current[file.path] = struct{}{}
			}
		}
	} else {
		files, err := discoverDayLogFiles(today)
		if err != nil {
			return err
		}
		for _, file := range files {
			if err := idx.refreshFile(file, config, now); err != nil {
				return err
			}
			current[file.path] = struct{}{}
		}
	}

	idx.mu.Lock()
	for path, indexed := range idx.files {
		if !fullRefresh && indexed.day != today {
			continue
		}
		if _, ok := current[path]; !ok {
			delete(idx.files, path)
		}
	}
	idx.lastRefresh = now
	if fullRefresh {
		idx.lastFullRefresh = now
	}
	idx.currentDay = today
	idx.mu.Unlock()
	return nil
}

func (idx *logIndex) refreshFile(file discoveredFile, config appConfig, now time.Time) error {
	var existing indexedFile
	var ok bool
	idx.mu.Lock()
	if current, exists := idx.files[file.path]; exists {
		existing = *current
		existing.tail = append([]indexedLine(nil), current.tail...)
		existing.criticalTimes = append([]time.Time(nil), current.criticalTimes...)
		ok = true
	}
	idx.mu.Unlock()

	if ok && existing.size == file.size && existing.modTime.Equal(file.modTime) {
		return nil
	}

	startOffset := int64(0)
	base := &indexedFile{
		path:    file.path,
		day:     file.day,
		name:    file.name,
		size:    file.size,
		modTime: file.modTime,
	}

	if ok && file.size >= existing.size && file.modTime.After(existing.modTime) {
		startOffset = existing.size
		base.lineCount = existing.lineCount
		base.lastSeen = existing.lastSeen
		base.tailRetained = existing.tailRetained
		base.tailLimit = existing.tailLimit
		base.tail = append([]indexedLine(nil), existing.tail...)
		base.criticalTimes = trimCriticalTimestamps(existing.criticalTimes, now.Add(-5*time.Minute))
	}

	retainTail := shouldRetainFileTail(file.modTime, config)
	tailLimit := max(config.StatsTailLines, dayChunkSize*4)
	if retainTail && tailLimit < dayChunkSize {
		tailLimit = dayChunkSize
	}

	handle, err := os.Open(file.path)
	if err != nil {
		return err
	}
	defer handle.Close()

	if startOffset > 0 {
		if _, err := handle.Seek(startOffset, 0); err != nil {
			return err
		}
	}

	scanner := bufio.NewScanner(handle)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	cutoff := now.Add(-5 * time.Minute)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		at := internalLineTime(line, file.modTime)
		base.lineCount++
		if base.lastSeen.IsZero() || at.After(base.lastSeen) {
			base.lastSeen = at
		}

		if retainTail {
			idx.mu.Lock()
			idx.nextSeq++
			seq := idx.nextSeq
			idx.mu.Unlock()

			base.tail = append(base.tail, indexedLine{
				seq:  seq,
				text: file.day + "/" + file.name + "  " + line,
				at:   at,
			})
			if len(base.tail) > tailLimit {
				copy(base.tail, base.tail[len(base.tail)-tailLimit:])
				base.tail = base.tail[:tailLimit]
			}
		}

		if !at.Before(cutoff) && isCriticalSeverity(line) {
			base.criticalTimes = append(base.criticalTimes, at)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	base.criticalTimes = trimCriticalTimestamps(base.criticalTimes, cutoff)
	base.tailRetained = retainTail
	if retainTail {
		base.tailLimit = tailLimit
	} else {
		base.tail = nil
		base.tailLimit = 0
	}
	if base.lastSeen.IsZero() {
		base.lastSeen = file.modTime
	}

	idx.mu.Lock()
	idx.files[file.path] = base
	idx.mu.Unlock()
	return nil
}

func (idx *logIndex) daysSnapshot(now time.Time) ([]Day, error) {
	if err := idx.refresh(now); err != nil {
		return nil, err
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	byDay := make(map[string][]LogFile)
	for _, file := range idx.files {
		byDay[file.day] = append(byDay[file.day], LogFile{Name: file.name, Size: file.size})
	}

	days := make([]Day, 0, len(byDay))
	for day, files := range byDay {
		sort.Slice(files, func(i, j int) bool {
			return files[i].Name < files[j].Name
		})
		days = append(days, Day{Name: day, Files: files})
	}
	sort.Slice(days, func(i, j int) bool {
		return days[i].Name > days[j].Name
	})
	return days, nil
}

func (idx *logIndex) filesSnapshot(now time.Time, day string) ([]LogFile, error) {
	if err := idx.refresh(now); err != nil {
		return nil, err
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	var files []LogFile
	for _, file := range idx.files {
		if file.day != day {
			continue
		}
		files = append(files, LogFile{Name: file.name, Size: file.size})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name < files[j].Name
	})
	return files, nil
}

func (idx *logIndex) dayLineCount(now time.Time, day, name string) (int, error) {
	if err := idx.refresh(now); err != nil {
		return 0, err
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	count := 0
	for _, file := range idx.files {
		if file.day != day || (name != "" && file.name != name) {
			continue
		}
		count += file.lineCount
	}
	return count, nil
}

func (idx *logIndex) currentStats(now time.Time) (lineStats, statsSnapshot, error) {
	if err := idx.refresh(now); err != nil {
		return lineStats{}, statsSnapshot{}, err
	}

	today := now.Format("2006/01/02")
	cutoff := now.Add(-5 * time.Minute)

	idx.mu.Lock()
	defer idx.mu.Unlock()

	stats := lineStats{todayDay: today}
	snapshot := statsSnapshot{}
	days := make(map[string]struct{})
	devices := make(map[string]struct{})

	for _, file := range idx.files {
		stats.allLines += file.lineCount
		snapshot.totalLogSize += file.size
		days[file.day] = struct{}{}
		devices[strings.TrimSuffix(file.name, ".log")] = struct{}{}
		if file.day == today {
			stats.todayLines += file.lineCount
		}

		trimmed := trimCriticalTimestamps(file.criticalTimes, cutoff)
		file.criticalTimes = trimmed
		stats.critical5m += len(trimmed)
	}

	stats.linesPerSecond = updateLineRate(now, stats.allLines)
	snapshot.critical5m = stats.critical5m
	snapshot.todayLines = stats.todayLines
	snapshot.allLines = stats.allLines
	snapshot.linesPerSecond = stats.linesPerSecond
	snapshot.dayCount = len(days)
	snapshot.deviceCount = len(devices)
	return stats, snapshot, nil
}

func (idx *logIndex) dashboard(now time.Time) (DashboardData, error) {
	if err := idx.refresh(now); err != nil {
		return DashboardData{}, err
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	if len(idx.files) == 0 {
		return DashboardData{}, nil
	}

	latestDay := ""
	devices := make(map[string]deviceRecord)
	daySet := make(map[string]struct{})
	for _, file := range idx.files {
		daySet[file.day] = struct{}{}
		if latestDay == "" || file.day > latestDay {
			latestDay = file.day
		}
		name := strings.TrimSuffix(file.name, ".log")
		record := deviceRecord{
			name:  name,
			day:   file.day,
			lines: file.lineCount,
			mod:   file.lastSeen,
		}
		if existing, ok := devices[name]; !ok || record.mod.After(existing.mod) {
			devices[name] = record
		}
	}

	data := DashboardData{
		LatestDay:   latestDay,
		DayCount:    formatInt(len(daySet)),
		DeviceCount: formatInt(len(devices)),
	}

	records := make([]deviceRecord, 0, len(devices))
	for _, record := range devices {
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].mod.After(records[j].mod)
	})

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

func (idx *logIndex) liveWindow(now time.Time, limit int) (indexDayWindow, error) {
	if err := idx.refresh(now); err != nil {
		return indexDayWindow{}, err
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	var records []indexedLine
	var cursor int64
	for _, file := range idx.files {
		if len(file.tail) == 0 {
			continue
		}
		records = append(records, file.tail...)
		if last := file.tail[len(file.tail)-1].seq; last > cursor {
			cursor = last
		}
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].at.Before(records[j].at)
	})
	if limit > 0 && len(records) > limit {
		records = records[len(records)-limit:]
	}
	lines := make([]string, 0, len(records))
	for _, record := range records {
		lines = append(lines, record.text)
	}
	return indexDayWindow{lines: lines, cursor: cursor}, nil
}

func (idx *logIndex) dayAppends(now time.Time, day, file string, filter logFilter, since int64) ([]string, int64, bool, error) {
	if err := idx.refresh(now); err != nil {
		return nil, 0, false, err
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	var records []indexedLine
	var maxSeq int64
	var minSeq int64
	haveSeq := false

	for _, candidate := range idx.files {
		if candidate.day != day {
			continue
		}
		if file != "" && candidate.name != file {
			continue
		}
		if len(candidate.tail) == 0 {
			continue
		}

		firstSeq := candidate.tail[0].seq
		lastSeq := candidate.tail[len(candidate.tail)-1].seq
		if !haveSeq || firstSeq < minSeq {
			minSeq = firstSeq
		}
		if !haveSeq || lastSeq > maxSeq {
			maxSeq = lastSeq
		}
		haveSeq = true

		for _, line := range candidate.tail {
			if line.seq <= since {
				continue
			}
			if matchesLogFilter(visibleLogText(line.text), filter) {
				records = append(records, line)
			}
		}
	}

	if !haveSeq {
		return nil, since, false, nil
	}
	if since > 0 && maxSeq > since && since < minSeq {
		return nil, maxSeq, true, nil
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].at.Before(records[j].at)
	})
	lines := make([]string, 0, len(records))
	for _, record := range records {
		lines = append(lines, record.text)
	}
	return lines, maxSeq, false, nil
}

func (idx *logIndex) dayCursor(now time.Time, day, file string) (int64, error) {
	if err := idx.refresh(now); err != nil {
		return 0, err
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	var cursor int64
	for _, candidate := range idx.files {
		if candidate.day != day {
			continue
		}
		if file != "" && candidate.name != file {
			continue
		}
		if len(candidate.tail) == 0 {
			continue
		}
		if last := candidate.tail[len(candidate.tail)-1].seq; last > cursor {
			cursor = last
		}
	}
	return cursor, nil
}

func discoverDayLogFiles(day string) ([]discoveredFile, error) {
	path := filepath.Join(logRoot, filepath.FromSlash(day))
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return discoverLogFilesFrom(path)
}

func discoverLogDays() ([]string, error) {
	var days []string
	err := filepath.WalkDir(logRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(logRoot, path)
		if err != nil || rel == "." {
			return nil
		}
		day := filepath.ToSlash(rel)
		if !validDayPath(day) {
			return nil
		}
		days = append(days, day)
		return filepath.SkipDir
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(days)
	return days, nil
}

func discoverLogFilesFrom(root string) ([]discoveredFile, error) {
	var files []discoveredFile
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") || !validName(entry.Name()) {
			return nil
		}
		rel, err := filepath.Rel(logRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		day, name, ok := strings.Cut(rel, "/")
		if ok {
			parts := strings.Split(rel, "/")
			if len(parts) >= 3 {
				day = strings.Join(parts[:3], "/")
				name = parts[len(parts)-1]
			} else {
				ok = false
			}
		}
		if !ok || !validDayPath(day) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		files = append(files, discoveredFile{
			path:    path,
			day:     day,
			name:    name,
			size:    info.Size(),
			modTime: info.ModTime(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].path < files[j].path
	})
	return files, nil
}
