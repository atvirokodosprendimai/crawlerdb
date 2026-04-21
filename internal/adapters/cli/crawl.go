package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/config"
	broker "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/nats"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
	"github.com/nats-io/nats.go"
	"github.com/urfave/cli/v3"
)

func crawlCommand() *cli.Command {
	return &cli.Command{
		Name:  "crawl",
		Usage: "Manage crawl jobs",
		Commands: []*cli.Command{
			{
				Name:  "start",
				Usage: "Start a new crawl job",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "url", Usage: "Seed URL to crawl", Required: true},
					&cli.StringFlag{Name: "scope", Usage: "Crawl scope (same_domain, include_subdomains, follow_externals)", Value: "same_domain"},
					&cli.IntFlag{Name: "max-depth", Usage: "Maximum crawl depth", Value: 10},
					&cli.StringFlag{Name: "extraction", Usage: "Extraction level (minimal, standard, full)", Value: "standard"},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					natsURL := cmd.Root().String("nats-url")
					conn, err := nats.Connect(natsURL)
					if err != nil {
						return fmt.Errorf("connect to NATS: %w", err)
					}
					defer conn.Close()

					b := broker.NewFromConn(conn)

					cfg := valueobj.CrawlConfig{
						Scope:      valueobj.CrawlScope(cmd.String("scope")),
						MaxDepth:   int(cmd.Int("max-depth")),
						Extraction: valueobj.ExtractionLevel(cmd.String("extraction")),
					}

					// Load defaults from config file.
					cfgFile := cmd.Root().String("config")
					if appCfg, err := config.LoadFromFile(cfgFile); err == nil {
						cfg.UserAgent = appCfg.Crawler.UserAgent
					} else {
						cfg.UserAgent = "CrawlerDB/1.0"
					}

					req := struct {
						SeedURL string              `json:"seed_url"`
						Config  valueobj.CrawlConfig `json:"config"`
					}{
						SeedURL: cmd.String("url"),
						Config:  cfg,
					}
					data, _ := json.Marshal(req)

					reply, err := b.Request(ctx, "job.create", data)
					if err != nil {
						return fmt.Errorf("create job: %w", err)
					}

					var resp struct {
						JobID string `json:"job_id"`
						Error string `json:"error,omitempty"`
					}
					if err := json.Unmarshal(reply, &resp); err != nil {
						return fmt.Errorf("parse response: %w", err)
					}
					if resp.Error != "" {
						return fmt.Errorf("server error: %s", resp.Error)
					}

					fmt.Fprintf(os.Stdout, "Job created: %s\n", resp.JobID)
					return nil
				},
			},
			{
				Name:  "status",
				Usage: "Show job status",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "job", Usage: "Job ID", Required: true},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return jobCommand(ctx, cmd, "job.status")
				},
			},
			{
				Name:  "stop",
				Usage: "Stop a crawl job",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "job", Usage: "Job ID", Required: true},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return jobCommand(ctx, cmd, "job.stop")
				},
			},
			{
				Name:  "pause",
				Usage: "Pause a crawl job",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "job", Usage: "Job ID", Required: true},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return jobCommand(ctx, cmd, "job.pause")
				},
			},
			{
				Name:  "resume",
				Usage: "Resume a paused crawl job",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "job", Usage: "Job ID", Required: true},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return jobCommand(ctx, cmd, "job.resume")
				},
			},
			{
				Name:  "list",
				Usage: "List all crawl jobs",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					natsURL := cmd.Root().String("nats-url")
					conn, err := nats.Connect(natsURL)
					if err != nil {
						return fmt.Errorf("connect to NATS: %w", err)
					}
					defer conn.Close()

					b := broker.NewFromConn(conn)
					reply, err := b.Request(ctx, "job.list", []byte("{}"))
					if err != nil {
						return fmt.Errorf("list jobs: %w", err)
					}

					fmt.Fprintln(os.Stdout, string(reply))
					return nil
				},
			},
		},
	}
}

func jobCommand(ctx context.Context, cmd *cli.Command, subject string) error {
	natsURL := cmd.Root().String("nats-url")
	conn, err := nats.Connect(natsURL)
	if err != nil {
		return fmt.Errorf("connect to NATS: %w", err)
	}
	defer conn.Close()

	b := broker.NewFromConn(conn)

	req := struct {
		JobID string `json:"job_id"`
	}{JobID: cmd.String("job")}
	data, _ := json.Marshal(req)

	reply, err := b.Request(ctx, subject, data)
	if err != nil {
		return fmt.Errorf("%s: %w", subject, err)
	}

	fmt.Fprintln(os.Stdout, string(reply))
	return nil
}
