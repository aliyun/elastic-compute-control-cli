package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCLIConfigDefaultsDoNotAliasTheSourceObject(t *testing.T) {
	if len(cloneConfigObject(nil)) != 0 {
		t.Fatal("nil config gained entries")
	}
	config := map[string]any{"profiles": false}
	ensureConfigShape(config)
	upsertConfigProfile(config, map[string]any{"region_id": "cn-hangzhou"})
	profile, ok := configProfile(config, "")
	if !ok || profile["name"] != "default" || profile["region_id"] != "cn-hangzhou" || config["current"] != "default" {
		t.Fatal("default profile shape lost")
	}
	profile["region_id"] = "changed"
	again, _ := configProfile(config, "")
	if again["region_id"] != "cn-hangzhou" {
		t.Fatal("profile lookup aliased config")
	}
	for _, product := range []string{"unknown", "ecs"} {
		caller := &CLICommandCaller{Product: product}
		for _, body := range []any{nil, map[string]any{"name": "value"}} {
			got, err := caller.requestBody("Unknown", map[string]any{"body": body}, map[string]bool{})
			if err != nil || !reflect.DeepEqual(got, body) {
				t.Fatalf("metadata fallback lost body: %v", err)
			}
		}
	}
	got, err := requestBodyWithoutMetadata(map[string]any{"body.name": "value"}, map[string]bool{})
	if err != nil || !reflect.DeepEqual(got, map[string]any{"name": "value"}) {
		t.Fatalf("flattened fallback body=%v, %v", got, err)
	}
}

func TestCLIFallbackClassifiesProviderFailures(t *testing.T) {
	for _, tt := range []struct {
		stderr string
		err    error
		code   string
	}{
		{"", &exec.Error{Name: "aliyun", Err: exec.ErrNotFound}, "AliyunCLINotFound"},
		{"InvalidInstanceId.NotFound: instance does not exist", errors.New("exit status 1"), "NotFound"},
		{"DependencyViolation: resource is in use", errors.New("exit status 1"), "DependencyViolation"},
		{"Forbidden: permission denied", errors.New("exit status 1"), "CloudAPIError"},
	} {
		runner := &fakeCLICommandRunner{stderr: tt.stderr, err: tt.err}
		caller, err := newCLICommandCaller("test", "", "ecs", "", mapGetenv(nil), runner)
		if err != nil {
			t.Fatal(err)
		}
		_, err = caller.CallWithArgs(context.Background(), "DescribeInstances", nil, []string{"--config-path=unused"})
		if appErrorCode(err) != tt.code {
			t.Fatalf("CLI error %q = %v, want %s", tt.stderr, err, tt.code)
		}
	}
	if cliCommandErrorMessage(nil, nil, nil) != "aliyun CLI command failed" || cliCommandErrorMessage(errors.New("fallback"), nil, nil) != "fallback" || cliCommandErrorMessage(nil, []byte("stdout"), nil) != "stdout" {
		t.Fatal("CLI error diagnostic precedence changed")
	}
}

func TestCLIFallbackRejectsUnserializableInputsBeforeExecution(t *testing.T) {
	caller := &CLICommandCaller{Product: "unknown"}
	for _, request := range []map[string]any{{"body": make(chan int)}, {"value": make(chan int)}} {
		if _, err := caller.requestArgs("Unknown", request); err == nil {
			t.Fatal("unserializable request accepted")
		}
	}
	for _, tt := range []struct {
		value any
		want  string
	}{{int64(4), "4"}, {1.25, "1.25"}, {json.Number("2"), "2"}, {nil, ""}, {false, "false"}} {
		if got, err := cliParamValue(tt.value); err != nil || got != tt.want {
			t.Fatalf("CLI value = %q, %v; want %q", got, err, tt.want)
		}
	}
	for _, raw := range []string{"", "raw text", "true", "[]", `{}`} {
		if _, err := decodeCLICommandResponse([]byte(raw)); err != nil {
			t.Fatalf("CLI output %q: %v", raw, err)
		}
	}
}

func TestCLIFallbackConfigurationFailureAndCleanup(t *testing.T) {
	dir := t.TempDir()
	aliyunPath, ecctlPath := filepath.Join(dir, "aliyun.json"), filepath.Join(dir, "ecctl.json")
	getenv := mapGetenv(map[string]string{"ECCTL_ALIYUN_CONFIG_PATH": aliyunPath})
	caller, err := newCLICommandCaller("test", ecctlPath, "unknown", "", getenv, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{aliyunPath, ecctlPath} {
		if err := os.WriteFile(path, []byte("invalid JSON"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := caller.commandConfig(nil); appErrorCode(err) != "InvalidConfig" {
			t.Fatalf("invalid config %s = %v", path, err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	writeJSONFile(t, aliyunPath, map[string]any{"current": "aliyun-profile", "profiles": []any{}})
	path, profile, cleanup, err := caller.commandConfig(nil)
	if err != nil || path != aliyunPath || profile != "test" || cleanup != nil {
		t.Fatalf("Aliyun-only config = %q, %q, %v", path, profile, err)
	}
	writeJSONFile(t, ecctlPath, map[string]any{"current": "ecctl-profile", "profiles": []any{}})
	path, profile, cleanup, err = caller.commandConfig(nil)
	if err != nil || cleanup == nil || profile != "test" || path == ecctlPath {
		t.Fatalf("merged config = %q, %q, %v", path, profile, err)
	}
	cleanup()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary config remains: %v", err)
	}
	if clone := cloneConfigObject(map[string]any{"invalid": make(chan int)}); len(clone) != 0 {
		t.Fatalf("unserializable config clone = %#v", clone)
	}
	if _, cleanup, err := writeTempConfig(map[string]any{"invalid": make(chan int)}); err == nil || cleanup != nil {
		t.Fatal("unserializable temp config accepted")
	}
	for _, args := range [][]string{{"--profile"}, {"--profile=one"}, {"-p=one"}, {"-p", "one"}} {
		if _, found := cliFlagValue(args, "profile"); !found {
			t.Fatalf("profile flag not detected: %v", args)
		}
	}
}

func TestExecCLICommandRunnerCapturesBothStreams(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "ECCTL_TEST_CLI_RUNNER=1")
	stdout, stderr, err := (execCLICommandRunner{}).Run(context.Background(), executable, []string{"-test.run=^TestExecCLICommandRunnerHelper$"}, env)
	if err == nil || !strings.Contains(string(stdout), "output") || string(stderr) != "diagnostic\n" {
		t.Fatalf("runner stdout=%q stderr=%q err=%v", stdout, stderr, err)
	}
	if !reflect.DeepEqual(env[len(env)-1:], []string{"ECCTL_TEST_CLI_RUNNER=1"}) {
		t.Fatal("runner mutated environment")
	}
}

func TestExecCLICommandRunnerHelper(t *testing.T) {
	if os.Getenv("ECCTL_TEST_CLI_RUNNER") != "1" {
		return
	}
	fmt.Fprintln(os.Stdout, "output")
	fmt.Fprintln(os.Stderr, "diagnostic")
	os.Exit(7)
}
