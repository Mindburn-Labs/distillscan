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
