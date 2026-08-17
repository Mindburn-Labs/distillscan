// Package report renders scan results three ways: terminal summary,
// report.json (machine-readable), report.html (single self-contained file).
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/Mindburn-Labs/distillscan/internal/ingest"
	"github.com/Mindburn-Labs/distillscan/internal/score"
)

const Version = "0.1.0-prototype"

// DefaultMinSavingsUSD is the default noise threshold: clusters whose
// estimated annual savings fall below it are folded into a collapsed
// "below threshold" section instead of the main table. Presentation only —
// scores and verdicts are unaffected. Override with -min-savings (0 disables).
const DefaultMinSavingsUSD = 100.0

// Report is the full machine-readable output (report.json).
type Report struct {
	Tool        string             `json:"tool"`
	Version     string             `json:"version"`
	GeneratedAt time.Time          `json:"generated_at"`
	ScannedPath string             `json:"scanned_path"`
	Ingest      IngestSummary      `json:"ingest"`
	Assumptions score.Assumptions  `json:"assumptions"`
	Weights     map[string]float64 `json:"weights"`
	Thresholds  map[string]float64 `json:"verdict_thresholds"`
	// MinSavingsUSD is the noise threshold applied at render time; clusters
	// below it carry below_min_savings=true but stay in Clusters (ranked).
	MinSavingsUSD float64        `json:"min_savings_threshold_usd"`
	Totals        Totals         `json:"totals"`
	Clusters      []score.Result `json:"clusters"`
}

type IngestSummary struct {
	Files        int      `json:"files"`
	Calls        int      `json:"calls"`
	SkippedSpans int      `json:"skipped_records"`
	Errors       []string `json:"errors,omitempty"`
}

type Totals struct {
	SpendUSD      float64 `json:"spend_usd_window"`
	AnnualizedUSD float64 `json:"spend_usd_annualized"`
	SavingsUSD    float64 `json:"est_annual_savings_usd"`
	UnpricedCalls int     `json:"unpriced_calls"`
}

// Build assembles the report struct shared by all renderers. minSavings is
// the noise threshold in USD/yr (0 disables folding); results are already
// ranked by savings, so below-threshold clusters are the tail of the list.
func Build(scanned string, stats ingest.Stats, results []score.Result, assum score.Assumptions, minSavings float64) *Report {
	if minSavings < 0 {
		minSavings = 0
	}
	t := Totals{}
	for i := range results {
		r := &results[i]
		r.BelowMinSavings = minSavings > 0 && r.SavingsUSD < minSavings
		t.SpendUSD += r.SpendUSD
		t.AnnualizedUSD += r.AnnualizedUSD
		t.SavingsUSD += r.SavingsUSD
		t.UnpricedCalls += r.UnpricedCalls
	}
	return &Report{
		Tool:        "distillscan",
		Version:     Version,
		GeneratedAt: time.Now().UTC(),
		ScannedPath: scanned,
		Ingest: IngestSummary{
			Files: stats.Files, Calls: stats.Calls,
			SkippedSpans: stats.SkippedSpans, Errors: stats.Errors,
		},
		Assumptions: assum,
		Weights: map[string]float64{
			"spend_share":     score.WSpendShare,
			"volume":          score.WVolume,
			"repetition":      score.WRepetition,
			"evaluability":    score.WEvaluability,
			"stability":       score.WStability,
			"edge_cases":      score.WEdgeCases,
			"data_rights":     score.WDataRights,
			"safety_critical": score.WSafety,
		},
		Thresholds: map[string]float64{
			"ready":      score.ThresholdReady,
			"borderline": score.ThresholdBorderline,
		},
		MinSavingsUSD: minSavings,
		Totals:        t,
		Clusters:      results,
	}
}

// MainClusters returns the clusters at or above the min-savings threshold,
// in rank order. BelowClusters returns the folded tail. Rank numbers are
// positions in the full ranked list, shared by all renderers and the JSON.
func (r *Report) MainClusters() []score.Result {
	out := make([]score.Result, 0, len(r.Clusters))
	for _, c := range r.Clusters {
		if !c.BelowMinSavings {
			out = append(out, c)
		}
	}
	return out
}

// BelowClusters returns the clusters folded under the min-savings threshold.
func (r *Report) BelowClusters() []score.Result {
	out := []score.Result{}
	for _, c := range r.Clusters {
		if c.BelowMinSavings {
			out = append(out, c)
		}
	}
	return out
}

// WriteJSON writes report.json into dir.
func WriteJSON(r *Report, dir string) (string, error) {
	path := filepath.Join(dir, "report.json")
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, append(raw, '\n'), 0o644)
}

