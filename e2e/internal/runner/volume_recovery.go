package runner

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalcmd"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
	"github.com/google/shlex"
	"github.com/spf13/pflag"
)

var writeVolumeCreateReservationJournal = writeRunnerJournalRoot
var writeVolumeTransferReservationJournalRoot = writeRunnerJournalRoot
var errVolumeAbsenceProvisional = errors.New("Volume absence remains provisional")

// Volume creation is non-idempotent. Every executable spelling must reach the
// durable reservation or fail before a provider call.
func volumeCreateCandidate(command string) bool {
	action, err := nativeVolumeMutationAction(command)
	return err == nil && (action == "create" || action == "new")
}

func volumeCreateFlags() *pflag.FlagSet {
	flags := pflag.NewFlagSet("owned Volume create", pflag.ContinueOnError)
	for _, name := range []string{"region", "profile", "lang", "output", "name", "team-id", "oss-volume-config", "agentic-fs-volume-config", "agentic-bucket-volume-config", "mount-config", "timeout"} {
		flags.String(name, "", "")
	}
	for _, name := range []string{"no-wait", "json", "agent-envelope", "no-color", "help", "version"} {
		flags.Bool(name, false, "")
	}
	return flags
}

func reserveVolumeCreate(ctx context.Context, command string, cfg execpkg.Config, cl *cleanup, st scenario.Step, timeout time.Duration, caseScopes ...string) (*templateCreateIntent, error) {
	if cl.session != nil && cl.session.hasRunTeamParent() {
		return nil, fmt.Errorf("owned Volume creation requires a case-owned Team")
	}
	args, err := shlex.Split(command)
	if err != nil {
		return nil, err
	}
	flags := volumeCreateFlags()
	if err := flags.Parse(args[1:]); err != nil {
		return nil, fmt.Errorf("invalid owned Volume create input: %w", err)
	}
	if flags.Lookup("timeout").Changed {
		value, _ := flags.GetString("timeout")
		duration, err := time.ParseDuration(value)
		if err != nil || duration <= 0 {
			return nil, fmt.Errorf("invalid owned Volume create timeout")
		}
	}
	noWait, _ := flags.GetBool("no-wait")
	pos := flags.Args()
	team, _ := flags.GetString("team-id")
	region, _ := flags.GetString("region")
	help, _ := flags.GetBool("help")
	version, _ := flags.GetBool("version")
	if len(pos) != 3 || (pos[0] != "sandbox" && pos[0] != "sbx") || pos[1] != "volume" || (pos[2] != "create" && pos[2] != "new") || help || version || !safeTeamIdentity(team) || flags.Lookup("region").Changed && region != cfg.Region || st.Teardown == "" || st.TeardownRegion != "" && st.TeardownRegion != "primary" {
		return nil, fmt.Errorf("owned Volume create requires exact command, Team, matching region and primary cleanup")
	}
	ossConfig, _ := flags.GetString("oss-volume-config")
	agenticFSConfig, _ := flags.GetString("agentic-fs-volume-config")
	agenticBucketConfig, _ := flags.GetString("agentic-bucket-volume-config")
	mountConfig, _ := flags.GetString("mount-config")
	meta, _, journal := cl.journalValues(&cleanupItem{role: "primary"})
	if _, err := cl.checkedJournalPath("primary"); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(journal); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("owned Volume create requires the real cleanup journal path")
	}
	canonicalJournal, err := journalfile.ResolveJournalPath(journal)
	if err != nil {
		return nil, err
	}
	cleanupCfg := cl.execCfg["primary"]
	if cl.journal == "" || meta.RunID == "" || meta.ExecutionID == "" || cfg.Region == "" || cfg.Bin == "" || !scenario.Surface(meta.Surface).Valid() || cleanupCfg.Region != cfg.Region || cleanupCfg.Bin != cfg.Bin {
		return nil, fmt.Errorf("Volume create requires a cleanup journal bound to run, execution, region, surface and binary")
	}
	cfg.Env = append(append([]string{}, os.Environ()...), cfg.Env...)
	sourceEnv := append([]string{}, cfg.Env...)
	paths, err := journalfile.CaptureTemplateConfig(sourceEnv)
	if err != nil {
		return nil, err
	}
	preflight := journalfile.TemplateCreateIntent{ConfigPaths: paths}
	cfg.Env = preflight.BoundConfig(cfg).Env
	profile, _ := flags.GetString("profile")
	if profile == "" {
		for _, name := range []string{"ECCTL_PROFILE", "ALIBABACLOUD_PROFILE", "ALIBABA_CLOUD_PROFILE", "ALICLOUD_PROFILE"} {
			if value := journalfile.TemplateEnvironmentValue(name, cfg.Env); value != "" {
				profile = value
				break
			}
		}
	}
	profileCtx, cancel := context.WithTimeout(ctx, timeout)
	profile, err = configuredTemplateProfile(profileCtx, cfg, profile)
	cancel()
	if err != nil {
		return nil, err
	}
	identityCtx, identityCancel := context.WithTimeout(ctx, timeout)
	identity, err := execpkg.ReadIdentity(identityCtx, cfg, profile)
	identityCancel()
	if err != nil {
		return nil, err
	}
	cfg.Profile = profile
	cfg.ExpectedIdentity = &identity
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	owner := hex.EncodeToString(nonce[:])
	name := "ecctl-p-" + owner
	caseScopeValue := "case"
	if len(caseScopes) != 0 {
		caseScopeValue = caseScopes[0]
	}
	if caseScopeValue == "" || strings.ContainsAny(caseScopeValue, "\r\n") {
		return nil, fmt.Errorf("Volume create requires a stable case scope")
	}
	intent := &templateCreateIntent{TemplateCreateIntent: journalfile.TemplateCreateIntent{Version: 2, Resource: "volume", RunID: meta.RunID, ExecutionID: meta.ExecutionID, RegionRole: meta.RegionRole, Region: meta.Region, Surface: meta.Surface, EcctlBin: meta.EcctlBin, TeamID: team, Name: name, Profile: profile, CaseScope: caseScopeValue, Selectors: map[string]string{}, ConfigPaths: paths, Identity: identity, CreatedAt: time.Now().UTC(), State: "pending"}, path: journal + ".template-create-" + owner + ".json", journal: journal, cfg: cfg, sourceEnv: sourceEnv, volumeWaitAfterReceipt: noWait}
	for _, key := range journalfile.TemplateSelectorNames {
		intent.Selectors[key] = journalfile.TemplateEnvironmentValue(key, sourceEnv)
	}
	actual := []string{"ecctl", "--output=json", "--agent-envelope=false", "--region", cfg.Region, "--profile", profile, "sandbox", "volume", "create", "--name", name}
	flags.Visit(func(flag *pflag.Flag) {
		switch flag.Name {
		case "name", "profile", "region", "output", "json", "agent-envelope", "help", "version":
		default:
			actual = append(actual, "--"+flag.Name+"="+flag.Value.String())
		}
	})
	intent.command = quotedTemplateCommand(actual)
	// Until the journal links this sidecar and the launch fence is held, no
	// provider call is possible. A crash between the two durable writes must
	// leave an unlinked, non-blocking receipt.
	unstarted := intent.TemplateCreateIntent
	unstarted.State = "not_started"
	exit, started := -1, false
	unstarted.Exit, unstarted.Started = &exit, &started
	body, err := json.MarshalIndent(unstarted, "", "  ")
	if err != nil {
		return nil, err
	}
	knownParent, err := cl.rememberedJournalParent(journal)
	if err != nil || knownParent == nil {
		return nil, fmt.Errorf("Volume journal parent is unavailable: %v", err)
	}
	err = journalfile.WithExistingReplayLock(ctx, journal, func() error {
		if err := checkJournalParentIdentity(journal, knownParent); err != nil {
			return err
		}
		cl.mu.Lock()
		parent, owned, ownErr := cl.retainedTeamParent("primary", team, false)
		cl.mu.Unlock()
		if ownErr != nil {
			return ownErr
		}
		if !owned {
			return fmt.Errorf("Volume Team lacks retained run ownership")
		}
		if parent.RegionRole != "primary" {
			return fmt.Errorf("Volume Team must belong to the primary cleanup role")
		}
		if parent.TeamReplayAttempted {
			return fmt.Errorf("Volume Team deletion was already attempted")
		}
		if !cl.testSyntheticVolumeReservation && (parent.Scope != caseScopeValue || !parent.VolumeTeamBound) {
			return fmt.Errorf("Volume Team is not an active creator in this case")
		}
		required, _, err := cl.requiredTemplateJournal("primary", meta, journal)
		if err != nil {
			return err
		}
		resolved, err := journalfile.ResolveJournalPath(journal)
		if err != nil || resolved != canonicalJournal {
			return fmt.Errorf("Volume journal target changed before reservation")
		}
		if err := journalfile.CheckTemplateCreateIdentity(journal, required, sourceEnv, identity); err != nil {
			return err
		}
		retainedCfg := cfg
		retainedCfg.Env = sourceEnv
		if _, err := journalfile.PrepareTemplateCleanup(ctx, journal, required, retainedCfg); err != nil {
			return fmt.Errorf("validate retained native cleanup credentials before Volume allocation: %w", err)
		}
		var reserved report.CleanupJournal
		err = journalfile.WithExistingParentLock(context.Background(), journal, func() error {
			root, err := cl.openRememberedJournalRoot(journal)
			if err != nil {
				return err
			}
			defer root.Close()
			snapshot, err := journalfile.ReadPrivateRecoverySnapshotRoot(root, filepath.Base(journal))
			if err != nil {
				return err
			}
			existing, legacy, err := decodeRunnerJournal(journal, snapshot.Body, true)
			if err != nil {
				return err
			}
			if legacy || existing.Version != 2 || existing.RunID != meta.RunID || existing.ExecutionID != meta.ExecutionID || existing.RegionRole != meta.RegionRole || existing.Region != meta.Region || existing.Surface != meta.Surface || existing.EcctlBin != meta.EcctlBin {
				return fmt.Errorf("existing Volume journal binding differs")
			}
			if err := journalfile.CheckTemplateCreateIdentity(journal, required, sourceEnv, identity); err != nil {
				return err
			}
			if err := journalfile.CheckTemplateChildTeamBinding(journal, existing, retainedCfg, team, intent.TemplateCreateIntent); err != nil {
				return err
			}
			if err := journalfile.CheckVolumeTeamCreatorSource(existing, team, intent.TemplateCreateIntent); err != nil {
				return err
			}
			if !cl.testSyntheticVolumeReservation {
				if err := journalfile.CheckVolumeTeamCreationLink(existing, team, &parent); err != nil {
					return fmt.Errorf("Volume Team accepted creator is unavailable: %w", err)
				}
			}
			if !cl.testSyntheticVolumeReservation || existing.VolumeFixtureBinding != nil {
				if err := validateOwnedVolumeBacking(existing, ossConfig, agenticFSConfig, agenticBucketConfig, mountConfig); err != nil {
					return err
				}
			}
			if creator := existing.VolumeTeamCreators[team]; creator != nil {
				if _, err := journalfile.ProbeQuotaBinding(ctx, creator, retainedCfg); err != nil {
					return fmt.Errorf("Volume Team creator credential changed before allocation: %w", err)
				}
			}
			if err := journalfile.WriteExclusiveDurableRoot(root, filepath.Base(intent.path), append(body, '\n')); err != nil {
				return err
			}
			intent.reservationParent, err = root.Stat(".")
			if err != nil {
				return err
			}
			intent.reservationSidecar, err = root.Lstat(filepath.Base(intent.path))
			if err != nil || !intent.reservationSidecar.Mode().IsRegular() {
				return fmt.Errorf("Volume create intent reservation is unavailable: %v", err)
			}
			intent.savedBody = append(append([]byte(nil), body...), '\n')
			reference := intent.path
			if existing.NativeJournalPath != "" && existing.NativeJournalPath != canonicalJournal {
				return fmt.Errorf("native ownership journal anchor changed")
			}
			existing.NativeJournalPath = canonicalJournal
			existing.NativeIntentPaths = append(existing.NativeIntentPaths, reference)
			reserved = existing
			if err := checkJournalParentIdentity(journal, knownParent); err != nil {
				return err
			}
			if err := writeVolumeCreateReservationJournal(root, journal, existing); err != nil {
				markedBody, marshalErr := runnerJournalBytes(existing)
				if marshalErr != nil {
					return errors.Join(err, marshalErr)
				}
				installed, readErr := root.ReadFile(filepath.Base(journal))
				if readErr != nil || !bytes.Equal(installed, snapshot.Body) && !bytes.Equal(installed, markedBody) {
					return errors.Join(err, fmt.Errorf("failed Volume reservation bytes changed: %v", readErr))
				}
				if bytes.Equal(installed, markedBody) {
					return errors.Join(err, journalfile.ReaffirmDurableRoot(root, filepath.Base(journal), markedBody))
				}
				return err
			}
			if err := checkJournalParentIdentity(journal, knownParent); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			return err
		}
		cl.mu.Lock()
		if cl.nativeJournals == nil {
			cl.nativeJournals = make(map[string]report.CleanupJournal)
		}
		reserved.NativeIntentPaths = append([]string(nil), reserved.NativeIntentPaths...)
		cl.nativeJournals["primary"] = reserved
		cl.mu.Unlock()
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reserve Volume create intent: %w", err)
	}
	return intent, nil
}

// Protected Volume cases may only reference the already settled fixture
// closure. The public CLI retains its general JSON and @file input contract.
func validateOwnedVolumeBacking(journal report.CleanupJournal, ossConfig, agenticFSConfig, agenticBucketConfig, mountConfig string) error {
	invalid := func() error { return fmt.Errorf("owned Volume create backing differs from its settled run fixture") }
	if journal.Region != "cn-hangzhou" || journal.VolumeFixtureBinding == nil || journal.VolumeFixtureBucket == "" || agenticFSConfig != "" || agenticBucketConfig != "" || strings.HasPrefix(ossConfig, "@") || strings.HasPrefix(mountConfig, "@") {
		return invalid()
	}
	if err := journalfile.ValidateVolumeFixtureIdentity(journal.VolumeFixtureBinding); err != nil {
		return invalid()
	}
	if _, _, err := journalfile.VolumeFixtureRAMParents(journal); err != nil {
		return invalid()
	}
	bucketCount, attachmentCount := 0, 0
	for _, entry := range journal.Entries {
		if entry.VolumeFixtureKind != "bucket" && entry.VolumeFixtureKind != "attachment" {
			continue
		}
		if err := journalfile.ValidateVolumeFixtureTeardown(entry, journal); err != nil || entry.VolumeFixturePending || entry.VolumeFixtureDeleteAttempted || entry.VolumeFixtureDeleteProtocol != 1 || entry.Scope != "stack" || entry.ExecutionID != journal.ExecutionID {
			return invalid()
		}
		if entry.VolumeFixtureKind == "bucket" {
			if !journalfile.ValidVolumeFixtureCreationDate(entry.VolumeFixtureBucketCreationDate) {
				return invalid()
			}
			bucketCount++
		} else {
			attachmentCount++
		}
	}
	if bucketCount != 1 || attachmentCount != 1 {
		return invalid()
	}
	var oss, mount map[string]any
	if execpkg.UnmarshalStrictJSON([]byte(ossConfig), &oss) != nil || execpkg.UnmarshalStrictJSON([]byte(mountConfig), &mount) != nil {
		return invalid()
	}
	if len(oss) != 4 || oss["bucketName"] != journal.VolumeFixtureBucket || oss["bucketPath"] != "/" || oss["endpoint"] != "https://oss-cn-hangzhou.aliyuncs.com" || oss["readOnly"] != true {
		return invalid()
	}
	roleARN, ok := mount["role"].(string)
	if !ok || len(mount) != 1 || !strings.EqualFold(roleARN, "acs:ram::"+journal.VolumeFixtureBinding.Identity.AccountID+":role/"+journal.VolumeFixtureBucket+"-role") {
		return invalid()
	}
	return nil
}

func finishVolumeCreate(res execpkg.Result, cl *cleanup, scope *[]*cleanupItem, data map[string]any, lockKeys []string, timeout time.Duration, intent *templateCreateIntent) error {
	digest := func(value string) string { sum := sha256.Sum256([]byte(value)); return hex.EncodeToString(sum[:]) }
	intent.Exit, intent.Started = &res.Exit, &res.Started
	intent.StdoutSHA256, intent.StderrSHA256 = digest(res.Stdout), digest(res.Stderr)
	if !res.Started {
		intent.State = "not_started"
		if err := intent.save(); err != nil {
			return fmt.Errorf("retain unstarted Volume intent %s: %w", intent.path, err)
		}
		return fmt.Errorf("Volume create did not start; intent %s records no allocation", intent.path)
	}
	root, _ := res.JSON.(map[string]any)
	if requestID := volumeCreateConflictRequest(res, root); requestID != "" {
		intent.State, intent.NoAllocationCode = "not_allocated", "VolumeConflict"
		intent.CreateRequestID, intent.NoAllocationVerified = requestID, true
		if err := intent.save(); err != nil {
			return fmt.Errorf("retain rejected Volume intent %s: %w", intent.path, err)
		}
		return nil
	}
	volume, _ := root["volume"].(map[string]any)
	id, _ := volume["id"].(string)
	commands := []string{}
	if safeTeamIdentity(id) {
		if volume["team_id"] != intent.TeamID || volume["name"] != intent.Name {
			return fmt.Errorf("Volume response ownership mismatch; pending intent %s", intent.path)
		}
		requestID, actionOK := volumeAcceptedCreateRequest(root)
		if !actionOK {
			return fmt.Errorf("Volume response contradicts its CreateVolume action; pending intent %s", intent.path)
		}
		intent.AcceptedVolumeID = id
		intent.CreateRequestID = requestID
		intent.VolumeObserved = res.Err == nil && res.Exit == 0 && volume["status"] == "AVAILABLE" && !intent.volumeWaitAfterReceipt
		commands = append(commands, intent.deleteCommand(id))
	} else if hasRecoveryCommand(res.JSON) {
		failure, _ := root["error"].(map[string]any)
		rawArgs, _ := failure["recovery_command"].([]any)
		if len(rawArgs) != 11 {
			return fmt.Errorf("Volume recovery rejected; pending intent %s", intent.path)
		}
		args := make([]string, len(rawArgs))
		for i, raw := range rawArgs {
			arg, ok := raw.(string)
			if !ok || arg == "" || strings.ContainsAny(arg, "\x00\t\r\n") {
				return fmt.Errorf("Volume recovery rejected; pending intent %s", intent.path)
			}
			args[i] = arg
		}
		// The CLI includes the creation Team as evidence. Its durable delete
		// finalizer omits that selector because the Volume may later transfer.
		command := quotedTemplateCommand(args)
		request, parseErr := journalfile.ParseVolumeDeleteRequest(command)
		if parseErr != nil || args[0] != "ecctl" || args[1] != "sandbox" || args[2] != "volume" || args[3] != "delete" || args[5] != "--region" || args[6] != intent.Region || args[7] != "--team-id" || args[8] != intent.TeamID || args[9] != "--profile" || args[10] != intent.Profile || request.ID != args[4] {
			return fmt.Errorf("Volume recovery selectors differ from accepted create; pending intent %s", intent.path)
		}
		requestID, actionOK := volumeAcceptedCreateRequest(root)
		_, hasActions := root["actions"]
		if !actionOK {
			requestID, actionOK = volumeAcceptedCreateErrorReceipt(res, root, intent, request.ID)
		}
		if !actionOK || requestID == "" && (!hasActions || failure["code"] != "WaitFailed" || failure["current_state"] != "ERROR") {
			return fmt.Errorf("Volume recovery lacks this invocation's accepted CreateVolume request; pending intent %s", intent.path)
		}
		intent.AcceptedVolumeID, intent.CreateRequestID = request.ID, requestID
		commands = append(commands, intent.deleteCommand(request.ID))
	} else {
		if err := intent.save(); err != nil {
			return fmt.Errorf("retain uncertain Volume result in %s: %w", intent.path, err)
		}
		if err := intent.ValidateConfig(intent.sourceEnv); err != nil {
			return fmt.Errorf("pending intent %s: %w", intent.path, err)
		}
		identityCtx, cancelIdentity := context.WithTimeout(context.Background(), timeout)
		_, identityErr := execpkg.ReadIdentity(identityCtx, intent.cfg, intent.Profile)
		cancelIdentity()
		if identityErr != nil {
			return fmt.Errorf("Volume identity probe failed; pending intent %s: %w", intent.path, identityErr)
		}
		args := []string{"ecctl", "sandbox", "volume", "list", "--team-id", intent.TeamID, "--name", intent.Name, "--region", intent.Region, "--profile", intent.Profile, "--limit", "100", "--all", "--output=json", "--agent-envelope=false"}
		listCtx, cancel := context.WithTimeout(context.Background(), timeout)
		listed := execpkg.Run(listCtx, intent.cfg, quotedTemplateCommand(args))
		cancel()
		if listed.Err != nil || listed.Exit != 0 {
			return fmt.Errorf("Volume reconciliation failed; pending intent %s", intent.path)
		}
		list, _ := listed.JSON.(map[string]any)
		items, validItems := list["volumes"].([]any)
		pagination, _ := list["pagination"].(map[string]any)
		more, validPage := pagination["has_more"].(bool)
		if !validItems || !validPage || more {
			return fmt.Errorf("Volume reconciliation incomplete; pending intent %s", intent.path)
		}
		for _, raw := range items {
			item, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("invalid Volume reconciliation item; pending intent %s", intent.path)
			}
			if item["name"] != intent.Name {
				continue
			}
			candidate, _ := item["id"].(string)
			if item["team_id"] != intent.TeamID || !safeTeamIdentity(candidate) || len(commands) != 0 {
				return fmt.Errorf("Volume reconciliation ownership mismatch; pending intent %s", intent.path)
			}
			commands = append(commands, intent.deleteCommand(candidate))
		}
		return fmt.Errorf("Volume name lookup found %d candidate(s) without an invocation-linked accepted ID; pending intent %s", len(commands), intent.path)
	}
	if err := intent.save(); err != nil {
		return fmt.Errorf("retain accepted Volume ID in %s: %w", intent.path, err)
	}
	for _, command := range commands {
		if err := intent.ValidateDelete(command); err != nil {
			return fmt.Errorf("Volume cleanup evidence rejected; pending intent %s: %w", intent.path, err)
		}
		if err := cl.push(scope, caseScope(data), command, "primary", lockKeys, &intent.TemplateCreateIntent); err != nil {
			return fmt.Errorf("Volume cleanup journal failed; pending intent %s: %w", intent.path, err)
		}
	}
	intent.State, intent.Deletes = "journaled", commands
	if err := intent.save(); err != nil {
		return fmt.Errorf("Volume intent commit uncertainty %s: %w", intent.path, err)
	}
	if res.Err == nil && res.Exit == 0 && safeTeamIdentity(id) {
		return nil
	}
	return fmt.Errorf("first Volume create failed; intent %s journaled one owned cleanup target", intent.path)
}

