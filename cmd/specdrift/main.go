package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/aliyun/elastic-compute-control-cli/internal/drift"
)

func main() {
	specDir := flag.String("spec-dir", "specs", "path to spec YAML directory")
	baselinePath := flag.String("baseline", "drift-baseline.json", "path to the drift baseline file")
	lang := flag.String("lang", "en", "OpenAPI metadata language for descriptions")
	format := flag.String("format", "table", "output format: table or json")
	check := flag.Bool("check", false, "exit non-zero on unacknowledged OpenAPI metadata or binding coverage drift")
	writeBaseline := flag.Bool("write-baseline", false, "record the current OpenAPI metadata as the drift baseline and exit")
	flag.Parse()

	opts := drift.Options{Language: *lang}

	if *writeBaseline {
		if err := validateWriteBaselineFlags(*check, *format); err != nil {
			fmt.Fprintf(os.Stderr, "specdrift: %v\n", err)
			os.Exit(2)
		}
		baseline, err := drift.CollectBaseline(*specDir, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "specdrift: %v\n", err)
			os.Exit(2)
		}
		if err := drift.WriteBaseline(*baselinePath, baseline); err != nil {
			fmt.Fprintf(os.Stderr, "specdrift: %v\n", err)
			os.Exit(2)
		}
		fmt.Printf("specdrift: recorded %d binding(s) in %s\n", len(baseline.Bindings), *baselinePath)
		return
	}

	report, err := drift.Detect(*specDir, *baselinePath, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "specdrift: %v (run 'make drift-baseline' after intentional spec or metadata changes)\n", err)
		os.Exit(2)
	}

	switch *format {
	case "json":
		if err := writeJSON(report); err != nil {
			fmt.Fprintf(os.Stderr, "specdrift: %v\n", err)
			os.Exit(2)
		}
	case "table":
		writeTable(report)
	default:
		fmt.Fprintf(os.Stderr, "specdrift: unsupported format %q\n", *format)
		os.Exit(2)
	}

	if *check {
		missing, removed, uncovered, gaps := blockingDriftCounts(report)
		if missing > 0 || removed > 0 || uncovered > 0 || gaps > 0 {
			fmt.Fprintf(os.Stderr, "specdrift: drift detected (missing=%d, removed=%d, uncovered=%d, baseline gaps=%d); adapt the resource specs or investigate the metadata snapshot, then refresh the baseline\n", missing, removed, uncovered, gaps)
			os.Exit(1)
		}
	}
}

func blockingDriftCounts(report drift.Report) (missing, removed, uncovered, gaps int) {
	return len(report.Missing()), len(report.Removed()), len(report.Uncovered()), report.BaselineGaps
}

type jsonReport struct {
	drift.Report
	Missing   int `json:"missing"`
	Removed   int `json:"removed"`
	Uncovered int `json:"uncovered"`
}

// validateWriteBaselineFlags rejects report-only flags that -write-baseline
// would silently ignore.
func validateWriteBaselineFlags(check bool, format string) error {
	if check {
		return errors.New("-check cannot be combined with -write-baseline")
	}
	if format != "table" {
		return errors.New("-format cannot be combined with -write-baseline")
	}
	return nil
}

func writeJSON(report drift.Report) error {
	out := jsonReport{Report: report, Missing: len(report.Missing()), Removed: len(report.Removed()), Uncovered: len(report.Uncovered())}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(out)
}

func writeTable(report drift.Report) {
	writer := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	defer writer.Flush()

	fmt.Fprintf(writer, "product\tresource\tbinding\tapi\tparam\tkind\ttype\trequired\tdescription\n")
	for _, item := range report.Items {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%t\t%s\n",
			item.Product, item.Resource, item.Binding, item.API, item.Param,
			item.Kind, item.Type, item.Required, item.Description)
	}
	for _, skipped := range report.Skipped {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t-\t-\t-\t-\t(skipped: %s)\n",
			skipped.Product, skipped.Resource, skipped.Binding, skipped.API, skipped.Reason)
	}

	fmt.Fprintf(writer, "\nbindings checked: %d\n", report.BindingsChecked)
	fmt.Fprintf(writer, "bindings skipped: %d\n", len(report.Skipped))
	fmt.Fprintf(writer, "missing: %d\n", len(report.Missing()))
	fmt.Fprintf(writer, "removed: %d\n", len(report.Removed()))
	fmt.Fprintf(writer, "uncovered: %d\n", len(report.Uncovered()))
	fmt.Fprintf(writer, "baseline gaps: %d\n", report.BaselineGaps)
}
