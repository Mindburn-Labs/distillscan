// genfixtures deterministically (re)generates the sample traces under
// fixtures/. Fixed seed, fixed time window — running it twice produces
// identical files. The data is synthetic but shaped like real exports:
//
//	otlp/invoice-extraction.json  legacy GenAI attrs (gen_ai.system,
//	                              prompt/completion_tokens), gpt-4o,
//	                              templated extraction, JSON outputs
//	otlp/support-triage.json      current attrs (provider.name,
//	                              input/output_tokens, input/output messages),
//	                              gpt-4o-mini, huge volume, cheap; the
//	                              instruction is revised mid-window (drift)
//	otlp/agent-tools.json         OpenLLMetry indexed prompts, claude-sonnet-4
//	                              tool-calling agent loop: heterogeneous
//	                              prompts, tool_calls outputs, errors,
//	                              latency outliers
//	otlp/chat-mixed.json          current attrs, NO task attribute ->
//	                              exercises template clustering; two prompt
//	                              families; a few calls on an unpriced model
//	langfuse/observations.jsonl   observations_v2 export: contract-summarize
//	                              on claude-opus-4 (costs carried in-trace),
//	                              product-translate on deepseek-chat (priced
//	                              from the bundled table); non-generation
//	                              lines mixed in
//
// Usage: go run ./tools/genfixtures [-out fixtures]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var (
	windowStart = time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	windowEnd   = time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
)

func main() {
	out := flag.String("out", "fixtures", "output directory")
	flag.Parse()
	r := rand.New(rand.NewSource(42)) // fixed seed: fixtures are reproducible

	must(os.MkdirAll(filepath.Join(*out, "otlp"), 0o755))
	must(os.MkdirAll(filepath.Join(*out, "langfuse"), 0o755))

	writeJSON(filepath.Join(*out, "otlp", "invoice-extraction.json"), invoiceFile(r))
	writeJSON(filepath.Join(*out, "otlp", "support-triage.json"), triageFile(r))
	writeJSON(filepath.Join(*out, "otlp", "agent-tools.json"), agentFile(r))
	writeJSON(filepath.Join(*out, "otlp", "chat-mixed.json"), chatFile(r))
	writeLines(filepath.Join(*out, "langfuse", "observations.jsonl"), langfuseLines(r))

	must(os.WriteFile(filepath.Join(*out, "distillscan.yaml"), []byte(demoConfig), 0o644))
	fmt.Println("fixtures written to", *out)
}

const demoConfig = `# Demo declared factors for the fixture scan.
# These are folded into scoring but always rendered "declared, not measured".
# The traces here are synthetic, so rights are declared "yes" to show the full
# verdict range; with the default "unknown", READY is capped at BORDERLINE
# (ready pending rights verification).
data_rights: "yes"      # yes | no | unknown - teacher ToS allows training on outputs
safety_critical: false  # true caps verdicts at BORDERLINE; see internal/score
substitution_ratio: 0.65
sampling_rate: 1.0      # declared share of real traffic in this export; <1 scales extrapolation up
`

// --- OTLP building blocks --------------------------------------------------

func kvStr(k, v string) map[string]any {
	return map[string]any{"key": k, "value": map[string]any{"stringValue": v}}
}

// kvIntStr encodes int64 the protojson way (string) — most real exporters.
func kvIntStr(k string, n int64) map[string]any {
	return map[string]any{"key": k, "value": map[string]any{"intValue": strconv.FormatInt(n, 10)}}
}

// kvIntNum encodes int64 as a bare number — some hand-rolled exporters.
func kvIntNum(k string, n int64) map[string]any {
	return map[string]any{"key": k, "value": map[string]any{"intValue": n}}
}

func otlpDoc(service string, spans []map[string]any) map[string]any {
	return map[string]any{
		"resourceSpans": []any{map[string]any{
			"resource": map[string]any{"attributes": []any{kvStr("service.name", service)}},
			"scopeSpans": []any{map[string]any{
				"scope": map[string]any{"name": "distillscan.fixtures", "version": "1.0.0"},
				"spans": spans,
			}},
		}},
	}
}