// The authored case uses --no-wait so its accepted ID can be committed before
// a long readiness poll. This read-only wait never changes cleanup authority.
func waitOwnedVolumeReady(ctx context.Context, intent *templateCreateIntent, timeout time.Duration, run func(context.Context, execpkg.Config, string) execpkg.Result) error {
	if intent == nil || intent.State != "journaled" || !safeTeamIdentity(intent.AcceptedVolumeID) || len(intent.Deletes) != 1 || timeout <= 0 {
		return fmt.Errorf("Volume readiness lacks a durable accepted ID")
	}
	bound := intent.BoundConfig(intent.cfg)
	bound.Region, bound.Profile = intent.Region, intent.Profile
	if bound.Bin != intent.EcctlBin || bound.ExpectedIdentity == nil || *bound.ExpectedIdentity != intent.Identity {
		return fmt.Errorf("Volume readiness credential binding changed")
	}
	command := quotedTemplateCommand([]string{"ecctl", "sandbox", "volume", "get", intent.AcceptedVolumeID, "--team-id", intent.TeamID, "--region", intent.Region, "--profile", intent.Profile, "--output=json", "--agent-envelope=false"})
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		if err := waitCtx.Err(); err != nil {
			return fmt.Errorf("Volume readiness remains pending for accepted ID %s: %w", intent.AcceptedVolumeID, err)
		}
		result := run(waitCtx, bound, command)
		root, ok := result.JSON.(map[string]any)
		if result.Err != nil || !result.Started || !ok {
			return fmt.Errorf("Volume readiness read is unavailable; accepted ID %s remains journaled", intent.AcceptedVolumeID)
		}
		if result.Exit == 0 {
			volume, ok := root["volume"].(map[string]any)
			if !ok || root["error"] != nil || volume["id"] != intent.AcceptedVolumeID || volume["name"] != intent.Name || volume["team_id"] != intent.TeamID {
				return fmt.Errorf("Volume readiness identity differs from its accepted create")
			}
			if !intent.VolumeObserved {
				intent.VolumeObserved = true
				if err := intent.save(); err != nil {
					return fmt.Errorf("retain observed Volume ID %s: %w", intent.AcceptedVolumeID, err)
				}
			}
			if volume["status"] == "AVAILABLE" {
				return nil
			}
			if volume["status"] == "ERROR" {
				return fmt.Errorf("Volume readiness reached ERROR for accepted ID %s", intent.AcceptedVolumeID)
			}
		} else if result.Exit != 4 || !volumeReadyVisibilityLag(root) {
			return fmt.Errorf("Volume readiness response is unproven; accepted ID %s remains journaled", intent.AcceptedVolumeID)
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("Volume readiness remains pending for accepted ID %s: %w", intent.AcceptedVolumeID, waitCtx.Err())
		case <-time.After(2 * time.Second):
		}
	}
}

