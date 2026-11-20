package journalfile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

func TestVolumeFixtureWholeCountAcceptsDecodedIntegralJSONNumber(t *testing.T) {
	for _, tc := range []struct {
		literal string
		want    int
		ok      bool
	}{
		{`0`, 0, true},
		{`12`, 12, true},
		{`12.0`, 12, true},
		{`1.2e1`, 12, true},
		{`-1`, 0, false},
		{`1.5`, 0, false},
		{`9223372036854775808`, 0, false},
	} {
		var decoded any
		decoder := json.NewDecoder(strings.NewReader(tc.literal))
		decoder.UseNumber()
		if err := decoder.Decode(&decoded); err != nil {
			t.Fatal(err)
		}
		got, ok := volumeFixtureWholeCount(decoded)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("count %s = %d, %v; want %d, %v", tc.literal, got, ok, tc.want, tc.ok)
		}
	}
}

func volumeFixtureTestJournal() report.CleanupJournal {
	return report.CleanupJournal{
		Version: 2, JournalPath: "/tmp/volume-fixture-journal", RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou",
		VolumeFixtureBucket:  "ecctl-e2e-20261104t120000-123456abcdef",
		VolumeFixtureBinding: &report.QuotaCredentialBinding{Identity: execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}},
	}
}

func TestVolumeFixtureRejectedPolicyCreateRequiresProviderProof(t *testing.T) {
	bucket := volumeFixtureTestJournal().VolumeFixtureBucket
	root := map[string]any{
		"error":   map[string]any{"kind": "service", "code": "CloudAPIError", "retryable": false},
		"actions": []any{map[string]any{"action_name": "CreatePolicy", "code": "Forbidden", "request_id": "req-policy", "provider_verified": true}},
	}
	result := execpkg.Result{Started: true, Exit: 2, JSON: root}
	if !VolumeFixtureDefinitiveCreateRejection("policy", bucket, result) {
		t.Fatal("verified policy rejection did not settle no-allocation")
	}
	if VolumeFixtureDefinitiveCreateRejection("role", bucket, result) {
		t.Fatal("policy proof settled a different fixture")
	}
	root["actions"].([]any)[0].(map[string]any)["provider_verified"] = false
	if VolumeFixtureDefinitiveCreateRejection("policy", bucket, result) {
		t.Fatal("diagnostic rejection settled policy create")
	}
	root["actions"].([]any)[0].(map[string]any)["provider_verified"] = true
	root["response"] = map[string]any{"Policy": map[string]any{}}
	if VolumeFixtureDefinitiveCreateRejection("policy", bucket, result) {
		t.Fatal("allocation evidence settled policy create")
	}
}

func TestVolumeFixtureRejectedRAMCreateRequiresExactTarget(t *testing.T) {
	bucket := volumeFixtureTestJournal().VolumeFixtureBucket
	for _, tc := range []struct {
		kind, operation string
		target          map[string]any
	}{
		{"role", "CreateRole", map[string]any{"RoleName": bucket + "-role"}},
		{"attachment", "AttachPolicyToRole", map[string]any{"RoleName": bucket + "-role", "PolicyName": bucket + "-read", "PolicyType": "Custom"}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			proof := func() (execpkg.Result, map[string]any, map[string]any) {
				action := map[string]any{"action_name": tc.operation, "code": "Forbidden", "request_id": "req-ram", "provider_verified": true, "target": tc.target}
				root := map[string]any{"error": map[string]any{"kind": "service", "code": "CloudAPIError", "retryable": false}, "actions": []any{action}}
				return execpkg.Result{Started: true, Exit: 2, JSON: root}, root, action
			}
			result, _, _ := proof()
			if !VolumeFixtureDefinitiveCreateRejection(tc.kind, bucket, result) {
				t.Fatal("exact verified RAM rejection did not retire reservation")
			}
			for _, mutate := range []struct {
				name string
				edit func(map[string]any, map[string]any)
			}{
				{"wrong target", func(_ map[string]any, action map[string]any) {
					action["target"] = map[string]any{"RoleName": bucket + "-other"}
				}},
				{"wrong bucket", func(_ map[string]any, action map[string]any) {
					action["target"] = map[string]any{"RoleName": "ecctl-e2e-20261104t120001-123456abcdef-role"}
				}},
				{"missing target", func(_ map[string]any, action map[string]any) { delete(action, "target") }},
				{"extra target", func(_ map[string]any, action map[string]any) {
					action["target"] = map[string]any{"RoleName": bucket + "-role", "Unexpected": "value"}
				}},
				{"wrong operation", func(_ map[string]any, action map[string]any) { action["action_name"] = "CreatePolicy" }},
				{"unverified", func(_ map[string]any, action map[string]any) { action["provider_verified"] = false }},
				{"ambiguous code", func(_ map[string]any, action map[string]any) { action["code"] = "EntityAlreadyExists.Role" }},
				{"missing request ID", func(_ map[string]any, action map[string]any) { delete(action, "request_id") }},
				{"allocation evidence", func(root map[string]any, _ map[string]any) {
					root["response"] = map[string]any{"Role": map[string]any{}}
				}},
			} {
				t.Run(mutate.name, func(t *testing.T) {
					result, root, action := proof()
					mutate.edit(root, action)
					if VolumeFixtureDefinitiveCreateRejection(tc.kind, bucket, result) {
						t.Fatal("ambiguous RAM rejection retired reservation")
					}
				})
			}
			if tc.kind == "attachment" {
				for _, field := range []string{"PolicyName", "PolicyType"} {
					t.Run("wrong "+field, func(t *testing.T) {
						result, _, action := proof()
						wrong := map[string]any{"RoleName": bucket + "-role", "PolicyName": bucket + "-read", "PolicyType": "Custom"}
						wrong[field] = "other"
						action["target"] = wrong
						if VolumeFixtureDefinitiveCreateRejection(tc.kind, bucket, result) {
							t.Fatal("different attachment target retired reservation")
						}
					})
				}
			}
		})
	}
}

