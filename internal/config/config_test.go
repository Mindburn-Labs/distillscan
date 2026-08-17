package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "distillscan.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaults(t *testing.T) {
	cfg := Default()
	if cfg.DataRights != "unknown" {
		t.Errorf("default data_rights = %q, want unknown", cfg.DataRights)
	}
	if cfg.SafetyCritical {
		t.Error("default safety_critical = true, want false")
	}
	if cfg.SubstitutionRatio != 0.65 {
		t.Errorf("default substitution_ratio = %v, want 0.65", cfg.SubstitutionRatio)
	}
	if cfg.SamplingRate != 1.0 {
		t.Errorf("default sampling_rate = %v, want 1.0", cfg.SamplingRate)
	}
}

func TestLoadSamplingRate(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		want    float64
		wantErr bool
	}{
		{name: "absent defaults to 1", yaml: "data_rights: unknown\n", want: 1.0},
		{name: "valid fraction", yaml: "sampling_rate: 0.25\n", want: 0.25},
		{name: "exactly one", yaml: "sampling_rate: 1.0\n", want: 1.0},
		{name: "zero rejected", yaml: "sampling_rate: 0\n", wantErr: true},
		{name: "negative rejected", yaml: "sampling_rate: -0.5\n", wantErr: true},
		{name: "above one rejected", yaml: "sampling_rate: 1.5\n", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load(writeTemp(t, tc.yaml))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Load accepted %q, want error", tc.yaml)
				}
				if !strings.Contains(err.Error(), "sampling_rate") {
					t.Errorf("error %v does not name sampling_rate", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.SamplingRate != tc.want {
				t.Errorf("sampling_rate = %v, want %v", cfg.SamplingRate, tc.want)
			}
		})
	}
}

func TestLoadDataRights(t *testing.T) {
	// Quoted and bare spellings must both decode as the string "yes"
	// (yaml.v3 does not treat yes/no as booleans).
	for _, y := range []string{"data_rights: \"yes\"\n", "data_rights: yes\n"} {
		cfg, err := Load(writeTemp(t, y))
		if err != nil {
			t.Fatalf("Load(%q): %v", y, err)
		}
		if cfg.DataRights != "yes" {
			t.Errorf("Load(%q) data_rights = %q, want yes", y, cfg.DataRights)
		}
	}
	if _, err := Load(writeTemp(t, "data_rights: maybe\n")); err == nil {
		t.Error("Load accepted data_rights: maybe, want error")
	}
	cfg, err := Load(writeTemp(t, "safety_critical: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataRights != "unknown" {
		t.Errorf("absent data_rights = %q, want unknown", cfg.DataRights)
	}
	if !cfg.SafetyCritical {
		t.Error("safety_critical: true not honored")
	}
}
