package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
)

func TestVolumeFirstResultCannotWriteIntoReplacementReservation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory rename test uses POSIX filesystem semantics")
	}
	for _, replacement := range []string{"parent", "sidecar"} {
		t.Run(replacement, func(t *testing.T) {
			parent := filepath.Join(t.TempDir(), "journal-parent")
			if err := os.Mkdir(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			journal := filepath.Join(parent, "cleanup.json")
			path := journal + ".template-create-0123456789abcdef01234567.json"
			intent := &templateCreateIntent{TemplateCreateIntent: journalfile.TemplateCreateIntent{Version: 2, Resource: "volume", State: "pending"}, path: path, journal: journal}
			body, err := json.MarshalIndent(intent.TemplateCreateIntent, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			body = append(body, '\n')
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatal(err)
			}
			intent.reservationParent, err = os.Stat(parent)
			if err != nil {
				t.Fatal(err)
			}
			intent.reservationSidecar, err = os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			intent.savedBody = append([]byte(nil), body...)
			if replacement == "parent" {
				if err := os.Rename(parent, parent+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(parent, 0o700); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatal(err)
			}
			intent.AcceptedVolumeID = "vol-owned"
			if err := intent.save(); err == nil {
				t.Fatal("accepted ID was installed into a replacement reservation")
			}
			installed, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(installed, body) {
				t.Fatalf("replacement sidecar changed: %v %q", err, installed)
			}
		})
	}
}

func TestVolumeCreateLaunchRefusesLostReservation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory replacement test uses POSIX filesystem semantics")
	}
	for _, replacement := range []string{"parent", "sidecar", "reference"} {
		t.Run(replacement, func(t *testing.T) {
			cfg, cl, _, _ := volumeRecoveryFixture(t)
			step := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
			intent, err := reserveVolumeCreate(context.Background(), step.Run, cfg, cl, step, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if replacement == "parent" {
				parent := filepath.Dir(cl.journal)
				moved := parent + "-moved"
				if err := os.Rename(parent, moved); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.RemoveAll(moved) })
				if err := os.Mkdir(parent, 0o700); err != nil {
					t.Fatal(err)
				}
			} else if replacement == "sidecar" {
				if err := os.Remove(intent.path); err != nil {
					t.Fatal(err)
				}
			} else {
				journal, _, err := readRunnerJournal(cl.journal)
				if err != nil {
					t.Fatal(err)
				}
				journal.NativeIntentPaths = nil
				if err := writeRunnerJournal(cl.journal, journal); err != nil {
					t.Fatal(err)
				}
			}
			invoked := false
			if err := intent.withVolumeLaunchFence(context.Background(), func() { invoked = true }); err == nil || invoked {
				t.Fatalf("lost %s reservation reached CreateVolume: invoked=%v err=%v", replacement, invoked, err)
			}
		})
	}
}

func TestCancelledVolumeLaunchSettlesUnusedReservation(t *testing.T) {
	cfg, cl, _, _ := volumeRecoveryFixture(t)
	step := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	intent, err := reserveVolumeCreate(context.Background(), step.Run, cfg, cl, step, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	invoked := false
	if err := intent.withVolumeLaunchFence(ctx, func() { invoked = true }); err == nil || invoked {
		t.Fatalf("cancelled launch invoked=%v err=%v", invoked, err)
	}
	body, err := os.ReadFile(intent.path)
	var retained journalfile.TemplateCreateIntent
	if err == nil {
		err = json.Unmarshal(body, &retained)
	}
	if err != nil || retained.State != "not_started" || retained.Started == nil || *retained.Started {
		t.Fatalf("unused reservation was not settled: %+v %v", retained, err)
	}
}
