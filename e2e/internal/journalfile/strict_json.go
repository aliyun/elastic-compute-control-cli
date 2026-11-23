package journalfile

import execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"

func unmarshalRecoveryJSON(data []byte, destination any) error {
	return execpkg.UnmarshalStrictJSON(data, destination)
}
