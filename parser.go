package main

import (
	"regexp"
	"strings"
	"time"
)

var (
	phpHeaderRe       = regexp.MustCompile(`^\[([^\]]+)\]\[([^\]]+)\]\s*(.*)$`)
	nginxHeaderRe     = regexp.MustCompile(`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}) \[([^\]]+)\]\s*(.*)$`)
	numericErrorTitle = regexp.MustCompile(`^(Ошибка|Error)\s+\d+\s+`)
	bracketsPrefixRe  = regexp.MustCompile(`^#?\d+\s*`)
	hasLetterRe       = regexp.MustCompile(`[A-Za-zА-Яа-я]`)
	pathStartRe       = regexp.MustCompile(`\s+(\/|https?:\/\/)`)
	multiSpacesRe     = regexp.MustCompile(`\s+`)
)

// ParsedEvent is a normalized log entry used by analyzer and TUI layers.
type ParsedEvent struct {
	// Timestamp is parsed from the log header.
	Timestamp time.Time
	// Level is a lower-cased severity (error, notice, warn, ...).
	Level string
	// Title is a compact grouping key derived from the raw message.
	Title string
	// FullText keeps the full multi-line payload for "new error" previews.
	FullText string
	// Source is the path to the file that produced this event.
	Source string
}

// EntryParser incrementally converts raw lines into ParsedEvent values.
type EntryParser struct {
	source      string
	pending     *ParsedEvent
	lastLineAt  time.Time
	fullBuilder strings.Builder
}

// NewEntryParser creates a parser bound to a specific source file.
func NewEntryParser(source string) *EntryParser {
	return &EntryParser{source: source}
}

// Consume appends one line and may return a finalized event.
func (p *EntryParser) Consume(line string) *ParsedEvent {
	now := time.Now()
	if parsed, ok := parseHeader(line); ok {
		ready := p.flush()
		p.pending = &ParsedEvent{
			Timestamp: parsed.Timestamp,
			Level:     parsed.Level,
			Title:     parsed.Title,
			Source:    p.source,
		}
		p.fullBuilder.Reset()
		p.fullBuilder.WriteString(line)
		p.lastLineAt = now
		return ready
	}

	if p.pending != nil {
		p.fullBuilder.WriteByte('\n')
		p.fullBuilder.WriteString(line)
		p.lastLineAt = now
	}

	return nil
}

// FlushStale finalizes pending data after maxIdle silence in tail mode.
func (p *EntryParser) FlushStale(maxIdle time.Duration) *ParsedEvent {
	if p.pending == nil {
		return nil
	}
	if time.Since(p.lastLineAt) < maxIdle {
		return nil
	}
	return p.flush()
}

// ForceFlush immediately returns the current pending event, if any.
func (p *EntryParser) ForceFlush() *ParsedEvent {
	return p.flush()
}

// flush moves pending parser state into a standalone event.
func (p *EntryParser) flush() *ParsedEvent {
	if p.pending == nil {
		return nil
	}
	ready := *p.pending
	ready.FullText = p.fullBuilder.String()
	p.pending = nil
	p.fullBuilder.Reset()
	return &ready
}

// parseHeader extracts timestamp, level and normalized title from a header line.
func parseHeader(line string) (ParsedEvent, bool) {
	if m := phpHeaderRe.FindStringSubmatch(line); m != nil {
		ts, ok := parseTime(m[1], []string{
			time.RFC3339,
			"2006-01-02 15:04:05",
		})
		if !ok {
			return ParsedEvent{}, false
		}

		level := strings.ToLower(strings.TrimSpace(m[2]))
		title := normalizeTitle(m[3])
		return ParsedEvent{Timestamp: ts, Level: level, Title: title}, title != ""
	}

	if m := nginxHeaderRe.FindStringSubmatch(line); m != nil {
		ts, _ := parseTime(m[1], []string{"2006/01/02 15:04:05"})
		level := strings.ToLower(strings.TrimSpace(m[2]))
		title := normalizeTitle(m[3])
		return ParsedEvent{Timestamp: ts, Level: level, Title: title}, title != ""
	}

	return ParsedEvent{}, false
}

// parseTime tries candidate layouts and returns the first successful parse.
func parseTime(value string, layouts []string) (time.Time, bool) {
	for _, layout := range layouts {
		if ts, err := time.Parse(layout, value); err == nil {
			return ts, true
		}
	}
	return time.Time{}, false
}

// normalizeTitle removes volatile details so similar errors collapse into one group.
func normalizeTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}

	if idx := pathStartRe.FindStringIndex(title); idx != nil {
		title = strings.TrimSpace(title[:idx[0]])
	}

	if idx := strings.Index(title, ":"); idx > 0 {
		title = strings.TrimSpace(title[:idx])
	}

	title = numericErrorTitle.ReplaceAllString(title, "")
	title = bracketsPrefixRe.ReplaceAllString(title, "")
	title = multiSpacesRe.ReplaceAllString(title, " ")
	title = strings.Trim(title, " .,-:;#")

	if !hasLetterRe.MatchString(title) {
		return ""
	}

	return strings.TrimSpace(title)
}
