package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	store "github.com/atvirokodosprendimai/crawlerdb/internal/adapters/db/gorm"
	"github.com/urfave/cli/v3"
)

func dbCommand() *cli.Command {
	return &cli.Command{
		Name:  "db",
		Usage: "Database management",
		Commands: []*cli.Command{
			{
				Name:  "migrate",
				Usage: "Run database migrations",
				Action: func(_ context.Context, cmd *cli.Command) error {
					dbPath := cmd.Root().String("db-path")
					db, err := store.Open(dbPath)
					if err != nil {
						return fmt.Errorf("open database: %w", err)
					}
					if err := store.Migrate(db); err != nil {
						return fmt.Errorf("migrate: %w", err)
					}
					fmt.Fprintln(os.Stdout, "Migrations complete")
					return nil
				},
			},
			{
				Name:  "status",
				Usage: "Show migration status",
				Action: func(_ context.Context, cmd *cli.Command) error {
					dbPath := cmd.Root().String("db-path")
					db, err := store.Open(dbPath)
					if err != nil {
						return fmt.Errorf("open database: %w", err)
					}
					sqlDB, err := db.DB()
					if err != nil {
						return fmt.Errorf("get sql.DB: %w", err)
					}
					return store.MigrationStatus(sqlDB)
				},
			},
			{
				Name:  "sweep-deleted",
				Usage: "Delete jobs marked for deletion before cutoff",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "before",
						Usage: "Delete jobs marked at least this long ago",
						Value: "24h",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					age, err := time.ParseDuration(cmd.String("before"))
					if err != nil {
						return fmt.Errorf("parse before duration: %w", err)
					}

					dbPath := cmd.Root().String("db-path")
					db, err := store.Open(dbPath)
					if err != nil {
						return fmt.Errorf("open database: %w", err)
					}
					jobRepo := store.NewJobRepository(db)
					jobs, err := jobRepo.FindMarkedForDeletionBefore(ctx, time.Now().Add(-age))
					if err != nil {
						return fmt.Errorf("find jobs marked for deletion: %w", err)
					}

					deleted := 0
					for _, job := range jobs {
						if err := jobRepo.DeleteCascade(ctx, job.ID); err != nil {
							return fmt.Errorf("delete job %s: %w", job.ID, err)
						}
						deleted++
					}

					fmt.Fprintf(os.Stdout, "Deleted %d marked jobs\n", deleted)
					return nil
				},
			},
		},
	}
}