func TestRejectedRAMReservationReleasesEarlierFixtureCleanup(t *testing.T) {
	for _, kind := range []string{"role", "attachment"} {
		t.Run(kind, func(t *testing.T) {
			journal := volumeFixtureTestJournal()
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			journal.JournalPath = filepath.Join(dir, "cleanup.json")
			journal.RunID, journal.ExecutionID = "run", "execution"
			bucket := journal.VolumeFixtureBucket
			resource := func(kind, teardown string) report.Resource {
				return report.Resource{Scope: "stack", ExecutionID: journal.ExecutionID, RegionRole: "primary", Region: journal.Region, VolumeFixtureKind: kind, VolumeFixtureDeleteProtocol: 1, Teardown: teardown}
			}
			role := resource("role", `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"`+bucket+`-role"}'`)
			policy := resource("policy", `ecctl call ram DeletePolicy --region cn-hangzhou --request '{"PolicyName":"`+bucket+`-read"}'`)
			policy.VolumeFixturePolicyCreateDate = "2026-11-04T12:00:00Z"
			var earlier report.Resource
			var operation string
			var target map[string]any
			if kind == "role" {
				earlier = resource("bucket", `ecctl call oss DeleteBucket --request '{"Bucket":"`+bucket+`"}' --region cn-hangzhou`)
				earlier.VolumeFixtureBucketCreationDate = "2026-11-04T12:00:00Z"
				journal.Entries = []report.Resource{earlier, role}
				operation = "CreateRole"
				target = map[string]any{"RoleName": bucket + "-role"}
			} else {
				earlier = policy
				attachment := resource("attachment", `ecctl call ram DetachPolicyFromRole --region cn-hangzhou --request '{"RoleName":"`+bucket+`-role","PolicyName":"`+bucket+`-read","PolicyType":"Custom"}'`)
				journal.Entries = []report.Resource{role, policy, attachment}
				operation = "AttachPolicyToRole"
				target = map[string]any{"RoleName": bucket + "-role", "PolicyName": bucket + "-read", "PolicyType": "Custom"}
			}
			journal.Entries[len(journal.Entries)-1].VolumeFixturePending = true
			if err := VolumeFixtureCleanupReady(journal.JournalPath, journal, earlier, nil); err == nil {
				t.Fatal("pending later RAM create allowed earlier fixture cleanup")
			}
			result := execpkg.Result{Started: true, Exit: 2, JSON: map[string]any{
				"error":   map[string]any{"kind": "service", "code": "CloudAPIError", "retryable": false},
				"actions": []any{map[string]any{"action_name": operation, "code": "Forbidden", "request_id": "req-ram", "provider_verified": true, "target": target}},
			}}
			if !VolumeFixtureDefinitiveCreateRejection(kind, bucket, result) {
				t.Fatal("exact RAM rejection did not prove no allocation")
			}
			journal.Entries = journal.Entries[:len(journal.Entries)-1]
			if err := VolumeFixtureCleanupReady(journal.JournalPath, journal, earlier, nil); err != nil {
				t.Fatalf("rejected later RAM create still blocked earlier cleanup: %v", err)
			}
		})
	}
}

func TestVolumeFixtureIdentityRequiresExclusiveCreatePrincipal(t *testing.T) {
	account := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "Account"}
	if err := account.Validate(); err != nil {
		t.Fatalf("generic Account identity changed: %v", err)
	}
	for _, pin := range []execpkg.IdentityPin{
		{Version: 1, AccountID: account.AccountID, Type: "RAMUser", UserID: "user-a"},
		{Version: 1, AccountID: account.AccountID, Type: "AssumedRoleUser", RoleID: "role-a"},
	} {
		if err := ValidateVolumeFixtureIdentity(&report.QuotaCredentialBinding{Identity: pin}); err != nil {
			t.Fatalf("trusted %s fixture identity rejected: %v", pin.Type, err)
		}
	}
	journal := volumeFixtureTestJournal()
	journal.VolumeFixtureBinding.Identity = account
	bucket := journal.VolumeFixtureBucket
	create := `ecctl call oss PutBucket --request '{"Bucket":"` + bucket + `","ACL":"private"}' --region cn-hangzhou`
	entry := report.Resource{Scope: "stack", ExecutionID: journal.ExecutionID, VolumeFixtureKind: "bucket", RegionRole: "primary", Region: journal.Region,
		Teardown: `ecctl call oss DeleteBucket --request '{"Bucket":"` + bucket + `"}' --region cn-hangzhou`}
	for _, check := range []struct {
		name string
		run  func() error
	}{
		{"identity", func() error { return ValidateVolumeFixtureIdentity(journal.VolumeFixtureBinding) }},
		{"create", func() error { return ValidateVolumeFixtureCreate("bucket", create, journal) }},
		{"teardown", func() error { return ValidateVolumeFixtureTeardown(entry, journal) }},
		{"cleanup", func() error { return VolumeFixtureCleanupReady("", journal, entry, nil) }},
	} {
		if err := check.run(); err == nil || !strings.Contains(err.Error(), "RAMUser or AssumedRoleUser") {
			t.Fatalf("old Account-bound %s was accepted: %v", check.name, err)
		}
	}
	teamID := "team-2"
	journal.VolumeTeamCreators = map[string]*report.QuotaCredentialBinding{teamID: journal.VolumeFixtureBinding}
	journal.Entries = []report.Resource{{Teardown: "ecctl sandbox team delete team-2 --timeout 5m", TeamCreateConfirmed: true, VolumeTeamBound: true}}
	if _, _, err := volumeTeamCreator(journal, teamID); err == nil || !strings.Contains(err.Error(), "RAMUser or AssumedRoleUser") {
		t.Fatalf("old Account-bound Team creator was accepted: %v", err)
	}
}

func TestOrdinaryTeamDoesNotInheritVolumeFixtureCreator(t *testing.T) {
	journal := volumeFixtureTestJournal()
	journal.Entries = []report.Resource{{Teardown: "ecctl sandbox team delete team-ordinary --timeout 5m", TeamCreateConfirmed: true}}
	marked, binding, err := volumeTeamCreator(journal, "team-ordinary")
	if err != nil || marked || binding != nil {
		t.Fatalf("ordinary Team inherited Volume creator: marked=%t binding=%+v err=%v", marked, binding, err)
	}
}

