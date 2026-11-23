package caselint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActualVolumeLifecycleCaseLints(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "cases", "sandbox", "volume-lifecycle.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cases := filepath.Join(root, "cases")
	inputs := filepath.Join(root, "inputs")
	mustMkdir(t, filepath.Join(cases, "sandbox"))
	mustMkdir(t, inputs)
	path := filepath.Join(cases, "sandbox", "volume-lifecycle.yaml")
	mustWrite(t, path, string(body))
	stack := filepath.Join("..", "..", "fixtures", "stack.yaml")
	policy := filepath.Join("..", "..", "fixtures", "parameter-policy.yaml")
	rep, err := Check(Options{CasesDir: cases, InputsDir: inputs, StackFile: stack, ParameterPolicy: policy})
	if err != nil || rep.Invalid != 0 {
		t.Fatalf("actual Volume lifecycle case did not lint: report=%+v err=%v", rep, err)
	}
	for _, tc := range []struct {
		name, old, replacement, message string
	}{
		{"unknown Volume variable", "{{.volume_name}}", "{{.unknown_volume_name}}", `expect references "unknown_volume_name"`},
		{"unknown Team teardown variable", "{{.team_id}}", "{{.unknown_team_id}}", `teardown references "unknown_team_id"`},
		{"Team ID in creator run", "{{.team_name}}", "{{.team_id}}", `run references "team_id"`},
		{"Team ID in creator expect", "status: {eq: active}", `status: {eq: "{{.team_id}}"}`, `expect references "team_id"`},
		{"Team ID in later step", "ecctl sandbox volume get {{.volume_id}}", "ecctl sandbox volume get {{.team_id}}", `run references "team_id"`},
		{"Team ID in unrelated teardown", "ecctl sandbox volume delete {{.volume_id}}", "ecctl sandbox volume delete {{.team_id}}", `teardown references "team_id"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mustWrite(t, path, strings.Replace(string(body), tc.old, tc.replacement, 1))
			rep, err := Check(Options{CasesDir: cases, InputsDir: inputs, StackFile: stack, ParameterPolicy: policy})
			if err != nil {
				t.Fatal(err)
			}
			for _, issue := range rep.Errors {
				if issue.Code == "undefined_var" && strings.Contains(issue.Message, tc.message) {
					return
				}
			}
			t.Fatalf("out-of-scope Team ID or unknown variable was accepted: report=%+v", rep)
		})
	}
}

func TestInterspersedVolumeCreateLintsWithOwnedName(t *testing.T) {
	root := t.TempDir()
	cases := filepath.Join(root, "cases")
	inputs := filepath.Join(root, "inputs")
	mustMkdir(t, filepath.Join(cases, "sandbox"))
	mustMkdir(t, inputs)
	mustWrite(t, filepath.Join(cases, "sandbox", "volume-lifecycle.yaml"), `
resource: sandbox/volume
steps:
  - name: create
    run: ecctl --region cn-hangzhou sbx volume new --name authored --team-id team-1 --output=json
    expect:
      name: {eq: "{{.volume_name}}"}
    teardown: ecctl sandbox volume delete vol-1 --output=json
`)
	rep, err := Check(Options{CasesDir: cases, InputsDir: inputs})
	if err != nil || rep.Invalid != 0 {
		t.Fatalf("interspersed Volume create did not lint: report=%+v err=%v", rep, err)
	}
}

func TestVolumeCaseLintRejectsUnsupportedOutputShorthand(t *testing.T) {
	for _, command := range []string{
		"ecctl sandbox volume create --name x --team-id team-1 -o json",
		"ecctl sandbox -o json volume create --name x --team-id team-1",
		"ecctl sandbox volume get vol-1 -o json",
	} {
		root := t.TempDir()
		cases, inputs := filepath.Join(root, "cases"), filepath.Join(root, "inputs")
		mustMkdir(t, filepath.Join(cases, "sandbox"))
		mustMkdir(t, inputs)
		body := "resource: sandbox/volume\nsteps:\n  - name: output alias\n    run: " + command + "\n"
		if strings.Contains(command, "create") {
			body += "    teardown: ecctl sandbox volume delete vol-1 --output=json\n"
		}
		mustWrite(t, filepath.Join(cases, "sandbox", "volume-lifecycle.yaml"), body)
		rep, err := Check(Options{CasesDir: cases, InputsDir: inputs})
		if err != nil || !hasLintCode(rep, "invalid_command_shape") {
			t.Errorf("unsupported Volume output alias passed case lint: %s: report=%+v err=%v", command, rep, err)
		}
	}
}

func TestVolumeRawMutationCaseLintRejectsCreate(t *testing.T) {
	for _, command := range []string{
		"ecctl call fcsandbox CreateVolume --request '{}'",
		"ecctl --region cn-hangzhou call FCSandbox CreateVolume --request '{}'",
		"ecctl call sbx UpdateVolume --request '{}'",
		"ecctl call fc-sandbox DeleteVolume --request '{}'",
		"ecctl call ' FCSandbox ' ' CreateVolume ' --request '{}'",
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
		_, cases, inputs, _ := writeLintFixture(t, "resource: sandbox/volume\nsteps:\n  - name: raw volume\n    run: "+command+"\n", "")
		rep, err := Check(Options{CasesDir: cases, InputsDir: inputs})
		if err != nil || !hasLintCode(rep, "unsupported_raw_volume_mutation") {
			t.Errorf("raw Volume case was admitted: %s: report=%+v err=%v", command, rep, err)
		}
	}
}

func TestVolumeRawMutationCaseLintIgnoresLaterOpenAPIValues(t *testing.T) {
	for _, command := range []string{
		"ecctl call ecs DescribeInstances --Tag.1.Key sandbox --Tag.1.Value CreateVolume",
		`ecctl call --api-param Tag.1.Key=FCSandbox --api-param Tag.1.Value=CreateVolume -- ecs DescribeInstances`,
		"ecctl call ecs DescribeInstances --Tag.1.Key sandbox --Tag.1.Value volume",
	} {
		_, cases, inputs, _ := writeLintFixture(t, "resource: sandbox/volume\nsteps:\n  - name: raw read\n    run: "+command+"\n", "")
		rep, err := Check(Options{CasesDir: cases, InputsDir: inputs})
		if err != nil || rep.Invalid != 0 {
			t.Fatalf("read-only OpenAPI argument was rejected: report=%+v err=%v", rep, err)
		}
	}
}

func TestVolumeSharedStackMutationLintRejectsWrites(t *testing.T) {
	for _, command := range []string{
		"ecctl sandbox volume create --name x --team-id team-1",
		"ecctl sbx volume --region=cn-hangzhou new --name x --team-id team-1",
		"ecctl sandbox volume update vol-1 --status=AVAILABLE",
		"ecctl sandbox volume delete vol-1",
		"ecctl call fcsandbox CreateVolume --request '{}'",
		"ecctl --region cn-hangzhou call FCSandbox DeleteVolume --request '{}'",
		"ecctl call ' sbx ' ' DeleteVolume ' --request '{}'",
		"ecctl call --request '{}' -- FCSandbox CreateVolume",
		"ecctl call --api-param teamID=team-other -- FCSandbox UpdateVolume",
		"ecctl call --api-param volumeID=vol-other -- FCSandbox DeleteVolume",
		"ecctl call --schema FCSandbox CreateVolume --schema=false --request '{}'",
		"ecctl call --list FCSandbox UpdateVolume --list=false --request '{}'",
		"ecctl call --generate-request FCSandbox DeleteVolume --generate-request=false --request '{}'",
	} {
		root, cases, inputs, coverage := writeLintFixture(t, validCaseYAML(), validCoverageYAML("cases/ecs/instance.yaml"))
		stack := filepath.Join(root, "stack.yaml")
		mustWrite(t, stack, "provision:\n  - id: volume\n    resource: sandbox/volume\n    run: "+command+"\n")
		rep, err := Check(Options{CasesDir: cases, InputsDir: inputs, CoveragePath: coverage, StackFile: stack})
		if err != nil || !hasLintCode(rep, "unsupported_volume_stack_mutation") {
			t.Errorf("shared-stack Volume write was admitted: %s: report=%+v err=%v", command, rep, err)
		}
	}
}

func TestVolumeSharedStackTeardownLintRejectsWrites(t *testing.T) {
	for _, teardown := range []string{
		"ecctl sandbox volume delete vol-1",
		"ecctl call fcsandbox DeleteVolume --request '{}'",
		`ecctl call --request '{"volumeID":"vol-other"}' -- FCSandbox DeleteVolume`,
	} {
		root, cases, inputs, coverage := writeLintFixture(t, validCaseYAML(), validCoverageYAML("cases/ecs/instance.yaml"))
		stack := filepath.Join(root, "stack.yaml")
		mustWrite(t, stack, "provision:\n  - id: volume\n    resource: sandbox/volume\n    run: ecctl sandbox volume list --output=json\n    teardown: "+teardown+"\n")
		rep, err := Check(Options{CasesDir: cases, InputsDir: inputs, CoveragePath: coverage, StackFile: stack})
		if err != nil || !hasLintCode(rep, "unsupported_volume_stack_mutation") {
			t.Errorf("shared-stack Volume teardown was admitted: %s: report=%+v err=%v", teardown, rep, err)
		}
	}
}

func TestVolumeSharedStackLintAcceptsUnrelatedRawRead(t *testing.T) {
	root, cases, inputs, coverage := writeLintFixture(t, validCaseYAML(), validCoverageYAML("cases/ecs/instance.yaml"))
	stack := filepath.Join(root, "stack.yaml")
	mustWrite(t, stack, "provision:\n  - id: ecs_read\n    resource: ecs/instance\n    mode: lookup\n    run: ecctl call ecs DescribeInstances --Tag.1.Key sandbox --Tag.1.Value volume\n")
	rep, err := Check(Options{CasesDir: cases, InputsDir: inputs, CoveragePath: coverage, StackFile: stack})
	if err != nil || rep.Invalid != 0 {
		t.Fatalf("unrelated raw read was rejected by shared stack lint: report=%+v err=%v", rep, err)
	}
}
