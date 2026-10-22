package telemetry

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestSessionExportsRootAttemptsAndIdentityWithoutErrorText(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	ctx, session := Start(WithExporterForTest(context.Background(), exporter), Options{
		Enabled:    true,
		Surface:    "public",
		Version:    "1.2.3",
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
	})
	if session == nil || FromContext(ctx) != session {
		t.Fatal("telemetry session was not attached to context")
	}
	operationID := session.NextOperationID()
	for attempt := 1; attempt <= 3; attempt++ {
		end := session.StartAPI(APIRequest{
			Service: "Ecs", API: "DescribeInstances", APIVersion: "2014-05-26",
			Transport: "darabonba", OperationID: operationID, Attempt: attempt, RetryObservable: true,
		})
		if attempt < 3 {
			end(APIResult{Err: errors.New("secret request-id req-123"), RequestID: fmt.Sprintf("req-attempt-%d", attempt)})
		} else {
			end(APIResult{RequestID: "req-attempt-3"})
		}
	}
	session.RegisterIdentity("test-ak", func(context.Context) (Identity, error) {
		return Identity{Hash: "principal-hash", Type: "RAMUser"}, nil
	})
	session.Finish("ecctl ecs instance list", 0)

	spans := exporter.GetSpans()
	if len(spans) != 4 {
		t.Fatalf("span count = %d, want 4", len(spans))
	}
	var rootTraceID, rootSpanID string
	apiCount := 0
	for _, span := range spans {
		attrs := attributeMap(span.Attributes)
		switch span.Name {
		case "ecctl.command":
			rootTraceID = span.SpanContext.TraceID().String()
			rootSpanID = span.SpanContext.SpanID().String()
			installationHash, _ := attrs["ecctl.installation.hash"].(string)
			validInstallationHash := len(installationHash) == 64 || (runtime.GOOS == "windows" && installationHash == "")
			if attrs["ecctl.command.name"] != "ecctl ecs instance list" || attrs["ecctl.identity.hash"] != "principal-hash" || !validInstallationHash {
				t.Fatalf("root attributes = %#v", attrs)
			}
		case "ecctl.cloud.api.request":
			apiCount++
			if attrs["ecctl.cloud.request_id"] != fmt.Sprintf("req-attempt-%d", apiCount) {
				t.Fatalf("request ID did not match API attempt: %#v", attrs)
			}
			if attrs["ecctl.cloud.operation_id"] != operationID {
				t.Fatalf("operation id = %#v, want %q", attrs["ecctl.cloud.operation_id"], operationID)
			}
			if len(span.Events) != 0 {
				t.Fatalf("API span recorded error event: %#v", span.Events)
			}
		}
	}
	if apiCount != 3 {
		t.Fatalf("API span count = %d, want 3", apiCount)
	}
	for _, span := range spans {
		if span.Name == "ecctl.cloud.api.request" {
			if span.SpanContext.TraceID().String() != rootTraceID || span.Parent.SpanID().String() != rootSpanID {
				t.Fatalf("API span is not a direct child of root: trace=%s parent=%s", span.SpanContext.TraceID(), span.Parent.SpanID())
			}
		}
	}
}

func TestSessionDisabledModesClearAmbientSessionWithoutEndingIt(t *testing.T) {
	tests := []Options{
		{Enabled: false, Surface: "public"},
		{Enabled: true, Surface: "full"},
		{Enabled: true, Surface: "custom"},
	}
	for _, options := range tests {
		t.Run(fmt.Sprintf("enabled=%t/surface=%s", options.Enabled, options.Surface), func(t *testing.T) {
			parentExporter := tracetest.NewInMemoryExporter()
			parentCtx, parent := Start(WithExporterForTest(context.Background(), parentExporter), Options{
				Enabled: true, Surface: "public", ConfigPath: filepath.Join(t.TempDir(), "config.json"),
			})
			if parent == nil {
				t.Fatal("parent session is nil")
			}
			childCtx, child := Start(parentCtx, options)
			if child != nil || FromContext(childCtx) != nil {
				t.Fatalf("disabled child retained ambient session: child=%p context=%p", child, FromContext(childCtx))
			}
			FromContext(childCtx).StartAPI(APIRequest{})
			if len(parentExporter.GetSpans()) != 0 {
				t.Fatal("disabled child unexpectedly ended or exported the parent session")
			}
			parent.Finish("ecctl parent", 0)
			if spans := parentExporter.GetSpans(); len(spans) != 1 || spans[0].Name != "ecctl.command" {
				t.Fatalf("parent session stopped working: %#v", spans)
			}
		})
	}
}

