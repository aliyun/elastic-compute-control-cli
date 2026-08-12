package telemetryconfig

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/http/httpguts"
)

var (
	ErrEndpoint = errors.New("invalid telemetry endpoint configuration")
	ErrHeaders  = errors.New("invalid telemetry headers configuration")
)

type Config struct {
	Endpoint string
	Headers  map[string]string
}

// Decode validates release-injected telemetry configuration without ever
// including the secret input in returned errors.
func Decode(endpointB64, headersB64 string) (Config, error) {
	endpointRaw, err := base64.StdEncoding.DecodeString(endpointB64)
	if err != nil || len(endpointRaw) == 0 {
		return Config{}, ErrEndpoint
	}
	endpoint := string(endpointRaw)
	if endpoint != strings.TrimSpace(endpoint) {
		return Config{}, ErrEndpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || parsed.Opaque != "" || !validEndpointPort(parsed.Host) || !validAlibabaCloudEndpointHost(parsed.Hostname()) {
		return Config{}, ErrEndpoint
	}

	headersRaw, err := base64.StdEncoding.DecodeString(headersB64)
	if err != nil || len(headersRaw) == 0 {
		return Config{}, ErrHeaders
	}
	headers := map[string]string{}
	if err := json.Unmarshal(headersRaw, &headers); err != nil || headers == nil {
		return Config{}, ErrHeaders
	}
	seenHeaders := make(map[string]struct{}, len(headers))
	for name, value := range headers {
		canonicalName := strings.ToLower(name)
		if !httpguts.ValidHeaderFieldName(name) || !httpguts.ValidHeaderFieldValue(value) || reservedTelemetryHeader(canonicalName) {
			return Config{}, ErrHeaders
		}
		if _, exists := seenHeaders[canonicalName]; exists {
			return Config{}, ErrHeaders
		}
		seenHeaders[canonicalName] = struct{}{}
	}
	return Config{Endpoint: endpoint, Headers: headers}, nil
}

func validAlibabaCloudEndpointHost(host string) bool {
	if host == "" || host != strings.ToLower(host) || strings.HasSuffix(host, ".") || net.ParseIP(host) != nil {
		return false
	}
	for _, character := range host {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' && character != '.' {
			return false
		}
	}
	_, allowed := managedOpenTelemetryHosts[host]
	return allowed
}

// managedOpenTelemetryHosts is the exact public/private HTTPS endpoint
// inventory for Alibaba Cloud Managed Service for OpenTelemetry. Some public
// documentation still names tracing-analysis-dc-* hosts, but those hosts serve
// certificates for *.arms.aliyuncs.com. Use their TLS-covered DNS aliases and
// keep this list explicit: other tenant-controlled services also use
// aliyuncs.com hosts.
var managedOpenTelemetryHosts = map[string]struct{}{
	"tracing-cn-hangzhou-internal.arms.aliyuncs.com":    {},
	"tracing-cn-hangzhou.arms.aliyuncs.com":             {},
	"tracing-cn-shanghai-internal.arms.aliyuncs.com":    {},
	"tracing-cn-shanghai.arms.aliyuncs.com":             {},
	"tracing-cn-qingdao-internal.arms.aliyuncs.com":     {},
	"tracing-cn-qingdao.arms.aliyuncs.com":              {},
	"tracing-cn-beijing-internal.arms.aliyuncs.com":     {},
	"tracing-cn-beijing.arms.aliyuncs.com":              {},
	"tracing-cn-zhangjiakou-internal.arms.aliyuncs.com": {},
	"tracing-cn-zhangjiakou.arms.aliyuncs.com":          {},
	"tracing-cn-huhehaote-internal.arms.aliyuncs.com":   {},
	"tracing-cn-huhehaote.arms.aliyuncs.com":            {},
	"tracing-cn-wulanchabu-internal.arms.aliyuncs.com":  {},
	"tracing-cn-wulanchabu.arms.aliyuncs.com":           {},
	"tracing-cn-shenzhen-internal.arms.aliyuncs.com":    {},
	"tracing-cn-shenzhen.arms.aliyuncs.com":             {},
	"tracing-cn-heyuan-internal.arms.aliyuncs.com":      {},
	"tracing-cn-heyuan.arms.aliyuncs.com":               {},
	"tracing-cn-guangzhou-internal.arms.aliyuncs.com":   {},
	"tracing-cn-guangzhou.arms.aliyuncs.com":            {},
	"tracing-cn-chengdu-internal.arms.aliyuncs.com":     {},
	"tracing-cn-chengdu.arms.aliyuncs.com":              {},
	"tracing-cn-hongkong-internal.arms.aliyuncs.com":    {},
	"tracing-cn-hongkong.arms.aliyuncs.com":             {},
	"tracing-ap-northeast-1-internal.arms.aliyuncs.com": {},
	"tracing-ap-northeast-1.arms.aliyuncs.com":          {},
	"tracing-ap-southeast-1-internal.arms.aliyuncs.com": {},
	"tracing-ap-southeast-1.arms.aliyuncs.com":          {},
	"tracing-ap-southeast-3-internal.arms.aliyuncs.com": {},
	"tracing-ap-southeast-3.arms.aliyuncs.com":          {},
	"tracing-ap-southeast-5-internal.arms.aliyuncs.com": {},
	"tracing-ap-southeast-5.arms.aliyuncs.com":          {},
	"tracing-eu-central-1-internal.arms.aliyuncs.com":   {},
	"tracing-eu-central-1.arms.aliyuncs.com":            {},
	"tracing-eu-west-1-internal.arms.aliyuncs.com":      {},
	"tracing-eu-west-1.arms.aliyuncs.com":               {},
	"tracing-us-west-1-internal.arms.aliyuncs.com":      {},
	"tracing-us-west-1.arms.aliyuncs.com":               {},
	"tracing-us-east-1-internal.arms.aliyuncs.com":      {},
	"tracing-us-east-1.arms.aliyuncs.com":               {},
}

func reservedTelemetryHeader(name string) bool {
	switch name {
	case "connection", "content-encoding", "content-length", "content-type", "host", "keep-alive", "proxy-authenticate", "proxy-authorization", "proxy-connection", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}

func validEndpointPort(host string) bool {
	port := ""
	explicit := false
	if strings.HasPrefix(host, "[") {
		closing := strings.LastIndexByte(host, ']')
		if closing < 0 {
			return false
		}
		suffix := host[closing+1:]
		if suffix != "" {
			if !strings.HasPrefix(suffix, ":") {
				return false
			}
			port = suffix[1:]
			explicit = true
		}
	} else if separator := strings.LastIndexByte(host, ':'); separator >= 0 {
		if strings.Count(host, ":") != 1 {
			return false
		}
		port = host[separator+1:]
		explicit = true
	}
	if !explicit {
		return true
	}
	numeric, err := strconv.Atoi(port)
	return err == nil && numeric >= 1 && numeric <= 65535
}
