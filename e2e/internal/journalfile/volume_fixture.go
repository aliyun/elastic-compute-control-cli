package journalfile

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/google/shlex"
)

var volumeFixtureBucketPattern = regexp.MustCompile(`^ecctl-e2e-[0-9]{8}t[0-9]{6}-[a-f0-9]{12}$`)

var ErrVolumeFixtureAttachmentPresent = errors.New("Volume fixture original policy attachment is still present or was recreated")

// Replay sets ExpectedBinSHA256 only after checking an explicit archive against
// retained original-run evidence and the recorded capability surface. Runtime
// reads keep the old journal selector untouched for on-disk comparisons.
func volumeFixtureRuntimeBinaryMatches(cfg execpkg.Config, journal report.CleanupJournal) bool {
	if cfg.Bin == journal.EcctlBin {
		return true
	}
	if !filepath.IsAbs(cfg.Bin) || cfg.ExpectedBinSHA256 == "" || journal.EcctlBinSHA256 != "" && journal.EcctlBinSHA256 != cfg.ExpectedBinSHA256 {
		return false
	}
	digest, err := ExecutableSHA256(cfg.Bin)
	return err == nil && digest == cfg.ExpectedBinSHA256
}

// ValidateVolumeFixtureIdentity requires a principal for which a duplicate
// PutBucket fails instead of modifying an existing bucket's metadata.
func ValidateVolumeFixtureIdentity(binding *report.QuotaCredentialBinding) error {
	if binding == nil {
		return fmt.Errorf("Volume fixture credential binding is missing")
	}
	if err := binding.Identity.Validate(); err != nil {
		return err
	}
	if binding.Identity.Type != "RAMUser" && binding.Identity.Type != "AssumedRoleUser" {
		return fmt.Errorf("Volume fixtures require RAMUser or AssumedRoleUser identity; Account PutBucket can modify an existing bucket")
	}
	return nil
}

// VolumeFixtureNameDeleteKind identifies finalizers whose target name may be
// reused after deletion. Attachment mutates a relationship and needs a
// separate absence proof; this delete protocol does not cover it.
func VolumeFixtureNameDeleteKind(kind string) bool {
	return kind == "bucket" || kind == "role" || kind == "policy"
}

// The attachment also mutates a reusable name, but needs relationship absence
// rather than the resource absence checks used by the three delete kinds.
func VolumeFixtureFencedKind(kind string) bool {
	return VolumeFixtureNameDeleteKind(kind) || kind == "attachment"
}

// VolumeFixtureMutationAllowed binds one fixture mutation to the original
// run's durable reservation and credential. Callers still need a fresh
// provider identity check before a name-based delete.
func VolumeFixtureMutationAllowed(phase string, journal report.CleanupJournal, entry report.Resource, cfg execpkg.Config) bool {
	if journal.Version != 2 || journal.JournalPath == "" || journal.RunID == "" || journal.ExecutionID == "" ||
		journal.RegionRole != "primary" || journal.Region != "cn-hangzhou" ||
		!volumeFixtureBucketPattern.MatchString(journal.VolumeFixtureBucket) ||
		!VolumeFixtureFencedKind(entry.VolumeFixtureKind) ||
		ValidateVolumeFixtureIdentity(journal.VolumeFixtureBinding) != nil ||
		ValidateVolumeFixtureTeardown(entry, journal) != nil ||
		!volumeFixtureRuntimeBinaryMatches(cfg, journal) || cfg.Region != journal.Region ||
		cfg.Profile != journal.VolumeFixtureBinding.Profile || cfg.ExpectedIdentity == nil ||
		*cfg.ExpectedIdentity != journal.VolumeFixtureBinding.Identity {
		return false
	}
	count := 0
	for _, candidate := range journal.Entries {
		if candidate == entry {
			count++
		}
	}
	if count != 1 {
		return false
	}
	switch phase {
	case "create":
		return entry.VolumeFixturePending && entry.VolumeFixtureDispatchProtocol == 1 &&
			entry.VolumeFixtureDeleteProtocol == 1 &&
			!entry.VolumeFixtureDispatched && !entry.VolumeFixtureCreateValidated &&
			entry.VolumeFixtureCreateRequestID == "" && !entry.VolumeFixtureDeleteAttempted
	case "delete":
		if entry.VolumeFixturePending || entry.VolumeFixtureDeleteProtocol != 1 || entry.VolumeFixtureDeleteAttempted ||
			!entry.VolumeFixtureCreateValidated || entry.VolumeFixtureCreateRequestID == "" {
			return false
		}
		switch entry.VolumeFixtureKind {
		case "bucket":
			return true
		case "role":
			return true
		case "policy":
			return true
		case "attachment":
			return true
		}
	}
	return false
}

func ValidVolumeFixtureCreationDate(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || !strings.HasSuffix(value, "Z") {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, value)
	return err == nil
}

// Only a typed, uncontradicted provider rejection for the exact reserved
// target may retire a launched pending RAM fixture without a finalizer.
func VolumeFixtureDefinitiveCreateRejection(kind, bucket string, result execpkg.Result) bool {
	operation := map[string]string{"role": "CreateRole", "policy": "CreatePolicy", "attachment": "AttachPolicyToRole"}[kind]
	if operation == "" || !volumeFixtureBucketPattern.MatchString(bucket) || result.Err != nil || !result.Started || result.Exit != 2 {
		return false
	}
	root, ok := result.JSON.(map[string]any)
	if !ok {
		return false
	}
	failure, ok := root["error"].(map[string]any)
	if !ok || failure["kind"] != "service" || failure["code"] != "CloudAPIError" || failure["retryable"] != false {
		return false
	}
	for _, key := range []string{"response", "product", "operation", "policy", "role", "attachment", "target", "resource", "recovery_command"} {
		if _, present := root[key]; present {
			return false
		}
	}
	actions, ok := root["actions"].([]any)
	if !ok || len(actions) != 1 {
		return false
	}
	action, ok := actions[0].(map[string]any)
	if !ok || action["action_name"] != operation || action["provider_verified"] != true || action["code"] != "Forbidden" && action["code"] != "NoPermission" && action["code"] != "InvalidParameter" {
		return false
	}
	requestID, ok := action["request_id"].(string)
	if !ok || !validVolumeProvenanceID(requestID) {
		return false
	}
	if kind == "policy" {
		_, hasTarget := action["target"]
		return !hasTarget
	}
	target, ok := action["target"].(map[string]any)
	if !ok {
		return false
	}
	want := map[string]any{"RoleName": bucket + "-role"}
	if kind == "attachment" {
		want["PolicyName"] = bucket + "-read"
		want["PolicyType"] = "Custom"
	}
	return reflect.DeepEqual(target, want)
}

