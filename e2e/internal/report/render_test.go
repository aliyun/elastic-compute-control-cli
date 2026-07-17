package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMultiExecutionRenderersExposeExecutionAndRegionMapping(t *testing.T) {
	started := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	run := Aggregate("run-1", "public", "ecctl-public", []Execution{
		{
			ID: "execution-01", Signature: "primary=[ecs.image]",
			Regions:   map[string]string{"primary": "cn-hangzhou"},
			Attempts:  []ExecutionAttempt{{Regions: map[string]string{"primary": "cn-hangzhou"}, Status: StatusPass}},
			StartedAt: started, FinishedAt: started.Add(time.Minute),
			Cases: []Case{{Name: "export", Resource: "ecs/image", Status: StatusPass}},
		},
		{
			ID: "execution-02", Signature: "primary=[];destination=[]!=primary",
			Regions:   map[string]string{"primary": "cn-hangzhou", "destination": "cn-heyuan"},
			Attempts:  []ExecutionAttempt{{Regions: map[string]string{"primary": "cn-hangzhou", "destination": "cn-heyuan"}, Status: StatusPass}},
			StartedAt: started, FinishedAt: started.Add(2 * time.Minute),
			Cases: []Case{{Name: "copy", Resource: "ecs/image", Status: StatusPass}},
		},
	})

	summaryPath := filepath.Join(t.TempDir(), "summary.md")
	if err := WriteSummary(run, summaryPath); err != nil {
		t.Fatal(err)
	}
	summary, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Execution units:", "`execution-02`", "destination=`cn-heyuan`", "| Case | Resource | Execution |"} {
		if !strings.Contains(string(summary), want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}

	htmlPath := filepath.Join(t.TempDir(), "report.html")
	if err := WriteHTML(run, htmlPath); err != nil {
		t.Fatal(err)
	}
	html, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"execution-02", "destination", "cn-heyuan"} {
		if !strings.Contains(string(html), want) {
			t.Fatalf("html missing %q", want)
		}
	}
}
