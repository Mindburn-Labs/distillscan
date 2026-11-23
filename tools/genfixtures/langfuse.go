package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"time"
)

var contractParties = []string{
	"Aldervane Holdings", "Brightmoor Capital", "Cindralux Energy", "Dovetail Robotics",
	"Eastgate Maritime", "Ferrowell Mining", "Glasswing Health", "Hollowbrook Estates",
	"Ironquay Logistics", "Juniper Cloudworks", "Kilnhurst Foundry", "Larkspur Biotech",
}

var productBlurbs = []string{
	"waterproof trail shoe with recycled mesh upper",
	"cold-brew maker with slow-drip valve",
	"modular standing desk in oak veneer",
	"noise-isolating earbuds with wireless case",
	"cast-iron skillet, pre-seasoned, 28 cm",
	"merino base layer for alpine touring",
	"smart thermostat with local-only mode",
	"packable down jacket, 650 fill",
	"stainless water bottle, 1 litre, insulated",
	"ergonomic vertical mouse for large hands",
	"linen duvet cover set, stonewashed",
	"solar lantern with USB-C out",
	"ceramic pour-over dripper, size 02",
	"trail running vest with soft flasks",
	"beechwood knife block with magnetic strip",
}

const opusPromptHead = "Summarize the following commercial agreement for the deal desk. Cover parties, term, " +
	"renewal and termination mechanics, liability caps, exclusivity, and any unusual indemnities. Write five " +
	"tight paragraphs a lawyer can skim; do not quote clauses verbatim. Agreement text (truncated) follows."

func langfuseLines(r *rand.Rand) [][]byte {
	var lines [][]byte
	const opusIn, opusOut = 15.0, 75.0 // USD per 1M tokens, matches the bundled snapshot

	// contract-summarize on claude-opus-4: the trace carries its own cost.
	const nSum = 700
	tsSum := timeline(r, nSum)
	for i := 0; i < nSum; i++ {
		a := contractParties[r.Intn(len(contractParties))]
		b := contractParties[r.Intn(len(contractParties))]
		inTok := 11000 + int64(r.Intn(6000))
		outTok := 1200 + int64(r.Intn(1000))
		costIn := float64(inTok) / 1e6 * opusIn
		costOut := float64(outTok) / 1e6 * opusOut
		model := "claude-opus-4"
		if i%3 == 0 {
			model = "claude-opus-4-20250514" // dated spelling appears in real exports
		}
		level := "DEFAULT"
		if r.Float64() < 0.02 {
			level = "ERROR"
		}
		start := tsSum[i]
		end := start.Add(time.Duration(6000+r.Intn(7000)) * time.Millisecond)

		line := map[string]any{
			"id":        fmt.Sprintf("obs-sum-%04d", i),
			"traceId":   fmt.Sprintf("tr-%08x", r.Uint32()),
			"type":      "GENERATION",
			"name":      "contract-summarize",
			"model":     model,
			"startTime": start.Format("2006-01-02T15:04:05.000Z"),
			"endTime":   end.Format("2006-01-02T15:04:05.000Z"),
			"input": []map[string]string{
				{"role": "system", "content": opusPromptHead},
				{"role": "user", "content": fmt.Sprintf("MASTER SERVICES AGREEMENT between %s and %s, dated %s ...[truncated]", a, b, start.AddDate(0, -r.Intn(18), 0).Format("2006-01-02"))},
			},
			"output": map[string]string{"role": "assistant", "content": fmt.Sprintf(
				"The agreement appoints %s as service provider to %s for an initial three-year term with automatic renewal unless either party gives 90 days notice. ...", a, b)},
			"usageDetails": map[string]int64{"input": inTok, "output": outTok, "total": inTok + outTok},
			"level":        level,
		}
		if level == "ERROR" {
			line["statusMessage"] = "upstream timeout"
			line["output"] = nil
		}
		if i%2 == 0 { // both cost spellings appear in the wild
			line["costDetails"] = map[string]float64{"input": round6(costIn), "output": round6(costOut), "total": round6(costIn + costOut)}
		} else {
			line["calculatedTotalCost"] = round6(costIn + costOut)
		}
		lines = append(lines, marshal(line))
	}

	// product-translate on deepseek-chat: no cost in trace -> bundled pricing.
	const nTr = 380
	tsTr := timeline(r, nTr)
	for i := 0; i < nTr; i++ {
		blurb := productBlurbs[r.Intn(len(productBlurbs))]
		start := tsTr[i]
		line := map[string]any{
			"id":               fmt.Sprintf("obs-tr-%04d", i),
			"traceId":          fmt.Sprintf("tr-%08x", r.Uint32()),
			"type":             "GENERATION",
			"name":             "product-translate",
			"model":            "deepseek-chat",
			"startTime":        start.Format("2006-01-02T15:04:05.000Z"),
			"endTime":          start.Add(time.Duration(600+r.Intn(900)) * time.Millisecond).Format("2006-01-02T15:04:05.000Z"),
			"input":            "Translate this product description to French, keep brand names in English: " + blurb,
			"output":           "Traduction: " + blurb + " (fr)",
			"promptTokens":     1500 + r.Intn(700), // legacy token spelling
			"completionTokens": 300 + r.Intn(250),
			"level":            "DEFAULT",
		}
		lines = append(lines, marshal(line))
	}

	// Non-generation lines: the parser must skip them.
	for i := 0; i < 6; i++ {
		lines = append(lines, marshal(map[string]any{
			"id":        fmt.Sprintf("obs-span-%02d", i),
			"traceId":   fmt.Sprintf("tr-%08x", r.Uint32()),
			"type":      "SPAN",
			"name":      "pipeline-step",
			"startTime": windowStart.Add(time.Duration(i) * time.Hour).Format("2006-01-02T15:04:05.000Z"),
		}))
	}
	return lines
}

func marshal(v any) []byte {
	raw, err := json.Marshal(v)
	must(err)
	return raw
}

func round6(v float64) float64 {
	return float64(int64(v*1e6+0.5)) / 1e6
}
