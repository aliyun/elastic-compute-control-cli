package main

import (
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/internal/drift"
)

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

func TestBlockingDriftCountsIncludesRemoved(t *testing.T) {
	report := drift.Report{Items: []drift.Item{
		{Kind: drift.KindMissing},
		{Kind: drift.KindRemoved},
		{Kind: drift.KindUncovered},
	}, BaselineGaps: 2}

	missing, removed, uncovered, gaps := blockingDriftCounts(report)
	if missing != 1 || removed != 1 || uncovered != 1 || gaps != 2 {
		t.Fatalf("blockingDriftCounts = %d/%d/%d/%d, want 1/1/1/2",
			missing, removed, uncovered, gaps)
	}
}
