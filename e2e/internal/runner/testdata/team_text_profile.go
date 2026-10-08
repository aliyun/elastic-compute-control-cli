// This fixture runs the real public CLI with an offline, file-backed Team caller.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aliyun/elastic-compute-control-cli/pkg/cli"
	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
	_ "github.com/aliyun/elastic-compute-control-cli/specs/sandbox"
)

type fakeTeamCaller struct {
	dir, region string
	created     bool
}

func (f *fakeTeamCaller) Call(_ context.Context, operation string, params map[string]any) (map[string]any, error) {
	log, err := os.OpenFile(filepath.Join(f.dir, "calls"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	_, err = fmt.Fprintln(log, operation, f.region)
	_ = log.Close()
	if err != nil {
		return nil, err
	}
	file := filepath.Join(f.dir, "team.json")
	body, readErr := os.ReadFile(file)
	var team map[string]any
	if readErr == nil {
		if err := json.Unmarshal(body, &team); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(readErr) {
		return nil, readErr
	}
	response := map[string]any{"code": "200", "requestId": "fake-" + operation}
	switch operation {
	case "CreateTeam":
		if team != nil {
			return nil, fmt.Errorf("CreateTeam replayed")
		}
		if journal := os.Getenv("FAKE_TEAM_JOURNAL"); journal != "" {
			intents, err := filepath.Glob(journal + ".team-create-*.json")
			if err != nil || len(intents) != 1 {
				return nil, fmt.Errorf("CreateTeam lacks durable intent")
			}
		}
		team = map[string]any{"teamID": "team-owned", "teamName": params["body.teamName"], "description": params["body.description"], "status": "active", "readOnly": false}
		body, err := json.Marshal(team)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(file, body, 0o600); err != nil {
			return nil, err
		}
		f.created = true
		response["team"] = team
	case "ListTeams":
		teams := []any{}
		if team != nil {
			teams = append(teams, team)
		}
		response["teams"], response["total"] = teams, len(teams)
		response["pageNumber"], response["pageSize"] = 1, 50
	case "GetTeam":
		if team == nil {
			return nil, ecerrors.NotFound("NotFound", "Team not found", ecerrors.WithRawCause("TeamNotFound", "team not found"))
		}
		response["team"] = team
	case "DeleteTeam":
		journal, err := os.ReadFile(os.Getenv("FAKE_TEAM_JOURNAL"))
		if err != nil || !strings.Contains(string(journal), "ecctl sandbox team delete team-owned --timeout 5m") || params["teamID"] != "team-owned" {
			return nil, fmt.Errorf("delete lacks strict registered finalizer")
		}
		if err := os.Remove(file); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unexpected fake Team operation %s", operation)
	}
	return response, nil
}

func main() {
	fake := &fakeTeamCaller{dir: os.Getenv("FAKE_TEAM_DIR")}
	ctx := cli.WithResourceCallerFactory(context.Background(), func(_, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "sandbox" || resource.Resource != "team" || region != "cn-hangzhou" {
			return nil, fmt.Errorf("unexpected CLI target %s/%s/%s", resource.Product, resource.Resource, region)
		}
		fake.region = region
		return fake, nil
	})
	var stdout, stderr bytes.Buffer
	code := cli.Run(ctx, os.Args[1:], &stdout, &stderr)
	if fake.created && code == 0 && os.Getenv("FAKE_TEAM_LOST_RESPONSE") == "1" {
		fmt.Fprintln(os.Stdout, `{"error":{"message":"connection reset by peer"}}`)
		os.Exit(1)
	}
	_, _ = os.Stdout.Write(stdout.Bytes())
	_, _ = os.Stderr.Write(stderr.Bytes())
	os.Exit(code)
}