func TestVolumeFixtureDeleteResultRequiresExactBusinessOutcome(t *testing.T) {
	journal := volumeFixtureTestJournal()
	journal.EcctlBin = "/tmp/ecctl"
	journal.VolumeFixtureBinding.Profile = "fixture-profile"
	bucket := journal.VolumeFixtureBucket
	cfg := execpkg.Config{Bin: journal.EcctlBin, Region: journal.Region, Profile: "fixture-profile", ExpectedIdentity: &journal.VolumeFixtureBinding.Identity}
	for _, tc := range []struct {
		kind, operation, command, absentCode string
	}{
		{"bucket", "DeleteBucket", `ecctl call oss DeleteBucket --request '{"Bucket":"` + bucket + `"}' --region cn-hangzhou`, "NoSuchBucket"},
		{"role", "DeleteRole", `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role"}'`, "EntityNotExist.Role"},
		{"policy", "DeletePolicy", `ecctl call ram DeletePolicy --region cn-hangzhou --request '{"PolicyName":"` + bucket + `-read"}'`, "EntityNotExist.Policy"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			entry := report.Resource{Scope: "stack", ExecutionID: journal.ExecutionID, VolumeFixtureKind: tc.kind, Teardown: tc.command, RegionRole: "primary", Region: journal.Region}
			product := "ram"
			if tc.kind == "bucket" {
				product = "oss"
			}
			response := map[string]any{}
			if product == "ram" {
				response["RequestId"] = "request-fixture"
			}
			success := execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"product": product, "operation": tc.operation, "region": journal.Region, "response": response}}
			if err := VolumeFixtureDeleteConfirmed(entry, journal, cfg, success); err != nil {
				t.Fatalf("business success rejected: %v", err)
			}
			if product == "ram" {
				for _, key := range []string{"Success", "success"} {
					response[key] = false
					if err := VolumeFixtureDeleteConfirmed(entry, journal, cfg, success); err == nil {
						t.Fatalf("RAM %s=false was accepted", key)
					}
					delete(response, key)
				}
			}
			absence := execpkg.Result{Started: true, Exit: 4, JSON: map[string]any{"error": map[string]any{"kind": "not_found", "code": "NotFound", "message": bucket + " not found"}, "actions": []any{map[string]any{"action_name": tc.operation, "code": tc.absentCode, "provider_verified": true}}}}
			if err := VolumeFixtureDeleteConfirmed(entry, journal, cfg, absence); err != nil {
				t.Fatalf("exact provider absence rejected: %v", err)
			}
			absence.JSON.(map[string]any)["actions"].([]any)[0].(map[string]any)["provider_verified"] = false
			if err := VolumeFixtureDeleteConfirmed(entry, journal, cfg, absence); err == nil {
				t.Fatal("diagnostic action consumed fixture")
			}
			absence.JSON.(map[string]any)["actions"].([]any)[0].(map[string]any)["provider_verified"] = true
			for _, code := range []string{"InvalidAccessKeyId.NotFound", "404", "NotFound", "EntityNotExist.Other"} {
				absence.JSON.(map[string]any)["actions"].([]any)[0].(map[string]any)["code"] = code
				if err := VolumeFixtureDeleteConfirmed(entry, journal, cfg, absence); err == nil {
					t.Fatalf("unrelated provider code %s consumed fixture", code)
				}
			}
			absence.JSON.(map[string]any)["actions"].([]any)[0].(map[string]any)["code"] = tc.absentCode
			absence.JSON.(map[string]any)["actions"].([]any)[0].(map[string]any)["action_name"] = "OtherOperation"
			if err := VolumeFixtureDeleteConfirmed(entry, journal, cfg, absence); err == nil {
				t.Fatal("different operation consumed fixture")
			}
			success.JSON.(map[string]any)["operation"] = "OtherOperation"
			if err := VolumeFixtureDeleteConfirmed(entry, journal, cfg, success); err == nil {
				t.Fatal("different successful operation consumed fixture")
			}
		})
	}
}

func TestVolumeFixtureDetachResponseAloneCannotRetireAttachment(t *testing.T) {
	journal := volumeFixtureTestJournal()
	journal.EcctlBin = "/tmp/ecctl"
	journal.VolumeFixtureBinding.Profile = "fixture-profile"
	bucket := journal.VolumeFixtureBucket
	entry := report.Resource{Scope: "stack", ExecutionID: journal.ExecutionID, VolumeFixtureKind: "attachment", VolumeFixtureDeleteProtocol: 1, VolumeFixtureDeleteAttempted: true,
		Teardown: `ecctl call ram DetachPolicyFromRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role","PolicyName":"` + bucket + `-read","PolicyType":"Custom"}'`, RegionRole: "primary", Region: journal.Region}
	cfg := execpkg.Config{Bin: journal.EcctlBin, Region: journal.Region, Profile: "fixture-profile", ExpectedIdentity: &journal.VolumeFixtureBinding.Identity}
	result := execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"product": "ram", "operation": "DetachPolicyFromRole", "region": journal.Region, "response": map[string]any{"RequestId": "request-detach"}}}
	if err := VolumeFixtureDeleteConfirmed(entry, journal, cfg, result); err == nil {
		t.Fatal("successful DetachPolicyFromRole response retired an unproven relationship")
	}
}

func TestVolumeFixtureRoleAbsenceRequiresExactReadResult(t *testing.T) {
	journal := volumeFixtureTestJournal()
	journal.EcctlBin = "/tmp/ecctl"
	journal.VolumeFixtureBinding.Profile = "fixture-profile"
	bucket := journal.VolumeFixtureBucket
	entry := report.Resource{Scope: "stack", ExecutionID: journal.ExecutionID, VolumeFixtureKind: "role", Teardown: `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role"}'`, RegionRole: "primary", Region: journal.Region}
	cfg := execpkg.Config{Bin: journal.EcctlBin, Region: journal.Region, Profile: "fixture-profile", ExpectedIdentity: &journal.VolumeFixtureBinding.Identity}
	for _, tc := range []struct {
		name   string
		result execpkg.Result
		absent bool
	}{
		{"exact absence", execpkg.Result{Started: true, Exit: 4, JSON: map[string]any{"error": map[string]any{"kind": "not_found", "code": "NotFound"}, "actions": []any{map[string]any{"action_name": "GetRole", "code": "EntityNotExist.Role", "provider_verified": true}}}}, true},
		{"replacement exists", execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"product": "ram", "operation": "GetRole", "region": "cn-hangzhou", "response": map[string]any{"Role": map[string]any{"RoleId": "replacement"}}}}, false},
		{"wrong action", execpkg.Result{Started: true, Exit: 4, JSON: map[string]any{"error": map[string]any{"kind": "not_found", "code": "NotFound"}, "actions": []any{map[string]any{"action_name": "DeleteRole", "code": "EntityNotExist.Role", "provider_verified": true}}}}, false},
		{"unverified diagnostic", execpkg.Result{Started: true, Exit: 4, JSON: map[string]any{"error": map[string]any{"kind": "not_found", "code": "NotFound"}, "actions": []any{map[string]any{"action_name": "GetRole", "code": "EntityNotExist.Role"}}}}, false},
		{"unstarted read", execpkg.Result{Exit: 4, JSON: map[string]any{"error": map[string]any{"kind": "not_found", "code": "NotFound"}, "actions": []any{map[string]any{"action_name": "GetRole", "code": "EntityNotExist.Role"}}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			_, err := VolumeFixtureRoleAbsent(context.Background(), cfg, entry, journal, func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				calls++
				if command != `ecctl call ram GetRole --region cn-hangzhou --request '{"RoleName":"`+bucket+`-role"}' --output=json` {
					t.Errorf("role read target changed: %q", command)
				}
				return tc.result
			})
			if (err == nil) != tc.absent || calls != 1 {
				t.Fatalf("role absence = %v, calls=%d", err, calls)
			}
		})
	}
	cfg.Profile = "other-profile"
	if _, err := VolumeFixtureRoleAbsent(context.Background(), cfg, entry, journal, func(context.Context, execpkg.Config, string) execpkg.Result {
		t.Fatal("changed profile reached provider")
		return execpkg.Result{}
	}); err == nil {
		t.Fatal("changed profile was admitted")
	}
}

