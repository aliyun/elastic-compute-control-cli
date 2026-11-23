package sweeper

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

// The test subprocess and journal are isolated to one fake provider namespace.
// The production replay path receives a nil mutation scope.
func grantSyntheticVolumeFixtureReplayScope(t *testing.T, opt *ReplayOptions, path, bucket string) {
	t.Helper()
	if opt.fixtureMutationScope != nil {
		t.Fatal("synthetic Volume replay scope already installed")
	}
	runID, surface, bin := opt.RunID, opt.Surface, opt.Config.Bin
	opt.fixtureMutationScope = func(journal report.CleanupJournal, entry report.Resource, cfg execpkg.Config) bool {
		return journal.JournalPath == path && journal.RunID == runID && journal.Surface == surface &&
			journal.VolumeFixtureBucket == bucket && journal.EcctlBin == bin && cfg.Bin == bin &&
			entry.Scope == "stack" && entry.ExecutionID == journal.ExecutionID &&
			(entry.VolumeFixtureKind == "bucket" || entry.VolumeFixtureKind == "role" ||
				entry.VolumeFixtureKind == "policy" || entry.VolumeFixtureKind == "attachment")
	}
}

func TestReplayRejectsNonprivateFixtureOnlyJournalBeforeProvider(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode bits and fake provider use Unix semantics")
	}
	dir := canonicalJournalTestDir(t)
	binding := quotaReplayBinding(t, dir)
	bucket := "ecctl-e2e-20261104t120000-123456abcdef"
	path, bin, calls := filepath.Join(dir, "cleanup.json"), filepath.Join(dir, "ecctl"), filepath.Join(dir, "calls")
	t.Setenv("FAKE_FIXTURE_JOURNAL_CALLS", calls)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho called >> \"$FAKE_FIXTURE_JOURNAL_CALLS\"\nexit 90\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	journal := report.CleanupJournal{
		Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: bin,
		VolumeFixtureBucket: bucket, VolumeFixtureBinding: binding,
		Entries: []report.Resource{{Scope: "stack", Teardown: `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role"}'`,
			RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", VolumeFixtureKind: "role", VolumeFixtureDeleteProtocol: 1}},
	}
	before, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := ReplayJournalWithOptions(context.Background(), path, ReplayOptions{Config: execpkg.Config{Bin: bin, Region: "cn-hangzhou", Env: []string{"HOME=" + dir}}, RunID: "run", Surface: "public"})
	if result != nil || err == nil || !strings.Contains(err.Error(), "protected cleanup journal is not a private regular file") {
		t.Fatalf("nonprivate fixture journal was admitted: result=%+v err=%v", result, err)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil || string(after) != string(before) {
		t.Fatalf("rejected journal changed: err=%v", readErr)
	}
	if log, readErr := os.ReadFile(calls); !os.IsNotExist(readErr) {
		t.Fatalf("rejected journal reached provider: calls=%s err=%v", log, readErr)
	}
}

func TestReplayVolumeBucketVerifiedAbsentRetiresWithoutMutationScope(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake provider uses a shell script")
	}
	dir := canonicalJournalTestDir(t)
	binding := quotaReplayBinding(t, dir)
	bucket := "ecctl-e2e-20261104t120000-123456abcdef"
	path, bin, calls := filepath.Join(dir, "cleanup.json"), filepath.Join(dir, "ecctl"), filepath.Join(dir, "calls")
	t.Setenv("FAKE_BUCKET_CALLS", calls)
	fake := `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_BUCKET_CALLS"
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}';;
  *"call oss ListObjects"*) echo '{"error":{"kind":"not_found","code":"NotFound","message":"ecctl-e2e-20261104t120000-123456abcdef not found"},"actions":[{"action_name":"ListObjects","code":"NoSuchBucket","provider_verified":true}]}'; exit 4;;
  *) exit 90;;
esac
`
	if err := os.WriteFile(bin, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	entry := report.Resource{Scope: "stack", Teardown: `ecctl call oss DeleteBucket --request '{"Bucket":"` + bucket + `"}' --region cn-hangzhou`,
		RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", VolumeFixtureKind: "bucket", VolumeFixtureDeleteProtocol: 1,
		VolumeFixtureBucketCreationDate: "2026-11-04T12:00:00Z"}
	journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: bin,
		VolumeFixtureBucket: bucket, VolumeFixtureBinding: binding, Entries: []report.Resource{entry}}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	opt := ReplayOptions{Config: execpkg.Config{Bin: bin, Region: "cn-hangzhou", Env: []string{"HOME=" + dir}}, RunID: "run", Surface: "public"}
	result, err := ReplayJournalWithOptions(context.Background(), path, opt)
	if err != nil || result == nil || result.Errors != 0 || result.Deleted != 1 {
		t.Fatalf("verified absent bucket was not retired: result=%+v err=%v", result, err)
	}
	stored, _, err := readReplayJournal(path)
	if err != nil || len(stored.Entries) != 0 {
		t.Fatalf("absent bucket finalizer was retained: %+v %v", stored, err)
	}
	log, err := os.ReadFile(calls)
	if err != nil || strings.Contains(string(log), "DeleteBucket") || !strings.Contains(string(log), "ListObjects") {
		t.Fatalf("absent bucket reached a mutation: %s %v", log, err)
	}
}

