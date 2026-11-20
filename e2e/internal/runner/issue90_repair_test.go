package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/shlex"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

func issue90VolumeTeamReservation(t *testing.T) (*cleanup, *teamCreateIntent, string) {
	t.Helper()
	dir := canonicalCleanupTestDir(t)
	path := filepath.Join(dir, "cleanup.json")
	name := "ecctl-t-0123456789abcdef01234567"
	binding := &report.QuotaCredentialBinding{Profile: "fixture-profile", Identity: execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}}
	cfg := execpkg.Config{Bin: filepath.Join(dir, "ecctl"), Region: "cn-hangzhou"}
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, path,
		report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public", EcctlBinSHA256: strings.Repeat("a", 64)}, func(string, ...any) {})
	cl.volumeFixtureBinding = binding
	cl.volumeFixtureBucket = "ecctl-e2e-20261104t120000-123456abcdef"
	parent, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	cl.journalParents = map[string]os.FileInfo{path: parent}
	journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: cfg.Region, Surface: "public", EcctlBin: cfg.Bin, EcctlBinSHA256: strings.Repeat("a", 64),
		VolumeFixtureBinding: binding, VolumeFixtureBucket: cl.volumeFixtureBucket,
		VolumeTeamCreateIDs: map[string]string{name: "", "ecctl-t-aaaaaaaaaaaaaaaaaaaaaaaa": "other-team"}}
	if err := writeRunnerJournal(path, journal); err != nil {
		t.Fatal(err)
	}
	intent := &teamCreateIntent{Version: 1, RunID: "run", ExecutionID: "execution", Region: cfg.Region, Surface: "public", EcctlBin: cfg.Bin,
		Name: name, Description: "ecctl E2E Team owner=0123456789abcdef01234567", Input: []string{"ecctl", "sandbox", "team", "create", "--name=" + name, "--description=ecctl E2E Team owner=0123456789abcdef01234567", "--plan=eco", "--output=json"}, CaseScope: "case", Teardown: "ecctl sandbox team delete intent-validation --timeout 5m --output=json", VolumeBinding: binding, DispatchProtocol: 1, CreatedAt: time.Now().UTC(),
		path: path + ".team-create-0123456789abcdef01234567.json", journalPath: path, parent: parent}
	body, err := json.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	if err := journalfile.WriteExclusiveDurable(intent.path, append(body, '\n')); err != nil {
		t.Fatal(err)
	}
	return cl, intent, path
}

func issue90TeamReceipt(intent *teamCreateIntent, id string) execpkg.Result {
	return execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"team": map[string]any{"id": id, "name": intent.Name, "description": intent.Description}}}
}

func mockVerifiedTeamRead(t *testing.T, command, binSHA256 string, result execpkg.Result) execpkg.Result {
	t.Helper()
	tokens, err := shlex.Split(command)
	if err != nil || len(tokens) < 2 {
		t.Fatalf("invalid Team read command %q: %v", command, err)
	}
	result.Argv = append([]string(nil), tokens[1:]...)
	result.VerifiedBinSHA256 = binSHA256
	return result
}

func TestRunAdmissionChecksPrimaryVolumeJournalBinaryDigest(t *testing.T) {
	dir := canonicalCleanupTestDir(t)
	path := filepath.Join(dir, "cleanup.json")
	bin := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	digest, err := journalfile.ExecutableSHA256(bin)
	if err != nil {
		t.Fatal(err)
	}
	cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou"}
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, path,
		report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public", EcctlBinSHA256: digest}, func(string, ...any) {})
	journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: cfg.Region, Surface: "public", EcctlBin: bin,
		VolumeFixtureBinding: &report.QuotaCredentialBinding{Profile: "fixture-profile", Identity: execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}},
		VolumeFixtureBucket:  "ecctl-e2e-20261104t120000-123456abcdef"}
	if err := writeRunnerJournal(path, journal); err != nil {
		t.Fatal(err)
	}
	if err := cl.checkDurablePendingQuotaUpdate(context.Background()); err == nil || !strings.Contains(err.Error(), "binary digest") {
		t.Fatalf("Volume journal without original binary digest reached allocation: %v", err)
	}
	journal.EcctlBinSHA256 = strings.Repeat("0", 64)
	if err := writeRunnerJournal(path, journal); err != nil {
		t.Fatal(err)
	}
	if err := cl.checkDurablePendingQuotaUpdate(context.Background()); err == nil || !strings.Contains(err.Error(), "binary digest") {
		t.Fatalf("Volume journal with changed binary digest reached allocation: %v", err)
	}
	journal.EcctlBinSHA256 = digest
	if err := writeRunnerJournal(path, journal); err != nil {
		t.Fatal(err)
	}
	if err := cl.checkDurablePendingQuotaUpdate(context.Background()); err != nil {
		t.Fatalf("matching Volume journal was refused: %v", err)
	}
}

