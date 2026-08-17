package ingest

import (
	"fmt"
	"testing"
)

// wrap builds a minimal OTLP/JSON file around one span's attributes/status.
func wrap(attrs, status string) []byte {
	return []byte(fmt.Sprintf(`{
	  "resourceSpans": [{"scopeSpans": [{"spans": [{
	    "spanId": "abc123",
	    "name": "chat",
	    "startTimeUnixNano": "1753142400000000000",
	    "endTimeUnixNano": "1753142401500000000",
	    "attributes": [%s],
	    "status": {%s}
	  }]}]}]
	}`, attrs, status))
}

func TestNormalizeAcrossAttributeGenerations(t *testing.T) {
	cases := []struct {
		name  string
		attrs string
		check func(t *testing.T, got any)
	}{
		{
			name: "legacy generation (gen_ai.system + prompt/completion_tokens)",
			attrs: `
			  {"key":"gen_ai.system","value":{"stringValue":"openai"}},
			  {"key":"gen_ai.request.model","value":{"stringValue":"gpt-4o"}},
			  {"key":"gen_ai.usage.prompt_tokens","value":{"intValue":"1200"}},
			  {"key":"gen_ai.usage.completion_tokens","value":{"intValue":"300"}},
			  {"key":"app.task","value":{"stringValue":"invoice-extraction"}},
			  {"key":"gen_ai.prompt","value":{"stringValue":"Extract fields from invoice INV-1234"}},
			  {"key":"gen_ai.completion","value":{"stringValue":"{\"total\": 12}"}}`,
		},
		{
			name: "current generation (provider.name + input/output_tokens, int as number)",
			attrs: `
			  {"key":"gen_ai.provider.name","value":{"stringValue":"openai"}},
			  {"key":"gen_ai.request.model","value":{"stringValue":"gpt-4o-2024-08-06"}},
			  {"key":"gen_ai.response.model","value":{"stringValue":"gpt-4o"}},
			  {"key":"gen_ai.usage.input_tokens","value":{"intValue":1200}},
			  {"key":"gen_ai.usage.output_tokens","value":{"intValue":300}},
			  {"key":"app.task","value":{"stringValue":"invoice-extraction"}},
			  {"key":"gen_ai.input.messages","value":{"stringValue":"[{\"role\":\"user\",\"content\":\"Extract fields from invoice INV-1234\"}]"}},
			  {"key":"gen_ai.output.messages","value":{"stringValue":"[{\"role\":\"assistant\",\"content\":\"{\\\"total\\\": 12}\"}]"}}`,
		},
		{
			name: "OpenLLMetry generation (indexed prompts)",
			attrs: `
			  {"key":"gen_ai.system","value":{"stringValue":"openai"}},
			  {"key":"llm.request.model","value":{"stringValue":"gpt-4o"}},
			  {"key":"llm.usage.prompt_tokens","value":{"intValue":"1200"}},
			  {"key":"llm.usage.completion_tokens","value":{"intValue":"300"}},
			  {"key":"app.task","value":{"stringValue":"invoice-extraction"}},
			  {"key":"gen_ai.prompt.0.role","value":{"stringValue":"system"}},
			  {"key":"gen_ai.prompt.0.content","value":{"stringValue":"Extract fields"}},
			  {"key":"gen_ai.prompt.1.role","value":{"stringValue":"user"}},
			  {"key":"gen_ai.prompt.1.content","value":{"stringValue":"from invoice INV-1234"}},
			  {"key":"gen_ai.completion.0.content","value":{"stringValue":"{\"total\": 12}"}}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls, skipped, err := parseOTLP(wrap(tc.attrs, `"code":"STATUS_CODE_UNSET"`), "test.json")
			if err != nil {
				t.Fatalf("parseOTLP: %v", err)
			}
			if skipped != 0 || len(calls) != 1 {
				t.Fatalf("got %d calls, %d skipped; want 1, 0", len(calls), skipped)
			}
			c := calls[0]
			if c.Provider != "openai" {
				t.Errorf("Provider = %q, want openai", c.Provider)
			}
			if c.Model != "gpt-4o" {
				t.Errorf("Model = %q, want gpt-4o (response model wins)", c.Model)
			}
			if c.InputTokens != 1200 || c.OutputTokens != 300 {
				t.Errorf("tokens = %d/%d, want 1200/300", c.InputTokens, c.OutputTokens)
			}
			if c.Task != "invoice-extraction" {
				t.Errorf("Task = %q, want invoice-extraction", c.Task)
			}
			if c.Prompt == "" {
				t.Error("Prompt is empty, want extracted text")
			}
			if !c.StructuredOutput {
				t.Errorf("StructuredOutput = false, want true (output %q is JSON)", c.Output)
			}
			if c.Error {
				t.Error("Error = true, want false")
			}
			if c.LatencyMS != 1500 {
				t.Errorf("LatencyMS = %v, want 1500", c.LatencyMS)
			}
			if c.Timestamp.IsZero() {
				t.Error("Timestamp is zero")
			}
		})
	}
}

func TestErrorStatusAndNonGenAISkipped(t *testing.T) {
	// Error status: protojson enum string.
	calls, _, err := parseOTLP(wrap(
		`{"key":"gen_ai.system","value":{"stringValue":"openai"}},
		 {"key":"gen_ai.request.model","value":{"stringValue":"gpt-4o"}}`,
		`"code":"STATUS_CODE_ERROR"`), "t.json")
	if err != nil || len(calls) != 1 {
		t.Fatalf("calls=%d err=%v", len(calls), err)
	}
	if !calls[0].Error {
		t.Error("string status: Error = false, want true")
	}

	// Error status: numeric code 2.
	calls, _, err = parseOTLP(wrap(
		`{"key":"gen_ai.system","value":{"stringValue":"openai"}}`,
		`"code":2`), "t.json")
	if err != nil || len(calls) != 1 {
		t.Fatalf("calls=%d err=%v", len(calls), err)
	}
	if !calls[0].Error {
		t.Error("numeric status: Error = false, want true")
	}

	// A span with no GenAI attributes is skipped, not miscounted as a call.
	calls, skipped, err := parseOTLP(wrap(
		`{"key":"http.method","value":{"stringValue":"GET"}}`,
		`"code":"STATUS_CODE_UNSET"`), "t.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 || skipped != 1 {
		t.Errorf("non-GenAI span: got %d calls, %d skipped; want 0, 1", len(calls), skipped)
	}
}
