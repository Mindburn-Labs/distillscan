package score

import (
	"fmt"
	"math"

	"github.com/Mindburn-Labs/distillscan/internal/config"
)

func appendDeclaredFactors(factors []Factor, cfg config.Config) []Factor {
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

	return append(factors, fRights, fSafety)
}

func applyVerdict(r *Result, cfg config.Config) {
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
	// Hard gates. READY requires explicitly declared data rights: "no"
	// forces NOT READY, anything short of "yes" caps at BORDERLINE.
	switch {
	case cfg.DataRights == "no":
		r.Verdict = VerdictNotReady
		r.Notes = append(r.Notes, "gate: data_rights=no forces NOT READY (declared)")
	case cfg.DataRights != "yes" && r.Verdict == VerdictReady:
		r.Verdict = VerdictBorderline
		r.Notes = append(r.Notes, "gate: data_rights=unknown caps at BORDERLINE — ready pending rights verification (declared)")
	}
	if cfg.SafetyCritical && r.Verdict == VerdictReady {
		r.Verdict = VerdictBorderline
		r.Notes = append(r.Notes, "gate: safety_critical=yes caps verdict at BORDERLINE (declared)")
	}
	if r.Calls < LowSampleCalls {
		r.Notes = append(r.Notes, fmt.Sprintf("low sample: %d calls (<%d), treat factors as weak evidence", r.Calls, LowSampleCalls))
	}
	if r.UnpricedCalls > 0 {
		r.Notes = append(r.Notes, fmt.Sprintf("%d call(s) on models missing from the pricing snapshot: excluded from spend", r.UnpricedCalls))
	}
}
