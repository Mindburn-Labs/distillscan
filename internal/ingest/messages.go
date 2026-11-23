package ingest

import (
	"encoding/json"
	"strings"
)

// looksLikeJSON reports whether s is a non-trivial JSON object or array —
// the "structured output" half of the evaluability proxy.
func looksLikeJSON(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return false
	}
	if s[0] != '{' && s[0] != '[' {
		return false
	}
	return json.Valid([]byte(s))
}

// extractTextFromJSON walks arbitrary JSON (message lists in any dialect) and
// concatenates every "content"/"text" string field, in document order. This
// keeps us tolerant of the many message encodings emitters use.
func extractTextFromJSON(raw []byte) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw) // not JSON: treat as plain text
	}
	var parts []string
	collectText(v, &parts)
	return strings.Join(parts, "\n")
}

func collectText(v any, out *[]string) {
	switch t := v.(type) {
	case map[string]any:
		for _, k := range []string{"content", "text"} {
			if s, ok := t[k].(string); ok && s != "" {
				*out = append(*out, s)
			}
		}
		// Recurse into nested structures (e.g. content: [{type:text,...}]).
		for _, k := range []string{"content", "parts", "messages"} {
			if sub, ok := t[k]; ok {
				if _, isString := sub.(string); !isString {
					collectText(sub, out)
				}
			}
		}
	case []any:
		for _, e := range t {
			collectText(e, out)
		}
	}
}
