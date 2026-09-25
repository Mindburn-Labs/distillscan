# distillscan

Offline scanner for LLM trace exports. Point it at the traces you already
have; it clusters your recurring workloads and reports which ones look ready
to distill onto a smaller, cheaper model — ranked by estimated annual
savings, with every assumption printed in the report it hands you.

Traces in → readiness report out. Nothing else: no training, no API calls,
no data leaving your machine.

**Status: early prototype (v0.1).** The CLI and report formats may still
change. Built by [Mindburn Labs](https://mindburn.org).

## Quickstart

With Go 1.25+:

```sh
# scan a directory of trace exports
go run github.com/Mindburn-Labs/distillscan/cmd/distillscan@latest scan ./my-traces/

# or install the binary
go install github.com/Mindburn-Labs/distillscan/cmd/distillscan@latest
```

Or clone and run the bundled synthetic demo:

```sh
git clone https://github.com/Mindburn-Labs/distillscan
cd distillscan
make demo   # = go run ./cmd/distillscan scan fixtures
```

Prebuilt binaries and a `uvx` shim are planned but **not available yet** —
for now the Go toolchain is the only install path.

Every scan prints a ranked table and writes `report.json` (machine-readable,
every factor and formula) and `report.html` (single self-contained file, no
scripts) to the current directory. `-out DIR` moves them, `-config FILE`
points at a config, `-min-savings USD` adjusts the noise threshold
(default $100/yr; `0` disables it).

## What a scan looks like

Real output of `make demo` against the bundled synthetic fixtures:

```
distillscan 0.1.0-prototype — offline trace scan
scanned fixtures: 5 file(s), 4430 LLM calls (16 non-GenAI records skipped)
observed window: 7.0 days | spend in window: $293 | annualized: $15,251

#  CLUSTER             CALLS  TOP MODEL                 SPEND(WIN)  EST.ANNUAL SAVINGS  SCORE  VERDICT
1  contract-summarize  700    claude-opus-4             $237        $8,033              0.84   READY
2  invoice-extraction  1100   gpt-4o-2024-08-06         $37         $1,245              0.88   READY
3  ops-agent           430    claude-sonnet-4-20250514  $15         $524                0.42   NOT READY

below min-savings threshold ($100/yr; -min-savings to change): 4 cluster(s), $111 combined est. savings
  4  "you are the in-product support copilot for a…  340 calls   $84  0.61  BORDERLINE
  5  product-translate                               380 calls   $13  0.61  BORDERLINE
  6  support-ticket-triage                           1300 calls  $11  0.77  READY
  7  "you are the pricing and plans assistant on t…  180 calls   $4   0.60  BORDERLINE

assumptions (printed, not hidden):
  - savings = window spend x 52.13 annualization x 65% assumed substitution ratio
  - prices: bundled snapshot 2026-09-25 (traces with their own cost use it instead)
  - declared, not measured: data_rights=yes, safety_critical=false (from fixtures/distillscan.yaml)
  - 15 call(s) on unknown models carry $0 in these numbers
  - verdicts: READY >= 0.65, BORDERLINE >= 0.45, else NOT READY

reports: report.json, report.html
```

The fixtures are synthetic but shaped like real exports; regenerate them with
`make fixtures` (fixed seed, byte-reproducible). Model names in the demo are
just what trace exports contain — the scanner ranks *your workloads*, it does
not benchmark or compare providers.

## Input formats

- **OTLP/JSON traces** with GenAI semantic-convention attributes, across the
  three attribute generations that coexist in the wild: legacy
  (`gen_ai.system`, `gen_ai.usage.prompt_tokens`), current
  (`gen_ai.provider.name`, `gen_ai.usage.input_tokens`,
  `gen_ai.input.messages`), and OpenLLMetry-style indexed prompts
  (`gen_ai.prompt.{n}.content`, `llm.*`). One alias table
  (`internal/ingest/otlp.go`) normalizes all of them into one call model.
- **Langfuse `observations_v2` JSONL** exports (one JSON object per line,
  tolerant field precedence; per-call calculated costs are used when
  present).

Files are routed by content sniffing; non-GenAI spans and non-generation
lines are skipped and counted in the report.

## How scoring works

Calls are clustered deterministically — explicit task/endpoint attribute
first, otherwise a prompt-template prefix hash (whitespace collapsed; URLs,
emails, UUIDs, dates, hex ids, digit runs replaced with slot markers). No
embeddings, no network, reproducible across runs.

Each cluster gets eight factors, every one 0..1 with its formula printed in
the report (`internal/score/score.go` holds the weights and thresholds):

| factor | weight | source |
|---|---|---|
| spend_share | 0.20 | measured |
| volume | 0.15 | measured |
| repetition (template-prefix uniformity) | 0.20 | measured |
| evaluability (JSON / tool-call output share) | 0.15 | measured |
| stability (1 − early-vs-late template drift) | 0.10 | measured |
| edge_cases (1 − error/outlier rate) | 0.10 | measured |
| data_rights | 0.05 | **declared, not measured** |
| safety_critical | 0.05 | **declared, not measured** |

Verdicts: READY ≥ 0.65, BORDERLINE ≥ 0.45, else NOT READY — plus three hard
gates on the declared inputs:

- `data_rights: no` forces **NOT READY** (the teacher's terms forbid it).
- `data_rights: unknown` (the default) caps the verdict at **BORDERLINE** —
  reported as *ready pending rights verification*. READY requires explicitly
  declared rights.
- `safety_critical: true` caps the verdict at **BORDERLINE**.

Clusters whose estimated savings fall under the min-savings threshold fold
into a collapsed "below threshold" section (a short block in the terminal, a
collapsed `<details>` in the HTML). They stay in `report.json`, marked
`below_min_savings`; scores and verdicts are unaffected.

### Declared inputs

Everything the traces cannot prove comes from an optional `distillscan.yaml`
next to your traces (see [`distillscan.example.yaml`](distillscan.example.yaml))
and is always rendered "declared, not measured":

- `data_rights` — whether the teacher model's terms allow training on its
  outputs (`yes` / `no` / `unknown`, default `unknown`).
- `safety_critical` — whether the workload sits in a safety-critical path.
- `substitution_ratio` — assumed share of a cluster's spend a distilled
  model could substitute (default 0.65).
- `sampling_rate` — declared share of real traffic present in the export,
  in (0,1]. Extrapolated spend/savings are divided by it; a 10% sample means
  true spend is treated as 10× what was observed. Default 1.0.

### Honest limits

- Savings are **estimates computed from your own traces**: window spend ×
  annualization ÷ declared sampling rate × an assumed substitution ratio,
  all printed in every report. They are a prioritization signal, not billing
  truth, and not a promise.
- `internal/pricing/prices.yaml` is a dated snapshot used only when a trace
  carries no cost of its own. Unknown models are counted as $0 and reported,
  never guessed.
- The snapshot's current Anthropic models (`claude-fable-5-1`,
  `claude-opus-5-5`, `claude-sonnet-5`, `claude-haiku-4-5`) and OpenAI models
  (`gpt-6-astra`, `gpt-6-sol`, `gpt-6-luna`) carry official standard-tier
  list prices verified on 2026-09-25; sources are in the file. Provider and
  OpenRouter IDs (`openai/gpt-6-sol`, `anthropic/claude-opus-5.5`) resolve
  to the same rows. Only base input/output rates are modeled: every input
  token a trace reports is priced at the base input rate, with no cache,
  batch or long-context adjustment.
- Repetition/drift look at the template *prefix* (the instruction head), so
  variable payloads (documents, tickets) don't read as churn — but freeform
  surfaces are caught by evaluability, and template-keyed clusters can't
  show drift by construction.

## What it does NOT do

- **No training, no distillation runs.** It tells you where distillation
  looks worth trying; it never touches a model.
- **No network, no telemetry.** The binary makes zero network calls — it
  reads the files you point it at and writes reports next to you. Pricing
  data is embedded at build time.
- **No provider benchmarking.** It ranks your workloads by your own spend
  and structure; it does not rate, compare, or recommend model vendors.
- **No verdict authority.** READY is a heuristic with its formula printed;
  rights, safety, and rollout decisions stay with you.

## Development

```sh
make check   # build + vet + test + gofmt
```

Go 1.25, single dependency (`gopkg.in/yaml.v3`). The Makefile pins
`GOWORK=off` so the repo builds standalone even inside a Go workspace.
See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[Apache-2.0](LICENSE) © 2026 Mindburn Labs.
