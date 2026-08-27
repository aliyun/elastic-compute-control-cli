package aliyun

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	credentialproviders "github.com/aliyun/credentials-go/credentials/providers"
)

func TestCredentialBrokerSatisfiesOfficialCredentialsURIRefreshContract(t *testing.T) {
	acquirer := &countingCredentialAcquirer{snapshots: []credentialSnapshot{
		{AccessKeyID: "id-one", AccessKeySecret: "secret-one", SecurityToken: "sts-one", Type: "sts"},
		{AccessKeyID: "id-two", AccessKeySecret: "secret-two", SecurityToken: "sts-two", Type: "sts"},
	}}
	broker, err := startCredentialBroker(context.Background(), acquirer)
	if err != nil {
		t.Fatal(err)
	}
	args, err := broker.CommandArgs([]string{"--auto-plugin-install", "false", "ossutil"}, "cn-hangzhou")
	if err != nil {
		t.Fatal(err)
	}
	if len(args) < 7 || args[0] != "--config-path" || args[2] != "--profile" || args[3] != credentialBrokerProfileName {
		t.Fatalf("broker command args = %#v", args)
	}
	configPath := args[1]
	info, err := os.Stat(configPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("broker config mode=%v err=%v", info.Mode().Perm(), err)
	}
	provider, err := credentialproviders.NewURLCredentialsProviderBuilder().WithUrl(broker.URL()).Build()
	if err != nil {
		t.Fatal(err)
	}
	first, err := provider.GetCredentials()
	if err != nil {
		t.Fatal(err)
	}
	second, err := provider.GetCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if first.AccessKeyId != "id-one" || second.AccessKeyId != "id-one" {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	broker.acquire.Lock()
	refresh := broker.refresh
	broker.acquire.Unlock()
	if refresh != nil {
		select {
		case <-refresh:
		case <-time.After(time.Second):
			t.Fatal("credential broker refresh did not finish")
		}
	}
	third, err := provider.GetCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if third.AccessKeyId != "id-two" {
		t.Fatalf("third=%#v", third)
	}
	if err := broker.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("broker config remained after close: %v", err)
	}
}

func TestCredentialBrokerFailureIsSecretFreeAndCloseStopsListener(t *testing.T) {
	acquirer := &countingCredentialAcquirer{err: context.DeadlineExceeded}
	broker, err := startCredentialBroker(context.Background(), acquirer)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(broker.URL())
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable || strings.Contains(string(body), "deadline") || strings.Contains(string(body), "credential") {
		t.Fatalf("failure response status=%d body=%q", response.StatusCode, body)
	}
	url := broker.URL()
	if err := broker.Close(); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 200 * time.Millisecond}
	if result, getErr := client.Get(url); getErr == nil {
		_ = result.Body.Close()
		t.Fatal("closed credential broker remained reachable")
	}
}

func TestCredentialBrokerBoundsHTTPConnections(t *testing.T) {
	broker, err := startCredentialBroker(context.Background(), &countingCredentialAcquirer{snapshot: credentialSnapshot{
		AccessKeyID: "id", AccessKeySecret: "secret", SecurityToken: "sts", Type: "sts",
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer broker.Close()
	if broker.server.ReadHeaderTimeout <= 0 || broker.server.ReadTimeout <= 0 || broker.server.WriteTimeout <= 0 || broker.server.IdleTimeout <= 0 || broker.server.MaxHeaderBytes <= 0 {
		t.Fatalf("unbounded broker server: %#v", broker.server)
	}
	if broker.server.WriteTimeout <= defaultExternalCredentialTimeout {
		t.Fatalf("broker write timeout %s cannot cover external credential timeout %s", broker.server.WriteTimeout, defaultExternalCredentialTimeout)
	}
}

func TestCredentialBrokerCloseCancelsInFlightRefresh(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	acquirer := credentialAcquirerFunc(func(ctx context.Context) (*credentialSnapshot, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return nil, ctx.Err()
	})
	broker, err := startCredentialBroker(context.Background(), acquirer)
	if err != nil {
		t.Fatal(err)
	}
	requestCtx, cancel := context.WithCancel(context.Background())
	request, _ := http.NewRequestWithContext(requestCtx, http.MethodGet, broker.URL(), nil)
	result := make(chan error, 1)
	go func() {
		response, err := http.DefaultClient.Do(request)
		if response != nil {
			_ = response.Body.Close()
		}
		result <- err
	}()
	<-started
	cancel()
	<-result
	if err := broker.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("broker refresh survived Close")
	}
}

func TestCredentialBrokerPreservesTypedRefreshError(t *testing.T) {
	sentinel := errors.New("typed provider failure")
	broker, err := startCredentialBroker(context.Background(), credentialAcquirerFunc(func(context.Context) (*credentialSnapshot, error) {
		return nil, sentinel
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer broker.Close()
	response, err := http.Get(broker.URL())
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable || !errors.Is(broker.RefreshError(), sentinel) {
		t.Fatalf("status=%d refresh error=%v", response.StatusCode, broker.RefreshError())
	}
}

func TestCredentialBrokerCompatibilitySeparatesExternalAKFromSTS(t *testing.T) {
	if credentialBrokerCompatibleSnapshot(&credentialSnapshot{AccessKeyID: "id", AccessKeySecret: "secret", Type: "access_key"}) {
		t.Fatal("static AK was accepted by the STS CredentialsURI broker")
	}
	if !credentialStaticExternalSnapshot(credentialModeExternal, &credentialSnapshot{AccessKeyID: "id", AccessKeySecret: "secret", Type: "access_key"}) {
		t.Fatal("External static AK was not recognized")
	}
	if !credentialBrokerCompatibleSnapshot(&credentialSnapshot{AccessKeyID: "id", AccessKeySecret: "secret", SecurityToken: "sts", Type: "sts"}) {
		t.Fatal("STS snapshot was rejected by the credential broker")
	}
	if credentialStaticExternalSnapshot(credentialModeExternal, &credentialSnapshot{AccessKeyID: "id", AccessKeySecret: "secret", Type: "access_key", ExpiresAt: time.Now().Add(time.Minute)}) {
		t.Fatal("expiring External AK was treated as operation-static")
	}
}
