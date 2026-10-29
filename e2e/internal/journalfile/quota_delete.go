package journalfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalcmd"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/google/shlex"
)

// IsQuotaDeleteCandidate is deliberately conservative for unattended journal
// entries. Any delete in the sandbox Quota command path must prove exact
// ownership, including aliases and interspersed flags.
func IsQuotaDeleteCandidate(command string) bool {
	return isSandboxDeleteCandidate(command, "quota")
}

// IsQuotaCommandCandidate identifies the resource family before a journal
// command can reach generic delete replay, including malformed action paths.
func IsQuotaCommandCandidate(command string) bool {
	args, err := shlex.Split(command)
	if err != nil {
		args = strings.Fields(command)
	}
	if len(args) == 0 || args[0] != "ecctl" {
		return false
	}
	product := false
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			key, _, inline := strings.Cut(strings.TrimLeft(arg, "-"), "=")
			switch key {
			case "force", "openapi", "no-wait", "json", "agent-envelope", "no-color", "help", "version", "public", "dry-run", "h", "v":
				continue
			case "region", "profile", "lang", "output", "name", "team-id", "runtime-config", "build-config", "timeout", "cpu-count", "memory-mb", "from-image", "from-template", "from-image-registry", "steps", "start-cmd", "ready-cmd", "tag", "step", "build-id", "cursor", "limit", "next-token", "filter", "alias", "disk-size-mb", "fields", "description", "resource-group", "request", "api-param", "aliyun-arg", "status", "policy-name", "policy-type", "fallback-default-version", "cluster", "instance-name", "cpu-cores", "memory-gb":
				if !inline {
					i++
				}
				continue
			default:
				if product {
					return true
				}
				return false
			}
		}
		if !product {
			if arg != "sandbox" && arg != "sbx" {
				return false
			}
			product = true
		} else {
			return arg == "quota"
		}
	}
	return false
}

func IsTeamDeleteCandidate(command string) bool {
	return isSandboxDeleteCandidate(command, "team")
}

func isSandboxDeleteCandidate(command, wantedResource string) bool {
	args, err := shlex.Split(command)
	if err != nil || len(args) == 0 || args[0] != "ecctl" {
		return false
	}
	product, resource := "", ""
	afterDash := false
	for index := 1; index < len(args); index++ {
		arg := args[index]
		if arg == "--" && !afterDash {
			afterDash = true
			continue
		}
		if !afterDash && strings.HasPrefix(arg, "-") {
			key, _, inline := strings.Cut(strings.TrimLeft(arg, "-"), "=")
			switch key {
			case "force", "openapi", "no-wait", "json", "agent-envelope", "no-color", "help", "version", "public", "dry-run", "h", "v":
				continue
			case "region", "profile", "lang", "output", "name", "team-id", "runtime-config", "build-config", "timeout", "cpu-count", "memory-mb", "from-image", "from-template", "from-image-registry", "steps", "start-cmd", "ready-cmd", "tag", "step", "build-id", "cursor", "limit", "next-token", "filter", "alias", "disk-size-mb", "fields", "description", "resource-group", "request", "api-param", "aliyun-arg", "status", "policy-name", "policy-type", "fallback-default-version", "cluster", "instance-name", "cpu-cores", "memory-gb":
				if !inline {
					index++
					if index >= len(args) {
						return false
					}
				}
				continue
			default:
				return resource == wantedResource // a known resource path must fail closed
			}
		}
		if product == "" {
			if arg != "sandbox" && arg != "sbx" {
				return false
			}
			product = arg
		} else if resource == "" {
			if arg != wantedResource {
				return false
			}
			resource = arg
		} else {
			return arg == "delete" || arg == "rm" || arg == "remove"
		}
	}
	return false
}

// ErrQuotaUpdatePending fences cleanup when UpdateQuota has no confirmed
// completed result. The exact child finalizer remains as recovery evidence.
var ErrQuotaUpdatePending = errors.New("pending Quota update")

