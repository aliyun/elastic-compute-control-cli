package aliyun

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/net/netutil"

	"github.com/aliyun/elastic-compute-control-cli/internal/configfile"
)

const brokerSyntheticExpiration = time.Minute
const credentialBrokerProfileName = "ecctl-broker"
const credentialBrokerAcquireWait = defaultExternalCredentialTimeout + 5*time.Second
const credentialBrokerCloseWait = 2 * time.Second
const credentialBrokerMaxConnections = 32

type credentialBroker struct {
	listener       net.Listener
	server         *http.Server
	url            string
	config         string
	ctx            context.Context
	cancel         context.CancelFunc
	acquirer       credentialAcquirer
	guard          *operationIdentityGuard
	acquire        sync.Mutex
	current        *credentialSnapshot
	refresh        chan struct{}
	refreshErr     error
	serveErr       error
	refreshEnabled bool
	wg             sync.WaitGroup
	close          sync.Once
	closeErr       error
}

func startCredentialBroker(ctx context.Context, acquirer credentialAcquirer, initial ...*credentialSnapshot) (*credentialBroker, error) {
	return startCredentialBrokerWithGuard(ctx, acquirer, nil, initial...)
}

func startCredentialBrokerWithGuard(ctx context.Context, acquirer credentialAcquirer, guard *operationIdentityGuard, initial ...*credentialSnapshot) (*credentialBroker, error) {
	if acquirer == nil {
		return nil, errors.New("credential source is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return nil, errors.New("credential broker token generation failed")
	}
	path := "/v1/credentials/" + hex.EncodeToString(token)
	rawListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("credential broker is unavailable")
	}
	listener := netutil.LimitListener(rawListener, credentialBrokerMaxConnections)
	brokerCtx, cancel := context.WithCancel(ctx)
	broker := &credentialBroker{listener: listener, ctx: brokerCtx, cancel: cancel, acquirer: acquirer, guard: guard, refreshEnabled: len(initial) == 0}
	if len(initial) > 0 {
		broker.Seed(initial[0])
	}
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != path || !requestIsLoopback(request) {
			http.NotFound(response, request)
			return
		}
		snapshot, acquireErr := broker.snapshot(request.Context())
		if acquireErr != nil || snapshot == nil || snapshot.AccessKeyID == "" || snapshot.AccessKeySecret == "" || snapshotExpired(snapshot, time.Now()) {
			broker.setServeError(acquireErr)
			response.Header().Set("Content-Type", "application/json")
			response.Header().Set("Cache-Control", "no-store")
			response.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(response, `{"Code":"Unavailable"}`)
			return
		}
		broker.setServeError(nil)
		expiration := snapshot.ExpiresAt
		if expiration.IsZero() {
			expiration = time.Now().Add(brokerSyntheticExpiration)
		}
		payload := map[string]string{
			"Code":            "Success",
			"AccessKeyId":     snapshot.AccessKeyID,
			"AccessKeySecret": snapshot.AccessKeySecret,
			"SecurityToken":   snapshot.SecurityToken,
			"Expiration":      expiration.UTC().Format(time.RFC3339),
		}
		response.Header().Set("Content-Type", "application/json")
		response.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(response).Encode(payload)
	})
	broker.server = &http.Server{
		Handler:           mux,
		ErrorLog:          log.New(io.Discard, "", 0),
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      credentialBrokerAcquireWait + 5*time.Second,
		IdleTimeout:       5 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}
	broker.url = "http://" + listener.Addr().String() + path
	go func() {
		_ = broker.server.Serve(listener)
	}()
	return broker, nil
}

func (b *credentialBroker) Seed(snapshot *credentialSnapshot) {
	if b == nil || snapshot == nil {
		return
	}
	seed := *snapshot
	b.acquire.Lock()
	b.current = &seed
	b.acquire.Unlock()
}

func (b *credentialBroker) Activate(snapshot *credentialSnapshot) {
	b.Seed(snapshot)
	if b == nil {
		return
	}
	b.acquire.Lock()
	b.refreshEnabled = true
	b.acquire.Unlock()
}

func (b *credentialBroker) snapshot(requestCtx context.Context) (*credentialSnapshot, error) {
	if b == nil || b.acquirer == nil {
		return nil, errors.New("credential broker is unavailable")
	}
	now := time.Now()
	b.acquire.Lock()
	current := b.current
	if current != nil && !snapshotExpired(current, now) {
		copy := *current
		if current.ExpiresAt.IsZero() || time.Until(current.ExpiresAt) <= credentialRefreshWindow {
			b.startRefreshLocked()
		}
		b.acquire.Unlock()
		return &copy, nil
	}
	b.startRefreshLocked()
	refresh := b.refresh
	b.acquire.Unlock()
	if refresh == nil {
		return nil, errors.New("credential refresh is unavailable")
	}
	timer := time.NewTimer(credentialBrokerAcquireWait)
	defer timer.Stop()
	select {
	case <-refresh:
	case <-requestCtx.Done():
		return nil, requestCtx.Err()
	case <-b.ctx.Done():
		return nil, b.ctx.Err()
	case <-timer.C:
		return nil, errors.New("credential refresh timed out")
	}
	b.acquire.Lock()
	defer b.acquire.Unlock()
	if b.current == nil || snapshotExpired(b.current, time.Now()) {
		if b.refreshErr != nil {
			return nil, b.refreshErr
		}
		return nil, errors.New("credential refresh failed")
	}
	copy := *b.current
	return &copy, nil
}

