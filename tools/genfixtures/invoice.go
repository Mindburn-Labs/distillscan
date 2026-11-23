package main

import (
	"fmt"
	"math/rand"
	"strings"
)

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
