package main

import (
	"context"
	"io"
	"os"

	"ecctl/pkg/cli"
	_ "ecctl/specs/ack"
	_ "ecctl/specs/ecs"
	_ "ecctl/specs/tag"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return cli.Run(cli.WithFullCommandSurface(context.Background()), args, stdout, stderr)
}
