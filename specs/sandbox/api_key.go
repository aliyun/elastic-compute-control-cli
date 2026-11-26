package sandbox

import spechooks "github.com/aliyun/elastic-compute-control-cli/specs"

func init() {
	// These nested provider objects are opaque JSON. Preserve their API keys
	// and array shape across detail and list probes, as the create/reset emit
	// path already does.
	for _, field := range []string{"ip_whitelist", "ip_blacklist", "permission_policy", "effective_permissions"} {
		spechooks.RegisterFieldMapper("sandbox", "api-key", field, func(value any) any { return value })
	}
}