func volumeReadyVisibilityLag(root map[string]any) bool {
	if root["volume"] != nil || root["response"] != nil || root["product"] != nil || root["operation"] != nil {
		return false
	}
	failure, ok := root["error"].(map[string]any)
	if !ok || failure["kind"] != "not_found" || failure["code"] != "NotFound" {
		return false
	}
	actions, ok := root["actions"].([]any)
	if !ok || len(actions) != 1 {
		return false
	}
	action, ok := actions[0].(map[string]any)
	requestID, _ := action["request_id"].(string)
	return ok && action["action_name"] == "GetVolume" && action["code"] == "VolumeNotFound" && action["provider_verified"] == true && requestID != ""
}

func volumeAcceptedCreateRequest(root map[string]any) (string, bool) {
	rawActions, present := root["actions"]
	if !present {
		return "", true
	}
	actions, ok := rawActions.([]any)
	if !ok {
		return "", false
	}
	var requestID string
	found := false
	for _, raw := range actions {
		action, ok := raw.(map[string]any)
		if !ok {
			return "", false
		}
		if action["action_name"] != "CreateVolume" {
			continue
		}
		if found || action["code"] != nil && action["code"] != "" {
			return "", false
		}
		found = true
		candidate, _ := action["request_id"].(string)
		requestID = candidate
	}
	return requestID, found
}