func TestReplayLegacyAccountVolumeFixtureRetainsDependencies(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake provider uses a shell script")
	}
	dir := canonicalJournalTestDir(t)
	binding := quotaReplayBinding(t, dir)
	binding.Identity = execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "Account"}
	bucket := "ecctl-e2e-20261104t120000-123456abcdef"
	path, bin := filepath.Join(dir, "cleanup.json"), filepath.Join(dir, "ecctl")
	calls := filepath.Join(dir, "provider-calls")
	t.Setenv("FAKE_VOLUME_ACCOUNT_CALLS", calls)
	fake := `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_VOLUME_ACCOUNT_CALLS"
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"Account"}}';;
  *) exit 90;;
esac
`
	if err := os.WriteFile(bin, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	journal := report.CleanupJournal{
		Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: bin,
		VolumeFixtureBucket: bucket, VolumeFixtureBinding: binding,
		Entries: []report.Resource{
			{Scope: "stack", Teardown: `ecctl call oss DeleteBucket --request '{"Bucket":"` + bucket + `"}' --region cn-hangzhou`, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", VolumeFixtureKind: "bucket", VolumeFixtureDeleteProtocol: 1},
			{Scope: "stack", Teardown: `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role"}'`, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", VolumeFixtureKind: "role", VolumeFixtureDeleteProtocol: 1},
		},
	}
	before, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := ReplayJournalWithOptions(context.Background(), path, ReplayOptions{Config: execpkg.Config{Bin: bin, Region: "cn-hangzhou", Env: []string{"HOME=" + dir}}, RunID: "run", Surface: "public"})
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	if result != nil {
		detail += strings.Join(result.Details, "\n")
	}
	if (result == nil && err == nil) || result != nil && result.Deleted != 0 || !strings.Contains(detail, "RAMUser or AssumedRoleUser") {
		t.Fatalf("old Account fixture replay did not fail closed: result=%+v err=%v", result, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("old Account fixture journal changed: err=%v", err)
	}
	log, _ := os.ReadFile(calls)
	if strings.Contains(string(log), "call oss ") || strings.Contains(string(log), "call ram ") {
		t.Fatalf("old Account fixture reached a dependency provider: %s", log)
	}
}

func TestReplayFirstRoleDeleteUsesAssignedName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake provider uses a shell script")
	}
	for _, tc := range []struct {
		name, createdID, currentID string
		mode                       string
		wantDeleted                int
	}{
		{"same RoleId", "role-created", "role-created", "present", 1},
		{"different RoleId", "role-created", "role-replacement", "present", 1},
		{"unknown response", "role-created", "", "unknown", 0},
		{"missing creation RoleId", "", "role-created", "present", 1},
		{"old journal exact absence", "", "", "absent", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := canonicalJournalTestDir(t)
			binding := quotaReplayBinding(t, dir)
			bucket := "ecctl-e2e-20261104t120000-123456abcdef"
			path, bin, calls := filepath.Join(dir, "cleanup.json"), filepath.Join(dir, "ecctl"), filepath.Join(dir, "calls")
			t.Setenv("FAKE_ROLE_MODE", tc.mode)
			t.Setenv("FAKE_ROLE_CURRENT_ID", tc.currentID)
			t.Setenv("FAKE_ROLE_CALLS", calls)
			t.Setenv("FAKE_ROLE_DELETED", filepath.Join(dir, "role-deleted"))
			fake := `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_ROLE_CALLS"
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}';;
  *"call oss ListObjects"*) echo '{"error":{"kind":"not_found","code":"NotFound","message":"ecctl-e2e-20261104t120000-123456abcdef not found"},"actions":[{"action_name":"ListObjects","code":"NoSuchBucket","provider_verified":true}]}'; exit 4;;
  *"call ram GetRole"*)
    if [ "$FAKE_ROLE_MODE" = absent ] || [ -f "$FAKE_ROLE_DELETED" ]; then
      echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetRole","code":"EntityNotExist.Role","provider_verified":true}]}'
      exit 4
    fi
    if [ "$FAKE_ROLE_MODE" = unknown ]; then echo '{"product":"ram","operation":"GetRole"}'; exit 0; fi
    printf '{"product":"ram","operation":"GetRole","region":"cn-hangzhou","response":{"RequestId":"request-get-role","Role":{"RoleId":"%s","RoleName":"ecctl-e2e-20261104t120000-123456abcdef-role","Arn":"acs:ram::1234567890123456:role/ecctl-e2e-20261104t120000-123456abcdef-role"}}}\n' "$FAKE_ROLE_CURRENT_ID";;
  *"call ram DeleteRole"*) touch "$FAKE_ROLE_DELETED"; echo '{"product":"ram","operation":"DeleteRole","region":"cn-hangzhou","response":{"RequestId":"request-delete-role"}}';;
  *) exit 90;;
esac
`
			if err := os.WriteFile(bin, []byte(fake), 0o755); err != nil {
				t.Fatal(err)
			}
			entry := report.Resource{Scope: "stack", Teardown: `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role"}'`, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", VolumeFixtureKind: "role", VolumeFixtureRoleID: tc.createdID, VolumeFixtureDeleteProtocol: 1}
			journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: bin, VolumeFixtureBucket: bucket, VolumeFixtureBinding: binding, Entries: []report.Resource{entry}}
			body, err := json.Marshal(journal)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatal(err)
			}
			opt := ReplayOptions{Config: execpkg.Config{Bin: bin, Region: "cn-hangzhou", Env: []string{"HOME=" + dir}}, RunID: "run", Surface: "public"}
			grantSyntheticVolumeFixtureReplayScope(t, &opt, path, bucket)
			result, err := ReplayJournalWithOptions(context.Background(), path, opt)
			if err != nil || result == nil || result.Deleted != tc.wantDeleted || result.Errors != 1-tc.wantDeleted {
				t.Fatalf("first role replay = %+v, err=%v", result, err)
			}
			stored, _, err := readReplayJournal(path)
			if err != nil || len(stored.Entries) != 1-tc.wantDeleted {
				t.Fatalf("role replay journal = %+v, err=%v", stored, err)
			}
			if tc.wantDeleted == 0 && (stored.Entries[0].VolumeFixtureDeleteAttempted || stored.Entries[0].VolumeFixtureRoleID != tc.createdID) {
				t.Fatalf("unowned role was adopted or marked for deletion: %+v", stored.Entries[0])
			}
			log, _ := os.ReadFile(calls)
			wantCall := tc.mode == "present"
			if strings.Contains(string(log), "DeleteRole") != wantCall || !strings.Contains(string(log), "GetRole") {
				t.Fatalf("first role replay calls = %s", log)
			}
		})
	}
}