// CheckQuotaUpdatePending validates the pending child against its durable Team
// parent before any replay or runtime cleanup may launch a delete.
func CheckQuotaUpdatePending(journal report.CleanupJournal) error {
	for index, entry := range journal.Entries {
		if !entry.QuotaUpdatePending {
			continue
		}
		if !IsSandboxQuotaDelete(entry.Teardown) || !entry.QuotaReserved || index == 0 {
			return fmt.Errorf("pending Quota update lacks an exact child finalizer")
		}
		args, _ := shlex.Split(entry.Teardown)
		parentCount, targetParents := 0, 0
		for _, team := range journal.Entries[:index] {
			id, err := TeamDeleteTarget(team.Teardown)
			if err != nil || id != args[4] {
				continue
			}
			targetParents++
			if team.TeamCreateConfirmed && !team.TeamReplayAttempted && team.Scope == entry.Scope && team.RegionRole == entry.RegionRole && team.Region == entry.Region && team.ExecutionID == entry.ExecutionID {
				parentCount++
			}
		}
		if parentCount != 1 || targetParents != 1 || entry.RegionRole != journal.RegionRole || entry.Region != journal.Region || entry.ExecutionID != journal.ExecutionID || journal.Version != 2 || journal.RunID == "" || journal.Surface == "" || journal.EcctlBin == "" {
			return fmt.Errorf("pending Quota update differs from its durable Team parent")
		}
		return ErrQuotaUpdatePending
	}
	return nil
}

// CheckOwnedQuotaDelete requires one settled child and one unattempted Team
// parent in the same run, execution, region and scope. Callers hold the replay
// fence while they validate, invoke and consume this exact child.
func CheckOwnedQuotaDelete(journal report.CleanupJournal, child report.Resource) error {
	if !IsSandboxQuotaDelete(child.Teardown) || child.Scope == "" || !child.QuotaReserved || child.QuotaUpdatePending || child.CacheCreatePending || child.TeamReplayAttempted || journal.Version != 2 || journal.RunID == "" || journal.ExecutionID == "" || journal.Surface == "" || journal.EcctlBin == "" || journal.Region == "" || journal.RegionRole == "" || child.Region != journal.Region || child.RegionRole != journal.RegionRole || child.ExecutionID != journal.ExecutionID {
		return fmt.Errorf("Quota delete lacks settled run-owned reservation")
	}
	args, _ := shlex.Split(child.Teardown)
	childCount, parentCount, targetChildren, targetParents, childIndex := 0, 0, 0, 0, -1
	for index, entry := range journal.Entries {
		if entry.Teardown == child.Teardown {
			targetChildren++
		}
		teamID, teamErr := TeamDeleteTarget(entry.Teardown)
		if teamErr == nil && teamID == args[4] {
			targetParents++
		}
		if entry == child {
			childCount++
			childIndex = index
		}
		if teamErr == nil && teamID == args[4] && entry.TeamCreateConfirmed && !entry.TeamReplayAttempted && !entry.CacheCreatePending && !entry.QuotaUpdatePending && entry.Scope == child.Scope && entry.RegionRole == child.RegionRole && entry.Region == child.Region && entry.ExecutionID == child.ExecutionID {
			parentCount++
			if childIndex >= 0 && index >= childIndex {
				return fmt.Errorf("Quota delete parent ordering changed")
			}
		}
	}
	if childCount != 1 || parentCount != 1 || targetChildren != 1 || targetParents != 1 {
		return fmt.Errorf("Quota delete requires one durable owned Team and settled child")
	}
	return nil
}

// TeamDeleteTarget accepts only a first-class Team delete with a safe target,
// one positive timeout, and optional JSON output. Its preserved spelling is
// the same contract for Team creation, runtime cleanup, and replay.
func TeamDeleteTarget(command string) (string, error) {
	if journalcmd.ValidateDelete(command) != nil {
		return "", fmt.Errorf("Team finalizer must be a delete command")
	}
	args, err := shlex.Split(command)
	if err != nil || len(args) < 6 || args[0] != "ecctl" || (args[1] != "sandbox" && args[1] != "sbx") || args[2] != "team" || args[3] != "delete" {
		return "", fmt.Errorf("Team finalizer requires the Team delete path")
	}
	id := ""
	seenTimeout, seenOutput := false, false
	for i := 4; i < len(args); i++ {
		key, value, inline := strings.Cut(args[i], "=")
		switch key {
		case "--timeout", "--output":
			if key == "--timeout" && seenTimeout || key == "--output" && seenOutput {
				return "", fmt.Errorf("duplicate Team finalizer flag %s", key)
			}
			if !inline {
				i++
				if i >= len(args) {
					return "", fmt.Errorf("missing Team finalizer flag value")
				}
				value = args[i]
			}
			if key == "--timeout" {
				duration, err := time.ParseDuration(value)
				if err != nil || duration <= 0 {
					return "", fmt.Errorf("invalid Team finalizer timeout")
				}
				seenTimeout = true
			} else {
				if value != "json" {
					return "", fmt.Errorf("Team finalizer requires JSON output")
				}
				seenOutput = true
			}
		default:
			if inline || strings.HasPrefix(key, "-") || id != "" || !IsSandboxQuotaDelete("ecctl sandbox quota delete "+key+" --output=json") {
				return "", fmt.Errorf("invalid Team finalizer target or flag")
			}
			id = key
		}
	}
	if id == "" || !seenTimeout {
		return "", fmt.Errorf("Team finalizer requires an ID and timeout")
	}
	return id, nil
}