func volumeAcceptedCreateErrorReceipt(res execpkg.Result, root map[string]any, intent *templateCreateIntent, id string) (string, bool) {
	if res.Err != nil || !res.Started || res.Exit != 2 || intent == nil || !safeTeamIdentity(id) {
		return "", false
	}
	failure, ok := root["error"].(map[string]any)
	if !ok || failure["kind"] != "service" || failure["code"] != "InvalidVolumeResponse" || failure["retryable"] != false {
		return "", false
	}
	accepted, ok := failure["accepted_volume"].(map[string]any)
	if !ok || accepted["allocation_confirmed"] != true || accepted["id"] != id || accepted["name"] != intent.Name || accepted["team_id"] != intent.TeamID || accepted["region"] != intent.Region || accepted["operation"] != "CreateVolume" {
		return "", false
	}
	requestID, ok := accepted["request_id"].(string)
	if !ok || requestID == "" || strings.TrimSpace(requestID) != requestID {
		return "", false
	}
	actions, ok := root["actions"].([]any)
	if !ok || len(actions) != 1 {
		return "", false
	}
	action, ok := actions[0].(map[string]any)
	return requestID, ok && action["action_name"] == "CreateVolume" && action["code"] == "InvalidVolumeResponse" && action["request_id"] == requestID && action["provider_verified"] != true
}

func volumeCreateConflictRequest(res execpkg.Result, root map[string]any) string {
	_, hasVolume := root["volume"]
	if res.Err != nil || !res.Started || res.Exit != 2 || root == nil || hasVolume || hasRecoveryCommand(res.JSON) {
		return ""
	}
	failure, _ := root["error"].(map[string]any)
	retryable, ok := failure["retryable"].(bool)
	if !ok || retryable || failure["kind"] != "service" || failure["code"] != "CloudAPIError" {
		return ""
	}
	actions, ok := root["actions"].([]any)
	if !ok || len(actions) != 1 {
		return ""
	}
	action, ok := actions[0].(map[string]any)
	if !ok || action["action_name"] != "CreateVolume" || action["code"] != "VolumeConflict" || action["provider_verified"] != true {
		return ""
	}
	requestID, _ := action["request_id"].(string)
	return requestID
}