func TestVolumeFixtureDeleteAttemptIsDurableBeforeFirstCall(t *testing.T) {
	for _, kind := range []string{"role", "bucket", "policy", "attachment"} {
		t.Run(kind, func(t *testing.T) {
			dir := canonicalCleanupTestDir(t)
			path := filepath.Join(dir, "cleanup.json")
			bucket := "ecctl-e2e-20261104t120000-123456abcdef"
			binding := &report.QuotaCredentialBinding{Profile: "fixture-profile", Identity: execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}}
			cfg := execpkg.Config{Bin: filepath.Join(dir, "ecctl"), Region: "cn-hangzhou"}
			cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, path,
				report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
			cl.volumeFixtureBinding, cl.volumeFixtureBucket = binding, bucket
			parent, err := os.Stat(dir)
			if err != nil {
				t.Fatal(err)
			}
			cl.journalParents = map[string]os.FileInfo{path: parent}
			command := `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role"}'`
			if kind == "bucket" {
				command = `ecctl call oss DeleteBucket --request '{"Bucket":"` + bucket + `"}' --region cn-hangzhou`
			} else if kind == "policy" {
				command = `ecctl call ram DeletePolicy --region cn-hangzhou --request '{"PolicyName":"` + bucket + `-read"}'`
			}
			item := &cleanupItem{scope: "stack", role: "primary", cmd: command, volumeFixtureKind: kind, volumeFixtureDeleteProtocol: 1}
			if kind == "role" {
				item.volumeFixtureRoleID = "role-created"
			} else if kind == "policy" {
				item.volumeFixturePolicyCreateDate = "2026-11-04T12:00:00Z"
			} else if kind == "bucket" {
				item.volumeFixtureBucketCreationDate = "2026-11-04T12:00:00Z"
			}
			if kind == "attachment" {
				item.cmd = `ecctl call ram DetachPolicyFromRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role","PolicyName":"` + bucket + `-read","PolicyType":"Custom"}'`
			}
			journal, entry, _ := cl.journalValues(item)
			journal.JournalPath = path
			journal.Entries = []report.Resource{entry}
			if err := writeRunnerJournal(path, journal); err != nil {
				t.Fatal(err)
			}
			if err := cl.markVolumeFixtureDeleteAttempt(item); err != nil {
				t.Fatal(err)
			}
			stored, _, err := readRunnerJournal(path)
			if err != nil || len(stored.Entries) != 1 || !stored.Entries[0].VolumeFixtureDeleteAttempted || !item.volumeFixtureDeleteAttempted {
				t.Fatalf("first destructive call lacks durable attempt marker: %+v %v", stored, err)
			}
			if err := cl.markVolumeFixtureDeleteAttempt(&cleanupItem{scope: item.scope, role: item.role, cmd: item.cmd, volumeFixtureKind: kind, volumeFixtureDeleteProtocol: 1}); err == nil {
				t.Fatal("fresh in-memory item could repeat a marked name-based delete")
			}
			previous := writeVolumeFixtureRestoreJournal
			t.Cleanup(func() { writeVolumeFixtureRestoreJournal = previous })
			writeVolumeFixtureRestoreJournal = func(*os.Root, string, report.CleanupJournal) error {
				return errors.New("injected restore durability failure")
			}
			if err := cl.restoreUnstartedVolumeFixtureDeleteAttempt(item); err == nil || !item.volumeFixtureDeleteAttempted {
				t.Fatalf("uncertain marker restoration lost the in-memory fence: %v", err)
			}
			stored, _, err = readRunnerJournal(path)
			if err != nil || !stored.Entries[0].VolumeFixtureDeleteAttempted {
				t.Fatalf("uncertain marker restoration lost the durable fence: %+v %v", stored, err)
			}
			writeVolumeFixtureRestoreJournal = previous
		})
	}
}

