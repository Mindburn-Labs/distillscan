# Security

Report suspected vulnerabilities privately to
[security@mindburn.org](mailto:security@mindburn.org). Do not publish
vulnerability details in a public issue, pull request, or discussion.

distillscan is an offline command-line tool that parses LLM trace exports and
generates workload reports. Reports can cover trace parsing and report
generation, including handling of OTLP JSON and Langfuse
`observations_v2` JSONL.

In your private report, include:

- The affected release, tag, or commit and your operating environment.
- A minimal synthetic or redacted trace export, command, and configuration
  needed to reproduce the behavior.
- The expected and actual behavior, likely security impact, and any known
  mitigation.

Remove credentials, personal data, and confidential prompts or outputs from
reproductions. Do not submit raw customer traces or production evidence
bundles.