// ValidateVolumeFixtureOSSCommand uses only the structured request grammar
// accepted by the pinned-profile executor and the OSS caller.
func ValidateVolumeFixtureOSSCommand(operation, command, bucket string) error {
	if !volumeFixtureBucketPattern.MatchString(bucket) {
		return fmt.Errorf("Volume fixture bucket is invalid")
	}
	request := `{"Bucket":"` + bucket + `"}`
	switch operation {
	case "PutBucket":
		request = `{"Bucket":"` + bucket + `","ACL":"private"}`
	case "ListObjects":
		request = `{"Bucket":"` + bucket + `","MaxKeys":1}`
	case "GetBucketInfo", "DeleteBucket":
	default:
		return fmt.Errorf("Volume fixture OSS operation is invalid")
	}
	parts, err := shlex.Split(command)
	want := []string{"ecctl", "call", "oss", operation, "--request", request, "--region", "cn-hangzhou"}
	if err != nil || !reflect.DeepEqual(parts, want) && !reflect.DeepEqual(parts, append(want, "--output=json")) {
		return fmt.Errorf("Volume fixture OSS command changed")
	}
	return nil
}

// ValidateVolumeFixtureCreate keeps fixture IDs from authorizing a different
// cloud mutation. Only the private bucket and its narrow FC role are allowed.
func ValidateVolumeFixtureCreate(kind, command string, journal report.CleanupJournal) error {
	if err := ValidateVolumeFixtureIdentity(journal.VolumeFixtureBinding); err != nil {
		return err
	}
	bucket := journal.VolumeFixtureBucket
	if !volumeFixtureBucketPattern.MatchString(bucket) || journal.Region != "cn-hangzhou" {
		return fmt.Errorf("Volume fixture create target is invalid")
	}
	parts, err := shlex.Split(command)
	if err != nil {
		return fmt.Errorf("Volume fixture create grammar is invalid")
	}
	if kind == "bucket" {
		return ValidateVolumeFixtureOSSCommand("PutBucket", command, bucket)
	}
	if (len(parts) != 8 && (len(parts) != 9 || parts[8] != "--output=json")) || parts[0] != "ecctl" || parts[1] != "call" || parts[2] != "ram" || parts[4] != "--region" || parts[5] != "cn-hangzhou" || parts[6] != "--request" {
		return fmt.Errorf("Volume fixture RAM create grammar is invalid")
	}
	wantOperation := map[string]string{"role": "CreateRole", "policy": "CreatePolicy", "attachment": "AttachPolicyToRole"}[kind]
	if wantOperation == "" || parts[3] != wantOperation {
		return fmt.Errorf("Volume fixture RAM create operation changed")
	}
	var request map[string]string
	if unmarshalRecoveryJSON([]byte(parts[7]), &request) != nil {
		return fmt.Errorf("Volume fixture RAM create request is invalid")
	}
	switch kind {
	case "attachment":
		want := map[string]string{"RoleName": bucket + "-role", "PolicyName": bucket + "-read", "PolicyType": "Custom"}
		if !reflect.DeepEqual(request, want) {
			return fmt.Errorf("Volume fixture RAM attachment changed")
		}
	case "role", "policy":
		field, document := "AssumeRolePolicyDocument", `{"Version":"1","Statement":[{"Effect":"Allow","Principal":{"Service":["fc.aliyuncs.com"]},"Action":"sts:AssumeRole"}]}`
		nameField, name := "RoleName", bucket+"-role"
		if kind == "policy" {
			field = "PolicyDocument"
			document = fmt.Sprintf(`{"Version":"1","Statement":[{"Effect":"Allow","Action":["oss:ListObjects"],"Resource":["acs:oss:*:*:%s"]},{"Effect":"Allow","Action":["oss:GetObject"],"Resource":["acs:oss:*:*:%s/*"]}]}`, bucket, bucket)
			nameField, name = "PolicyName", bucket+"-read"
		}
		if len(request) != 3 || request[nameField] != name || request["Description"] != "ecctl E2E Volume "+bucket {
			return fmt.Errorf("Volume fixture RAM create target changed")
		}
		var actual, expected any
		if unmarshalRecoveryJSON([]byte(request[field]), &actual) != nil || unmarshalRecoveryJSON([]byte(document), &expected) != nil || !reflect.DeepEqual(actual, expected) {
			return fmt.Errorf("Volume fixture RAM permission document changed")
		}
	}
	return nil
}

