package spec_resource

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/cli"
	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/i18n"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestFCSandboxTeamAbsencePublicChineseOutput(t *testing.T) {
	for _, rawCode := range []string{"TeamNotFound", "404"} {
		for _, mode := range []string{"json", "text"} {
			t.Run(rawCode+"/"+mode, func(t *testing.T) {
				failure := ecerrors.NotFound("NotFound", i18n.NewLocalizer("en").Message("TeamNotFound"), ecerrors.WithRawCause(rawCode, "provider missing team"), ecerrors.WithRequestID("https://example.com/?[REDACTED]"))
				fake := &fakeSpecCaller{errors: []error{failure}}
				ctx := cli.WithResourceCallerFactory(context.Background(), func(_, _ string, r spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
					if r.Product != "sandbox" || r.Resource != "team" || region != "cn-hangzhou" {
						t.Fatalf("wrong public target: %+v %s", r, region)
					}
					return fake, nil
				})
				var stdout, stderr bytes.Buffer
				code := cli.Run(ctx, []string{"--lang", "zh-CN", "--output", mode, "sandbox", "team", "get", "team-1", "--region", "cn-hangzhou"}, &stdout, &stderr)
				if code != 4 || !strings.Contains(stdout.String(), "团队不存在") || strings.Contains(stdout.String(), "team 资源不存在") {
					t.Fatalf("public Chinese absence: exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
				}
				if len(fake.calls) != 1 || fake.calls[0].operation != "GetTeam" {
					t.Fatalf("public call path changed: %+v", fake.calls)
				}
				if mode == "json" {
					payload := decodeObject(t, stdout.String())
					errPayload := payload["error"].(map[string]any)
					actions := payload["actions"].([]any)
					if errPayload["kind"] != "not_found" || errPayload["code"] != "NotFound" || len(actions) != 1 {
						t.Fatalf("public absence category changed: %s", stdout.String())
					}
					action := actions[0].(map[string]any)
					if action["code"] != rawCode || action["message"] != "provider missing team" || action["request_id"] != "https://example.com/?[REDACTED]" {
						t.Fatalf("public provider actions changed: %s", stdout.String())
					}
				}
			})
		}
	}
}

func TestFCSandboxTeamCreateDoesNotReadBackOrLoseIdentity(t *testing.T) {
	t.Parallel()
	for _, product := range []string{"sandbox", "sbx"} {
		t.Run(product, func(t *testing.T) {
			fake := &fakeSpecCaller{
				responses: []map[string]any{teamResponse("created-team-1", "active")},
				errors:    []error{nil, ecerrors.Service("CloudAPIError", "dial tcp: readback unavailable", true)},
			}
			stdout, stderr, code := teamCaller(t, fake)(product, "team", "create", "--name", "dev", "--region", "cn-hangzhou")
			if code != 0 || len(fake.calls) != 1 || fake.calls[0].operation != "CreateTeam" {
				t.Fatalf("create introduced a failure-prone readback: %d %s %s %#v", code, stdout, stderr, fake.calls)
			}
			obj := decodeObject(t, stdout)
			team := obj["team"].(map[string]any)
			if team["id"] != "created-team-1" || team["status"] != "active" || team["read_only"] != false || team["allow_update_team_name"] != true {
				t.Fatalf("validated create resource was lost: %s", stdout)
			}
		})
	}
}

func TestFCSandboxTeamListAllPreservesFailingPagePublicActions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, rawCode, message string }{
		{"business", "409", "provider rejected page 2"},
		{"transport", "CloudAPIError", "dial tcp: page 2 unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failure := ecerrors.Service("CloudAPIError", tc.message, false, ecerrors.WithRawCause(tc.rawCode, tc.message), ecerrors.WithRequestID("req-page-2-failure"))
			fake := &fakeSpecCaller{
				responses: []map[string]any{{"code": "200", "requestId": "req-page-1", "teams": []any{teamResponse("team-1", "active")["team"]}, "total": 2}},
				errors:    []error{nil, failure},
			}
			stdout, stderr, code := teamCaller(t, fake)("sandbox", "team", "list", "--region", "cn-hangzhou", "--limit", "1", "--all")
			if code == 0 {
				t.Fatalf("page failure succeeded: %s %s", stdout, stderr)
			}
			actions, ok := decodeObject(t, stdout)["actions"].([]any)
			if !ok || len(actions) != 2 {
				t.Fatalf("failing page action missing: %s", stdout)
			}
			first, failed := actions[0].(map[string]any), actions[1].(map[string]any)
			if first["request_id"] != "req-page-1" || failed["request_id"] != "req-page-2-failure" || failed["code"] != tc.rawCode || failed["message"] != tc.message || failed["action_name"] != "ListTeams" {
				t.Fatalf("public provider evidence lost: %s", stdout)
			}
		})
	}
}

