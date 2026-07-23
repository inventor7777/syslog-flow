package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const dayCacheVersion = 1

type dayCache struct {
	Version     int            `json:"version"`
	Day         string         `json:"day"`
	GeneratedAt time.Time      `json:"generated_at"`
	Files       []dayCacheFile `json:"files"`
}

type dayCacheFile struct {
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"mod_time"`
	Lines    int       `json:"lines"`
	LastSeen time.Time `json:"last_seen"`
}

type cacheMonth struct {
	Month  string
	Label  string
	Days   int
	Cached int
}

type cacheRefreshJob struct {
	Running   bool   `json:"running"`
	Month     string `json:"month"`
	Completed int    `json:"completed"`
	Total     int    `json:"total"`
	Error     string `json:"error,omitempty"`
}

var cacheJob struct {
	sync.Mutex
	cacheRefreshJob
}

func dayCachePath(day string) string {
	return filepath.Join(logRoot, filepath.FromSlash(day), strings.ReplaceAll(day, "/", "-")+".json")
}

func readDayCache(day string) (dayCache, error) {
	data, err := os.ReadFile(dayCachePath(day))
	if err != nil {
		return dayCache{}, err
	}
	var cache dayCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return dayCache{}, err
	}
	if cache.Version != dayCacheVersion || cache.Day != day || cache.GeneratedAt.IsZero() {
		return dayCache{}, fmt.Errorf("invalid cache for %s", day)
	}
	seen := make(map[string]struct{}, len(cache.Files))
	for _, file := range cache.Files {
		if !validName(file.Name) || file.Size < 0 || file.Lines < 0 || file.ModTime.IsZero() || file.LastSeen.IsZero() {
			return dayCache{}, fmt.Errorf("invalid cache file for %s", day)
		}
		if _, ok := seen[file.Name]; ok {
			return dayCache{}, fmt.Errorf("duplicate cache file for %s", day)
		}
		seen[file.Name] = struct{}{}
	}
	return cache, nil
}

func buildDayCache(day string, now time.Time) (dayCache, error) {
	files, err := discoverDayLogFiles(day)
	if err != nil {
		return dayCache{}, err
	}
	cache := dayCache{Version: dayCacheVersion, Day: day, GeneratedAt: now, Files: make([]dayCacheFile, 0, len(files))}
	for _, file := range files {
		summary, err := scanDayCacheFile(file)
		if err != nil {
			return dayCache{}, err
		}
		cache.Files = append(cache.Files, summary)
	}
	sort.Slice(cache.Files, func(i, j int) bool { return cache.Files[i].Name < cache.Files[j].Name })
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return dayCache{}, err
	}
	data = append(data, '\n')
	if err := writeCacheFile(dayCachePath(day), data); err != nil {
		return dayCache{}, err
	}
	return cache, nil
}

func scanDayCacheFile(file discoveredFile) (dayCacheFile, error) {
	handle, err := os.Open(file.path)
	if err != nil {
		return dayCacheFile{}, err
	}
	defer handle.Close()

	summary := dayCacheFile{Name: file.name, Size: file.size, ModTime: file.modTime}
	scanner := bufio.NewScanner(handle)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		summary.Lines++
		if at := internalLineTime(line, file.modTime); summary.LastSeen.IsZero() || at.After(summary.LastSeen) {
			summary.LastSeen = at
		}
	}
	if summary.LastSeen.IsZero() {
		summary.LastSeen = file.modTime
	}
	return summary, scanner.Err()
}

func writeCacheFile(path string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".syslog-flow-cache-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
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

func (idx *logIndex) applyDayCache(cache dayCache) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	for path, file := range idx.files {
		if file.day == cache.Day {
			delete(idx.files, path)
		}
	}
	for _, file := range cache.Files {
		path := filepath.Join(logRoot, filepath.FromSlash(cache.Day), file.Name)
		idx.files[path] = &indexedFile{path: path, day: cache.Day, name: file.Name, size: file.Size, modTime: file.ModTime, lineCount: file.Lines, lastSeen: file.LastSeen}
	}
}

func cacheMonths(now time.Time) ([]cacheMonth, error) {
	days, err := discoverLogDays()
	if err != nil {
		return nil, err
	}
	today := now.Format("2006/01/02")
	byMonth := make(map[string]*cacheMonth)
	for _, day := range days {
		if day >= today {
			continue
		}
		month := day[:7]
		entry := byMonth[month]
		if entry == nil {
			entry = &cacheMonth{Month: month, Label: monthLabel(month)}
			byMonth[month] = entry
		}
		entry.Days++
		if _, err := readDayCache(day); err == nil {
			entry.Cached++
		}
	}
	months := make([]cacheMonth, 0, len(byMonth))
	for _, month := range byMonth {
		months = append(months, *month)
	}
	sort.Slice(months, func(i, j int) bool { return months[i].Month > months[j].Month })
	return months, nil
}

func monthLabel(month string) string {
	parsed, err := time.Parse("2006/01", month)
	if err != nil {
		return month
	}
	return parsed.Format("January 2006")
}

func startMonthCacheRefresh(month string) error {
	if _, err := time.Parse("2006/01", month); err != nil {
		return fmt.Errorf("invalid cache month")
	}
	days, err := discoverLogDays()
	if err != nil {
		return err
	}
	today := appNow().Format("2006/01/02")
	selected := make([]string, 0)
	for _, day := range days {
		if strings.HasPrefix(day, month+"/") && day < today {
			selected = append(selected, day)
		}
	}
	if len(selected) == 0 {
		return fmt.Errorf("no completed log days in %s", month)
	}

	cacheJob.Lock()
	if cacheJob.Running {
		cacheJob.Unlock()
		return fmt.Errorf("a cache refresh is already running")
	}
	cacheJob.cacheRefreshJob = cacheRefreshJob{Running: true, Month: month, Total: len(selected)}
	cacheJob.Unlock()

	go func() {
		for _, day := range selected {
			cache, err := buildDayCache(day, appNow())
			if err != nil {
				cacheJob.Lock()
				cacheJob.Error = err.Error()
				cacheJob.Running = false
				cacheJob.Unlock()
				return
			}
			stateIndex.applyDayCache(cache)
			cacheJob.Lock()
			cacheJob.Completed++
			cacheJob.Unlock()
		}
		cacheJob.Lock()
		cacheJob.Running = false
		cacheJob.Unlock()
	}()
	return nil
}

func currentCacheRefreshJob() cacheRefreshJob {
	cacheJob.Lock()
	defer cacheJob.Unlock()
	return cacheJob.cacheRefreshJob
}
