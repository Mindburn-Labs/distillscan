package score

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/Mindburn-Labs/distillscan/internal/cluster"
	"github.com/Mindburn-Labs/distillscan/internal/trace"
)

func measureFactors(cl *cluster.Cluster, r *Result, totalSpend float64) {
	n := r.Calls
	prefixTemplates := map[string]int{}
	structured, errs := 0, 0
	var inTok, outTok int64
	latencies := make([]float64, 0, n)
	outLens := make([]float64, 0, n)
	for _, c := range cl.Calls {
		r.Models[c.Model]++
		inTok += c.InputTokens
		outTok += c.OutputTokens
		prefixTemplates[cluster.PrefixTemplateHash(c.Prompt)]++
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

	// Spend share: what fraction of total observed spend this cluster is.
	// score = min(1, share / 0.30): owning 30%+ of spend maxes the factor.
	share := 0.0
	if totalSpend > 0 {
		share = r.SpendUSD / totalSpend
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

	// Repetition: instruction uniformity. distinct = template-prefix
	// variants (payload variety after the prefix is fine for distillation;
	// instruction churn is not). score = 1 - (distinct-1)/(n-1).
	distinct := len(prefixTemplates)
	fRep := Factor{Name: "repetition", Raw: float64(distinct), Weight: WRepetition}
	if n < 3 {
		fRep.Score = 0.5
		fRep.Detail = fmt.Sprintf("neutral 0.5: only %d call(s), no repetition evidence", n)
	} else {
		fRep.Score = clamp01(1 - float64(distinct-1)/float64(n-1))
		fRep.Detail = fmt.Sprintf("1 - (%d distinct template prefixes - 1)/(%d calls - 1)", distinct, n)
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
	r.Factors = append(r.Factors, fSpend, fVolume, fRep, fEval, fStab, fEdge)
}

const minDriftCalls = 8

// templateDrift splits the cluster's calls (already time-sorted) in half and
// returns the total-variation distance between the two halves' template-prefix
// distributions. Catches instruction/template revisions inside a task cluster.
// Known limitation: template-keyed clusters share one prefix by construction,
// so their drift is ~0 — drift detection is only meaningful where a task
// label groups heterogeneous prompts.
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
		h := cluster.PrefixTemplateHash(c.Prompt)
		if i < half {
			early[h]++
		} else {
			late[h]++
		}
	}
	nEarly, nLate := float64(half), float64(n-half)
	keySet := map[string]bool{}
	for k := range early {
		keySet[k] = true
	}
	for k := range late {
		keySet[k] = true
	}
	// Sorted iteration: float summation order must not depend on Go's
	// randomized map order, or report.json stops being byte-reproducible.
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	tv := 0.0
	for _, k := range keys {
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
