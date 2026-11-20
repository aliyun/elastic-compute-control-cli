package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
)

func markVolumeProvider(t *testing.T, cfg execpkg.Config, dir string) string {
	t.Helper()
	body, err := os.ReadFile(cfg.Bin)
	if err != nil {
		t.Fatal(err)
	}
	script := strings.Replace(string(body), "#!/usr/bin/env bash\n", `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$FAKE_NATIVE_DIR/provider-calls"
if [[ "$*" == *"volume --region"* && "$*" == *" create "* && "$FAKE_VOLUME_MODE" == read-transient ]]; then
 echo 'connection reset by peer' >&2; exit 1
fi
`, 1)
	if script == string(body) {
		t.Fatal("fake provider insertion point missing")
	}
	if err := os.WriteFile(cfg.Bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "provider-calls")
}

func TestVolumeRawWritesRejectedBeforeCaseProvider(t *testing.T) {
	for _, command := range []string{
		"ecctl call fcsandbox CreateVolume --request '{}'",
		"ecctl --region cn-hangzhou call FCSandbox CreateVolume --request '{}'",
		"ecctl call sbx UpdateVolume --request '{}'",
		"ecctl call fc-sandbox DeleteVolume --request '{}'",
		"ecctl call --request '{}' -- FCSandbox CreateVolume",
		"ecctl call --api-param name=owned -- FCSandbox CreateVolume",
		"ecctl call --request '{}' -- FCSandbox UpdateVolume",
		"ecctl call --api-param teamID=team-other -- FCSandbox UpdateVolume",
		`ecctl call --request '{"volumeID":"vol-other"}' -- FCSandbox DeleteVolume`,
		"ecctl call --api-param volumeID=vol-other -- FCSandbox DeleteVolume",
		"ecctl call --schema FCSandbox CreateVolume --schema=false --request '{}'",
		"ecctl call --list FCSandbox UpdateVolume --list=false --request '{}'",
		"ecctl call --generate-request FCSandbox DeleteVolume --generate-request=false --request '{}'",
	} {
		t.Run(command, func(t *testing.T) {
			cfg, cl, dir, scope := volumeRecoveryFixture(t)
			marker := markVolumeProvider(t, cfg, dir)
			step := scenario.Step{Name: "raw Volume mutation", Run: command}
			result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second)
			if ok || result.Error == "" {
				t.Errorf("raw Volume write was accepted: %+v", result)
			}
			calls, _ := os.ReadFile(marker)
			if strings.Contains(string(calls), "CreateVolume") || strings.Contains(string(calls), "UpdateVolume") || strings.Contains(string(calls), "DeleteVolume") {
				t.Errorf("raw Volume write reached fake provider: %s", calls)
			}
		})
	}
}

func TestVolumeSeparatedRawWritesRejectedBeforeRunSetup(t *testing.T) {
	for _, command := range []string{
		"ecctl call --request '{}' -- FCSandbox CreateVolume",
		"ecctl call --api-param teamID=team-other -- FCSandbox UpdateVolume",
		`ecctl call --request '{"volumeID":"vol-other"}' -- FCSandbox DeleteVolume`,
		"ecctl call --schema FCSandbox CreateVolume --schema=false --request '{}'",
		"ecctl call --list FCSandbox UpdateVolume --list=false --request '{}'",
		"ecctl call --generate-request FCSandbox DeleteVolume --generate-request=false --request '{}'",
	} {
		t.Run(command, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "must-not-launch")
			suite := &scenario.Suite{Resource: "sandbox/volume", Steps: []scenario.Step{{Name: "raw write", Run: command}}}
			run, err := Run(context.Background(), Options{Suites: []*scenario.Suite{suite}, Region: "cn-hangzhou", Surface: "public", EcctlBin: bin})
			if err == nil || run == nil || len(run.Cases) != 1 || run.Cases[0].Resource != "setup" || !strings.Contains(run.Cases[0].Error, "raw FCSandbox Volume mutation") {
				t.Fatalf("raw Volume write passed setup admission: run=%+v err=%v", run, err)
			}
		})
	}
}

func TestVolumeWriteInOtherSuiteRejectedBeforeFixtureSetup(t *testing.T) {
	for _, resource := range []string{"sandbox/team", "sandbox/template-cache", "ecs/instance"} {
		suite := &scenario.Suite{Resource: resource, Steps: []scenario.Step{{Name: "write", Run: "ecctl sandbox volume create --name owned --team-id team-1 --output=json"}}}
		setupCalled := false
		run, err := Run(context.Background(), Options{Suites: []*scenario.Suite{suite}, Region: "cn-hangzhou", Surface: "public", EcctlBin: filepath.Join(t.TempDir(), "must-not-launch"), testFixtureSetup: func(*cleanup, map[string]any) error { setupCalled = true; return nil }})
		if err == nil || run == nil || len(run.Cases) != 1 || run.Cases[0].Resource != "setup" || !strings.Contains(run.Cases[0].Error, "Volume mutation requires") || setupCalled {
			t.Fatalf("%s Volume write reached fixture setup: run=%+v err=%v", resource, run, err)
		}
	}
}

