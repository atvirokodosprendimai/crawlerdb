package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/config"
	"github.com/pelletier/go-toml/v2"
	"github.com/urfave/cli/v3"
)

func configCommand() *cli.Command {
	return &cli.Command{
		Name:  "config",
		Usage: "Manage configuration",
		Commands: []*cli.Command{
			{
				Name:  "init",
				Usage: "Generate default config file",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Usage: "Output path", Value: "configs/default.toml"},
				},
				Action: func(_ context.Context, cmd *cli.Command) error {
					path := cmd.String("output")
					if err := config.GenerateDefault(path); err != nil {
						return fmt.Errorf("generate config: %w", err)
					}
					fmt.Fprintf(os.Stdout, "Config written to %s\n", path)
					return nil
				},
			},
			{
				Name:  "show",
				Usage: "Show active configuration",
				Action: func(_ context.Context, cmd *cli.Command) error {
					cfgFile := cmd.Root().String("config")
					cfg, err := config.LoadFromFile(cfgFile)
					if err != nil {
						cfg = config.LoadDefault()
						fmt.Fprintln(os.Stderr, "Using defaults (config file not found)")
					}
					data, _ := toml.Marshal(cfg)
					fmt.Fprintln(os.Stdout, string(data))
					return nil
				},
			},
		},
	}
}
