package report

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// humanDuration formats a millisecond duration in a human-readable way:
// "850ms", "5.2s", "2m 5s", "1h 3m".
func humanDuration(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", ms)
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Hour:
		d = d.Round(time.Second)
		return fmt.Sprintf("%dm %ds", d/time.Minute, (d%time.Minute)/time.Second)
	default:
		d = d.Round(time.Minute)
		return fmt.Sprintf("%dh %dm", d/time.Hour, (d%time.Hour)/time.Minute)
	}
}

// WriteJSON writes the machine-readable report.
func WriteJSON(r *Run, path string) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// --- JUnit ---

type junitSuites struct {
	XMLName  xml.Name     `xml:"testsuites"`
	Tests    int          `xml:"tests,attr"`
	Failures int          `xml:"failures,attr"`
	Skipped  int          `xml:"skipped,attr"`
	Suites   []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Skipped  int         `xml:"skipped,attr"`
	Cases    []junitCase `xml:"testcase"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Time      float64       `xml:"time,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	Skipped   *struct{}     `xml:"skipped,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

// WriteJUnit writes a JUnit XML report for the GitHub test UI.
func WriteJUnit(r *Run, path string) error {
	suite := junitSuite{Name: "ecctl-e2e"}
	for _, c := range r.Cases {
		jc := junitCase{Name: c.Name, Classname: c.Resource, Time: float64(c.DurationMs) / 1000}
		switch c.Status {
		case StatusSkipped:
			jc.Skipped = &struct{}{}
			suite.Skipped++
		case StatusPass:
		default:
			jc.Failure = &junitFailure{Message: c.Status, Text: caseFailureText(c)}
			suite.Failures++
		}
		suite.Tests++
		suite.Cases = append(suite.Cases, jc)
	}
	doc := junitSuites{Tests: suite.Tests, Failures: suite.Failures, Skipped: suite.Skipped, Suites: []junitSuite{suite}}
	b, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(xml.Header), append(b, '\n')...), 0o644)
}

func caseFailureText(c Case) string {
	var b strings.Builder
	if c.Error != "" {
		fmt.Fprintf(&b, "%s\n", c.Error)
	}
	for _, s := range c.Steps {
		if s.Status == StatusPass || s.Status == StatusSkipped {
			continue
		}
		fmt.Fprintf(&b, "step %q: exit=%d (want %d)\n  %s\n", s.Name, s.Exit, s.WantExit, s.Command)
		for _, ck := range s.Checks {
			if !ck.OK {
				fmt.Fprintf(&b, "  ✗ %s: %s\n", ck.Path, ck.Detail)
			}
		}
		if s.Stderr != "" {
			fmt.Fprintf(&b, "  stderr: %s\n", s.Stderr)
		}
	}
	return b.String()
}

// --- GitHub step summary (GFM) ---

// WriteSummary appends a compact markdown summary (intended for
// $GITHUB_STEP_SUMMARY). It appends so a later workflow step can add a link.
func WriteSummary(r *Run, path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	banner := "✅ PASS"
	if r.Failed() {
		banner = "❌ FAIL"
	}
	fmt.Fprintf(f, "## ecctl-e2e %s\n\n", banner)
	if len(r.Executions) > 1 {
		fmt.Fprintf(f, "**%d passed · %d failed · %d skipped** of %d cases — %d execution units, run `%s`\n\n",
			r.Summary.Passed, r.Summary.Failed, r.Summary.Skipped, r.Summary.Cases, len(r.Executions), r.RunID)
	} else {
		fmt.Fprintf(f, "**%d passed · %d failed · %d skipped** of %d cases — region `%s`, run `%s`\n\n",
			r.Summary.Passed, r.Summary.Failed, r.Summary.Skipped, r.Summary.Cases, r.Region, r.RunID)
	}
	if len(r.Executions) > 1 {
		fmt.Fprintln(f, "Execution units:")
		for _, execution := range r.Executions {
			fmt.Fprintf(f, "- `%s`: %s — %s\n", execution.ID, execution.Signature, formatRegionsMarkdown(execution.Regions))
			for _, attempt := range execution.Attempts {
				if attempt.Reason == "" {
					fmt.Fprintf(f, "  - %s: %s\n", formatRegionsMarkdown(attempt.Regions), attempt.Status)
				} else {
					fmt.Fprintf(f, "  - %s: %s — %s\n", formatRegionsMarkdown(attempt.Regions), attempt.Status, attempt.Reason)
				}
			}
		}
		fmt.Fprintln(f)
	}
	if len(r.RegionAttempts) > 1 {
		fmt.Fprintln(f, "Region attempts:")
		for _, attempt := range r.RegionAttempts {
			if attempt.Reason == "" {
				fmt.Fprintf(f, "- `%s`: %s\n", attempt.Region, attempt.Status)
			} else {
				fmt.Fprintf(f, "- `%s`: %s — %s\n", attempt.Region, attempt.Status, attempt.Reason)
			}
		}
		fmt.Fprintln(f)
	}
	if len(r.Executions) > 1 {
		fmt.Fprintln(f, "| Case | Resource | Execution | Status | Steps | Duration |")
		fmt.Fprintln(f, "|---|---|---|---|---:|---:|")
	} else {
		fmt.Fprintln(f, "| Case | Resource | Status | Steps | Duration |")
		fmt.Fprintln(f, "|---|---|---|---:|---:|")
	}
	for _, c := range r.Cases {
		if len(r.Executions) > 1 {
			fmt.Fprintf(f, "| %s | `%s` | `%s` | %s | %d | %s |\n",
				c.Name, c.Resource, c.ExecutionID, statusIcon(c.Status), len(c.Steps), humanDuration(c.DurationMs))
		} else {
			fmt.Fprintf(f, "| %s | `%s` | %s | %d | %s |\n",
				c.Name, c.Resource, statusIcon(c.Status), len(c.Steps), humanDuration(c.DurationMs))
		}
	}
	fmt.Fprintln(f)
	return nil
}

func formatRegionsMarkdown(regions map[string]string) string {
	roles := sortedRegionRoles(regions)
	parts := make([]string, 0, len(roles))
	for _, role := range roles {
		parts = append(parts, fmt.Sprintf("%s=`%s`", role, regions[role]))
	}
	return strings.Join(parts, ", ")
}

func formatRegionsPlain(regions map[string]string) string {
	roles := sortedRegionRoles(regions)
	parts := make([]string, 0, len(roles))
	for _, role := range roles {
		parts = append(parts, fmt.Sprintf("%s=%s", role, regions[role]))
	}
	return strings.Join(parts, ", ")
}

func sortedRegionRoles(regions map[string]string) []string {
	roles := make([]string, 0, len(regions))
	for role := range regions {
		roles = append(roles, role)
	}
	sort.Slice(roles, func(i, j int) bool {
		if roles[i] == roles[j] {
			return false
		}
		if roles[i] == "primary" {
			return true
		}
		if roles[j] == "primary" {
			return false
		}
		return roles[i] < roles[j]
	})
	return roles
}

func statusIcon(s string) string {
	switch s {
	case StatusPass:
		return "✅ pass"
	case StatusSkipped:
		return "⏭️ skip"
	default:
		return "❌ " + s
	}
}