// Bind an authored delete to the exact protected Volume before it can execute.
// The durable ownership check is repeated inside the replay fence.
func (c *cleanup) bindOwnedVolumeDelete(scope []*cleanupItem, command, role string) (string, *cleanupItem, error) {
	action, err := nativeVolumeMutationAction(command)
	if err != nil {
		return "", nil, err
	}
	if action != "delete" && action != "rm" && action != "remove" {
		return command, nil, nil
	}
	request, err := journalfile.ParseVolumeDeleteRequest(command)
	if err != nil {
		return "", nil, err
	}
	if request.NoWait {
		return "", nil, fmt.Errorf("owned Volume delete requires the waited absence result")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var found *cleanupItem
	for _, item := range scope {
		if item.templateBinding == nil || item.templateBinding.Resource != "volume" {
			continue
		}
		owned, parseErr := journalfile.ParseVolumeDeleteRequest(item.cmd)
		if parseErr != nil {
			return "", nil, parseErr
		}
		if owned.ID != request.ID {
			continue
		}
		if item.role != role || item.done {
			return "", nil, fmt.Errorf("Volume delete target is consumed or belongs to another cleanup role")
		}
		if found != nil {
			return "", nil, fmt.Errorf("ambiguous owned Volume delete target")
		}
		found = item
	}
	if found == nil {
		return "", nil, fmt.Errorf("Volume delete lacks an active run-owned finalizer")
	}
	binding := found.templateBinding
	if request.RegionSet && request.Region != binding.Region || request.ProfileSet && request.Profile != binding.Profile {
		return "", nil, fmt.Errorf("owned Volume delete changed region or profile")
	}
	if request.OutputSet && request.Output != "json" || request.AgentEnvelopeSet && request.AgentEnvelope {
		return "", nil, fmt.Errorf("owned Volume delete requires plain JSON output")
	}
	args, err := shlex.Split(command)
	if err != nil {
		return "", nil, err
	}
	if !request.RegionSet {
		args = append(args, "--region", binding.Region)
	}
	if !request.ProfileSet {
		args = append(args, "--profile", binding.Profile)
	}
	if !request.OutputSet {
		args = append(args, "--output=json")
	}
	if !request.AgentEnvelopeSet {
		args = append(args, "--agent-envelope=false")
	}
	return quotedTemplateCommand(args), found, nil
}

// Bind an authored same-Team update to the exact protected Volume before it
// can execute. The update replaces configuration in the owned Team scope and
// never consumes or re-anchors the creation finalizer.
func (c *cleanup) bindOwnedVolumeUpdate(scope []*cleanupItem, command string) (string, error) {
	action, err := nativeVolumeMutationAction(command)
	if err != nil {
		return "", err
	}
	if action != "update" {
		return command, nil
	}
	request, err := journalfile.ParseVolumeUpdateRequest(command)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var found *cleanupItem
	for _, item := range scope {
		if item.templateBinding == nil || item.templateBinding.Resource != "volume" {
			continue
		}
		owned, parseErr := journalfile.ParseVolumeDeleteRequest(item.cmd)
		if parseErr != nil {
			return "", parseErr
		}
		if owned.ID != request.ID {
			continue
		}
		if item.role != "primary" || item.done {
			return "", fmt.Errorf("Volume update target is consumed or belongs to another cleanup role")
		}
		if found != nil {
			return "", fmt.Errorf("ambiguous owned Volume update target")
		}
		found = item
	}
	if found == nil {
		return "", fmt.Errorf("Volume update lacks an active run-owned finalizer")
	}
	binding := found.templateBinding
	if request.TeamID != binding.TeamID {
		return "", fmt.Errorf("Volume update requires the owned Team scope")
	}
	if request.RegionSet && request.Region != binding.Region || request.ProfileSet && request.Profile != binding.Profile {
		return "", fmt.Errorf("owned Volume update changed region or profile")
	}
	if request.OutputSet && request.Output != "json" || request.AgentEnvelopeSet && request.AgentEnvelope {
		return "", fmt.Errorf("owned Volume update requires plain JSON output")
	}
	args, err := shlex.Split(command)
	if err != nil {
		return "", err
	}
	if !request.RegionSet {
		args = append(args, "--region", binding.Region)
	}
	if !request.ProfileSet {
		args = append(args, "--profile", binding.Profile)
	}
	if !request.OutputSet {
		args = append(args, "--output=json")
	}
	if !request.AgentEnvelopeSet {
		args = append(args, "--agent-envelope=false")
	}
	return quotedTemplateCommand(args), nil
}

// The same classifier feeds case reservation, stack admission and offline lint.
func nativeVolumeMutationAction(command string) (string, error) {
	return journalcmd.VolumeMutationAction(command)
}

// classifyVolumeFinalizer keeps every in-memory cleanup path behind the same
// Volume family parser used by case admission. On-disk replay remains stricter.
func classifyVolumeFinalizer(command string) (bool, error) {
	if journalcmd.RawVolumeMutationCandidate(command) {
		return false, fmt.Errorf("raw Volume mutation cannot be an ordinary finalizer")
	}
	action, err := journalcmd.VolumeCommandAction(command)
	if err != nil {
		return false, fmt.Errorf("cannot classify Volume finalizer: %w", err)
	}
	switch action {
	case "":
		return false, nil
	case "delete", "rm", "remove":
		return true, nil
	default:
		return false, fmt.Errorf("Volume finalizer must be a delete")
	}
}

// A read keeps the creation source even after its delete finalizer was
// consumed. This binding never makes that completed finalizer executable.
func (c *cleanup) bindOwnedVolumeRead(ctx context.Context, command, role string, cfg execpkg.Config, timeout time.Duration) (string, execpkg.Config, error) {
	action, err := journalcmd.VolumeCommandAction(command)
	if err != nil {
		return "", cfg, err
	}
	if action != "get" && action != "show" && action != "describe" && action != "list" && action != "ls" {
		return command, cfg, nil
	}
	args, err := shlex.Split(command)
	if err != nil {
		return "", cfg, err
	}
	flags := pflag.NewFlagSet("owned Volume read", pflag.ContinueOnError)
	flags.SetOutput(io.Discard)
	for _, name := range []string{"region", "profile", "team-id", "lang", "output", "name", "status", "resource-group", "user-id", "limit", "next-token", "filter", "fields", "timeout"} {
		flags.String(name, "", "")
	}
	for _, name := range []string{"all", "json", "agent-envelope", "no-color", "version"} {
		flags.Bool(name, false, "")
	}
	flags.BoolP("help", "h", false, "")
	if err := flags.Parse(args[1:]); err != nil {
		return "", cfg, fmt.Errorf("invalid owned Volume read flags: %w", err)
	}
	selectors := map[string]int{}
	for i := 1; i < len(args); i++ {
		name, _, inline := strings.Cut(args[i], "=")
		if name != "--region" && name != "--profile" && name != "--team-id" {
			continue
		}
		selectors[name]++
		if selectors[name] > 1 {
			return "", cfg, fmt.Errorf("duplicate owned Volume read selector: %s", name)
		}
		if !inline {
			i++
		}
	}
	if help, _ := flags.GetBool("help"); help {
		return command, cfg, nil
	}
	if version, _ := flags.GetBool("version"); version {
		return command, cfg, nil
	}
	pos := flags.Args()
	list := action == "list" || action == "ls"
	if list && len(pos) != 3 || !list && len(pos) != 4 || (pos[0] != "sandbox" && pos[0] != "sbx") || pos[1] != "volume" {
		return "", cfg, fmt.Errorf("invalid owned Volume read target")
	}
	region, _ := flags.GetString("region")
	if flags.Lookup("region").Changed && region != cfg.Region {
		return "", cfg, fmt.Errorf("owned Volume read region differs from assigned region")
	}
	team, _ := flags.GetString("team-id")
	if flags.Lookup("team-id").Changed && !safeTeamIdentity(team) {
		return "", cfg, fmt.Errorf("invalid owned Volume read Team")
	}
	if !list && !safeTeamIdentity(pos[3]) {
		return "", cfg, fmt.Errorf("invalid owned Volume read ID")
	}
	c.mu.Lock()
	hasOwnedTarget := false
	for _, item := range c.ownedTemplates {
		if item.role != role || item.templateBinding == nil || item.templateBinding.Resource != "volume" {
			continue
		}
		if list {
			hasOwnedTarget = true
			break
		}
		owned, parseErr := journalfile.ParseVolumeDeleteRequest(item.cmd)
		if parseErr != nil {
			c.mu.Unlock()
			return "", cfg, parseErr
		}
		if owned.ID == pos[3] {
			hasOwnedTarget = true
			break
		}
	}
	c.mu.Unlock()
	if !hasOwnedTarget {
		return command, cfg, nil
	}
	// Transfer markers are durable and identify the new Team's original
	// Volume source. Require the matching confirmed Team entry as well.
	transferName := ""
	var destinationJournal report.CleanupJournal
	if team != "" && c.journal != "" {
		journal, legacy, readErr := readRunnerJournal(c.journal)
		if readErr != nil || legacy || journal.RunID != c.journalMeta.RunID || journal.ExecutionID != c.journalMeta.ExecutionID || journal.Region != cfg.Region || journal.EcctlBin != cfg.Bin {
			return "", cfg, fmt.Errorf("owned Volume read journal binding changed")
		}
		transferName = journal.VolumeTeamBindings[team]
		destinationJournal = journal
		if transferName != "" {
			matches := 0
			for _, entry := range journal.Entries {
				if !entry.TeamCreateConfirmed {
					continue
				}
				id, parseErr := journalfile.TeamDeleteTarget(entry.Teardown)
				if parseErr != nil {
					return "", cfg, parseErr
				}
				if id == team && entry.VolumeTransferIntent == transferName && entry.RegionRole == role && entry.Region == cfg.Region && entry.ExecutionID == journal.ExecutionID {
					matches++
				}
			}
			if matches != 1 {
				return "", cfg, fmt.Errorf("owned Volume read Team transfer binding is ambiguous")
			}
		}
	}
	c.mu.Lock()
	var selected *journalfile.TemplateCreateIntent
	selectedDone := false
	selectedTransferTeam := ""
	for _, item := range c.ownedTemplates {
		if item.role != role || item.templateBinding == nil || item.templateBinding.Resource != "volume" {
			continue
		}
		owned, parseErr := journalfile.ParseVolumeDeleteRequest(item.cmd)
		if parseErr != nil {
			c.mu.Unlock()
			return "", cfg, parseErr
		}
		binding := item.templateBinding
		if list {
			if team != "" && binding.TeamID != team && (transferName == "" || item.volumeTransferTeam != team) {
				continue
			}
			if selected != nil && selectedDone != item.done {
				if item.done {
					continue
				}
				selected = nil
			}
		} else if owned.ID != pos[3] {
			continue
		}
		if selected != nil && (!list || selected.Profile != binding.Profile || selected.Identity != binding.Identity || selected.Region != binding.Region || selected.EcctlBin != binding.EcctlBin || !reflect.DeepEqual(selected.ConfigPaths, binding.ConfigPaths) || !reflect.DeepEqual(selected.Selectors, binding.Selectors)) {
			c.mu.Unlock()
			return "", cfg, fmt.Errorf("ambiguous owned Volume read source")
		}
		copy := *binding
		selected = &copy
		selectedDone = item.done
		selectedTransferTeam = item.volumeTransferTeam
	}
	c.mu.Unlock()
	if selected == nil {
		if transferName != "" {
			return "", cfg, fmt.Errorf("owned Volume read lacks its transferred source")
		}
		return command, cfg, nil
	}
	if team != "" && !list && team != selected.TeamID && (transferName == "" || selectedTransferTeam != team) {
		return "", cfg, fmt.Errorf("owned Volume read Team selector conflicts with its source")
	}
	profile, _ := flags.GetString("profile")
	if cfg.Bin != selected.EcctlBin || cfg.Region != selected.Region || cfg.Profile != "" && cfg.Profile != selected.Profile || cfg.ExpectedIdentity != nil && *cfg.ExpectedIdentity != selected.Identity || flags.Lookup("region").Changed && region != selected.Region || flags.Lookup("profile").Changed && profile != selected.Profile {
		return "", cfg, fmt.Errorf("owned Volume read changed protected selectors")
	}
	if err := selected.ValidateConfig(cfg.Env); err != nil {
		return "", cfg, err
	}
	for key, value := range selected.Selectors {
		if journalfile.TemplateEnvironmentValue(key, cfg.Env) != value {
			return "", cfg, fmt.Errorf("owned Volume read configuration selector changed: %s", key)
		}
	}
	if transferName != "" {
		if err := journalfile.CheckVolumeTransferTeamBinding(c.journal, destinationJournal, cfg, team, *selected); err != nil {
			return "", cfg, err
		}
	}
	bound := selected.BoundConfig(cfg)
	bound.Region, bound.Profile = selected.Region, selected.Profile
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	_, err = execpkg.ReadIdentity(probeCtx, bound, selected.Profile)
	cancel()
	if err != nil {
		return "", cfg, err
	}
	if !flags.Lookup("region").Changed {
		args = append(args, "--region", selected.Region)
	}
	if !flags.Lookup("profile").Changed {
		args = append(args, "--profile", selected.Profile)
	}
	return quotedTemplateCommand(args), bound, nil
}

func (c *cleanup) requireTerminalVolumeAbsence(item *cleanupItem) error {
	if item == nil || item.templateBinding == nil {
		return fmt.Errorf("Volume absence lacks an accepted create binding")
	}
	_, expected, path := c.journalValues(item)
	journal, legacy, err := readRunnerJournal(path)
	if err != nil || legacy {
		return fmt.Errorf("Volume absence journal is unavailable: %v", err)
	}
	intent, err := journalfile.VolumeDeleteBinding(path, journal, c.execCfg[item.role].Env, item.cmd)
	if err != nil {
		return err
	}
	for _, entry := range journal.Entries {
		if entry.Teardown == expected.Teardown {
			if journalfile.VolumeAbsenceTerminal(journal, intent, entry) {
				return nil
			}
			return fmt.Errorf("%w for accepted ID %s", errVolumeAbsenceProvisional, intent.AcceptedVolumeID)
		}
	}
	return fmt.Errorf("Volume absence finalizer is missing")
}

func (c *cleanup) ownedTeamAbsence(ctx context.Context, item *cleanupItem) (execpkg.Result, error) {
	var proof execpkg.Result
	if item == nil || !safeTeamIdentity(item.ownedTeamID) || !item.quotaBound && !item.volumeTeamBound {
		return proof, fmt.Errorf("protected Team absence lacks a retained creator")
	}
	_, _, path := c.journalValues(item)
	journal, legacy, err := readRunnerJournal(path)
	if err != nil || legacy {
		return proof, fmt.Errorf("protected Team absence journal is unavailable: %v", err)
	}
	var bound execpkg.Config
	if item.volumeTeamBound {
		var found bool
		bound, found, err = journalfile.VolumeTeamCreatorConfig(ctx, journal, c.execCfg[item.role], item.ownedTeamID)
		if err != nil || !found {
			return proof, fmt.Errorf("Volume Team original credential is unavailable: %v", err)
		}
	} else {
		bound, err = journalfile.ProbeQuotaBinding(ctx, journal.QuotaBinding, c.execCfg[item.role])
		if err != nil {
			return proof, fmt.Errorf("Quota Team original credential is unavailable: %w", err)
		}
	}
	run := func(ctx context.Context, cfg execpkg.Config, command string) execpkg.Result {
		proof = c.runCommand(ctx, cfg, command)
		return proof
	}
	err = journalfile.ProbeVolumeTeamAbsent(ctx, bound, item.ownedTeamID, bound.Region, bound.Profile, run)
	return proof, err
}

func verifyOwnedVolumeExecution(binding *journalfile.TemplateCreateIntent, current execpkg.Config) error {
	if binding == nil || current.Bin != binding.EcctlBin || current.Region != binding.Region || current.Profile != "" && current.Profile != binding.Profile {
		return fmt.Errorf("owned Volume execution target changed")
	}
	if current.ExpectedIdentity != nil && *current.ExpectedIdentity != binding.Identity {
		return fmt.Errorf("owned Volume identity binding changed")
	}
	return binding.ValidateConfig(current.Env)
}

// The caller holds the replay lock. A step must prove durable ownership before
// invoking a protected Volume delete, then prove absence before consumption.
func (c *cleanup) verifyOwnedVolumeDelete(item *cleanupItem, command string) error {
	if item == nil || item.templateBinding == nil || item.templateBinding.Resource != "volume" {
		return fmt.Errorf("owned Volume delete lacks an active creation binding")
	}
	c.mu.Lock()
	done := item.done
	c.mu.Unlock()
	if done {
		return fmt.Errorf("owned Volume delete lacks an active creation binding")
	}
	request, err := journalfile.ParseVolumeDeleteRequest(command)
	if err != nil {
		return err
	}
	owned, err := journalfile.ParseVolumeDeleteRequest(item.cmd)
	if err != nil || request.ID != owned.ID || request.Region != item.templateBinding.Region || request.Profile != item.templateBinding.Profile || request.Output != "json" || request.AgentEnvelope {
		return fmt.Errorf("owned Volume delete target or selectors changed")
	}
	if err := item.templateBinding.ValidateDelete(item.cmd); err != nil {
		return err
	}
	meta, expected, requested := c.journalValues(item)
	meta, path, err := c.requiredTemplateJournal(item.role, meta, requested)
	if err != nil {
		return err
	}
	if err := journalfile.CheckTemplateDeleteOwnership(path, meta, c.execCfg[item.role].Env, *item.templateBinding, item.cmd); err != nil {
		return err
	}
	var possibleTeams []string
	if err := journalfile.WithExistingParentLock(context.Background(), path, func() error {
		journal, legacy, err := readRunnerJournal(path)
		if err != nil {
			return err
		}
		if legacy || journal.Version != meta.Version || journal.RunID != meta.RunID || journal.ExecutionID != meta.ExecutionID || journal.RegionRole != meta.RegionRole || journal.Region != meta.Region || journal.Surface != meta.Surface || journal.EcctlBin != meta.EcctlBin {
			return fmt.Errorf("owned Volume journal binding changed")
		}
		count := 0
		for _, entry := range journal.Entries {
			if entry.Teardown == expected.Teardown {
				if entry != expected {
					return fmt.Errorf("owned Volume finalizer metadata changed")
				}
				count++
			}
		}
		if count != 1 {
			return fmt.Errorf("owned Volume finalizer is missing or ambiguous")
		}
		possibleTeams, err = journalfile.VolumePossibleTeams(journal, *item.templateBinding)
		if err != nil {
			return err
		}
		return journalfile.CheckVolumeDeleteTeamEvidence(journal, *item.templateBinding, possibleTeams, c.execCfg[item.role].Env)
	}); err != nil {
		return err
	}
	if request.TeamSet {
		matched := false
		for _, team := range possibleTeams {
			matched = matched || team == request.TeamID
		}
		if !matched {
			return fmt.Errorf("Volume delete Team selector was never retained for this Volume")
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		parent, owned, err := c.retainedTeamParent(item.role, request.TeamID, false)
		if err != nil {
			return err
		}
		if !owned || !parent.TeamCreateConfirmed || parent.RegionRole != item.role || parent.TeamReplayAttempted {
			return fmt.Errorf("Volume delete Team selector lacks active run ownership")
		}
	}
	return nil
}

// The replay lock is held through the provider operation. The journal write
// records uncertainty before the first protected Volume delete can be sent.
func (c *cleanup) markOwnedVolumeDeleteAttempt(item *cleanupItem) error {
	if item == nil || item.templateBinding == nil || item.templateBinding.Resource != "volume" || item.volumeDeleteAttempted || item.done {
		return fmt.Errorf("Volume has no fresh delete attempt")
	}
	meta, expected, path := c.journalValues(item)
	if _, err := c.checkedJournalPath(item.role); err != nil {
		return err
	}
	err := journalfile.WithExistingParentLock(context.Background(), path, func() error {
		root, err := c.openRememberedJournalRoot(path)
		if err != nil {
			return err
		}
		defer root.Close()
		journal, legacy, err := readRunnerJournalRoot(root, path)
		if err != nil {
			return err
		}
		if legacy || journal.Version != meta.Version || journal.JournalPath != path || journal.RunID != meta.RunID || journal.ExecutionID != meta.ExecutionID || journal.RegionRole != meta.RegionRole || journal.Region != meta.Region || journal.Surface != meta.Surface || journal.EcctlBin != meta.EcctlBin {
			return fmt.Errorf("Volume delete journal binding changed")
		}
		index := -1
		for i, entry := range journal.Entries {
			if entry.Teardown != expected.Teardown {
				continue
			}
			if entry != expected || index >= 0 {
				return fmt.Errorf("Volume delete entry changed or became ambiguous")
			}
			index = i
		}
		if index < 0 {
			return fmt.Errorf("Volume delete entry disappeared")
		}
		originalBody, err := root.ReadFile(filepath.Base(path))
		if err != nil {
			return err
		}
		journal.Entries[index].VolumeDeleteAttempted = true
		markedBody, err := json.MarshalIndent(journal, "", "  ")
		if err != nil {
			return err
		}
		markedBody = append(markedBody, '\n')
		if err := writeRunnerJournalRoot(root, path, journal); err != nil {
			// A rename may have succeeded before its directory barrier failed.
			// No subprocess has started, so restore this exact fresh marker.
			if installed, readErr := root.ReadFile(filepath.Base(path)); readErr == nil && bytes.Equal(installed, markedBody) {
				return errors.Join(err, journalfile.RestoreUninvokedMarkerRoot(root, filepath.Base(path), markedBody, originalBody))
			}
			return err
		}
		installed, legacy, err := readRunnerJournalRoot(root, path)
		if err != nil || legacy || !reflect.DeepEqual(installed, journal) {
			return fmt.Errorf("Volume delete attempt was not installed exactly")
		}
		return journalRootMatchesPath(root, path)
	})
	if err == nil {
		item.volumeDeleteAttempted = true
	}
	return err
}

// Only the call that just installed this exact bit may undo it after a
// demonstrably unstarted request or a verified no-mutation provider rejection.
func (c *cleanup) restoreUnstartedOwnedVolumeDeleteAttempt(item *cleanupItem) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if item == nil || item.templateBinding == nil || item.templateBinding.Resource != "volume" || !item.volumeDeleteAttempted || item.done {
		return fmt.Errorf("Volume has no newly marked delete attempt")
	}
	meta, expected, path := c.journalValues(item)
	if _, err := c.checkedJournalPath(item.role); err != nil {
		return err
	}
	restoreCtx, cancelRestore := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelRestore()
	err := journalfile.WithExistingParentLock(restoreCtx, path, func() error {
		if _, err := c.checkedJournalParentLocked(path); err != nil {
			return err
		}
		root, err := c.openLockedJournalRoot(path)
		if err != nil {
			return err
		}
		defer root.Close()
		journal, legacy, err := readRunnerJournalRoot(root, path)
		if err != nil {
			return err
		}
		if legacy || journal.Version != meta.Version || journal.JournalPath != path || journal.RunID != meta.RunID || journal.ExecutionID != meta.ExecutionID || journal.RegionRole != meta.RegionRole || journal.Region != meta.Region || journal.Surface != meta.Surface || journal.EcctlBin != meta.EcctlBin {
			return fmt.Errorf("Volume delete journal binding changed before marker restoration")
		}
		index := -1
		for i, entry := range journal.Entries {
			if entry.Teardown != expected.Teardown {
				continue
			}
			if entry != expected || index >= 0 {
				return fmt.Errorf("Volume delete entry changed or became ambiguous before marker restoration")
			}
			index = i
		}
		if index < 0 {
			return fmt.Errorf("Volume delete entry disappeared before marker restoration")
		}
		journal.Entries[index].VolumeDeleteAttempted = false
		if err := writeRunnerJournalRoot(root, path, journal); err != nil {
			body, bodyErr := runnerJournalBytes(journal)
			if bodyErr != nil {
				return errors.Join(err, bodyErr)
			}
			if barrierErr := journalfile.ReaffirmDurableRoot(root, filepath.Base(path), body); barrierErr != nil {
				return errors.Join(err, barrierErr)
			}
		}
		installed, legacy, err := readRunnerJournalRoot(root, path)
		if err != nil || legacy || !reflect.DeepEqual(installed, journal) {
			return fmt.Errorf("unused Volume delete marker was not restored exactly")
		}
		return journalRootMatchesPath(root, path)
	})
	if err == nil {
		item.volumeDeleteAttempted = false
	}
	return err
}

func (c *cleanup) ownedVolumeDeleteScopes(item *cleanupItem) ([]string, error) {
	if item == nil || item.templateBinding == nil || item.templateBinding.Resource != "volume" {
		return nil, fmt.Errorf("Volume delete lacks its creation binding")
	}
	_, expected, path := c.journalValues(item)
	journal, legacy, err := readRunnerJournal(path)
	if err != nil || legacy || journal.RunID != item.templateBinding.RunID || journal.ExecutionID != item.templateBinding.ExecutionID || journal.Region != item.templateBinding.Region || journal.EcctlBin != item.templateBinding.EcctlBin {
		return nil, fmt.Errorf("Volume delete journal binding changed: %v", err)
	}
	count := 0
	for _, entry := range journal.Entries {
		if entry.Teardown == expected.Teardown {
			if entry != expected {
				return nil, fmt.Errorf("Volume delete entry changed")
			}
			count++
		}
	}
	if count != 1 {
		return nil, fmt.Errorf("Volume delete entry is missing or ambiguous")
	}
	if len(journal.VolumeScopeBindings[item.templateBinding.Name]) != 0 || len(journal.VolumeTransferHistory[item.templateBinding.Name]) != 0 || expected.VolumeTransferPending {
		return nil, fmt.Errorf("historical Volume transfer requires separate reconciliation")
	}
	teams, err := journalfile.VolumePossibleTeams(journal, *item.templateBinding)
	if err != nil {
		return nil, err
	}
	if err := journalfile.CheckVolumeDeleteTeamEvidence(journal, *item.templateBinding, teams, c.execCfg[item.role].Env); err != nil {
		return nil, err
	}
	return teams, nil
}

// The provider has rejected this invocation before mutation. A fresh read
// must still bind the exact owned ID, name and Team before the marker is
// restored. The next invocation performs its own preflight before retrying.
func (c *cleanup) restoreRejectedOwnedVolumeDeleteAttempt(ctx context.Context, item *cleanupItem, bound execpkg.Config) error {
	if item == nil || item.templateBinding == nil {
		return fmt.Errorf("rejected Volume delete lacks its retained binding")
	}
	request, err := journalfile.ParseVolumeDeleteRequest(item.cmd)
	if err != nil {
		return err
	}
	teams, err := c.ownedVolumeDeleteScopes(item)
	if err != nil {
		return err
	}
	current, absent, err := journalfile.ProbeVolumeScopes(ctx, bound, request.ID, item.templateBinding.Name, item.templateBinding.Region, item.templateBinding.Profile, teams, c.runCommand)
	if err != nil || absent || current == "" {
		return fmt.Errorf("rejected Volume remains present in an owned Team is unproven: %v", err)
	}
	return c.restoreUnstartedOwnedVolumeDeleteAttempt(item)
}

// The caller holds the replay lock. Recheck both the exact entry and the
// cross-Team absence authority at commit time before removing a finalizer.
func (c *cleanup) consumeOwnedVolumeDelete(item *cleanupItem) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if item.done || item.templateBinding == nil || item.templateBinding.Resource != "volume" {
		return fmt.Errorf("owned Volume finalizer is already consumed")
	}
	meta, expected, path := c.journalValues(item)
	if _, err := c.checkedJournalPath(item.role); err != nil {
		return err
	}
	err := journalfile.WithExistingParentLock(context.Background(), path, func() error {
		root, err := c.openLockedJournalRoot(path)
		if err != nil {
			return err
		}
		defer root.Close()
		journal, legacy, err := readRunnerJournalRoot(root, path)
		if err != nil {
			return err
		}
		if legacy || journal.Version != meta.Version || journal.RunID != meta.RunID || journal.ExecutionID != meta.ExecutionID || journal.RegionRole != meta.RegionRole || journal.Region != meta.Region || journal.Surface != meta.Surface || journal.EcctlBin != meta.EcctlBin {
			return fmt.Errorf("owned Volume journal binding changed before consumption")
		}
		index := -1
		for i, entry := range journal.Entries {
			if entry.Teardown != expected.Teardown {
				continue
			}
			if entry != expected || index >= 0 {
				return fmt.Errorf("owned Volume finalizer changed or became ambiguous")
			}
			index = i
		}
		if index < 0 {
			return fmt.Errorf("owned Volume finalizer disappeared before consumption")
		}
		intent, err := journalfile.VolumeDeleteBinding(path, journal, c.execCfg[item.role].Env, item.cmd)
		if err != nil || !journalfile.VolumeAbsenceTerminal(journal, intent, journal.Entries[index]) {
			return fmt.Errorf("owned Volume absence is not authoritative across Team changes: %v", err)
		}
		if err := journalfile.RetireVolumeFinalizer(&journal, intent, journal.Entries[index]); err != nil {
			return err
		}
		if err := writeRunnerJournalRoot(root, path, journal); err != nil {
			return err
		}
		return journalRootMatchesPath(root, path)
	})
	if err == nil {
		item.done = true
	}
	return err
}
