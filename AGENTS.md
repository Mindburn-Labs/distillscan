# Agent Operational Guidelines for distillscan

distillscan is an offline Go CLI. It reads LLM trace exports (OTLP JSON and
Langfuse `observations_v2` JSONL), clusters recurring workloads, and writes a
report ranking which ones look ready to distill onto a smaller model. It is an
early prototype (v0.1). It trains nothing, makes no network calls and collects
no telemetry.

## Dev Commands
* Check: `make check` (what CI runs) builds, vets and tests, then fails on
  unformatted Go files.
* Build: `make build` (`go build ./...`)
* Test: `make test` (`go test ./...`)
* Format: `make fmt` (`gofmt -l -w .`)
* Demo: `make demo` scans `fixtures/` and writes `report.json` and
  `report.html` to the repository root (gitignored). `make clean` removes them.
* Fixtures: `make fixtures` regenerates `fixtures/` with `tools/genfixtures`
  (fixed seed, byte-reproducible). Commit the result when the generator changes.

The Makefile sets `GOWORK=off`, so the module builds standalone.

## Boundaries
From CONTRIBUTING.md and the README:
* Offline stays offline: no network access, phone-home behaviour or telemetry.
* Same input and config give the same report, apart from the generation
  timestamp. Watch map iteration order and float summation order.
* Every number in a report is measured from the traces or declared in config,
  and declared inputs are shown as "declared, not measured".
* `internal/pricing/prices.yaml` is a dated snapshot, used only when a trace
  carries no cost of its own. Unknown models count as $0 and are reported.
