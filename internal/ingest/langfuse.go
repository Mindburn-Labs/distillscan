package ingest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/Mindburn-Labs/distillscan/internal/trace"
)

// Langfuse observations_v2 JSONL export: one JSON object per line.
// Parsed tolerantly — unknown fields ignored, several field spellings
// accepted (usageDetails/costDetails vs legacy usage/promptTokens), only
// GENERATION-type observations become Calls.
type lfObservation struct {
	ID        string          `json:"id"`
	TraceID   string          `json:"traceId"`
	Type      string          `json:"type"`
	Name      string          `json:"name"`
	Model     string          `json:"model"`
	StartTime string          `json:"startTime"`
	EndTime   string          `json:"endTime"`
	Input     json.RawMessage `json:"input"`
	Output    json.RawMessage `json:"output"`

	// Token accounting, newest to oldest spelling.
	UsageDetails map[string]int64 `json:"usageDetails"`
	Usage        *struct {
		Input  int64 `json:"input"`
		Output int64 `json:"output"`
	} `json:"usage"`
	PromptTokens     *int64 `json:"promptTokens"`
	CompletionTokens *int64 `json:"completionTokens"`

	// Cost, newest to oldest spelling. USD.
	CostDetails          map[string]float64 `json:"costDetails"`
	CalculatedTotalCost  *float64           `json:"calculatedTotalCost"`
	CalculatedInputCost  *float64           `json:"calculatedInputCost"`
	CalculatedOutputCost *float64           `json:"calculatedOutputCost"`

	Level         string   `json:"level"`
	StatusMessage *string  `json:"statusMessage"`
	LatencyMS     *float64 `json:"latency"`
}

// parseLangfuseJSONL converts an observations_v2 export into Calls.
// Returns calls plus the count of skipped lines (non-generations, blanks,
// unparsable lines).
func parseLangfuseJSONL(raw []byte, source string) ([]trace.Call, int, error) {
	var calls []trace.Call
	skipped := 0
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024) // tolerate long lines
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var o lfObservation
		if err := json.Unmarshal(line, &o); err != nil {
			skipped++
			continue
		}
		if !strings.EqualFold(o.Type, "GENERATION") {
			skipped++
			continue
		}
		calls = append(calls, normalizeObservation(o, source))
	}
	if err := sc.Err(); err != nil {
		return calls, skipped, err
	}
	return calls, skipped, nil
}

func normalizeObservation(o lfObservation, source string) trace.Call {
	c := trace.Call{
		ID:     o.ID,
		Source: source,
		Model:  o.Model,
		Task:   o.Name, // Langfuse observation name is an explicit task label
	}
	c.Provider = trace.GuessProvider(o.Model)

	// Tokens: usageDetails > usage > promptTokens/completionTokens.
	switch {
	case len(o.UsageDetails) > 0:
		c.InputTokens = o.UsageDetails["input"]
		c.OutputTokens = o.UsageDetails["output"]
	case o.Usage != nil:
		c.InputTokens = o.Usage.Input
		c.OutputTokens = o.Usage.Output
	default:
		if o.PromptTokens != nil {
			c.InputTokens = *o.PromptTokens
		}
		if o.CompletionTokens != nil {
			c.OutputTokens = *o.CompletionTokens
		}
	}

	// Cost: costDetails.total > calculatedTotalCost > input+output parts.
	switch {
	case o.CostDetails["total"] > 0:
		c.CostUSD, c.HasCost = o.CostDetails["total"], true
	case o.CalculatedTotalCost != nil && *o.CalculatedTotalCost > 0:
		c.CostUSD, c.HasCost = *o.CalculatedTotalCost, true
	case o.CalculatedInputCost != nil || o.CalculatedOutputCost != nil:
		var sum float64
		if o.CalculatedInputCost != nil {
			sum += *o.CalculatedInputCost
		}
		if o.CalculatedOutputCost != nil {
			sum += *o.CalculatedOutputCost
		}
		if sum > 0 {
			c.CostUSD, c.HasCost = sum, true
		}
	}

	if ts, err := time.Parse(time.RFC3339, o.StartTime); err == nil {
		c.Timestamp = ts.UTC()
	}
	if o.LatencyMS != nil {
		c.LatencyMS = *o.LatencyMS
	} else if o.EndTime != "" {
		if end, err := time.Parse(time.RFC3339, o.EndTime); err == nil && !c.Timestamp.IsZero() && end.After(c.Timestamp) {
			c.LatencyMS = float64(end.Sub(c.Timestamp)) / float64(time.Millisecond)
		}
	}

	c.Prompt = rawToText(o.Input)
	c.Output = rawToText(o.Output)
	c.Error = strings.EqualFold(o.Level, "ERROR")
	c.StructuredOutput = looksLikeJSON(c.Output)
	return c
}

// rawToText renders a Langfuse input/output field (string, object, or message
// array) as plain text, reusing the tolerant JSON text walker.
func rawToText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s // plain JSON string
	}
	return extractTextFromJSON(raw)
}
