package schema

import (
	"strings"
	"testing"
)

func TestNativeTemplateUpdateDiscoveryMatchesInputs(t *testing.T) {
	for _, mode := range []CommandSchemaMode{CommandSchemaBrief, CommandSchemaFull} {
		command, ok := CommandForLanguageMode("sandbox.template.update", "en", mode)
		if !ok {
			t.Fatal("missing template update schema")
		}
		public, ok := command.Params["public"]
		if !ok || public.Required {
			t.Fatalf("native discovery requires a rejected flag: %+v", command.Params)
		}
		if !strings.Contains(public.Description, "E2B") || !strings.Contains(public.Description, "--openapi") {
			t.Fatalf("conditional public contract is missing: %+v", public)
		}
		if !strings.Contains(command.Params["runtime-config"].Description, "--build-config") || !strings.Contains(command.Params["build-config"].Description, "--runtime-config") {
			t.Fatalf("native update one-of is undiscoverable: %+v", command.Params)
		}
	}
}

func TestNativeTemplateUpdateDoesNotAdvertiseE2BPoll(t *testing.T) {
	command, ok := CommandForLanguage("sandbox.template.update", "en")
	if !ok {
		t.Fatal("missing template update schema")
	}
	if command.Contract == nil || command.Contract.Wait == nil {
		t.Fatal("native waiter missing")
	}
	if command.Contract.Wait.PollCommand != "" {
		t.Fatalf("conditional native waiter advertises E2B poll: %q", command.Contract.Wait.PollCommand)
	}
}