func TestReplayVolumeFixtureRetainsBucketOnUncertaintyOrUnexpectedContents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake provider uses a shell script")
	}
	for _, tc := range []struct {
		name, keyCount, deleteCode, pendingKind, preflight string
		pending                                            bool
		attachment                                         bool
		fullRAM                                            bool
		skipAttachment                                     bool
		pendingTeam                                        bool
		wantDeleted, wantErrors                            int
	}{
		{name: "interrupted-create", keyCount: "0", pending: true, wantErrors: 1},
		{name: "unexpected-object", keyCount: "1", wantErrors: 1},
		{name: "unexpected-object-retains-attachment", keyCount: "1", attachment: true, wantErrors: 1},
		{name: "generic-bucket-404", keyCount: "0", deleteCode: "404", wantErrors: 1},
		{name: "auth-bucket-not-found", keyCount: "0", deleteCode: "InvalidAccessKeyId.NotFound", wantErrors: 1},
		{name: "empty DeleteBucket response with live bucket", keyCount: "0", deleteCode: "EmptyBucket", wantErrors: 1},
		{name: "auth-attachment-not-found", keyCount: "0", deleteCode: "InvalidAccessKeyId.NotFound", attachment: true, wantDeleted: 1, wantErrors: 1},
		{name: "unowned-attachment-cannot-infer-absence", keyCount: "0", deleteCode: "EntityNotExist.Role", attachment: true, wantDeleted: 1, wantErrors: 1},
		{name: "multipart-retains-all-ram", keyCount: "0", deleteCode: "0015-00000302", fullRAM: true, wantErrors: 1},
		{name: "pending-role-retains-all", keyCount: "0", pendingKind: "role", fullRAM: true, wantErrors: 1},
		{name: "pending-policy-retains-all", keyCount: "0", pendingKind: "policy", fullRAM: true, wantErrors: 1},
		{name: "pending-attachment-retains-all", keyCount: "0", pendingKind: "attachment", fullRAM: true, wantErrors: 1},
		{name: "pending-Team-create-retains-all", keyCount: "0", pendingTeam: true, fullRAM: true, wantErrors: 1},
		{name: "empty-bucket-clears-all-ram", keyCount: "0", fullRAM: true, wantDeleted: 4},
		{name: "fresh policy already absent", keyCount: "0", fullRAM: true, skipAttachment: true, preflight: "policy_only_absent", wantDeleted: 3},
		{name: "fresh attachment role already absent", keyCount: "0", fullRAM: true, preflight: "role_absent", wantDeleted: 4},
		{name: "fresh attachment policy already absent", keyCount: "0", fullRAM: true, preflight: "policy_absent", wantDeleted: 4},
		{name: "fresh attachment role permission denied", keyCount: "0", fullRAM: true, preflight: "role_permission", wantDeleted: 1, wantErrors: 1},
		{name: "fresh attachment policy unknown", keyCount: "0", fullRAM: true, preflight: "policy_unknown", wantDeleted: 1, wantErrors: 1},
		{name: "fresh attachment policy malformed absence", keyCount: "0", fullRAM: true, preflight: "policy_malformed", wantDeleted: 1, wantErrors: 1},
		{name: "fresh policy permission denied", keyCount: "0", fullRAM: true, skipAttachment: true, preflight: "policy_only_permission", wantDeleted: 1, wantErrors: 1},
		{name: "fresh policy unknown", keyCount: "0", fullRAM: true, skipAttachment: true, preflight: "policy_only_unknown", wantDeleted: 1, wantErrors: 1},
		{name: "RAM policy Success=false retains parents", keyCount: "0", deleteCode: "FalsePolicy", fullRAM: true, wantDeleted: 2, wantErrors: 1},
		{name: "empty-bucket", keyCount: "0", wantDeleted: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := canonicalJournalTestDir(t)
			binding := quotaReplayBinding(t, dir)
			bucket := "ecctl-e2e-20261104t120000-123456abcdef"
			path := filepath.Join(dir, "cleanup.json")
			bin := filepath.Join(dir, "ecctl")
			calls := filepath.Join(dir, "calls")
			t.Setenv("FAKE_VOLUME_FIXTURE_CALLS", calls)
			t.Setenv("FAKE_VOLUME_FIXTURE_COUNT", tc.keyCount)
			t.Setenv("FAKE_VOLUME_FIXTURE_DELETE_CODE", tc.deleteCode)
			t.Setenv("FAKE_VOLUME_FIXTURE_PREFLIGHT", tc.preflight)
			t.Setenv("FAKE_VOLUME_FIXTURE_DELETED", filepath.Join(dir, "bucket-deleted"))
			fake := `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_VOLUME_FIXTURE_CALLS"
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}';;
  *"call oss ListObjects"*)
    if [ -f "$FAKE_VOLUME_FIXTURE_DELETED" ]; then
      echo '{"error":{"kind":"not_found","code":"NotFound","message":"ecctl-e2e-20261104t120000-123456abcdef not found"},"actions":[{"action_name":"ListObjects","code":"NoSuchBucket","provider_verified":true}]}'
      exit 4
    fi
    printf '{"product":"oss","operation":"ListObjects","response":{"Name":"ecctl-e2e-20261104t120000-123456abcdef","IsTruncated":false,"KeyCount":"%s"}}\n' "$FAKE_VOLUME_FIXTURE_COUNT";;
  *"call oss GetBucketInfo"*) echo '{"product":"oss","operation":"GetBucketInfo","region":"cn-hangzhou","response":{"Bucket":{"Name":"ecctl-e2e-20261104t120000-123456abcdef","Location":"oss-cn-hangzhou","ExtranetEndpoint":"oss-cn-hangzhou.aliyuncs.com","CreationDate":"2026-11-04T12:00:00Z","Owner":{"ID":"1234567890123456"}}}}';;
  *"call ram DetachPolicyFromRole"*)
    if [ "$FAKE_VOLUME_FIXTURE_DELETE_CODE" != "" ] && [ "$FAKE_VOLUME_FIXTURE_DELETE_CODE" != FalsePolicy ]; then
      printf '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"DetachPolicyFromRole","code":"%s"}]}\n' "$FAKE_VOLUME_FIXTURE_DELETE_CODE"
      exit 4
    fi
    echo '{"product":"ram","operation":"DetachPolicyFromRole","region":"cn-hangzhou","response":{"RequestId":"request-detach"}}';;
  *"call ram DeletePolicy"*)
    touch "$FAKE_VOLUME_FIXTURE_DELETED.policy"
    if [ "$FAKE_VOLUME_FIXTURE_DELETE_CODE" = FalsePolicy ]; then
      echo '{"product":"ram","operation":"DeletePolicy","region":"cn-hangzhou","response":{"RequestId":"request-policy","Success":false}}'
    else
      echo '{"product":"ram","operation":"DeletePolicy","region":"cn-hangzhou","response":{"RequestId":"request-policy"}}'
    fi;;
  *"call ram GetPolicy"*)
    if [ "$FAKE_VOLUME_FIXTURE_PREFLIGHT" = policy_only_permission ]; then
      echo '{"error":{"kind":"provider","code":"Forbidden"},"actions":[{"action_name":"GetPolicy","code":"Forbidden","provider_verified":true}]}'
      exit 4
    fi
    if [ "$FAKE_VOLUME_FIXTURE_PREFLIGHT" = policy_absent ] || [ "$FAKE_VOLUME_FIXTURE_PREFLIGHT" = policy_only_absent ]; then
      echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetPolicy","code":"EntityNotExist.Policy","provider_verified":true}]}'
      exit 4
    fi
    if [ "$FAKE_VOLUME_FIXTURE_PREFLIGHT" = policy_malformed ]; then
      echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetPolicy","code":"EntityNotExist.Policy"}]}'
      exit 4
    fi
    if [ "$FAKE_VOLUME_FIXTURE_PREFLIGHT" = policy_unknown ] || [ "$FAKE_VOLUME_FIXTURE_PREFLIGHT" = policy_only_unknown ]; then echo '{"product":"ram","operation":"GetPolicy"}'; exit 0; fi
    if [ -f "$FAKE_VOLUME_FIXTURE_DELETED.policy" ]; then
      echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetPolicy","code":"EntityNotExist.Policy","provider_verified":true}]}'
      exit 4
    fi
    echo '{"product":"ram","operation":"GetPolicy","region":"cn-hangzhou","response":{"RequestId":"request-get-policy","Policy":{"PolicyName":"ecctl-e2e-20261104t120000-123456abcdef-read","PolicyType":"Custom","CreateDate":"2026-11-04T12:00:00Z"}}}';;
  *"call ram ListPoliciesForRole"*) echo '{"product":"ram","operation":"ListPoliciesForRole","region":"cn-hangzhou","response":{"RequestId":"request-list","Policies":{"Policy":[]}}}';;
  *"call ram GetRole"*)
    if [ "$FAKE_VOLUME_FIXTURE_PREFLIGHT" = role_absent ]; then
      echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetRole","code":"EntityNotExist.Role","provider_verified":true}]}'
      exit 4
    fi
    if [ "$FAKE_VOLUME_FIXTURE_PREFLIGHT" = role_permission ]; then
      echo '{"error":{"kind":"provider","code":"Forbidden"},"actions":[{"action_name":"GetRole","code":"Forbidden","provider_verified":true}]}'
      exit 4
    fi
    if [ -f "$FAKE_VOLUME_FIXTURE_DELETED.role" ]; then
      echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetRole","code":"EntityNotExist.Role","provider_verified":true}]}'
      exit 4
    fi
    echo '{"product":"ram","operation":"GetRole","region":"cn-hangzhou","response":{"RequestId":"request-get-role","Role":{"RoleId":"role-created","RoleName":"ecctl-e2e-20261104t120000-123456abcdef-role","Arn":"acs:ram::1234567890123456:role/ecctl-e2e-20261104t120000-123456abcdef-role"}}}';;
  *"call ram DeleteRole"*) touch "$FAKE_VOLUME_FIXTURE_DELETED.role"; echo '{"product":"ram","operation":"DeleteRole","region":"cn-hangzhou","response":{"RequestId":"request-role"}}';;
  *"call oss DeleteBucket"*)
    if [ "$FAKE_VOLUME_FIXTURE_DELETE_CODE" = 0015-00000302 ]; then
      echo '{"error":{"kind":"provider","code":"0015-00000302","message":"unfinished multipart upload"},"actions":[{"action_name":"DeleteBucket","code":"0015-00000302"}]}'
      exit 2
    fi
    if [ "$FAKE_VOLUME_FIXTURE_ATTACHMENT" != 1 ] && { [ "$FAKE_VOLUME_FIXTURE_DELETE_CODE" = 404 ] || [ "$FAKE_VOLUME_FIXTURE_DELETE_CODE" = InvalidAccessKeyId.NotFound ]; }; then
      printf '{"error":{"kind":"not_found","code":"NotFound","message":"ecctl-e2e-20261104t120000-123456abcdef not found"},"actions":[{"action_name":"DeleteBucket","code":"%s"}]}\n' "$FAKE_VOLUME_FIXTURE_DELETE_CODE"
      exit 4
    fi
    if [ "$FAKE_VOLUME_FIXTURE_DELETE_CODE" != EmptyBucket ]; then touch "$FAKE_VOLUME_FIXTURE_DELETED"; fi
    echo '{"product":"oss","operation":"DeleteBucket","region":"cn-hangzhou","response":{}}';;
  *) exit 90;;
esac
`
			if err := os.WriteFile(bin, []byte(fake), 0o755); err != nil {
				t.Fatal(err)
			}
			journal := report.CleanupJournal{
				Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: bin,
				VolumeFixtureBucket: bucket, VolumeFixtureBinding: binding,
				Entries: []report.Resource{{Scope: "stack", Teardown: `ecctl call oss DeleteBucket --request '{"Bucket":"` + bucket + `"}' --region cn-hangzhou`, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", VolumeFixtureKind: "bucket", VolumeFixtureBucketCreationDate: "2026-11-04T12:00:00Z", VolumeFixturePending: tc.pending, VolumeFixtureDeleteProtocol: 1}},
			}
			if tc.attachment {
				t.Setenv("FAKE_VOLUME_FIXTURE_ATTACHMENT", "1")
				journal.Entries = append(journal.Entries, report.Resource{Scope: "stack", Teardown: "ecctl call ram DetachPolicyFromRole --region cn-hangzhou --request '{\"RoleName\":\"" + bucket + "-role\",\"PolicyName\":\"" + bucket + "-read\",\"PolicyType\":\"Custom\"}'", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", VolumeFixtureKind: "attachment", VolumeFixtureDeleteProtocol: 1})
			} else {
				t.Setenv("FAKE_VOLUME_FIXTURE_ATTACHMENT", "")
			}
			if tc.fullRAM {
				journal.Entries = append(journal.Entries,
					report.Resource{Scope: "stack", Teardown: `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role"}'`, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", VolumeFixtureKind: "role", VolumeFixtureRoleID: "role-created", VolumeFixtureDeleteProtocol: 1},
					report.Resource{Scope: "stack", Teardown: `ecctl call ram DeletePolicy --region cn-hangzhou --request '{"PolicyName":"` + bucket + `-read"}'`, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", VolumeFixtureKind: "policy", VolumeFixturePolicyCreateDate: "2026-11-04T12:00:00Z", VolumeFixtureDeleteProtocol: 1},
				)
				if !tc.skipAttachment {
					journal.Entries = append(journal.Entries, report.Resource{Scope: "stack", Teardown: `ecctl call ram DetachPolicyFromRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role","PolicyName":"` + bucket + `-read","PolicyType":"Custom"}'`, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", VolumeFixtureKind: "attachment", VolumeFixtureDeleteProtocol: 1})
				}
			}
			if tc.pendingKind != "" {
				found := false
				for i := range journal.Entries {
					if journal.Entries[i].VolumeFixtureKind == tc.pendingKind {
						journal.Entries[i].VolumeFixturePending = true
						found = true
					}
				}
				if !found {
					t.Fatalf("missing %s fixture in replay test", tc.pendingKind)
				}
			}
			if tc.pendingTeam {
				journal.VolumeTeamCreateIDs = map[string]string{"ecctl-t-0123456789abcdef01234567": ""}
			}
			body, err := json.Marshal(journal)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatal(err)
			}
			opt := ReplayOptions{Config: execpkg.Config{Bin: bin, Region: "cn-hangzhou", Env: []string{"HOME=" + dir}}, RunID: "run", Surface: "public"}
			grantSyntheticVolumeFixtureReplayScope(t, &opt, path, bucket)
			result, err := ReplayJournalWithOptions(context.Background(), path, opt)
			if err != nil {
				t.Fatal(err)
			}
			if result.Deleted != tc.wantDeleted || result.Errors != tc.wantErrors {
				t.Fatalf("replay result = %+v", result)
			}
			stored, _, err := readReplayJournal(path)
			wantEntries := 1 - tc.wantDeleted
			if tc.attachment {
				wantEntries++
			}
			if tc.fullRAM {
				wantEntries += 3
			}
			if tc.skipAttachment {
				wantEntries--
			}
			if err != nil || len(stored.Entries) != wantEntries {
				t.Fatalf("replay journal = %+v, err=%v", stored, err)
			}
			if tc.preflight != "" && tc.wantErrors != 0 {
				for _, retained := range stored.Entries {
					if retained.VolumeFixtureDeleteAttempted {
						t.Fatalf("unproven RAM absence spent a replay delete attempt: %+v", retained)
					}
				}
			}
			log, _ := os.ReadFile(calls)
			if tc.wantDeleted == 0 && tc.deleteCode == "" && (strings.Contains(string(log), "DeleteBucket") || strings.Contains(string(log), "DetachPolicyFromRole")) {
				t.Fatalf("unsafe bucket delete reached fake provider: %s", log)
			}
			if tc.wantDeleted == 0 && strings.Contains(string(log), "call ram ") {
				t.Fatalf("RAM dependency deleted before bucket confirmation: %s", log)
			}
			if tc.attachment && tc.wantDeleted == 1 && (strings.Index(string(log), "call oss DeleteBucket") < 0 || strings.Contains(string(log), "call ram DetachPolicyFromRole")) {
				t.Fatalf("attachment without durable parents reached provider: %s", log)
			}
			if tc.fullRAM && tc.wantDeleted == 4 && tc.preflight == "" && (strings.Index(string(log), "call oss DeleteBucket") < 0 || strings.Index(string(log), "call ram DetachPolicyFromRole") < strings.Index(string(log), "call oss DeleteBucket")) {
				t.Fatalf("RAM delete did not follow bucket confirmation: %s", log)
			}
			if tc.preflight != "" {
				wantPolicyDelete := tc.preflight == "role_absent"
				wantRoleDelete := tc.preflight == "policy_absent" || tc.preflight == "policy_only_absent"
				if strings.Contains(string(log), "call ram DetachPolicyFromRole") ||
					strings.Contains(string(log), "call ram DeletePolicy") != wantPolicyDelete ||
					strings.Contains(string(log), "call ram DeleteRole") != wantRoleDelete {
					t.Fatalf("RAM absence proof crossed mutation fence: %s", log)
				}
			}
			if tc.deleteCode == "FalsePolicy" && (strings.Contains(string(log), "call ram DeleteRole") || len(stored.Entries) != 2 || stored.Entries[0].VolumeFixtureKind != "role" && stored.Entries[1].VolumeFixtureKind != "role") {
				t.Fatalf("RAM Success=false consumed policy or role dependency: entries=%+v calls=%s", stored.Entries, log)
			}
		})
	}
}

func TestReplayVolumeFixtureResumesAfterDeleteBeforeJournalCommit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake provider uses a shell script")
	}
	dir := canonicalJournalTestDir(t)
	binding := quotaReplayBinding(t, dir)
	bucket := "ecctl-e2e-20261104t120000-123456abcdef"
	path := filepath.Join(dir, "cleanup.json")
	bin := filepath.Join(dir, "ecctl")
	calls := filepath.Join(dir, "calls")
	t.Setenv("FAKE_VOLUME_FIXTURE_CALLS", calls)
	t.Setenv("FAKE_VOLUME_FIXTURE_DELETED", filepath.Join(dir, "deleted"))
	t.Setenv("FAKE_VOLUME_FIXTURE_JOURNAL", path)
	fake := `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_VOLUME_FIXTURE_CALLS"
bucket_retired() {
  [ -f "$FAKE_VOLUME_FIXTURE_JOURNAL" ] || exit 92
  if grep -q 'DeleteBucket' "$FAKE_VOLUME_FIXTURE_JOURNAL"; then exit 91; fi
}
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}';;
  *"call oss ListObjects"*)
    if [ -f "$FAKE_VOLUME_FIXTURE_DELETED" ]; then
      echo '{"error":{"kind":"not_found","code":"NotFound","message":"ecctl-e2e-20261104t120000-123456abcdef not found"},"actions":[{"action_name":"ListObjects","code":"NoSuchBucket","provider_verified":true}]}'
      exit 4
    fi
    echo '{"product":"oss","operation":"ListObjects","response":{"Name":"ecctl-e2e-20261104t120000-123456abcdef","IsTruncated":false,"KeyCount":"0"}}';;
  *"call oss GetBucketInfo"*)
    echo '{"product":"oss","operation":"GetBucketInfo","region":"cn-hangzhou","response":{"Bucket":{"Name":"ecctl-e2e-20261104t120000-123456abcdef","Location":"oss-cn-hangzhou","ExtranetEndpoint":"oss-cn-hangzhou.aliyuncs.com","CreationDate":"2026-11-04T12:00:00Z","Owner":{"ID":"1234567890123456"}}}}';;
  *"call oss DeleteBucket"*)
    if [ -f "$FAKE_VOLUME_FIXTURE_DELETED" ]; then
      echo '{"error":{"kind":"not_found","code":"NotFound","message":"ecctl-e2e-20261104t120000-123456abcdef not found"},"actions":[{"action_name":"DeleteBucket","code":"NoSuchBucket"}]}'
      exit 4
    fi
    touch "$FAKE_VOLUME_FIXTURE_DELETED"
    echo '{"product":"oss","operation":"DeleteBucket","region":"cn-hangzhou","response":{}}';;
  *"call ram DetachPolicyFromRole"*) bucket_retired; echo '{"product":"ram","operation":"DetachPolicyFromRole","region":"cn-hangzhou","response":{"RequestId":"request-detach"}}';;
  *"call ram GetPolicy"*)
    if [ -f "$FAKE_VOLUME_FIXTURE_DELETED.policy" ]; then
      echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetPolicy","code":"EntityNotExist.Policy","provider_verified":true}]}'
      exit 4
    fi
    echo '{"product":"ram","operation":"GetPolicy","region":"cn-hangzhou","response":{"RequestId":"request-get-policy","Policy":{"PolicyName":"ecctl-e2e-20261104t120000-123456abcdef-read","PolicyType":"Custom","CreateDate":"2026-11-04T12:00:00Z"}}}';;
  *"call ram ListPoliciesForRole"*) echo '{"product":"ram","operation":"ListPoliciesForRole","region":"cn-hangzhou","response":{"RequestId":"request-list","Policies":{"Policy":[]}}}';;
  *"call ram DeletePolicy"*) bucket_retired; touch "$FAKE_VOLUME_FIXTURE_DELETED.policy"; echo '{"product":"ram","operation":"DeletePolicy","region":"cn-hangzhou","response":{"RequestId":"request-policy"}}';;
  *"call ram GetRole"*)
    if [ -f "$FAKE_VOLUME_FIXTURE_DELETED.role" ]; then
      echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetRole","code":"EntityNotExist.Role","provider_verified":true}]}'
      exit 4
    fi
    echo '{"product":"ram","operation":"GetRole","region":"cn-hangzhou","response":{"RequestId":"request-get-role","Role":{"RoleId":"role-created","RoleName":"ecctl-e2e-20261104t120000-123456abcdef-role","Arn":"acs:ram::1234567890123456:role/ecctl-e2e-20261104t120000-123456abcdef-role"}}}';;
  *"call ram DeleteRole"*) bucket_retired; touch "$FAKE_VOLUME_FIXTURE_DELETED.role"; echo '{"product":"ram","operation":"DeleteRole","region":"cn-hangzhou","response":{"RequestId":"request-role"}}';;
  *) exit 90;;
esac
`
	if err := os.WriteFile(bin, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	journal := report.CleanupJournal{
		Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: bin,
		VolumeFixtureBucket: bucket, VolumeFixtureBinding: binding,
		Entries: []report.Resource{
			{Scope: "stack", Teardown: `ecctl call oss DeleteBucket --request '{"Bucket":"` + bucket + `"}' --region cn-hangzhou`, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", VolumeFixtureKind: "bucket", VolumeFixtureBucketCreationDate: "2026-11-04T12:00:00Z", VolumeFixtureDeleteProtocol: 1},
			{Scope: "stack", Teardown: `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role"}'`, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", VolumeFixtureKind: "role", VolumeFixtureRoleID: "role-created", VolumeFixtureDeleteProtocol: 1},
			{Scope: "stack", Teardown: `ecctl call ram DeletePolicy --region cn-hangzhou --request '{"PolicyName":"` + bucket + `-read"}'`, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", VolumeFixtureKind: "policy", VolumeFixturePolicyCreateDate: "2026-11-04T12:00:00Z", VolumeFixtureDeleteProtocol: 1},
			{Scope: "stack", Teardown: `ecctl call ram DetachPolicyFromRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role","PolicyName":"` + bucket + `-read","PolicyType":"Custom"}'`, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", VolumeFixtureKind: "attachment", VolumeFixtureDeleteProtocol: 1},
		},
	}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	opt := ReplayOptions{Config: execpkg.Config{Bin: bin, Region: "cn-hangzhou", Env: []string{"HOME=" + dir}}, RunID: "run", Surface: "public"}
	grantSyntheticVolumeFixtureReplayScope(t, &opt, path, bucket)
	previousWrite := writeReplayConsumptionJournal
	defer func() { writeReplayConsumptionJournal = previousWrite }()
	writeReplayConsumptionJournal = func(string, []byte) error { return errors.New("simulated commit interruption") }
	first, err := ReplayJournalWithOptions(context.Background(), path, opt)
	if err == nil || first.Deleted != 1 {
		t.Fatalf("first replay did not stop after bucket deletion: result=%+v err=%v", first, err)
	}
	stored, _, err := readReplayJournal(path)
	if err != nil || len(stored.Entries) != 4 {
		t.Fatalf("interrupted journal lost bucket or RAM dependencies: journal=%+v err=%v", stored, err)
	}
	firstLog, err := os.ReadFile(calls)
	if err != nil || strings.Contains(string(firstLog), "call ram ") {
		t.Fatalf("RAM dependency delete ran before bucket journal commit: calls=%s err=%v", firstLog, err)
	}
	writeReplayConsumptionJournal = previousWrite
	second, err := ReplayJournalWithOptions(context.Background(), path, opt)
	if err != nil || second.Errors != 0 || second.Deleted != 4 {
		t.Fatalf("restart could not reconcile NoSuchBucket: result=%+v err=%v", second, err)
	}
	stored, _, err = readReplayJournal(path)
	if err != nil || len(stored.Entries) != 0 {
		t.Fatalf("restart retained consumed bucket: journal=%+v err=%v", stored, err)
	}
	log, err := os.ReadFile(calls)
	callLog := string(log)
	firstList, bucketDelete := strings.Index(callLog, "call oss ListObjects"), strings.Index(callLog, "call oss DeleteBucket")
	attachmentDelete, policyDelete, roleDelete := strings.Index(callLog, "call ram DetachPolicyFromRole"), strings.Index(callLog, "call ram DeletePolicy"), strings.Index(callLog, "call ram DeleteRole")
	listAfterDelete := -1
	if bucketDelete >= 0 {
		if next := strings.Index(callLog[bucketDelete+len("call oss DeleteBucket"):], "call oss ListObjects"); next >= 0 {
			listAfterDelete = bucketDelete + len("call oss DeleteBucket") + next
		}
	}
	if err != nil || firstList < 0 || firstList >= bucketDelete || listAfterDelete <= bucketDelete || listAfterDelete >= attachmentDelete ||
		attachmentDelete < 0 || attachmentDelete >= policyDelete || policyDelete >= roleDelete ||
		strings.Count(callLog, "call oss DeleteBucket") != 1 || strings.Count(callLog, "call ram DetachPolicyFromRole") != 1 ||
		strings.Count(callLog, "call ram DeletePolicy") != 1 || strings.Count(callLog, "call ram DeleteRole") != 1 {
		t.Fatalf("restart did not reconcile bucket before RAM cleanup: calls=%s err=%v", log, err)
	}
}

func TestReplayVolumeRoleNeverDeletesSameNameReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake provider uses a shell script")
	}
	for _, installed := range []bool{false, true} {
		t.Run(map[bool]string{false: "before consumption install", true: "after consumption install"}[installed], func(t *testing.T) {
			dir := canonicalJournalTestDir(t)
			binding := quotaReplayBinding(t, dir)
			bucket := "ecctl-e2e-20261104t120000-123456abcdef"
			path, bin := filepath.Join(dir, "cleanup.json"), filepath.Join(dir, "ecctl")
			present, calls := filepath.Join(dir, "replacement"), filepath.Join(dir, "calls")
			if err := os.WriteFile(present, []byte("replacement"), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("FAKE_ROLE_PRESENT", present)
			t.Setenv("FAKE_ROLE_CALLS", calls)
			fake := `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_ROLE_CALLS"
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}';;
  *"call oss ListObjects"*) echo '{"error":{"kind":"not_found","code":"NotFound","message":"ecctl-e2e-20261104t120000-123456abcdef not found"},"actions":[{"action_name":"ListObjects","code":"NoSuchBucket","provider_verified":true}]}'; exit 4;;
  *"call ram GetRole"*)
    if [ -f "$FAKE_ROLE_PRESENT" ]; then
      echo '{"product":"ram","operation":"GetRole","region":"cn-hangzhou","response":{"Role":{"RoleId":"ordinary-replacement"}}}'
    else
      echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetRole","code":"EntityNotExist.Role","provider_verified":true}]}'
      exit 4
    fi;;
  *"call ram DeleteRole"*) echo '{"product":"ram","operation":"DeleteRole","region":"cn-hangzhou","response":{"RequestId":"unsafe"}}';;
  *) exit 90;;
esac
`
			if err := os.WriteFile(bin, []byte(fake), 0o755); err != nil {
				t.Fatal(err)
			}
			entry := report.Resource{Scope: "stack", Teardown: `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role"}'`, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", VolumeFixtureKind: "role", VolumeFixtureDeleteAttempted: true}
			journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: bin, VolumeFixtureBucket: bucket, VolumeFixtureBinding: binding, Entries: []report.Resource{entry}}
			body, err := json.Marshal(journal)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatal(err)
			}
			opt := ReplayOptions{Config: execpkg.Config{Bin: bin, Region: "cn-hangzhou", Env: []string{"HOME=" + dir}}, RunID: "run", Surface: "public"}
			first, err := ReplayJournalWithOptions(context.Background(), path, opt)
			if err != nil || first.Errors != 1 || first.Deleted != 0 {
				t.Fatalf("replacement was consumed: %+v %v", first, err)
			}
			if err := os.Remove(present); err != nil {
				t.Fatal(err)
			}
			previous := writeReplayConsumptionJournal
			t.Cleanup(func() { writeReplayConsumptionJournal = previous })
			writeReplayConsumptionJournal = func(path string, body []byte) error {
				if installed {
					if err := previous(path, body); err != nil {
						return err
					}
				}
				return errors.New("injected role consumption failure")
			}
			second, err := ReplayJournalWithOptions(context.Background(), path, opt)
			if err == nil || second.Deleted != 1 {
				t.Fatalf("consumption failure was hidden: %+v %v", second, err)
			}
			writeReplayConsumptionJournal = previous
			third, err := ReplayJournalWithOptions(context.Background(), path, opt)
			if err != nil || third.Errors != 0 {
				t.Fatalf("exact absence did not settle role: %+v %v", third, err)
			}
			stored, _, err := readReplayJournal(path)
			if err != nil || len(stored.Entries) != 0 {
				t.Fatalf("role finalizer remained after absence: %+v %v", stored, err)
			}
			log, err := os.ReadFile(calls)
			if err != nil || strings.Contains(string(log), "DeleteRole") || !strings.Contains(string(log), "GetRole") {
				t.Fatalf("role replay issued destructive call: %s %v", log, err)
			}
		})
	}
}

func TestReplayFreshVolumeFixtureMarksBeforeFirstDeleteAndRetainsReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake provider uses a shell script")
	}
	for _, kind := range []string{"role", "bucket", "policy"} {
		for _, installed := range []bool{false, true} {
			name := kind + "/before consumption install"
			if installed {
				name = kind + "/after consumption install"
			}
			t.Run(name, func(t *testing.T) {
				dir := canonicalJournalTestDir(t)
				binding := quotaReplayBinding(t, dir)
				bucket := "ecctl-e2e-20261104t120000-123456abcdef"
				path, bin := filepath.Join(dir, "cleanup.json"), filepath.Join(dir, "ecctl")
				present, calls := filepath.Join(dir, "original-or-replacement"), filepath.Join(dir, "calls")
				if err := os.WriteFile(present, []byte("original"), 0o600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("FAKE_FIXTURE_KIND", kind)
				t.Setenv("FAKE_FIXTURE_PRESENT", present)
				t.Setenv("FAKE_FIXTURE_CALLS", calls)
				t.Setenv("FAKE_FIXTURE_JOURNAL", path)
				fake := `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_FIXTURE_CALLS"
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}';;
  *"call oss ListObjects"*)
    if [ "$FAKE_FIXTURE_KIND" != bucket ] || [ ! -f "$FAKE_FIXTURE_PRESENT" ]; then
      echo '{"error":{"kind":"not_found","code":"NotFound","message":"ecctl-e2e-20261104t120000-123456abcdef not found"},"actions":[{"action_name":"ListObjects","code":"NoSuchBucket","provider_verified":true}]}'
      exit 4
    fi
    echo '{"product":"oss","operation":"ListObjects","response":{"Name":"ecctl-e2e-20261104t120000-123456abcdef","IsTruncated":false,"KeyCount":"0"}}';;
  *"call oss GetBucketInfo"*)
    if [ -f "$FAKE_FIXTURE_PRESENT" ] && [ "$(cat "$FAKE_FIXTURE_PRESENT")" = original ]; then
      echo '{"product":"oss","operation":"GetBucketInfo","region":"cn-hangzhou","response":{"Bucket":{"Name":"ecctl-e2e-20261104t120000-123456abcdef","Location":"oss-cn-hangzhou","ExtranetEndpoint":"oss-cn-hangzhou.aliyuncs.com","CreationDate":"2026-11-04T12:00:00Z","Owner":{"ID":"1234567890123456"}}}}'
    else
      echo '{"product":"oss","operation":"GetBucketInfo","region":"cn-hangzhou","response":{"Bucket":{"Name":"ecctl-e2e-20261104t120000-123456abcdef","Location":"oss-cn-hangzhou","ExtranetEndpoint":"oss-cn-hangzhou.aliyuncs.com","CreationDate":"2026-11-05T12:00:00Z","Owner":{"ID":"1234567890123456"}}}}'
    fi;;
  *"call ram GetRole"*)
    if [ -f "$FAKE_FIXTURE_PRESENT" ] && [ "$(cat "$FAKE_FIXTURE_PRESENT")" = original ]; then
      echo '{"product":"ram","operation":"GetRole","region":"cn-hangzhou","response":{"RequestId":"request-get-role","Role":{"RoleId":"role-created","RoleName":"ecctl-e2e-20261104t120000-123456abcdef-role","Arn":"acs:ram::1234567890123456:role/ecctl-e2e-20261104t120000-123456abcdef-role"}}}'
    elif [ -f "$FAKE_FIXTURE_PRESENT" ]; then
      echo '{"product":"ram","operation":"GetRole","region":"cn-hangzhou","response":{"Role":{"RoleId":"replacement"}}}'
    else
      echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetRole","code":"EntityNotExist.Role","provider_verified":true}]}'
      exit 4
    fi;;
  *"call ram GetPolicy"*)
    if [ -f "$FAKE_FIXTURE_PRESENT" ] && [ "$(cat "$FAKE_FIXTURE_PRESENT")" = original ]; then
      echo '{"product":"ram","operation":"GetPolicy","region":"cn-hangzhou","response":{"RequestId":"request-get-policy","Policy":{"PolicyName":"ecctl-e2e-20261104t120000-123456abcdef-read","PolicyType":"Custom","CreateDate":"2026-11-04T12:00:00Z"}}}'
    elif [ -f "$FAKE_FIXTURE_PRESENT" ]; then
      echo '{"product":"ram","operation":"GetPolicy","region":"cn-hangzhou","response":{"Policy":{"PolicyName":"replacement"}}}'
    else
      echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetPolicy","code":"EntityNotExist.Policy","provider_verified":true}]}'
      exit 4
    fi;;
  *"call ram DeleteRole"*|*"call oss DeleteBucket"*|*"call ram DeletePolicy"*)
    if ! grep -q '"volume_fixture_delete_attempted": true' "$FAKE_FIXTURE_JOURNAL"; then exit 91; fi
    rm "$FAKE_FIXTURE_PRESENT"
    if [ "$FAKE_FIXTURE_KIND" = role ]; then
      echo '{"product":"ram","operation":"DeleteRole","region":"cn-hangzhou","response":{"RequestId":"first-role-delete"}}'
    elif [ "$FAKE_FIXTURE_KIND" = policy ]; then
      echo '{"product":"ram","operation":"DeletePolicy","region":"cn-hangzhou","response":{"RequestId":"first-policy-delete"}}'
    else
      echo '{"product":"oss","operation":"DeleteBucket","region":"cn-hangzhou","response":{}}'
    fi;;
  *) exit 90;;
esac
`
				if err := os.WriteFile(bin, []byte(fake), 0o755); err != nil {
					t.Fatal(err)
				}
				command := `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role"}'`
				if kind == "bucket" {
					command = `ecctl call oss DeleteBucket --request '{"Bucket":"` + bucket + `"}' --region cn-hangzhou`
				} else if kind == "policy" {
					command = `ecctl call ram DeletePolicy --region cn-hangzhou --request '{"PolicyName":"` + bucket + `-read"}'`
				}
				entry := report.Resource{Scope: "stack", Teardown: command, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", VolumeFixtureKind: kind, VolumeFixtureDeleteProtocol: 1}
				if kind == "role" {
					entry.VolumeFixtureRoleID = "role-created"
				} else if kind == "policy" {
					entry.VolumeFixturePolicyCreateDate = "2026-11-04T12:00:00Z"
				} else if kind == "bucket" {
					entry.VolumeFixtureBucketCreationDate = "2026-11-04T12:00:00Z"
				}
				journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: bin, VolumeFixtureBucket: bucket, VolumeFixtureBinding: binding, Entries: []report.Resource{entry}}
				body, err := json.Marshal(journal)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, body, 0o600); err != nil {
					t.Fatal(err)
				}
				opt := ReplayOptions{Config: execpkg.Config{Bin: bin, Region: "cn-hangzhou", Env: []string{"HOME=" + dir}}, RunID: "run", Surface: "public"}
				grantSyntheticVolumeFixtureReplayScope(t, &opt, path, bucket)
				legacy := journal
				legacy.Entries = []report.Resource{entry}
				legacy.Entries[0].VolumeFixtureDeleteProtocol = 0
				legacy.Entries[0].VolumeFixtureRoleID = ""
				legacyBody, err := json.Marshal(legacy)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, legacyBody, 0o600); err != nil {
					t.Fatal(err)
				}
				legacyResult, err := ReplayJournalWithOptions(context.Background(), path, opt)
				if err != nil || legacyResult == nil || legacyResult.Deleted != 0 || legacyResult.Errors != 1 {
					t.Fatalf("old unmarked name was deleted: %+v %v", legacyResult, err)
				}
				if err := os.WriteFile(path, body, 0o600); err != nil {
					t.Fatal(err)
				}
				previous := writeReplayConsumptionJournal
				t.Cleanup(func() { writeReplayConsumptionJournal = previous })
				writeReplayConsumptionJournal = func(path string, body []byte) error {
					if installed {
						if err := previous(path, body); err != nil {
							return err
						}
					}
					return errors.New("injected consumption durability failure")
				}
				first, err := ReplayJournalWithOptions(context.Background(), path, opt)
				if err == nil || first == nil || first.Deleted != 1 {
					t.Fatalf("first delete was not attempted exactly once: %+v %v", first, err)
				}
				writeReplayConsumptionJournal = previous
				stored, _, err := readReplayJournal(path)
				if err != nil || len(stored.Entries) != map[bool]int{false: 1, true: 0}[installed] || !installed && !stored.Entries[0].VolumeFixtureDeleteAttempted {
					t.Fatalf("first delete attempt was not retained correctly: %+v %v", stored, err)
				}
				if err := os.WriteFile(present, []byte("replacement"), 0o600); err != nil {
					t.Fatal(err)
				}
				second, err := ReplayJournalWithOptions(context.Background(), path, opt)
				if err != nil || second == nil || second.Deleted != 0 || second.Errors != map[bool]int{false: 1, true: 0}[installed] {
					t.Fatalf("replacement replay result: %+v %v", second, err)
				}
				log, err := os.ReadFile(calls)
				if err != nil || strings.Count(string(log), "DeleteRole")+strings.Count(string(log), "DeleteBucket")+strings.Count(string(log), "DeletePolicy") != 1 {
					t.Fatalf("same-name replacement was deleted: %s %v", log, err)
				}
				if !installed {
					if err := os.Remove(present); err != nil {
						t.Fatal(err)
					}
					third, err := ReplayJournalWithOptions(context.Background(), path, opt)
					if err != nil || third == nil || third.Deleted != 1 || third.Errors != 0 {
						t.Fatalf("exact absence did not retire finalizer: %+v %v", third, err)
					}
				}
			})
		}
	}
}

func TestReplayVolumeFixtureRestoresFreshMarkerAfterSubprocessNeverStarts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake provider uses a shell script")
	}
	for _, kind := range []string{"bucket", "role", "policy"} {
		t.Run(kind, func(t *testing.T) {
			dir := canonicalJournalTestDir(t)
			binding := quotaReplayBinding(t, dir)
			bucket := "ecctl-e2e-20261104t120000-123456abcdef"
			path, bin := filepath.Join(dir, "cleanup.json"), filepath.Join(dir, "ecctl")
			calls, disable := filepath.Join(dir, "calls"), filepath.Join(dir, "disable-next-delete")
			deleted := filepath.Join(dir, "fixture-deleted")
			t.Setenv("FAKE_FIXTURE_KIND", kind)
			t.Setenv("FAKE_FIXTURE_CALLS", calls)
			t.Setenv("FAKE_DISABLE_NEXT_DELETE", disable)
			t.Setenv("FAKE_FIXTURE_DELETED", deleted)
			t.Setenv("FAKE_FIXTURE_JOURNAL", path)
			if err := os.WriteFile(disable, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			fake := `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_FIXTURE_CALLS"
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}';;
  *"call oss ListObjects"*)
    if [ "$FAKE_FIXTURE_KIND" = bucket ]; then
      if [ -f "$FAKE_FIXTURE_DELETED" ]; then
        echo '{"error":{"kind":"not_found","code":"NotFound","message":"ecctl-e2e-20261104t120000-123456abcdef not found"},"actions":[{"action_name":"ListObjects","code":"NoSuchBucket","provider_verified":true}]}'
        exit 4
      fi
      echo '{"product":"oss","operation":"ListObjects","response":{"Name":"ecctl-e2e-20261104t120000-123456abcdef","IsTruncated":false,"KeyCount":"0"}}'
    else
      echo '{"error":{"kind":"not_found","code":"NotFound","message":"ecctl-e2e-20261104t120000-123456abcdef not found"},"actions":[{"action_name":"ListObjects","code":"NoSuchBucket","provider_verified":true}]}'
      exit 4
    fi;;
  *"call ram GetRole"*)
    if [ -f "$FAKE_FIXTURE_DELETED" ]; then
      echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetRole","code":"EntityNotExist.Role","provider_verified":true}]}'
      exit 4
    fi
    if [ -f "$FAKE_DISABLE_NEXT_DELETE" ]; then
      rm "$FAKE_DISABLE_NEXT_DELETE"
      chmod 0644 "$0"
    fi
    echo '{"product":"ram","operation":"GetRole","region":"cn-hangzhou","response":{"RequestId":"request-get-role","Role":{"RoleId":"role-created","RoleName":"ecctl-e2e-20261104t120000-123456abcdef-role","Arn":"acs:ram::1234567890123456:role/ecctl-e2e-20261104t120000-123456abcdef-role"}}}';;
  *"call ram GetPolicy"*)
    if [ -f "$FAKE_FIXTURE_DELETED" ]; then
      echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetPolicy","code":"EntityNotExist.Policy","provider_verified":true}]}'
      exit 4
    fi
    if [ -f "$FAKE_DISABLE_NEXT_DELETE" ]; then
      rm "$FAKE_DISABLE_NEXT_DELETE"
      chmod 0644 "$0"
    fi
    echo '{"product":"ram","operation":"GetPolicy","region":"cn-hangzhou","response":{"RequestId":"request-get-policy","Policy":{"PolicyName":"ecctl-e2e-20261104t120000-123456abcdef-read","PolicyType":"Custom","CreateDate":"2026-11-04T12:00:00Z"}}}';;
  *"call oss GetBucketInfo"*)
    if [ -f "$FAKE_DISABLE_NEXT_DELETE" ]; then
      rm "$FAKE_DISABLE_NEXT_DELETE"
      chmod 0644 "$0"
    fi
    echo '{"product":"oss","operation":"GetBucketInfo","region":"cn-hangzhou","response":{"Bucket":{"Name":"ecctl-e2e-20261104t120000-123456abcdef","Location":"oss-cn-hangzhou","ExtranetEndpoint":"oss-cn-hangzhou.aliyuncs.com","CreationDate":"2026-11-04T12:00:00Z","Owner":{"ID":"1234567890123456"}}}}';;
  *"call oss DeleteBucket"*)
    grep -q '"volume_fixture_delete_attempted": true' "$FAKE_FIXTURE_JOURNAL" || exit 91
    touch "$FAKE_FIXTURE_DELETED"
    echo '{"product":"oss","operation":"DeleteBucket","region":"cn-hangzhou","response":{}}';;
  *"call ram DeleteRole"*)
    grep -q '"volume_fixture_delete_attempted": true' "$FAKE_FIXTURE_JOURNAL" || exit 91
    touch "$FAKE_FIXTURE_DELETED"
    echo '{"product":"ram","operation":"DeleteRole","region":"cn-hangzhou","response":{"RequestId":"request-role"}}';;
  *"call ram DeletePolicy"*)
    grep -q '"volume_fixture_delete_attempted": true' "$FAKE_FIXTURE_JOURNAL" || exit 91
    touch "$FAKE_FIXTURE_DELETED"
    echo '{"product":"ram","operation":"DeletePolicy","region":"cn-hangzhou","response":{"RequestId":"request-policy"}}';;
  *) exit 90;;
esac
`
			if err := os.WriteFile(bin, []byte(fake), 0o755); err != nil {
				t.Fatal(err)
			}
			command := `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role"}'`
			if kind == "bucket" {
				command = `ecctl call oss DeleteBucket --request '{"Bucket":"` + bucket + `"}' --region cn-hangzhou`
			} else if kind == "policy" {
				command = `ecctl call ram DeletePolicy --region cn-hangzhou --request '{"PolicyName":"` + bucket + `-read"}'`
			}
			entry := report.Resource{Scope: "stack", Teardown: command, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", VolumeFixtureKind: kind, VolumeFixtureDeleteProtocol: 1}
			if kind == "role" {
				entry.VolumeFixtureRoleID = "role-created"
			} else if kind == "policy" {
				entry.VolumeFixturePolicyCreateDate = "2026-11-04T12:00:00Z"
			} else if kind == "bucket" {
				entry.VolumeFixtureBucketCreationDate = "2026-11-04T12:00:00Z"
			}
			journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: bin, VolumeFixtureBucket: bucket, VolumeFixtureBinding: binding, Entries: []report.Resource{entry}}
			body, err := json.Marshal(journal)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatal(err)
			}
			opt := ReplayOptions{Config: execpkg.Config{Bin: bin, Region: "cn-hangzhou", Env: []string{"HOME=" + dir}}, RunID: "run", Surface: "public"}
			grantSyntheticVolumeFixtureReplayScope(t, &opt, path, bucket)
			first, err := ReplayJournalWithOptions(context.Background(), path, opt)
			stored, _, readErr := readReplayJournal(path)
			if err != nil || first == nil || first.Deleted != 0 || first.Errors != 1 || readErr != nil || len(stored.Entries) != 1 || stored.Entries[0].VolumeFixtureDeleteAttempted {
				t.Fatalf("unstarted replay did not restore fresh marker: result=%+v journal=%+v err=%v read=%v", first, stored, err, readErr)
			}
			if _, err := os.Stat(deleted); !os.IsNotExist(err) {
				t.Fatalf("never-started deletion changed fake provider state: %v", err)
			}
			if err := os.Chmod(bin, 0o755); err != nil {
				t.Fatal(err)
			}
			second, err := ReplayJournalWithOptions(context.Background(), path, opt)
			stored, _, readErr = readReplayJournal(path)
			if err != nil || second == nil || second.Deleted != 1 || second.Errors != 0 || readErr != nil || len(stored.Entries) != 0 {
				t.Fatalf("unstarted replay was not eligible again: result=%+v journal=%+v err=%v read=%v", second, stored, err, readErr)
			}
			if _, err := os.Stat(deleted); err != nil {
				t.Fatalf("authorized deletion did not change fake provider state: %v", err)
			}
			log, err := os.ReadFile(calls)
			if err != nil || strings.Count(string(log), "DeleteBucket")+strings.Count(string(log), "DeleteRole")+strings.Count(string(log), "DeletePolicy") != 1 {
				t.Fatalf("unstarted replay launched an extra delete: %s %v", log, err)
			}
		})
	}
}

func TestReplayVolumeFixtureRestoreWriteFailureRetainsAttemptFence(t *testing.T) {
	dir := canonicalJournalTestDir(t)
	binding := quotaReplayBinding(t, dir)
	bucket := "ecctl-e2e-20261104t120000-123456abcdef"
	path, bin := filepath.Join(dir, "cleanup.json"), filepath.Join(dir, "ecctl")
	entry := report.Resource{Scope: "stack", Teardown: `ecctl call oss DeleteBucket --request '{"Bucket":"` + bucket + `"}' --region cn-hangzhou`, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", VolumeFixtureKind: "bucket", VolumeFixtureDeleteProtocol: 1, VolumeFixtureDeleteAttempted: true}
	journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: bin, VolumeFixtureBucket: bucket, VolumeFixtureBinding: binding, Entries: []report.Resource{entry}}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	parent, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	opt := ReplayOptions{Config: execpkg.Config{Bin: bin, Region: "cn-hangzhou", Env: []string{"HOME=" + dir}}, RunID: "run", Surface: "public"}
	previous := writeVolumeFixtureReplayRestoreJournal
	t.Cleanup(func() { writeVolumeFixtureReplayRestoreJournal = previous })
	writeVolumeFixtureReplayRestoreJournal = func(string, []byte) error { return errors.New("injected restore durability failure") }
	restored, err := restoreUnstartedVolumeFixtureReplayAttempt(context.Background(), path, opt, journal, entry, parent)
	stored, _, readErr := readReplayJournal(path)
	if err == nil || !restored.VolumeFixtureDeleteAttempted || readErr != nil || len(stored.Entries) != 1 || !stored.Entries[0].VolumeFixtureDeleteAttempted {
		t.Fatalf("restore write failure lost replay fence: entry=%+v journal=%+v err=%v read=%v", restored, stored, err, readErr)
	}
}

func TestReplayVolumeFixtureAttemptWriteErrorNeverLaunchesDelete(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake provider uses a shell script")
	}
	for _, kind := range []string{"role", "bucket", "policy"} {
		for _, installed := range []bool{false, true} {
			t.Run(kind+map[bool]string{false: "/before install", true: "/after install"}[installed], func(t *testing.T) {
				dir := canonicalJournalTestDir(t)
				binding := quotaReplayBinding(t, dir)
				bucket := "ecctl-e2e-20261104t120000-123456abcdef"
				path := filepath.Join(dir, "cleanup.json")
				command := `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role"}'`
				if kind == "bucket" {
					command = `ecctl call oss DeleteBucket --request '{"Bucket":"` + bucket + `"}' --region cn-hangzhou`
				} else if kind == "policy" {
					command = `ecctl call ram DeletePolicy --region cn-hangzhou --request '{"PolicyName":"` + bucket + `-read"}'`
				}
				entry := report.Resource{Scope: "stack", Teardown: command, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", VolumeFixtureKind: kind, VolumeFixtureDeleteProtocol: 1}
				if kind == "role" {
					entry.VolumeFixtureRoleID = "role-created"
				} else if kind == "policy" {
					entry.VolumeFixturePolicyCreateDate = "2026-11-04T12:00:00Z"
				} else if kind == "bucket" {
					entry.VolumeFixtureBucketCreationDate = "2026-11-04T12:00:00Z"
				}
				journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: filepath.Join(dir, "ecctl"), VolumeFixtureBucket: bucket, VolumeFixtureBinding: binding, Entries: []report.Resource{entry}}
				body, err := json.Marshal(journal)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, body, 0o600); err != nil {
					t.Fatal(err)
				}
				calls := filepath.Join(dir, "calls")
				t.Setenv("FAKE_FIXTURE_KIND", kind)
				t.Setenv("FAKE_FIXTURE_CALLS", calls)
				fake := `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_FIXTURE_CALLS"
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}';;
  *"call oss ListObjects"*)
    if [ "$FAKE_FIXTURE_KIND" != bucket ]; then
      echo '{"error":{"kind":"not_found","code":"NotFound","message":"ecctl-e2e-20261104t120000-123456abcdef not found"},"actions":[{"action_name":"ListObjects","code":"NoSuchBucket","provider_verified":true}]}'
      exit 4
    fi
    echo '{"product":"oss","operation":"ListObjects","response":{"Name":"ecctl-e2e-20261104t120000-123456abcdef","IsTruncated":false,"KeyCount":"0"}}';;
  *"call ram GetRole"*) echo '{"product":"ram","operation":"GetRole","region":"cn-hangzhou","response":{"RequestId":"request-get-role","Role":{"RoleId":"role-created","RoleName":"ecctl-e2e-20261104t120000-123456abcdef-role","Arn":"acs:ram::1234567890123456:role/ecctl-e2e-20261104t120000-123456abcdef-role"}}}';;
  *"call ram GetPolicy"*) echo '{"product":"ram","operation":"GetPolicy","region":"cn-hangzhou","response":{"RequestId":"request-get-policy","Policy":{"PolicyName":"ecctl-e2e-20261104t120000-123456abcdef-read","PolicyType":"Custom","CreateDate":"2026-11-04T12:00:00Z"}}}';;
  *"call oss GetBucketInfo"*) echo '{"product":"oss","operation":"GetBucketInfo","region":"cn-hangzhou","response":{"Bucket":{"Name":"ecctl-e2e-20261104t120000-123456abcdef","Location":"oss-cn-hangzhou","ExtranetEndpoint":"oss-cn-hangzhou.aliyuncs.com","CreationDate":"2026-11-04T12:00:00Z","Owner":{"ID":"1234567890123456"}}}}';;
  *"call ram DeleteRole"*|*"call oss DeleteBucket"*|*"call ram DeletePolicy"*) echo '{"product":"unexpected-delete"}';;
  *) exit 90;;
esac
`
				if err := os.WriteFile(journal.EcctlBin, []byte(fake), 0o755); err != nil {
					t.Fatal(err)
				}
				opt := ReplayOptions{Config: execpkg.Config{Bin: journal.EcctlBin, Region: journal.Region, Env: []string{"HOME=" + dir}}, RunID: journal.RunID, Surface: journal.Surface}
				grantSyntheticVolumeFixtureReplayScope(t, &opt, path, bucket)
				previous := writeVolumeFixtureReplayAttemptJournal
				t.Cleanup(func() { writeVolumeFixtureReplayAttemptJournal = previous })
				writeVolumeFixtureReplayAttemptJournal = func(path string, body []byte) error {
					if installed {
						if err := previous(path, body); err != nil {
							return err
						}
					}
					return errors.New("injected attempt durability failure")
				}
				result, err := ReplayJournalWithOptions(context.Background(), path, opt)
				if err == nil || result == nil || result.Deleted != 0 {
					t.Fatal("attempt write failure was reported durable")
				}
				stored, _, err := readReplayJournal(path)
				if err != nil || len(stored.Entries) != 1 || stored.Entries[0].VolumeFixtureDeleteAttempted {
					t.Fatalf("uninvoked fixture delete marker was not restored: %+v %v", stored, err)
				}
				log, err := os.ReadFile(calls)
				if err != nil || strings.Contains(string(log), "DeleteRole") || strings.Contains(string(log), "DeleteBucket") || strings.Contains(string(log), "DeletePolicy") {
					t.Fatalf("attempt write error launched a destructive call: %s %v", log, err)
				}
			})
		}
	}
}