func TestVolumeRunRejectsOrdinaryTeamCreatorBeforeFixtureSetup(t *testing.T) {
	suites := []*scenario.Suite{
		{Resource: "sandbox/team", Steps: []scenario.Step{{Name: "parent", Run: "ecctl sandbox team create --name owned --output=json"}}},
		{Resource: "sandbox/volume", Steps: []scenario.Step{{Name: "child", Run: "ecctl sandbox volume create --name owned --team-id team-1 --output=json"}}},
	}
	setupCalled := false
	run, err := Run(context.Background(), Options{Suites: suites, Region: "cn-hangzhou", Surface: "public", EcctlBin: filepath.Join(t.TempDir(), "must-not-launch"), testFixtureSetup: func(*cleanup, map[string]any) error { setupCalled = true; return nil }})
	if err == nil || run == nil || len(run.Cases) != 1 || run.Cases[0].Resource != "setup" || !strings.Contains(run.Cases[0].Error, "ordinary Team creator") || setupCalled {
		t.Fatalf("ordinary Team creator reached fixture setup with Volume: run=%+v err=%v", run, err)
	}
}

func TestVolumeRunAllowsIndependentTeamSuiteWhenVolumeOwnsItsTeam(t *testing.T) {
	suites := []*scenario.Suite{
		{Resource: "sandbox/team", Serial: true, Steps: []scenario.Step{{Name: "independent Team", Run: "ecctl sandbox team create --name separate --output=json"}}},
		{Resource: "sandbox/volume", Serial: true, Steps: []scenario.Step{
			{Name: "owned Team", Run: "ecctl sandbox team create --name owned --output=json"},
			{Name: "owned Volume", Run: "ecctl sandbox volume create --name owned --team-id team-1 --output=json"},
		}},
	}
	fixtureReached := errors.New("fixture setup reached")
	setupCalled := false
	run, err := Run(context.Background(), Options{Suites: suites, Region: "cn-hangzhou", Surface: "public", EcctlBin: filepath.Join(t.TempDir(), "must-not-launch"), testFixtureSetup: func(*cleanup, map[string]any) error { setupCalled = true; return fixtureReached }})
	if !setupCalled || !errors.Is(err, fixtureReached) || run == nil || len(run.Cases) != 1 || run.Cases[0].Resource != "setup" {
		t.Fatalf("independent Team suite was blocked before fixture setup: run=%+v err=%v", run, err)
	}
}

func TestVolumeLookingRawArgumentReachesReadOnlyProvider(t *testing.T) {
	for _, command := range []string{
		"ecctl call ecs DescribeInstances --Tag.1.Key sandbox --Tag.1.Value CreateVolume",
		"ecctl call --api-param Tag.1.Key=FCSandbox --api-param Tag.1.Value=CreateVolume -- ecs DescribeInstances",
		"ecctl call ecs DescribeInstances --Tag.1.Key sandbox --Tag.1.Value volume",
	} {
		t.Run(command, func(t *testing.T) {
			cfg, cl, dir, scope := volumeRecoveryFixture(t)
			marker := markVolumeProvider(t, cfg, dir)
			result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, scenario.Step{Name: "raw read", Run: command}, time.Second)
			if !ok || result.Status != report.StatusPass {
				t.Fatalf("read-only OpenAPI call was rejected: %+v", result)
			}
			calls, err := os.ReadFile(marker)
			if err != nil || !strings.Contains(string(calls), "DescribeInstances") {
				t.Fatalf("read-only provider call missing: %q, err=%v", calls, err)
			}
		})
	}
}

