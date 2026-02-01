// Package db provides PostgreSQL database connectivity for the RFP platform.
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps a pgx connection pool with helper methods.
type DB struct {
	Pool *pgxpool.Pool
}

// Querier is an interface for database query methods, implemented by both
// *pgxpool.Pool and pgx.Tx, allowing code to work with either.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Connect creates a new database connection pool from the given URL.
func Connect(ctx context.Context, databaseURL string) (*DB, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("database URL is required")
	}

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Verify connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{Pool: pool}, nil
}

// Close closes the connection pool.
func (db *DB) Close() {
	db.Pool.Close()
}

// Exec executes a query that doesn't return rows.
func (db *DB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return db.Pool.Exec(ctx, sql, args...)
}

// Query executes a query that returns rows.
func (db *DB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return db.Pool.Query(ctx, sql, args...)
}

// QueryRow executes a query that returns at most one row.
func (db *DB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return db.Pool.QueryRow(ctx, sql, args...)
}

// BeginTx starts a new transaction.
func (db *DB) BeginTx(ctx context.Context) (pgx.Tx, error) {
	return db.Pool.Begin(ctx)
}

// WithTx executes a function within a transaction. If the function returns
// an error, the transaction is rolled back. Otherwise, it's committed.
func (db *DB) WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return fmt.Errorf("tx error: %w, rollback error: %v", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
