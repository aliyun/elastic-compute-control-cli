package aliyun_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/pkg/cli"
	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/i18n"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

type templateCacheCall struct {
	api     string
	request map[string]any
}

type templateCacheCLICaller struct {
	responses     []map[string]any
	errors        []error
	calls         []templateCacheCall
	probeTimeouts []time.Duration
}

func (c *templateCacheCLICaller) Call(ctx context.Context, api string, request map[string]any) (map[string]any, error) {
	c.calls = append(c.calls, templateCacheCall{api: api, request: request})
	if api == "DescribeTemplateCache" {
		if deadline, ok := ctx.Deadline(); ok {
			c.probeTimeouts = append(c.probeTimeouts, time.Until(deadline))
		}
	}
	i := len(c.calls) - 1
	if i < len(c.errors) && c.errors[i] != nil {
		return nil, c.errors[i]
	}
	if i >= len(c.responses) {
		return nil, ecerrors.Service("UnexpectedCall", "unexpected cache API call", false)
	}
	return c.responses[i], nil
}

func runTemplateCacheCLI(t *testing.T, caller engine.Caller, args ...string) (map[string]any, int) {
	return runTemplateCacheCLIWithLang(t, caller, "en", args...)
}

func runTemplateCacheCLIWithLang(t *testing.T, caller engine.Caller, lang string, args ...string) (map[string]any, int) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(dir, "missing-ecctl.json"))
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", filepath.Join(dir, "missing-aliyun.json"))
	t.Setenv("ECCTL_PROFILE", "")
	ctx := cli.WithResourceCallerFactory(context.Background(), func(_, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "sandbox" || resource.Resource != "template-cache" || resource.Provider != "aliyun" || resource.APIProduct != "FCSandbox" || region != "cn-hangzhou" {
			t.Fatalf("cache command routed incorrectly: %+v region=%q", resource, region)
		}
		return caller, nil
	})
	argv := append([]string{"--lang", lang, "--output", "json", "sandbox", "template-cache"}, args...)
	argv = append(argv, "--region", "cn-hangzhou", "--team-id", "team-1")
	var stdout, stderr bytes.Buffer
	code := cli.Run(ctx, argv, &stdout, &stderr)
	data := stdout.Bytes()
	if len(data) == 0 {
		data = stderr.Bytes()
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("CLI output is not JSON: %q (%v)", data, err)
	}
	return decoded, code
}

func TestTemplateCacheCLIMissingCacheUsesSelectedLanguage(t *testing.T) {
	for _, lang := range []string{"en", "zh-CN"} {
		t.Run(lang, func(t *testing.T) {
			caller := &templateCacheCLICaller{errors: []error{ecerrors.NotFound("NotFound", i18n.NewLocalizer("en").Message("TemplateCacheNotFound"),
				ecerrors.WithRawCause("TemplateCacheNotFound", "cache absent"))}}
			output, exit := runTemplateCacheCLIWithLang(t, caller, lang, "get", "tpl-1")
			failure, _ := output["error"].(map[string]any)
			want := i18n.NewLocalizer(lang).Message("TemplateCacheNotFound")
			if exit == 0 || failure["code"] != "NotFound" || failure["message"] != want || len(caller.calls) != 1 || caller.calls[0].api != "DescribeTemplateCache" {
				t.Fatalf("missing cache language %q: exit=%d output=%#v calls=%+v", lang, exit, output, caller.calls)
			}
		})
	}
}

func TestTemplateCacheCLIResourceMapping(t *testing.T) {
	cache := map[string]any{"templateID": "tpl-1", "teamID": "team-1", "status": "InProgress", "progress": 50, "imageSizeMB": 512}
	for _, tc := range []struct {
		name     string
		args     []string
		api      string
		response map[string]any
	}{
		{"create", []string{"create", "--template-id", "tpl-1", "--no-wait"}, "CreateTemplateCache", map[string]any{"code": "200", "requestId": "req-create", "templateCache": cache}},
		{"get", []string{"get", "tpl-1"}, "DescribeTemplateCache", map[string]any{"code": "200", "requestId": "req-get", "templateCache": cache}},
		{"list", []string{"list", "--template-id", "tpl-1", "--status", "InProgress", "--limit", "2", "--next-token", "page-1"}, "ListTemplateCache", map[string]any{"code": "200", "requestId": "req-list", "templateCaches": []any{cache}, "nextToken": "page-2", "maxResults": 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &templateCacheCLICaller{responses: []map[string]any{tc.response}}
			output, code := runTemplateCacheCLI(t, caller, tc.args...)
			if code != 0 || len(caller.calls) != 1 || caller.calls[0].api != tc.api {
				t.Fatalf("code=%d calls=%+v output=%+v", code, caller.calls, output)
			}
			request := caller.calls[0].request
			if tc.name == "create" && (request["body.teamID"] != "team-1" || request["body.templateID"] != "tpl-1") {
				t.Fatalf("create placement = %+v", request)
			}
			if tc.name == "get" && (request["teamID"] != "team-1" || request["templateID"] != "tpl-1") {
				t.Fatalf("get placement = %+v", request)
			}
			if tc.name == "list" && (request["teamID"] != "team-1" || request["templateID"] != "tpl-1" || request["status"] != "InProgress" || request["maxResults"] != 2 || request["nextToken"] != "page-1") {
				t.Fatalf("list placement = %+v", request)
			}
			if tc.name == "list" {
				pagination, _ := output["pagination"].(map[string]any)
				if pagination["next_token"] != "page-2" {
					t.Fatalf("server pagination token lost: %+v", output)
				}
			} else {
				item, _ := output["template_cache"].(map[string]any)
				if item["id"] != "tpl-1" || item["status"] != "InProgress" || item["progress"] != float64(50) {
					t.Fatalf("cache view lost: %+v", output)
				}
			}
		})
	}
}