func TestVolumeFixtureFirstRoleDeleteUsesAssignedNameAndAccount(t *testing.T) {
	journal := volumeFixtureTestJournal()
	journal.EcctlBin = "/tmp/ecctl"
	journal.VolumeFixtureBinding.Profile = "fixture-profile"
	name := journal.VolumeFixtureBucket + "-role"
	entry := report.Resource{Scope: "stack", ExecutionID: journal.ExecutionID, VolumeFixtureKind: "role", VolumeFixtureRoleID: "role-created", VolumeFixtureDeleteProtocol: 1,
		Teardown: `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"` + name + `"}'`, RegionRole: "primary", Region: journal.Region}
	cfg := execpkg.Config{Bin: journal.EcctlBin, Region: journal.Region, Profile: "fixture-profile", ExpectedIdentity: &journal.VolumeFixtureBinding.Identity}
	success := func(id string) execpkg.Result {
		return execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"product": "ram", "operation": "GetRole", "region": journal.Region,
			"response": map[string]any{"RequestId": "request-get-role", "Role": map[string]any{"RoleId": id, "RoleName": name, "Arn": "acs:ram::1234567890123456:role/" + name}}}}
	}
	for _, tc := range []struct {
		name   string
		result execpkg.Result
		match  bool
	}{
		{"same RoleId", success("role-created"), true},
		{"different RoleId", success("role-replacement"), true},
		{"missing RoleId", success(""), true},
		{"unknown response", execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"product": "ram", "operation": "GetRole", "region": journal.Region}}, false},
		{"unstarted read", execpkg.Result{Exit: -1}, false},
		{"wrong action", execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"product": "ram", "operation": "DeleteRole", "region": journal.Region, "response": map[string]any{}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			_, err := VolumeFixtureRoleMatches(context.Background(), cfg, entry, journal, func(_ context.Context, bound execpkg.Config, command string) execpkg.Result {
				calls++
				if bound.Profile != cfg.Profile || bound.ExpectedIdentity == nil || *bound.ExpectedIdentity != *cfg.ExpectedIdentity || command != `ecctl call ram GetRole --region cn-hangzhou --request '{"RoleName":"`+name+`"}' --output=json` {
					t.Errorf("created role lookup lost its exact binding: config=%+v command=%s", bound, command)
				}
				return tc.result
			})
			if (err == nil) != tc.match || calls != 1 {
				t.Fatalf("first role identity check = %v, calls=%d", err, calls)
			}
		})
	}
	entry.VolumeFixtureRoleID = ""
	if _, err := VolumeFixtureRoleMatches(context.Background(), cfg, entry, journal, func(context.Context, execpkg.Config, string) execpkg.Result {
		return success("")
	}); err != nil {
		t.Fatalf("named role without a creation ID was rejected: %v", err)
	}
}

func TestVolumeFixtureFirstPolicyDeleteUsesAssignedName(t *testing.T) {
	journal := volumeFixtureTestJournal()
	journal.EcctlBin = "/tmp/ecctl"
	journal.VolumeFixtureBinding.Profile = "fixture-profile"
	name := journal.VolumeFixtureBucket + "-read"
	entry := report.Resource{Scope: "stack", ExecutionID: journal.ExecutionID, VolumeFixtureKind: "policy", VolumeFixturePolicyCreateDate: "2026-11-04T12:00:00Z", VolumeFixtureDeleteProtocol: 1,
		Teardown: `ecctl call ram DeletePolicy --region cn-hangzhou --request '{"PolicyName":"` + name + `"}'`, RegionRole: "primary", Region: journal.Region}
	cfg := execpkg.Config{Bin: journal.EcctlBin, Region: journal.Region, Profile: "fixture-profile", ExpectedIdentity: &journal.VolumeFixtureBinding.Identity}
	for _, tc := range []struct {
		name, currentDate string
		match             bool
	}{
		{"original", "2026-11-04T12:00:00Z", true},
		{"different date", "2026-11-05T12:00:00Z", true},
		{"missing date", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			_, err := VolumeFixturePolicyMatches(context.Background(), cfg, entry, journal, func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				calls++
				if command != `ecctl call ram GetPolicy --region cn-hangzhou --request '{"PolicyName":"`+name+`","PolicyType":"Custom"}' --output=json` {
					t.Errorf("changed policy read: %s", command)
				}
				return execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"product": "ram", "operation": "GetPolicy", "region": journal.Region,
					"response": map[string]any{"RequestId": "request-get-policy", "Policy": map[string]any{"PolicyName": name, "PolicyType": "Custom", "CreateDate": tc.currentDate}}}}
			})
			if (err == nil) != tc.match || calls != 1 {
				t.Fatalf("policy match=%v calls=%d", err, calls)
			}
		})
	}
	entry.VolumeFixturePolicyCreateDate = ""
	if _, err := VolumeFixturePolicyMatches(context.Background(), cfg, entry, journal, func(context.Context, execpkg.Config, string) execpkg.Result {
		return execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"product": "ram", "operation": "GetPolicy", "region": journal.Region,
			"response": map[string]any{"RequestId": "request-get-policy", "Policy": map[string]any{"PolicyName": name, "PolicyType": "Custom"}}}}
	}); err != nil {
		t.Fatalf("named policy without a creation date was rejected: %v", err)
	}
}

