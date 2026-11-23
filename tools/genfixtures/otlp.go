package main

import (
	"fmt"
	"math/rand"
	"strconv"
	"time"
)

func kvStr(k, v string) map[string]any {
	return map[string]any{"key": k, "value": map[string]any{"stringValue": v}}
}

// kvIntStr encodes int64 the protojson way (string) — most real exporters.
func kvIntStr(k string, n int64) map[string]any {
	return map[string]any{"key": k, "value": map[string]any{"intValue": strconv.FormatInt(n, 10)}}
}

// kvIntNum encodes int64 as a bare number — some hand-rolled exporters.
func kvIntNum(k string, n int64) map[string]any {
	return map[string]any{"key": k, "value": map[string]any{"intValue": n}}
}

func otlpDoc(service string, spans []map[string]any) map[string]any {
	return map[string]any{
		"resourceSpans": []any{map[string]any{
			"resource": map[string]any{"attributes": []any{kvStr("service.name", service)}},
			"scopeSpans": []any{map[string]any{
				"scope": map[string]any{"name": "distillscan.fixtures", "version": "1.0.0"},
				"spans": spans,
			}},
		}},
	}
}

func mkSpan(r *rand.Rand, name string, start time.Time, durMS float64, attrs []map[string]any, isErr bool) map[string]any {
	code := "STATUS_CODE_UNSET"
	if isErr {
		code = "STATUS_CODE_ERROR"
	}
	return map[string]any{
		"traceId":           fmt.Sprintf("%016x%016x", r.Uint64(), r.Uint64()),
		"spanId":            fmt.Sprintf("%016x", r.Uint64()),
		"name":              name,
		"kind":              "SPAN_KIND_CLIENT",
		"startTimeUnixNano": strconv.FormatInt(start.UnixNano(), 10),
		"endTimeUnixNano":   strconv.FormatInt(start.Add(time.Duration(durMS*float64(time.Millisecond))).UnixNano(), 10),
		"attributes":        attrs,
		"status":            map[string]any{"code": code},
	}
}