// ValidateVolumeFixtureTeardown admits only the exact run-owned dependency
// targets. Raw RAM calls are deliberately not admitted by journalcmd generally.
func ValidateVolumeFixtureTeardown(entry report.Resource, journal report.CleanupJournal) error {
	if err := ValidateVolumeFixtureIdentity(journal.VolumeFixtureBinding); err != nil {
		return err
	}
	if entry.VolumeFixtureKind == "" || !volumeFixtureBucketPattern.MatchString(journal.VolumeFixtureBucket) || journal.RunID == "" || journal.ExecutionID == "" || entry.Scope != "stack" || entry.ExecutionID != journal.ExecutionID || entry.RegionRole != journal.RegionRole || entry.RegionRole != "primary" || entry.Region != journal.Region || entry.Region != "cn-hangzhou" {
		return fmt.Errorf("Volume fixture ownership or region is incomplete")
	}
	fenced := VolumeFixtureFencedKind(entry.VolumeFixtureKind)
	if entry.VolumeFixtureDeleteProtocol != 0 && (entry.VolumeFixtureDeleteProtocol != 1 || !fenced) {
		return fmt.Errorf("Volume fixture delete protocol is invalid")
	}
	if entry.VolumeFixtureDeleteAttempted && !fenced {
		return fmt.Errorf("Volume fixture delete attempt belongs only to a fenced finalizer")
	}
	if entry.VolumeFixtureRoleID != "" && (entry.VolumeFixtureKind != "role" || strings.TrimSpace(entry.VolumeFixtureRoleID) != entry.VolumeFixtureRoleID) {
		return fmt.Errorf("Volume fixture role creation ID is invalid")
	}
	if entry.VolumeFixturePolicyCreateDate != "" && (entry.VolumeFixtureKind != "policy" || !ValidVolumeFixtureCreationDate(entry.VolumeFixturePolicyCreateDate)) {
		return fmt.Errorf("Volume fixture policy creation date is invalid")
	}
	if entry.VolumeFixtureBucketCreationDate != "" && (entry.VolumeFixtureKind != "bucket" || !ValidVolumeFixtureCreationDate(entry.VolumeFixtureBucketCreationDate)) {
		return fmt.Errorf("Volume fixture bucket creation date is invalid")
	}
	if (entry.VolumeFixtureCreateRequestID == "") != (entry.VolumeFixtureCreateResponseSHA256 == "") {
		return fmt.Errorf("Volume fixture create receipt is incomplete")
	}
	if entry.VolumeFixtureCreateRequestID != "" {
		if len(entry.VolumeFixtureCreateRequestID) > 128 || strings.IndexFunc(entry.VolumeFixtureCreateRequestID, func(r rune) bool { return r < 33 || r > 126 }) >= 0 || len(entry.VolumeFixtureCreateResponseSHA256) != 64 {
			return fmt.Errorf("Volume fixture create receipt is invalid")
		}
		if _, err := hex.DecodeString(entry.VolumeFixtureCreateResponseSHA256); err != nil || strings.ToLower(entry.VolumeFixtureCreateResponseSHA256) != entry.VolumeFixtureCreateResponseSHA256 {
			return fmt.Errorf("Volume fixture create response digest is invalid")
		}
	}
	bucket := journal.VolumeFixtureBucket
	parts, err := shlex.Split(entry.Teardown)
	if err != nil || (len(parts) != 8 && (len(parts) != 9 || parts[8] != "--output=json")) || parts[0] != "ecctl" || parts[1] != "call" {
		return fmt.Errorf("Volume fixture teardown grammar is invalid")
	}
	if entry.VolumeFixtureKind == "bucket" {
		return ValidateVolumeFixtureOSSCommand("DeleteBucket", entry.Teardown, bucket)
	}
	if parts[2] != "ram" || parts[4] != "--region" || parts[5] != "cn-hangzhou" || parts[6] != "--request" {
		return fmt.Errorf("Volume fixture RAM teardown grammar is invalid")
	}
	wantOperation := map[string]string{"attachment": "DetachPolicyFromRole", "policy": "DeletePolicy", "role": "DeleteRole"}[entry.VolumeFixtureKind]
	if wantOperation == "" || parts[3] != wantOperation {
		return fmt.Errorf("Volume fixture RAM operation changed")
	}
	var request map[string]string
	if err := unmarshalRecoveryJSON([]byte(parts[7]), &request); err != nil {
		return fmt.Errorf("Volume fixture RAM request is invalid")
	}
	want := map[string]string{}
	switch entry.VolumeFixtureKind {
	case "attachment":
		want = map[string]string{"RoleName": bucket + "-role", "PolicyName": bucket + "-read", "PolicyType": "Custom"}
	case "policy":
		want = map[string]string{"PolicyName": bucket + "-read"}
	case "role":
		want = map[string]string{"RoleName": bucket + "-role"}
	}
	if !reflect.DeepEqual(request, want) {
		return fmt.Errorf("Volume fixture RAM target changed")
	}
	return nil
}

// VolumeFixtureRAMParents returns only this run's settled original endpoints.
// A lookup of the current same-name resources cannot fill missing evidence.
func VolumeFixtureRAMParents(journal report.CleanupJournal) (report.Resource, report.Resource, error) {
	var role, policy report.Resource
	roleSeen, policySeen := false, false
	for _, entry := range journal.Entries {
		if entry.VolumeFixtureKind != "role" && entry.VolumeFixtureKind != "policy" {
			continue
		}
		if err := ValidateVolumeFixtureTeardown(entry, journal); err != nil {
			return role, policy, err
		}
		if entry.Scope != "stack" || entry.ExecutionID != journal.ExecutionID || entry.VolumeFixturePending || entry.VolumeFixtureDeleteAttempted || entry.VolumeFixtureDeleteProtocol != 1 {
			return role, policy, fmt.Errorf("Volume fixture original RAM parent is unavailable")
		}
		if entry.VolumeFixtureKind == "role" {
			if roleSeen {
				return role, policy, fmt.Errorf("Volume fixture original role identity is unavailable")
			}
			role, roleSeen = entry, true
		} else {
			if policySeen {
				return role, policy, fmt.Errorf("Volume fixture original policy identity is unavailable")
			}
			policy, policySeen = entry, true
		}
	}
	if !roleSeen || !policySeen {
		return role, policy, fmt.Errorf("Volume fixture original RAM parents are missing")
	}
	return role, policy, nil
}

