// genfixtures generates the synthetic sample traces under fixtures/.
// A fixed seed and time window keep the files reproducible.
//
//	otlp/invoice-extraction.json  legacy GenAI attrs (gen_ai.system,
//	                              prompt/completion_tokens), gpt-4o,
//	                              templated extraction, JSON outputs
//	otlp/support-triage.json      current attrs (provider.name,
//	                              input/output_tokens, input/output messages),
//	                              gpt-4o-mini, huge volume, cheap; the
//	                              instruction is revised mid-window (drift)
//	otlp/agent-tools.json         OpenLLMetry indexed prompts, claude-sonnet-4
//	                              tool-calling agent loop: heterogeneous
//	                              prompts, tool_calls outputs, errors,
//	                              latency outliers
//	otlp/chat-mixed.json          current attrs, NO task attribute ->
//	                              exercises template clustering; two prompt
//	                              families; a few calls on an unpriced model
//	langfuse/observations.jsonl   observations_v2 export: contract-summarize
//	                              on claude-opus-4 (costs carried in-trace),
//	                              product-translate on deepseek-chat (priced
//	                              from the bundled table); non-generation
//	                              lines mixed in
//
// Usage: go run ./tools/genfixtures [-out fixtures]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"
)

var (
	windowStart = time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	windowEnd   = time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
)

func main() {
	out := flag.String("out", "fixtures", "output directory")
	flag.Parse()
	r := rand.New(rand.NewSource(42)) // fixed seed: fixtures are reproducible

	must(os.MkdirAll(filepath.Join(*out, "otlp"), 0o755))
	must(os.MkdirAll(filepath.Join(*out, "langfuse"), 0o755))

	writeJSON(filepath.Join(*out, "otlp", "invoice-extraction.json"), invoiceFile(r))
	writeJSON(filepath.Join(*out, "otlp", "support-triage.json"), triageFile(r))
	writeJSON(filepath.Join(*out, "otlp", "agent-tools.json"), agentFile(r))
	writeJSON(filepath.Join(*out, "otlp", "chat-mixed.json"), chatFile(r))
	writeLines(filepath.Join(*out, "langfuse", "observations.jsonl"), langfuseLines(r))

	must(os.WriteFile(filepath.Join(*out, "distillscan.yaml"), []byte(demoConfig), 0o644))
	fmt.Println("fixtures written to", *out)
}

const demoConfig = `# Demo declared factors for the fixture scan.
# These are folded into scoring but always rendered "declared, not measured".
# The traces here are synthetic, so rights are declared "yes" to show the full
# verdict range; with the default "unknown", READY is capped at BORDERLINE
# (ready pending rights verification).
data_rights: "yes"      # yes | no | unknown - teacher ToS allows training on outputs
safety_critical: false  # true caps verdicts at BORDERLINE; see internal/score
substitution_ratio: 0.65
sampling_rate: 1.0      # declared share of real traffic in this export; <1 scales extrapolation up
`

// timeline spreads n timestamps across the window with jitter, sorted.
func timeline(r *rand.Rand, n int) []time.Time {
	span := windowEnd.Sub(windowStart)
	step := span / time.Duration(n)
	ts := make([]time.Time, n)
	for i := range ts {
		jitter := time.Duration((r.Float64() - 0.5) * float64(step) * 0.8)
		ts[i] = windowStart.Add(step*time.Duration(i) + jitter)
	}
	return ts
}

func writeJSON(path string, doc map[string]any) {
	raw, err := json.MarshalIndent(doc, "", " ")
	must(err)
	must(os.WriteFile(path, append(raw, '\n'), 0o644))
	fmt.Printf("  %s (%d KB)\n", path, len(raw)/1024)
}

func writeLines(path string, lines [][]byte) {
	var b []byte
	for _, l := range lines {
		b = append(b, l...)
		b = append(b, '\n')
	}
	must(os.WriteFile(path, b, 0o644))
	fmt.Printf("  %s (%d KB, %d lines)\n", path, len(b)/1024, len(lines))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