func TestVolumeFixtureFirstBucketDeleteUsesAssignedNameAndOwner(t *testing.T) {
	journal := volumeFixtureTestJournal()
	journal.EcctlBin = "/tmp/ecctl"
	journal.VolumeFixtureBinding.Profile = "fixture-profile"
	bucket := journal.VolumeFixtureBucket
	entry := report.Resource{Scope: "stack", ExecutionID: journal.ExecutionID, VolumeFixtureKind: "bucket", VolumeFixtureBucketCreationDate: "2026-11-04T12:00:00Z", VolumeFixtureDeleteProtocol: 1,
		Teardown: `ecctl call oss DeleteBucket --request '{"Bucket":"` + bucket + `"}' --region cn-hangzhou`, RegionRole: "primary", Region: journal.Region}
	cfg := execpkg.Config{Bin: journal.EcctlBin, Region: journal.Region, Profile: "fixture-profile", ExpectedIdentity: &journal.VolumeFixtureBinding.Identity}
	for _, tc := range []struct {
		name, date, owner  string
		match              bool
		wrapped, malformed bool
	}{
		{"original", "2026-11-04T12:00:00Z", "1234567890123456", true, false, false},
		{"different date", "2026-11-05T12:00:00Z", "1234567890123456", true, false, false},
		{"different owner", "2026-11-04T12:00:00Z", "0000000000000000", false, false, false},
		{"obsolete wrapped response", "2026-11-04T12:00:00Z", "1234567890123456", false, true, false},
		{"malformed bucket response", "2026-11-04T12:00:00Z", "1234567890123456", false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			_, err := VolumeFixtureBucketMatches(context.Background(), cfg, entry, journal, func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				calls++
				if command != `ecctl call oss GetBucketInfo --request '{"Bucket":"`+bucket+`"}' --region cn-hangzhou --output=json` {
					t.Errorf("changed bucket read: %s", command)
				}
				bucketInfo := map[string]any{"Name": bucket, "Location": "oss-cn-hangzhou", "ExtranetEndpoint": "oss-cn-hangzhou.aliyuncs.com", "CreationDate": tc.date, "Owner": map[string]any{"ID": tc.owner}}
				response := map[string]any{"Bucket": bucketInfo}
				if tc.wrapped {
					response = map[string]any{"BucketInfo": map[string]any{"Bucket": bucketInfo}}
				} else if tc.malformed {
					response = map[string]any{"Bucket": "malformed"}
				}
				return execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"product": "oss", "operation": "GetBucketInfo", "region": journal.Region,
					"response": response}}
			})
			if (err == nil) != tc.match || calls != 1 {
				t.Fatalf("bucket match=%v calls=%d", err, calls)
			}
		})
	}
	entry.VolumeFixtureBucketCreationDate = ""
	if _, err := VolumeFixtureBucketMatches(context.Background(), cfg, entry, journal, func(context.Context, execpkg.Config, string) execpkg.Result {
		return execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"product": "oss", "operation": "GetBucketInfo", "region": journal.Region,
			"response": map[string]any{"Bucket": map[string]any{"Name": bucket, "Location": "oss-cn-hangzhou", "ExtranetEndpoint": "oss-cn-hangzhou.aliyuncs.com", "Owner": map[string]any{"ID": journal.VolumeFixtureBinding.Identity.AccountID}}}}}
	}); err != nil {
		t.Fatalf("named bucket without a creation date was rejected: %v", err)
	}
}

func TestVolumeFixtureAttachmentRecoveryReadsOriginalEndpointsAndRelationship(t *testing.T) {
	journal := volumeFixtureTestJournal()
	journal.EcctlBin = "/tmp/ecctl"
	journal.ExecutionID = "execution"
	journal.VolumeFixtureBinding.Profile = "fixture-profile"
	bucket := journal.VolumeFixtureBucket
	role := report.Resource{Scope: "stack", ExecutionID: journal.ExecutionID, RegionRole: "primary", Region: journal.Region, VolumeFixtureKind: "role", VolumeFixtureRoleID: "role-created", VolumeFixtureDeleteProtocol: 1,
		Teardown: `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role"}'`}
	policy := report.Resource{Scope: "stack", ExecutionID: journal.ExecutionID, RegionRole: "primary", Region: journal.Region, VolumeFixtureKind: "policy", VolumeFixturePolicyCreateDate: "2026-11-04T12:00:00Z", VolumeFixtureDeleteProtocol: 1,
		Teardown: `ecctl call ram DeletePolicy --region cn-hangzhou --request '{"PolicyName":"` + bucket + `-read"}'`}
	attachment := report.Resource{Scope: "stack", ExecutionID: journal.ExecutionID, RegionRole: "primary", Region: journal.Region, VolumeFixtureKind: "attachment", VolumeFixtureDeleteProtocol: 1, VolumeFixtureDeleteAttempted: true,
		Teardown: `ecctl call ram DetachPolicyFromRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role","PolicyName":"` + bucket + `-read","PolicyType":"Custom"}'`}
	journal.Entries = []report.Resource{role, policy, attachment}
	cfg := execpkg.Config{Bin: journal.EcctlBin, Region: journal.Region, Profile: "fixture-profile", ExpectedIdentity: &journal.VolumeFixtureBinding.Identity}
	for _, tc := range []struct {
		name, roleID, policyDate string
		attached                 bool
		absent                   bool
		documented               bool
	}{
		{"absent after first detach", "role-created", "2026-11-04T12:00:00Z", false, true, true},
		{"same-name relation present", "role-created", "2026-11-04T12:00:00Z", true, false, true},
		{"different role ID", "role-replaced", "2026-11-04T12:00:00Z", false, true, false},
		{"different policy date", "role-created", "2026-11-05T12:00:00Z", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := []string{}
			_, err := VolumeFixtureAttachmentAbsent(context.Background(), cfg, attachment, journal, func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				calls = append(calls, command)
				switch {
				case strings.Contains(command, "GetRole"):
					return execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"product": "ram", "operation": "GetRole", "region": journal.Region, "response": map[string]any{"RequestId": "request-role", "Role": map[string]any{"RoleName": bucket + "-role", "RoleId": tc.roleID, "Arn": "acs:ram::1234567890123456:role/" + bucket + "-role"}}}}
				case strings.Contains(command, "GetPolicy"):
					return execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"product": "ram", "operation": "GetPolicy", "region": journal.Region, "response": map[string]any{"RequestId": "request-policy", "Policy": map[string]any{"PolicyName": bucket + "-read", "PolicyType": "Custom", "CreateDate": tc.policyDate}}}}
				case strings.Contains(command, "ListPoliciesForRole"):
					items := []any{}
					if tc.attached {
						items = append(items, map[string]any{"PolicyName": bucket + "-read", "PolicyType": "Custom"})
					}
					if tc.documented {
						return execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"product": "ram", "operation": "ListPoliciesForRole", "region": journal.Region, "response": map[string]any{"RequestId": "request-list", "Policies": items}}}
					}
					return execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"product": "ram", "operation": "ListPoliciesForRole", "region": journal.Region, "response": map[string]any{"RequestId": "request-list", "TotalCount": float64(len(items)), "Policies": map[string]any{"Policy": items}}}}
				}
				t.Fatalf("recovery attempted a mutation: %s", command)
				return execpkg.Result{}
			})
			if (err == nil) != tc.absent || len(calls) < 1 || tc.absent && len(calls) != 3 {
				t.Fatalf("attachment absence=%v calls=%v", err, calls)
			}
		})
	}
	journal.Entries = []report.Resource{attachment}
	if _, err := VolumeFixtureAttachmentAbsent(context.Background(), cfg, attachment, journal, func(context.Context, execpkg.Config, string) execpkg.Result {
		t.Fatal("missing original RAM parents reached provider")
		return execpkg.Result{}
	}); err == nil {
		t.Fatal("old attachment journal adopted current same-name endpoints")
	}
}

