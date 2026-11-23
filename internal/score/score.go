// Package score turns clusters into distillation-readiness verdicts.
//
// Every factor is 0..1 where higher = more distillation-ready, computed by a
// formula recorded in the report. Measured factors come
// from the traces; declared factors (data rights, safety criticality) come
// from distillscan.yaml and are always rendered "declared, not measured".
package score

import (
	"math"
	"sort"
	"time"

	"github.com/Mindburn-Labs/distillscan/internal/cluster"
	"github.com/Mindburn-Labs/distillscan/internal/config"
	"github.com/Mindburn-Labs/distillscan/internal/pricing"
)

// Weights sum to 1.0.
const (
	WSpendShare   = 0.20
	WVolume       = 0.15
	WRepetition   = 0.20
	WEvaluability = 0.15
	WStability    = 0.10
	WEdgeCases    = 0.10
	WDataRights   = 0.05
	WSafety       = 0.05
)

// Verdict thresholds on the composite score.
const (
	ThresholdReady      = 0.65
	ThresholdBorderline = 0.45
)

// Hard gates, applied after the composite:
//   - data_rights: "no"      -> verdict forced to NOT READY (ToS forbids it)
//   - data_rights: "unknown" -> verdict capped at BORDERLINE (READY requires
//     explicitly declared rights; report reads "ready pending rights
//     verification")
//   - safety_critical: yes   -> verdict capped at BORDERLINE
const (
	VerdictReady      = "READY"
	VerdictBorderline = "BORDERLINE"
	VerdictNotReady   = "NOT READY"
)

// LowSampleCalls annotates (does not cap) clusters below this size.
const LowSampleCalls = 20

type Factor struct {
	Name     string  `json:"name"`
	Raw      float64 `json:"raw"`   // the measured quantity (share, rate, distance, count)
	Score    float64 `json:"score"` // 0..1 readiness contribution
	Weight   float64 `json:"weight"`
	Declared bool    `json:"declared"` // true = from config, not measured
	Detail   string  `json:"detail"`   // formula with the actual numbers
}

type Result struct {
	Key            string         `json:"key"`
	Label          string         `json:"label"`
	Kind           string         `json:"kind"`
	Calls          int            `json:"calls"`
	Errors         int            `json:"errors"`
	Models         map[string]int `json:"models"` // model -> call count
	TopModel       string         `json:"top_model"`
	InputTokens    int64          `json:"input_tokens"`
	OutputTokens   int64          `json:"output_tokens"`
	SpendUSD       float64        `json:"spend_usd_window"`
	AnnualizedUSD  float64        `json:"spend_usd_annualized"`
	SavingsUSD     float64        `json:"est_annual_savings_usd"`
	UnpricedCalls  int            `json:"unpriced_calls"`
	Factors        []Factor       `json:"factors"`
	Composite      float64        `json:"composite_score"`
	Verdict        string         `json:"verdict"`
	Notes          []string       `json:"notes"`
	SampleTemplate string         `json:"sample_template"`

	// BelowMinSavings marks clusters whose estimated annual savings fall
	// under the report's min-savings noise threshold. Set at report build
	// time (presentation only — factors and verdict are unaffected).
	BelowMinSavings bool `json:"below_min_savings"`
}

// Assumptions echoes every knob that shaped the numbers.
type Assumptions struct {
	SubstitutionRatio   float64 `json:"substitution_ratio"`
	AnnualizationFactor float64 `json:"annualization_factor"`
	WindowDays          float64 `json:"window_days"`
	PricingSnapshot     string  `json:"pricing_snapshot_date"`
	DataRights          string  `json:"declared_data_rights"`
	SafetyCritical      bool    `json:"declared_safety_critical"`
	SamplingRate        float64 `json:"declared_sampling_rate"`
	ConfigPath          string  `json:"config_path,omitempty"`
}

// Run scores all clusters and ranks them by estimated annual savings.
func Run(clusters []*cluster.Cluster, prices *pricing.Table, cfg config.Config, cfgPath string) ([]Result, Assumptions) {
	// Observation window across the whole dataset (not per cluster): the
	// export is one slice of production time.
	start, end := window(clusters)
	windowDays := math.Max(end.Sub(start).Hours()/24, 1) // clamp: never extrapolate from <1 day as if it were less
	annualFactor := 365.0 / windowDays

	// Declared sampling rate: the export is a sample of production traffic,
	// so extrapolated spend/savings are divided by it. Guard against
	// zero-value configs built outside config.Load.
	samplingRate := cfg.SamplingRate
	if samplingRate <= 0 || samplingRate > 1 {
		samplingRate = 1.0
	}

	totalSpend := 0.0
	spend := make([]float64, len(clusters))
	unpriced := make([]int, len(clusters))
	for i, cl := range clusters {
		s, u := clusterSpend(cl, prices)
		spend[i], unpriced[i] = s, u
		totalSpend += s
	}

	results := make([]Result, 0, len(clusters))
	for i, cl := range clusters {
		results = append(results, scoreCluster(cl, spend[i], unpriced[i], totalSpend, annualFactor, samplingRate, cfg))
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].SavingsUSD != results[j].SavingsUSD {
			return results[i].SavingsUSD > results[j].SavingsUSD
		}
		return results[i].Key < results[j].Key
	})

	return results, Assumptions{
		SubstitutionRatio:   cfg.SubstitutionRatio,
		AnnualizationFactor: annualFactor,
		WindowDays:          windowDays,
		PricingSnapshot:     prices.SnapshotDate,
		DataRights:          cfg.DataRights,
		SafetyCritical:      cfg.SafetyCritical,
		SamplingRate:        samplingRate,
		ConfigPath:          cfgPath,
	}
}

func window(clusters []*cluster.Cluster) (time.Time, time.Time) {
	var start, end time.Time
	for _, cl := range clusters {
		for _, c := range cl.Calls {
			if c.Timestamp.IsZero() {
				continue
			}
			if start.IsZero() || c.Timestamp.Before(start) {
				start = c.Timestamp
			}
			if end.IsZero() || c.Timestamp.After(end) {
				end = c.Timestamp
			}
		}
	}
	return start, end
}

func clusterSpend(cl *cluster.Cluster, prices *pricing.Table) (usd float64, unpriced int) {
	for _, c := range cl.Calls {
		switch {
		case c.HasCost:
			usd += c.CostUSD // trace-carried cost wins
		default:
			if est, ok := prices.Cost(c.Model, c.InputTokens, c.OutputTokens); ok {
				usd += est
			} else {
				unpriced++
			}
		}
	}
	return usd, unpriced
}

func scoreCluster(cl *cluster.Cluster, spendUSD float64, unpricedCalls int, totalSpend, annualFactor, samplingRate float64, cfg config.Config) Result {
	r := Result{
		Key: cl.Key, Label: cl.Label, Kind: cl.Kind,
		Calls: len(cl.Calls), Models: map[string]int{},
		SpendUSD: spendUSD, UnpricedCalls: unpricedCalls,
	}

	r.Factors = make([]Factor, 0, 8)
	measureFactors(cl, &r, totalSpend)
	r.Factors = appendDeclaredFactors(r.Factors, cfg)
	applyVerdict(&r, cfg)

	// Savings: window spend, annualized, scaled up by the declared sampling
	// rate (observed spend is samplingRate of the real thing), times the
	// assumed substitution ratio.
	r.AnnualizedUSD = spendUSD * annualFactor / samplingRate
	r.SavingsUSD = r.AnnualizedUSD * cfg.SubstitutionRatio
	return r
}