func TestOrdinaryFirstRoleCleanupUsesAssignedName(t *testing.T) {
	for _, tc := range []struct {
		name, createdID, currentID string
		unknown, absent            bool
		wantDeleted, falseSuccess  bool
	}{
		{"same RoleId", "role-created", "role-created", false, false, true, false},
		{name: "false success", createdID: "role-created", currentID: "role-created", wantDeleted: true, falseSuccess: true},
		{"different RoleId", "role-created", "role-replacement", false, false, true, false},
		{"unknown read", "role-created", "", true, false, false, false},
		{"missing creation RoleId", "", "role-created", false, false, true, false},
		{"old journal exact absence", "", "", false, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := canonicalCleanupTestDir(t)
			path, bin := filepath.Join(dir, "cleanup.json"), filepath.Join(dir, "ecctl")
			configureQuotaProbeFixture(t, dir, bin)
			bucket := "ecctl-e2e-20261104t120000-123456abcdef"
			cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou"}
			cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, path,
				report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
			grantSyntheticVolumeFixtureScope(t, cl, bucket, "delete")
			if _, err := cl.boundVolumeFixtureConfig(context.Background(), cfg, bucket, time.Second); err != nil {
				t.Fatal(err)
			}
			parent, err := os.Stat(dir)
			if err != nil {
				t.Fatal(err)
			}
			cl.journalParents = map[string]os.FileInfo{path: parent}
			item := &cleanupItem{scope: "stack", role: "primary", cmd: `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role"}'`,
				volumeFixtureKind: "role", volumeFixtureRoleID: tc.createdID, volumeFixtureDeleteProtocol: 1}
			journal, entry, _ := cl.journalValues(item)
			journal.JournalPath, journal.Entries = path, []report.Resource{entry}
			if err := writeRunnerJournal(path, journal); err != nil {
				t.Fatal(err)
			}
			reads, deletes := 0, 0
			deleted := false
			cl.runCommand = func(_ context.Context, bound execpkg.Config, command string) execpkg.Result {
				if bound.Profile != "quota-test" || bound.ExpectedIdentity == nil || *bound.ExpectedIdentity != cl.volumeFixtureBinding.Identity {
					t.Errorf("ordinary role cleanup lost pinned caller: %+v", bound)
				}
				result := execpkg.Result{Command: command, Started: true, Exit: 0}
				switch {
				case strings.Contains(command, "ListObjects"):
					result.Exit = 4
					result.JSON = map[string]any{"error": map[string]any{"kind": "not_found", "code": "NotFound", "message": bucket + " not found"}, "actions": []any{map[string]any{"action_name": "ListObjects", "code": "NoSuchBucket", "provider_verified": true}}}
				case strings.Contains(command, "GetRole"):
					reads++
					if tc.absent || deleted {
						result.Exit = 4
						result.JSON = map[string]any{"error": map[string]any{"kind": "not_found", "code": "NotFound"}, "actions": []any{map[string]any{"action_name": "GetRole", "code": "EntityNotExist.Role", "provider_verified": true}}}
					} else if tc.unknown {
						result.JSON = map[string]any{"product": "ram", "operation": "GetRole", "region": "cn-hangzhou"}
					} else {
						result.JSON = map[string]any{"product": "ram", "operation": "GetRole", "region": "cn-hangzhou", "response": map[string]any{"RequestId": "request-get-role", "Role": map[string]any{"RoleId": tc.currentID, "RoleName": bucket + "-role", "Arn": "acs:ram::1234567890123456:role/" + bucket + "-role"}}}
					}
				case strings.Contains(command, "DeleteRole"):
					deletes++
					deleted = true
					response := map[string]any{"RequestId": "request-delete-role"}
					if tc.falseSuccess {
						response["Success"] = false
					}
					result.JSON = map[string]any{"product": "ram", "operation": "DeleteRole", "region": "cn-hangzhou", "response": response}
				default:
					t.Errorf("unexpected provider call: %s", command)
					result.Err = errors.New("unexpected provider call")
				}
				return result
			}
			failures := cl.runCleanup([]*cleanupItem{item}, false)
			wantSuccess := tc.wantDeleted && !tc.falseSuccess || tc.absent
			wantReads := 1
			if tc.absent || tc.wantDeleted && !tc.falseSuccess {
				wantReads = 2
			}
			if (len(failures) == 0) != wantSuccess || deletes != map[bool]int{true: 1, false: 0}[tc.wantDeleted] || reads != wantReads {
				t.Fatalf("ordinary cleanup: failures=%v reads=%d deletes=%d", failures, reads, deletes)
			}
			stored, _, err := readRunnerJournal(path)
			if err != nil || len(stored.Entries) != map[bool]int{true: 0, false: 1}[wantSuccess] {
				t.Fatalf("ordinary role journal changed unexpectedly: %+v %v", stored, err)
			}
			if !tc.wantDeleted && !tc.absent && stored.Entries[0].VolumeFixtureDeleteAttempted {
				t.Fatalf("unowned role was marked for deletion: %+v", stored.Entries[0])
			}
			if tc.falseSuccess && !stored.Entries[0].VolumeFixtureDeleteAttempted {
				t.Fatalf("contradictory RAM response lost attempted marker: %+v", stored.Entries[0])
			}
		})
	}
}

