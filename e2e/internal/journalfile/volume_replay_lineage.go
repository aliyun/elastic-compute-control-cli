package journalfile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

// VolumeReplayReadback retains the exact public output and request used by a
// scoped read. The transition archive binds these bytes before replay writes.
type VolumeReplayReadback struct {
	Command string `json:"command"`
	Exit    int    `json:"exit"`
	Stdout  string `json:"stdout"`
}

type VolumeReplayProof struct {
	Version     int                    `json:"version"`
	RunID       string                 `json:"run_id"`
	ExecutionID string                 `json:"execution_id"`
	Region      string                 `json:"region"`
	Surface     string                 `json:"surface"`
	VolumeName  string                 `json:"volume_name"`
	VolumeID    string                 `json:"volume_id"`
	Profile     string                 `json:"profile"`
	CurrentTeam string                 `json:"current_team"`
	Status      string                 `json:"status"`
	Readbacks   []VolumeReplayReadback `json:"readbacks"`
}

func volumeReplayProtectedSidecars(root *os.Root, path string, expected ...map[string]string) (map[string][]byte, map[string]bool, error) {
	entries, err := readRootInventory(root)
	if err != nil {
		return nil, nil, err
	}
	base := filepath.Base(path)
	contents, authenticated := map[string][]byte{}, map[string]bool{}
	selected := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, base+".team-create-") && !strings.HasPrefix(name, base+".template-create-") && !strings.HasPrefix(name, base+".replay-completion-") && !strings.HasPrefix(name, base+".template-cache-reconcile-") {
			continue
		}
		if !entry.Type().IsRegular() || !strings.HasSuffix(name, ".json") {
			return nil, nil, fmt.Errorf("historical replay sidecar registry is invalid or too large")
		}
		selected[name] = true
	}
	if len(expected) != 0 {
		for full, digest := range expected[0] {
			if full == path {
				continue
			}
			if filepath.Dir(full) != filepath.Dir(path) || filepath.Clean(full) != full || !strings.HasPrefix(filepath.Base(full), base+".") || !strings.HasSuffix(full, ".json") || !validHistoricalDigest(digest) {
				return nil, nil, fmt.Errorf("historical replay sidecar digest path is invalid")
			}
			selected[filepath.Base(full)] = true
		}
	}
	if len(selected) > 1024 {
		return nil, nil, fmt.Errorf("historical replay sidecar registry is invalid or too large")
	}
	for name := range selected {
		before, err := root.Lstat(name)
		if err != nil || !before.Mode().IsRegular() {
			return nil, nil, fmt.Errorf("historical replay sidecar is not bounded or regular")
		}
		file, err := root.Open(name)
		if err != nil {
			return nil, nil, err
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || !os.SameFile(before, info) || info.Size() > 8<<20 {
			file.Close()
			return nil, nil, fmt.Errorf("historical replay sidecar is not bounded or regular")
		}
		body, private, readErr := ReadRecoveryFileRoot(root, file, name)
		closeErr := file.Close()
		after, statErr := root.Lstat(name)
		if readErr != nil || closeErr != nil || statErr != nil || !os.SameFile(before, after) || !private || len(body) > 8<<20 {
			return nil, nil, fmt.Errorf("historical replay sidecar is not private or exact: %v", readErr)
		}
		if _, err := execpkg.DecodeStrictJSON(body); err != nil {
			return nil, nil, err
		}
		contents[name], authenticated[name] = body, true
	}
	return contents, authenticated, nil
}

