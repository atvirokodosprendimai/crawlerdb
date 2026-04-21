package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	broker "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/nats"
	"github.com/nats-io/nats.go"
	"github.com/urfave/cli/v3"
)

func exportCommand() *cli.Command {
	return &cli.Command{
		Name:  "export",
		Usage: "Export crawl data",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "job", Usage: "Job ID", Required: true},
			&cli.StringFlag{Name: "format", Aliases: []string{"f"}, Usage: "Export format (json, csv, sqlite, sitemap)", Value: "json"},
			&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Usage: "Output file path", Required: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			natsURL := cmd.Root().String("nats-url")
			conn, err := nats.Connect(natsURL)
			if err != nil {
				return fmt.Errorf("connect to NATS: %w", err)
			}
			defer conn.Close()

			b := broker.NewFromConn(conn)

			req := struct {
				JobID  string `json:"job_id"`
				Format string `json:"format"`
				Output string `json:"output"`
			}{
				JobID:  cmd.String("job"),
				Format: cmd.String("format"),
				Output: cmd.String("output"),
			}
			data, _ := json.Marshal(req)

			reply, err := b.Request(ctx, "job.export", data)
			if err != nil {
				return fmt.Errorf("export: %w", err)
			}

			fmt.Fprintln(os.Stdout, string(reply))
			return nil
		},
	}
}