func TestVolumeFixturePolicyAbsenceRequiresExactReadResult(t *testing.T) {
	journal := volumeFixtureTestJournal()
	journal.EcctlBin = "/tmp/ecctl"
	journal.VolumeFixtureBinding.Profile = "fixture-profile"
	bucket := journal.VolumeFixtureBucket
	entry := report.Resource{Scope: "stack", ExecutionID: journal.ExecutionID, VolumeFixtureKind: "policy", Teardown: `ecctl call ram DeletePolicy --region cn-hangzhou --request '{"PolicyName":"` + bucket + `-read"}'`, RegionRole: "primary", Region: journal.Region, VolumeFixtureDeleteProtocol: 1, VolumeFixtureDeleteAttempted: true}
	cfg := execpkg.Config{Bin: journal.EcctlBin, Region: journal.Region, Profile: "fixture-profile", ExpectedIdentity: &journal.VolumeFixtureBinding.Identity}
	exact := func(action, code string) execpkg.Result {
		return execpkg.Result{Started: true, Exit: 4, JSON: map[string]any{"error": map[string]any{"kind": "not_found", "code": "NotFound"}, "actions": []any{map[string]any{"action_name": action, "code": code, "provider_verified": true}}}}
	}
	for _, tc := range []struct {
		name   string
		result execpkg.Result
		absent bool
	}{
		{"exact absence", exact("GetPolicy", "EntityNotExist.Policy"), true},
		{"unverified diagnostic", execpkg.Result{Started: true, Exit: 4, JSON: map[string]any{"error": map[string]any{"kind": "not_found", "code": "NotFound"}, "actions": []any{map[string]any{"action_name": "GetPolicy", "code": "EntityNotExist.Policy"}}}}, false},
		{"replacement exists", execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"product": "ram", "operation": "GetPolicy", "response": map[string]any{"Policy": map[string]any{"PolicyName": bucket + "-read"}}}}, false},
		{"generic 404", exact("GetPolicy", "404"), false},
		{"auth not found", exact("GetPolicy", "InvalidAccessKeyId.NotFound"), false},
		{"wrong action", exact("DeletePolicy", "EntityNotExist.Policy"), false},
		{"wrong absence kind", execpkg.Result{Started: true, Exit: 4, JSON: map[string]any{"error": map[string]any{"kind": "provider", "code": "NotFound"}, "actions": []any{map[string]any{"action_name": "GetPolicy", "code": "EntityNotExist.Policy"}}}}, false},
		{"unstarted read", execpkg.Result{Exit: 4, JSON: map[string]any{"error": map[string]any{"kind": "not_found", "code": "NotFound"}, "actions": []any{map[string]any{"action_name": "GetPolicy", "code": "EntityNotExist.Policy"}}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			_, err := VolumeFixturePolicyAbsent(context.Background(), cfg, entry, journal, func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				calls++
				if command != `ecctl call ram GetPolicy --region cn-hangzhou --request '{"PolicyName":"`+bucket+`-read","PolicyType":"Custom"}' --output=json` {
					t.Errorf("policy read target changed: %q", command)
				}
				return tc.result
			})
			if (err == nil) != tc.absent || calls != 1 {
				t.Fatalf("policy absence = %v, calls=%d", err, calls)
			}
		})
	}
	cfg.Profile = "other-profile"
	if _, err := VolumeFixturePolicyAbsent(context.Background(), cfg, entry, journal, func(context.Context, execpkg.Config, string) execpkg.Result {
		t.Fatal("changed policy profile reached provider")
		return execpkg.Result{}
	}); err == nil {
		t.Fatal("changed policy profile was admitted")
	}
}

func TestVolumeFixtureBucketAbsenceRejectsSameNameReplacement(t *testing.T) {
	journal := volumeFixtureTestJournal()
	journal.EcctlBin = "/tmp/ecctl"
	journal.VolumeFixtureBinding.Profile = "fixture-profile"
	bucket := journal.VolumeFixtureBucket
	entry := report.Resource{Scope: "stack", ExecutionID: journal.ExecutionID, VolumeFixtureKind: "bucket", Teardown: `ecctl call oss DeleteBucket --request '{"Bucket":"` + bucket + `"}' --region cn-hangzhou`, RegionRole: "primary", Region: journal.Region, VolumeFixtureDeleteAttempted: true}
	cfg := execpkg.Config{Bin: journal.EcctlBin, Region: journal.Region, Profile: "fixture-profile", ExpectedIdentity: &journal.VolumeFixtureBinding.Identity}
	for _, tc := range []struct {
		name   string
		result execpkg.Result
		absent bool
	}{
		{"exact absence", execpkg.Result{Started: true, Exit: 4, JSON: map[string]any{"error": map[string]any{"kind": "not_found", "code": "NotFound", "message": bucket + " not found"}, "actions": []any{map[string]any{"action_name": "ListObjects", "code": "NoSuchBucket", "provider_verified": true}}}}, true},
		{"unverified diagnostic", execpkg.Result{Started: true, Exit: 4, JSON: map[string]any{"error": map[string]any{"kind": "not_found", "code": "NotFound", "message": bucket + " not found"}, "actions": []any{map[string]any{"action_name": "ListObjects", "code": "NoSuchBucket"}}}}, false},
		{"empty replacement", execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"product": "oss", "operation": "ListObjects", "response": map[string]any{"Name": bucket, "IsTruncated": false, "KeyCount": "0"}}}, false},
		{"generic 404", execpkg.Result{Started: true, Exit: 4, JSON: map[string]any{"error": map[string]any{"kind": "not_found", "code": "NotFound", "message": bucket + " not found"}}}, false},
		{"wrong bucket", execpkg.Result{Started: true, Exit: 4, JSON: map[string]any{"error": map[string]any{"kind": "not_found", "code": "NotFound", "message": "other-bucket not found"}, "actions": []any{map[string]any{"action_name": "ListObjects", "code": "NoSuchBucket"}}}}, false},
		{"wrong operation", execpkg.Result{Started: true, Exit: 4, JSON: map[string]any{"error": map[string]any{"kind": "not_found", "code": "NotFound", "message": bucket + " not found"}, "actions": []any{map[string]any{"action_name": "DeleteBucket", "code": "NoSuchBucket"}}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			_, err := VolumeFixtureBucketAbsent(context.Background(), cfg, entry, journal, func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				calls++
				if command != `ecctl call oss ListObjects --request '{"Bucket":"`+bucket+`","MaxKeys":1}' --region cn-hangzhou --output=json` {
					t.Errorf("bucket read target changed: %q", command)
				}
				return tc.result
			})
			if (err == nil) != tc.absent || calls != 1 {
				t.Fatalf("bucket absence = %v, calls=%d", err, calls)
			}
		})
	}
	cfg.Profile = "other-profile"
	if _, err := VolumeFixtureBucketAbsent(context.Background(), cfg, entry, journal, func(context.Context, execpkg.Config, string) execpkg.Result {
		t.Fatal("changed profile reached provider")
		return execpkg.Result{}
	}); err == nil {
		t.Fatal("changed profile was admitted")
	}
}

