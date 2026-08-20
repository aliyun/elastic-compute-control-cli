package main

import (
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/internal/drift"
)

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

func TestSyncRequiresExactlyOneOfDryRunOrWrite(t *testing.T) {
	if code := sync(nil); code != 2 {
		t.Fatalf("sync(nil) = %d, want 2", code)
	}
	if code := sync([]string{"-write", "-dry-run"}); code != 2 {
		t.Fatalf("sync(-write -dry-run) = %d, want 2", code)
	}
}

func TestRunRejectsMissingOrUnknownSubcommand(t *testing.T) {
	if code := run(nil); code != 2 {
		t.Fatalf("run(nil) = %d, want 2", code)
	}
	if code := run([]string{"unknown"}); code != 2 {
		t.Fatalf("run(unknown) = %d, want 2", code)
	}
}

func TestRunDispatchesHelp(t *testing.T) {
	if code := run([]string{"help"}); code != 0 {
		t.Fatalf("run(help) = %d, want 0", code)
	}
	if code := run([]string{"-h"}); code != 0 {
		t.Fatalf("run(-h) = %d, want 0", code)
	}
}

func TestRenderRejectsUnsupportedFormat(t *testing.T) {
	if code := render([]string{"-format", "json"}); code != 2 {
		t.Fatalf("render(-format json) = %d, want 2", code)
	}
}
