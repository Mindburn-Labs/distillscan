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
	Totals      Totals             `json:"totals"`
	Clusters    []score.Result     `json:"clusters"`
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

// Build assembles the report struct shared by all renderers.
func Build(scanned string, stats ingest.Stats, results []score.Result, assum score.Assumptions) *Report {
	t := Totals{}
	for _, r := range results {
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
		Totals:   t,
		Clusters: results,
	}
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

	tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "#\tCLUSTER\tCALLS\tTOP MODEL\tSPEND(WIN)\tEST.ANNUAL SAVINGS\tSCORE\tVERDICT")
	for i, c := range r.Clusters {
		fmt.Fprintf(tw, "%d\t%s\t%d\t%s\t%s\t%s\t%.2f\t%s\n",
			i+1, clip(c.Label, 46), c.Calls, c.TopModel,
			usd(c.SpendUSD), usd(c.SavingsUSD), c.Composite, c.Verdict)
	}
	tw.Flush()

	fmt.Fprintf(w, "\nassumptions (printed, not hidden):\n")
	fmt.Fprintf(w, "  - savings = window spend x %.2f annualization x %.0f%% assumed substitution ratio\n",
		r.Assumptions.AnnualizationFactor, r.Assumptions.SubstitutionRatio*100)
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
