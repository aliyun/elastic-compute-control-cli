package e2bapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/pkg/config"
)

const (
	fcSandboxEndpointsURL = "https://api.aliyun.com/meta/v1/products/FCSandbox/endpoints.json"
	fcRegionLookupTimeout = 2 * time.Second
	maxFCRegionBodyLen    = 1 << 20
)

// The OpenAPI Portal publishes FCSandbox region metadata without authentication.
// Its endpoints are the OpenAPI control plane, not the E2B data plane: only use
// the region membership, and construct the E2B hostname locally.
func defaultFCDomain(ctx context.Context, region string) string {
	if !config.ValidRegion(region) || region == "cn-hangzhou" {
		return defaultDomain
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, fcRegionLookupTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fcSandboxEndpointsURL, nil)
	if err != nil {
		return defaultDomain
	}
	req.Header.Set("Accept", "application/json")
	// Keep metadata discovery independent of the sandbox API key and private CA.
	client := &http.Client{
		Timeout: fcRegionLookupTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return defaultDomain
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return defaultDomain
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxFCRegionBodyLen+1))
	if err != nil || len(raw) > maxFCRegionBodyLen {
		return defaultDomain
	}
	var result struct {
		Code *int `json:"code"`
		Data struct {
			Type      string `json:"type"`
			Endpoints []struct {
				RegionID string `json:"regionId"`
				Public   string `json:"public"`
			} `json:"endpoints"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Code == nil || *result.Code != 0 || result.Data.Type != "regional" {
		return defaultDomain
	}
	for _, endpoint := range result.Data.Endpoints {
		if endpoint.RegionID == region && endpoint.Public == "fcsandbox."+region+".aliyuncs.com" {
			return region + "." + fcDomain
		}
	}
	return defaultDomain
}
