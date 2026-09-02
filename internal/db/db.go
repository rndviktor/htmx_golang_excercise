// Package db owns the SQLite database: connection, schema migration and
// default seed data. All queries are executed via the sqlc-generated
// package internal/sqlc/sqlite/db.
package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	sqlcemb "htmx-golang-excercise/internal/sqlc/sqlite"
	sqlite "htmx-golang-excercise/internal/sqlc/sqlite/db"
)

// IDs of the seed rows created by Open. Used by callers that don't yet
// have real multi-user support.
const (
	DefaultUserID        = 1
	DefaultServerGroupID = 1
)

// Open opens (creating if needed) the SQLite database at path, applies the
// schema, seeds the default user/server group, and returns a handle plus
// sqlc queries bound to it.
func Open(path string) (*sql.DB, *sqlite.Queries, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", path)
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, nil, err
	}

	// SQLite is single-writer; avoid SQLITE_BUSY under concurrent requests.
	database.SetMaxOpenConns(1)

	if err := database.Ping(); err != nil {
		database.Close()
		return nil, nil, err
	}

	if _, err := database.Exec(sqlcemb.Schema); err != nil {
		database.Close()
		return nil, nil, fmt.Errorf("apply schema: %w", err)
	}

	if err := migrate(database); err != nil {
		database.Close()
		return nil, nil, fmt.Errorf("migrate: %w", err)
	}

	if err := seedDefaults(database); err != nil {
		database.Close()
		return nil, nil, fmt.Errorf("seed defaults: %w", err)
	}

	return database, sqlite.New(database), nil
}

// migrate applies additive changes to tables that already exist. The base
// schema uses CREATE TABLE IF NOT EXISTS, which leaves pre-existing tables
// untouched, so columns added later are applied here idempotently.
func migrate(database *sql.DB) error {
	rows, err := database.Query(`SELECT name FROM pragma_table_info('query_history') WHERE name = 'tab_id'`)
	if err != nil {
		return err
	}
	exists := rows.Next()
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if !exists {
		if _, err := database.Exec(`ALTER TABLE query_history ADD COLUMN tab_id TEXT`); err != nil {
			return err
		}
	}

	rows2, err := database.Query(`SELECT name FROM pragma_table_info('query_history') WHERE name = 'rows_affected'`)
	if err != nil {
		return err
	}
	exists2 := rows2.Next()
	rows2.Close()
	if err := rows2.Err(); err != nil {
		return err
	}
	if !exists2 {
		if _, err := database.Exec(`ALTER TABLE query_history ADD COLUMN rows_affected INTEGER`); err != nil {
			return err
		}
	}

	return nil
}

// seedDefaults creates the demo admin user and its "Servers" group used
// until real user management exists.
func seedDefaults(database *sql.DB) error {
	_, err := database.Exec(`
		INSERT OR IGNORE INTO "user" (id, email, password, active)
		VALUES (?, 'admin', 'secret', 1);

		INSERT OR IGNORE INTO servergroup (id, user_id, name)
		VALUES (?, ?, 'Servers');
	`, DefaultUserID, DefaultServerGroupID, DefaultUserID)
	return err
}
