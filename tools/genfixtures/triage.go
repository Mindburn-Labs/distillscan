package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
)

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