func mkSpan(r *rand.Rand, name string, start time.Time, durMS float64, attrs []map[string]any, isErr bool) map[string]any {
	code := "STATUS_CODE_UNSET"
	if isErr {
		code = "STATUS_CODE_ERROR"
	}
	return map[string]any{
		"traceId":           fmt.Sprintf("%016x%016x", r.Uint64(), r.Uint64()),
		"spanId":            fmt.Sprintf("%016x", r.Uint64()),
		"name":              name,
		"kind":              "SPAN_KIND_CLIENT",
		"startTimeUnixNano": strconv.FormatInt(start.UnixNano(), 10),
		"endTimeUnixNano":   strconv.FormatInt(start.Add(time.Duration(durMS*float64(time.Millisecond))).UnixNano(), 10),
		"attributes":        attrs,
		"status":            map[string]any{"code": code},
	}
}

// timeline spreads n timestamps across the window with jitter, sorted.
func timeline(r *rand.Rand, n int) []time.Time {
	span := windowEnd.Sub(windowStart)
	step := span / time.Duration(n)
	ts := make([]time.Time, n)
	for i := range ts {
		jitter := time.Duration((r.Float64() - 0.5) * float64(step) * 0.8)
		ts[i] = windowStart.Add(step*time.Duration(i) + jitter)
	}
	return ts
}

// --- fixture: invoice extraction (legacy attribute generation) -------------

var companies = []string{
	"Meridian Logistics BV", "Northwind Traders GmbH", "Alpenglow Consulting AG",
	"Baltic Freight Partners", "Cobalt Systems Ltd", "Delta Print Services",
	"Everfield Catering", "Fjordline Supplies AS", "Granite Office Group",
	"Harbor Light Media", "Ionic Materials SA", "Juniper Cloudworks",
	"Kestrel Security BV", "Lumen Facility Care", "Mistral Packaging",
	"Nordica Rentals", "Opaline Labs", "Pinewheel Transport", "Quarry Steelworks",
	"Riverstone Legal",
}

func invoiceFile(r *rand.Rand) map[string]any {
	const n = 1100
	ts := timeline(r, n)
	spans := make([]map[string]any, 0, n+10)
	for i := 0; i < n; i++ {
		company := companies[r.Intn(len(companies))]
		invNo := 1000 + r.Intn(9000)
		day := windowStart.AddDate(0, 0, -r.Intn(40))
		amount := fmt.Sprintf("%d.%02d", 200+r.Intn(24000), r.Intn(100))
		email := strings.ToLower(strings.Split(company, " ")[0]) + "-billing@example-vendor.com"
		isErr := r.Float64() < 0.015

		prompt := fmt.Sprintf(
			"Extract the following fields from the invoice below. Return strict JSON with keys "+
				"vendor, invoice_number, date, currency, total. Amounts must be plain numbers and "+
				"dates ISO 8601. Do not add commentary.\n\nINVOICE\nInvoice no: INV-%d\nDate: %s\n"+
				"Vendor: %s\nContact: %s\nTotal due: EUR %s\nPayment terms: net %d days\n",
			invNo, day.Format("2006-01-02"), company, email, amount, 15+r.Intn(30)*15)
		output := fmt.Sprintf(`{"vendor":%q,"invoice_number":"INV-%d","date":%q,"currency":"EUR","total":%s}`,
			company, invNo, day.Format("2006-01-02"), amount)
		if isErr {
			output = ""
		}

		attrs := []map[string]any{
			kvStr("gen_ai.system", "openai"),                                 // legacy provider spelling
			kvStr("gen_ai.request.model", "gpt-4o-2024-08-06"),               // dated model name
			kvIntStr("gen_ai.usage.prompt_tokens", 9000+int64(r.Intn(4000))), // legacy usage keys
			kvIntStr("gen_ai.usage.completion_tokens", 450+int64(r.Intn(300))),
			kvStr("app.task", "invoice-extraction"),
			kvStr("gen_ai.prompt", prompt),
			kvStr("gen_ai.completion", output),
		}
		if isErr {
			attrs = append(attrs, kvStr("error.type", "server_error"))
		}
		spans = append(spans, mkSpan(r, "chat gpt-4o", ts[i], 2000+r.Float64()*2500, attrs, isErr))
	}
	// A few non-GenAI spans: the scanner must skip these, not count them.
	for i := 0; i < 10; i++ {
		spans = append(spans, mkSpan(r, "GET /api/invoices", ts[i*100], 40+r.Float64()*80, []map[string]any{
			kvStr("http.method", "GET"),
			kvStr("http.route", "/api/invoices"),
			kvIntStr("http.status_code", 200),
		}, false))
	}
	return otlpDoc("billing-pipeline", spans)
}

