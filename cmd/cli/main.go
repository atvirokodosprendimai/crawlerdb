package main

import (
	"context"
	"log/slog"
	"os"

	adaptercli "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/cli"
)

func main() {
	if err := adaptercli.Run(context.Background(), os.Args); err != nil {
		slog.New(slog.NewTextHandler(os.Stderr, nil)).Error("cli error", "err", err)
		os.Exit(1)
	}
}