func TestTemplateCacheCLIDeletionWaitsForCacheAbsence(t *testing.T) {
	restore := engine.SetTimeScaleForTest(0)
	defer restore()
	caller := &templateCacheCLICaller{
		responses: []map[string]any{{"code": "200", "requestId": "req-delete"}},
		errors:    []error{nil, ecerrors.NotFound("NotFound", "cache absent", ecerrors.WithRawCause("TemplateCacheNotFound", "cache absent"))},
	}
	output, code := runTemplateCacheCLI(t, caller, "delete", "tpl-1")
	if code != 0 || len(caller.calls) != 2 || caller.calls[0].api != "DeleteTemplateCache" || caller.calls[1].api != "DescribeTemplateCache" || output["deleted"] != true {
		t.Fatalf("delete confirmation: code=%d calls=%+v output=%+v", code, caller.calls, output)
	}
	caller = &templateCacheCLICaller{responses: []map[string]any{{"code": "200", "requestId": "req-delete"}}}
	output, code = runTemplateCacheCLI(t, caller, "delete", "tpl-1", "--no-wait")
	if code != 0 || len(caller.calls) != 1 || output["deleted"] != nil || output["deletion_requested"] != true {
		t.Fatalf("no-wait claim: code=%d calls=%+v output=%+v", code, caller.calls, output)
	}
}

func TestTemplateCacheCLIWaitTimeoutDefaultsAndOverrides(t *testing.T) {
	for _, tc := range []struct {
		name, action string
		args         []string
		want         time.Duration
	}{
		{"create default", "create", nil, 20 * time.Minute},
		{"delete default", "delete", nil, 5 * time.Minute},
		{"create explicit", "create", []string{"--timeout", "7s"}, 7 * time.Second},
		{"delete explicit", "delete", []string{"--timeout", "7s"}, 7 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &templateCacheCLICaller{}
			args := []string{tc.action}
			if tc.action == "create" {
				args = append(args, "--template-id", "tpl-1")
				caller.responses = []map[string]any{
					{"code": "200", "templateCache": map[string]any{"templateID": "tpl-1", "teamID": "team-1", "status": "InProgress"}},
					{"code": "200", "templateCache": map[string]any{"templateID": "tpl-1", "teamID": "team-1", "status": "Success"}},
				}
			} else {
				args = append(args, "tpl-1")
				caller.responses = []map[string]any{{"code": "200"}}
				caller.errors = []error{nil, ecerrors.NotFound("NotFound", "cache absent")}
			}
			args = append(args, tc.args...)
			_, code := runTemplateCacheCLI(t, caller, args...)
			if code != 0 || len(caller.probeTimeouts) != 1 {
				t.Fatalf("wait probe missing: exit=%d deadlines=%d", code, len(caller.probeTimeouts))
			}
			got := caller.probeTimeouts[0]
			if got < tc.want-3*time.Second || got > tc.want {
				t.Fatalf("wait timeout = %s, want about %s", got, tc.want)
			}
		})
	}
}

func TestTemplateCacheCLIFailureIncludesReasonAndRequestContext(t *testing.T) {
	restore := engine.SetTimeScaleForTest(0)
	defer restore()
	for _, status := range []string{"Failed", "Evicted"} {
		t.Run(status, func(t *testing.T) {
			caller := &templateCacheCLICaller{responses: []map[string]any{
				{"code": "200", "requestId": "req-create", "templateCache": map[string]any{"templateID": "tpl-1", "teamID": "team-1", "status": "InProgress"}},
				{"code": "200", "requestId": "req-describe", "templateCache": map[string]any{"templateID": "tpl-1", "teamID": "team-1", "status": status, "statusReason": "image unavailable"}},
			}}
			output, code := runTemplateCacheCLI(t, caller, "create", "--template-id", "tpl-1")
			failure, _ := output["error"].(map[string]any)
			if code != 2 || failure["code"] != "WaitFailed" || failure["current_state"] != status || failure["detail"] != "image unavailable" {
				t.Fatalf("failure details lost: code=%d output=%#v", code, output)
			}
			actions, _ := json.Marshal(output["actions"])
			if !strings.Contains(string(actions), "req-describe") {
				t.Fatalf("Describe request context lost: %#v", output)
			}
		})
	}
}

func TestTemplateCacheCLIInvalidResponseRetainsRequestID(t *testing.T) {
	caller := &templateCacheCLICaller{errors: []error{ecerrors.Service("InvalidTemplateCacheResponse", "invalid cache response", false,
		ecerrors.WithField("templateCache"), ecerrors.WithDetail("missing"), ecerrors.WithRequestID("req-123"))}}
	output, code := runTemplateCacheCLI(t, caller, "get", "tpl-1")
	actions, err := json.Marshal(output["actions"])
	if code == 0 || err != nil || !strings.Contains(string(actions), `"request_id":"req-123"`) {
		t.Fatalf("invalid-response CLI diagnostic lost request ID: code=%d actions=%s err=%v", code, actions, err)
	}
}
