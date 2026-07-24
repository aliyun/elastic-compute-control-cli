package main

import (
	"context"
	"os"

	"github.com/aliyun/elastic-compute-control-cli/pkg/cli"
	_ "github.com/aliyun/elastic-compute-control-cli/specs/ack"
	_ "github.com/aliyun/elastic-compute-control-cli/specs/ecs"
	_ "github.com/aliyun/elastic-compute-control-cli/specs/rg"
	_ "github.com/aliyun/elastic-compute-control-cli/specs/tag"
)

func main() {
	os.Exit(cli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
