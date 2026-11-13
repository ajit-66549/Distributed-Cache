package config

import (
	"testing"
	"time"
)

func TestSnapshotIntervalConfiguration(t *testing.T) {
	t.Setenv("CACHE_MAX_BYTES", "")
	t.Setenv("CACHE_DATA_DIR", "")
	for _, test := range []struct {
		name    string
		value   string
		want    time.Duration
		invalid bool
	}{
		{name: "default", want: time.Minute},
		{name: "seconds", value: "10s", want: 10 * time.Second},
		{name: "compound", value: "1m30s", want: 90 * time.Second},
		{name: "fractional", value: "0.5s", want: 500 * time.Millisecond},
		{name: "zero", value: "0s", invalid: true},
		{name: "negative", value: "-1m", invalid: true},
		{name: "missing unit", value: "10", invalid: true},
		{name: "malformed", value: "later", invalid: true},
		{name: "overflow", value: "999999999999999999999h", invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CACHE_SNAPSHOT_INTERVAL", test.value)
			cfg := Load()
			if err := cfg.Validate(); (err != nil) != test.invalid {
				t.Fatalf("Validate() = %v, invalid = %v", err, test.invalid)
			}
			if !test.invalid && cfg.SnapshotInterval != test.want {
				t.Errorf("interval = %s, want %s", cfg.SnapshotInterval, test.want)
			}
		})
	}
}