func teamCaller(t *testing.T, fake *fakeSpecCaller) func(...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "sandbox" || resource.APIProduct != "FCSandbox" || resource.Resource != "team" || region != "cn-hangzhou" {
			t.Fatalf("unexpected resource/region: %#v %s", resource, region)
		}
		return fake, nil
	})
}

func teamResponse(id, status string) map[string]any {
	return map[string]any{"code": "200", "message": "success", "requestId": "req-team", "team": map[string]any{
		"teamID": id, "teamName": "dev", "description": "development", "resourceGroupID": "rg-test", "plan": "eco",
		"status": status, "createdTime": "2026-10-08T02:00:00Z", "userID": "12345", "allowUpdateTeamName": true, "readOnly": false,
	}}
}

func TestFCSandboxTeamCreateAndGet(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{teamResponse("team-1", "active"), teamResponse("team-1", "active"), teamResponse("team-1", "active")}}
	run := teamCaller(t, fake)
	stdout, stderr, code := run("sandbox", "team", "create", "--region", "cn-hangzhou", "--name", "dev", "--description", "development", "--resource-group", "rg-test", "--plan", "eco")
	if code != 0 {
		t.Fatalf("create exit %d: %s %s", code, stdout, stderr)
	}
	if got := strings.Join(callNames(fake.calls), ","); got != "CreateTeam" {
		t.Fatalf("create calls = %s", got)
	}
	request := fake.calls[0].request
	if request["body.teamName"] != "dev" || request["body.description"] != "development" || request["body.resourceGroupID"] != "rg-test" || request["body.plan"] != "eco" {
		t.Fatalf("create mapping = %#v", fake.calls)
	}
	stdout, stderr, code = run("sandbox", "team", "get", "team-1", "--region", "cn-hangzhou")
	if code != 0 {
		t.Fatalf("get exit %d: %s %s", code, stdout, stderr)
	}
	team := decodeObject(t, stdout)["team"].(map[string]any)
	for key, want := range map[string]any{"id": "team-1", "name": "dev", "description": "development", "resource_group": "rg-test", "plan": "eco", "status": "active", "created_time": "2026-10-08T02:00:00Z", "user_id": "12345", "allow_update_team_name": true, "read_only": false} {
		if team[key] != want {
			t.Fatalf("team.%s = %#v, want %#v; %s", key, team[key], want, stdout)
		}
	}
}

func TestFCSandboxTeamListPagesAndAll(t *testing.T) {
	t.Parallel()
	page := func(id string, n int) map[string]any {
		return map[string]any{"code": "200", "requestId": "req-list", "teams": []any{teamResponse(id, "active")["team"]}, "pageNumber": n, "pageSize": 1, "total": 2}
	}
	fake := &fakeSpecCaller{responses: []map[string]any{page("team-1", 1), page("team-2", 2)}}
	stdout, stderr, code := teamCaller(t, fake)("sandbox", "team", "list", "--region", "cn-hangzhou", "--filter", "name=dev", "--filter", "resource-group=rg-test", "--filter", "plan=eco", "--limit", "1", "--all")
	if code != 0 {
		t.Fatalf("list all exit %d: %s %s", code, stdout, stderr)
	}
	items := decodeObject(t, stdout)["teams"].([]any)
	if len(items) != 2 || items[1].(map[string]any)["id"] != "team-2" || len(fake.calls) != 2 {
		t.Fatalf("incomplete aggregation: %s calls=%#v", stdout, fake.calls)
	}
	for i, call := range fake.calls {
		if call.request["pageNumber"] != i+1 || call.request["pageSize"] != 1 || call.request["teamName"] != "dev" || call.request["resourceGroupID"] != "rg-test" || call.request["plan"] != "eco" {
			t.Fatalf("list page %d request = %#v", i, call.request)
		}
	}
	if decodeObject(t, stdout)["pagination"].(map[string]any)["has_more"] != false {
		t.Fatalf("full list still advertises more: %s", stdout)
	}
	fake = &fakeSpecCaller{responses: []map[string]any{page("team-2", 2)}}
	stdout, stderr, code = teamCaller(t, fake)("sandbox", "team", "list", "--region", "cn-hangzhou", "--page", "2", "--limit", "1")
	if code != 0 || fake.calls[0].request["pageNumber"] != 2 || decodeObject(t, stdout)["total"] != float64(2) {
		t.Fatalf("single page failed: %d %s %s %#v", code, stdout, stderr, fake.calls)
	}
}

