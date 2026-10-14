package sandbox

import spechooks "github.com/aliyun/elastic-compute-control-cli/specs"

func init() {
	// Native configuration is an API-shaped JSON document. In particular, a
	// one-member object containing an array is not a list-response wrapper.
	// The provider has already scrubbed sensitive configuration values.
	spechooks.RegisterFieldMapper("sandbox", "template", "runtime_config", func(value any) any { return value })
}
