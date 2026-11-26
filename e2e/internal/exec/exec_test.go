package exec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunPreservesLargeJSONNumbers(t *testing.T) {
	script := filepath.Join(t.TempDir(), "print-json")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' '{\"owner_id\":1754580903499898}'\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	res := Run(context.Background(), Config{}, script)
	if res.Exit != 0 || res.Err != nil {
		t.Fatalf("Run exit=%d err=%v stderr=%s", res.Exit, res.Err, res.Stderr)
	}
	obj, ok := res.JSON.(map[string]any)
	if !ok {
		t.Fatalf("JSON = %T, want object", res.JSON)
	}
	if got := fmt.Sprint(obj["owner_id"]); got != "1754580903499898" {
		t.Fatalf("owner_id = %q, want exact integer text", got)
	}
}

func TestProtectedRawFixturePinsJSONWithTextDefault(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	bin := filepath.Join(t.TempDir(), "ecctl")
	script := "#!/bin/sh\ncase \" $* \" in *' --output=json '*) printf '%s' '{\"ok\":true}';; *) printf '%s' 'text output';; esac\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	pin := IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user"}
	cfg := Config{Bin: bin, Region: "cn-hangzhou", Profile: "fixture", ExpectedIdentity: &pin}
	result := Run(context.Background(), cfg, `ecctl call oss GetBucketInfo --request '{"Bucket":"owned"}' --region cn-hangzhou`)
	if result.Err != nil || result.Exit != 0 || result.JSON == nil || !strings.Contains(result.Command, "--output=json") {
		t.Fatalf("protected fixture did not pin JSON: %+v", result)
	}
	changed := Run(context.Background(), cfg, `ecctl call ram GetRole --request '{"RoleName":"owned"}' --output=text`)
	if changed.Err == nil || changed.Started {
		t.Fatalf("protected fixture accepted text override: %+v", changed)
	}
	hidden := Run(context.Background(), cfg, `ecctl call ram GetRole --request '{"RoleName":"owned"}' -- --output=text`)
	if hidden.Err == nil || hidden.Started {
		t.Fatalf("protected fixture accepted an option separator: %+v", hidden)
	}
}

func TestActualProfileOptionConsumesStringValues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		value string
		count int
		bad   bool
	}{
		{"flag-shaped language value", []string{"sandbox", "team", "create", "--lang", "--profile=prod"}, "", 0, false},
		{"real split profile after string value", []string{"sandbox", "team", "create", "--lang", "--profile=other", "--profile", "prod"}, "prod", 1, false},
		{"real inline profile", []string{"sandbox", "team", "create", "--profile=prod"}, "prod", 1, false},
		{"owned Volume config", []string{"--output=json", "--agent-envelope=false", "--region", "cn-hangzhou", "--profile", "prod", "sandbox", "volume", "create", "--mount-config={}", "--no-wait=true", "--oss-volume-config={}", "--agentic-fs-volume-config={}", "--agentic-bucket-volume-config={}", "--team-id=team-1"}, "prod", 1, false},
		{"conflicting duplicate", []string{"sandbox", "team", "create", "--profile", "prod", "--profile=other"}, "other", 2, false},
		{"separator", []string{"sandbox", "team", "create", "--", "--profile=prod"}, "", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value, count, err := actualProfileOption(tc.args)
			if value != tc.value || count != tc.count || (err != nil) != tc.bad {
				t.Fatalf("profile option = %q, %d, %v", value, count, err)
			}
		})
	}
}

func TestRunPinnedProfileSurvivesFlagShapedStringValue(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	bin := filepath.Join(t.TempDir(), "ecctl")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s' \"$*\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	result := Run(context.Background(), Config{Bin: bin, Profile: "prod"}, "ecctl sandbox team create --lang --profile=prod")
	if result.Err != nil || result.Exit != 0 || result.Stdout != "sandbox team create --lang --profile=prod --profile prod" {
		t.Fatalf("pinned profile was not a real option: %+v", result)
	}
	for _, command := range []string{"ecctl sandbox team create --profile other", "ecctl sandbox team create --profile prod --profile=prod", "ecctl sandbox team create -- --profile prod"} {
		result := Run(context.Background(), Config{Bin: bin, Profile: "prod"}, command)
		if result.Err == nil || result.Started {
			t.Fatalf("unsafe profile layout reached subprocess: %+v", result)
		}
	}
}

