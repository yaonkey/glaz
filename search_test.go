package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSearchInFiles(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.log")
	b := filepath.Join(dir, "b.log")

	if err := os.WriteFile(a, []byte("line 1\n[ERROR] crash happened\nline 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("[notice] all good\nerror in lowercase\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := SearchInFiles([]string{a, b}, "error", 1)
	if result.Total != 2 {
		t.Fatalf("expected 2 matches, got %d", result.Total)
	}
	if len(result.Hits) != 1 {
		t.Fatalf("expected 1 preview hit due to limit, got %d", len(result.Hits))
	}
	if !result.Truncated {
		t.Fatalf("expected truncated=true when total exceeds limit")
	}
}
