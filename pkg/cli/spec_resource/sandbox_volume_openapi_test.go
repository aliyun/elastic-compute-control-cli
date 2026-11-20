package spec_resource

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestSandboxVolumeNestedConfigurationOutputKeepsProviderShape(t *testing.T) {
	configs := map[string]any{
		"oss_volume_config":            map[string]any{"bucketName": "owned-bucket", "readOnly": false, "rules": []any{map[string]any{"matchType": "prefix"}}},
		"agentic_fs_volume_config":     map[string]any{"serverAddr": "example.test:/", "userID": float64(1000)},
		"agentic_bucket_volume_config": map[string]any{"agenticBucket": "owned", "readOnly": true},
		"mount_config":                 map[string]any{"vpcConfig": map[string]any{"vSwitchIds": []any{"vsw-1"}}},
	}
	provider := map[string]any{"volumeID": "vol-1", "volumeName": "workspace", "teamID": "team-1", "status": "AVAILABLE"}
	for field, key := range map[string]string{
		"oss_volume_config": "ossVolumeConfig", "agentic_fs_volume_config": "agenticFSVolumeConfig",
		"agentic_bucket_volume_config": "agenticBucketVolumeConfig", "mount_config": "mountConfig",
	} {
		provider[key] = configs[field]
	}
	for _, tc := range []struct {
		name       string
		args       []string
		responses  []map[string]any
		operations []string
	}{
		{"get", []string{"get", "vol-1"}, []map[string]any{{"code": "200", "volume": provider}}, []string{"GetVolume"}},
		{"list", []string{"list"}, []map[string]any{{"code": "200", "volumes": []any{provider}}}, []string{"ListVolumes"}},
		{"waited create", []string{"create", "--name", "workspace"}, []map[string]any{{"code": "200", "volume": provider}, {"code": "200", "volume": provider}}, []string{"CreateVolume", "GetVolume"}},
		{"no-wait create", []string{"create", "--name", "workspace", "--no-wait"}, []map[string]any{{"code": "200", "volume": provider}}, []string{"CreateVolume"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeSpecCaller{responses: tc.responses}
			run := withCaller(func(_, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
				if resource.Provider != "aliyun" || resource.APIProduct != "FCSandbox" || resource.Resource != "volume" || region != "cn-hangzhou" {
					t.Fatalf("wrong native Volume route: %+v %q", resource, region)
				}
				return fake, nil
			})
			args := append([]string{"--lang", "en", "sandbox", "volume"}, tc.args...)
			args = append(args, "--region", "cn-hangzhou", "--output=json")
			stdout, stderr, code := run(args...)
			if code != 0 {
				t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout, stderr)
			}
			if len(fake.calls) != len(tc.operations) {
				t.Fatalf("native Volume calls=%+v", fake.calls)
			}
			for index, operation := range tc.operations {
				if fake.calls[index].operation != operation {
					t.Fatalf("native Volume calls=%+v", fake.calls)
				}
			}
			out := decodeObject(t, stdout)
			volume, _ := out["volume"].(map[string]any)
			if tc.name == "list" {
				items, ok := out["volumes"].([]any)
				if !ok || len(items) != 1 {
					t.Fatalf("native Volume list shape=%s", stdout)
				}
				volume, _ = items[0].(map[string]any)
			}
			if volume == nil {
				t.Fatalf("native Volume output shape=%s", stdout)
			}
			for field, want := range configs {
				if !reflect.DeepEqual(volume[field], want) {
					t.Errorf("%s %s=%#v, want %#v", tc.name, field, volume[field], want)
				}
			}
		})
	}
}

func TestSandboxVolumeAgenticFSIntegerBoundsBeforeCall(t *testing.T) {
	for _, field := range []string{"userID", "groupID"} {
		for _, tc := range []struct {
			value string
			valid bool
		}{
			{"-2147483648", true}, {"2147483647", true},
			{"-2147483649", false}, {"2147483648", false},
		} {
			for _, fromFile := range []bool{false, true} {
				name := fmt.Sprintf("%s/%s/file=%t", field, tc.value, fromFile)
				t.Run(name, func(t *testing.T) {
					input := fmt.Sprintf(`{"serverAddr":"example.test:/","%s":%s}`, field, tc.value)
					if fromFile {
						path := filepath.Join(t.TempDir(), "agentic-fs.json")
						if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
							t.Fatal(err)
						}
						input = "@" + path
					}
					fake := &fakeSpecCaller{responses: []map[string]any{{"code": "200", "volume": map[string]any{"volumeID": "vol-1", "volumeName": "workspace", "teamID": "team-1", "status": "CREATING"}}}}
					run := withCaller(func(_, _ string, _ spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
						return fake, nil
					})
					_, stderr, code := run("--lang", "en", "sandbox", "volume", "create", "--name", "workspace", "--team-id", "team-1", "--agentic-fs-volume-config", input, "--no-wait", "--region", "cn-hangzhou", "--output=json")
					if tc.valid && (code != 0 || len(fake.calls) != 1) || !tc.valid && (code == 0 || len(fake.calls) != 0) {
						t.Fatalf("value %s file=%t: exit=%d calls=%d stderr=%s", tc.value, fromFile, code, len(fake.calls), stderr)
					}
				})
			}
		}
	}
}

func TestSandboxVolumeAgenticBucketNullableFieldsReachProvider(t *testing.T) {
	for _, fromFile := range []bool{false, true} {
		t.Run(fmt.Sprintf("file=%t", fromFile), func(t *testing.T) {
			input := `{"agenticBucket":"owned","bucketPath":null,"readOnly":null}`
			if fromFile {
				path := filepath.Join(t.TempDir(), "agentic-bucket.json")
				if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
					t.Fatal(err)
				}
				input = "@" + path
			}
			fake := &fakeSpecCaller{responses: []map[string]any{{"code": "200", "volume": map[string]any{"volumeID": "vol-1", "volumeName": "workspace", "teamID": "team-1", "status": "CREATING"}}}}
			run := withCaller(func(_, _ string, _ spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
				return fake, nil
			})
			_, stderr, code := run("--lang", "en", "sandbox", "volume", "create", "--name", "workspace", "--team-id", "team-1",
				"--agentic-bucket-volume-config", input, "--no-wait", "--region", "cn-hangzhou", "--output=json")
			if code != 0 || len(fake.calls) != 1 {
				t.Fatalf("nullable fields file=%t exit=%d calls=%d stderr=%s", fromFile, code, len(fake.calls), stderr)
			}
			body, ok := fake.calls[0].request["body.agenticBucketVolumeConfig"].(string)
			if !ok {
				t.Fatalf("request body = %#v", fake.calls[0].request)
			}
			var config map[string]any
			if err := json.Unmarshal([]byte(body), &config); err != nil || config["agenticBucket"] != "owned" {
				t.Fatalf("agentic bucket request = %#v", fake.calls[0].request)
			}
			for _, field := range []string{"bucketPath", "readOnly"} {
				if value, present := config[field]; !present || value != nil {
					t.Fatalf("%s null was lost: %#v", field, config)
				}
			}
		})
	}
}
