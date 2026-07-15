package report

import (
	_ "embed"
	"html/template"
	"os"
)

//go:embed template.html
var htmlTemplate string

var htmlTmpl = template.Must(template.New("report").Funcs(template.FuncMap{
	"secs":    humanDuration,
	"icon":    statusLabel, // HTML stays emoji-free; the colored CSS tag conveys status
	"bucket":  statusBucket,
	"regions": formatRegionsPlain,
}).Parse(htmlTemplate))

// statusLabel is the plain-text (emoji-free) status shown in the HTML report.
func statusLabel(s string) string {
	switch s {
	case StatusPass:
		return "pass"
	case StatusSkipped:
		return "skip"
	default:
		return s
	}
}

// statusBucket collapses statuses into the three filter buckets used by the UI.
func statusBucket(s string) string {
	switch s {
	case StatusPass:
		return "pass"
	case StatusSkipped:
		return "skip"
	default:
		return "fail"
	}
}

// WriteHTML renders a single self-contained HTML report.
func WriteHTML(r *Run, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return htmlTmpl.Execute(f, r)
}
