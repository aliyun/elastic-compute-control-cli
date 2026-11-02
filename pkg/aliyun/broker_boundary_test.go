package aliyun

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestCredentialBrokerUnavailableAndUnauthenticatedRequestsFailClosed(t *testing.T) {
	var missing *credentialBroker
	missing.Seed(nil)
	missing.Activate(nil)
	missing.setServeError(errors.New("ignored"))
	if missing.URL() != "" || missing.RefreshError() != nil || missing.Close() != nil {
		t.Fatal("nil broker returned state")
	}
	if _, err := missing.snapshot(context.Background()); err == nil {
		t.Fatal("nil broker returned credentials")
	}
	if _, err := missing.CommandArgs(nil, ""); err == nil {
		t.Fatal("nil broker wrote config")
	}
	if _, err := startCredentialBroker(nil, nil); err == nil {
		t.Fatal("nil source accepted")
	}
	if requestIsLoopback(nil) || requestIsLoopback(&http.Request{RemoteAddr: "invalid"}) {
		t.Fatal("unverifiable request treated as loopback")
	}
	broker, err := startCredentialBroker(nil, &staticCredentialAcquirer{snapshot: credentialSnapshot{AccessKeyID: "id", AccessKeySecret: "secret"}})
	if err != nil {
		t.Fatal(err)
	}
	defer broker.Close()
	parsed, err := url.Parse(broker.URL())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ method, remote string }{{http.MethodPost, "127.0.0.1:1234"}, {http.MethodGet, "192.0.2.1:1234"}} {
		request := httptest.NewRequest(test.method, parsed.Path, nil)
		request.RemoteAddr = test.remote
		response := httptest.NewRecorder()
		broker.server.Handler.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("unauthorized broker request=%d", response.Code)
		}
	}
	if _, err := broker.CommandArgs(nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := broker.CommandArgs(nil, ""); err == nil {
		t.Fatal("broker configuration created twice")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := (&credentialBroker{listener: listener}).Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := listener.Accept(); err == nil {
		t.Fatal("partial broker listener still open")
	}
}

func TestCredentialBrokerCanceledAndIncompleteRefreshes(t *testing.T) {
	disabled := &credentialBroker{acquirer: &staticCredentialAcquirer{}, ctx: context.Background()}
	if _, err := disabled.snapshot(context.Background()); err == nil {
		t.Fatal("inactive refresh succeeded")
	}
	for _, snapshot := range []*credentialSnapshot{nil, {AccessKeyID: "incomplete"}} {
		broker, err := startCredentialBroker(nil, credentialAcquirerFunc(func(context.Context) (*credentialSnapshot, error) { return snapshot, nil }))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := broker.snapshot(context.Background()); err == nil {
			t.Fatal("incomplete refresh succeeded")
		}
		if err := broker.Close(); err != nil {
			t.Fatal(err)
		}
	}
	broker, err := startCredentialBroker(nil, credentialAcquirerFunc(func(ctx context.Context) (*credentialSnapshot, error) { <-ctx.Done(); return nil, ctx.Err() }))
	if err != nil {
		t.Fatal(err)
	}
	defer broker.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := broker.snapshot(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("request cancellation lost: %v", err)
	}
}