func (b *credentialBroker) startRefreshLocked() {
	if !b.refreshEnabled || b.refresh != nil {
		return
	}
	done := make(chan struct{})
	b.refresh = done
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		defer close(done)
		snapshot, err := b.acquirer.Acquire(b.ctx)
		if err == nil && b.guard != nil {
			err = b.guard.Validate(b.ctx, snapshot)
		}
		b.acquire.Lock()
		defer b.acquire.Unlock()
		b.refreshErr = err
		if err == nil && snapshot != nil && snapshot.AccessKeyID != "" && snapshot.AccessKeySecret != "" && !snapshotExpired(snapshot, time.Now()) {
			copy := *snapshot
			b.current = &copy
		}
		b.refresh = nil
	}()
}

func (b *credentialBroker) RefreshError() error {
	if b == nil {
		return nil
	}
	b.acquire.Lock()
	defer b.acquire.Unlock()
	return b.serveErr
}

func (b *credentialBroker) setServeError(err error) {
	if b == nil {
		return
	}
	b.acquire.Lock()
	b.serveErr = err
	b.acquire.Unlock()
}

func requestIsLoopback(request *http.Request) bool {
	if request == nil {
		return false
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func credentialBrokerCompatibleSnapshot(snapshot *credentialSnapshot) bool {
	return snapshot != nil && snapshot.AccessKeyID != "" && snapshot.AccessKeySecret != "" && snapshot.SecurityToken != "" && !snapshotExpired(snapshot, time.Now())
}

func credentialStaticExternalSnapshot(mode string, snapshot *credentialSnapshot) bool {
	return mode == credentialModeExternal && snapshot != nil && snapshot.AccessKeyID != "" && snapshot.AccessKeySecret != "" && snapshot.SecurityToken == "" && snapshot.ExpiresAt.IsZero()
}

func (b *credentialBroker) URL() string {
	if b == nil {
		return ""
	}
	return b.url
}

func (b *credentialBroker) CommandArgs(args []string, region string) ([]string, error) {
	if b == nil || b.url == "" {
		return nil, errors.New("credential broker is unavailable")
	}
	if b.config != "" {
		return nil, errors.New("credential broker configuration already exists")
	}
	payload := map[string]any{
		"current": credentialBrokerProfileName,
		"profiles": []any{map[string]any{
			"name": credentialBrokerProfileName, "mode": credentialModeCredentialsURI,
			"credentials_uri": b.url, "region_id": region, "output_format": "json", "language": "en",
		}},
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, errors.New("credential broker configuration is invalid")
	}
	temp, err := configfile.CreateSensitiveTemp("", "ecctl-oss-credentials-*.json")
	if err != nil {
		return nil, errors.New("credential broker configuration is unavailable")
	}
	tempPath := temp.Name()
	remove := true
	defer func() {
		if remove {
			_ = configfile.CleanupSensitiveTemp(tempPath)
		}
	}()
	closeWithError := func(current error) error {
		if closeErr := temp.Close(); current == nil {
			return closeErr
		}
		return current
	}
	if err := temp.Chmod(0o600); err != nil {
		return nil, closeWithError(err)
	}
	if _, err := temp.Write(append(raw, '\n')); err != nil {
		return nil, closeWithError(err)
	}
	if err := temp.Sync(); err != nil {
		return nil, closeWithError(err)
	}
	if err := temp.Close(); err != nil {
		return nil, err
	}
	b.config = tempPath
	remove = false
	configured := []string{"--config-path", tempPath, "--profile", credentialBrokerProfileName}
	configured = append(configured, args...)
	return configured, nil
}

func (b *credentialBroker) Close() error {
	if b == nil {
		return nil
	}
	b.close.Do(func() {
		if b.cancel != nil {
			b.cancel()
		}
		if b.server != nil {
			b.closeErr = errors.Join(b.closeErr, b.server.Close())
		} else if b.listener != nil {
			b.closeErr = errors.Join(b.closeErr, b.listener.Close())
		}
		if b.config != "" {
			b.closeErr = errors.Join(b.closeErr, configfile.CleanupSensitiveTemp(b.config))
			b.config = ""
		}
		waited := make(chan struct{})
		go func() {
			b.wg.Wait()
			close(waited)
		}()
		select {
		case <-waited:
		case <-time.After(credentialBrokerCloseWait):
			if b.closeErr == nil {
				b.closeErr = errors.New("credential broker refresh did not stop")
			}
		}
	})
	return b.closeErr
}
