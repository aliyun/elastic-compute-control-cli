package main

import (
	"context"
	"io"
	"os"

	"github.com/aliyun/elastic-compute-control-cli/pkg/cli"
	_ "github.com/aliyun/elastic-compute-control-cli/specs/ack"
	_ "github.com/aliyun/elastic-compute-control-cli/specs/ecs"
	_ "github.com/aliyun/elastic-compute-control-cli/specs/tag"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return cli.Run(cli.WithFullCommandSurface(context.Background()), args, stdout, stderr)
}