// --- fixture: support triage (current attribute generation) ----------------

var tickets = []string{
	"my card was charged twice for the same month",
	"cannot reset my password, the email never arrives",
	"the export to CSV times out on large workspaces",
	"how do I add a teammate with read-only access",
	"invoice PDF shows the wrong company address",
	"API returns 429 even though we are under the limit",
	"dark mode flickers when switching tabs",
	"webhook deliveries stopped after we rotated the secret",
	"need SOC 2 report for our procurement team",
	"the mobile app logs me out every hour",
	"data residency options for EU customers",
	"upgrade from starter to team lost our saved views",
	"SSO login loops back to the sign-in page",
	"can I get a refund for unused seats",
	"search does not find archived projects",
	"rate limits documentation contradicts the response headers",
	"attachment upload fails for files over 20 MB",
	"how to export audit logs automatically",
	"billing email should go to accounting, not the owner",
	"two-factor codes rejected after phone migration",
}

const triageInstrV1 = "Classify the customer ticket into one of: billing, auth, api, bug, feature_request, compliance. " +
	"Also rate urgency 1-5. Respond as JSON {\"label\": ..., \"urgency\": ...} with no extra text. Ticket follows."
const triageInstrV2 = "Classify the customer ticket into one of: billing, auth, api, bug, feature_request, compliance, escalation. " +
	"Also rate urgency 1-5. Respond as JSON {\"label\": ..., \"urgency\": ...} with no extra text. Ticket follows."

var triageLabels = []string{"billing", "auth", "api", "bug", "feature_request", "compliance"}

func triageFile(r *rand.Rand) map[string]any {
	const n = 1300
	ts := timeline(r, n)
	spans := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		instr := triageInstrV1
		if i >= n*85/100 { // instruction revised late in the window -> mild drift
			instr = triageInstrV2
		}
		ticket := tickets[r.Intn(len(tickets))]
		label := triageLabels[r.Intn(len(triageLabels))]
		isErr := r.Float64() < 0.01

		inMsgs, _ := json.Marshal([]map[string]string{
			{"role": "system", "content": instr},
			{"role": "user", "content": "Ticket #" + strconv.Itoa(40000+i) + ": " + ticket},
		})
		outBody := fmt.Sprintf(`{"label":%q,"urgency":%d}`, label, 1+r.Intn(5))
		if isErr {
			outBody = ""
		}
		outMsgs, _ := json.Marshal([]map[string]string{{"role": "assistant", "content": outBody}})

		attrs := []map[string]any{
			kvStr("gen_ai.provider.name", "openai"), // current provider spelling
			kvStr("gen_ai.request.model", "gpt-4o-mini"),
			kvStr("gen_ai.response.model", "gpt-4o-mini-2024-07-18"),
			kvIntNum("gen_ai.usage.input_tokens", 1100+int64(r.Intn(600))), // ints as numbers here
			kvIntNum("gen_ai.usage.output_tokens", 40+int64(r.Intn(50))),
			kvStr("gen_ai.operation.name", "chat"),
			kvStr("app.task", "support-ticket-triage"),
			kvStr("gen_ai.input.messages", string(inMsgs)),
			kvStr("gen_ai.output.messages", string(outMsgs)),
		}
		spans = append(spans, mkSpan(r, "chat gpt-4o-mini", ts[i], 350+r.Float64()*550, attrs, isErr))
	}
	return otlpDoc("support-inbox", spans)
}

// --- fixture: tool-calling agent loop (OpenLLMetry indexed prompts) --------

var alerts = []string{
	"HighPodRestartRate", "DiskPressureWarning", "CertExpiryImminent", "QueueDepthGrowing",
	"LatencyBudgetBurn", "OOMKilledSpike", "NodeNotReady", "ReplicaLagHigh",
	"IngressErrorRate", "BackupJobFailed", "CronDrift", "TLSHandshakeErrors",
	"CacheEvictionStorm", "ThrottledRequests", "DNSResolutionSlow", "PVCAlmostFull",
	"DeploymentStuck", "ImagePullBackoff", "HPAAtMax", "ConnPoolExhausted",
	"LogVolumeSurge", "ShardImbalance", "ColdStartSpike", "RetryStormDetected",
}

