// Package regionselect classifies run failures that justify trying the next
// configured region candidate. It intentionally keeps the allowlist narrow so
// credentials, permission, quota, assertion, and cleanup failures are not
// hidden by a region retry.
package regionselect

import (
	"fmt"
	"strings"

	"ecctl/e2e/internal/report"
)

var unavailableMarkers = []string{
	"invalidregionid",
	"invalidregion",
	"regionsnotsupport",
	"regionnotsupported",
	"unsupportedregion",
	"forbidden.region",
	"forbiddenregion",
	"unsupported.region",
	"invalidzoneid",
	"zonenotsupported",
	"unsupportedzone",
	"no compatible ecs test combination found in region",
	"no compatible ecs test combination found in explicit zone",
	"no ecs zone supporting vswitch creation found in region",
	"no creatable ack cluster type/version found in region",
	"no compatible creatable ack cluster version found in region",
	"no compatible lingjun cluster type",
	"no compatible lingjun node profile found in region",
}

var nonRetryableMarkers = []string{
	"accessdenied",
	"unauthorized",
	"forbidden",
	"invalidaccesskey",
	"signaturedoesnotmatch",
	"missingsecuritytoken",
	"securitytoken",
	"credential",
	"cleanup",
	"teardown",
	"quota",
	"limitexceed",
}

// Classify returns true only when every observed failure is a known
// region/zone-unavailable failure. A run-level setup error may be supplied
// when parameter resolution fails before a report contains any cases.
func Classify(run *report.Run, runErr error) (bool, string) {
	var failures []string
	if runErr != nil {
		failures = append(failures, runErr.Error())
	}
	if run != nil {
		for _, c := range run.Cases {
			if c.Status != report.StatusFail && c.Status != report.StatusError {
				continue
			}
			if c.Error != "" {
				failures = append(failures, c.Error)
			}
			for _, step := range c.Steps {
				if step.Status == report.StatusFail || step.Status == report.StatusError {
					failures = append(failures, step.Error, step.Stdout, step.Stderr)
				}
			}
		}
	}
	var reasons []string
	for _, failure := range failures {
		failure = strings.TrimSpace(failure)
		if failure == "" {
			continue
		}
		lower := strings.ToLower(strings.ReplaceAll(failure, "_", ""))
		for _, marker := range nonRetryableMarkers {
			if marker == "forbidden" {
				continue
			}
			if strings.Contains(lower, marker) {
				return false, fmt.Sprintf("non-region failure: %s", failure)
			}
		}
		if strings.Contains(lower, "forbidden") && !strings.Contains(lower, "forbidden.region") && !strings.Contains(lower, "forbiddenregion") {
			return false, fmt.Sprintf("non-region failure: %s", failure)
		}
		matched := false
		for _, marker := range unavailableMarkers {
			if strings.Contains(lower, marker) {
				matched = true
				reasons = append(reasons, failure)
				break
			}
		}
		if matched {
			continue
		}
		if !matched {
			return false, fmt.Sprintf("non-region failure: %s", failure)
		}
	}
	if len(reasons) == 0 {
		return false, ""
	}
	return true, reasons[0]
}
