# Contributing to distillscan

Thanks for looking at this. The tool is early; small, focused changes land
fastest.

## Ground rules

- **Offline stays offline.** distillscan makes no network calls and collects
  no telemetry. Changes that add network access, phone-home behavior, or
  "optional" telemetry will not be merged.
- **Determinism is a feature.** Same input, same config, same report (modulo
  the generation timestamp). Watch for map-iteration order and float
  summation order; tests enforce this.
- **Assumptions stay printed.** Any new number in a report must either be
  measured from the traces or declared in config — and every declared input
  is rendered as "declared, not measured", never silently folded in.

## Workflow

1. Fork, branch, make the change.
2. `make check` must pass (build + vet + test), and `gofmt -l .` must print
   nothing — CI enforces both.
3. If you touched fixtures or the generator, run `make fixtures` and commit
   the result; generation is seeded and byte-reproducible.
4. Open a PR with a short description of what changed and why. For behavior
   changes, include a before/after of the terminal report.

## Bugs and ideas

Open a GitHub issue. For ingestion bugs, an anonymized minimal trace snippet
(a few spans/lines with prompts redacted) makes fixes dramatically faster.

By contributing you agree that your contributions are licensed under the
Apache License 2.0, the license of this repository.
