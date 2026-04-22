package store

import (
	"database/sql"
	"fmt"

	"github.com/atvirokodosprendimai/crawlerdb/internal/adapters/db/migrations"
	"github.com/glebarez/sqlite"
	"github.com/pressly/goose/v3"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open creates a GORM DB connection to SQLite.
func Open(dbPath string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Enable WAL mode for better concurrency.
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB: %w", err)
	}
	if _, err := sqlDB.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("enable WAL: %w", err)
	}
	if _, err := sqlDB.Exec("PRAGMA busy_timeout=5000"); err != nil {
		return nil, fmt.Errorf("enable busy timeout: %w", err)
	}
	if _, err := sqlDB.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	return db, nil
}

// Migrate runs all pending goose migrations.
func Migrate(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get sql.DB: %w", err)
	}
	return RunMigrations(sqlDB)
}

// RunMigrations runs goose migrations using the embedded SQL files.
func RunMigrations(db *sql.DB) error {
	goose.SetBaseFS(migrations.FS)

	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}

	if err := goose.Up(db, "."); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	return nil
}

// MigrationStatus returns current migration status.
func MigrationStatus(db *sql.DB) error {
	goose.SetBaseFS(migrations.FS)

	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}

	return goose.Status(db, ".")
}