func IsQuotaParentTeamDelete(command string) bool {
	_, err := TeamDeleteTarget(command)
	return err == nil
}

// TeamDeleteCandidateTarget recognizes ordinary first-class Team deletes,
// including the no-timeout shared-stack form. It does not grant ownership.
func TeamDeleteCandidateTarget(command string) (string, error) {
	return TeamDeleteCandidateTargetForRegion(command, "")
}

// TeamDeleteCandidateTargetForRegion accepts an ordinary Team finalizer's
// explicit region only when it agrees with the journal/runner role. The
// protected confirmed-parent grammar remains TeamDeleteTarget.
func TeamDeleteCandidateTargetForRegion(command, expectedRegion string) (string, error) {
	if id, err := TeamDeleteTarget(command); err == nil {
		return id, nil
	}
	args, err := shlex.Split(command)
	if err != nil || len(args) < 5 || args[0] != "ecctl" {
		return "", fmt.Errorf("invalid Team delete path")
	}
	id, pathParts := "", 0
	seen := map[string]bool{}
	for i := 1; i < len(args); i++ {
		if args[i] == "--" {
			return "", fmt.Errorf("Team delete passthrough is unsupported")
		}
		key, value, inline := strings.Cut(args[i], "=")
		switch key {
		case "--timeout", "--output", "--region", "--lang":
			if key == "--timeout" && pathParts != 3 {
				return "", fmt.Errorf("Team delete timeout must follow the action")
			}
			if seen[key] {
				return "", fmt.Errorf("duplicate Team delete flag %s", key)
			}
			seen[key] = true
			if !inline {
				i++
				if i >= len(args) {
					return "", fmt.Errorf("missing Team delete flag value")
				}
				value = args[i]
			}
			if value == "" {
				return "", fmt.Errorf("empty Team delete flag value")
			}
			if key == "--timeout" {
				if duration, err := time.ParseDuration(value); err != nil || duration <= 0 {
					return "", fmt.Errorf("invalid Team delete timeout")
				}
			} else if key == "--output" {
				if value != "json" {
					return "", fmt.Errorf("invalid Team delete output")
				}
			} else if key == "--region" {
				if expectedRegion == "" || value != expectedRegion {
					return "", fmt.Errorf("Team delete region differs from journal region")
				}
			}
		case "--json", "--no-color", "--agent-envelope":
			if seen[key] {
				return "", fmt.Errorf("duplicate Team delete flag %s", key)
			}
			seen[key] = true
			if inline {
				if _, err := strconv.ParseBool(value); err != nil {
					return "", fmt.Errorf("invalid Team delete boolean flag %s", key)
				}
			}
		default:
			if inline || strings.HasPrefix(key, "-") {
				return "", fmt.Errorf("invalid Team delete flag or target")
			}
			switch pathParts {
			case 0:
				if key != "sandbox" && key != "sbx" {
					return "", fmt.Errorf("invalid Team delete product")
				}
			case 1:
				if key != "team" {
					return "", fmt.Errorf("invalid Team delete resource")
				}
			case 2:
				if key != "delete" && key != "rm" && key != "remove" {
					return "", fmt.Errorf("invalid Team delete action")
				}
			default:
				if id != "" || !IsSandboxQuotaDelete("ecctl sandbox quota delete "+key+" --output=json") {
					return "", fmt.Errorf("invalid Team delete target")
				}
				id = key
			}
			if pathParts < 3 {
				pathParts++
			}
		}
	}
	if pathParts != 3 || id == "" {
		return "", fmt.Errorf("Team delete requires a safe target")
	}
	return id, nil
}

