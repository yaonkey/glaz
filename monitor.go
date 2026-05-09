package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/hpcloud/tail"
)

// Coordinator owns the currently active monitoring session and allows switching dates.
type Coordinator struct {
	mu      sync.RWMutex
	ctx     context.Context
	cfg     Config
	session *MonitorSession
}

// NewCoordinator creates a coordinator and initializes the first session.
// If startup date is "today" and no files exist, it may fallback to previous day.
func NewCoordinator(ctx context.Context, cfg Config, date string) (*Coordinator, error) {
	c := &Coordinator{
		ctx: ctx,
		cfg: cfg,
	}
	if date == time.Now().Format("2006-01-02") {
		resolved, fallbackErr := resolveStartupDate(cfg.LogDirs, date)
		if fallbackErr == nil {
			date = resolved
		}
	}
	if _, _, err := c.switchDateLocked(date); err != nil {
		return nil, err
	}
	return c, nil
}

// Close stops the active session and all background workers.
func (c *Coordinator) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session != nil {
		c.session.Close()
		c.session = nil
	}
}

// SwitchDate rebuilds session for date and returns the actual opened day.
// fallbackDate is non-empty when the previous day was chosen automatically.
func (c *Coordinator) SwitchDate(date string) (actualDate string, fallbackDate string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.switchDateLocked(date)
}

// switchDateLocked is a mutex-protected implementation of date switching.
func (c *Coordinator) switchDateLocked(date string) (actualDate string, fallbackDate string, err error) {
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return "", "", err
	}

	chosen := date
	fallback := ""
	files, err := discoverFiles(c.cfg.LogDirs, chosen, chosen == time.Now().Format("2006-01-02"))
	if err != nil {
		return "", "", err
	}
	if len(files) == 0 {
		parsed, parseErr := time.Parse("2006-01-02", date)
		if parseErr == nil {
			prev := parsed.AddDate(0, 0, -1).Format("2006-01-02")
			prevFiles, prevErr := discoverFiles(c.cfg.LogDirs, prev, false)
			if prevErr == nil && len(prevFiles) > 0 {
				chosen = prev
				fallback = prev
			}
		}
	}

	next, err := NewMonitorSession(c.ctx, c.cfg.LogDirs, chosen)
	if err != nil {
		return "", "", err
	}

	if c.session != nil {
		c.session.Close()
	}
	c.session = next
	return chosen, fallback, nil
}

// Snapshot returns a UI-ready analyzer snapshot from the current session.
func (c *Coordinator) Snapshot() AnalyzerSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.session == nil {
		return AnalyzerSnapshot{}
	}
	return c.session.analyzer.Snapshot()
}

// Meta returns current session metadata for headers and diagnostics.
func (c *Coordinator) Meta() SessionMeta {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.session == nil {
		return SessionMeta{}
	}
	return c.session.Meta()
}

// Search executes keyword lookup in files tracked by the current session.
func (c *Coordinator) Search(keyword string, limit int) SearchResult {
	c.mu.RLock()
	session := c.session
	c.mu.RUnlock()
	if session == nil {
		return SearchResult{}
	}
	return session.Search(keyword, limit)
}

// SessionMeta contains non-aggregated information about active monitoring state.
type SessionMeta struct {
	Date      string
	Live      bool
	Files     []string
	Dirs      []string
	LastError string
}

// MonitorSession represents monitoring state for one selected date.
type MonitorSession struct {
	ctx      context.Context
	cancel   context.CancelFunc
	analyzer *Analyzer
	date     string
	dateTag  string
	live     bool
	dirs     []string

	mu      sync.RWMutex
	files   map[string]struct{}
	closers map[string]context.CancelFunc
	wg      sync.WaitGroup

	lastError string
}

// NewMonitorSession scans initial files and starts live watchers when applicable.
func NewMonitorSession(parent context.Context, dirs []string, date string) (*MonitorSession, error) {
	ctx, cancel := context.WithCancel(parent)
	session := &MonitorSession{
		ctx:      ctx,
		cancel:   cancel,
		analyzer: NewAnalyzer(),
		date:     date,
		dateTag:  date,
		dirs:     append([]string(nil), dirs...),
		files:    make(map[string]struct{}),
		closers:  make(map[string]context.CancelFunc),
	}

	today := time.Now().Format("2006-01-02")
	session.live = (date == today)

	initial, err := discoverFiles(dirs, date, session.live)
	if err != nil {
		cancel()
		return nil, err
	}

	for _, file := range initial {
		session.files[file] = struct{}{}
	}

	for _, file := range initial {
		if scanErr := scanFileOnce(file, session.addEvent, false); scanErr != nil {
			session.setLastError(scanErr.Error())
		}
	}

	if session.live {
		for _, file := range initial {
			session.startTail(file)
		}
		session.wg.Add(1)
		go session.discoveryLoop()
	}

	return session, nil
}

// Close stops all tail/discovery goroutines and waits until they exit.
func (s *MonitorSession) Close() {
	s.cancel()

	s.mu.Lock()
	for _, stop := range s.closers {
		stop()
	}
	s.mu.Unlock()

	s.wg.Wait()
}

// Meta builds a consistent metadata snapshot for UI.
func (s *MonitorSession) Meta() SessionMeta {
	s.mu.RLock()
	files := make([]string, 0, len(s.files))
	for file := range s.files {
		files = append(files, file)
	}
	s.mu.RUnlock()
	slices.Sort(files)

	return SessionMeta{
		Date:      s.date,
		Live:      s.live,
		Files:     files,
		Dirs:      append([]string(nil), s.dirs...),
		LastError: s.lastError,
	}
}