// VolumeFixtureRoleMatches reads the named role under the pinned test account.
func VolumeFixtureRoleMatches(ctx context.Context, cfg execpkg.Config, entry report.Resource, journal report.CleanupJournal, run func(context.Context, execpkg.Config, string) execpkg.Result) (execpkg.Result, error) {
	if err := ValidateVolumeFixtureTeardown(entry, journal); err != nil {
		return execpkg.Result{}, err
	}
	if entry.VolumeFixtureKind != "role" || entry.VolumeFixturePending || entry.VolumeFixtureDeleteProtocol != 1 || entry.VolumeFixtureDeleteAttempted ||
		!volumeFixtureRuntimeBinaryMatches(cfg, journal) || cfg.Region != journal.Region || cfg.Profile != journal.VolumeFixtureBinding.Profile || cfg.ExpectedIdentity == nil || *cfg.ExpectedIdentity != journal.VolumeFixtureBinding.Identity {
		return execpkg.Result{}, fmt.Errorf("Volume fixture role creation or read binding is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return execpkg.Result{}, err
	}
	name := journal.VolumeFixtureBucket + "-role"
	command := "ecctl call ram GetRole --region cn-hangzhou --request '{\"RoleName\":\"" + name + "\"}' --output=json"
	result := run(ctx, cfg, command)
	root, ok := result.JSON.(map[string]any)
	if result.Err != nil || !result.Started || result.Exit != 0 || !ok || root["product"] != "ram" || root["operation"] != "GetRole" || root["region"] != journal.Region || root["error"] != nil || root["actions"] != nil {
		return result, fmt.Errorf("Volume fixture current role identity is unproven")
	}
	response, ok := root["response"].(map[string]any)
	if !ok || response["Code"] != nil || response["code"] != nil || response["Error"] != nil || response["error"] != nil || response["Success"] != nil || response["success"] != nil {
		return result, fmt.Errorf("Volume fixture current role identity is unproven")
	}
	requestID, _ := response["RequestId"].(string)
	role, ok := response["Role"].(map[string]any)
	if !ok || strings.TrimSpace(requestID) == "" || role["RoleName"] != name {
		return result, fmt.Errorf("Volume fixture current role name changed")
	}
	arn, _ := role["Arn"].(string)
	if !strings.EqualFold(arn, "acs:ram::"+journal.VolumeFixtureBinding.Identity.AccountID+":role/"+name) {
		return result, fmt.Errorf("Volume fixture current role ARN does not match its trusted account")
	}
	return result, nil
}

func volumeFixtureReadResponse(result execpkg.Result, product, operation, region string) (map[string]any, error) {
	root, ok := result.JSON.(map[string]any)
	if result.Err != nil || !result.Started || result.Exit != 0 || !ok || root["product"] != product || root["operation"] != operation || root["region"] != region || root["error"] != nil || root["actions"] != nil {
		return nil, fmt.Errorf("Volume fixture %s response is unproven", operation)
	}
	response, ok := root["response"].(map[string]any)
	if !ok || response["Code"] != nil || response["code"] != nil || response["Error"] != nil || response["error"] != nil || response["Success"] != nil || response["success"] != nil {
		return nil, fmt.Errorf("Volume fixture %s response is unproven", operation)
	}
	return response, nil
}

// VolumeFixturePolicyMatches reads the named policy under the pinned test account.
func VolumeFixturePolicyMatches(ctx context.Context, cfg execpkg.Config, entry report.Resource, journal report.CleanupJournal, run func(context.Context, execpkg.Config, string) execpkg.Result) (execpkg.Result, error) {
	if err := ValidateVolumeFixtureTeardown(entry, journal); err != nil {
		return execpkg.Result{}, err
	}
	if entry.VolumeFixtureKind != "policy" || entry.VolumeFixturePending || entry.VolumeFixtureDeleteProtocol != 1 || entry.VolumeFixtureDeleteAttempted ||
		!volumeFixtureRuntimeBinaryMatches(cfg, journal) || cfg.Region != journal.Region || cfg.Profile != journal.VolumeFixtureBinding.Profile || cfg.ExpectedIdentity == nil || *cfg.ExpectedIdentity != journal.VolumeFixtureBinding.Identity {
		return execpkg.Result{}, fmt.Errorf("Volume fixture policy creation or read binding is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return execpkg.Result{}, err
	}
	name := journal.VolumeFixtureBucket + "-read"
	command := "ecctl call ram GetPolicy --region cn-hangzhou --request '{\"PolicyName\":\"" + name + "\",\"PolicyType\":\"Custom\"}' --output=json"
	result := run(ctx, cfg, command)
	response, err := volumeFixtureReadResponse(result, "ram", "GetPolicy", journal.Region)
	if err != nil {
		return result, err
	}
	requestID, _ := response["RequestId"].(string)
	policy, ok := response["Policy"].(map[string]any)
	if !ok || strings.TrimSpace(requestID) == "" || policy["PolicyName"] != name || policy["PolicyType"] != "Custom" {
		return result, fmt.Errorf("Volume fixture current policy name changed")
	}
	return result, nil
}

// VolumeFixtureBucketMatches reads the named bucket under the pinned test account.
func VolumeFixtureBucketMatches(ctx context.Context, cfg execpkg.Config, entry report.Resource, journal report.CleanupJournal, run func(context.Context, execpkg.Config, string) execpkg.Result) (execpkg.Result, error) {
	if err := ValidateVolumeFixtureTeardown(entry, journal); err != nil {
		return execpkg.Result{}, err
	}
	if entry.VolumeFixtureKind != "bucket" || entry.VolumeFixturePending || entry.VolumeFixtureDeleteProtocol != 1 || entry.VolumeFixtureDeleteAttempted ||
		!volumeFixtureRuntimeBinaryMatches(cfg, journal) || cfg.Region != journal.Region || cfg.Profile != journal.VolumeFixtureBinding.Profile || cfg.ExpectedIdentity == nil || *cfg.ExpectedIdentity != journal.VolumeFixtureBinding.Identity {
		return execpkg.Result{}, fmt.Errorf("Volume fixture bucket creation or read binding is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return execpkg.Result{}, err
	}
	command := "ecctl call oss GetBucketInfo --request '{\"Bucket\":\"" + journal.VolumeFixtureBucket + "\"}' --region cn-hangzhou --output=json"
	result := run(ctx, cfg, command)
	response, err := volumeFixtureReadResponse(result, "oss", "GetBucketInfo", journal.Region)
	if err != nil {
		return result, err
	}
	bucket, ok := response["Bucket"].(map[string]any)
	if !ok || bucket["Name"] != journal.VolumeFixtureBucket || bucket["Location"] != "oss-cn-hangzhou" || bucket["ExtranetEndpoint"] != "oss-cn-hangzhou.aliyuncs.com" {
		return result, fmt.Errorf("Volume fixture current bucket name changed")
	}
	owner, ok := bucket["Owner"].(map[string]any)
	if !ok || owner["ID"] != journal.VolumeFixtureBinding.Identity.AccountID {
		return result, fmt.Errorf("Volume fixture current bucket owner does not match the trusted account")
	}
	return result, nil
}

// VolumeFixtureAttachmentAbsent performs only reads before or after a detach.
// After an attempted detach, a current same-name attachment is never detached
// again, even when the first response or journal consumption was lost.
func VolumeFixtureAttachmentAbsent(ctx context.Context, cfg execpkg.Config, entry report.Resource, journal report.CleanupJournal, run func(context.Context, execpkg.Config, string) execpkg.Result) (execpkg.Result, error) {
	if err := ValidateVolumeFixtureTeardown(entry, journal); err != nil {
		return execpkg.Result{}, err
	}
	if entry.VolumeFixtureKind != "attachment" || entry.VolumeFixturePending {
		return execpkg.Result{}, fmt.Errorf("Volume fixture attachment is unavailable for absence proof")
	}
	role, policy, err := VolumeFixtureRAMParents(journal)
	if err != nil {
		return execpkg.Result{}, err
	}
	roleResult, roleErr := VolumeFixtureRoleMatches(ctx, cfg, role, journal, run)
	if roleErr != nil {
		if _, absentErr := VolumeFixtureRoleAbsent(ctx, cfg, role, journal, func(context.Context, execpkg.Config, string) execpkg.Result { return roleResult }); absentErr == nil {
			return roleResult, nil
		}
		return roleResult, fmt.Errorf("Volume fixture original attachment role is unproven: %w", roleErr)
	}
	policyResult, policyErr := VolumeFixturePolicyMatches(ctx, cfg, policy, journal, run)
	if policyErr != nil {
		if _, absentErr := VolumeFixturePolicyAbsent(ctx, cfg, policy, journal, func(context.Context, execpkg.Config, string) execpkg.Result { return policyResult }); absentErr == nil {
			return policyResult, nil
		}
		return policyResult, fmt.Errorf("Volume fixture original attachment policy is unproven: %w", policyErr)
	}
	command := "ecctl call ram ListPoliciesForRole --region cn-hangzhou --request '{\"RoleName\":\"" + journal.VolumeFixtureBucket + "-role\"}' --output=json"
	result := run(ctx, cfg, command)
	response, err := volumeFixtureReadResponse(result, "ram", "ListPoliciesForRole", journal.Region)
	if err != nil {
		return result, err
	}
	requestID, _ := response["RequestId"].(string)
	if strings.TrimSpace(requestID) == "" {
		return result, fmt.Errorf("Volume fixture attachment list completeness is unproven")
	}
	var items []any
	switch policies := response["Policies"].(type) {
	case []any:
		items = policies // Pinned RAM schema publishes Policies as an array.
	case map[string]any:
		// Retained raw RAM envelopes may keep the Policy wrapper.
		var ok bool
		items, ok = policies["Policy"].([]any)
		if !ok || len(policies) != 1 {
			return result, fmt.Errorf("Volume fixture attachment list is unproven")
		}
	default:
		return result, fmt.Errorf("Volume fixture attachment list is unproven")
	}
	if token, present := response["NextToken"]; present {
		value, ok := token.(string)
		if !ok || value != "" {
			return result, fmt.Errorf("Volume fixture attachment list has another page")
		}
	}
	if truncated, present := response["IsTruncated"]; present && truncated != false {
		return result, fmt.Errorf("Volume fixture attachment list is truncated")
	}
	if rawTotal, present := response["TotalCount"]; present {
		total, ok := volumeFixtureWholeCount(rawTotal)
		if !ok || total != len(items) {
			return result, fmt.Errorf("Volume fixture attachment list completeness is unproven")
		}
	}
	if page, present := response["PageNumber"]; present {
		value, ok := volumeFixtureWholeCount(page)
		if !ok || value != 1 {
			return result, fmt.Errorf("Volume fixture attachment list is not the first page")
		}
	}
	if rawSize, present := response["PageSize"]; present {
		size, ok := volumeFixtureWholeCount(rawSize)
		if !ok || size < len(items) || response["TotalCount"] == nil {
			return result, fmt.Errorf("Volume fixture attachment list page size is unproven")
		}
	}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		name, nameOK := item["PolicyName"].(string)
		kind, kindOK := item["PolicyType"].(string)
		if !ok || !nameOK || !kindOK || name == "" || kind == "" {
			return result, fmt.Errorf("Volume fixture attachment list item is unproven")
		}
		if name == journal.VolumeFixtureBucket+"-read" && kind == "Custom" {
			return result, ErrVolumeFixtureAttachmentPresent
		}
	}
	return result, nil
}

func volumeFixtureWholeCount(value any) (int, bool) {
	switch number := value.(type) {
	case json.Number:
		return volumeFixtureJSONWholeCount(number)
	case int:
		return number, number >= 0
	case int64:
		if number >= 0 && number <= math.MaxInt {
			return int(number), true
		}
	case float64:
		if number >= 0 && number <= float64(math.MaxInt) && math.Trunc(number) == number {
			return int(number), true
		}
	}
	return 0, false
}

func volumeFixtureJSONWholeCount(number json.Number) (int, bool) {
	value := number.String()
	if !json.Valid([]byte(value)) {
		return 0, false
	}
	negative := strings.HasPrefix(value, "-")
	value = strings.TrimPrefix(value, "-")
	exponent := 0
	if index := strings.IndexAny(value, "eE"); index >= 0 {
		parsed, err := strconv.Atoi(value[index+1:])
		if err != nil {
			return 0, false
		}
		exponent, value = parsed, value[:index]
	}
	fraction := 0
	if index := strings.IndexByte(value, '.'); index >= 0 {
		fraction = len(value) - index - 1
		value = value[:index] + value[index+1:]
	}
	digits := strings.TrimLeft(value, "0")
	if digits == "" {
		return 0, true
	}
	if negative || exponent < -len(digits) || exponent > len(strconv.Itoa(math.MaxInt))+fraction {
		return 0, false
	}
	scale := exponent - fraction
	if scale < 0 {
		places := -scale
		if places > len(digits) || strings.Trim(digits[len(digits)-places:], "0") != "" {
			return 0, false
		}
		digits = digits[:len(digits)-places]
	} else if scale > 0 {
		if len(digits)+scale > len(strconv.Itoa(math.MaxInt)) {
			return 0, false
		}
		digits += strings.Repeat("0", scale)
	}
	count, err := strconv.Atoi(digits)
	if err != nil || count < 0 {
		return 0, false
	}
	return count, true
}

// VolumeFixtureRoleAbsent is the only role replay proof after a name-based
// DeleteRole may have run. A present same-name role is never deleted again.
func VolumeFixtureRoleAbsent(ctx context.Context, cfg execpkg.Config, entry report.Resource, journal report.CleanupJournal, run func(context.Context, execpkg.Config, string) execpkg.Result) (execpkg.Result, error) {
	if err := ValidateVolumeFixtureTeardown(entry, journal); err != nil {
		return execpkg.Result{}, err
	}
	if entry.VolumeFixtureKind != "role" || !volumeFixtureRuntimeBinaryMatches(cfg, journal) || cfg.Region != journal.Region || cfg.Profile != journal.VolumeFixtureBinding.Profile || cfg.ExpectedIdentity == nil || *cfg.ExpectedIdentity != journal.VolumeFixtureBinding.Identity {
		return execpkg.Result{}, fmt.Errorf("Volume fixture role read binding changed")
	}
	if err := ctx.Err(); err != nil {
		return execpkg.Result{}, err
	}
	command := "ecctl call ram GetRole --region cn-hangzhou --request '{\"RoleName\":\"" + journal.VolumeFixtureBucket + "-role\"}' --output=json"
	result := run(ctx, cfg, command)
	root, ok := result.JSON.(map[string]any)
	if result.Err != nil || !result.Started || result.Exit != 4 || !ok || root["product"] != nil || root["operation"] != nil || root["response"] != nil {
		return result, fmt.Errorf("Volume fixture role absence is unproven")
	}
	failure, ok := root["error"].(map[string]any)
	if !ok || failure["kind"] != "not_found" || failure["code"] != "NotFound" {
		return result, fmt.Errorf("Volume fixture role absence is unproven")
	}
	actions, ok := root["actions"].([]any)
	if !ok || len(actions) != 1 {
		return result, fmt.Errorf("Volume fixture role absence is unproven")
	}
	action, ok := actions[0].(map[string]any)
	if !ok || action["provider_verified"] != true || action["action_name"] != "GetRole" || action["code"] != "EntityNotExist.Role" {
		return result, fmt.Errorf("Volume fixture role absence is unproven")
	}
	return result, nil
}

// A possible DeletePolicy attempt makes the policy name ambiguous. Only the
// exact GetPolicy absence for this Custom policy can retire its finalizer.
func VolumeFixturePolicyAbsent(ctx context.Context, cfg execpkg.Config, entry report.Resource, journal report.CleanupJournal, run func(context.Context, execpkg.Config, string) execpkg.Result) (execpkg.Result, error) {
	if err := ValidateVolumeFixtureTeardown(entry, journal); err != nil {
		return execpkg.Result{}, err
	}
	if entry.VolumeFixtureKind != "policy" || !volumeFixtureRuntimeBinaryMatches(cfg, journal) || cfg.Region != journal.Region || cfg.Profile != journal.VolumeFixtureBinding.Profile || cfg.ExpectedIdentity == nil || *cfg.ExpectedIdentity != journal.VolumeFixtureBinding.Identity {
		return execpkg.Result{}, fmt.Errorf("Volume fixture policy read binding changed")
	}
	if err := ctx.Err(); err != nil {
		return execpkg.Result{}, err
	}
	command := "ecctl call ram GetPolicy --region cn-hangzhou --request '{\"PolicyName\":\"" + journal.VolumeFixtureBucket + "-read\",\"PolicyType\":\"Custom\"}' --output=json"
	result := run(ctx, cfg, command)
	root, ok := result.JSON.(map[string]any)
	if result.Err != nil || !result.Started || result.Exit != 4 || !ok || root["product"] != nil || root["operation"] != nil || root["response"] != nil {
		return result, fmt.Errorf("Volume fixture policy absence is unproven")
	}
	failure, ok := root["error"].(map[string]any)
	if !ok || failure["kind"] != "not_found" || failure["code"] != "NotFound" {
		return result, fmt.Errorf("Volume fixture policy absence is unproven")
	}
	actions, ok := root["actions"].([]any)
	if !ok || len(actions) != 1 {
		return result, fmt.Errorf("Volume fixture policy absence is unproven")
	}
	action, ok := actions[0].(map[string]any)
	if !ok || action["provider_verified"] != true || action["action_name"] != "GetPolicy" || action["code"] != "EntityNotExist.Policy" {
		return result, fmt.Errorf("Volume fixture policy absence is unproven")
	}
	return result, nil
}

// A bucket name can be reused after DeleteBucket. Only the exact ListObjects
// NoSuchBucket result allows an attempted or legacy finalizer to be consumed.
func VolumeFixtureBucketAbsent(ctx context.Context, cfg execpkg.Config, entry report.Resource, journal report.CleanupJournal, run func(context.Context, execpkg.Config, string) execpkg.Result) (execpkg.Result, error) {
	if err := ValidateVolumeFixtureTeardown(entry, journal); err != nil {
		return execpkg.Result{}, err
	}
	if entry.VolumeFixtureKind != "bucket" || !volumeFixtureRuntimeBinaryMatches(cfg, journal) || cfg.Region != journal.Region || cfg.Profile != journal.VolumeFixtureBinding.Profile || cfg.ExpectedIdentity == nil || *cfg.ExpectedIdentity != journal.VolumeFixtureBinding.Identity {
		return execpkg.Result{}, fmt.Errorf("Volume fixture bucket read binding changed")
	}
	if err := ctx.Err(); err != nil {
		return execpkg.Result{}, err
	}
	command := "ecctl call oss ListObjects --request '{\"Bucket\":\"" + journal.VolumeFixtureBucket + "\",\"MaxKeys\":1}' --region cn-hangzhou --output=json"
	result := run(ctx, cfg, command)
	if !volumeFixtureBucketAbsenceResult(result, journal.VolumeFixtureBucket) {
		return result, fmt.Errorf("Volume fixture bucket absence is unproven")
	}
	return result, nil
}

// A delete response establishes at most request acceptance. Only the matching
// read-only absence probe may retire a name-bound fixture finalizer.
func VolumeFixtureTerminalAbsent(ctx context.Context, cfg execpkg.Config, entry report.Resource, journal report.CleanupJournal, run func(context.Context, execpkg.Config, string) execpkg.Result) (execpkg.Result, error) {
	switch entry.VolumeFixtureKind {
	case "bucket":
		return VolumeFixtureBucketAbsent(ctx, cfg, entry, journal, run)
	case "role":
		return VolumeFixtureRoleAbsent(ctx, cfg, entry, journal, run)
	case "policy":
		return VolumeFixturePolicyAbsent(ctx, cfg, entry, journal, run)
	default:
		return execpkg.Result{}, fmt.Errorf("Volume fixture has no name-bound absence probe")
	}
}

// CheckVolumeFixtureParents runs under the journal transaction lock before a
// child reservation. A missing or uncertain ancestor cannot support a create.
func CheckVolumeFixtureParents(kind string, journal report.CleanupJournal) error {
	order := map[string]int{"bucket": 0, "role": 1, "policy": 2, "attachment": 3}
	position, ok := order[kind]
	if !ok {
		return fmt.Errorf("unknown Volume fixture dependency")
	}
	seen := map[string]bool{}
	for _, entry := range journal.Entries {
		if entry.VolumeFixtureKind == "" {
			continue
		}
		if err := ValidateVolumeFixtureTeardown(entry, journal); err != nil {
			return err
		}
		if seen[entry.VolumeFixtureKind] || entry.ExecutionID != journal.ExecutionID {
			return fmt.Errorf("Volume fixture ancestor ownership is ambiguous")
		}
		seen[entry.VolumeFixtureKind] = true
		if order[entry.VolumeFixtureKind] >= position || entry.VolumeFixturePending {
			return fmt.Errorf("Volume fixture ancestor is unavailable")
		}
	}
	for dependency, index := range order {
		if index < position && !seen[dependency] {
			return fmt.Errorf("Volume fixture ancestor %s is missing", dependency)
		}
	}
	return nil
}

// VolumeFixtureCleanupReady requires protected consumers to be absent before
// deleting the bucket, and confirmed bucket removal before RAM cleanup.
func VolumeFixtureCleanupReady(path string, journal report.CleanupJournal, entry report.Resource, env []string) error {
	if err := ValidateVolumeFixtureTeardown(entry, journal); err != nil {
		return err
	}
	if entry.VolumeFixturePending {
		return fmt.Errorf("Volume fixture creation outcome is uncertain")
	}
	for _, teamID := range journal.VolumeTeamCreateIDs {
		if teamID == "" {
			return fmt.Errorf("Volume Team creation outcome is uncertain")
		}
	}
	if err := CheckTemplateCreateIntents(path, journal, env); err != nil {
		return err
	}
	if outstanding, err := OutstandingVolumeChild(path, journal, env); err != nil {
		return err
	} else if outstanding {
		return fmt.Errorf("owned Volume absence is unproven before dependency cleanup")
	}
	index := -1
	for i, candidate := range journal.Entries {
		if candidate == entry {
			if index >= 0 {
				return fmt.Errorf("Volume fixture cleanup entry is ambiguous")
			}
			index = i
		}
	}
	if index < 0 {
		return fmt.Errorf("Volume fixture cleanup entry is missing")
	}
	for _, candidate := range journal.Entries {
		if candidate.VolumeFixturePending {
			return fmt.Errorf("Volume fixture creation outcome is uncertain")
		}
		if candidate.VolumeTeamBound {
			if err := VolumeTeamChildrenReady(path, journal, candidate); err != nil {
				return fmt.Errorf("Volume Team creator child cleanup is still pending: %w", err)
			}
		}
		if entry.VolumeFixtureKind != "bucket" && candidate.VolumeFixtureKind == "bucket" {
			return fmt.Errorf("Volume fixture bucket deletion is not durably confirmed")
		}
	}
	if entry.VolumeFixtureKind != "bucket" {
		for _, candidate := range journal.Entries[index+1:] {
			if candidate.VolumeFixtureKind != "" {
				return fmt.Errorf("Volume fixture child cleanup is still pending")
			}
		}
	}
	return nil
}

type VolumeFixtureBucketPresence uint8

const (
	VolumeFixtureBucketUnknown VolumeFixtureBucketPresence = iota
	VolumeFixtureBucketVerifiedAbsent
	VolumeFixtureBucketPresentEmpty
)

// VolumeFixtureBucketEmpty distinguishes verified absence from present-empty.
// DeleteBucket must still confirm absence of other storage, including parts.
func VolumeFixtureBucketEmpty(ctx context.Context, cfg execpkg.Config, bucket string, run func(context.Context, execpkg.Config, string) execpkg.Result) (VolumeFixtureBucketPresence, error) {
	if !volumeFixtureBucketPattern.MatchString(bucket) || cfg.Region != "cn-hangzhou" || cfg.ExpectedIdentity == nil {
		return VolumeFixtureBucketUnknown, fmt.Errorf("Volume fixture bucket, region or credential binding changed")
	}
	command := "ecctl call oss ListObjects --request '{\"Bucket\":\"" + bucket + "\",\"MaxKeys\":1}' --region cn-hangzhou --output=json"
	if err := ValidateVolumeFixtureOSSCommand("ListObjects", command, bucket); err != nil {
		return VolumeFixtureBucketUnknown, err
	}
	result := run(ctx, cfg, command)
	if volumeFixtureBucketAbsenceResult(result, bucket) {
		return VolumeFixtureBucketVerifiedAbsent, nil
	}
	root, _ := result.JSON.(map[string]any)
	response, _ := root["response"].(map[string]any)
	if result.Err != nil || !result.Started || result.Exit != 0 || root["product"] != "oss" || root["operation"] != "ListObjects" || response == nil || response["Name"] != bucket || !strings.EqualFold(fmt.Sprint(response["IsTruncated"]), "false") {
		return VolumeFixtureBucketUnknown, fmt.Errorf("Volume fixture bucket emptiness is unproven")
	}
	count := response["KeyCount"]
	if count == nil {
		return VolumeFixtureBucketUnknown, fmt.Errorf("Volume fixture bucket emptiness is unproven")
	}
	if strings.TrimSpace(fmt.Sprint(count)) != "0" {
		return VolumeFixtureBucketUnknown, fmt.Errorf("Volume fixture bucket contains unexpected objects")
	}
	if contents := response["Contents"]; contents != nil {
		switch value := contents.(type) {
		case []any:
			if len(value) != 0 {
				return VolumeFixtureBucketUnknown, fmt.Errorf("Volume fixture bucket contains unexpected objects")
			}
		default:
			return VolumeFixtureBucketUnknown, fmt.Errorf("Volume fixture bucket contains unexpected objects")
		}
	}
	return VolumeFixtureBucketPresentEmpty, nil
}

func volumeFixtureBucketAbsenceResult(result execpkg.Result, bucket string) bool {
	root, ok := result.JSON.(map[string]any)
	if result.Err != nil || !result.Started || result.Exit != 4 || !ok || root["product"] != nil || root["operation"] != nil || root["response"] != nil {
		return false
	}
	failure, ok := root["error"].(map[string]any)
	if !ok || failure["kind"] != "not_found" || failure["code"] != "NotFound" || !strings.HasPrefix(fmt.Sprint(failure["message"]), bucket+" ") {
		return false
	}
	actions, ok := root["actions"].([]any)
	if !ok || len(actions) != 1 {
		return false
	}
	action, ok := actions[0].(map[string]any)
	return ok && action["provider_verified"] == true && action["action_name"] == "ListObjects" && action["code"] == "NoSuchBucket"
}

// VolumeFixtureDeleteConfirmed classifies only the exact resource teardown.
// Attachments require a read-only relationship proof after their one attempt.
// The CLI's generic exit 4 also covers unrelated provider failures.
func VolumeFixtureDeleteConfirmed(entry report.Resource, journal report.CleanupJournal, cfg execpkg.Config, result execpkg.Result) error {
	if err := ValidateVolumeFixtureTeardown(entry, journal); err != nil {
		return err
	}
	if !volumeFixtureRuntimeBinaryMatches(cfg, journal) || cfg.Region != journal.Region || cfg.Profile != journal.VolumeFixtureBinding.Profile || cfg.ExpectedIdentity == nil || *cfg.ExpectedIdentity != journal.VolumeFixtureBinding.Identity {
		return fmt.Errorf("Volume fixture delete credential or execution binding changed")
	}
	if entry.VolumeFixtureKind == "attachment" {
		return fmt.Errorf("Volume fixture attachment needs a read-only relationship proof")
	}
	root, ok := result.JSON.(map[string]any)
	if result.Err != nil || !result.Started || !ok {
		return fmt.Errorf("Volume fixture delete outcome is unproven")
	}
	product, operation := "ram", map[string]string{"attachment": "DetachPolicyFromRole", "policy": "DeletePolicy", "role": "DeleteRole"}[entry.VolumeFixtureKind]
	if entry.VolumeFixtureKind == "bucket" {
		product, operation = "oss", "DeleteBucket"
	}
	if result.Exit == 0 {
		response, responseOK := root["response"].(map[string]any)
		if root["product"] == product && root["operation"] == operation && root["region"] == journal.Region && responseOK && root["error"] == nil && root["actions"] == nil && response["Code"] == nil && response["code"] == nil && response["Error"] == nil && response["error"] == nil {
			if product == "ram" {
				requestID, _ := response["RequestId"].(string)
				if strings.TrimSpace(requestID) == "" {
					return fmt.Errorf("Volume fixture RAM delete lacks a trusted success response")
				}
				for _, key := range []string{"Success", "success"} {
					if value, present := response[key]; present && value != true {
						return fmt.Errorf("Volume fixture RAM delete reports failed success")
					}
				}
			}
			return nil
		}
	}
	if result.Exit == 4 && root["product"] == nil && root["operation"] == nil && root["response"] == nil {
		failure, _ := root["error"].(map[string]any)
		actions, _ := root["actions"].([]any)
		if failure["kind"] == "not_found" && failure["code"] == "NotFound" && len(actions) == 1 {
			action, _ := actions[0].(map[string]any)
			if action["provider_verified"] == true && action["action_name"] == operation {
				code, _ := action["code"].(string)
				switch entry.VolumeFixtureKind {
				case "bucket":
					if code == "NoSuchBucket" && strings.HasPrefix(fmt.Sprint(failure["message"]), journal.VolumeFixtureBucket+" ") {
						return nil
					}
				case "role":
					if code == "EntityNotExist.Role" {
						return nil
					}
				case "policy":
					if code == "EntityNotExist.Policy" {
						return nil
					}
				}
			}
		}
	}
	return fmt.Errorf("Volume fixture delete outcome is unproven")
}
