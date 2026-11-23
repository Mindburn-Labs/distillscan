package ingest

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/Mindburn-Labs/distillscan/internal/trace"
)

// These wire types cover the OTLP JSON fields needed for ingestion.
type otlpFile struct {
	ResourceSpans []struct {
		ScopeSpans []struct {
			Spans []otlpSpan `json:"spans"`
		} `json:"scopeSpans"`
	} `json:"resourceSpans"`
}

type otlpSpan struct {
	SpanID            string          `json:"spanId"`
	Name              string          `json:"name"`
	StartTimeUnixNano json.RawMessage `json:"startTimeUnixNano"` // string or number
	EndTimeUnixNano   json.RawMessage `json:"endTimeUnixNano"`
	Attributes        []otlpKV        `json:"attributes"`
	Status            struct {
		Code json.RawMessage `json:"code"` // 2 or "STATUS_CODE_ERROR"
	} `json:"status"`
}

type otlpKV struct {
	Key   string   `json:"key"`
	Value anyValue `json:"value"`
}

type anyValue struct {
	StringValue *string         `json:"stringValue"`
	IntValue    json.RawMessage `json:"intValue"` // protojson encodes int64 as string
	DoubleValue *float64        `json:"doubleValue"`
	BoolValue   *bool           `json:"boolValue"`
	ArrayValue  *struct {
		Values []anyValue `json:"values"`
	} `json:"arrayValue"`
}

func (v anyValue) asString() string {
	switch {
	case v.StringValue != nil:
		return *v.StringValue
	case v.IntValue != nil:
		return strings.Trim(string(v.IntValue), `"`)
	case v.DoubleValue != nil:
		return strconv.FormatFloat(*v.DoubleValue, 'f', -1, 64)
	case v.BoolValue != nil:
		return strconv.FormatBool(*v.BoolValue)
	case v.ArrayValue != nil:
		parts := make([]string, 0, len(v.ArrayValue.Values))
		for _, e := range v.ArrayValue.Values {
			parts = append(parts, e.asString())
		}
		return strings.Join(parts, ",")
	}
	return ""
}

func (v anyValue) asInt() (int64, bool) {
	if v.IntValue != nil {
		s := strings.Trim(string(v.IntValue), `"`)
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n, true
		}
	}
	if v.DoubleValue != nil {
		return int64(*v.DoubleValue), true
	}
	if v.StringValue != nil {
		if n, err := strconv.ParseInt(*v.StringValue, 10, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}

// parseOTLP extracts normalized Calls from an OTLP/JSON trace export.
// Spans without any GenAI signal (no model, provider, or token usage) are
// skipped and counted.
func parseOTLP(raw []byte, source string) ([]trace.Call, int, error) {
	var f otlpFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, 0, err
	}
	var calls []trace.Call
	skipped := 0
	for _, rs := range f.ResourceSpans {
		for _, ss := range rs.ScopeSpans {
			for _, sp := range ss.Spans {
				c, ok := normalizeSpan(sp, source)
				if !ok {
					skipped++
					continue
				}
				calls = append(calls, c)
			}
		}
	}
	return calls, skipped, nil
}

func parseUnixNano(raw json.RawMessage) time.Time {
	s := strings.Trim(string(raw), `"`)
	if s == "" || s == "null" {
		return time.Time{}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}
