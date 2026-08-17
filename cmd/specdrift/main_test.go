package main

import "testing"

func TestValidateWriteBaselineFlags(t *testing.T) {
	tests := []struct {
		name   string
		check  bool
		format string
		wantOK bool
	}{
		{"defaults allowed", false, "table", true},
		{"check rejected", true, "table", false},
		{"json format rejected", false, "json", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWriteBaselineFlags(tt.check, tt.format)
			if (err == nil) != tt.wantOK {
				t.Errorf("validateWriteBaselineFlags(check=%v, format=%q) error = %v, wantOK=%v",
					tt.check, tt.format, err, tt.wantOK)
			}
		})
	}
}