func TestFCSandboxTeamUpdateRestrictions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, field string
		value       any
		args        []string
		want        string
	}{
		{"read only", "readOnly", true, []string{"--description", "updated"}, "TeamReadOnly"},
		{"rename denied", "allowUpdateTeamName", false, []string{"--name", "renamed"}, "TeamRenameNotAllowed"},
		{"missing permission", "readOnly", nil, []string{"--description", "updated"}, "InvalidTeamResponse"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := teamResponse("team-1", "active")
			response["team"].(map[string]any)[tc.field] = tc.value
			fake := &fakeSpecCaller{responses: []map[string]any{response}}
			args := append([]string{"sandbox", "team", "update", "team-1", "--region", "cn-hangzhou"}, tc.args...)
			stdout, _, code := teamCaller(t, fake)(args...)
			if code == 0 || errorCode(t, stdout) != tc.want || len(fake.calls) != 1 || fake.calls[0].operation != "GetTeam" {
				t.Fatalf("restriction not enforced: %d %s %#v", code, stdout, fake.calls)
			}
		})
	}
	fake := &fakeSpecCaller{responses: []map[string]any{teamResponse("team-1", "active"), teamResponse("team-1", "active"), teamResponse("team-1", "active")}}
	stdout, stderr, code := teamCaller(t, fake)("sandbox", "team", "update", "team-1", "--region", "cn-hangzhou", "--description", "updated", "--resource-group", "rg-other", "--plan", "std")
	if code != 0 || strings.Join(callNames(fake.calls), ",") != "GetTeam,UpdateTeam,GetTeam" || fake.calls[1].request["body.description"] != "updated" || fake.calls[1].request["body.resourceGroupID"] != "rg-other" || fake.calls[1].request["body.plan"] != "std" || fake.calls[1].request["teamID"] != "team-1" {
		t.Fatalf("update failed: %d %s %s %#v", code, stdout, stderr, fake.calls)
	}
}

func TestFCSandboxTeamDeleteTruthfulCompletion(t *testing.T) {
	t.Parallel()
	ack := map[string]any{"code": "200", "message": "success", "requestId": "req-delete"}
	for _, tc := range []struct {
		name, state, want string
		noWait, absent    bool
	}{
		{"pending then absent", "deleting", "", false, true},
		{"failed", "delete_failed", "WaitFailed", false, false},
		{"still deleting", "deleting", "WaitTimeout", false, false},
		{"no wait", "deleting", "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeSpecCaller{responses: []map[string]any{teamResponse("team-1", "active"), ack, teamResponse("team-1", tc.state)}, responseWhenExhausted: teamResponse("team-1", tc.state)}
			if tc.absent {
				fake.errors = []error{nil, nil, nil, ecerrors.NotFound("NotFound", "team not found")}
			}
			args := []string{"sandbox", "team", "delete", "team-1", "--region", "cn-hangzhou", "--timeout", "20ms"}
			if tc.noWait {
				args = append(args, "--no-wait")
			}
			stdout, stderr, code := teamCaller(t, fake)(args...)
			obj := decodeObject(t, stdout)
			if tc.want != "" {
				if code == 0 || errorCode(t, stdout) != tc.want || obj["deleted"] == true {
					t.Fatalf("false deletion: %d %s %s", code, stdout, stderr)
				}
			} else if tc.noWait {
				if code != 0 || obj["deleted"] == true || obj["deletion_requested"] != true || len(fake.calls) != 2 {
					t.Fatalf("ack falsely completes: %d %s %#v", code, stdout, fake.calls)
				}
			} else if code != 0 || obj["deleted"] != true || len(fake.calls) != 4 {
				t.Fatalf("absent completion failed: %d %s %s %#v", code, stdout, stderr, fake.calls)
			}
		})
	}
	response := teamResponse("team-1", "active")
	response["team"].(map[string]any)["readOnly"] = true
	fake := &fakeSpecCaller{responses: []map[string]any{response}}
	stdout, _, code := teamCaller(t, fake)("sandbox", "team", "delete", "team-1", "--region", "cn-hangzhou")
	if code == 0 || errorCode(t, stdout) != "TeamReadOnly" || len(fake.calls) != 1 {
		t.Fatalf("read-only delete: %d %s %#v", code, stdout, fake.calls)
	}
}

func TestFCSandboxTeamInvalidInputDoesNotCallCloud(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"create"}, {"update", "team-1"}, {"list", "--limit", "51"}, {"list", "--limit", "0"}, {"list", "--page", "0"}, {"list", "--filter", "unknown=value"},
	} {
		fake := &fakeSpecCaller{}
		full := append([]string{"sandbox", "team", "--region", "cn-hangzhou"}, args...)
		stdout, _, code := teamCaller(t, fake)(full...)
		if code == 0 || len(fake.calls) != 0 {
			t.Fatalf("invalid input reached cloud: %v %d %s %#v", args, code, stdout, fake.calls)
		}
	}
}
