package report

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
)

// WriteHTML writes report.html into dir: one self-contained file, inline CSS,
// no scripts, no external assets.
func WriteHTML(r *Report, dir string) (string, error) {
	path := filepath.Join(dir, "report.html")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := htmlTmpl.Execute(f, r); err != nil {
		return "", err
	}
	return path, nil
}

var htmlTmpl = template.Must(template.New("report").Funcs(template.FuncMap{
	"usd":  usd,
	"add1": func(i int) int { return i + 1 },
	"pct":  func(v float64) string { return fmt.Sprintf("%.0f%%", v*100) },
	"f2":   func(v float64) string { return fmt.Sprintf("%.2f", v) },
	"bar": func(v float64) template.CSS {
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		return template.CSS(fmt.Sprintf("width:%.0f%%", v*100))
	},
	"verdictClass": func(v string) string {
		switch v {
		case "READY":
			return "ready"
		case "BORDERLINE":
			return "borderline"
		default:
			return "notready"
		}
	},
	"factorLabel": func(name string) string { return strings.ReplaceAll(name, "_", " ") },
}).Parse(htmlSrc))

const htmlSrc = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>distillscan report</title>
<style>
  :root {
    --bg: #ffffff; --fg: #1a1d21; --muted: #6b7280; --line: #e5e7eb;
    --card: #f9fafb; --accent: #1f6feb;
    --ready: #116329; --ready-bg: #dafbe1;
    --borderline: #7d4e00; --borderline-bg: #fff3cd;
    --notready: #82071e; --notready-bg: #ffebe9;
  }
  * { box-sizing: border-box; }
  body {
    margin: 0; padding: 2.5rem 1.25rem 4rem; background: var(--bg); color: var(--fg);
    font: 15px/1.55 ui-sans-serif, -apple-system, "Segoe UI", Roboto, sans-serif;
  }
  main { max-width: 980px; margin: 0 auto; }
  h1 { font-size: 1.5rem; margin: 0 0 .25rem; }
  h1 span { color: var(--muted); font-weight: 400; font-size: 1rem; }
  .meta { color: var(--muted); margin-bottom: 1.5rem; }
  .assume {
    background: var(--card); border: 1px solid var(--line); border-radius: 8px;
    padding: .9rem 1.1rem; margin: 0 0 1.75rem; font-size: .9rem;
  }
  .assume strong { display: block; margin-bottom: .3rem; }
  .assume li { margin: .15rem 0; }
  table { border-collapse: collapse; width: 100%; font-size: .92rem; }
  .tablewrap { overflow-x: auto; }
  th, td { text-align: left; padding: .5rem .7rem; border-bottom: 1px solid var(--line); vertical-align: top; }
  th { font-size: .78rem; text-transform: uppercase; letter-spacing: .04em; color: var(--muted); }
  td.num, th.num { text-align: right; font-variant-numeric: tabular-nums; }
  .verdict { font-weight: 600; font-size: .8rem; padding: .15rem .5rem; border-radius: 999px; white-space: nowrap; }
  .verdict.ready { color: var(--ready); background: var(--ready-bg); }
  .verdict.borderline { color: var(--borderline); background: var(--borderline-bg); }
  .verdict.notready { color: var(--notready); background: var(--notready-bg); }
  details { margin: 1rem 0; border: 1px solid var(--line); border-radius: 8px; background: var(--card); }
  summary { cursor: pointer; padding: .8rem 1.1rem; font-weight: 600; }
  summary .sub { color: var(--muted); font-weight: 400; font-size: .88rem; }
  .body { padding: 0 1.1rem 1rem; }
  .factor { display: grid; grid-template-columns: 10rem 1fr 3.2rem; gap: .8rem; align-items: center; margin: .45rem 0; font-size: .88rem; }
  .factor .name { color: var(--fg); }
  .factor .name small { color: var(--muted); display: block; }
  .track { background: var(--line); border-radius: 999px; height: 8px; overflow: hidden; }
  .fill { background: var(--accent); height: 100%; border-radius: 999px; }
  .declared .fill { background: repeating-linear-gradient(45deg, #9ca3af 0 6px, #d1d5db 6px 12px); }
  .detail { color: var(--muted); font-size: .8rem; margin: .1rem 0 .6rem 10.8rem; }
  .notes { margin: .6rem 0 0; padding-left: 1.1rem; color: var(--muted); font-size: .85rem; }
  .sample { color: var(--muted); font-size: .8rem; font-family: ui-monospace, monospace; background: #fff; border: 1px dashed var(--line); border-radius: 6px; padding: .5rem .7rem; margin-top: .6rem; word-break: break-word; }
  footer { margin-top: 2.5rem; color: var(--muted); font-size: .8rem; }
</style>
</head>
<body>
<main>
  <h1>distillscan <span>{{.Version}} — distillation readiness report</span></h1>
  <div class="meta">
    scanned <code>{{.ScannedPath}}</code> · {{.Ingest.Files}} file(s) · {{.Ingest.Calls}} LLM calls
    · window {{f2 .Assumptions.WindowDays}} days · generated {{.GeneratedAt.Format "2006-01-02 15:04 UTC"}}
  </div>

  <div class="assume">
    <strong>Assumptions behind every number below</strong>
    <ul>
      <li>Est. annual savings = window spend × {{f2 .Assumptions.AnnualizationFactor}} annualization × {{pct .Assumptions.SubstitutionRatio}} assumed substitution ratio.</li>
      <li>Prices: bundled snapshot {{.Assumptions.PricingSnapshot}}; traces carrying their own cost use it instead.{{if gt .Totals.UnpricedCalls 0}} {{.Totals.UnpricedCalls}} call(s) on unknown models counted as $0.{{end}}</li>
      <li>Declared, not measured: data_rights = <b>{{.Assumptions.DataRights}}</b>, safety_critical = <b>{{.Assumptions.SafetyCritical}}</b>{{if .Assumptions.ConfigPath}} (from {{.Assumptions.ConfigPath}}){{else}} (defaults){{end}}.</li>
      <li>Verdicts: READY ≥ {{index .Thresholds "ready" | f2}}, BORDERLINE ≥ {{index .Thresholds "borderline" | f2}}, else NOT READY.</li>
    </ul>
  </div>

  <div class="tablewrap">
  <table>
    <thead><tr>
      <th>#</th><th>Cluster</th><th class="num">Calls</th><th>Top model</th>
      <th class="num">Spend (window)</th><th class="num">Est. annual savings</th>
      <th class="num">Score</th><th>Verdict</th>
    </tr></thead>
    <tbody>
    {{range $i, $c := .Clusters}}
      <tr>
        <td>{{add1 $i}}</td>
        <td>{{$c.Label}}</td>
        <td class="num">{{$c.Calls}}</td>
        <td>{{$c.TopModel}}</td>
        <td class="num">{{usd $c.SpendUSD}}</td>
        <td class="num"><b>{{usd $c.SavingsUSD}}</b></td>
        <td class="num">{{f2 $c.Composite}}</td>
        <td><span class="verdict {{verdictClass $c.Verdict}}">{{$c.Verdict}}</span></td>
      </tr>
    {{end}}
    </tbody>
  </table>
  </div>

  <h2 style="font-size:1.1rem;margin-top:2rem">Factor breakdown</h2>
  {{range $i, $c := .Clusters}}
  <details{{if eq $i 0}} open{{end}}>
    <summary>{{add1 $i}}. {{$c.Label}}
      <span class="sub">— {{$c.Calls}} calls · {{usd $c.SpendUSD}} in window · score {{f2 $c.Composite}} · {{$c.Verdict}}</span>
    </summary>
    <div class="body">
      {{range $c.Factors}}
      <div class="factor{{if .Declared}} declared{{end}}">
        <div class="name">{{factorLabel .Name}}{{if .Declared}} <small>declared, not measured</small>{{end}}</div>
        <div class="track"><div class="fill" style="{{bar .Score}}"></div></div>
        <div class="num">{{f2 .Score}}</div>
      </div>
      <div class="detail">weight {{f2 .Weight}} · {{.Detail}}</div>
      {{end}}
      {{if $c.Notes}}<ul class="notes">{{range $c.Notes}}<li>{{.}}</li>{{end}}</ul>{{end}}
      {{if $c.SampleTemplate}}<div class="sample">template: {{$c.SampleTemplate}}</div>{{end}}
    </div>
  </details>
  {{end}}

  <footer>
    distillscan {{.Version}} · fully offline scan — no telemetry, no network ·
    numbers are estimates for prioritization, not billing truth.
  </footer>
</main>
</body>
</html>
`
