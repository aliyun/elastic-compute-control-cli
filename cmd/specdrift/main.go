package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/aliyun/elastic-compute-control-cli/internal/drift"
	"github.com/aliyun/elastic-compute-control-cli/internal/specsync"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	switch args[0] {
	case "detect":
		return detect(args[1:])
	case "baseline":
		return baseline(args[1:])
	case "sync":
		return sync(args[1:])
	case "render":
		return render(args[1:])
	case "help", "-h", "--help":
		usage()
		return 0
	default:
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage:
  specdrift detect [flags]   detect OpenAPI metadata drift
  specdrift baseline [flags] record the current OpenAPI metadata as the drift baseline
  specdrift sync [flags]     apply mechanical patches for missing OpenAPI parameters
  specdrift render [flags]   render a bounded Markdown drift summary

Run "specdrift <command> -h" for command-specific flags.
`)
}

func detect(args []string) int {
	fs := flag.NewFlagSet("detect", flag.ContinueOnError)
	specDir := fs.String("spec-dir", "specs", "path to spec YAML directory")
	baselinePath := fs.String("baseline", "drift-baseline.json", "path to the drift baseline file")
	lang := fs.String("lang", "en", "OpenAPI metadata language for descriptions")
	format := fs.String("format", "table", "output format: table or json")
	check := fs.Bool("check", false, "exit non-zero on unacknowledged OpenAPI metadata or binding coverage drift")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	opts := drift.Options{Language: *lang}

	report, err := drift.Detect(*specDir, *baselinePath, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "specdrift: %v (run 'make drift-baseline' after intentional spec or metadata changes)\n", err)
		return 2
	}

	switch *format {
	case "json":
		if err := writeJSON(report); err != nil {
			fmt.Fprintf(os.Stderr, "specdrift: %v\n", err)
			return 2
		}
	case "table":
		writeTable(report)
	default:
		fmt.Fprintf(os.Stderr, "specdrift: unsupported format %q\n", *format)
		return 2
	}

	if *check {
		missing, removed, uncovered, gaps := blockingDriftCounts(report)
		if missing > 0 || removed > 0 || uncovered > 0 || gaps > 0 {
			fmt.Fprintf(os.Stderr, "specdrift: drift detected (missing=%d, removed=%d, uncovered=%d, baseline gaps=%d); adapt the resource specs or investigate the metadata snapshot, then refresh the baseline\n", missing, removed, uncovered, gaps)
			return 1
		}
	}
	return 0
}

func baseline(args []string) int {
	fs := flag.NewFlagSet("baseline", flag.ContinueOnError)
	specDir := fs.String("spec-dir", "specs", "path to spec YAML directory")
	baselinePath := fs.String("baseline", "drift-baseline.json", "path to the drift baseline file")
	lang := fs.String("lang", "en", "OpenAPI metadata language for descriptions")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	opts := drift.Options{Language: *lang}
	if err := runBaseline(*specDir, *baselinePath, opts); err != nil {
		fmt.Fprintf(os.Stderr, "specdrift: %v\n", err)
		return 2
	}
	return 0
}

func sync(args []string) int {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	driftPath := fs.String("drift", "drift.json", "path to the specdrift JSON report")
	specDir := fs.String("spec-dir", "specs", "path to spec YAML directory")
	dryRun := fs.Bool("dry-run", false, "preview the patch each missing item would apply without writing any file")
	write := fs.Bool("write", false, "apply the deterministic patch to the resource specs in place")
	planOut := fs.String("plan-out", "", "write the structured per-item sync plan to this JSON path")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if *dryRun == *write {
		fmt.Fprintln(os.Stderr, "specdrift sync: exactly one of -dry-run or -write must be set")
		return 2
	}
	if err := specsync.Run(*driftPath, *specDir, *dryRun, *planOut); err != nil {
		fmt.Fprintf(os.Stderr, "specdrift sync: %v\n", err)
		return 2
	}
	return 0
}

func render(args []string) int {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	driftPath := fs.String("input", "drift.json", "path to the specdrift JSON report")
	planPath := fs.String("plan", "", "optional path to a specdrift sync plan")
	format := fs.String("format", "markdown", "output format: markdown")
	limit := fs.Int("limit", 50, "maximum drift rows to include (1-200)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *format != "markdown" {
		fmt.Fprintf(os.Stderr, "specdrift render: unsupported format %q\n", *format)
		return 2
	}
	raw, err := specsync.RenderMarkdown(*driftPath, *planPath, *limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "specdrift render: %v\n", err)
		return 2
	}
	if _, err := os.Stdout.Write(raw); err != nil {
		fmt.Fprintf(os.Stderr, "specdrift render: %v\n", err)
		return 2
	}
	return 0
}

func runBaseline(specDir, baselinePath string, opts drift.Options) error {
	baseline, err := drift.CollectBaseline(specDir, opts)
	if err != nil {
		return err
	}
	if err := drift.WriteBaseline(baselinePath, baseline); err != nil {
		return err
	}
	fmt.Printf("specdrift: recorded %d binding(s) in %s\n", len(baseline.Bindings), baselinePath)
	return nil
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