func validateVolumeReplayTransferProof(root *os.Root, transition volumeReconcileTransition, prior, next report.CleanupJournal, body []byte) error {
	if len(body) == 0 || len(body) > 8<<20 {
		return fmt.Errorf("Volume transfer replay readback is unavailable")
	}
	if _, err := execpkg.DecodeStrictJSON(body); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var proof VolumeReplayProof
	if decoder.Decode(&proof) != nil || decoder.Decode(new(any)) != io.EOF || proof.Version != 1 || proof.RunID != prior.RunID || proof.ExecutionID != prior.ExecutionID || proof.Region != prior.Region || proof.Surface != prior.Surface || proof.VolumeName == "" || proof.VolumeID == "" || proof.Profile == "" || proof.CurrentTeam == "" || len(proof.Readbacks) == 0 || len(proof.Readbacks) > 16 {
		return fmt.Errorf("Volume transfer replay proof selectors changed")
	}
	intentPath := volumeReconcileSidecarName(transition.JournalPath, "volume-register", proof.VolumeName)
	if intentPath == "" {
		return fmt.Errorf("Volume transfer replay owner is invalid")
	}
	intentBody, err := readVolumeReconcilePeerSidecar(root, transition.JournalPath, intentPath)
	if err != nil {
		return err
	}
	var intent TemplateCreateIntent
	if unmarshalRecoveryJSON(intentBody, &intent) != nil || intent.Version != 2 || intent.Resource != "volume" || intent.State != "journaled" || intent.Name != proof.VolumeName || intent.AcceptedVolumeID != proof.VolumeID || intent.Profile != proof.Profile || intent.RunID != prior.RunID || intent.ExecutionID != prior.ExecutionID || intent.RegionRole != prior.RegionRole || intent.Region != prior.Region || intent.Surface != prior.Surface || intent.EcctlBin != prior.EcctlBin || intent.CaseScope == "" || len(intent.Deletes) != 1 || intent.ValidateDelete(intent.Deletes[0]) != nil || CheckVolumeTeamCreatorSource(prior, intent.TeamID, intent) != nil {
		return fmt.Errorf("Volume transfer replay owner changed")
	}
	references, finalizers := 0, 0
	for _, candidate := range prior.NativeIntentPaths {
		if candidate == intentPath {
			references++
		}
	}
	for _, entry := range prior.Entries {
		if entry.Teardown == intent.Deletes[0] && entry.Scope == intent.CaseScope && entry.RegionRole == prior.RegionRole && entry.Region == prior.Region && entry.ExecutionID == prior.ExecutionID {
			finalizers++
		}
	}
	if references != 1 || finalizers != 1 {
		return fmt.Errorf("Volume transfer replay lacks one retained owner and finalizer")
	}
	teams, err := VolumePossibleTeams(prior, intent)
	if err != nil {
		return err
	}
	index := 0
	cfg := execpkg.Config{Region: intent.Region, Profile: intent.Profile, ExpectedIdentity: &intent.Identity}
	current, status, absent, err := ProbeVolumeScopesWithStatus(context.Background(), cfg, proof.VolumeID, proof.VolumeName, proof.Region, proof.Profile, teams, func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		if index >= len(proof.Readbacks) || proof.Readbacks[index].Command != command || len(proof.Readbacks[index].Stdout) > 1<<19 {
			return execpkg.Result{Err: fmt.Errorf("Volume transfer replay readback command changed")}
		}
		read := proof.Readbacks[index]
		index++
		parsed, parseErr := execpkg.DecodeStrictJSON([]byte(read.Stdout))
		if parseErr != nil {
			return execpkg.Result{Err: parseErr}
		}
		return execpkg.Result{Command: command, Started: true, Exit: read.Exit, Stdout: read.Stdout, JSON: parsed}
	})
	if err != nil || absent || index != len(proof.Readbacks) || current != proof.CurrentTeam || status != proof.Status {
		return fmt.Errorf("Volume transfer replay readback does not prove the recorded destination: %v", err)
	}
	expected, err := cloneVolumeReconcileJournal(prior)
	if err != nil {
		return err
	}
	expected.OriginalJournalSHA256 = transition.OriginalSHA256
	changed, err := ResolveVolumeTransferObservation(&expected, intent, current, status)
	if err != nil || !changed || !reflect.DeepEqual(expected, next) {
		return fmt.Errorf("Volume transfer replay changed outside exact observation: %v", err)
	}
	return nil
}
