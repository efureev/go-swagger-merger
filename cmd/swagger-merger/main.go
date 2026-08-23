// Command swagger-merger merges OpenAPI and Swagger documents into one.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/efureev/go-swagger-merger/v2/internal/cli"
)

// Stamped at link time; see the Makefile and .goreleaser.yaml.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	// os.Exit lives in its own frame so that run's deferred signal teardown
	// actually happens.
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return cli.Run(ctx, os.Args[1:],
		cli.IO{In: os.Stdin, Out: os.Stdout, Err: os.Stderr},
		cli.BuildInfo{Version: version, Commit: commit, Date: date},
	)
}
