package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func BenchmarkRunRootHelp(b *testing.B) {
	benchmarkRun(b, "--lang", "en", "--help")
}

func BenchmarkRunProductHelp(b *testing.B) {
	benchmarkRun(b, "--lang", "en", "vpc", "--help")
}

func BenchmarkRunActionHelp(b *testing.B) {
	benchmarkRun(b, "--lang", "en", "vpc", "list", "--help")
}

func BenchmarkRunSchemaListVPC(b *testing.B) {
	benchmarkRun(b, "--lang", "en", "schema", "--list", "vpc")
}

func BenchmarkRunColdRootHelp(b *testing.B) {
	benchmarkRunColdSpecCache(b, "--lang", "en", "--help")
}

func BenchmarkRunColdActionHelp(b *testing.B) {
	benchmarkRunColdSpecCache(b, "--lang", "en", "vpc", "list", "--help")
}

func BenchmarkRunECSInstanceHelp(b *testing.B) {
	benchmarkRun(b, "--lang", "en", "ecs", "instance", "--help")
}

func BenchmarkRunECSInstanceActionHelp(b *testing.B) {
	benchmarkRun(b, "--lang", "en", "ecs", "instance", "list", "--help")
}

func benchmarkRun(b *testing.B, args ...string) {
	b.Helper()
	b.ReportAllocs()
	benchmarkEnv(b)
	spec.ResetCacheForTest()
	runBenchmarkCLI(b, args...)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		runBenchmarkCLI(b, args...)
	}
}

func benchmarkRunColdSpecCache(b *testing.B, args ...string) {
	b.Helper()
	b.ReportAllocs()
	benchmarkEnv(b)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		spec.ResetCacheForTest()
		runBenchmarkCLI(b, args...)
	}
}

func benchmarkEnv(b *testing.B) {
	b.Helper()
	dir := b.TempDir()
	b.Setenv("ECCTL_CONFIG_PATH", filepath.Join(dir, "missing-ecctl-config.json"))
	b.Setenv("ECCTL_ALIYUN_CONFIG_PATH", filepath.Join(dir, "missing-aliyun-config.json"))
	b.Setenv("ECCTL_SPEC_DIR", "")
	b.Setenv("ECCTL_REGION", "")
	b.Setenv("ECCTL_PROFILE", "")
}

func runBenchmarkCLI(b *testing.B, args ...string) {
	b.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run(context.Background(), args, &stdout, &stderr); code != 0 {
		b.Fatalf("Run(%v) exit %d stdout=%s stderr=%s", args, code, stdout.String(), stderr.String())
	}
}
