package runner

import (
	"strings"
	"testing"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

func TestOwnedVolumeBackingRejectsUnownedDependencies(t *testing.T) {
	bucket := "ecctl-e2e-20261104t120000-123456abcdef"
	binding := &report.QuotaCredentialBinding{Profile: "owner", Identity: execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}}
	journal := report.CleanupJournal{Version: 2, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", VolumeFixtureBucket: bucket, VolumeFixtureBinding: binding}
	entry := func(kind, command string) report.Resource {
		return report.Resource{Scope: "stack", ExecutionID: journal.ExecutionID, RegionRole: "primary", Region: journal.Region, VolumeFixtureKind: kind, VolumeFixtureDeleteProtocol: 1, Teardown: command}
	}
	bucketEntry := entry("bucket", `ecctl call oss DeleteBucket --request '{"Bucket":"`+bucket+`"}' --region cn-hangzhou`)
	bucketEntry.VolumeFixtureBucketCreationDate = "2026-11-04T12:00:00Z"
	roleEntry := entry("role", `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"`+bucket+`-role"}'`)
	roleEntry.VolumeFixtureRoleID = "role-created"
	policyEntry := entry("policy", `ecctl call ram DeletePolicy --region cn-hangzhou --request '{"PolicyName":"`+bucket+`-read"}'`)
	policyEntry.VolumeFixturePolicyCreateDate = "2026-11-04T12:00:00Z"
	attachmentEntry := entry("attachment", `ecctl call ram DetachPolicyFromRole --region cn-hangzhou --request '{"RoleName":"`+bucket+`-role","PolicyName":"`+bucket+`-read","PolicyType":"Custom"}'`)
	journal.Entries = []report.Resource{bucketEntry, roleEntry, policyEntry, attachmentEntry}
	oss := `{"bucketName":"` + bucket + `","bucketPath":"/","endpoint":"https://oss-cn-hangzhou.aliyuncs.com","readOnly":true}`
	mount := `{"role":"acs:ram::1234567890123456:role/` + bucket + `-role"}`
	if err := validateOwnedVolumeBacking(journal, oss, "", "", mount); err != nil {
		t.Fatalf("settled owned backing rejected: %v", err)
	}
	for _, tc := range []struct {
		name, oss, fs, agentic, mount string
		change                        func(*report.CleanupJournal)
	}{
		{name: "other bucket", oss: strings.Replace(oss, bucket, "existing-business-bucket", 1), mount: mount},
		{name: "writable", oss: strings.Replace(oss, `"readOnly":true`, `"readOnly":false`, 1), mount: mount},
		{name: "other endpoint", oss: strings.Replace(oss, "oss-cn-hangzhou", "oss-cn-beijing", 1), mount: mount},
		{name: "file input", oss: "@/tmp/oss.json", mount: mount},
		{name: "agentic backend", oss: oss, fs: `{}`, mount: mount},
		{name: "unowned network", oss: oss, mount: strings.TrimSuffix(mount, "}") + `,"vpcConfig":{"vpcId":"existing"}}`},
		{name: "other role", oss: oss, mount: strings.Replace(mount, bucket+"-role", "business-role", 1)},
		{name: "missing attachment", oss: oss, mount: mount, change: func(j *report.CleanupJournal) { j.Entries = j.Entries[:3] }},
		{name: "pending bucket", oss: oss, mount: mount, change: func(j *report.CleanupJournal) { j.Entries[0].VolumeFixturePending = true }},
		{name: "attempted role cleanup", oss: oss, mount: mount, change: func(j *report.CleanupJournal) { j.Entries[1].VolumeFixtureDeleteAttempted = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := journal
			j.Entries = append([]report.Resource(nil), journal.Entries...)
			if tc.change != nil {
				tc.change(&j)
			}
			if err := validateOwnedVolumeBacking(j, tc.oss, tc.fs, tc.agentic, tc.mount); err == nil {
				t.Fatal("unowned backing was accepted")
			}
		})
	}
}
