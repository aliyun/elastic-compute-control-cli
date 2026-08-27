package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	ecsMetadataEndpoint      = "http://100.100.100.200"
	ecsMetadataTokenTTL      = "21600"
	ecsMetadataResponseLimit = 1 << 20
)

type ecsMetadataCredentialResponse struct {
	Code            string `json:"Code"`
	AccessKeyID     string `json:"AccessKeyId"`
	AccessKeySecret string `json:"AccessKeySecret"`
	SecurityToken   string `json:"SecurityToken"`
	Expiration      string `json:"Expiration"`
}

func acquireECSMetadataCredential(ctx context.Context, configuredRole string, disableIMDSv1 bool, client credentialHTTPClient) (*credentialSnapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if client == nil {
		client = newECSMetadataHTTPClient()
	}
	token, tokenErr := fetchECSMetadataToken(ctx, client)
	if tokenErr != nil && disableIMDSv1 {
		return nil, tokenErr
	}
	if tokenErr != nil {
		token = ""
	}
	roleName := strings.TrimSpace(configuredRole)
	if roleName == "" {
		body, err := fetchECSMetadata(ctx, client, "/latest/meta-data/ram/security-credentials/", token)
		if err != nil && token != "" && !disableIMDSv1 {
			body, err = fetchECSMetadata(ctx, client, "/latest/meta-data/ram/security-credentials/", "")
		}
		if err != nil {
			return nil, fmt.Errorf("get ECS RAM role name: %w", err)
		}
		roleName = strings.TrimSpace(string(body))
		if roleName == "" || strings.ContainsAny(roleName, "/\\\r\n\x00") {
			return nil, errors.New("ECS RAM role name is invalid")
		}
	}
	path := "/latest/meta-data/ram/security-credentials/" + url.PathEscape(roleName)
	body, err := fetchECSMetadata(ctx, client, path, token)
	if err != nil && token != "" && !disableIMDSv1 {
		body, err = fetchECSMetadata(ctx, client, path, "")
	}
	if err != nil {
		return nil, fmt.Errorf("get ECS RAM role credential: %w", err)
	}
	var payload ecsMetadataCredentialResponse
	if json.Unmarshal(body, &payload) != nil || payload.Code != "Success" || payload.AccessKeyID == "" || payload.AccessKeySecret == "" || payload.SecurityToken == "" || payload.Expiration == "" {
		return nil, errors.New("ECS metadata returned incomplete credentials")
	}
	expiresAt, err := time.Parse(time.RFC3339, payload.Expiration)
	if err != nil || !expiresAt.After(time.Now()) {
		return nil, errors.New("ECS metadata returned invalid credential expiration")
	}
	return &credentialSnapshot{
		AccessKeyID: payload.AccessKeyID, AccessKeySecret: payload.AccessKeySecret,
		SecurityToken: payload.SecurityToken, ProviderName: "ecs_ram_role", Type: "sts", ExpiresAt: expiresAt,
	}, nil
}

func fetchECSMetadataToken(ctx context.Context, client credentialHTTPClient) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, ecsMetadataEndpoint+"/latest/api/token", http.NoBody)
	if err != nil {
		return "", err
	}
	request.Header.Set("X-aliyun-ecs-metadata-token-ttl-seconds", ecsMetadataTokenTTL)
	body, status, err := doECSMetadataRequest(client, request)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK || strings.TrimSpace(string(body)) == "" {
		return "", fmt.Errorf("IMDSv2 token request returned HTTP %d", status)
	}
	return strings.TrimSpace(string(body)), nil
}

func fetchECSMetadata(ctx context.Context, client credentialHTTPClient, path, token string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, ecsMetadataEndpoint+path, http.NoBody)
	if err != nil {
		return nil, err
	}
	if token != "" {
		request.Header.Set("x-aliyun-ecs-metadata-token", token)
	}
	body, status, err := doECSMetadataRequest(client, request)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("metadata request returned HTTP %d", status)
	}
	return body, nil
}

func doECSMetadataRequest(client credentialHTTPClient, request *http.Request) ([]byte, int, error) {
	response, err := client.Do(request)
	if err != nil {
		if request.Context().Err() != nil {
			return nil, 0, request.Context().Err()
		}
		return nil, 0, errors.New("ECS metadata request failed")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, ecsMetadataResponseLimit+1))
	if err != nil || len(body) > ecsMetadataResponseLimit {
		return nil, response.StatusCode, errors.New("ECS metadata response is unreadable or too large")
	}
	return body, response.StatusCode, nil
}

func newECSMetadataHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout: time.Second,
		}).DialContext,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
