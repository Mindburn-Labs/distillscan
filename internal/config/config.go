// Package config loads the optional distillscan.yaml file with declared
// (not measured) factors and scan assumptions.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds operator-declared inputs. These are folded into scoring but
// always rendered as "declared, not measured" — the scanner cannot verify
// them from traces.
type Config struct {
	// DataRights: does the teacher model's ToS allow training on its
	// outputs? One of "yes", "no", "unknown". Default "unknown".
	DataRights string `yaml:"data_rights"`

	// SafetyCritical: are these calls in a safety-critical path where a
	// distilled student model would need extra review? Default false.
	SafetyCritical bool `yaml:"safety_critical"`

	// SubstitutionRatio is the assumed share of a cluster's spend that a
	// distilled model could substitute. Used only for the savings estimate,
	// printed explicitly in every report. Default 0.65 (conservative:
	// assumes residual teacher fallback + student serving cost).
	SubstitutionRatio float64 `yaml:"substitution_ratio"`

	// SamplingRate is the declared share of production traffic present in
	// the scanned export, in (0,1]. Extrapolated spend/savings are divided
	// by it (a 10% sample means true spend is ~10x what was observed).
	// Declared, not measured — always printed in the report when it shapes
	// the numbers. Default 1.0 (the export is the full traffic).
	SamplingRate float64 `yaml:"sampling_rate"`
}

// Default returns the config used when no distillscan.yaml is present.
func Default() Config {
	return Config{
		DataRights:        "unknown",
		SafetyCritical:    false,
		SubstitutionRatio: 0.65,
		SamplingRate:      1.0,
	}
}

// Load reads a config file. Unknown fields are ignored (tolerant parse).
func Load(path string) (Config, error) {
	cfg := Default()
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	switch cfg.DataRights {
	case "yes", "no", "unknown":
	case "":
		cfg.DataRights = "unknown"
	default:
		return cfg, fmt.Errorf("%s: data_rights must be yes|no|unknown, got %q", path, cfg.DataRights)
	}
	if cfg.SubstitutionRatio <= 0 || cfg.SubstitutionRatio > 1 {
		return cfg, fmt.Errorf("%s: substitution_ratio must be in (0,1], got %v", path, cfg.SubstitutionRatio)
	}
	if cfg.SamplingRate <= 0 || cfg.SamplingRate > 1 {
		return cfg, fmt.Errorf("%s: sampling_rate must be in (0,1], got %v", path, cfg.SamplingRate)
	}
	return cfg, nil
}

// Autodetect looks for distillscan.yaml next to the scan path, then in the
// current directory. Returns the default config (and empty path) when none
// exists.
func Autodetect(scanPath string) (Config, string, error) {
	candidates := []string{}
	if fi, err := os.Stat(scanPath); err == nil {
		dir := scanPath
		if !fi.IsDir() {
			dir = filepath.Dir(scanPath)
		}
		candidates = append(candidates, filepath.Join(dir, "distillscan.yaml"))
	}
	candidates = append(candidates, "distillscan.yaml")
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			cfg, err := Load(c)
			return cfg, c, err
		}
	}
	return Default(), "", nil
}