func TestSessionExportsAccountIdentifiersByIdentityType(t *testing.T) {
	tests := []struct {
		identity Identity
		want     map[string]any
	}{
		{
			identity: Identity{Hash: "account-hash", Type: "Account", AccountID: "123456789", UserID: "irrelevant-user", RoleID: "irrelevant-role"},
			want:     map[string]any{"ecctl.identity.account_id": "123456789"},
		},
		{
			identity: Identity{Hash: "user-hash", Type: "RAMUser", AccountID: "123456789", UserID: "user-123", RoleID: "irrelevant-role"},
			want:     map[string]any{"ecctl.identity.account_id": "123456789", "ecctl.identity.user_id": "user-123"},
		},
		{
			identity: Identity{Hash: "role-hash", Type: "AssumedRoleUser", AccountID: "123456789", UserID: "session-specific-user", RoleID: "role-123"},
			want:     map[string]any{"ecctl.identity.account_id": "123456789", "ecctl.identity.role_id": "role-123"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.identity.Type, func(t *testing.T) {
			exporter := tracetest.NewInMemoryExporter()
			_, session := Start(WithExporterForTest(context.Background(), exporter), Options{
				Enabled: true, Surface: "public", ConfigPath: filepath.Join(t.TempDir(), "config.json"),
			})
			session.StartAPI(APIRequest{Service: "Ecs", API: "DescribeInstances"})(APIResult{})
			session.RegisterIdentity("test-credential", func(context.Context) (Identity, error) { return tc.identity, nil })
			session.Finish("ecctl ecs instance list", 0)
			spans := exporter.GetSpans()
			if len(spans) != 2 {
				t.Fatalf("spans = %d, want command and API spans", len(spans))
			}
			for _, span := range spans {
				attrs := attributeMap(span.Attributes)
				if span.Name == "ecctl.command" {
					if attrs["ecctl.identity.hash"] != tc.identity.Hash || attrs["ecctl.identity.type"] != tc.identity.Type {
						t.Fatalf("compatibility identity attributes = %#v", attrs)
					}
				}
				for _, key := range []string{"ecctl.identity.account_id", "ecctl.identity.user_id", "ecctl.identity.role_id"} {
					got, exists := attrs[key]
					want, wantExists := tc.want[key]
					if span.Name != "ecctl.command" {
						wantExists = false
					}
					if exists != wantExists || (wantExists && got != want) {
						t.Fatalf("%s %s = %#v (present=%t), want %#v (present=%t)", span.Name, key, got, exists, want, wantExists)
					}
				}
			}
		})
	}
}

func TestSessionEnabledStartOwnsIndependentRootAndProvider(t *testing.T) {
	parentExporter := tracetest.NewInMemoryExporter()
	parentCtx, parent := Start(WithExporterForTest(context.Background(), parentExporter), Options{
		Enabled: true, Surface: "public", ConfigPath: filepath.Join(t.TempDir(), "parent.json"),
	})
	childExporter := tracetest.NewInMemoryExporter()
	childCtx, child := Start(WithExporterForTest(parentCtx, childExporter), Options{
		Enabled: true, Surface: "public", ConfigPath: filepath.Join(t.TempDir(), "child.json"),
	})
	if child == nil || child == parent || FromContext(childCtx) != child {
		t.Fatalf("child session ownership is invalid: parent=%p child=%p context=%p", parent, child, FromContext(childCtx))
	}
	child.Finish("ecctl child", 0)
	if spans := childExporter.GetSpans(); len(spans) != 1 || spans[0].Parent.IsValid() {
		t.Fatalf("child root inherited ambient parent: %#v", spans)
	}
	if len(parentExporter.GetSpans()) != 0 {
		t.Fatal("finishing child unexpectedly finished parent")
	}
	parent.Finish("ecctl parent", 0)
	parentSpans := parentExporter.GetSpans()
	childSpans := childExporter.GetSpans()
	if len(parentSpans) != 1 || len(childSpans) != 1 || parentSpans[0].SpanContext.TraceID() == childSpans[0].SpanContext.TraceID() {
		t.Fatalf("roots are not independent: parent=%#v child=%#v", parentSpans, childSpans)
	}
}

func TestSessionFinishIsIdempotent(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	_, session := Start(WithExporterForTest(context.Background(), exporter), Options{
		Enabled: true, Surface: "public", ConfigPath: filepath.Join(t.TempDir(), "config.json"),
	})
	session.Finish("ecctl first", 0)
	session.Finish("ecctl second", 1)
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("Finish exported %d roots, want 1", len(spans))
	}
	if got := attributeMap(spans[0].Attributes)["ecctl.command.name"]; got != "ecctl first" {
		t.Fatalf("command name = %#v, want first Finish value", got)
	}
}

func attributeMap(attributes []attribute.KeyValue) map[string]any {
	out := make(map[string]any, len(attributes))
	for _, value := range attributes {
		out[string(value.Key)] = value.Value.AsInterface()
	}
	return out
}

func TestTruthy(t *testing.T) {
	for _, value := range []string{"1", "true", "TRUE", "yes", "on"} {
		if !Truthy(value) {
			t.Fatalf("Truthy(%q) = false", value)
		}
	}
	for _, value := range []string{"", "0", "false", "no", "off", "anything"} {
		if Truthy(value) {
			t.Fatalf("Truthy(%q) = true", value)
		}
	}
}

type blockingExporter struct{}

func (blockingExporter) ExportSpans(ctx context.Context, _ []sdktrace.ReadOnlySpan) error {
	<-ctx.Done()
	return ctx.Err()
}

func (blockingExporter) Shutdown(context.Context) error { return nil }

func TestFinishBoundsExporterFailure(t *testing.T) {
	_, session := Start(WithExporterForTest(context.Background(), blockingExporter{}), Options{
		Enabled: true, Surface: "public", Version: "test", ConfigPath: filepath.Join(t.TempDir(), "config.json"),
	})
	started := time.Now()
	session.Finish("ecctl --version", 0)
	if elapsed := time.Since(started); elapsed > 750*time.Millisecond {
		t.Fatalf("Finish took %s, want at most 750ms including scheduler tolerance", elapsed)
	}
}
