package main

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const newEventsLimit = 20

// GroupSnapshot represents aggregated stats for one error group.
type GroupSnapshot struct {
	Level      string
	Title      string
	Count      int
	LastSeen   time.Time
	LastSource string
}

// NewEventSnapshot keeps the first appearance of a new group in current session.
type NewEventSnapshot struct {
	Level    string
	Title    string
	LastSeen time.Time
	Source   string
	FullText string
}

// AnalyzerSnapshot is a read-only copy consumed by the TUI.
type AnalyzerSnapshot struct {
	Groups    []GroupSnapshot
	NewEvents []NewEventSnapshot
}

type groupStat struct {
	GroupSnapshot
}

// Analyzer is a thread-safe in-memory aggregator of parsed events.
type Analyzer struct {
	mu        sync.RWMutex
	groups    map[string]*groupStat
	newEvents []NewEventSnapshot
}

// NewAnalyzer creates an empty analyzer.
func NewAnalyzer() *Analyzer {
	return &Analyzer{
		groups: make(map[string]*groupStat),
	}
}

// Add puts a parsed event into aggregation state.
// If markAsNew is true and the group is seen for the first time,
// the event is also added to the NewEvents feed.
func (a *Analyzer) Add(event ParsedEvent, markAsNew bool) {
	if event.Title == "" {
		return
	}

	key := strings.ToLower(event.Level) + "|" + strings.ToLower(event.Title)

	a.mu.Lock()
	defer a.mu.Unlock()

	stat, exists := a.groups[key]
	if !exists {
		stat = &groupStat{
			GroupSnapshot: GroupSnapshot{
				Level:      strings.ToLower(event.Level),
				Title:      event.Title,
				Count:      0,
				LastSource: event.Source,
			},
		}
		a.groups[key] = stat
	}

	stat.Count++
	stat.LastSeen = event.Timestamp
	stat.LastSource = event.Source

	if markAsNew && !exists {
		a.newEvents = append([]NewEventSnapshot{
			{
				Level:    stat.Level,
				Title:    stat.Title,
				LastSeen: event.Timestamp,
				Source:   event.Source,
				FullText: event.FullText,
			},
		}, a.newEvents...)

		if len(a.newEvents) > newEventsLimit {
			a.newEvents = a.newEvents[:newEventsLimit]
		}
	}
}

// Snapshot returns a stable copy of analyzer state sorted for UI rendering.
func (a *Analyzer) Snapshot() AnalyzerSnapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()

	groups := make([]GroupSnapshot, 0, len(a.groups))
	for _, group := range a.groups {
		groups = append(groups, group.GroupSnapshot)
	}

	sort.Slice(groups, func(i, j int) bool {
		if groups[i].LastSeen.Equal(groups[j].LastSeen) {
			if groups[i].Count == groups[j].Count {
				return groups[i].Title < groups[j].Title
			}
			return groups[i].Count > groups[j].Count
		}
		return groups[i].LastSeen.After(groups[j].LastSeen)
	})

	newEvents := make([]NewEventSnapshot, len(a.newEvents))
	copy(newEvents, a.newEvents)

	return AnalyzerSnapshot{
		Groups:    groups,
		NewEvents: newEvents,
	}
}