var hosts = []string{
	"prod-api-east", "prod-api-west", "prod-worker-a", "prod-worker-b", "prod-ingest",
	"prod-search", "prod-billing", "staging-api", "prod-scheduler", "prod-gateway",
	"prod-reports", "prod-webhooks", "prod-etl", "prod-notify", "prod-auth",
	"prod-files", "prod-metrics", "prod-relay",
}

var agentAsks = []string{
	"correlate with the last deploy and say if we should roll back",
	"check whether the error budget for the tier is already burned",
	"summarize probable root cause and next diagnostic step",
	"decide if this needs a page or can wait for business hours",
	"compare with the same window last week and flag anomalies",
	"list the top three suspects and the command to confirm each",
	"draft a status-page note if customer impact is likely",
	"check recent config changes that could explain this",
}

var toolNames = []string{"kubectl_get", "promql_query", "loki_search", "deploy_history", "runbook_lookup"}

func agentFile(r *rand.Rand) map[string]any {
	const n = 430
	ts := timeline(r, n)
	spans := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		alert := alerts[r.Intn(len(alerts))]
		host := hosts[r.Intn(len(hosts))]
		ask := agentAsks[r.Intn(len(agentAsks))]
		isErr := r.Float64() < 0.08
		wantsTool := r.Float64() < 0.70

		attrs := []map[string]any{
			kvStr("gen_ai.system", "anthropic"),
			kvStr("llm.request.model", "claude-sonnet-4-20250514"), // OpenLLMetry spelling
			kvIntStr("llm.usage.prompt_tokens", 5500+int64(r.Intn(3500))),
			kvIntStr("llm.usage.completion_tokens", 600+int64(r.Intn(700))),
			kvStr("gen_ai.agent.name", "ops-agent"),
			kvStr("gen_ai.prompt.0.role", "system"),
			kvStr("gen_ai.prompt.0.content", "You are ops-agent."),
			kvStr("gen_ai.prompt.1.role", "user"),
			kvStr("gen_ai.prompt.1.content",
				fmt.Sprintf("Alert %s firing on %s since %02d:%02d UTC; %s.", alert, host, r.Intn(24), r.Intn(60), ask)),
		}
		if r.Float64() < 0.5 { // some iterations carry a prior tool result
			attrs = append(attrs,
				kvStr("gen_ai.prompt.2.role", "tool"),
				kvStr("gen_ai.prompt.2.content",
					fmt.Sprintf(`{"tool":%q,"rows":%d,"truncated":%v}`, toolNames[r.Intn(len(toolNames))], r.Intn(400), r.Float64() < 0.3)),
			)
		}
		if wantsTool && !isErr {
			tool := toolNames[r.Intn(len(toolNames))]
			attrs = append(attrs,
				kvStr("gen_ai.completion.0.finish_reason", "tool_calls"),
				kvStr("gen_ai.completion.0.tool_calls.0.name", tool),
				kvStr("gen_ai.completion.0.tool_calls.0.arguments",
					fmt.Sprintf(`{"target":%q,"window":"15m"}`, host)),
			)
		} else if !isErr {
			attrs = append(attrs,
				kvStr("gen_ai.completion.0.finish_reason", "stop"),
				kvStr("gen_ai.completion.0.content",
					fmt.Sprintf("Most likely cause on %s is %s pressure; verify with %s before acting.",
						host, alert, toolNames[r.Intn(len(toolNames))])),
			)
		}
		dur := 2000 + r.Float64()*6000
		if r.Float64() < 0.05 {
			dur = 25000 + r.Float64()*15000 // stuck-loop latency outliers
		}
		spans = append(spans, mkSpan(r, "chat claude-sonnet-4", ts[i], dur, attrs, isErr))
	}
	return otlpDoc("ops-agent", spans)
}

// --- fixture: mixed chat, no task attributes (template clustering) ---------

const chatPreambleA = "You are the in-product support copilot for Acme Cloud. Answer briefly, cite documentation " +
	"slugs like docs/permissions when relevant, never promise refunds, and hand off to a human when the customer " +
	"is angry or mentions legal action. Customer message follows."
const chatPreambleB = "You are the pricing and plans assistant on the Acme Cloud marketing site. Explain plan " +
	"differences in plain words, compare seat and usage pricing honestly, and never invent discounts that are " +
	"not listed on the public pricing page. Visitor question follows."

