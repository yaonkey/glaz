package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SearchResult contains full-text lookup summary and a limited preview list.
type SearchResult struct {
	Keyword   string
	Total     int
	Hits      []string
	Truncated bool
}

// SearchInFiles performs case-insensitive streaming search over files.
// It stores up to limit preview lines while keeping total matches count.
func SearchInFiles(files []string, keyword string, limit int) SearchResult {
	needle := strings.TrimSpace(strings.ToLower(keyword))
	if needle == "" {
		return SearchResult{}
	}
	if limit <= 0 {
		limit = 30
	}

	result := SearchResult{
		Keyword: needle,
		Hits:    make([]string, 0, limit),
	}

	for _, path := range files {
		file, err := os.Open(path)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(file)
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()
			if !strings.Contains(strings.ToLower(line), needle) {
				continue
			}
			result.Total++
			if len(result.Hits) < limit {
				result.Hits = append(result.Hits, filepath.Base(path)+": "+strconv.Itoa(lineNo)+": "+line)
			} else {
				result.Truncated = true
			}
		}
		_ = file.Close()
	}

	return result
}
