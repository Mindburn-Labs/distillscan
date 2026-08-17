// Package score turns clusters into distillation-readiness verdicts.
//
// Every factor is 0..1 where higher = more distillation-ready, computed by a
// transparent formula kept next to its weight below. Measured factors come
// from the traces; declared factors (data rights, safety criticality) come
// from distillscan.yaml and are always rendered "declared, not measured".
package score

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Mindburn-Labs/distillscan/internal/cluster"
	"github.com/Mindburn-Labs/distillscan/internal/config"
	"github.com/Mindburn-Labs/distillscan/internal/pricing"
	"github.com/Mindburn-Labs/distillscan/internal/trace"
)

// --- Tunables: weights, thresholds, gates (all visible here) ---------------

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
//   - data_rights: "no"    -> verdict forced to NOT READY (ToS forbids it)
//   - safety_critical: yes -> verdict capped at BORDERLINE
const (
	VerdictReady      = "READY"
	VerdictBorderline = "BORDERLINE"
	VerdictNotReady   = "NOT READY"
)

// LowSampleCalls annotates (does not cap) clusters below this size.
const LowSampleCalls = 20

// --- Result model ----------------------------------------------------------

type Factor struct {
	Name     string  `json:"name"`
	Raw      float64 `json:"raw"`      // the measured quantity (share, rate, distance, count)
	Score    float64 `json:"score"`    // 0..1 readiness contribution
	Weight   float64 `json:"weight"`
	Declared bool    `json:"declared"` // true = from config, not measured
	Detail   string  `json:"detail"`   // formula with the actual numbers
}

type Result struct {
	Key           string         `json:"key"`
	Label         string         `json:"label"`
	Kind          string         `json:"kind"`
	Calls         int            `json:"calls"`
	Errors        int            `json:"errors"`
	Models        map[string]int `json:"models"` // model -> call count
	TopModel      string         `json:"top_model"`
	InputTokens   int64          `json:"input_tokens"`
	OutputTokens  int64          `json:"output_tokens"`
	SpendUSD      float64        `json:"spend_usd_window"`
	AnnualizedUSD float64        `json:"spend_usd_annualized"`
	SavingsUSD    float64        `json:"est_annual_savings_usd"`
	UnpricedCalls int            `json:"unpriced_calls"`
	Factors       []Factor       `json:"factors"`
	Composite     float64        `json:"composite_score"`
	Verdict       string         `json:"verdict"`
	Notes         []string       `json:"notes"`
	SampleTemplate string        `json:"sample_template"`
}

// Assumptions echoes every knob that shaped the numbers.
type Assumptions struct {
	SubstitutionRatio   float64 `json:"substitution_ratio"`
	AnnualizationFactor float64 `json:"annualization_factor"`
	WindowDays          float64 `json:"window_days"`
	PricingSnapshot     string  `json:"pricing_snapshot_date"`
	DataRights          string  `json:"declared_data_rights"`
	SafetyCritical      bool    `json:"declared_safety_critical"`
	ConfigPath          string  `json:"config_path,omitempty"`
}

