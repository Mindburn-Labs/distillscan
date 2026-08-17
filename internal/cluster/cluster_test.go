package cluster

import (
	"testing"
	"time"

	"github.com/Mindburn-Labs/distillscan/internal/trace"
)

func TestTemplateSlotExtraction(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "digits, dates, whitespace",
			in:   "Extract invoice INV-4821   dated 2026-07-03,\n total 12,480.55 EUR",
			want: "extract invoice inv-<num> dated <date>, total <num> eur",
		},
		{
			name: "uuid and email",
			in:   "User 550e8400-e29b-41d4-a716-446655440000 (jane.doe+x@example.co.uk) asked",
			want: "user <uuid> (<email>) asked",
		},
		{
			name: "url and hex id",
			in:   "Fetch https://api.example.com/v1/things?id=9 for request abcdef0123456789",
			want: "fetch <url> for request <hex>",
		},
		{
			name: "single digits survive",
			in:   "Rate from 1 to 5",
			want: "rate from 1 to 5",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Template(tc.in); got != tc.want {
				t.Errorf("Template(%q)\n got:  %q\n want: %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestTemplateKeyStability(t *testing.T) {
	a := "Extract fields from invoice INV-1001 dated 2026-07-01 for acct 8842"
	b := "Extract   fields from invoice INV-9977 dated 2026-07-28 for acct 1150"
	c := "Classify this support ticket: printer on fire"

	if TemplateKey(a) != TemplateKey(b) {
		t.Errorf("same template, different slot values should share a key:\n a=%s\n b=%s", TemplateKey(a), TemplateKey(b))
	}
	if TemplateKey(a) == TemplateKey(c) {
		t.Error("different templates must not share a key")
	}
	// Deterministic across calls (pure function of input).
	if TemplateKey(a) != TemplateKey(a) {
		t.Error("TemplateKey is not deterministic")
	}
}

func TestGroupPrecedenceAndDeterminism(t *testing.T) {
	ts := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	calls := []trace.Call{
		{Task: "invoice-extraction", Prompt: "whatever A", Timestamp: ts},
		{Task: "Invoice-Extraction", Prompt: "whatever B", Timestamp: ts.Add(time.Hour)}, // case-folded into same task
		{Prompt: "Summarize document 123 please", Timestamp: ts},
		{Prompt: "Summarize document 999 please", Timestamp: ts},
		{Model: "gpt-4o", Timestamp: ts}, // no task, no prompt -> model bucket
	}
	got := Group(calls)
	if len(got) != 3 {
		for _, g := range got {
			t.Logf("cluster %s (%s): %d calls", g.Key, g.Kind, len(g.Calls))
		}
		t.Fatalf("got %d clusters, want 3 (task, template, model)", len(got))
	}
	// Deterministic ordering by key and stable membership across runs.
	again := Group(calls)
	for i := range got {
		if got[i].Key != again[i].Key || len(got[i].Calls) != len(again[i].Calls) {
			t.Fatalf("Group is not deterministic: run1[%d]=%s(%d) run2[%d]=%s(%d)",
				i, got[i].Key, len(got[i].Calls), i, again[i].Key, len(again[i].Calls))
		}
	}
	kinds := map[string]int{}
	for _, g := range got {
		kinds[g.Kind] = len(g.Calls)
	}
	if kinds["task"] != 2 || kinds["template"] != 2 || kinds["model"] != 1 {
		t.Errorf("membership by kind = %v, want task:2 template:2 model:1", kinds)
	}
}