func TestOrdinaryVolumeFixtureKeepsNameMutationFenced(t *testing.T) {
	for _, kind := range []string{"bucket", "role", "policy"} {
		for _, mode := range []string{"fenced", "started", "unstarted"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				dir := canonicalCleanupTestDir(t)
				path, bin := filepath.Join(dir, "cleanup.json"), filepath.Join(dir, "ecctl")
				configureQuotaProbeFixture(t, dir, bin)
				bucket := "ecctl-e2e-20261104t120000-123456abcdef"
				cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou"}
				cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, path,
					report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
				if mode != "fenced" {
					grantSyntheticVolumeFixtureScope(t, cl, bucket, "delete")
				}
				if _, err := cl.boundVolumeFixtureConfig(context.Background(), cfg, bucket, time.Second); err != nil {
					t.Fatal(err)
				}
				parent, err := os.Stat(dir)
				if err != nil {
					t.Fatal(err)
				}
				cl.journalParents = map[string]os.FileInfo{path: parent}
				command := `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role"}'`
				if kind == "bucket" {
					command = `ecctl call oss DeleteBucket --request '{"Bucket":"` + bucket + `"}' --region cn-hangzhou`
				} else if kind == "policy" {
					command = `ecctl call ram DeletePolicy --region cn-hangzhou --request '{"PolicyName":"` + bucket + `-read"}'`
				}
				item := &cleanupItem{scope: "stack", role: "primary", cmd: command, volumeFixtureKind: kind, volumeFixtureDeleteProtocol: 1}
				if kind == "role" {
					item.volumeFixtureRoleID = "role-created"
				} else if kind == "policy" {
					item.volumeFixturePolicyCreateDate = "2026-11-04T12:00:00Z"
				} else if kind == "bucket" {
					item.volumeFixtureBucketCreationDate = "2026-11-04T12:00:00Z"
				}
				journal, entry, _ := cl.journalValues(item)
				journal.JournalPath = path
				journal.Entries = []report.Resource{entry}
				if err := writeRunnerJournal(path, journal); err != nil {
					t.Fatal(err)
				}
				deletes := 0
				markerAtCall := false
				cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
					result := execpkg.Result{Command: command, Started: true, Exit: 0}
					switch {
					case strings.Contains(command, "ListObjects"):
						if kind == "bucket" {
							result.JSON = map[string]any{"product": "oss", "operation": "ListObjects", "response": map[string]any{"Name": bucket, "IsTruncated": false, "KeyCount": "0"}}
						} else {
							result.Exit = 4
							result.JSON = map[string]any{"error": map[string]any{"kind": "not_found", "code": "NotFound", "message": bucket + " not found"}, "actions": []any{map[string]any{"action_name": "ListObjects", "code": "NoSuchBucket", "provider_verified": true}}}
						}
					case strings.Contains(command, "GetRole"):
						result.JSON = map[string]any{"product": "ram", "operation": "GetRole", "region": "cn-hangzhou", "response": map[string]any{"RequestId": "request-get-role", "Role": map[string]any{"RoleId": "role-created", "RoleName": bucket + "-role", "Arn": "acs:ram::1234567890123456:role/" + bucket + "-role"}}}
					case strings.Contains(command, "GetPolicy"):
						result.JSON = map[string]any{"product": "ram", "operation": "GetPolicy", "region": "cn-hangzhou", "response": map[string]any{"RequestId": "request-get-policy", "Policy": map[string]any{"PolicyName": bucket + "-read", "PolicyType": "Custom", "CreateDate": "2026-11-04T12:00:00Z"}}}
					case strings.Contains(command, "GetBucketInfo"):
						result.JSON = map[string]any{"product": "oss", "operation": "GetBucketInfo", "region": "cn-hangzhou", "response": map[string]any{"Bucket": map[string]any{"Name": bucket, "Location": "oss-cn-hangzhou", "ExtranetEndpoint": "oss-cn-hangzhou.aliyuncs.com", "CreationDate": "2026-11-04T12:00:00Z", "Owner": map[string]any{"ID": "1234567890123456"}}}}
					case strings.Contains(command, "DeleteBucket") || strings.Contains(command, "DeleteRole") || strings.Contains(command, "DeletePolicy"):
						stored, _, err := readRunnerJournal(path)
						if err != nil || len(stored.Entries) != 1 || !stored.Entries[0].VolumeFixtureDeleteAttempted {
							t.Fatalf("destructive subprocess started without durable marker: %+v %v", stored, err)
						}
						markerAtCall = true
						deletes++
						if mode == "started" {
							return execpkg.Result{Command: command, Started: true, Exit: 2, JSON: map[string]any{"error": map[string]any{"code": "provider-failure"}}}
						}
						return execpkg.Result{Command: command, Err: errors.New("start failed"), Exit: -1}
					default:
						t.Fatalf("unexpected cleanup call: %s", command)
					}
					return result
				}
				scope := []*cleanupItem{item}
				failures := cl.runCleanup(scope, false)
				if len(failures) != 1 {
					t.Fatalf("failed delete did not report failure: %v", failures)
				}
				stored, _, err := readRunnerJournal(path)
				if err != nil || len(stored.Entries) != 1 || stored.Entries[0].VolumeFixtureDeleteAttempted != (mode == "started") || item.volumeFixtureDeleteAttempted != (mode == "started") || deletes != map[bool]int{false: 0, true: 1}[mode != "fenced"] || markerAtCall != (mode != "fenced") {
					t.Fatalf("delete marker or invocation proof changed: journal=%+v err=%v calls=%d marker=%v failures=%v", stored, err, deletes, markerAtCall, failures)
				}
				if mode == "fenced" && !strings.Contains(strings.Join(failures, "\n"), "name mutation requires") {
					t.Fatalf("negative row did not reach mutation fence: %v", failures)
				}
				if mode == "started" {
					if failures := cl.runCleanup(scope, false); len(failures) != 1 || deletes != 1 {
						t.Fatalf("marked delete was retried: failures=%v calls=%d", failures, deletes)
					}
				}
			})
		}
	}
}