// Terminal prints the ranked summary table plus the assumption block.
func Terminal(w io.Writer, r *Report) {
	fmt.Fprintf(w, "\ndistillscan %s — offline trace scan\n", r.Version)
	fmt.Fprintf(w, "scanned %s: %d file(s), %d LLM calls (%d non-GenAI records skipped)\n",
		r.ScannedPath, r.Ingest.Files, r.Ingest.Calls, r.Ingest.SkippedSpans)
	for _, e := range r.Ingest.Errors {
		fmt.Fprintf(w, "  warning: %s\n", e)
	}
	fmt.Fprintf(w, "observed window: %.1f days | spend in window: %s | annualized: %s\n\n",
		r.Assumptions.WindowDays, usd(r.Totals.SpendUSD), usd(r.Totals.AnnualizedUSD))

	main := r.MainClusters()
	below := r.BelowClusters()

	tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "#\tCLUSTER\tCALLS\tTOP MODEL\tSPEND(WIN)\tEST.ANNUAL SAVINGS\tSCORE\tVERDICT")
	for i, c := range main {
		fmt.Fprintf(tw, "%d\t%s\t%d\t%s\t%s\t%s\t%.2f\t%s\n",
			i+1, clip(c.Label, 46), c.Calls, c.TopModel,
			usd(c.SpendUSD), usd(c.SavingsUSD), c.Composite, c.Verdict)
	}
	tw.Flush()
	if len(main) == 0 {
		fmt.Fprintf(w, "(every cluster is below the %s/yr min-savings threshold — see the block below)\n", usd(r.MinSavingsUSD))
	}

	if len(below) > 0 {
		combined := 0.0
		for _, c := range below {
			combined += c.SavingsUSD
		}
		fmt.Fprintf(w, "\nbelow min-savings threshold (%s/yr; -min-savings to change): %d cluster(s), %s combined est. savings\n",
			usd(r.MinSavingsUSD), len(below), usd(combined))
		tb := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
		for i, c := range below {
			fmt.Fprintf(tb, "  %d\t%s\t%d calls\t%s\t%.2f\t%s\n",
				len(main)+i+1, clip(c.Label, 46), c.Calls, usd(c.SavingsUSD), c.Composite, c.Verdict)
		}
		tb.Flush()
	}

	fmt.Fprintf(w, "\nassumptions (printed, not hidden):\n")
	if r.Assumptions.SamplingRate != 1 {
		fmt.Fprintf(w, "  - savings = window spend x %.2f annualization / %.2f declared sampling rate x %.0f%% assumed substitution ratio\n",
			r.Assumptions.AnnualizationFactor, r.Assumptions.SamplingRate, r.Assumptions.SubstitutionRatio*100)
		fmt.Fprintf(w, "  - sampling_rate=%.2f is declared, not measured: the export is treated as that share of real traffic\n",
			r.Assumptions.SamplingRate)
	} else {
		fmt.Fprintf(w, "  - savings = window spend x %.2f annualization x %.0f%% assumed substitution ratio\n",
			r.Assumptions.AnnualizationFactor, r.Assumptions.SubstitutionRatio*100)
	}
	fmt.Fprintf(w, "  - prices: bundled snapshot %s (traces with their own cost use it instead)\n", r.Assumptions.PricingSnapshot)
	fmt.Fprintf(w, "  - declared, not measured: data_rights=%s, safety_critical=%v",
		r.Assumptions.DataRights, r.Assumptions.SafetyCritical)
	if r.Assumptions.ConfigPath != "" {
		fmt.Fprintf(w, " (from %s)", r.Assumptions.ConfigPath)
	} else {
		fmt.Fprintf(w, " (defaults; no distillscan.yaml found)")
	}
	fmt.Fprintln(w)
	if r.Totals.UnpricedCalls > 0 {
		fmt.Fprintf(w, "  - %d call(s) on unknown models carry $0 in these numbers\n", r.Totals.UnpricedCalls)
	}
	fmt.Fprintf(w, "  - verdicts: READY >= %.2f, BORDERLINE >= %.2f, else NOT READY\n",
		score.ThresholdReady, score.ThresholdBorderline)
	switch r.Assumptions.DataRights {
	case "yes", "no":
	default:
		fmt.Fprintf(w, "  - data_rights=%s: READY capped at BORDERLINE — ready pending rights verification\n",
			r.Assumptions.DataRights)
	}
}

func usd(v float64) string {
	switch {
	case v >= 1000:
		return fmt.Sprintf("$%s", comma(int64(v+0.5)))
	case v >= 1:
		return fmt.Sprintf("$%.0f", v)
	default:
		return fmt.Sprintf("$%.2f", v)
	}
}

func comma(n int64) string {
	s := fmt.Sprintf("%d", n)
	out := ""
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out += ","
		}
		out += string(c)
	}
	return out
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
