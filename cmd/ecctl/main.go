package main

import (
	"context"
	"os"

	"ecctl/pkg/cli"
	_ "ecctl/specs/ack"
	_ "ecctl/specs/ecs"
	_ "ecctl/specs/tag"
)

func main() {
	os.Exit(cli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