func TestNeverStartedVolumeTeamRetiresOnlyItsReservation(t *testing.T) {
	cl, intent, path := issue90VolumeTeamReservation(t)
	if err := journalfile.WithExistingReplayLock(context.Background(), path, func() error {
		return retireUnstartedVolumeTeamCreate(cl, intent)
	}); err != nil {
		t.Fatal(err)
	}
	journal, _, err := readRunnerJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := journal.VolumeTeamCreateIDs[intent.Name]; exists || journal.VolumeTeamCreateIDs["ecctl-t-aaaaaaaaaaaaaaaaaaaaaaaa"] != "other-team" {
		t.Fatalf("retirement changed another Team reservation: %+v", journal.VolumeTeamCreateIDs)
	}
	if cl.volumeTeamRetirementUncertain {
		t.Fatal("successful durable retirement kept an in-process fence")
	}
}

func TestObservedVolumeTeamNonAllocationRetiresDispatchedReservation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result execpkg.Result
	}{
		{name: "start failed", result: execpkg.Result{Err: errors.New("start failed"), Exit: -1}},
		{name: "verified rejection", result: execpkg.Result{Started: true, Exit: 2, JSON: map[string]any{
			"error":   map[string]any{"kind": "service", "code": "CloudAPIError", "retryable": false},
			"actions": []any{map[string]any{"action_name": "CreateTeam", "code": "NoPermission", "provider_verified": true, "request_id": "req-rejected"}},
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cl, intent, path := issue90VolumeTeamReservation(t)
			if err := markVolumeTeamCreateDispatch(cl, intent); err != nil {
				t.Fatal(err)
			}
			if err := retireUnstartedVolumeTeamCreate(cl, intent); err == nil {
				t.Fatal("historical retirement accepted a dispatch marker")
			}
			if err := journalfile.WithExistingReplayLock(context.Background(), path, func() error {
				return retireObservedVolumeTeamCreate(cl, intent, tc.result)
			}); err != nil {
				t.Fatal(err)
			}
			journal, _, err := readRunnerJournal(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, retained := journal.VolumeTeamCreateIDs[intent.Name]; retained || journal.VolumeTeamCreateIDs["ecctl-t-aaaaaaaaaaaaaaaaaaaaaaaa"] != "other-team" {
				t.Fatalf("observed retirement changed another reservation: %+v", journal.VolumeTeamCreateIDs)
			}
		})
	}
}

func TestNeverStartedVolumeTeamRetiresSoleReservation(t *testing.T) {
	cl, intent, path := issue90VolumeTeamReservation(t)
	journal, _, err := readRunnerJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	delete(journal.VolumeTeamCreateIDs, "ecctl-t-aaaaaaaaaaaaaaaaaaaaaaaa")
	if err := writeRunnerJournal(path, journal); err != nil {
		t.Fatal(err)
	}
	if err := journalfile.WithExistingReplayLock(context.Background(), path, func() error {
		return retireUnstartedVolumeTeamCreate(cl, intent)
	}); err != nil {
		t.Fatal(err)
	}
	installed, _, err := readRunnerJournal(path)
	if err != nil || len(installed.VolumeTeamCreateIDs) != 0 || cl.volumeTeamRetirementUncertain || cl.allocationFence != "" {
		t.Fatalf("sole Team reservation was not retired durably: journal=%+v uncertain=%v fence=%q err=%v", installed, cl.volumeTeamRetirementUncertain, cl.allocationFence, err)
	}
}

func TestNeverStartedVolumeTeamRetirementRetryBarrier(t *testing.T) {
	for _, tc := range []struct {
		name, retryError     string
		installed, wantFence bool
	}{
		{name: "before install", wantFence: true},
		{name: "after install with successful retry", installed: true},
		{name: "after install with failed retry", installed: true, retryError: "injected retry barrier failure", wantFence: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cl, intent, path := issue90VolumeTeamReservation(t)
			previous, priorReaffirm := writeTeamCreateJournal, reaffirmVolumeTeamRetirementJournal
			t.Cleanup(func() { writeTeamCreateJournal, reaffirmVolumeTeamRetirementJournal = previous, priorReaffirm })
			writeTeamCreateJournal = func(path string, journal report.CleanupJournal) error {
				if tc.installed {
					if err := previous(path, journal); err != nil {
						return err
					}
				}
				return errors.New("injected retirement durability failure")
			}
			if tc.retryError != "" {
				reaffirmVolumeTeamRetirementJournal = func(string, report.CleanupJournal) error { return errors.New(tc.retryError) }
			}
			err := journalfile.WithExistingReplayLock(context.Background(), path, func() error {
				return retireUnstartedVolumeTeamCreate(cl, intent)
			})
			if tc.wantFence {
				if err == nil || !cl.volumeTeamRetirementUncertain || cl.allocationFence == "" {
					t.Fatalf("uncertain retirement lost fence: %v", err)
				}
			} else if err != nil || cl.volumeTeamRetirementUncertain || cl.allocationFence != "" {
				t.Fatalf("durably retired Team retained fence: %v", err)
			}
			journal, _, err := readRunnerJournal(path)
			if err != nil || journal.VolumeTeamCreateIDs["ecctl-t-aaaaaaaaaaaaaaaaaaaaaaaa"] != "other-team" {
				t.Fatalf("uncertain retirement changed another reservation: %+v %v", journal.VolumeTeamCreateIDs, err)
			}
			if _, exists := journal.VolumeTeamCreateIDs[intent.Name]; !tc.wantFence && exists {
				t.Fatalf("retired Team name remains reserved: %+v", journal.VolumeTeamCreateIDs)
			}
		})
	}
}

