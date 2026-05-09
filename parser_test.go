package main

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeTitle(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "strip numeric error prefix",
			in:   "Ошибка 0 Call to a member function renderDecorated() on null",
			want: "Call to a member function renderDecorated() on null",
		},
		{
			name: "drop noisy token",
			in:   "29#29",
			want: "",
		},
		{
			name: "cut by colon",
			in:   "Notice: Undefined variable /var/www/file.php",
			want: "Notice",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeTitle(tc.in)
			if got != tc.want {
				t.Fatalf("normalizeTitle(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseHeaderPHP(t *testing.T) {
	line := "[2026-05-08T09:45:04+00:00][ERROR] Ошибка 0 Call to a member function renderDecorated() on null"
	event, ok := parseHeader(line)
	if !ok {
		t.Fatalf("parseHeader should parse valid php header")
	}
	if event.Level != "error" {
		t.Fatalf("unexpected level: %s", event.Level)
	}
	if event.Title != "Call to a member function renderDecorated() on null" {
		t.Fatalf("unexpected title: %s", event.Title)
	}
}

func TestEntryParserMultiline(t *testing.T) {
	p := NewEntryParser("/tmp/app.log")
	if out := p.Consume("[2026-05-08T09:45:04+00:00][ERROR] First error"); out != nil {
		t.Fatalf("first header should not flush anything")
	}
	if out := p.Consume("detail line"); out != nil {
		t.Fatalf("details should not flush anything")
	}

	flushed := p.Consume("[2026-05-08T09:46:04+00:00][ERROR] Second error")
	if flushed == nil {
		t.Fatalf("expected first event to flush on second header")
	}
	if flushed.Title != "First error" {
		t.Fatalf("unexpected flushed title: %s", flushed.Title)
	}
	if !strings.Contains(flushed.FullText, "detail line") {
		t.Fatalf("full text should contain multiline details")
	}

	// allow stale flush path to run for coverage
	time.Sleep(5 * time.Millisecond)
	if out := p.FlushStale(1 * time.Millisecond); out == nil {
		t.Fatalf("expected stale flush for pending event")
	}
}
