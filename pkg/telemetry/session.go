package telemetry

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const (
	identityTimeout = 800 * time.Millisecond
	flushTimeout    = 500 * time.Millisecond
)

type sessionKey struct{}
type testExporterKey struct{}

type Options struct {
	Enabled    bool
	Surface    string
	Version    string
	ConfigPath string
}

type Identity struct {
	Hash string
	Type string
}

type IdentityResolver func(context.Context) (Identity, error)

type identityRegistration struct {
	accessKeyID string
	resolver    IdentityResolver
}

// Session owns a private provider and one command root span. It deliberately
// does not install a process-global TracerProvider.
type Session struct {
	provider   *sdktrace.TracerProvider
	root       trace.Span
	configPath string

	operation atomic.Uint64
	identity  sync.Once
	identityR identityRegistration
	finish    sync.Once
}

func Start(ctx context.Context, options Options) (context.Context, *Session) {
	ctx = context.WithValue(ctx, sessionKey{}, (*Session)(nil))
	if !options.Enabled || options.Surface != "public" {
		return ctx, nil
	}
	exporter, _ := ctx.Value(testExporterKey{}).(sdktrace.SpanExporter)
	if exporter != nil {
		exporter = retainedExporter{SpanExporter: exporter}
	} else {
		var err error
		exporter, err = newReleaseExporter(ctx)
		if err != nil {
			return ctx, nil
		}
	}
	return startWithExporter(ctx, options, exporter)
}

// WithExporterForTest supplies an in-process exporter while preserving Start's
// normal ownership and enablement rules.
func WithExporterForTest(ctx context.Context, exporter sdktrace.SpanExporter) context.Context {
	return context.WithValue(ctx, testExporterKey{}, exporter)
}

// retainedExporter lets integration tests inspect the final batch after the
// Session shuts its provider down. Production exporters still receive normal
// shutdown through Start.
type retainedExporter struct {
	sdktrace.SpanExporter
}

func (retainedExporter) Shutdown(context.Context) error { return nil }

func startWithExporter(ctx context.Context, options Options, exporter sdktrace.SpanExporter) (context.Context, *Session) {
	rootAttributes := []attribute.KeyValue{
		attribute.String("ecctl.surface", options.Surface),
		attribute.String("ecctl.version", options.Version),
		attribute.String("os.type", runtime.GOOS),
		attribute.String("host.arch", runtime.GOARCH),
	}
	if hash := activeInstallationHash(options.ConfigPath); hash != "" {
		rootAttributes = append(rootAttributes, attribute.String("ecctl.installation.hash", hash))
	}
	processor := sdktrace.NewBatchSpanProcessor(exporter)
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(processor),
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", "ecctl"))),
	)
	rootParent := trace.ContextWithSpanContext(ctx, trace.SpanContext{})
	rootCtx, root := provider.Tracer("github.com/aliyun/elastic-compute-control-cli/pkg/telemetry").Start(rootParent, "ecctl.command",
		trace.WithAttributes(rootAttributes...),
	)
	session := &Session{provider: provider, root: root, configPath: options.ConfigPath}
	return context.WithValue(rootCtx, sessionKey{}, session), session
}

func FromContext(ctx context.Context) *Session {
	if ctx == nil {
		return nil
	}
	session, _ := ctx.Value(sessionKey{}).(*Session)
	return session
}

func (s *Session) RegisterIdentity(accessKeyID string, resolver IdentityResolver) {
	if s == nil || accessKeyID == "" || resolver == nil {
		return
	}
	s.identity.Do(func() {
		s.identityR = identityRegistration{accessKeyID: accessKeyID, resolver: resolver}
	})
}

func (s *Session) NextOperationID() string {
	if s == nil || s.root == nil {
		return ""
	}
	n := s.operation.Add(1)
	return fmt.Sprintf("%s-%d", s.root.SpanContext().TraceID().String(), n)
}

type APIRequest struct {
	Service         string
	API             string
	APIVersion      string
	Transport       string
	OperationID     string
	Attempt         int
	RetryObservable bool
}

// StartAPI starts a direct child of the command root span. The returned
// function records only a coarse outcome; it never records the error itself.
func (s *Session) StartAPI(request APIRequest) func(error) {
	if s == nil || s.root == nil {
		return func(error) {}
	}
	parent := trace.ContextWithSpan(context.Background(), s.root)
	_, span := s.provider.Tracer("github.com/aliyun/elastic-compute-control-cli/pkg/telemetry").Start(parent, "ecctl.cloud.api.request",
		trace.WithAttributes(
			attribute.String("ecctl.cloud.service", request.Service),
			attribute.String("ecctl.cloud.api", request.API),
			attribute.String("ecctl.cloud.api_version", request.APIVersion),
			attribute.String("ecctl.cloud.transport", request.Transport),
			attribute.String("ecctl.cloud.operation_id", request.OperationID),
			attribute.Int("ecctl.cloud.attempt", request.Attempt),
			attribute.Bool("ecctl.cloud.retry", request.Attempt > 1),
			attribute.Bool("ecctl.cloud.retry_observable", request.RetryObservable),
		),
	)
	return func(err error) {
		outcome := "success"
		if err != nil {
			outcome = "error"
			span.SetStatus(codes.Error, "")
		}
		span.SetAttributes(attribute.String("ecctl.cloud.outcome", outcome))
		span.End()
	}
}

func (s *Session) Finish(command string, exitCode int) {
	if s == nil || s.root == nil || s.provider == nil {
		return
	}
	s.finish.Do(func() { s.finishOnce(command, exitCode) })
}

func (s *Session) finishOnce(command string, exitCode int) {
	if command == "" {
		command = "unknown"
	}
	if registration := s.identityR; registration.resolver != nil {
		ctx, cancel := context.WithTimeout(context.Background(), identityTimeout)
		if identity, err := resolveCachedIdentity(ctx, s.configPath, registration.accessKeyID, registration.resolver); err == nil && identity.Hash != "" && identity.Type != "" {
			s.root.SetAttributes(
				attribute.String("ecctl.identity.hash", identity.Hash),
				attribute.String("ecctl.identity.type", identity.Type),
			)
		}
		cancel()
	}
	outcome := "success"
	if exitCode != 0 {
		outcome = "error"
		s.root.SetStatus(codes.Error, "")
	}
	s.root.SetAttributes(
		attribute.String("ecctl.command.name", command),
		attribute.String("ecctl.command.outcome", outcome),
		attribute.Int("ecctl.command.exit_code", exitCode),
	)
	s.root.End()

	ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
	_ = s.provider.ForceFlush(ctx)
	_ = s.provider.Shutdown(ctx)
	cancel()
}

func Truthy(value string) bool {
	value = strings.TrimSpace(value)
	parsed, err := strconv.ParseBool(value)
	if err == nil {
		return parsed
	}
	switch strings.ToLower(value) {
	case "yes", "on":
		return true
	default:
		return false
	}
}
