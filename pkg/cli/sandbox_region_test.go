package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type sandboxRegionTransport func(*http.Request) (*http.Response, error)

func (f sandboxRegionTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestSandboxDefaultEndpointFromRegion(t *testing.T) {
	const aliyunConfig = `{"current":"work","profiles":[{"name":"work","mode":"AK","region_id":"cn-beijing"},{"name":"other","region_id":"us-west-1"}]}`
	const ecctlConfig = `{"current":"work","profiles":[{"name":"work","mode":"OAuth","region_id":"cn-shanghai"}]}`
	for _, tt := range []struct {
		name, ecctl, aliyun string
		env                 map[string]string
		args                []string
		wantHost            string
		metadataUnavailable bool
	}{
		{name: "no config", wantHost: "api.cn-hangzhou.e2b.fc.aliyuncs.com"},
		{name: "Aliyun current profile without AK", aliyun: aliyunConfig, wantHost: "api.cn-beijing.e2b.fc.aliyuncs.com"},
		{name: "ecctl overrides same Aliyun profile without OAuth login", ecctl: ecctlConfig, aliyun: aliyunConfig, wantHost: "api.cn-shanghai.e2b.fc.aliyuncs.com"},
		{name: "explicit profile", ecctl: ecctlConfig, aliyun: aliyunConfig, args: []string{"--profile", "other"}, wantHost: "api.us-west-1.e2b.fc.aliyuncs.com"},
		{name: "environment profile", aliyun: aliyunConfig, env: map[string]string{"ALIBABA_CLOUD_PROFILE": "other"}, wantHost: "api.us-west-1.e2b.fc.aliyuncs.com"},
		{name: "explicit region", ecctl: ecctlConfig, args: []string{"--region", "cn-shenzhen"}, env: map[string]string{"ECCTL_REGION": "cn-beijing"}, wantHost: "api.cn-shenzhen.e2b.fc.aliyuncs.com"},
		{name: "ecctl environment region", ecctl: ecctlConfig, env: map[string]string{"ECCTL_REGION": "cn-hongkong"}, wantHost: "api.cn-hongkong.e2b.fc.aliyuncs.com"},
		{name: "Alibaba environment region", env: map[string]string{"ALIBABA_CLOUD_REGION_ID": "ap-southeast-1"}, wantHost: "api.ap-southeast-1.e2b.fc.aliyuncs.com"},
		{name: "profile precedes Alibaba environment", aliyun: aliyunConfig, env: map[string]string{"ALIBABA_CLOUD_REGION_ID": "us-east-1"}, wantHost: "api.cn-beijing.e2b.fc.aliyuncs.com"},
		{name: "ignore profile", ecctl: ecctlConfig, aliyun: aliyunConfig, env: map[string]string{"ALIBABA_CLOUD_IGNORE_PROFILE": "true"}, wantHost: "api.cn-hangzhou.e2b.fc.aliyuncs.com"},
		{name: "unsupported profile region", aliyun: strings.ReplaceAll(aliyunConfig, "cn-beijing", "cn-qingdao"), wantHost: "api.cn-hangzhou.e2b.fc.aliyuncs.com"},
		{name: "invalid profile region", aliyun: strings.ReplaceAll(aliyunConfig, "cn-beijing", "../bad"), wantHost: "api.cn-hangzhou.e2b.fc.aliyuncs.com"},
		{name: "missing profile region", ecctl: `{"current":"work","profiles":[{"name":"work"}]}`, wantHost: "api.cn-hangzhou.e2b.fc.aliyuncs.com"},
		{name: "unreadable config content", ecctl: `{`, wantHost: "api.cn-hangzhou.e2b.fc.aliyuncs.com"},
		{name: "domain overrides region", ecctl: ecctlConfig, env: map[string]string{"E2B_DOMAIN": "custom.example"}, wantHost: "api.custom.example"},
		{name: "URL overrides domain and region", ecctl: ecctlConfig, env: map[string]string{"E2B_DOMAIN": "custom.example", "E2B_API_URL": "https://api.e2b.app"}, wantHost: "api.e2b.app"},
		{name: "explicit endpoint with invalid config", ecctl: `{`, env: map[string]string{"E2B_API_URL": "https://api.e2b.app"}, wantHost: "api.e2b.app"},
		{name: "new region from metadata", args: []string{"--region", "cn-future-1"}, wantHost: "api.cn-future-1.e2b.fc.aliyuncs.com"},
		{name: "metadata unavailable", aliyun: aliyunConfig, metadataUnavailable: true, wantHost: "api.cn-hangzhou.e2b.fc.aliyuncs.com"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, key := range []string{
				"ECCTL_REGION", "ALIBABA_CLOUD_REGION_ID", "ALIBABACLOUD_REGION_ID", "ALICLOUD_REGION_ID", "REGION_ID", "REGION",
				"ECCTL_PROFILE", "ALIBABA_CLOUD_PROFILE", "ALIBABACLOUD_PROFILE", "ALICLOUD_PROFILE",
				"ALIBABA_CLOUD_IGNORE_PROFILE", "ALIBABACLOUD_IGNORE_PROFILE",
				"E2B_API_URL", "E2B_DOMAIN", "ECCTL_SANDBOX_CA_FILE", "ECCTL_SPEC_DIR",
			} {
				t.Setenv(key, "")
			}
			t.Setenv("E2B_API_KEY", "sandbox-test-key")
			t.Setenv("ECCTL_SANDBOX_BACKEND", "fc")
			files := map[string]string{}
			for key, raw := range map[string]string{"ECCTL_CONFIG_PATH": tt.ecctl, "ECCTL_ALIYUN_CONFIG_PATH": tt.aliyun} {
				path := filepath.Join(t.TempDir(), "config.json")
				t.Setenv(key, path)
				files[path] = raw
				if raw != "" {
					if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			for key, value := range tt.env {
				t.Setenv(key, value)
			}
			previous := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = previous })
			calls := 0
			metadataCalls := 0
			http.DefaultTransport = sandboxRegionTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() == "https://api.aliyun.com/meta/v1/products/FCSandbox/endpoints.json" {
					metadataCalls++
					if r.Header.Get("X-API-Key") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
						t.Fatal("metadata lookup sent credentials")
					}
					if tt.metadataUnavailable {
						return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
					}
					endpoints := []map[string]string{}
					for _, region := range []string{"cn-beijing", "cn-hangzhou", "cn-shanghai", "cn-shenzhen", "cn-hongkong", "ap-southeast-1", "us-east-1", "us-west-1", "cn-future-1"} {
						endpoints = append(endpoints, map[string]string{"regionId": region, "public": "fcsandbox." + region + ".aliyuncs.com"})
					}
					raw, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{"type": "regional", "endpoints": endpoints}})
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
				}
				calls++
				if r.URL.Scheme != "https" || r.URL.Host != tt.wantHost || r.URL.Path != "/templates" || r.Header.Get("X-API-Key") != "sandbox-test-key" {
					t.Fatalf("unexpected sandbox request: %s", r.URL)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`[]`))}, nil
			})
			args := append([]string{"--lang", "en", "sbx", "template", "list"}, tt.args...)
			stdout, stderr, code := runCLI(args...)
			if code != 0 || calls != 1 {
				t.Fatalf("code=%d calls=%d stdout=%s stderr=%s", code, calls, stdout, stderr)
			}
			if metadataCalls > 1 || ((tt.env["E2B_API_URL"] != "" || tt.env["E2B_DOMAIN"] != "") && metadataCalls != 0) {
				t.Fatalf("unexpected metadata lookup count: %d", metadataCalls)
			}
			for path, want := range files {
				raw, err := os.ReadFile(path)
				if want == "" && os.IsNotExist(err) {
					continue
				}
				if err != nil || string(raw) != want {
					t.Fatalf("configuration changed: %s", path)
				}
			}
		})
	}
}