// RawTeamDeleteTarget reads only inline, literal raw DeleteTeam requests. An
// @file, passthrough argument, profile switch or ambiguous override is unsafe
// to classify before the provider subprocess.
func RawTeamDeleteTarget(command, expectedRegion string) (string, error) {
	args, err := shlex.Split(command)
	if err != nil || len(args) < 5 || args[0] != "ecctl" {
		return "", fmt.Errorf("invalid raw Team delete path")
	}
	var request map[string]any
	seenRequest, seenParam, seenRegion := false, false, false
	region, parameterID := "", ""
	positionals := make([]string, 0, 3)
	for i := 1; i < len(args); i++ {
		if !strings.HasPrefix(args[i], "-") {
			positionals = append(positionals, args[i])
			continue
		}
		key, value, inline := strings.Cut(args[i], "=")
		if key != "--request" && key != "--api-param" && key != "--region" && key != "--output" && key != "-o" {
			return "", fmt.Errorf("unsupported raw Team delete flag")
		}
		if key == "-o" && len(positionals) == 0 {
			return "", fmt.Errorf("raw Team delete output shorthand must follow call")
		}
		if !inline {
			i++
			if i >= len(args) {
				return "", fmt.Errorf("missing raw Team delete flag value")
			}
			value = args[i]
		}
		switch key {
		case "--request":
			if seenRequest || strings.HasPrefix(value, "@") || json.Unmarshal([]byte(value), &request) != nil || request == nil {
				return "", fmt.Errorf("invalid raw Team delete request")
			}
			seenRequest = true
		case "--api-param":
			if seenParam {
				return "", fmt.Errorf("duplicate raw Team delete parameter")
			}
			name, raw, ok := strings.Cut(value, "=")
			if !ok || name != "teamID" || raw == "" {
				return "", fmt.Errorf("invalid raw Team delete parameter")
			}
			parameterID = raw
			var decoded any
			if json.Unmarshal([]byte(strings.TrimSpace(raw)), &decoded) == nil {
				stringID, ok := decoded.(string)
				if !ok {
					return "", fmt.Errorf("raw Team delete parameter must be a string")
				}
				parameterID = stringID
			}
			seenParam = true
		case "--region":
			if seenRegion || value == "" {
				return "", fmt.Errorf("invalid raw Team delete region")
			}
			region, seenRegion = value, true
		case "--output", "-o":
			if value != "json" {
				return "", fmt.Errorf("invalid raw Team delete output")
			}
		}
	}
	if len(positionals) != 3 || positionals[0] != "call" || !strings.EqualFold(strings.TrimSpace(positionals[1]), "fcsandbox") || !strings.EqualFold(strings.TrimSpace(positionals[2]), "DeleteTeam") {
		return "", fmt.Errorf("invalid raw Team delete path")
	}
	if region != "" && region != expectedRegion {
		return "", fmt.Errorf("raw Team delete region differs from cleanup region")
	}
	if request == nil {
		request = map[string]any{}
	}
	if seenParam {
		request["teamID"] = parameterID
	}
	for key := range request {
		if key != "teamID" && key != "RegionId" {
			return "", fmt.Errorf("unsupported raw Team delete request field")
		}
	}
	if value, exists := request["RegionId"]; exists {
		requestedRegion, ok := value.(string)
		if !ok || requestedRegion != expectedRegion {
			return "", fmt.Errorf("raw Team delete request region differs from cleanup region")
		}
	}
	id, ok := request["teamID"].(string)
	if !ok || !IsSandboxQuotaDelete("ecctl sandbox quota delete "+id+" --output=json") {
		return "", fmt.Errorf("raw Team delete lacks a safe target")
	}
	return id, nil
}

// IsSandboxQuotaDelete recognizes the exact run-owned Quota finalizer spelling.
// Reservation verifies Team ownership separately before persisting this command.
// Both in-process cleanup and crash replay use this same child failure fence.
func IsSandboxQuotaDelete(command string) bool {
	_, err := SandboxQuotaDeleteTarget(command)
	return err == nil
}

// SandboxQuotaDeleteTarget parses the exact owned delete grammar and returns
// the Team ID independently of command spacing or shell quoting.
func SandboxQuotaDeleteTarget(command string) (string, error) {
	if journalcmd.ValidateDelete(command) != nil {
		return "", fmt.Errorf("Quota finalizer must be a delete command")
	}
	args, err := shlex.Split(command)
	if err != nil || len(args) != 6 || args[0] != "ecctl" || args[1] != "sandbox" || args[2] != "quota" || args[3] != "delete" || args[5] != "--output=json" {
		return "", fmt.Errorf("Quota finalizer requires the exact delete path")
	}
	id := args[4]
	if id == "" || id[0] == '-' || id[0] == '_' {
		return "", fmt.Errorf("invalid Quota target")
	}
	for _, ch := range id {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
			return "", fmt.Errorf("invalid Quota target")
		}
	}
	return id, nil
}