func TestUnboundVolumeTeardownNeverReachesProvider(t *testing.T) {
	for _, command := range []string{
		"ecctl sandbox --region cn-hangzhou volume delete vol-foreign --profile account-a",
		"ecctl --region=cn-hangzhou sbx --profile account-a volume rm vol-foreign",
		"ecctl sbx volume --profile=account-a remove vol-foreign --region=cn-hangzhou",
		"ecctl sandbox --future value volume delete vol-foreign --region cn-hangzhou --profile account-a",
		`ecctl call fcsandbox DeleteVolume --request '{"volumeID":"vol-foreign"}'`,
	} {
		t.Run(command, func(t *testing.T) {
			cfg, cl, dir, scope := volumeRecoveryFixture(t)
			marker := markVolumeProvider(t, cfg, dir)
			step := scenario.Step{Name: "ordinary read", Run: "ecctl sandbox volume get vol-read --output=json", Teardown: command}
			result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second)
			if ok || !strings.Contains(result.Error, "Volume") {
				t.Fatalf("unbound Volume teardown was registered: %+v", result)
			}
			if len(*scope) != 1 || len(readJournalEntries(t, cl.journal)) != 1 {
				t.Fatalf("rejected teardown changed retained cleanup: scope=%d", len(*scope))
			}
			if err := cl.push(scope, "case", command, "primary", nil); err == nil || !strings.Contains(err.Error(), "Volume") {
				t.Fatalf("direct unbound Volume registration was accepted: %v", err)
			}
			if len(*scope) != 1 || len(readJournalEntries(t, cl.journal)) != 1 {
				t.Fatalf("direct rejection changed retained cleanup: scope=%d", len(*scope))
			}

			// Exercise the final sink with an item retained by an older run.
			legacy := &cleanupItem{scope: "case", cmd: command, role: "primary"}
			cl.keep = false
			failures := cl.runCleanup([]*cleanupItem{(*scope)[0], legacy}, false)
			if len(failures) != 1 || !strings.Contains(failures[0], "Volume") || legacy.done || (*scope)[0].done {
				t.Fatalf("unbound Volume child did not stop cleanup before its Team parent: %v", failures)
			}
			calls, err := os.ReadFile(marker)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(calls), "vol-read") || strings.Contains(string(calls), "vol-foreign") || strings.Contains(string(calls), "team delete") || strings.Contains(string(calls), "DeleteVolume") {
				t.Fatalf("unbound Volume or Team parent reached fake provider: %s", calls)
			}
		})
	}
}

func TestVolumeCreateInterspersedFlagsStillReserve(t *testing.T) {
	for _, command := range []string{
		"ecctl sandbox volume --region=cn-hangzhou create --name authored --team-id team-1 --output=json",
		"ecctl sbx --region=cn-hangzhou volume create --name authored --team-id team-1 --output=json",
		"ecctl --region cn-hangzhou sbx volume new --name authored --team-id team-1 --output=json",
		"ecctl sandbox volume --region cn-hangzhou create --name authored --team-id team-1 --no-wait --output=json",
	} {
		t.Run(command, func(t *testing.T) {
			cfg, cl, dir, scope := volumeRecoveryFixture(t)
			marker := markVolumeProvider(t, cfg, dir)
			step := scenario.Step{Name: "protected create", Run: command, Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
			result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second)
			if !ok {
				t.Fatalf("supported create was rejected: %+v", result)
			}
			calls, _ := os.ReadFile(marker)
			if strings.Count(string(calls), "sandbox volume create") != 1 || strings.Contains(string(calls), "volume --region") || len(readJournalEntries(t, cl.journal)) != 2 {
				t.Fatalf("create missed reservation or ran unbound: calls=%s", calls)
			}
		})
	}
}

func TestMixedTeamSuiteVolumeCreateRequiresOwnedFixtureClosure(t *testing.T) {
	cfg, cl, dir, _ := volumeRecoveryFixture(t)
	cl.testSyntheticVolumeReservation = false
	marker := markVolumeProvider(t, cfg, dir)
	suite := &scenario.Suite{Resource: "sandbox/team", Serial: true, Steps: []scenario.Step{{
		Name: "mixed suite Volume create", Run: `ecctl sandbox volume create --name mixed --team-id team-1 --oss-volume-config '{"bucketName":"existing-business-bucket"}' --mount-config '{"role":"business-role"}'`,
		Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json",
	}}}
	caseResult := runCase(context.Background(), Options{RunName: "mixed-volume", InputsDir: dir, StepTimeout: time.Second, Logf: func(string, ...any) {}}, cfg, cl, cl.operations, map[string]any{}, nil, nil, suite)
	if caseResult.Status != report.StatusFail || len(caseResult.Steps) != 1 || caseResult.Steps[0].Status != report.StatusError || !strings.Contains(caseResult.Steps[0].Error, "Volume Team is not an active creator in this case") {
		t.Fatalf("mixed suite admitted an unowned Volume Team: %+v", caseResult)
	}
	calls, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(calls), "volume create") || strings.Contains(string(calls), "CreateVolume") {
		t.Fatalf("unowned mixed suite launched Volume allocation: %s", calls)
	}
}

