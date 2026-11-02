package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/i18n"
	"github.com/aliyun/elastic-compute-control-cli/pkg/updater"
	"github.com/spf13/pflag"
)

func TestConfigureCommandsPropagateCorruptAndReadOnlyConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("ECCTL_CONFIG_PATH", path)
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", filepath.Join(t.TempDir(), "missing.json"))
	if err := os.WriteFile(path, []byte("malformed"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"get"}, {"set", "output", "json"}, {"use", "default"}} {
		cmd := newConfigCommand(&globalOptions{lang: "en"}, io.Discard)
		cmd.SetArgs(args)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		err := cmd.Execute()
		if code, _, ok := appErrorCodeMessage(err); !ok || code != "InvalidConfig" {
			t.Fatalf("corrupt configure %v=%v", args, err)
		}
	}
	if err := os.WriteFile(path, []byte(`{"current":"default","profiles":[{"name":"default"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	for _, args := range [][]string{{"set", "output", "text"}, {"use", "default"}} {
		cmd := newConfigCommand(&globalOptions{lang: "en"}, io.Discard)
		cmd.SetArgs(args)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		err := cmd.Execute()
		if code, _, ok := appErrorCodeMessage(err); !ok || code != "ConfigWriteFailed" {
			t.Fatalf("read-only configure %v=%v", args, err)
		}
	}
	if raw, err := os.ReadFile(path); err != nil || strings.Contains(string(raw), "text") {
		t.Fatalf("failed save changed config: %v", err)
	}
}

func TestLocalizedErrorsPreserveActionEvidenceAndNormalizeNil(t *testing.T) {
	for _, agent := range []bool{false, true} {
		var output bytes.Buffer
		var code int
		if agent {
			code = writeAgentLocalizedError(&output, &globalOptions{lang: "en"}, nil)
		} else {
			code = writeLocalizedError(&output, nil, "en", "invalid")
		}
		if code == 0 || !strings.Contains(output.String(), "InternalError") {
			t.Fatalf("missing internal error envelope: %s", output.String())
		}
	}
	err := ecerrors.Service("Failure", "failed", false, ecerrors.WithActionsOption(ecerrors.Action{ActionName: "TestAction", RequestID: "req-failed"}))
	var output bytes.Buffer
	if code := writeAgentLocalizedError(&output, &globalOptions{lang: "en", agentEnvelope: true}, err); code == 0 || !strings.Contains(output.String(), "req-failed") || !strings.Contains(output.String(), "actions") {
		t.Fatalf("failed action evidence missing: %s", output.String())
	}
	if err := updateCommandError("en", updater.WrapError(updater.ErrorKind("unknown"), errors.New("failed"))); err == nil || !strings.Contains(err.Error(), "UpdateInstallFailed") {
		t.Fatalf("unknown update error=%v", err)
	}
}

func TestRootHelpDefaultsAndPublicSchemaNamesRemainStable(t *testing.T) {
	if got := formatVersion("", "", "", nil); got != "dev" {
		t.Fatalf("unknown version=%s", got)
	}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.Duration("timeout", time.Hour, "")
	flag := flags.Lookup("timeout")
	if got := compactFlagDefault(flag); got != "1h" {
		t.Fatalf("duration help=%s", got)
	}
	flag.DefValue = "invalid"
	if got := compactFlagDefault(flag); got != "invalid" {
		t.Fatalf("invalid default silently changed=%s", got)
	}
	for _, test := range []struct {
		name string
		want bool
	}{{"ecs", true}, {"ecs.parent.instance.get", true}, {"too.many.schema.name.parts", false}} {
		if got := publicCLICommandSchema(test.name); got != test.want {
			t.Fatalf("public schema %s=%t", test.name, got)
		}
	}
	if got := localizerForCommand(nil).Message("InternalError"); got != i18n.NewLocalizer("en").Message("InternalError") {
		t.Fatalf("help default language message=%s", got)
	}
}

func TestSchemaAndUpdateRejectUnsupportedRequestsBeforeWork(t *testing.T) {
	t.Setenv("ECCTL_SPEC_DIR", "")
	for _, args := range [][]string{{"list", "unknown"}, {"unknown", "resource"}, {"ros"}, {"ros", "stack", "list"}, {"ros.stack.list"}} {
		for _, full := range []bool{false, true} {
			cmd := newSchemaCommand(&globalOptions{lang: "en", fullSurface: full}, io.Discard)
			cmd.SetArgs(args)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			if err := cmd.Execute(); err == nil {
				t.Fatalf("unknown schema accepted: %v", args)
			}
		}
	}
	for _, args := range [][]string{{"update", "--check", "--force"}, {"__update", "unknown-mode"}, {"--output=unsupported", "capabilities"}} {
		stdout, _, code := runCLI(append([]string{"--lang", "en"}, args...)...)
		if code == 0 {
			t.Fatalf("invalid root request accepted: %v", args)
		}
		if !strings.Contains(stdout, "error") {
			t.Fatalf("missing error response for %v", args)
		}
	}
	if !productCommandBuildTarget([]string{"help", "configure"}).stubsOnly {
		t.Fatal("builtin help unexpectedly loads resources")
	}
	if got := commandPositionals([]string{"--", "ecs", "instance"}); !reflect.DeepEqual(got, []string{"ecs", "instance"}) {
		t.Fatalf("positionals=%v", got)
	}
}
