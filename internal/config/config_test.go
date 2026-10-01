package config

import "testing"

func TestLoadUsesDefaultPort(t *testing.T) {
	t.Setenv("CACHE_PORT", "")

	cfg := Load()

	if cfg.Port != "8080" {
		t.Errorf("expected port 8080, got %s", cfg.Port)
	}
}

func TestLoadMemoryBudget(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		wantBytes int64
		wantError bool
	}{
		{name: "default", value: "", wantBytes: 64 * 1024 * 1024},
		{name: "configured", value: "1048576", wantBytes: 1048576},
		{name: "minimum positive", value: "1", wantBytes: 1},
		{name: "maximum int64", value: "9223372036854775807", wantBytes: 9223372036854775807},
		{name: "zero", value: "0", wantError: true},
		{name: "negative", value: "-1", wantError: true},
		{name: "non-numeric", value: "64MB", wantError: true},
		{name: "fractional", value: "1.5", wantError: true},
		{name: "overflow", value: "9223372036854775808", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CACHE_PORT", "")
			t.Setenv("CACHE_MAX_BYTES", test.value)
			cfg := Load()
			err := cfg.Validate()
			if test.wantError {
				if err == nil {
					t.Fatal("expected invalid budget to fail validation")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.MaxBytes != test.wantBytes {
				t.Errorf("memory budget is %d, want %d", cfg.MaxBytes, test.wantBytes)
			}
		})
	}
}

func TestLoadUsesEnvironmentPort(t *testing.T) {
	t.Setenv("CACHE_PORT", "9000")

	cfg := Load()

	if cfg.Port != "9000" {
		t.Errorf("expected port 9000, got %s", cfg.Port)
	}
}