var chatQuestions = []string{
	"can I move a project between workspaces without losing history",
	"why did my usage jump this month",
	"do you support SAML on the team plan",
	"how are seats counted for guests",
	"what happens to my data if I cancel",
	"can I pin a dashboard to the sidebar",
	"is there an on-prem option",
	"how do I rotate API keys safely",
	"difference between admin and owner roles",
	"can invoices be in EUR",
	"do you have a student discount",
	"how long are audit logs retained",
	"can I limit exports to certain roles",
	"why is the editor slow on big documents",
	"how do I bulk-archive old projects",
	"is the API rate limit per key or per org",
	"can two people edit the same doc offline",
	"what regions can data be stored in",
	"how do I transfer ownership of a workspace",
	"do webhooks retry on failure",
	"can I trial the enterprise plan",
	"how do I export everything before offboarding",
	"is there a sandbox environment",
	"what counts as an active user for billing",
	"can I restrict sign-ups to my domain",
}

func chatFile(r *rand.Rand) map[string]any {
	const nA, nB = 340, 180
	ts := timeline(r, nA+nB)
	spans := make([]map[string]any, 0, nA+nB)
	for i := 0; i < nA+nB; i++ {
		family := "A"
		preamble := chatPreambleA
		model, respModel := "gpt-4o", "gpt-4o-2024-08-06"
		if i%3 == 0 {
			model, respModel = "gpt-4o-mini", "gpt-4o-mini"
		}
		if i >= nA {
			family = "B"
			preamble = chatPreambleB
			model, respModel = "gpt-4o-mini", "gpt-4o-mini"
		}
		// A handful of calls on a model missing from the pricing snapshot:
		// the report must count these as unpriced, not silently guess.
		if family == "A" && i%23 == 0 {
			model, respModel = "grok-4-fast", "grok-4-fast"
		}
		q := chatQuestions[r.Intn(len(chatQuestions))]
		isErr := r.Float64() < 0.03

		inMsgs, _ := json.Marshal([]map[string]string{
			{"role": "system", "content": preamble},
			{"role": "user", "content": q + "?"},
		})
		reply := fmt.Sprintf("Short answer: %s. See docs/%s for the exact steps and edge cases.",
			strings.Split(q, " ")[0], strings.ReplaceAll(strings.Join(strings.Split(q, " ")[:2], "-"), "'", ""))
		if isErr {
			reply = ""
		}
		outMsgs, _ := json.Marshal([]map[string]string{{"role": "assistant", "content": reply}})

		attrs := []map[string]any{
			kvStr("gen_ai.provider.name", providerOf(model)),
			kvStr("gen_ai.request.model", model),
			kvStr("gen_ai.response.model", respModel),
			kvIntStr("gen_ai.usage.input_tokens", 1800+int64(r.Intn(1400))),
			kvIntStr("gen_ai.usage.output_tokens", 300+int64(r.Intn(400))),
			kvStr("gen_ai.operation.name", "chat"),
			// NOTE: deliberately no task attribute -> template clustering.
			kvStr("gen_ai.input.messages", string(inMsgs)),
			kvStr("gen_ai.output.messages", string(outMsgs)),
		}
		spans = append(spans, mkSpan(r, "chat "+model, ts[i], 900+r.Float64()*2200, attrs, isErr))
	}
	return otlpDoc("web-chat", spans)
}

func providerOf(model string) string {
	if strings.HasPrefix(model, "grok") {
		return "xai"
	}
	return "openai"
}

// --- fixture: Langfuse observations_v2 JSONL -------------------------------

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

// --- plumbing --------------------------------------------------------------

func writeJSON(path string, doc map[string]any) {
	raw, err := json.MarshalIndent(doc, "", " ")
	must(err)
	must(os.WriteFile(path, append(raw, '\n'), 0o644))
	fmt.Printf("  %s (%d KB)\n", path, len(raw)/1024)
}

func writeLines(path string, lines [][]byte) {
	var b []byte
	for _, l := range lines {
		b = append(b, l...)
		b = append(b, '\n')
	}
	must(os.WriteFile(path, b, 0o644))
	fmt.Printf("  %s (%d KB, %d lines)\n", path, len(b)/1024, len(lines))
}

func marshal(v any) []byte {
	raw, err := json.Marshal(v)
	must(err)
	return raw
}

func round6(v float64) float64 {
	return float64(int64(v*1e6+0.5)) / 1e6
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
