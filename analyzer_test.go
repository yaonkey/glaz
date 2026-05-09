package main

import (
	"testing"
	"time"
)

func TestAnalyzerAggregationAndSorting(t *testing.T) {
	a := NewAnalyzer()
	base := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)

	a.Add(ParsedEvent{
		Timestamp: base,
		Level:     "error",
		Title:     "Old error",
		Source:    "a.log",
	}, true)
	a.Add(ParsedEvent{
		Timestamp: base.Add(10 * time.Minute),
		Level:     "error",
		Title:     "Old error",
		Source:    "a.log",
	}, true)
	a.Add(ParsedEvent{
		Timestamp: base.Add(20 * time.Minute),
		Level:     "notice",
		Title:     "Fresh notice",
		Source:    "b.log",
	}, true)

	s := a.Snapshot()
	if len(s.Groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(s.Groups))
	}
	if s.Groups[0].Title != "Fresh notice" {
		t.Fatalf("expected most recent group first, got %q", s.Groups[0].Title)
	}
	if s.Groups[1].Count != 2 {
		t.Fatalf("expected aggregated count=2, got %d", s.Groups[1].Count)
	}
	if len(s.NewEvents) != 2 {
		t.Fatalf("expected 2 new events, got %d", len(s.NewEvents))
	}
}

func TestAnalyzerSkipEmptyTitle(t *testing.T) {
	a := NewAnalyzer()
	a.Add(ParsedEvent{
		Timestamp: time.Now(),
		Level:     "error",
		Title:     "",
	}, true)
	if got := len(a.Snapshot().Groups); got != 0 {
		t.Fatalf("empty title should be ignored, got %d groups", got)
	}
}
