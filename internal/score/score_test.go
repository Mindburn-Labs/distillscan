package score

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Mindburn-Labs/distillscan/internal/cluster"
	"github.com/Mindburn-Labs/distillscan/internal/config"
	"github.com/Mindburn-Labs/distillscan/internal/pricing"
	"github.com/Mindburn-Labs/distillscan/internal/trace"
)

// readyCalls builds a cluster that scores READY on the measured factors:
// one task, one template, structured outputs, no errors, no outliers,
// trace-carried costs, timestamps spread across days.
func readyCalls(n int) []trace.Call {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	calls := make([]trace.Call, 0, n)
	for i := 0; i < n; i++ {
		calls = append(calls, trace.Call{
			Task:             "invoice-extraction",
			Model:            "test-model",
			Prompt:           "Extract the invoice fields from the document below.",
			Output:           `{"total": 100}`,
			StructuredOutput: true,
			InputTokens:      500,
			OutputTokens:     100,
			CostUSD:          0.01,
			HasCost:          true,
			LatencyMS:        400,
			Timestamp:        base.Add(time.Duration(i) * time.Hour),
		})
	}
	return calls
}

func runWith(t *testing.T, cfg config.Config) ([]Result, Assumptions) {
	t.Helper()
	prices, err := pricing.Load()
	if err != nil {
		t.Fatal(err)
	}
	return Run(cluster.Group(readyCalls(40)), prices, cfg, "")
}

func hasNote(r Result, substr string) bool {
	for _, n := range r.Notes {
		if strings.Contains(n, substr) {
			return true
		}
	}
	return false
}

func TestDataRightsGates(t *testing.T) {
	base := config.Default() // data_rights: unknown

	t.Run("yes passes READY through", func(t *testing.T) {
		cfg := base
		cfg.DataRights = "yes"
		res, _ := runWith(t, cfg)
		if res[0].Verdict != VerdictReady {
			t.Fatalf("verdict = %s (composite %v), want READY", res[0].Verdict, res[0].Composite)
		}
	})

	t.Run("unknown caps READY at BORDERLINE", func(t *testing.T) {
		res, _ := runWith(t, base)
		r := res[0]
		if r.Composite < ThresholdReady {
			t.Fatalf("test cluster composite %v below READY threshold; cap not exercised", r.Composite)
		}
		if r.Verdict != VerdictBorderline {
			t.Fatalf("verdict = %s, want BORDERLINE (data_rights=unknown cap)", r.Verdict)
		}
		if !hasNote(r, "ready pending rights verification") {
			t.Errorf("notes %v missing %q wording", r.Notes, "ready pending rights verification")
		}
	})

	t.Run("no forces NOT READY", func(t *testing.T) {
		cfg := base
		cfg.DataRights = "no"
		res, _ := runWith(t, cfg)
		if res[0].Verdict != VerdictNotReady {
			t.Fatalf("verdict = %s, want NOT READY", res[0].Verdict)
		}
		if !hasNote(res[0], "data_rights=no") {
			t.Errorf("notes %v missing data_rights=no gate note", res[0].Notes)
		}
	})

	t.Run("safety_critical caps declared-rights READY", func(t *testing.T) {
		cfg := base
		cfg.DataRights = "yes"
		cfg.SafetyCritical = true
		res, _ := runWith(t, cfg)
		if res[0].Verdict != VerdictBorderline {
			t.Fatalf("verdict = %s, want BORDERLINE (safety cap)", res[0].Verdict)
		}
		if !hasNote(res[0], "safety_critical") {
			t.Errorf("notes %v missing safety_critical gate note", res[0].Notes)
		}
	})
}

func TestSamplingRateScalesExtrapolation(t *testing.T) {
	full := config.Default()
	full.DataRights = "yes"

	sampled := full
	sampled.SamplingRate = 0.5

	resFull, asFull := runWith(t, full)
	resHalf, asHalf := runWith(t, sampled)

	if asFull.SamplingRate != 1.0 || asHalf.SamplingRate != 0.5 {
		t.Fatalf("assumptions sampling rates = %v / %v, want 1.0 / 0.5", asFull.SamplingRate, asHalf.SamplingRate)
	}
	f, h := resFull[0], resHalf[0]
	if f.SpendUSD != h.SpendUSD {
		t.Errorf("window spend changed with sampling: %v vs %v (must stay measured)", f.SpendUSD, h.SpendUSD)
	}
	if !close2(h.AnnualizedUSD, 2*f.AnnualizedUSD) {
		t.Errorf("annualized with sampling 0.5 = %v, want 2x %v", h.AnnualizedUSD, f.AnnualizedUSD)
	}
	if !close2(h.SavingsUSD, 2*f.SavingsUSD) {
		t.Errorf("savings with sampling 0.5 = %v, want 2x %v", h.SavingsUSD, f.SavingsUSD)
	}
	// Verdict and factors must be untouched by sampling.
	if f.Verdict != h.Verdict || f.Composite != h.Composite {
		t.Errorf("sampling changed verdict/composite: %s/%v vs %s/%v", f.Verdict, f.Composite, h.Verdict, h.Composite)
	}
}

func TestSamplingRateZeroValueGuard(t *testing.T) {
	// Configs built without config.Load (zero value) must not divide by zero.
	cfg := config.Config{DataRights: "yes", SubstitutionRatio: 0.65} // SamplingRate: 0
	res, as := runWith(t, cfg)
	if as.SamplingRate != 1.0 {
		t.Errorf("zero-value sampling rate normalized to %v, want 1.0", as.SamplingRate)
	}
	if math.IsInf(res[0].AnnualizedUSD, 0) || math.IsNaN(res[0].AnnualizedUSD) {
		t.Errorf("annualized = %v with zero-value sampling rate", res[0].AnnualizedUSD)
	}
}

func close2(a, b float64) bool {
	if b == 0 {
		return a == 0
	}
	return math.Abs(a-b)/math.Abs(b) < 1e-9
}
