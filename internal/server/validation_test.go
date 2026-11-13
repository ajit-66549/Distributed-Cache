package server

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestValidateKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr error
	}{
		{
			name:    "valid key",
			key:     "name",
			wantErr: nil,
		},
		{
			name:    "empty key",
			key:     "",
			wantErr: errKeyRequired,
		},
		{
			name:    "key too long",
			key:     strings.Repeat("a", maxKeyLength+1),
			wantErr: errKeyTooLong,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateKey(test.key)

			if err != test.wantErr {
				t.Errorf("expected error %v, got %v", test.wantErr, err)
			}
		})
	}
}

func TestValidateValue(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr error
	}{
		{
			name:    "valid value",
			value:   "Ajit",
			wantErr: nil,
		},
		{
			name:    "empty value",
			value:   "",
			wantErr: nil,
		},
		{
			name:    "value too large",
			value:   strings.Repeat("a", maxValueSize+1),
			wantErr: errValueTooLarge,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := valueValidate(test.value)

			if err != test.wantErr {
				t.Errorf("expected error %v, got %v", test.wantErr, err)
			}
		})
	}
}

func TestValidateTTLDurationBounds(t *testing.T) {
	limit := int64(math.MaxInt64 / int64(time.Second))
	for _, test := range []struct {
		name    string
		seconds int64
		invalid bool
	}{
		{name: "one second", seconds: 1},
		{name: "largest safe duration", seconds: limit},
		{name: "first overflowing duration", seconds: limit + 1, invalid: true},
		{name: "maximum integer", seconds: math.MaxInt64, invalid: true},
		{name: "would wrap to zero", seconds: 1 << 55, invalid: true},
		{name: "zero", invalid: true},
		{name: "negative", seconds: -1, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateTTL(&test.seconds); (err != nil) != test.invalid {
				t.Fatalf("validateTTL(%d) = %v, invalid = %v", test.seconds, err, test.invalid)
			}
		})
	}
	if err := validateTTL(nil); err != nil {
		t.Fatalf("optional TTL was rejected: %v", err)
	}
}
