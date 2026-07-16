package report

// Aggregate builds one top-level report from independently executed units.
// Legacy single-region fields remain populated only when there is one unit.
func Aggregate(runID, surface, ecctlBin string, executions []Execution) *Run {
	run := &Run{SchemaVersion: CurrentSchemaVersion, RunID: runID, Surface: surface, EcctlBin: ecctlBin, Executions: executions}
	for i := range run.Executions {
		execution := &run.Executions[i]
		execution.Summary = summarizeCases(execution.Cases)
		if run.StartedAt.IsZero() || (!execution.StartedAt.IsZero() && execution.StartedAt.Before(run.StartedAt)) {
			run.StartedAt = execution.StartedAt
		}
		if execution.FinishedAt.After(run.FinishedAt) {
			run.FinishedAt = execution.FinishedAt
		}
		for caseIndex := range execution.Cases {
			execution.Cases[caseIndex].ExecutionID = execution.ID
			execution.Cases[caseIndex].Regions = cloneRegions(execution.Regions)
			item := execution.Cases[caseIndex]
			item.ExecutionID = execution.ID
			item.Regions = cloneRegions(execution.Regions)
			run.Cases = append(run.Cases, item)
		}
		for _, source := range execution.Manifest {
			item := source
			if item.ExecutionID == "" {
				item.ExecutionID = execution.ID
			}
			run.Manifest = append(run.Manifest, item)
		}
	}
	run.Summary = summarizeCases(run.Cases)
	if len(run.Executions) == 1 {
		execution := run.Executions[0]
		run.Region = execution.Regions["primary"]
		run.Parameters = execution.Parameters
		for _, attempt := range execution.Attempts {
			run.RegionAttempts = append(run.RegionAttempts, RegionAttempt{
				Region: attempt.Regions["primary"], Status: attempt.Status, Reason: attempt.Reason,
			})
		}
	}
	return run
}

func summarizeCases(cases []Case) Summary {
	summary := Summary{Cases: len(cases)}
	for _, item := range cases {
		switch item.Status {
		case StatusPass:
			summary.Passed++
		case StatusSkipped:
			summary.Skipped++
		default:
			summary.Failed++
		}
	}
	return summary
}

func cloneRegions(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	clone := make(map[string]string, len(source))
	for role, region := range source {
		clone[role] = region
	}
	return clone
}
