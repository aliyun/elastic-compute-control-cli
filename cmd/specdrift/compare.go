package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/aliyun/elastic-compute-control-cli/internal/drift"
)

func compare(args []string) int {
	flags := flag.NewFlagSet("compare", flag.ContinueOnError)
	baseDir := flags.String("base-dir", "", "directory containing base manifest.json and .generated metadata")
	candidateDir := flags.String("candidate-dir", "", "directory containing candidate manifest.json and .generated metadata")
	specDir := flags.String("spec-dir", "specs", "resource spec YAML directory")
	format := flags.String("format", "json", "output format: json")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *baseDir == "" || *candidateDir == "" || *specDir == "" || *format != "json" || flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "specdrift compare: -base-dir, -candidate-dir, -spec-dir and -format json are required")
		return 2
	}
	report, err := drift.CompareMetadata(*baseDir, *candidateDir, *specDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "specdrift compare: %v\n", err)
		return 2
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintf(os.Stderr, "specdrift compare: %v\n", err)
		return 2
	}
	if len(report.Changes) > 0 {
		return 1
	}
	return 0
}