func TestIssue128ProtectedMachineArgv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses /bin/sh")
	}
	t.Setenv("ECCTL_DISPLAY_MODE", "Human")
	bin := filepath.Join(t.TempDir(), "ecctl")
	const script = `#!/bin/sh
[ "$ECCTL_DISPLAY_MODE" = Human ] && [ "$NO_COLOR" = 1 ] || exit 81
for flag in "$@"; do
    if [ "$flag" = --no-color=true ]; then
        printf '%s\n' '{"ok":true}'
        exit 0
    fi
done
printf '\033[32m%s\033[0m\n' '{"ok":true}'
`
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	pin := IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user"}
	cfg := Config{Bin: bin, Region: "cn-hangzhou", Profile: "sandbox", ExpectedIdentity: &pin}
	for _, tc := range []struct{ name, command string }{
		{"template-list", "ecctl sandbox template list --openapi --team-id team-1 --output=json --agent-envelope=false"},
		{"template-get", "ecctl sandbox template get tpl-1 --openapi --team-id team-1 --output=json --agent-envelope=false"},
		{"template-delete", "ecctl sandbox template delete tpl-1 --openapi --team-id team-1 --output=json --agent-envelope=false"},
		{"team-prefixed", "ecctl --output=json --agent-envelope=false --region cn-hangzhou sandbox team create --name owned"},
		{"team-list-prefixed", "ecctl --output=json --agent-envelope=false --region cn-hangzhou sandbox team list --filter name=owned --all"},
		{"team-interspersed", "ecctl --profile sandbox sbx --output json team --lang=en create --name owned"},
		{"template-interspersed", "ecctl --output json sandbox --region=cn-hangzhou tpl --json create --openapi=true --name owned --team-id team-1"},
		{"volume-interspersed", "ecctl sbx --profile sandbox volume --output=json create --name owned --team-id team-1"},
		{"explicit-true", "ecctl --output=json sandbox --lang en volume create --team-id team-1 --name owned --no-color=true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Run(context.Background(), cfg, tc.command)
			if got.Err != nil || !got.Started || got.Exit != 0 {
				t.Fatalf("fixture did not produce an exit-zero response: %+v", got)
			}
			count := 0
			for _, arg := range got.Argv {
				if arg == "--no-color=true" {
					count++
				}
			}
			value, _ := got.JSON.(map[string]any)
			if count != 1 || value["ok"] != true {
				t.Fatalf("protected argv did not produce machine JSON: no-color=%d argv=%q stdout=%q JSON=%#v", count, got.Argv, got.Stdout, got.JSON)
			}
		})
	}
	for _, selector := range []string{"--no-color=false", "--no-color=true --no-color=true"} {
		got := Run(context.Background(), cfg, "ecctl --output=json sandbox --lang en volume create --team-id team-1 --name owned "+selector)
		if got.Started || got.Err == nil {
			t.Errorf("conflicting selector %q reached the subprocess: %+v", selector, got)
		}
	}
}

func TestProtectedTeamNoColorBoolean(t *testing.T) {
	pin := IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user"}
	cfg := Config{Region: "cn-hangzhou", Profile: "fixture", ExpectedIdentity: &pin}
	const teamArgs = "--output=json --agent-envelope=false --region cn-hangzhou sandbox --lang=en team create --name owned"

	for _, selector := range []string{
		"--no-color", "--no-color=true", "--no-color=1", "--no-color=t", "--no-color=T", "--no-color=TRUE", "--no-color=True",
	} {
		t.Run(selector, func(t *testing.T) {
			_, argv, _, err := prepareCommandArgs(cfg, "ecctl "+teamArgs+" "+selector)
			want := teamArgs + " " + selector + " --profile fixture"
			if err != nil || strings.Join(argv, " ") != want {
				t.Fatalf("prepared argv = %q, err = %v, want %q", argv, err, want)
			}
		})
	}

	for _, selector := range []string{
		"--no-color=false", "--no-color=0", "--no-color=F",
		"--no-color=", "--no-color=invalid",
		"--no-color=1 --no-color=TRUE", "--no-color --no-color=0",
	} {
		t.Run("reject "+selector, func(t *testing.T) {
			if _, argv, _, err := prepareCommandArgs(cfg, "ecctl "+teamArgs+" "+selector); err == nil {
				t.Fatalf("invalid color selector reached prepared argv: %q", argv)
			}
		})
	}
}