func TestVolumeFixtureTeardownAcceptsOnlyOwnedTargets(t *testing.T) {
	journal := volumeFixtureTestJournal()
	bucket := journal.VolumeFixtureBucket
	cases := []struct{ kind, command string }{
		{"bucket", `ecctl call oss DeleteBucket --request '{"Bucket":"` + bucket + `"}' --region cn-hangzhou`},
		{"role", `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role"}'`},
		{"policy", `ecctl call ram DeletePolicy --region cn-hangzhou --request '{"PolicyName":"` + bucket + `-read"}'`},
		{"attachment", `ecctl call ram DetachPolicyFromRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role","PolicyName":"` + bucket + `-read","PolicyType":"Custom"}'`},
	}
	for _, tc := range cases {
		entry := report.Resource{Scope: "stack", ExecutionID: journal.ExecutionID, VolumeFixtureKind: tc.kind, RegionRole: "primary", Region: "cn-hangzhou", Teardown: tc.command}
		if err := ValidateVolumeFixtureTeardown(entry, journal); err != nil {
			t.Fatalf("%s rejected: %v", tc.kind, err)
		}
		entry.VolumeFixtureDeleteProtocol = 1
		entry.VolumeFixtureDeleteAttempted = true
		if err := ValidateVolumeFixtureTeardown(entry, journal); (err == nil) != VolumeFixtureFencedKind(tc.kind) {
			t.Fatalf("%s protocol validation = %v", tc.kind, err)
		}
		entry.VolumeFixtureDeleteProtocol = 0
		entry.VolumeFixtureDeleteAttempted = false
		for _, mutation := range []string{strings.Replace(tc.command, bucket, "other-bucket", 1), tc.command + " --force", strings.Replace(tc.command, "cn-hangzhou", "cn-beijing", 1)} {
			entry.Teardown = mutation
			if err := ValidateVolumeFixtureTeardown(entry, journal); err == nil {
				t.Fatalf("%s accepted changed teardown %q", tc.kind, mutation)
			}
		}
	}
}

func TestVolumeFixtureReservationRequiresSettledAncestors(t *testing.T) {
	journal := volumeFixtureTestJournal()
	journal.ExecutionID = "execution"
	bucket := journal.VolumeFixtureBucket
	entry := report.Resource{Scope: "stack", VolumeFixtureKind: "bucket", RegionRole: "primary", Region: journal.Region, ExecutionID: journal.ExecutionID,
		Teardown: `ecctl call oss DeleteBucket --request '{"Bucket":"` + bucket + `"}' --region cn-hangzhou`}
	if err := CheckVolumeFixtureParents("bucket", journal); err != nil {
		t.Fatalf("first bucket reservation: %v", err)
	}
	if err := CheckVolumeFixtureParents("role", journal); err == nil {
		t.Fatal("role reservation accepted a missing bucket")
	}
	journal.Entries = []report.Resource{entry}
	if err := CheckVolumeFixtureParents("role", journal); err != nil {
		t.Fatalf("settled bucket did not support role reservation: %v", err)
	}
	journal.Entries[0].VolumeFixturePending = true
	if err := CheckVolumeFixtureParents("role", journal); err == nil {
		t.Fatal("role reservation accepted an uncertain bucket")
	}
	journal.Entries[0].VolumeFixturePending = false
	journal.Entries[0].ExecutionID = "other-execution"
	if err := CheckVolumeFixtureParents("role", journal); err == nil {
		t.Fatal("role reservation accepted a foreign bucket")
	}
	journal.Entries[0].ExecutionID = journal.ExecutionID
	journal.Entries = append(journal.Entries, entry)
	if err := CheckVolumeFixtureParents("role", journal); err == nil {
		t.Fatal("role reservation accepted duplicate bucket ownership")
	}
}

