package database

import (
	"database/sql"
	"errors"
	"io/fs"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

func TestMigrations(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite", ":memory:?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	require.Nil(t, err, "could not open database")

	migrationFS, err := fs.Sub(embedMigrations, "migrations")
	require.Nil(t, err, "could not sub migration fs")

	prov, err := goose.NewProvider("sqlite3", db, migrationFS)
	require.Nil(t, err, "could not create goose provider")

	result, err := prov.Up(t.Context())
	if err != nil {
		var partialError *goose.PartialError
		switch {
		case errors.As(err, &partialError):
			require.Nil(t, partialError.Err, "could not apply migrations")
		default:
			require.Nil(t, err, "could not apply migrations")
		}
		return
	}

	for _, r := range result {
		if r.Error != nil {
			require.Nilf(t, r.Error, "could not apply migration %s", r.Source.Path)
		}
	}

	require.Positive(t, len(result))

	result, err = prov.DownTo(t.Context(), 0)
	if err != nil {
		var partialError *goose.PartialError
		switch {
		case errors.As(err, &partialError):
			require.Nil(t, partialError.Err, "could not roll back migrations")
		default:
			require.Nil(t, err, "could not roll back migrations")
		}
		return
	}

	for _, r := range result {
		if r.Error != nil {
			require.Nilf(t, r.Error, "could not roll back migration %s", r.Source.Path)
		}
	}

	// check for leftover indexes
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type = 'index'")
	require.Nil(t, err)
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			require.Nil(t, err)
		}
	}(rows)

	var indexNames []string
	for rows.Next() {
		var name string
		err = rows.Scan(&name)
		require.Nil(t, err)
		indexNames = append(indexNames, name)
	}
	require.Nil(t, rows.Err())

	assert.Len(t, indexNames, 0, "found undeleted indexes")

	// check for leftover tables
	rows, err = db.Query("SELECT name FROM sqlite_master WHERE type = 'table' and name != 'goose_db_version' and name != 'sqlite_sequence'")
	require.Nil(t, err)
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			require.Nil(t, err)
		}
	}(rows)

	var tableNames []string
	for rows.Next() {
		var name string
		err = rows.Scan(&name)
		require.Nil(t, err)
		tableNames = append(tableNames, name)
	}
	require.Nil(t, rows.Err())

	assert.Len(t, tableNames, 0, "found undeleted tables")
}