func TestMachineProofArgvBoundaries(t *testing.T) {
	pin := IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user"}
	pinned := Config{ExpectedIdentity: &pin}

	for _, tc := range []struct {
		name     string
		args     string
		want     string
		unpinned bool
	}{
		{
			name: "root string values are not command words",
			args: "--profile sandbox sbx --region cn-hangzhou volume --lang template get vol-1 --output json",
			want: "--profile sandbox sbx --region cn-hangzhou volume --lang template get vol-1 --output json --no-color=true",
		},
		{
			name: "option value cannot supply a product",
			args: "--profile sandbox volume get vol-1",
		},
		{
			name: "option value cannot supply the color selector",
			args: "--lang --no-color=false sandbox volume list",
			want: "--lang --no-color=false sandbox volume list --no-color=true",
		},
		{
			name: "authored bare true selector is retained",
			args: "--no-color sbx --json team list",
		},
		{
			name: "native list alias with bare transport selector",
			args: "--json sbx --agent-envelope=false tpl ls --openapi",
			want: "--json sbx --agent-envelope=false tpl ls --openapi --no-color=true",
		},
		{
			name: "native delete alias with inline transport selector before action",
			args: "sandbox --lang=en tpl --openapi=1 rm tpl-1",
			want: "sandbox --lang=en tpl --openapi=1 rm tpl-1 --no-color=true",
		},
		{
			name: "last native selector is false",
			args: "sandbox tpl show tpl-1 --openapi --openapi=false",
		},
		{
			name: "invalid transport is left to the CLI",
			args: "sandbox template list --openapi=invalid",
		},
		{
			name: "absent native selector stays on E2B route",
			args: "--output=json sandbox template ls",
		},
		{
			name: "flag-shaped string value cannot select native transport",
			args: "--lang --openapi=true sbx template get tpl-1",
		},
		{
			name: "native transport alone does not select a proof operation",
			args: "sandbox template build-status tpl-1 --openapi",
		},
		{
			name:     "unbound native read stays outside the proof",
			args:     "sandbox template list --openapi",
			unpinned: true,
		},
		{
			name: "unknown prefix stays with the CLI parser",
			args: "--future=sandbox sandbox team list",
		},
		{
			name: "command words after terminator are not a proof",
			args: "--json -- sbx team list",
		},
		{
			name: "transport after terminator is not an option",
			args: "sandbox tpl get tpl-1 -- --openapi",
		},
		{
			name: "insert before the real terminator after consuming a terminator-shaped value",
			args: "--lang -- sbx volume list -- --no-color=false",
			want: "--lang -- sbx volume list --no-color=true -- --no-color=false",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := pinned
			if tc.unpinned {
				cfg = Config{}
			}
			want := tc.want
			if want == "" {
				want = tc.args
			}
			_, args, _, err := prepareCommandArgs(cfg, "ecctl "+tc.args)
			if err != nil || strings.Join(args, " ") != want {
				t.Fatalf("prepared argv = %q, err = %v, want %q", args, err, want)
			}
		})
	}

	pinned.Profile = "fixture"
	for _, args := range []string{
		"--output=json sandbox template list --openapi --lang",
		"--future=sandbox sbx volume list",
		"--json sbx template list --openapi -- --profile fixture",
	} {
		if _, argv, _, err := prepareCommandArgs(pinned, "ecctl "+args); err == nil {
			t.Errorf("invalid pinned argv was prepared: %q", argv)
		}
	}
}

func TestRunDefaultRegionPreservesRawCallSelection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	bin := filepath.Join(t.TempDir(), "ecctl")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s' \"$*\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ command, want string }{
		{"ecctl call ecs DescribeInstances --PageSize 10 --region cn-shanghai", "call ecs DescribeInstances --PageSize 10 --region cn-shanghai"},
		{"ecctl call oss ListObjects --Bucket demo --region=cn-shanghai", "call oss ListObjects --Bucket demo --region=cn-shanghai"},
		{"ecctl call ecs DescribeInstances --FutureParam value --region cn-shanghai", "call ecs DescribeInstances --FutureParam value --region cn-shanghai"},
		{"ecctl call ecs DescribeInstances --FutureBool --region=cn-shanghai", "call ecs DescribeInstances --FutureBool --region=cn-shanghai"},
		{"ecctl call ecs DescribeInstances --PageSize 10", "call ecs DescribeInstances --PageSize 10 --region cn-beijing"},
		{"ecctl call ecs DescribeInstances --lang --region=cn-shanghai", "call ecs DescribeInstances --lang --region=cn-shanghai --region cn-beijing"},
		{"ecctl sandbox quota list --next-token '--' --output=json", "sandbox quota list --next-token -- --output=json --region cn-beijing"},
		{"ecctl sandbox team list --lang '--' --output=json", "sandbox team list --lang -- --output=json --region cn-beijing"},
		{"ecctl sandbox quota list --next-token=-- --output=json", "sandbox quota list --next-token=-- --output=json --region cn-beijing"},
		{"ecctl sandbox quota list --next-token '--' -- tail", "sandbox quota list --next-token -- --region cn-beijing -- tail"},
		{"ecctl call ecs DescribeInstances --PageSize 10 -- tail", "call ecs DescribeInstances --PageSize 10 --region cn-beijing -- tail"},
	} {
		t.Run(tc.command, func(t *testing.T) {
			result := Run(context.Background(), Config{Bin: bin, Region: "cn-beijing"}, tc.command)
			if result.Err != nil || result.Exit != 0 || result.Stdout != tc.want {
				t.Fatalf("region argv changed: got=%+v want=%q", result, tc.want)
			}
		})
	}
	for _, command := range []string{"ecctl sandbox quota list --next-token"} {
		result := Run(context.Background(), Config{Bin: bin, Region: "cn-beijing"}, command)
		if result.Err == nil || result.Started {
			t.Fatalf("invalid region option layout reached subprocess: %+v", result)
		}
	}
}
