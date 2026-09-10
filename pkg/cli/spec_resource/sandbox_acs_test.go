package spec_resource

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/cli"
)

func setupACSCLI(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	t.Setenv("ECCTL_SPEC_DIR", "")
	t.Setenv("E2B_API_KEY", "acs-test-key")
	t.Setenv("E2B_API_URL", server.URL)
	t.Setenv("ECCTL_SANDBOX_BACKEND", "acs")
	t.Setenv("ECCTL_SANDBOX_CA_FILE", "")
}

func runPublicSandbox(args ...string) (string, int) {
	var out, stderr bytes.Buffer
	code := cli.Run(context.Background(), append([]string{"--lang", "en", "sbx"}, args...), &out, &stderr)
	return out.String(), code
}

func TestACSCLIRejectsEntireUpdateBeforeMutation(t *testing.T) {
	calls := 0
	setupACSCLI(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method == "GET" {
			fmt.Fprint(w, `{"sandboxID":"sbx","state":"running"}`)
		} else {
			w.WriteHeader(204)
		}
	})
	out, code := runPublicSandbox("update", "sbx", "--timeout-seconds", "600", "--network", "{}")
	if code == 0 || errorCode(t, out) != "UnsupportedOperation" || calls != 0 {
		t.Fatalf("code=%d calls=%d out=%s", code, calls, out)
	}
	out, code = runPublicSandbox("update", "sbx", "--timeout-seconds", "600")
	if code != 0 || calls != 2 {
		t.Fatalf("TTL-only: code=%d calls=%d out=%s", code, calls, out)
	}
}

func TestACSCLITemplateCommandsAndLocalization(t *testing.T) {
	setupACSCLI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/templates" {
			t.Errorf("unexpected request: %s", r.URL)
		}
		fmt.Fprint(w, `[{"templateID":"a"},{"templateID":"b"}]`)
	})
	out, code := runPublicSandbox("template", "list", "--limit", "1")
	if code != 0 || len(decodeObject(t, out)["templates"].([]any)) != 1 {
		t.Fatalf("%d: %s", code, out)
	}
	for _, args := range [][]string{
		{"template", "build-status", "a", "--build-id", "missing"},
		{"template", "build-logs", "a", "--build-id", "missing"},
		{"template", "tag-list", "a"},
		{"template", "create", "--name", "test", "--from-image", "test"},
	} {
		out, code := runPublicSandbox(args...)
		if code == 0 || errorCode(t, out) != "UnsupportedOperation" {
			t.Fatalf("%v: %d %s", args, code, out)
		}
	}
	var outZH, stderr bytes.Buffer
	code = cli.Run(context.Background(), []string{"--lang", "zh-CN", "sbx", "template", "tag-list", "a"}, &outZH, &stderr)
	if code == 0 || !strings.Contains(errorMessage(t, outZH.String()), "ACS") {
		t.Fatalf("localized error: %s", &outZH)
	}
}

func TestSandboxDeleteWaitsForAbsence(t *testing.T) {
	gets, deletes := 0, 0
	setupACSCLI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			deletes++
			w.WriteHeader(204)
			return
		}
		gets++
		if gets < 3 {
			fmt.Fprint(w, `{"sandboxID":"sbx","state":"running"}`)
			return
		}
		http.Error(w, "Sandbox route not found", 404)
	})
	out, code := runPublicSandbox("delete", "sbx")
	if code != 0 || gets != 3 || deletes != 1 || decodeObject(t, out)["deleted"] != true {
		t.Fatalf("code=%d gets=%d deletes=%d out=%s", code, gets, deletes, out)
	}
	gets, deletes = 0, 0
	out, code = runPublicSandbox("delete", "sbx", "--no-wait")
	if code != 0 || gets != 0 || deletes != 1 {
		t.Fatalf("no-wait: %d %d %d %s", code, gets, deletes, out)
	}
}

func TestSandboxDeleteDoesNotTreatFailuresAsAbsence(t *testing.T) {
	for _, status := range []int{200, 401, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			setupACSCLI(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "DELETE" {
					w.WriteHeader(204)
					return
				}
				w.WriteHeader(status)
				fmt.Fprint(w, `{"sandboxID":"sbx","state":"running"}`)
			})
			out, code := runPublicSandbox("delete", "sbx", "--timeout", "20ms")
			if code == 0 || decodeObject(t, out)["deleted"] == true {
				t.Fatalf("false deletion success: %d %s", code, out)
			}
		})
	}
}

func TestUnknownBackendCannotNormalizeTemplateDetailAsTags(t *testing.T) {
	setupACSCLI(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"templateID":"a","builds":[]}`) })
	t.Setenv("ECCTL_SANDBOX_BACKEND", "auto")
	out, code := runPublicSandbox("template", "tag-list", "a")
	if code == 0 || errorCode(t, out) != "InvalidResponse" {
		t.Fatalf("false success: %d %s", code, out)
	}
}

func TestSandboxDeleteRejectsWrongResponseIdentity(t *testing.T) {
	setupACSCLI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			w.WriteHeader(204)
			return
		}
		fmt.Fprint(w, `{"sandboxID":"other","state":"running"}`)
	})
	out, code := runPublicSandbox("delete", "sbx")
	if code == 0 || errorCode(t, out) != "InvalidResponse" || decodeObject(t, out)["deleted"] == true {
		t.Fatalf("wrong resource was treated as absence: %d %s", code, out)
	}
}
