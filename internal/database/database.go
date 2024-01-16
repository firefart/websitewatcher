package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/firefart/websitewatcher/internal/config"

	_ "modernc.org/sqlite"
)

const create string = `
	CREATE TABLE IF NOT EXISTS WATCHES (
		ID INTEGER NOT NULL PRIMARY KEY,
		NAME TEXT NOT NULL,
		URL TEXT NOT NULL,
		LAST_FETCH DATETIME,
		LAST_CONTENT BLOB
	);
	CREATE UNIQUE INDEX IF NOT EXISTS IDX_NAME_URL
	ON WATCHES(NAME, URL);
`

var ErrNotFound = errors.New("url not found in database")

type Database struct {
	db *sql.DB
}

func New(configuration config.Configuration) (*Database, error) {
	db, err := sql.Open("sqlite", fmt.Sprintf("%s?_pragma=journal_mode(WAL)", configuration.Database))
	if err != nil {
		return nil, fmt.Errorf("could not open database %s: %w", configuration.Database, err)
	}
	if _, err := db.Exec(create); err != nil {
		return nil, fmt.Errorf("could not create tables: %w", err)
	}
	return &Database{
		db: db,
	}, nil
}

func (db *Database) Close() error {
	return db.db.Close()
}

func (db *Database) GetLastContent(ctx context.Context, name, url string) (int64, []byte, error) {
	row := db.db.QueryRowContext(ctx, "SELECT ID, LAST_CONTENT FROM WATCHES WHERE NAME=? AND URL=?", name, url)
	var lastContent []byte
	var id int64
	err := row.Scan(&id, &lastContent)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return -1, nil, ErrNotFound
		}
		return -1, nil, fmt.Errorf("error on select last content: %w", err)
	}
	if err := row.Err(); err != nil {
		return -1, nil, fmt.Errorf("error on close last content: %w", err)
	}
	return id, lastContent, nil
}

func (db *Database) InsertLastContent(ctx context.Context, name, url string, content []byte) (int64, error) {
	res, err := db.db.ExecContext(ctx, "INSERT INTO WATCHES(NAME, URL, LAST_FETCH, LAST_CONTENT) VALUES(?,?,CURRENT_TIMESTAMP,?);", name, url, content)
	if err != nil {
		return -1, fmt.Errorf("error on insert: %w", err)
	}
	dbID, err := res.LastInsertId()
	if err != nil {
		return -1, fmt.Errorf("error on insert lastinsertid: %w", err)
	}
	return dbID, nil
}

func (db *Database) UpdateLastContent(ctx context.Context, id int64, content []byte) error {
	res, err := db.db.ExecContext(ctx, "UPDATE WATCHES SET LAST_FETCH=CURRENT_TIMESTAMP, LAST_CONTENT=? WHERE ID=?;", content, id)
	if err != nil {
		return fmt.Errorf("error on update: %w", err)
	}
	if _, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("error on rows affected")
	}
	return nil
}

// PrepareDatabase cleans up old entries and returns new ones
func (db *Database) PrepareDatabase(ctx context.Context, c config.Configuration) ([]config.WatchConfig, int64, error) {
	var newWatches []config.WatchConfig
	var foundIDs []any // needs to be any, so we can pass it to execcontext
	var rowsAffected int64

	for _, c := range c.Watches {
		row := db.db.QueryRowContext(ctx, "SELECT ID FROM WATCHES WHERE NAME=? AND URL=?", c.Name, c.URL)
		var id int64
		if err := row.Scan(&id); err != nil {
			// new entry not yet fetched. add to array and continue with the next config entry
			if errors.Is(err, sql.ErrNoRows) {
				newWatches = append(newWatches, c)
				continue
			}
			return nil, rowsAffected, fmt.Errorf("error on select: %w", err)
		}
		foundIDs = append(foundIDs, id)
	}

	if len(foundIDs) > 0 {
		// remove all items in database that have no corresponding config entry (==remove old items)
		query := fmt.Sprintf("DELETE FROM WATCHES WHERE ID NOT IN (?%s)", strings.Repeat(",?", len(foundIDs)-1))
		res, err := db.db.ExecContext(ctx, query, foundIDs...)
		if err != nil {
			return nil, rowsAffected, fmt.Errorf("error on select in: %w", err)
		}
		rowsAffected, err = res.RowsAffected()
		if err != nil {
			return nil, rowsAffected, fmt.Errorf("error on rowsaffected: %w", err)
		}
	}

	return newWatches, rowsAffected, nil
}
