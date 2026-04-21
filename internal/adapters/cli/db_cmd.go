package cli

import (
	"context"
	"fmt"
	"os"

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
		},
	}
}
