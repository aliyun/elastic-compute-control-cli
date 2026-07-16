package report

import (
	"encoding/json"
	"fmt"
	"os"
)

type CheckOptions struct {
	Failed int
}

type CheckReport struct {
	Cases   int                    `json:"cases"`
	Passed  int                    `json:"passed"`
	Failed  int                    `json:"failed"`
	Skipped int                    `json:"skipped"`
	Invalid int                    `json:"invalid"`
	Errors  []CheckValidationError `json:"errors"`
}

type CheckValidationError struct {
	Case    string `json:"case,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func CheckFile(path string, opts CheckOptions) (*CheckReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var run Run
	if err := json.Unmarshal(data, &run); err != nil {
		return nil, err
	}
	return CheckRun(&run, opts), nil
}

func CheckRun(run *Run, opts CheckOptions) *CheckReport {
	rep := &CheckReport{
		Cases:   run.Summary.Cases,
		Passed:  run.Summary.Passed,
		Failed:  run.Summary.Failed,
		Skipped: run.Summary.Skipped,
	}
	if run.Summary.Failed > opts.Failed {
		rep.add("", "too_many_failed", fmt.Sprintf("summary failed=%d exceeds allowed %d", run.Summary.Failed, opts.Failed))
	}
	for _, c := range run.Cases {
		if c.Status != StatusPass && c.Status != StatusSkipped {
			rep.add(c.Name, "case_not_passed", fmt.Sprintf("case status is %s", c.Status))
		}
	}
	rep.Invalid = len(rep.Errors)
	return rep
}

func (r *CheckReport) add(name, code, msg string) {
	r.Errors = append(r.Errors, CheckValidationError{Case: name, Code: code, Message: msg})
}
