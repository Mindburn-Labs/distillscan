package ingest

import "testing"

func TestLangfuseTokenAndCostPrecedence(t *testing.T) {
	lines := []byte(`
{"id":"o1","type":"GENERATION","name":"summarize","model":"claude-opus-4","startTime":"2026-07-22T10:00:00.000Z","endTime":"2026-07-22T10:00:04.200Z","usageDetails":{"input":14000,"output":1800,"total":15800},"costDetails":{"input":0.21,"output":0.135,"total":0.345},"input":[{"role":"user","content":"Summarize this contract"}],"output":{"role":"assistant","content":"The contract..."},"level":"DEFAULT"}
{"id":"o2","type":"GENERATION","name":"translate","model":"deepseek-chat","startTime":"2026-07-22T11:00:00.000Z","promptTokens":1800,"completionTokens":400,"input":"Translate: hello","output":"bonjour","level":"DEFAULT"}
{"id":"o3","type":"SPAN","name":"pipeline","startTime":"2026-07-22T11:00:00.000Z"}
{"id":"o4","type":"GENERATION","name":"summarize","model":"claude-opus-4","startTime":"2026-07-22T12:00:00.000Z","calculatedTotalCost":0.31,"usage":{"input":13000,"output":1500},"input":"doc","output":"summary","level":"ERROR"}
`)
	calls, skipped, err := parseLangfuseJSONL(lines, "obs.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 || skipped != 1 {
		t.Fatalf("got %d calls, %d skipped; want 3 calls, 1 skipped (the SPAN)", len(calls), skipped)
	}

	// o1: usageDetails + costDetails win.
	c := calls[0]
	if c.InputTokens != 14000 || c.OutputTokens != 1800 {
		t.Errorf("o1 tokens = %d/%d, want 14000/1800", c.InputTokens, c.OutputTokens)
	}
	if !c.HasCost || c.CostUSD != 0.345 {
		t.Errorf("o1 cost = %v (has=%v), want 0.345 from costDetails.total", c.CostUSD, c.HasCost)
	}
	if c.Task != "summarize" {
		t.Errorf("o1 task = %q, want summarize (observation name)", c.Task)
	}
	if c.Provider != "anthropic" {
		t.Errorf("o1 provider = %q, want anthropic (guessed)", c.Provider)
	}
	if c.LatencyMS != 4200 {
		t.Errorf("o1 latency = %v, want 4200", c.LatencyMS)
	}

	// o2: legacy promptTokens/completionTokens fallback; no cost carried.
	c = calls[1]
	if c.InputTokens != 1800 || c.OutputTokens != 400 {
		t.Errorf("o2 tokens = %d/%d, want 1800/400", c.InputTokens, c.OutputTokens)
	}
	if c.HasCost {
		t.Error("o2 HasCost = true, want false (priced from bundled table later)")
	}

	// o4: usage{} mid-generation spelling + calculatedTotalCost + ERROR level.
	c = calls[2]
	if c.InputTokens != 13000 || c.OutputTokens != 1500 {
		t.Errorf("o4 tokens = %d/%d, want 13000/1500", c.InputTokens, c.OutputTokens)
	}
	if !c.HasCost || c.CostUSD != 0.31 {
		t.Errorf("o4 cost = %v, want 0.31 from calculatedTotalCost", c.CostUSD)
	}
	if !c.Error {
		t.Error("o4 Error = false, want true (level=ERROR)")
	}
}
