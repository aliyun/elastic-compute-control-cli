package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
)

func TestVolumeCaseTeamDeleteOwnershipWiring(t *testing.T) {
	for _, tc := range []struct {
		name      string
		command   string
		data      map[string]any
		retire    bool
		wantError string
	}{
		{name: "external Team", command: "ecctl sandbox team delete team-other --timeout 5m --output=json", data: map[string]any{}, wantError: "lacks an active case-owned creation"},
		{name: "cross-case creator", command: "ecctl sandbox team delete team-1 --timeout 5m --output=json", data: map[string]any{"run_name": "another-case"}, retire: true, wantError: "differs from its owned creator"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, cl, dir, scope := volumeRecoveryFixture(t)
			opt := Options{volumeLifecycle: true, Logf: func(string, ...any) {}}
			create := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
			if result, ok := runStep(context.Background(), opt, cfg, cl, scope, map[string]any{}, create, time.Second); !ok {
				t.Fatalf("Volume create failed: %+v", result)
			}
			if tc.retire {
				retire := scenario.Step{Name: "retire Volume", Run: "ecctl sandbox volume delete vol-owned --team-id team-1 --output=json"}
				if result, ok := runStep(context.Background(), opt, cfg, cl, scope, map[string]any{}, retire, time.Second); !ok {
					t.Fatalf("Volume retirement failed: %+v", result)
				}
			}
			deleteTeam := scenario.Step{Name: "Team delete", Run: tc.command}
			result, ok := runStep(context.Background(), opt, cfg, cl, scope, tc.data, deleteTeam, time.Second)
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if ok || !strings.Contains(result.Error, tc.wantError) || strings.Contains(string(calls), "team-delete") {
				t.Fatalf("Volume case Team delete escaped production ownership scope: ok=%v error=%q calls=%s", ok, result.Error, calls)
			}
		})
	}
}

func TestNonVolumeTeamDeleteDispatchUnchanged(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	deleteTeam := scenario.Step{Name: "Team delete", Run: "ecctl sandbox team delete team-1 --timeout 5m --output=json"}
	result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, deleteTeam, time.Second)
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if !ok || !strings.Contains(string(calls), "team-delete") {
		t.Fatalf("ordinary owned Team delete lost its production dispatch: ok=%v result=%+v calls=%s", ok, result, calls)
	}
}
