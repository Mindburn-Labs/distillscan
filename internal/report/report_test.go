package report

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Mindburn-Labs/distillscan/internal/ingest"
	"github.com/Mindburn-Labs/distillscan/internal/score"
)

// fakeResults returns ranked results straddling the $100 threshold:
// two above ($8000 READY, $500 NOT READY), two below ($60 READY, $5 BORDERLINE).
func fakeResults() []score.Result {
	mk := func(key, label, verdict string, savings float64) score.Result {
		return score.Result{
			Key: key, Label: label, Kind: "task", Calls: 100,
			Models: map[string]int{"m": 100}, TopModel: "m",
			SpendUSD: savings / 52, AnnualizedUSD: savings / 0.65, SavingsUSD: savings,
			Composite: 0.7, Verdict: verdict,
		}
	}
	return []score.Result{
		mk("task:big", "big-workload", score.VerdictReady, 8000),
		mk("task:mid", "mid-workload", score.VerdictNotReady, 500),
		mk("task:small", "small-workload", score.VerdictReady, 60),
		mk("task:tiny", "tiny-workload", score.VerdictBorderline, 5),
	}
}

func fakeAssumptions() score.Assumptions {
	return score.Assumptions{
		SubstitutionRatio:   0.65,
		AnnualizationFactor: 52.0,
		WindowDays:          7.0,
		PricingSnapshot:     "2026-08-01",
		DataRights:          "unknown",
		SamplingRate:        1.0,
	}
}

func buildFixed(minSavings float64) *Report {
	r := Build("testdata", ingest.Stats{Files: 1, Calls: 400}, fakeResults(), fakeAssumptions(), minSavings)
	r.GeneratedAt = time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC) // determinism for byte-compares
	return r
}

func TestBuildMarksBelowMinSavings(t *testing.T) {
	r := buildFixed(DefaultMinSavingsUSD)
	want := []bool{false, false, true, true}
	for i, c := range r.Clusters {
		if c.BelowMinSavings != want[i] {
			t.Errorf("cluster %d (%s, $%.0f) below_min_savings = %v, want %v",
				i, c.Label, c.SavingsUSD, c.BelowMinSavings, want[i])
		}
	}
	if got := len(r.MainClusters()); got != 2 {
		t.Errorf("MainClusters = %d, want 2", got)
	}
	if got := len(r.BelowClusters()); got != 2 {
		t.Errorf("BelowClusters = %d, want 2", got)
	}
	if r.MinSavingsUSD != DefaultMinSavingsUSD {
		t.Errorf("MinSavingsUSD = %v, want %v", r.MinSavingsUSD, DefaultMinSavingsUSD)
	}

	// Zero disables folding entirely.
	r0 := buildFixed(0)
	for i, c := range r0.Clusters {
		if c.BelowMinSavings {
			t.Errorf("threshold 0: cluster %d still folded", i)
		}
	}
	if got := len(r0.BelowClusters()); got != 0 {
		t.Errorf("threshold 0: BelowClusters = %d, want 0", got)
	}
}

func TestTerminalFoldsBelowThreshold(t *testing.T) {
	var buf bytes.Buffer
	Terminal(&buf, buildFixed(DefaultMinSavingsUSD))
	out := buf.String()

	head := strings.Index(out, "below min-savings threshold ($100/yr")
	if head < 0 {
		t.Fatalf("terminal output missing below-threshold block:\n%s", out)
	}
	for _, label := range []string{"small-workload", "tiny-workload"} {
		pos := strings.Index(out, label)
		if pos < 0 {
			t.Fatalf("folded cluster %q missing from output", label)
		}
		if pos < head {
			t.Errorf("folded cluster %q rendered before the below-threshold block (in the main table)", label)
		}
	}
	if pos := strings.Index(out, "big-workload"); pos < 0 || pos > head {
		t.Errorf("main cluster big-workload not in the main table above the fold")
	}
	// Ranks continue across the fold: the first folded row is rank 3.
	if !strings.Contains(out, "  3  small-workload") {
		t.Errorf("folded rows do not continue the overall ranking:\n%s", out)
	}
	// The unknown-rights cap wording is surfaced in the assumptions block.
	if !strings.Contains(out, "ready pending rights verification") {
		t.Errorf("assumptions block missing rights-verification wording:\n%s", out)
	}
}

func TestTerminalNoFoldWhenDisabled(t *testing.T) {
	var buf bytes.Buffer
	Terminal(&buf, buildFixed(0))
	out := buf.String()
	if strings.Contains(out, "below min-savings threshold") {
		t.Errorf("threshold 0 must not render a below-threshold block:\n%s", out)
	}
	head := strings.Index(out, "assumptions")
	if pos := strings.Index(out, "tiny-workload"); pos < 0 || pos > head {
		t.Errorf("threshold 0: tiny-workload must sit in the main table")
	}
}

func TestTerminalDeterministic(t *testing.T) {
	var a, b bytes.Buffer
	Terminal(&a, buildFixed(DefaultMinSavingsUSD))
	Terminal(&b, buildFixed(DefaultMinSavingsUSD))
	if a.String() != b.String() {
		t.Error("Terminal output differs across identical builds")
	}
}

func TestJSONCarriesThresholdAndFlags(t *testing.T) {
	dir := t.TempDir()
	path, err := WriteJSON(buildFixed(DefaultMinSavingsUSD), dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		MinSavings float64 `json:"min_savings_threshold_usd"`
		Clusters   []struct {
			Label string `json:"label"`
			Below bool   `json:"below_min_savings"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.MinSavings != DefaultMinSavingsUSD {
		t.Errorf("json min_savings_threshold_usd = %v, want %v", parsed.MinSavings, DefaultMinSavingsUSD)
	}
	if len(parsed.Clusters) != 4 {
		t.Fatalf("json clusters = %d, want all 4 (folding must not drop data)", len(parsed.Clusters))
	}
	if !parsed.Clusters[3].Below || parsed.Clusters[0].Below {
		t.Errorf("json below_min_savings flags wrong: %+v", parsed.Clusters)
	}
}

func TestHTMLFoldsBelowThreshold(t *testing.T) {
	dir := t.TempDir()
	path, err := WriteHTML(buildFixed(DefaultMinSavingsUSD), dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, filepath.Base(path)))
	if err != nil {
		t.Fatal(err)
	}
	html := string(raw)
	fold := strings.Index(html, "Below min-savings threshold")
	if fold < 0 {
		t.Fatal("html missing the below-threshold details section")
	}
	if pos := strings.Index(html, "tiny-workload"); pos < 0 || pos < fold {
		t.Errorf("folded cluster must render only inside the collapsed section (pos=%d fold=%d)", pos, fold)
	}
	if pos := strings.Index(html, "big-workload"); pos < 0 || pos > fold {
		t.Errorf("main cluster must render in the main table before the fold")
	}
	if !strings.Contains(html, "ready pending rights verification") {
		t.Errorf("html assumptions missing rights-verification wording")
	}
	// The fold is a collapsed <details> (no open attribute on it).
	detailsAt := strings.LastIndex(html[:fold], "<details")
	if detailsAt < 0 {
		t.Fatal("below-threshold section is not inside a <details> element")
	}
	if strings.Contains(html[detailsAt:fold], " open") {
		t.Error("below-threshold details must start collapsed")
	}
}
