package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

// Version is set at build time.
var Version = "dev"

// NewApp creates the root CLI application.
func NewApp() *cli.Command {
	return &cli.Command{
		Name:    "crawlerdb",
		Usage:   "Modular recursive web crawler",
		Version: Version,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Usage:   "Path to config file",
				Value:   "configs/default.toml",
			},
			&cli.StringFlag{
				Name:    "nats-url",
				Usage:   "NATS server URL",
				Value:   "nats://localhost:4222",
			},
			&cli.StringFlag{
				Name:    "db-path",
				Usage:   "SQLite database path",
				Value:   "crawlerdb.sqlite",
			},
		},
		Commands: []*cli.Command{
			crawlCommand(),
			configCommand(),
			dbCommand(),
			exportCommand(),
		},
	}
}

// Run executes the CLI application.
func Run(ctx context.Context, args []string) error {
	app := NewApp()
	if err := app.Run(ctx, args); err != nil {
		return fmt.Errorf("cli: %w", err)
	}
	return nil
}
