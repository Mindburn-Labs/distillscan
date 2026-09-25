// Package pricing estimates per-call USD cost from a bundled snapshot table.
// The table is embedded at build time — the scanner never fetches prices.
package pricing

import (
	_ "embed"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed prices.yaml
var raw []byte

// ModelPrice is USD per 1M input/output tokens.
type ModelPrice struct {
	Input  float64 `yaml:"input"`
	Output float64 `yaml:"output"`
}

type Table struct {
	SnapshotDate string                `yaml:"snapshot_date"`
	Models       map[string]ModelPrice `yaml:"models"`

	keys []string // normalized keys, longest first, for prefix matching
}

// Load parses the embedded snapshot.
func Load() (*Table, error) {
	var t Table
	if err := yaml.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("embedded prices.yaml: %w", err)
	}
	for k := range t.Models {
		t.keys = append(t.keys, k)
	}
	// Longest key first so "gpt-4o-mini" matches before "gpt-4o".
	sort.Slice(t.keys, func(i, j int) bool {
		if len(t.keys[i]) != len(t.keys[j]) {
			return len(t.keys[i]) > len(t.keys[j])
		}
		return t.keys[i] < t.keys[j]
	})
	return &t, nil
}

var reSuffix = regexp.MustCompile(`-(20\d{6}|20\d\d-\d\d-\d\d|latest|v\d+|\d{4})$`)

// Normalize canonicalizes a raw model name for table lookup.
func Normalize(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(m, "/"); i >= 0 { // "openai/gpt-4o" -> "gpt-4o"
		m = m[i+1:]
	}
	if strings.HasPrefix(m, "claude-") { // OpenRouter "claude-opus-5.5" -> Anthropic "claude-opus-5-5"
		m = strings.ReplaceAll(m, ".", "-")
	}
	for {
		next := reSuffix.ReplaceAllString(m, "")
		if next == m {
			break
		}
		m = next
	}
	return m
}

// Cost estimates USD for a call. ok=false means the model is not in the
// snapshot (the call is counted as unpriced, never guessed).
func (t *Table) Cost(model string, inTok, outTok int64) (usd float64, ok bool) {
	m := Normalize(model)
	if m == "" {
		return 0, false
	}
	if p, exact := t.Models[m]; exact {
		return price(p, inTok, outTok), true
	}
	for _, k := range t.keys {
		if strings.HasPrefix(m, k) {
			return price(t.Models[k], inTok, outTok), true
		}
	}
	return 0, false
}

func price(p ModelPrice, inTok, outTok int64) float64 {
	return float64(inTok)/1e6*p.Input + float64(outTok)/1e6*p.Output
}