func TestVolumeTeamRegistrationRepairSettlesExactPendingName(t *testing.T) {
	for _, installed := range []bool{false, true} {
		t.Run(map[bool]string{false: "before install", true: "after install"}[installed], func(t *testing.T) {
			cl, intent, path := issue90VolumeTeamReservation(t)
			if err := markVolumeTeamCreateDispatch(cl, intent); err != nil {
				t.Fatal(err)
			}
			if err := recordVolumeTeamCreateID(cl, intent, "team-owned", issue90TeamReceipt(intent, "team-owned")); err != nil {
				t.Fatal(err)
			}
			previous := writeTeamRegistrationJournal
			t.Cleanup(func() { writeTeamRegistrationJournal = previous })
			writeTeamRegistrationJournal = func(path string, journal report.CleanupJournal) error {
				if installed {
					if err := previous(path, journal); err != nil {
						return err
					}
				}
				return errors.New("injected first registration failure")
			}
			var scope []*cleanupItem
			item, err := cl.pushItemWithFixture(&scope, "case", "ecctl sandbox team delete team-owned --timeout 5m --output=json", "primary", nil, false, true, false, intent.VolumeBinding, "", intent)
			if err == nil || item == nil || !item.teamRegistrationFailed {
				t.Fatalf("failed registration lost confirmed Team: item=%+v err=%v", item, err)
			}
			writeTeamRegistrationJournal = previous
			if err := cl.repairTeamRegistration(item); err != nil {
				t.Fatal(err)
			}
			journal, _, err := readRunnerJournal(path)
			if err != nil || len(journal.Entries) != 1 || journal.VolumeTeamCreateIDs[intent.Name] != "team-owned" || journal.VolumeTeamCreateIDs["ecctl-t-aaaaaaaaaaaaaaaaaaaaaaaa"] != "other-team" || journal.VolumeTeamCreators["team-owned"] == nil {
				t.Fatalf("repaired registration did not settle exact name: %+v %v", journal, err)
			}
			journal.Entries[0].TeamReplayAttempted = true
			if err := writeRunnerJournal(path, journal); err != nil {
				t.Fatal(err)
			}
			item.teamReplayAttempted = true
			absenceBody := `{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetTeam","code":"TeamNotFound","request_id":"req-exact","provider_verified":true}]}`
			var absence any
			if err := json.Unmarshal([]byte(absenceBody), &absence); err != nil {
				t.Fatal(err)
			}
			command := "ecctl sandbox team get 'team-owned' --region 'cn-hangzhou' --profile 'fixture-profile' --output=json --agent-envelope=false"
			proof := mockVerifiedTeamRead(t, command, journal.EcctlBinSHA256, execpkg.Result{Command: command, Started: true, Exit: 4, Stdout: absenceBody, JSON: absence})
			outcome := execpkg.Result{Started: true, Exit: 0, Stdout: `{"deleted":true,"team":{"id":"team-owned"}}`}
			if err := cl.consumeOwnedTeamDelete(item, outcome, proof); err != nil {
				t.Fatal(err)
			}
			journal, _, err = readRunnerJournal(path)
			if err != nil || len(journal.Entries) != 0 || journal.VolumeTeamCreateIDs[intent.Name] != "team-owned" {
				t.Fatalf("Team consumption lost terminal history: %+v %v", journal, err)
			}
		})
	}
}