func TestVolumeCreateInterspersedTransientNeverRetries(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	marker := markVolumeProvider(t, cfg, dir)
	t.Setenv("FAKE_VOLUME_MODE", "read-transient")
	previous := stepRetryDelays
	stepRetryDelays = []time.Duration{0}
	t.Cleanup(func() { stepRetryDelays = previous })
	step := scenario.Step{Name: "uncertain create", Run: "ecctl sandbox volume --region=cn-hangzhou create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second)
	if ok || !strings.Contains(result.Error, "journaled one owned cleanup target") {
		t.Fatalf("uncertain create did not retain ownership: %+v", result)
	}
	calls, _ := os.ReadFile(marker)
	if strings.Count(string(calls), "sandbox volume create") != 1 || strings.Contains(string(calls), "volume --region") || len(readJournalEntries(t, cl.journal)) != 2 {
		t.Fatalf("non-idempotent create retried or lost finalizer: %s", calls)
	}
}

func TestMalformedVolumeCreateRejectedBeforeProvider(t *testing.T) {
	for _, command := range []string{
		"ecctl sandbox volume --future=x create --name authored --team-id team-1",
		"ecctl sandbox volume --region create --name authored --team-id team-1",
		"ecctl sandbox volume --region=cn-beijing create --name authored --team-id team-1",
		"ecctl sandbox volume --region=cn-hangzhou create --name authored --team-id team-1 --help",
		"ecctl sandbox volume create --name authored --team-id team-1 --timeout invalid",
		"ecctl sandbox volume create --name authored --team-id team-1 --timeout 0s",
	} {
		t.Run(command, func(t *testing.T) {
			cfg, cl, dir, scope := volumeRecoveryFixture(t)
			marker := markVolumeProvider(t, cfg, dir)
			step := scenario.Step{Name: "invalid create", Run: command, Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
			result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second)
			if ok || result.Error == "" {
				t.Fatalf("invalid Volume create was admitted: %+v", result)
			}
			calls, _ := os.ReadFile(marker)
			if strings.Contains(string(calls), "volume create") || strings.Contains(string(calls), "volume --region") || strings.Contains(string(calls), "volume --future") || len(readJournalEntries(t, cl.journal)) != 1 {
				t.Errorf("invalid Volume create reached fake provider: %s", calls)
			}
		})
	}
}

func TestVolumeSharedStackWritesRejectBeforeProvider(t *testing.T) {
	for _, command := range []string{
		"ecctl sandbox volume create --name x --team-id team-1",
		"ecctl sbx volume --region=cn-hangzhou new --name x --team-id team-1",
		"ecctl sandbox volume update vol-owned --status=AVAILABLE",
		"ecctl sandbox volume delete vol-owned",
		"ecctl call fcsandbox CreateVolume --request '{}'",
		"ecctl --region cn-hangzhou call FCSandbox UpdateVolume --request '{}'",
		"ecctl call sbx DeleteVolume --request '{}'",
		"ecctl call --request '{}' -- FCSandbox CreateVolume",
		"ecctl call --api-param teamID=team-other -- FCSandbox UpdateVolume",
		"ecctl call --api-param volumeID=vol-other -- FCSandbox DeleteVolume",
		"ecctl call --schema FCSandbox CreateVolume --schema=false --request '{}'",
		"ecctl call --list FCSandbox UpdateVolume --list=false --request '{}'",
		"ecctl call --generate-request FCSandbox DeleteVolume --generate-request=false --request '{}'",
	} {
		t.Run(command, func(t *testing.T) {
			cfg, cl, dir, _ := volumeRecoveryFixture(t)
			marker := markVolumeProvider(t, cfg, dir)
			var scope []*cleanupItem
			step := ProvisionStep{ID: "volume", Resource: "sandbox/volume", Run: command}
			opt := Options{StepTimeout: time.Second, Region: cfg.Region, Surface: "public", EcctlBin: cfg.Bin, Logf: func(string, ...any) {}}
			sc := report.Case{Name: "shared stack"}
			failures := provisionStack(context.Background(), opt, cfg, cl, &scope, map[string]any{}, map[string]any{}, &Fixture{Provision: []ProvisionStep{step}}, &sc, nil)
			if failures["volume"] == "" {
				t.Errorf("shared-stack Volume write was admitted: %+v", sc)
			}
			calls, _ := os.ReadFile(marker)
			if strings.Contains(string(calls), "volume create") || strings.Contains(string(calls), "volume update") || strings.Contains(string(calls), "volume delete") || strings.Contains(string(calls), "CreateVolume") || strings.Contains(string(calls), "UpdateVolume") || strings.Contains(string(calls), "DeleteVolume") || strings.Contains(string(calls), "volume --region") {
				t.Errorf("shared-stack Volume write reached fake provider: %s", calls)
			}
		})
	}
}

func TestVolumeSharedStackTeardownRejectedBeforeProvision(t *testing.T) {
	for _, teardown := range []string{
		"ecctl sandbox volume delete vol-1",
		"ecctl call fcsandbox DeleteVolume --request '{}'",
		`ecctl call --request '{"volumeID":"vol-other"}' -- FCSandbox DeleteVolume`,
	} {
		t.Run(teardown, func(t *testing.T) {
			cfg, cl, dir, _ := volumeRecoveryFixture(t)
			marker := markVolumeProvider(t, cfg, dir)
			var scope []*cleanupItem
			step := ProvisionStep{ID: "volume", Resource: "sandbox/volume", Run: "ecctl sandbox volume list --output=json", Teardown: teardown}
			opt := Options{StepTimeout: time.Second, Region: cfg.Region, Surface: "public", EcctlBin: cfg.Bin, Logf: func(string, ...any) {}}
			sc := report.Case{Name: "shared stack"}
			failures := provisionStack(context.Background(), opt, cfg, cl, &scope, map[string]any{}, map[string]any{}, &Fixture{Provision: []ProvisionStep{step}}, &sc, nil)
			if failures["volume"] == "" {
				t.Fatalf("shared-stack Volume teardown was admitted: %+v", sc)
			}
			calls, _ := os.ReadFile(marker)
			if len(calls) != 0 {
				t.Errorf("provision ran before rejecting Volume teardown: %s", calls)
			}
		})
	}
}

func TestVolumeSharedStackReadOnlyAdmission(t *testing.T) {
	for _, command := range []string{
		"ecctl sandbox volume --region=cn-hangzhou get vol-1",
		"ecctl --region cn-hangzhou sbx volume list --limit 1",
		"ecctl call fcsandbox GetVolume --request '{}'",
		"ecctl call --api-param Tag.1.Value=CreateVolume -- ecs DescribeInstances",
		"ecctl call ecs DescribeInstances --Tag.1.Key sandbox --Tag.1.Value volume",
	} {
		if err := validateStackTemplateCommand(command); err != nil {
			t.Errorf("read-only Volume command %q was rejected: %v", command, err)
		}
	}
}

func TestVolumeSharedStackUnrelatedRawReadReachesProvider(t *testing.T) {
	cfg, cl, dir, _ := volumeRecoveryFixture(t)
	marker := markVolumeProvider(t, cfg, dir)
	var scope []*cleanupItem
	step := ProvisionStep{ID: "ecs_read", Resource: "ecs/instance", Mode: "lookup", Run: "ecctl call ecs DescribeInstances --Tag.1.Key sandbox --Tag.1.Value volume"}
	sc := report.Case{Name: "shared stack"}
	failures := provisionStack(context.Background(), Options{StepTimeout: time.Second, Region: cfg.Region, Surface: "public", EcctlBin: cfg.Bin, Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, map[string]any{}, &Fixture{Provision: []ProvisionStep{step}}, &sc, nil)
	if len(failures) != 0 || len(sc.Steps) != 1 || sc.Steps[0].Status != report.StatusPass {
		t.Fatalf("unrelated shared-stack read was rejected: failures=%+v case=%+v", failures, sc)
	}
	calls, err := os.ReadFile(marker)
	if err != nil || !strings.Contains(string(calls), "DescribeInstances") {
		t.Fatalf("unrelated shared-stack read never reached provider: calls=%q err=%v", calls, err)
	}
}

func TestVolumeOutputShorthandRejectsBeforeProvider(t *testing.T) {
	for _, command := range []string{
		"ecctl sandbox volume update vol-owned --status=AVAILABLE -o json",
		"ecctl sandbox -o json volume update vol-owned --status=AVAILABLE",
		"ecctl sandbox volume delete vol-owned -o json",
	} {
		t.Run(command, func(t *testing.T) {
			cfg, cl, dir, scope := volumeRecoveryFixture(t)
			createOwnedVolumeForMutation(t, cfg, cl, scope)
			marker := markVolumeProvider(t, cfg, dir)
			step := scenario.Step{Name: "unsupported output alias", Run: command}
			if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second); ok || result.Error == "" {
				t.Fatalf("unsupported -o reached Volume provider: %+v", result)
			}
			if calls, _ := os.ReadFile(marker); len(calls) != 0 {
				t.Fatalf("unsupported -o launched fake provider: %s", calls)
			}
		})
	}
}
