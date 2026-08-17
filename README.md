# distillscan

**Prototype — internal, not published.** Scans LLM trace exports offline and
reports which recurring workloads are ready to distill onto a cheaper model,
ranked by estimated annual savings. No network calls, no telemetry: it reads
the files you point it at and writes a report next to you.

## Demo

```sh
make demo
# = GOWORK=off go run ./cmd/distillscan scan fixtures
```

Prints a ranked cluster table and writes `report.json` (machine-readable
factors) and `report.html` (single self-contained file) to the current
directory. `-out DIR` moves them, `-config FILE` points at a config
(default: auto-detect `distillscan.yaml` next to the scan path).

The bundled `fixtures/` are synthetic but shaped like real exports —
regenerate them with `make fixtures` (fixed seed, byte-reproducible).

## What it ingests

- **OTLP/JSON traces** with GenAI semantic-convention attributes, across the
  three attribute generations that coexist in the wild: legacy
  (`gen_ai.system`, `gen_ai.usage.prompt_tokens`), current
  (`gen_ai.provider.name`, `gen_ai.usage.input_tokens`,
  `gen_ai.input.messages`), and OpenLLMetry-style indexed prompts
  (`gen_ai.prompt.{n}.content`, `llm.*`). One alias table
  (`internal/ingest/otlp.go`) normalizes all of them into one call model.
- **Langfuse `observations_v2` JSONL** exports (one JSON object per line,
  tolerant field precedence; per-call calculated costs are used when present).

Files are routed by content sniffing; non-GenAI spans and non-generation
lines are skipped and counted.

## How it scores

Calls are clustered deterministically: explicit task/endpoint attribute
first, otherwise a prompt-template prefix hash (whitespace collapsed; URLs,
emails, UUIDs, dates, hex ids, digit runs replaced with slot markers).

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

Verdicts: READY ≥ 0.65, BORDERLINE ≥ 0.45, else NOT READY. Two hard gates:
`data_rights: no` forces NOT READY; `safety_critical: true` caps at
BORDERLINE. Declared inputs come from `distillscan.yaml`
(see `distillscan.example.yaml`).

## Honesty notes

- Savings = window spend × annualization × an **assumed** substitution ratio
  (default 0.65, configurable, printed in every report). It is a
  prioritization signal, not billing truth.
- `internal/pricing/prices.yaml` is a **demo snapshot** (dated inside), used
  only when a trace carries no cost of its own. Unknown models are counted
  as $0 and reported, never guessed.
- Repetition/drift look at the template *prefix* (the instruction head), so
  variable payloads (documents, tickets) don't read as churn — but freeform
  surfaces are caught by evaluability, and template-keyed clusters can't
  show drift by construction.

## Development

```sh
make check   # build + vet + test
```

Go 1.25, single dependency (`gopkg.in/yaml.v3`). The workspace `go.work`
does not include this repo; the Makefile pins `GOWORK=off`.
