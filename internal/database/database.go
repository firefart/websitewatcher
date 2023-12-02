package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/firefart/websitewatcher/internal/config"
	"github.com/firefart/websitewatcher/internal/logger"

	_ "github.com/mattn/go-sqlite3"
)

const create string = `
  CREATE TABLE IF NOT EXISTS WATCHES (
  ID INTEGER NOT NULL PRIMARY KEY,
	NAME TEXT NOT NULL,
	URL TEXT NOT NULL,
  LAST_FETCH DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  LAST_CONTENT BLOB
  );
	CREATE UNIQUE INDEX IF NOT EXISTS IDX_NAME_URL
  ON WATCHES(NAME, URL);
	`

var ErrNotFound = errors.New("url not found in database")

type Database struct {
	db *sql.DB
}

type dbEntry struct {
	id   int64
	name string
	url  string
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

func (db *Database) GetLastContentForURL(ctx context.Context, name, url string) (int64, []byte, error) {
	row := db.db.QueryRowContext(ctx, "SELECT ID, LAST_CONTENT FROM WATCHES WHERE NAME=? AND URL=?", name, url)
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

func (db *Database) SetLastContentForID(ctx context.Context, id int64, name, url string, content []byte) error {
	var err error
	if id > 0 {
		_, err = db.db.Exec("INSERT OR REPLACE INTO WATCHES(ID, NAME, URL, LAST_FETCH, LAST_CONTENT) VALUES(?,?,?,CURRENT_TIMESTAMP,?);", id, name, url, content)
	} else {
		// no id == mew emtry. So omit the ID from the update to auto generate a new one
		_, err = db.db.Exec("INSERT OR REPLACE INTO WATCHES(NAME, URL, LAST_FETCH, LAST_CONTENT) VALUES(?,?,CURRENT_TIMESTAMP,?);", name, url, content)
	}
	if err != nil {
		return fmt.Errorf("error on insert/update: %w", err)
	}
	return nil
}

// removes old feeds from database
func (db *Database) CleanupDatabase(ctx context.Context, log logger.Logger, c config.Configuration) error {
	rows, err := db.db.QueryContext(ctx, "SELECT ID, NAME, URL FROM WATCHES ORDER BY ID DESC")
	if err != nil {
		// empty database, just return
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("error on select: %w", err)
	}
	defer rows.Close()

	var databaseRows []dbEntry
	for rows.Next() {
		var dbWatch dbEntry
		if err := rows.Scan(&dbWatch.id, &dbWatch.name, &dbWatch.url); err != nil {
			return fmt.Errorf("error on row scan: %w", err)
		}
		databaseRows = append(databaseRows, dbWatch)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error on rows: %w", err)
	}

	for _, dbObj := range databaseRows {
		found := false
		for _, configObj := range c.Watches {
			// do not count disabled entries and delete them from the database
			if configObj.Disabled {
				continue
			}

			if configObj.Name == dbObj.name && configObj.URL == dbObj.url {
				found = true
				break
			}
		}
		// remove entries not present in config anymore
		if !found {
			log.Infof("Removing entry %s (%s) from database", dbObj.name, dbObj.url)
			if _, err := db.db.ExecContext(ctx, "DELETE FROM WATCHES WHERE ID = ?", dbObj.id); err != nil {
				return fmt.Errorf("error on delete: %w", err)
			}
			continue
		}
	}

	return nil
}
