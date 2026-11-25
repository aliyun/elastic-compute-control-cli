package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
)

func TestVolumeUpdateCandidate(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    bool
	}{
		{"ecctl sandbox volume update vol-1 --team-id team-2", true},
		{"ecctl sbx volume update vol-1 --team-id team-2", true},
		{"ecctl sandbox volume create --name x --team-id team-1", false},
		{"ecctl sandbox volume delete vol-1 --team-id team-1", false},
		{"ecctl sandbox volume get vol-1 --team-id team-1", false},
		{"ecctl call fcsandbox UpdateVolume --request '{}'", false},
	} {
		if got := volumeUpdateCandidate(tc.command); got != tc.want {
			t.Errorf("volumeUpdateCandidate(%q) = %t, want %t", tc.command, got, tc.want)
		}
	}
}

func TestVolumeTransferAccepted(t *testing.T) {
	good := map[string]any{"actions": []any{map[string]any{"action_name": "UpdateVolume", "provider_verified": true, "request_id": "req-update"}}}
	if !volumeTransferAccepted(good) {
		t.Fatal("verified UpdateVolume action was rejected")
	}
	for _, root := range []map[string]any{
		{},
		{"actions": []any{map[string]any{"action_name": "GetVolume", "provider_verified": true, "request_id": "req"}}},
		{"actions": []any{map[string]any{"action_name": "UpdateVolume", "provider_verified": false, "request_id": "req"}}},
		{"actions": []any{map[string]any{"action_name": "UpdateVolume", "provider_verified": true}}},
		{"actions": []any{map[string]any{"action_name": "UpdateVolume", "provider_verified": true, "request_id": "req", "code": "Forbidden"}}},
	} {
		if volumeTransferAccepted(root) {
			t.Errorf("unproven UpdateVolume action was accepted: %+v", root)
		}
	}
}

func TestReserveVolumeTransferRequiresOwnedBinding(t *testing.T) {
	cl := &cleanup{}
	_, err := reserveVolumeTransfer(context.Background(), "ecctl sandbox volume update vol-1 --team-id team-2", execpkg.Config{Region: "cn-hangzhou", Bin: "/bin/ecctl"}, cl, scenario.Step{}, time.Second, "case")
	if err == nil {
		t.Fatal("transfer without an owned creation binding was accepted")
	}
	if _, err := reserveVolumeTransfer(context.Background(), "ecctl sandbox volume update vol-1", execpkg.Config{}, cl, scenario.Step{}, time.Second, "case"); err == nil {
		t.Fatal("transfer without a destination Team was accepted")
	}
	if _, err := reserveVolumeTransfer(context.Background(), "ecctl sandbox volume update vol-1 --team-id team-2", execpkg.Config{}, cl, scenario.Step{Teardown: "ecctl sandbox volume delete vol-1"}, time.Second, "case"); err == nil {
		t.Fatal("transfer registering a second finalizer was accepted")
	}
}

func TestRewriteVolumeIntentTeamRoot(t *testing.T) {
	dir := t.TempDir()
	journal := filepath.Join(dir, "cleanup.json")
	intent := journalfile.TemplateCreateIntent{
		Version: 2, Resource: "volume", State: "journaled", Name: "ecctl-p-0123456789abcdef01234567",
		RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou",
		Surface: "public", EcctlBin: "/bin/ecctl", Profile: "owner", TeamID: "team-source", AcceptedVolumeID: "vol-owned",
	}
	body, err := json.MarshalIndent(intent, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(journal) + ".template-create-0123456789abcdef01234567.json"
	if err := os.WriteFile(filepath.Join(dir, name), append(body, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	reanchored := intent
	reanchored.TeamID = "team-destination"
	if err := rewriteVolumeIntentTeamRoot(root, journal, reanchored); err != nil {
		t.Fatalf("rewrite failed: %v", err)
	}
	installed, err := root.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var retained journalfile.TemplateCreateIntent
	if err := json.Unmarshal(installed, &retained); err != nil {
		t.Fatal(err)
	}
	if retained.TeamID != "team-destination" || retained.AcceptedVolumeID != "vol-owned" || retained.Name != intent.Name {
		t.Fatalf("rewrite changed retained fields: %+v", retained)
	}
	mismatch := reanchored
	mismatch.AcceptedVolumeID = "vol-other"
	if err := rewriteVolumeIntentTeamRoot(root, journal, mismatch); err == nil {
		t.Fatal("rewrite accepted a mismatched creation binding")
	}
}
