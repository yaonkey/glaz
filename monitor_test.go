package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDiscoverFiles(t *testing.T) {
	dir := t.TempDir()
	date := "2026-05-08"

	dated := filepath.Join(dir, "app_2026-05-08.log")
	undated := filepath.Join(dir, "error.log")
	other := filepath.Join(dir, "random.txt")

	if err := os.WriteFile(dated, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(undated, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	modAt := time.Date(2026, 5, 8, 12, 0, 0, 0, time.Local)
	if err := os.Chtimes(undated, modAt, modAt); err != nil {
		t.Fatal(err)
	}

	files, err := discoverFiles([]string{dir}, date, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
}

func TestResolveStartupDateFallback(t *testing.T) {
	dir := t.TempDir()
	today := "2026-05-09"
	prev := filepath.Join(dir, "app_2026-05-08.log")
	if err := os.WriteFile(prev, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	prevMod := time.Date(2026, 5, 8, 12, 0, 0, 0, time.Local)
	if err := os.Chtimes(prev, prevMod, prevMod); err != nil {
		t.Fatal(err)
	}

	got, err := resolveStartupDate([]string{dir}, today)
	if err != nil {
		t.Fatal(err)
	}
	if got != "2026-05-08" {
		t.Fatalf("expected fallback date 2026-05-08, got %s", got)
	}
}
