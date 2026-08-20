package specsync

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

const DriftIssueMarker = "<!-- ecctl-openapi-drift -->"

// RenderMarkdown renders a bounded issue summary from a complete drift report
// and its optional sync plan. The full JSON artifacts remain the source of
// truth; this output is intentionally small enough for a GitHub issue body.
func RenderMarkdown(driftPath, planPath string, limit int) ([]byte, error) {
	if limit < 1 || limit > 200 {
		return nil, fmt.Errorf("render limit must be between 1 and 200, got %d", limit)
	}
	report, err := loadReport(driftPath)
	if err != nil {
		return nil, err
	}
	actions, counts, err := loadPlanActions(planPath)
	if err != nil {
		return nil, err
	}

	type row struct {
		product, resource, binding, kind, api, param, apiType, action string
	}
	rows := make([]row, 0, len(report.Items)+len(report.Skipped))
	for _, item := range report.Items {
		rows = append(rows, row{
			product: item.Product, resource: item.Resource, binding: item.Binding,
			kind: item.Kind, api: item.API, param: item.Param, apiType: item.Type,
			action: actions[planItemKey(item.Product, item.Resource, item.Binding, item.API, item.Param)],
		})
	}
	for _, skipped := range report.Skipped {
		rows = append(rows, row{
			product: skipped.Product, resource: skipped.Resource, binding: skipped.Binding,
			kind: "skipped", api: skipped.API, param: "-", apiType: "-", action: skipped.Reason,
		})
	}

	var out strings.Builder
	fmt.Fprintf(&out, "%s\n\n# OpenAPI drift monitor\n\n", DriftIssueMarker)
	fmt.Fprintf(&out, "- bindings checked: %d\n", report.BindingsChecked)
	fmt.Fprintf(&out, "- baseline gaps: %d\n", report.BaselineGaps)
	fmt.Fprintf(&out, "- drift: missing=%d, removed=%d, uncovered=%d, skipped=%d\n",
		len(report.Missing()), len(report.Removed()), len(report.Uncovered()), len(report.Skipped))
	if planPath != "" {
		fmt.Fprintf(&out, "- sync plan: patched=%d, flagged=%d, already-synced=%d\n",
			counts["patched"], counts["flagged"], counts["already-synced"])
	}
	out.WriteString("\n| product | resource | binding | kind | API | parameter | type | sync action |\n")
	out.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	shown := len(rows)
	if shown > limit {
		shown = limit
	}
	for _, item := range rows[:shown] {
		fmt.Fprintf(&out, "| %s | %s | %s | %s | %s | %s | %s | %s |\n",
			markdownCell(item.product), markdownCell(item.resource), markdownCell(item.binding), markdownCell(item.kind),
			markdownCell(item.api), markdownCell(item.param), markdownCell(item.apiType), markdownCell(item.action))
	}
	if len(rows) > shown {
		fmt.Fprintf(&out, "\nShowing %d of %d rows. See the workflow artifact for the complete JSON report and sync plan.\n", shown, len(rows))
	} else {
		out.WriteString("\nSee the workflow artifact for the complete JSON report and sync plan.\n")
	}
	return []byte(out.String()), nil
}

func loadPlanActions(path string) (map[string]string, map[string]int, error) {
	actions := map[string]string{}
	counts := map[string]int{}
	if path == "" {
		return actions, counts, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read sync plan %s: %w", path, err)
	}
	var plan planFile
	if err := json.Unmarshal(raw, &plan); err != nil {
		return nil, nil, fmt.Errorf("parse sync plan %s: %w", path, err)
	}
	for _, item := range plan.Items {
		action := item.Action
		if item.FlagKind != "" {
			action += ": " + item.FlagKind
		}
		actions[planItemKey(item.Product, item.Resource, item.Binding, item.API, item.Param)] = action
		counts[item.Action]++
	}
	return actions, counts, nil
}

func planItemKey(product, resource, binding, api, param string) string {
	return strings.Join([]string{product, resource, binding, api, param}, "\x00")
}

func markdownCell(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	const maxRunes = 120
	if utf8.RuneCountInString(value) > maxRunes {
		runes := []rune(value)
		value = string(runes[:maxRunes-1]) + "…"
	}
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "|", "\\|")
	if value == "" {
		return "-"
	}
	return value
}