func TestLegacyIndependentVolumeTeamSettlementFailureRepairsAtomically(t *testing.T) {
	for _, installed := range []bool{false, true} {
		t.Run(map[bool]string{false: "before install", true: "after install"}[installed], func(t *testing.T) {
			cl, intent, path := issue90VolumeTeamReservation(t)
			if err := recordVolumeTeamCreateID(cl, intent, "team-owned", issue90TeamReceipt(intent, "team-owned")); err != nil {
				t.Fatal(err)
			}
			var scope []*cleanupItem
			item, err := cl.pushItemWithFixture(&scope, "case", "ecctl sandbox team delete team-owned --timeout 5m", "primary", nil, false, true, false, intent.VolumeBinding, "", intent)
			if err != nil {
				t.Fatal(err)
			}
			journal, _, err := readRunnerJournal(path)
			if err != nil {
				t.Fatal(err)
			}
			// Recreate the older two-write state: finalizer installed, name pending.
			journal.VolumeTeamCreateIDs[intent.Name] = ""
			if err := writeRunnerJournal(path, journal); err != nil {
				t.Fatal(err)
			}
			previous := writeVolumeTeamSettlementRoot
			t.Cleanup(func() { writeVolumeTeamSettlementRoot = previous })
			writeVolumeTeamSettlementRoot = func(root *os.Root, path string, journal report.CleanupJournal) error {
				if installed {
					if err := previous(root, path, journal); err != nil {
						return err
					}
				}
				return errors.New("injected independent settlement failure")
			}
			if err := settleVolumeTeamCreate(cl, intent, "team-owned"); err == nil {
				t.Fatal("failed independent settlement was reported durable")
			}
			writeVolumeTeamSettlementRoot = previous
			if err := cl.repairTeamRegistration(item); err != nil {
				t.Fatal(err)
			}
			journal, _, err = readRunnerJournal(path)
			if err != nil || len(journal.Entries) != 1 || journal.VolumeTeamCreateIDs[intent.Name] != "team-owned" || journal.VolumeTeamCreateIDs["ecctl-t-aaaaaaaaaaaaaaaaaaaaaaaa"] != "other-team" {
				t.Fatalf("repair lost pending Team association: %+v %v", journal, err)
			}
		})
	}
}

