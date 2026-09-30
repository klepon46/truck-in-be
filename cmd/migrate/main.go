package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"truckin-be/internal/config"
)

func main() {
	if len(os.Args) < 2 {
		slog.Error("migration command is required")
		os.Exit(2)
	}

	context, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	applicationConfig, err := config.Load(context)
	if err != nil {
		slog.Error("load configuration", "error", err)
		os.Exit(1)
	}

	migrationsPath := os.Getenv("MIGRATIONS_PATH")
	if migrationsPath == "" {
		migrationsPath = "migrations"
	}
	absPath, err := filepath.Abs(migrationsPath)
	if err != nil {
		slog.Error("resolve migrations path", "error", err)
		os.Exit(1)
	}

	migration, err := migrate.New("file://"+filepath.ToSlash(absPath), applicationConfig.PostgresDSN)
	if err != nil {
		slog.Error("initialize migration", "error", err)
		os.Exit(1)
	}
	defer migration.Close()

	err = run(migration, os.Args[1:])
	if errors.Is(err, migrate.ErrNoChange) {
		slog.Info("database is already at the requested migration version")
		return
	}
	if err != nil {
		slog.Error("run migration", "error", err)
		os.Exit(1)
	}
}

func run(migration *migrate.Migrate, arguments []string) error {
	switch arguments[0] {
	case "up":
		return migration.Up()
	case "down":
		steps := 1
		if len(arguments) == 2 {
			parsed, err := strconv.Atoi(arguments[1])
			if err != nil || parsed < 1 {
				return fmt.Errorf("down steps must be a positive integer")
			}
			steps = parsed
		}
		return migration.Steps(-steps)
	default:
		return fmt.Errorf("unsupported migration command %q", arguments[0])
	}
}
