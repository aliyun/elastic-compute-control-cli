package sandbox

import spechooks "github.com/aliyun/elastic-compute-control-cli/specs"

func init() {
	// These nested provider configurations are opaque JSON objects. Preserve
	// their API keys and array shape across detail and list probes, as the
	// create emit path already does.
	for _, field := range []string{"oss_volume_config", "agentic_fs_volume_config", "agentic_bucket_volume_config", "mount_config"} {
		spechooks.RegisterFieldMapper("sandbox", "volume", field, func(value any) any { return value })
	}
}
