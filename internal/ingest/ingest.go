// Package ingest reads trace files (OTLP/JSON GenAI spans, Langfuse
// observations_v2 JSONL) and normalizes them into trace.Calls.
// Fully offline: it only ever reads local files.
package ingest

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Mindburn-Labs/distillscan/internal/trace"
)

// Stats summarizes what a scan ingested.
type Stats struct {
	Files        int
	Calls        int
	SkippedSpans int // non-GenAI spans / non-generation lines / bad lines
	Errors       []string
}

// Scan ingests a file or directory (recursive). Files are routed by content:
// anything containing "resourceSpans" parses as OTLP/JSON, .jsonl/.ndjson
// files parse as Langfuse observations. Other files are ignored.
func Scan(path string) ([]trace.Call, Stats, error) {
	var files []string
	fi, err := os.Stat(path)
	if err != nil {
		return nil, Stats{}, err
	}
	if fi.IsDir() {
		err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			switch strings.ToLower(filepath.Ext(p)) {
			case ".json", ".jsonl", ".ndjson":
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return nil, Stats{}, err
		}
	} else {
		files = []string{path}
	}
	sort.Strings(files) // deterministic ingest order

	var all []trace.Call
	stats := Stats{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("%s: %v", f, err))
			continue
		}
		calls, skipped, err := parseFile(raw, f)
		if err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("%s: %v", f, err))
			continue
		}
		stats.Files++
		stats.SkippedSpans += skipped
		all = append(all, calls...)
	}
	stats.Calls = len(all)
	if stats.Files == 0 {
		return nil, stats, fmt.Errorf("no ingestible files under %s (need OTLP .json or Langfuse .jsonl)", path)
	}
	return all, stats, nil
}

func parseFile(raw []byte, path string) ([]trace.Call, int, error) {
	name := filepath.Base(path)
	if bytes.Contains(raw, []byte(`"resourceSpans"`)) {
		return parseOTLP(raw, name)
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".jsonl" || ext == ".ndjson" {
		return parseLangfuseJSONL(raw, name)
	}
	// A .json file without resourceSpans: try Langfuse JSONL as a fallback
	// (some exports use .json), otherwise report unsupported.
	calls, skipped, err := parseLangfuseJSONL(raw, name)
	if err == nil && len(calls) > 0 {
		return calls, skipped, nil
	}
	return nil, 0, fmt.Errorf("unrecognized trace format")
}
