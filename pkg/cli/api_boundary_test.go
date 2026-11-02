package cli

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/aliyun"
	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/spf13/cobra"
)

func TestCallRejectsConflictingModesAndMissingOperations(t *testing.T) {
	for _, args := range [][]string{{"--schema", "--list"}, {"--schema", "ecs"}, {"--list", "ecs", "vpc"}, {"ecs"}, {"ecs", "DescribeInstances", "extra"}} {
		cmd := newAPICommand(&globalOptions{lang: "en"}, io.Discard)
		cmd.SetArgs(args)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("invalid call form accepted: %v", args)
		}
	}
	if got := completeAPICallArgs("en", false, []string{"ecs", "DescribeInstances"}, ""); got != nil {
		t.Fatalf("extra argument completions=%v", got)
	}
	if got := apiOperationCompletions("en", "unknown", ""); got != nil {
		t.Fatalf("unknown product completions=%v", got)
	}
	if got := apiProductCompletions("en", "ack"); len(got) == 0 {
		t.Fatal("alias completion lost")
	}
	if !matchesAPICompletionPrefix("", "anything") {
		t.Fatal("empty completion prefix rejected")
	}
	if _, ok := apiProductSelectionByCode(" ", "en"); ok {
		t.Fatal("empty product accepted")
	}
	if apiProductDescription(aliyun.OpenAPIProduct{Code: "fake"}, "en") != "fake" || apiMessage("en", "UnknownMessage", nil) != "" {
		t.Fatal("metadata fallback changed")
	}
	if !apiOperationItemsLess(apiOperationItem{Name: "Same", Product: "a"}, apiOperationItem{Name: "Same", Product: "b"}, "") {
		t.Fatal("API list ordering is nondeterministic across products")
	}
	selection := apiProductSelection{Requested: "test", Products: []aliyun.OpenAPIProduct{{Code: "a", APINames: []string{"SyntheticOperation"}}, {Code: "b", APINames: []string{"SyntheticOperation"}}}}
	if list := apiProductSelectionOperations(selection, "en", apiListOptions{}); len(list.APIs) != 2 || list.APIs[0].Product != "a" {
		t.Fatalf("unknown-summary operations lost: %+v", list)
	}
	if list := apiProductSelectionOperations(selection, "en", apiListOptions{filter: "nothing"}); len(list.APIs) != 0 {
		t.Fatalf("operation filter ignored: %+v", list)
	}
}

func TestCallPropagatesFactoryAndOperationFailures(t *testing.T) {
	cause := errors.New("offline caller failure")
	options := &globalOptions{lang: "en", region: "cn-hangzhou"}
	for _, factory := range []APICallerFactory{
		func(string, string, string, string, func(string) string) (engine.Caller, error) { return nil, cause },
		func(string, string, string, string, func(string) string) (engine.Caller, error) {
			return failingAPICaller{err: cause}, nil
		},
	} {
		cmd := &cobra.Command{}
		cmd.SetContext(WithAPICallerFactory(context.Background(), factory))
		if err := runAPICall(cmd, options, io.Discard, "ecs", "DescribeInstances", "", nil, nil); !errors.Is(err, cause) {
			t.Fatalf("caller failure lost: %v", err)
		}
	}
	payload := apiCallPayload("ecs", "DescribeInstances", "cn-hangzhou", nil)
	if payload == nil {
		t.Fatal("nil response discarded command metadata")
	}
	if text := apiMessage("en", "SuggestionCallListProducts", nil); !strings.Contains(text, "call") {
		t.Fatalf("localized recovery suggestion=%s", text)
	}
}

type failingAPICaller struct{ err error }

func (f failingAPICaller) Call(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, f.err
}
