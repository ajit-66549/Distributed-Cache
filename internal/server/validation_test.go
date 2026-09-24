package server

import (
	"strings"
	"testing"
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