func TestVolumeFixtureCleanupUsesOriginalNativeSelectors(t *testing.T) {
	for _, selector := range []string{"default", "alias"} {
		t.Run(selector, func(t *testing.T) {
			if selector == "alias" && runtime.GOOS == "windows" {
				t.Skip("symlink selector requires POSIX filesystem semantics")
			}
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "cleanup.json")
			journal := volumeFixtureTestJournal()
			journal.JournalPath, journal.RunID, journal.ExecutionID = path, "run", "execution"
			journal.Surface, journal.EcctlBin = "public", "ecctl-public"
			bucket := journal.VolumeFixtureBucket
			entry := report.Resource{Scope: "stack", Teardown: `ecctl call oss DeleteBucket --request '{"Bucket":"` + bucket + `"}' --region cn-hangzhou`,
				RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: journal.ExecutionID, VolumeFixtureKind: "bucket"}
			journal.Entries = []report.Resource{entry}
			sourceEnv := []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=", "ECCTL_ALIYUN_CONFIG_PATH="}
			if selector == "alias" {
				real, alias := filepath.Join(dir, "real.json"), filepath.Join(dir, "alias.json")
				if err := os.WriteFile(real, []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(real, alias); err != nil {
					t.Fatal(err)
				}
				sourceEnv = []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + alias, "ECCTL_ALIYUN_CONFIG_PATH=" + alias}
			}
			paths, err := CaptureTemplateConfig(sourceEnv)
			if err != nil {
				t.Fatal(err)
			}
			exit := 0
			started := true
			intent := TemplateCreateIntent{Version: 2, Resource: "volume", RunID: journal.RunID, ExecutionID: journal.ExecutionID, CaseScope: "stack",
				RegionRole: journal.RegionRole, Region: journal.Region, Surface: journal.Surface, EcctlBin: journal.EcctlBin,
				TeamID: "team-1", Name: "ecctl-p-0123456789abcdef01234567", Profile: "owner", ConfigPaths: paths,
				Selectors: map[string]string{}, Identity: journal.VolumeFixtureBinding.Identity, CreatedAt: time.Now().UTC(),
				State: "journaled", Started: &started, Exit: &exit, AcceptedVolumeID: "vol-owned",
				StdoutSHA256: strings.Repeat("a", 64), StderrSHA256: strings.Repeat("b", 64),
				Deletes: []string{"ecctl sandbox volume delete vol-owned --region cn-hangzhou --profile owner"}}
			for _, name := range TemplateSelectorNames {
				intent.Selectors[name] = TemplateEnvironmentValue(name, sourceEnv)
			}
			intentPath := path + ".template-create-0123456789abcdef01234567.json"
			journal.NativeJournalPath, journal.NativeIntentPaths = path, []string{intentPath}
			journalBody, _ := json.Marshal(journal)
			intentBody, _ := json.Marshal(intent)
			if err := os.WriteFile(path, journalBody, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(intentPath, intentBody, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := VolumeFixtureCleanupReady(path, journal, entry, sourceEnv); err == nil {
				t.Fatal("accepted Volume without a finalizer released fixture cleanup")
			}
			bound := intent.BoundConfig(execpkg.Config{Env: sourceEnv})
			if err := VolumeFixtureCleanupReady(path, journal, entry, bound.Env); err == nil {
				t.Fatal("test did not expose the canonical provider selector drift")
			}
			journal.Entries = append(journal.Entries, report.Resource{Scope: intent.CaseScope, Teardown: intent.Deletes[0], ExecutionID: intent.ExecutionID, RegionRole: intent.RegionRole, Region: intent.Region})
			journalBody, _ = json.Marshal(journal)
			if err := os.WriteFile(path, journalBody, 0o600); err != nil {
				t.Fatal(err)
			}
			outstanding, err := OutstandingVolumeChild(path, journal, sourceEnv)
			if err != nil || !outstanding {
				t.Fatalf("accepted Volume finalizer lost its child fence: %v %v", outstanding, err)
			}
			journal.Entries = append(journal.Entries, journal.Entries[len(journal.Entries)-1])
			journalBody, _ = json.Marshal(journal)
			if err := os.WriteFile(path, journalBody, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := OutstandingVolumeChild(path, journal, sourceEnv); err == nil {
				t.Fatal("duplicate accepted Volume finalizers were accepted")
			}
		})
	}
}

func TestVolumeFixtureBucketMustBeProvenEmpty(t *testing.T) {
	journal := volumeFixtureTestJournal()
	cfg := execpkg.Config{Region: "cn-hangzhou", ExpectedIdentity: &journal.VolumeFixtureBinding.Identity}
	for _, tc := range []struct {
		name, count string
		contents    any
		wantError   bool
	}{
		{"empty", "0", nil, false},
		{"object", "1", []any{map[string]any{"Key": "unexpected"}}, true},
		{"unknown", "", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				if command != `ecctl call oss ListObjects --request '{"Bucket":"`+journal.VolumeFixtureBucket+`","MaxKeys":1}' --region cn-hangzhou --output=json` {
					t.Fatalf("unexpected probe %s", command)
				}
				response := map[string]any{"Name": journal.VolumeFixtureBucket, "IsTruncated": false}
				if tc.count != "" {
					response["KeyCount"] = tc.count
				}
				if tc.contents != nil {
					response["Contents"] = tc.contents
				}
				return execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"product": "oss", "operation": "ListObjects", "response": response}}
			}
			presence, err := VolumeFixtureBucketEmpty(context.Background(), cfg, journal.VolumeFixtureBucket, probe)
			if (err != nil) != tc.wantError || !tc.wantError && presence != VolumeFixtureBucketPresentEmpty || tc.wantError && presence != VolumeFixtureBucketUnknown {
				t.Fatal(fmt.Sprintf("bucket probe = %v, %v, wantError=%v", presence, err, tc.wantError))
			}
		})
	}
}

func TestVolumeFixtureBucketAbsenceRequiresExactPinnedProviderProof(t *testing.T) {
	journal := volumeFixtureTestJournal()
	bucket := journal.VolumeFixtureBucket
	for _, tc := range []struct {
		name      string
		mutate    func(*execpkg.Result)
		unpin     bool
		wantError bool
	}{
		{name: "NoSuchBucket", wantError: false},
		{name: "unverified diagnostic", mutate: func(result *execpkg.Result) {
			result.JSON.(map[string]any)["actions"].([]any)[0].(map[string]any)["provider_verified"] = false
		}, wantError: true},
		{name: "unbound credential", unpin: true, wantError: true},
		{name: "generic 404", mutate: func(result *execpkg.Result) {
			result.JSON.(map[string]any)["actions"].([]any)[0].(map[string]any)["code"] = "404"
		}, wantError: true},
		{name: "foreign target", mutate: func(result *execpkg.Result) {
			result.JSON.(map[string]any)["error"].(map[string]any)["message"] = "other-bucket not found"
		}, wantError: true},
		{name: "wrong operation", mutate: func(result *execpkg.Result) {
			result.JSON.(map[string]any)["actions"].([]any)[0].(map[string]any)["action_name"] = "GetBucketInfo"
		}, wantError: true},
		{name: "no launched probe", mutate: func(result *execpkg.Result) { result.Started = false }, wantError: true},
		{name: "error response carries objects", mutate: func(result *execpkg.Result) {
			result.JSON.(map[string]any)["response"] = map[string]any{"Contents": []any{map[string]any{"Key": "foreign"}}}
		}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := execpkg.Config{Region: "cn-hangzhou", ExpectedIdentity: &journal.VolumeFixtureBinding.Identity}
			if tc.unpin {
				cfg.ExpectedIdentity = nil
			}
			probe := func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				if command != `ecctl call oss ListObjects --request '{"Bucket":"`+bucket+`","MaxKeys":1}' --region cn-hangzhou --output=json` {
					t.Fatalf("unexpected probe %s", command)
				}
				result := execpkg.Result{Started: true, Exit: 4, JSON: map[string]any{
					"error":   map[string]any{"kind": "not_found", "code": "NotFound", "message": bucket + " not found"},
					"actions": []any{map[string]any{"action_name": "ListObjects", "code": "NoSuchBucket", "provider_verified": true}},
				}}
				if tc.mutate != nil {
					tc.mutate(&result)
				}
				return result
			}
			presence, err := VolumeFixtureBucketEmpty(context.Background(), cfg, bucket, probe)
			if (err != nil) != tc.wantError || !tc.wantError && presence != VolumeFixtureBucketVerifiedAbsent || tc.wantError && presence != VolumeFixtureBucketUnknown {
				t.Fatalf("absence proof = %v, %v, wantError=%v", presence, err, tc.wantError)
			}
		})
	}
}
