package journalcmd

import "testing"

func TestRawVolumeMutationCandidate(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    bool
	}{
		{`ecctl call fcsandbox CreateVolume --request '{"volumeName":"x"}'`, true},
		{`ecctl --region=cn-hangzhou call --output json FCSandbox CreateVolume --request '{}'`, true},
		{`ecctl call --profile account-a fc-sandbox --request '{}' updatevolume`, true},
		{`ecctl --region cn-hangzhou call sbx DeleteVolume --request '{}'`, true},
		{`ecctl call ' FCSandbox ' ' CreateVolume ' --request '{}'`, true},
		{`ecctl call FCSandbox 'UpdateVolume ' --request '{}'`, true},
		{`ecctl call 'sbx ' ' DeleteVolume' --request '{}'`, true},
		{`ecctl --unknown value call fcsandbox --future value CreateVolume`, true},
		{`ecctl --unknown call fcsandbox CreateVolume`, true},
		{`ecctl call --request '{}' -- FCSandbox CreateVolume`, true},
		{`ecctl call --api-param name=owned -- FCSandbox CreateVolume`, true},
		{`ecctl call --request '{}' -- FCSandbox UpdateVolume`, true},
		{`ecctl call --api-param teamID=team-other -- FCSandbox UpdateVolume`, true},
		{`ecctl call --request '{"volumeID":"vol-other"}' -- FCSandbox DeleteVolume`, true},
		{`ecctl call --api-param volumeID=vol-other -- FCSandbox DeleteVolume`, true},
		{`ecctl call --request='{"volumeID":"vol-other"}' -- FCSandbox DeleteVolume`, true},
		{`ecctl --lang call call --api-param teamID=team-other -- FCSandbox DeleteVolume`, true},
		{`ecctl call --schema FCSandbox CreateVolume --schema=false --request '{}'`, true},
		{`ecctl call --list FCSandbox UpdateVolume --list=false --request '{}'`, true},
		{`ecctl call --generate-request FCSandbox DeleteVolume --generate-request=false --request '{}'`, true},
		{`ecctl call fcsandbox CreateVolume --request '{broken`, true},
		{`ecctl call fcsandbox GetVolume --request '{"action":"CreateVolume"}'`, false},
		{`ecctl call fcsandbox GetVolume --request '{"operation":" DeleteVolume "}'`, false},
		{`ecctl call ecs DescribeInstances --Tag.1.Key sandbox --Tag.1.Value CreateVolume`, false},
		{`ecctl --region cn-hangzhou call ecs DescribeInstances --Tag.1.Key sbx --Tag.1.Value DeleteVolume`, false},
		{`ecctl call --request '{"Product":"FCSandbox","Action":"DeleteVolume"}' -- ecs DescribeInstances`, false},
		{`ecctl call --api-param Tag.1.Key=FCSandbox --api-param Tag.1.Value=CreateVolume -- ecs DescribeInstances`, false},
		{`ecctl call --api-param=Tag.1.Value=CreateVolume -- ecs DescribeInstances`, false},
		{`ecctl call ecs DescribeInstances --Tag.1.Key sandbox --Tag.1.Value CreateVolume -- FCSandbox DeleteVolume`, false},
		{`ecctl --lang sandbox call fcsandbox DeleteVolume --request '{}' -h=false`, true},
		{`ecctl --lang call ecs instance get --name CreateVolume -h=false`, false},
		{`ecctl call fcsandbox DescribeVolume`, false},
		{`ecctl call ecs CreateVolume`, false},
		{`ecctl call sts GetCallerIdentity`, false},
	} {
		if got := RawVolumeMutationCandidate(tc.command); got != tc.want {
			t.Errorf("RawVolumeMutationCandidate(%q) = %t, want %t", tc.command, got, tc.want)
		}
	}
}

func TestVolumeMutationAction(t *testing.T) {
	for _, tc := range []struct {
		command, action string
		wantErr         bool
	}{
		{command: `ecctl sandbox volume create --name x`, action: "create"},
		{command: `ecctl sandbox volume --region=cn-hangzhou create --name x`, action: "create"},
		{command: `ecctl sbx --region=cn-hangzhou volume create --name x`, action: "create"},
		{command: `ecctl --region cn-hangzhou sbx --output=json volume new --name x`, action: "new"},
		{command: `ecctl sbx volume --region cn-hangzhou update vol-1 --status=AVAILABLE`, wantErr: true},
		{command: `ecctl sandbox volume --profile account-a rm vol-1`, action: "rm"},
		{command: `ecctl --lang sandbox sandbox volume update vol-1 -h=false`, wantErr: true},
		{command: `ecctl --profile sandbox sandbox volume delete vol-1 -h=false`, action: "delete"},
		{command: `ecctl --lang sandbox sandbox volume create --name x -h=false`, action: "create"},
		{command: `ecctl --lang sandbox volume update vol-1 -h=false`},
		{command: `ecctl --lang sandbox sandbox volume update vol-1 --future=x`, wantErr: true},
		{command: `ecctl --profile sandbox sandbox volume delete vol-1 -o json`, wantErr: true},
		{command: `ecctl sandbox volume get vol-1 --team-id team-1`},
		{command: `ecctl sandbox volume list --limit 1 --next-token token --all`},
		{command: `ecctl call fcsandbox GetVolume`},
		{command: `ecctl call ecs DescribeInstances --Tag.1.Key sandbox --Tag.1.Value volume`},
		{command: `ecctl call ecs DescribeInstances --Tag.1.Key sandbox --Tag.1.Value volume --request 'broken`, wantErr: false},
		{command: `ecctl ecs instance create --name sandbox`},
		{command: `ecctl sandbox volume --future=x create --name x`, wantErr: true},
		{command: `ecctl --future value sandbox volume delete vol-1`, wantErr: true},
		{command: `ecctl sandbox --future value volume delete vol-1`, wantErr: true},
		{command: `ecctl sandbox volume create --name x -o json`, wantErr: true},
		{command: `ecctl sandbox -o json volume create --name x`, wantErr: true},
		{command: `ecctl sbx volume update vol-1 --status=AVAILABLE -o json`, wantErr: true},
		{command: `ecctl sandbox -p owner volume create --name x`, wantErr: true},
		{command: `ecctl sandbox volume --region create`, wantErr: true},
		{command: `ecctl sandbox volume create --name 'broken`, wantErr: true},
	} {
		got, err := VolumeMutationAction(tc.command)
		if got != tc.action || (err != nil) != tc.wantErr {
			t.Errorf("VolumeMutationAction(%q) = %q, %v; want %q, err=%t", tc.command, got, err, tc.action, tc.wantErr)
		}
	}
}

func TestVolumeCommandActionReadOnly(t *testing.T) {
	for _, command := range []string{
		`ecctl sandbox volume --region=cn-hangzhou get vol-1`,
		`ecctl --region cn-hangzhou sbx volume list --limit 1`,
	} {
		action, err := VolumeCommandAction(command)
		if err != nil || (action != "get" && action != "list") {
			t.Errorf("VolumeCommandAction(%q) = %q, %v", command, action, err)
		}
	}
}

func TestVolumeReadOnlyRejectsUnsupportedOutputShorthand(t *testing.T) {
	if action, err := VolumeCommandAction(`ecctl sandbox volume get vol-1 -o json`); err == nil || action != "" {
		t.Fatalf("unsupported Volume read shorthand was accepted: action=%q err=%v", action, err)
	}
}