func TestVolumeTeamReceiptStaysInRememberedParentAfterPathSwap(t *testing.T) {
	cl, intent, path := issue90VolumeTeamReservation(t)
	previous := writeVolumeTeamCreateReceiptRoot
	t.Cleanup(func() { writeVolumeTeamCreateReceiptRoot = previous })
	parent := filepath.Dir(path)
	moved := parent + "-moved"
	writeVolumeTeamCreateReceiptRoot = func(root *os.Root, name string, body []byte) error {
		if err := os.Rename(parent, moved); err != nil {
			return err
		}
		if err := os.Mkdir(parent, 0o700); err != nil {
			return err
		}
		return previous(root, name, body)
	}
	if err := recordVolumeTeamCreateID(cl, intent, "team-owned", issue90TeamReceipt(intent, "team-owned")); err == nil {
		t.Fatal("Volume Team receipt accepted a replaced parent")
	}
	if intent.AcceptedID != "" {
		t.Fatal("Volume Team receipt was published after parent replacement")
	}
	if _, err := os.Stat(intent.path); !os.IsNotExist(err) {
		t.Fatalf("replacement parent received a receipt: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(moved, filepath.Base(intent.path)))
	if err != nil || !bytes.Contains(body, []byte(`"accepted_id": "team-owned"`)) {
		t.Fatalf("remembered parent lost the rooted receipt: %v", err)
	}
}
