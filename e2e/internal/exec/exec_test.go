package exec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
