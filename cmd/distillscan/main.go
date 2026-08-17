// distillscan — offline scanner that finds distillation-ready LLM workloads
// in trace exports. Prototype. No network calls, no telemetry: it reads the
// files you point it at and writes report.json / report.html next to you.
//
// Usage:
//
//	distillscan scan [-config distillscan.yaml] [-out DIR] <path>
//
// <path> is a trace file or a directory scanned recursively for OTLP/JSON
// trace exports and Langfuse observations_v2 JSONL exports.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Mindburn-Labs/distillscan/internal/cluster"
	"github.com/Mindburn-Labs/distillscan/internal/config"
	"github.com/Mindburn-Labs/distillscan/internal/ingest"
	"github.com/Mindburn-Labs/distillscan/internal/pricing"
	"github.com/Mindburn-Labs/distillscan/internal/report"
	"github.com/Mindburn-Labs/distillscan/internal/score"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "scan":
		if err := runScan(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "distillscan: %v\n", err)
			os.Exit(2)
		}
	case "version", "-v", "--version":
		fmt.Println("distillscan", report.Version)
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: distillscan scan [-config distillscan.yaml] [-out DIR] <path>

Scans OTLP/JSON GenAI traces and Langfuse observations_v2 JSONL exports,
clusters LLM calls into recurring tasks, and reports which clusters are
ready to distill onto a cheaper model. Fully offline.`)
}

func runScan(args []string) error {
	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	cfgPath := fs.String("config", "", "path to distillscan.yaml (default: auto-detect next to <path>, then ./distillscan.yaml)")
	outDir := fs.String("out", ".", "directory for report.json / report.html")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		usage()
		return fmt.Errorf("scan needs exactly one <path> argument")
	}
	path := fs.Arg(0)

	// Config: explicit flag > auto-detect > defaults.
	var (
		cfg     config.Config
		cfgFrom string
		err     error
	)
	if *cfgPath != "" {
		cfg, err = config.Load(*cfgPath)
		cfgFrom = *cfgPath
	} else {
		cfg, cfgFrom, err = config.Autodetect(path)
	}
	if err != nil {
		return err
	}

	prices, err := pricing.Load()
	if err != nil {
		return err
	}

	calls, stats, err := ingest.Scan(path)
	if err != nil {
		return err
	}
	if len(calls) == 0 {
		return fmt.Errorf("parsed %d file(s) but found no LLM calls", stats.Files)
	}

	clusters := cluster.Group(calls)
	results, assumptions := score.Run(clusters, prices, cfg, cfgFrom)

	rep := report.Build(path, stats, results, assumptions)
	report.Terminal(os.Stdout, rep)

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}
	jsonPath, err := report.WriteJSON(rep, *outDir)
	if err != nil {
		return err
	}
	htmlPath, err := report.WriteHTML(rep, *outDir)
	if err != nil {
		return err
	}
	fmt.Printf("\nreports: %s, %s\n", jsonPath, htmlPath)
	return nil
}
