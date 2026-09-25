package pricing

import "testing"

func TestModelMatching(t *testing.T) {
	tbl, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		model  string
		wantOK bool
	}{
		{"gpt-4o", true},
		{"gpt-4o-2024-08-06", true},        // date suffix stripped
		{"openai/gpt-4o", true},            // provider prefix stripped
		{"GPT-4o-mini", true},              // case-insensitive, longest key wins
		{"claude-sonnet-4-20250514", true}, // Anthropic date suffix
		{"claude-3-5-haiku-latest", true},  // -latest suffix
		{"deepseek-chat", true},
		{"totally-unknown-model", false},
		{"", false},
	}
	for _, tc := range cases {
		_, ok := tbl.Cost(tc.model, 1000, 100)
		if ok != tc.wantOK {
			t.Errorf("Cost(%q) matched=%v, want %v (normalized: %q)", tc.model, ok, tc.wantOK, Normalize(tc.model))
		}
	}

	// gpt-4o-mini must not fall through to gpt-4o pricing.
	mini, _ := tbl.Cost("gpt-4o-mini", 1_000_000, 0)
	full, _ := tbl.Cost("gpt-4o", 1_000_000, 0)
	if mini >= full {
		t.Errorf("gpt-4o-mini (%v) should be cheaper than gpt-4o (%v): longest-prefix match broken", mini, full)
	}
}

// Current models, official list prices verified 2026-09-25, reached through
// the IDs traces actually carry: direct API, dated snapshots, and OpenRouter.
func TestCurrentModelPrices(t *testing.T) {
	tbl, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		model         string
		input, output float64
	}{
		{"claude-fable-5-1", 10, 50},
		{"anthropic/claude-fable-5.1", 10, 50},
		{"claude-opus-5-5", 4, 20},
		{"anthropic/claude-opus-5.5", 4, 20},
		{"claude-sonnet-5", 2, 10},
		{"anthropic/claude-sonnet-5", 2, 10},
		{"claude-haiku-4-5", 1, 5},
		{"claude-haiku-4-5-20251001", 1, 5},
		{"anthropic/claude-haiku-4.5", 1, 5},
		{"gpt-6-astra", 10, 50},
		{"openai/gpt-6-astra", 10, 50},
		{"gpt-6-sol", 2, 10},
		{"openai/gpt-6-sol", 2, 10},
		{"gpt-6-luna", 0.10, 0.50},
		{"openai/gpt-6-luna", 0.10, 0.50},
		// Opus 4.5+ must not prefix-match claude-opus-4 ($15/$75).
		{"claude-opus-4-5-20251101", 5, 25},
		{"anthropic/claude-opus-4.8", 5, 25},
		{"claude-opus-4-1", 15, 75},
	}
	for _, tc := range cases {
		in, okIn := tbl.Cost(tc.model, 1_000_000, 0)
		out, okOut := tbl.Cost(tc.model, 0, 1_000_000)
		if !okIn || !okOut || in != tc.input || out != tc.output {
			t.Errorf("Cost(%q) per 1M = %v in / %v out (ok=%v), want %v / %v (normalized: %q)",
				tc.model, in, out, okIn, tc.input, tc.output, Normalize(tc.model))
		}
	}
}