// Search runs full-text search in all files currently attached to the session.
func (s *MonitorSession) Search(keyword string, limit int) SearchResult {
	meta := s.Meta()
	return SearchInFiles(meta.Files, keyword, limit)
}

// discoveryLoop periodically discovers newly created files for the active date.
func (s *MonitorSession) discoveryLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(8 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			files, err := discoverFiles(s.dirs, s.date, true)
			if err != nil {
				s.setLastError(err.Error())
				continue
			}
			for _, file := range files {
				s.mu.RLock()
				_, exists := s.files[file]
				s.mu.RUnlock()
				if exists {
					continue
				}
				s.registerFile(file)
			}
		}
	}
}

// registerFile performs initial scan and starts live tail for a newly discovered file.
func (s *MonitorSession) registerFile(file string) {
	if err := scanFileOnce(file, s.addEvent, false); err != nil {
		s.setLastError(err.Error())
	}

	s.mu.Lock()
	if _, exists := s.files[file]; exists {
		s.mu.Unlock()
		return
	}
	s.files[file] = struct{}{}
	s.mu.Unlock()

	s.startTail(file)
}

// startTail begins asynchronous follow mode for one file.
func (s *MonitorSession) startTail(path string) {
	readerCtx, readerCancel := context.WithCancel(s.ctx)

	s.mu.Lock()
	s.closers[path] = readerCancel
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			delete(s.closers, path)
			s.mu.Unlock()
		}()

		t, err := tail.TailFile(path, tail.Config{
			Follow:    true,
			ReOpen:    true,
			MustExist: false,
			Poll:      true,
			Location: &tail.SeekInfo{
				Offset: 0,
				Whence: 2,
			},
		})
		if err != nil {
			s.setLastError(err.Error())
			return
		}
		defer t.Cleanup()

		parser := NewEntryParser(path)
		flushTicker := time.NewTicker(1200 * time.Millisecond)
		defer flushTicker.Stop()

		for {
			select {
			case <-readerCtx.Done():
				_ = t.Stop()
				if ready := parser.ForceFlush(); ready != nil {
					s.addEvent(*ready, true)
				}
				return
			case line, ok := <-t.Lines:
				if !ok {
					if ready := parser.ForceFlush(); ready != nil {
						s.addEvent(*ready, true)
					}
					return
				}
				if line == nil {
					continue
				}
				if ready := parser.Consume(line.Text); ready != nil {
					s.addEvent(*ready, true)
				}
			case <-flushTicker.C:
				if ready := parser.FlushStale(1200 * time.Millisecond); ready != nil {
					s.addEvent(*ready, true)
				}
			}
		}
	}()
}

// addEvent applies date guard and forwards event into analyzer.
func (s *MonitorSession) addEvent(event ParsedEvent, markAsNew bool) {
	if event.Timestamp.IsZero() {
		return
	}
	if event.Timestamp.Format("2006-01-02") != s.dateTag {
		return
	}
	s.analyzer.Add(event, markAsNew)
}

// setLastError stores latest operational error for visibility in TUI.
func (s *MonitorSession) setLastError(err string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastError = err
}

// discoverFiles finds candidate log files for a specific date in configured directories.
func discoverFiles(dirs []string, date string, includeActiveUndated bool) ([]string, error) {
	seen := map[string]struct{}{}
	files := make([]string, 0)

	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		if !info.IsDir() {
			continue
		}

		err = filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if entry.IsDir() {
				return nil
			}

			if !looksLikeLogFile(path) {
				return nil
			}

			name := filepath.Base(path)
			matchesDate := strings.Contains(name, date)
			if matchesDate {
				if _, ok := seen[path]; !ok {
					seen[path] = struct{}{}
					files = append(files, path)
				}
				return nil
			}

			if includeActiveUndated {
				info, statErr := entry.Info()
				if statErr != nil {
					return nil
				}
				if info.ModTime().Format("2006-01-02") == date {
					if _, ok := seen[path]; !ok {
						seen[path] = struct{}{}
						files = append(files, path)
					}
				}
			}

			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	slices.Sort(files)
	return files, nil
}

// resolveStartupDate prefers today's files, then falls back to the previous day.
func resolveStartupDate(dirs []string, today string) (string, error) {
	filesToday, err := discoverFiles(dirs, today, true)
	if err != nil {
		return today, err
	}
	if len(filesToday) > 0 {
		return today, nil
	}

	parsedToday, err := time.Parse("2006-01-02", today)
	if err != nil {
		return today, err
	}
	prev := parsedToday.AddDate(0, 0, -1).Format("2006-01-02")
	filesPrev, err := discoverFiles(dirs, prev, false)
	if err != nil {
		return today, err
	}
	if len(filesPrev) == 0 {
		return today, fmt.Errorf("логи не найдены ни за %s, ни за %s", today, prev)
	}
	return prev, nil
}

// looksLikeLogFile is a lightweight filename heuristic for log discovery.
func looksLikeLogFile(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	return strings.HasSuffix(name, ".log") ||
		strings.Contains(name, ".log.") ||
		strings.Contains(name, "error")
}

// scanFileOnce streams an entire file and emits parsed events to consume callback.
func scanFileOnce(path string, consume func(ParsedEvent, bool), markAsNew bool) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	parser := NewEntryParser(path)
	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, 4*1024*1024)

	for scanner.Scan() {
		if ready := parser.Consume(scanner.Text()); ready != nil {
			consume(*ready, markAsNew)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if ready := parser.ForceFlush(); ready != nil {
		consume(*ready, markAsNew)
	}
	return nil
}
