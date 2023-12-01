package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/firefart/websitewatcher/internal/config"
	"github.com/firefart/websitewatcher/internal/logger"

	_ "github.com/mattn/go-sqlite3"
)

const create string = `
  CREATE TABLE IF NOT EXISTS WATCHES (
  ID INTEGER NOT NULL PRIMARY KEY,
	URL TEXT NOT NULL,
  LAST_FETCH DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  LAST_CONTENT BLOB
  );
	CREATE INDEX IF NOT EXISTS IDX_URL
  ON WATCHES(URL);
	`

var ErrNotFound = errors.New("url not found in database")

type Database struct {
	db *sql.DB
}

type DBWatch struct {
	ID          int64
	URL         string
	LastFetch   time.Time
	LastContent []byte
}

func New(configuration config.Configuration) (*Database, error) {
	db, err := sql.Open("sqlite3", configuration.Database)
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

func (db *Database) GetLastContentForURL(ctx context.Context, url string) (int64, []byte, error) {
	row := db.db.QueryRowContext(ctx, "SELECT ID, LAST_CONTENT FROM WATCHES WHERE URL=?", url)
	var last_content []byte
	var id int64
	err := row.Scan(&id, &last_content)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return -1, nil, ErrNotFound
		}
		return -1, nil, fmt.Errorf("error on select last content: %w", err)
	}
	if err := row.Err(); err != nil {
		return -1, nil, fmt.Errorf("error on close last content: %w", err)
	}
	return id, last_content, nil
}

func (db *Database) SetLastContentForID(ctx context.Context, id int64, url string, content []byte) error {
	var err error
	if id > 0 {
		_, err = db.db.Exec("INSERT OR REPLACE INTO WATCHES(ID, URL, LAST_FETCH, LAST_CONTENT) VALUES(?,?,CURRENT_TIMESTAMP,?);", id, url, content)
	} else {
		// no id == mew emtry. So omit the ID from the update to auto generate a new one
		_, err = db.db.Exec("INSERT OR REPLACE INTO WATCHES(URL, LAST_FETCH, LAST_CONTENT) VALUES(?,CURRENT_TIMESTAMP,?);", url, content)
	}
	if err != nil {
		return fmt.Errorf("error on insert/update: %w", err)
	}
	return nil
}

// removes old feeds from database
func (db *Database) CleanupDatabase(ctx context.Context, log logger.Logger, c config.Configuration) error {
	configURLs := make(map[string]bool)
	for _, x := range c.Watches {
		configURLs[x.URL] = x.Disabled
	}

	rows, err := db.db.QueryContext(ctx, "SELECT ID, URL FROM WATCHES ORDER BY ID DESC")
	if err != nil {
		// empty database, just return
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("error on select: %w", err)
	}
	defer rows.Close()

	databaseRows := make(map[int64]string)
	for rows.Next() {
		var id int64
		var url string
		if err := rows.Scan(&id, &url); err != nil {
			return fmt.Errorf("error on row scan: %w", err)
		}
		databaseRows[id] = url
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error on rows: %w", err)
	}

	for dbID, url := range databaseRows {
		disabled, ok := configURLs[url]
		// remove entries not present in config anymore and disabled items
		if !ok || disabled {
			log.Infof("Removing entry %s from database", url)
			if _, err := db.db.ExecContext(ctx, "DELETE FROM WATCHES WHERE ID = ?", dbID); err != nil {
				return fmt.Errorf("error on delete: %w", err)
			}
			continue
		}
	}

	return nil
}
