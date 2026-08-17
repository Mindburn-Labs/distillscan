// Package trace defines the normalized call model that every ingester maps
// raw records into. Everything downstream (clustering, scoring, reporting)
// only ever sees Calls — the format-specific mess stays inside internal/ingest.
package trace

import "time"

// Call is one LLM invocation, normalized from any supported input format
// (OTLP/JSON GenAI spans across attribute generations, Langfuse
// observations_v2 JSONL).
type Call struct {
	ID       string // span id / observation id when present
	Source   string // file the call came from
	Provider string // "openai", "anthropic", ... best effort
	Model    string // response model preferred over request model

	InputTokens  int64
	OutputTokens int64

	// CostUSD is the per-call cost carried by the trace itself (e.g. Langfuse
	// calculated cost). When HasCost is false, cost is estimated later from
	// the bundled pricing table.
	CostUSD float64
	HasCost bool

	LatencyMS float64
	Timestamp time.Time

	Prompt string // best-effort prompt text (concatenated message contents)
	Output string // best-effort output text

	// Task is an explicit task/endpoint hint (app.task, http.route, agent
	// name, Langfuse observation name, ...). Empty when the trace carries
	// none; such calls are clustered by prompt template instead.
	Task string

	// Error marks failed calls (OTLP error status / error.type attribute,
	// Langfuse level=ERROR).
	Error bool

	// StructuredOutput marks outputs that parse as JSON or are tool calls.
	// The evaluability factor is the share of these in a cluster.
	StructuredOutput bool
}

// GuessProvider infers a provider from a model name when the trace does not
// carry an explicit provider attribute. Best effort only.
func GuessProvider(model string) string {
	switch {
	case has(model, "gpt", "o1", "o3", "o4", "davinci"):
		return "openai"
	case has(model, "claude"):
		return "anthropic"
	case has(model, "gemini", "gemma"):
		return "google"
	case has(model, "deepseek"):
		return "deepseek"
	case has(model, "llama"):
		return "meta"
	case has(model, "mistral", "mixtral", "ministral"):
		return "mistralai"
	case has(model, "qwen"):
		return "alibaba"
	default:
		return ""
	}
}

func has(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if len(s) >= len(p) && equalFold(s[:len(p)], p) {
			return true
		}
	}
	return false
}

// equalFold is a tiny ASCII-only case-insensitive compare (avoids importing
// strings just for this file's hot path; models are ASCII).
func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
