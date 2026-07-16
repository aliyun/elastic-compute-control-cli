package aliyun

import (
	stderrors "errors"
	"path/filepath"
	"strings"
	"testing"

	ecerrors "ecctl/pkg/errors"
)

func TestParseTagAssignmentsTrimsValuesAndRejectsInvalidTags(t *testing.T) {
	got, err := parseTagAssignments([]string{" env = dev ", "owner=platform"})
	if err != nil {
		t.Fatalf("parseTagAssignments: %v", err)
	}
	if len(got) != 2 || got[0].Key != "env" || got[0].Value != "dev" || got[1].Key != "owner" {
		t.Fatalf("unexpected assignments: %#v", got)
	}

	_, err = parseTagAssignments([]string{"missing-separator"})
	if err == nil {
		t.Fatal("expected invalid tag error")
	}
	var appErr *ecerrors.AppError
	if !stderrors.As(err, &appErr) || appErr.Payload().Code != "InvalidTag" {
		t.Fatalf("invalid tag error = %T %v, want InvalidTag AppError", err, err)
	}
}

func TestClientTokenIsUniquePerCall(t *testing.T) {
	payload := map[string]any{
		"name": "test",
		"cidr": "10.10.0.0/24",
	}
	first := clientToken("vpc-create", payload)
	second := clientToken("vpc-create", payload)

	if first == second {
		t.Fatalf("clientToken returned duplicate token %q for repeated create calls", first)
	}
	if !strings.HasPrefix(first, "vpc-create-") || !strings.HasPrefix(second, "vpc-create-") {
		t.Fatalf("tokens must keep prefix, got %q and %q", first, second)
	}
}

func TestCloudErrorHelpersClassifyLocalErrors(t *testing.T) {
	if !isAppNotFound(ecerrors.NotFound("NotFound", "vpc vpc-123 not found")) {
		t.Fatal("NotFound AppError must be classified as app not found")
	}
	if isAppNotFound(stderrors.New("not found")) {
		t.Fatal("plain errors must not be classified as app not found")
	}

	for _, message := range []string{"DependencyViolation", "resource is dependent", "resource is in use", "ResourceInUse"} {
		t.Run(message, func(t *testing.T) {
			if !isDependencyViolation(stderrors.New(message)) {
				t.Fatalf("%q should be classified as dependency violation", message)
			}
		})
	}
	if isDependencyViolation(nil) {
		t.Fatal("nil error must not be dependency violation")
	}
}

func TestCloudNotFoundAndSanitizedErrorHelpers(t *testing.T) {
	for _, message := range []string{"NotFound", "not found", "resource does not exist"} {
		t.Run(message, func(t *testing.T) {
			if !isCloudNotFound(stderrors.New(message)) {
				t.Fatalf("%q should be classified as cloud not found", message)
			}
		})
	}
	if isCloudNotFound(nil) {
		t.Fatal("nil error must not be cloud not found")
	}
}

func TestNewCloudServicesRequireCredentials(t *testing.T) {
	dir := t.TempDir()
	ecctlPath := filepath.Join(dir, "ecctl.json")
	aliyunPath := filepath.Join(dir, "aliyun.json")
	getenv := func(name string) string {
		if name == "ECCTL_ALIYUN_CONFIG_PATH" {
			return aliyunPath
		}
		return ""
	}

	tests := []struct {
		name string
		call func() (any, error)
	}{
		{name: "vpc", call: func() (any, error) { return NewOpenAPICaller("", ecctlPath, "vpc", "", getenv) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, err := tt.call()
			if err == nil {
				t.Fatalf("expected MissingCredentials error, got service %#v", service)
			}
			var appErr *ecerrors.AppError
			if !stderrors.As(err, &appErr) || appErr.Payload().Code != "MissingCredentials" {
				t.Fatalf("error = %T %v, want MissingCredentials AppError", err, err)
			}
		})
	}
}