// Run scores all clusters and ranks them by estimated annual savings.
func Run(clusters []*cluster.Cluster, prices *pricing.Table, cfg config.Config, cfgPath string) ([]Result, Assumptions) {
	// Observation window across the whole dataset (not per cluster): the
	// export is one slice of production time.
	start, end := window(clusters)
	windowDays := math.Max(end.Sub(start).Hours()/24, 1) // clamp: never extrapolate from <1 day as if it were less
	annualFactor := 365.0 / windowDays

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
		results = append(results, scoreCluster(cl, spend[i], unpriced[i], totalSpend, annualFactor, cfg))
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

func scoreCluster(cl *cluster.Cluster, spendUSD float64, unpricedCalls int, totalSpend, annualFactor float64, cfg config.Config) Result {
	n := len(cl.Calls)
	r := Result{
		Key: cl.Key, Label: cl.Label, Kind: cl.Kind,
		Calls: n, Models: map[string]int{},
		SpendUSD: spendUSD, UnpricedCalls: unpricedCalls,
	}

	// Bookkeeping shared by several factors.
	fullTemplates := map[string]int{}
	structured, errs := 0, 0
	var inTok, outTok int64
	latencies := make([]float64, 0, n)
	outLens := make([]float64, 0, n)
	for _, c := range cl.Calls {
		r.Models[c.Model]++
		inTok += c.InputTokens
		outTok += c.OutputTokens
		fullTemplates[cluster.FullTemplateHash(c.Prompt)]++
		if c.StructuredOutput {
			structured++
		}
		if c.Error {
			errs++
		}
		if c.LatencyMS > 0 {
			latencies = append(latencies, c.LatencyMS)
		}
		outLens = append(outLens, float64(len(c.Output)))
	}
	r.InputTokens, r.OutputTokens = inTok, outTok
	r.Errors = errs
	r.TopModel = topKey(r.Models)
	r.SampleTemplate = truncate(cluster.Template(cl.Calls[0].Prompt), 220)

	// ---- Measured factors -------------------------------------------------

	// Spend share: what fraction of total observed spend this cluster is.
	// score = min(1, share / 0.30): owning 30%+ of spend maxes the factor.
	share := 0.0
	if totalSpend > 0 {
		share = spendUSD / totalSpend
	}
	fSpend := Factor{
		Name: "spend_share", Raw: share, Weight: WSpendShare,
		Score:  clamp01(share / 0.30),
		Detail: fmt.Sprintf("min(1, %.1f%%/30%%) of total observed spend", share*100),
	}

	// Volume: log-scaled call count; 1000+ calls in window maxes it.
	// score = min(1, log10(1+n)/3)
	fVolume := Factor{
		Name: "volume", Raw: float64(n), Weight: WVolume,
		Score:  clamp01(math.Log10(1+float64(n)) / 3),
		Detail: fmt.Sprintf("min(1, log10(1+%d)/3); 1000 calls => 1.0", n),
	}

	// Repetition: how template-alike the prompts are.
	// distinct = full-template variants; score = 1 - (distinct-1)/(n-1).
	distinct := len(fullTemplates)
	fRep := Factor{Name: "repetition", Raw: float64(distinct), Weight: WRepetition}
	if n < 3 {
		fRep.Score = 0.5
		fRep.Detail = fmt.Sprintf("neutral 0.5: only %d call(s), no repetition evidence", n)
	} else {
		fRep.Score = clamp01(1 - float64(distinct-1)/float64(n-1))
		fRep.Detail = fmt.Sprintf("1 - (%d distinct templates - 1)/(%d calls - 1)", distinct, n)
	}

	// Evaluability proxy: share of structured (JSON / tool-call) outputs —
	// those are cheap to verify a student against.
	evalShare := float64(structured) / float64(maxInt(n, 1))
	fEval := Factor{
		Name: "evaluability", Raw: evalShare, Weight: WEvaluability,
		Score:  evalShare,
		Detail: fmt.Sprintf("%d/%d outputs are JSON or tool calls", structured, n),
	}

	// Stability (1 - drift): early-vs-late template distribution shift as
	// total-variation distance. drift 0 => stable task; drift 1 => changed.
	drift, driftOK := templateDrift(cl)
	fStab := Factor{Name: "stability", Raw: drift, Weight: WStability}
	if !driftOK {
		fStab.Score = 0.5
		fStab.Detail = fmt.Sprintf("neutral 0.5: <%d calls or no timestamps, drift not measurable", minDriftCalls)
	} else {
		fStab.Score = clamp01(1 - drift)
		fStab.Detail = fmt.Sprintf("1 - TV(early,late template dist) = 1 - %.2f", drift)
	}

	// Edge cases: union share of error calls, latency outliers (>3x median)
	// and output-length outliers (>3x or <0.2x median).
	// score = 1 - min(1, rate/0.25): a 25% edge rate zeroes the factor.
	edgeRate := edgeCaseRate(cl.Calls, latencies, outLens)
	fEdge := Factor{
		Name: "edge_cases", Raw: edgeRate, Weight: WEdgeCases,
		Score:  clamp01(1 - edgeRate/0.25),
		Detail: fmt.Sprintf("1 - min(1, %.1f%%/25%%) outlier/error calls", edgeRate*100),
	}

	// ---- Declared factors (from config, not measured) ---------------------

	var drScore float64
	switch cfg.DataRights {
	case "yes":
		drScore = 1.0
	case "no":
		drScore = 0.0
	default:
		drScore = 0.5
	}
	fRights := Factor{
		Name: "data_rights", Raw: drScore, Weight: WDataRights, Declared: true,
		Score:  drScore,
		Detail: fmt.Sprintf("declared, not measured: teacher ToS allows training on outputs = %q", cfg.DataRights),
	}
	safScore := 1.0
	if cfg.SafetyCritical {
		safScore = 0.2
	}
	fSafety := Factor{
		Name: "safety_critical", Raw: safScore, Weight: WSafety, Declared: true,
		Score:  safScore,
		Detail: fmt.Sprintf("declared, not measured: safety_critical = %v", cfg.SafetyCritical),
	}

	r.Factors = []Factor{fSpend, fVolume, fRep, fEval, fStab, fEdge, fRights, fSafety}

	// ---- Composite + verdict ---------------------------------------------

	for _, f := range r.Factors {
		r.Composite += f.Score * f.Weight
	}
	r.Composite = math.Round(r.Composite*1000) / 1000

	switch {
	case r.Composite >= ThresholdReady:
		r.Verdict = VerdictReady
	case r.Composite >= ThresholdBorderline:
		r.Verdict = VerdictBorderline
	default:
		r.Verdict = VerdictNotReady
	}
	// Hard gates.
	if cfg.DataRights == "no" {
		r.Verdict = VerdictNotReady
		r.Notes = append(r.Notes, "gate: data_rights=no forces NOT READY (declared)")
	} else if cfg.SafetyCritical && r.Verdict == VerdictReady {
		r.Verdict = VerdictBorderline
		r.Notes = append(r.Notes, "gate: safety_critical=yes caps verdict at BORDERLINE (declared)")
	}
	if n < LowSampleCalls {
		r.Notes = append(r.Notes, fmt.Sprintf("low sample: %d calls (<%d), treat factors as weak evidence", n, LowSampleCalls))
	}
	if unpricedCalls > 0 {
		r.Notes = append(r.Notes, fmt.Sprintf("%d call(s) on models missing from the pricing snapshot: excluded from spend", unpricedCalls))
	}

	// Savings: window spend, annualized, times the assumed substitution ratio.
	r.AnnualizedUSD = spendUSD * annualFactor
	r.SavingsUSD = r.AnnualizedUSD * cfg.SubstitutionRatio
	return r
}

// --- helpers ---------------------------------------------------------------

const minDriftCalls = 8

// templateDrift splits the cluster's calls (already time-sorted) in half and
// returns the total-variation distance between the two halves' full-template
// distributions.
func templateDrift(cl *cluster.Cluster) (float64, bool) {
	n := len(cl.Calls)
	if n < minDriftCalls {
		return 0, false
	}
	timestamped := 0
	for _, c := range cl.Calls {
		if !c.Timestamp.IsZero() {
			timestamped++
		}
	}
	if timestamped < n/2 {
		return 0, false
	}
	half := n / 2
	early, late := map[string]float64{}, map[string]float64{}
	for i, c := range cl.Calls {
		h := cluster.FullTemplateHash(c.Prompt)
		if i < half {
			early[h]++
		} else {
			late[h]++
		}
	}
	nEarly, nLate := float64(half), float64(n-half)
	keys := map[string]bool{}
	for k := range early {
		keys[k] = true
	}
	for k := range late {
		keys[k] = true
	}
	tv := 0.0
	for k := range keys {
		tv += math.Abs(early[k]/nEarly - late[k]/nLate)
	}
	return tv / 2, true
}

func edgeCaseRate(calls []trace.Call, latencies, outLens []float64) float64 {
	if len(calls) == 0 {
		return 0
	}
	medLat := median(latencies)
	medLen := median(outLens)
	edge := 0
	for _, c := range calls {
		isEdge := c.Error
		if !isEdge && medLat > 0 && c.LatencyMS > 3*medLat {
			isEdge = true
		}
		if !isEdge && medLen > 0 {
			l := float64(len(c.Output))
			if l > 3*medLen || l < 0.2*medLen {
				isEdge = true
			}
		}
		if isEdge {
			edge++
		}
	}
	return float64(edge) / float64(len(calls))
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	m := len(s) / 2
	if len(s)%2 == 1 {
		return s[m]
	}
	return (s[m-1] + s[m]) / 2
}

func topKey(m map[string]int) string {
	best, bestN := "", -1
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if m[k] > bestN {
			best, bestN = k, m[k]
		}
	}
	return best
}

func clamp01(x float64) float64 { return math.Max(0, math.Min(1, x)) }

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n]) + "…"
}
