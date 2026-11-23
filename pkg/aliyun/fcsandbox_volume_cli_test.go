package aliyun_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/aliyun"
	"github.com/aliyun/elastic-compute-control-cli/pkg/cli"
	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestVolumeEnriched404SDKErrorReachesCLIAsNotFound(t *testing.T) {
	caller := aliyun.VolumeSDKErrorCallerForTest(t, 404, `{"code":"VolumeNotFound","httpStatusCode":404,"message":"gone","requestId":"req-wire","success":false}`)
	dir := t.TempDir()
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(dir, "missing-ecctl.json"))
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", filepath.Join(dir, "missing-aliyun.json"))
	t.Setenv("ECCTL_PROFILE", "")
	ctx := cli.WithResourceCallerFactory(context.Background(), func(_, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "sandbox" || resource.Resource != "volume" || resource.APIProduct != "FCSandbox" || region != "cn-hangzhou" {
			t.Fatalf("Volume command routed incorrectly: %+v region=%q", resource, region)
		}
		return caller, nil
	})
	var stdout, stderr bytes.Buffer
	exit := cli.Run(ctx, []string{"--lang", "en", "--output", "json", "sandbox", "volume", "get", "vol-1", "--team-id", "team-1", "--region", "cn-hangzhou"}, &stdout, &stderr)
	if exit != 4 || stderr.Len() != 0 {
		t.Fatalf("enriched 404 CLI exit=%d stdout=%s stderr=%s", exit, stdout.String(), stderr.String())
	}
	var output map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("invalid CLI JSON: %v", err)
	}
	failure, _ := output["error"].(map[string]any)
	actions, _ := output["actions"].([]any)
	if failure["code"] != "NotFound" || len(actions) != 1 {
		t.Fatalf("enriched 404 lost scoped CLI absence: %#v", output)
	}
	action, _ := actions[0].(map[string]any)
	if action["action_name"] != "GetVolume" || action["code"] != "VolumeNotFound" || action["request_id"] != "req-wire" || action["provider_verified"] != true {
		t.Fatalf("enriched 404 lost verified CLI action: %#v", output)
	}
}
