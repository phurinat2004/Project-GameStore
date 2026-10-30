package main

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"path"
	"sort"
)

//go:embed db/migrations/*.sql
var migrationFiles embed.FS

func runMigrations(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	files, err := fs.Glob(migrationFiles, "db/migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list database migrations: %w", err)
	}
	sort.Strings(files)
	for _, file := range files {
		if err := applyMigration(ctx, db, file); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration(ctx context.Context, db *sql.DB, file string) error {
	transaction, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", file, err)
	}
	defer func() {
		if err := transaction.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			log.Printf("rollback migration %s: %v", file, err)
		}
	}()

	var lockResult any
	if err := transaction.QueryRowContext(ctx, `SELECT pg_advisory_xact_lock(78192301)`).Scan(&lockResult); err != nil {
		return fmt.Errorf("lock migration %s: %w", file, err)
	}
	version := path.Base(file)
	var applied bool
	if err := transaction.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version,
	).Scan(&applied); err != nil {
		return fmt.Errorf("check migration %s: %w", file, err)
	}
	if applied {
		if err := transaction.Commit(); err != nil {
			return fmt.Errorf("finish migration check %s: %w", file, err)
		}
		return nil
	}

	script, err := migrationFiles.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", file, err)
	}
	if _, err := transaction.ExecContext(ctx, string(script)); err != nil {
		return fmt.Errorf("execute migration %s: %w", file, err)
	}
	if _, err := transaction.ExecContext(ctx,
		`INSERT INTO schema_migrations (version) VALUES ($1)`, version,
	); err != nil {
		return fmt.Errorf("record migration %s: %w", file, err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", file, err)
	}
	log.Printf("applied database migration %s", version)
	return nil
}
