package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
)

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
