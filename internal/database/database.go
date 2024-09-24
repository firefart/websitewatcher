package database

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"

	"github.com/firefart/websitewatcher/internal/config"
	"github.com/firefart/websitewatcher/internal/database/sqlc"
	"github.com/pressly/goose/v3"

	// use the sqlite implementation
	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("url not found in database")

//go:embed migrations/*.sql
var embedMigrations embed.FS

type Interface interface {
	Close() error
	GetLastContent(ctx context.Context, name, url string) (int64, []byte, error)
	InsertLastContent(ctx context.Context, name, url string, content []byte) (int64, error)
	UpdateLastContent(ctx context.Context, id int64, content []byte) error
	PrepareDatabase(ctx context.Context, c config.Configuration) ([]config.WatchConfig, int, error)
}

type Database struct {
	reader    *sqlc.Queries
	writer    *sqlc.Queries
	readerRAW *sql.DB
	writerRAW *sql.DB
}

// compile time check that struct implements the interface
var _ Interface = (*Database)(nil)

func New(ctx context.Context, configuration config.Configuration, logger *slog.Logger) (*Database, error) {
	if strings.ToLower(configuration.Database) == ":memory:" {
		// not possible because of the two db instances, with in memory they
		// would be separate instances
		return nil, fmt.Errorf("in memory databases are not supported")
	}

	reader, err := newDatabase(ctx, configuration, logger, true)
	if err != nil {
		return nil, fmt.Errorf("could not create reader: %w", err)
	}
	reader.SetMaxOpenConns(100)
	// no migrations on the second connection
	writer, err := newDatabase(ctx, configuration, logger, false)
	if err != nil {
		return nil, fmt.Errorf("could not create writer: %w", err)
	}
	// only one writer connection as there can only be one
	writer.SetMaxOpenConns(1)
	writer.SetMaxIdleConns(1)

	return &Database{
		reader:    sqlc.New(reader),
		writer:    sqlc.New(writer),
		readerRAW: reader,
		writerRAW: writer,
	}, nil
}

func newDatabase(ctx context.Context, configuration config.Configuration, logger *slog.Logger, skipMigrations bool) (*sql.DB, error) {
	db, err := sql.Open("sqlite", fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", configuration.Database))
	if err != nil {
		return nil, fmt.Errorf("could not open database %s: %w", configuration.Database, err)
	}

	// we have a reader and a writer so no need to apply all migrations twice
	if !skipMigrations {
		migrationFS, err := fs.Sub(embedMigrations, "migrations")
		if err != nil {
			return nil, fmt.Errorf("could not sub migration fs: %w", err)
		}

		prov, err := goose.NewProvider("sqlite3", db, migrationFS)
		if err != nil {
			return nil, fmt.Errorf("could not create goose provider: %w", err)
		}

		result, err := prov.Up(ctx)
		if err != nil {
			return nil, fmt.Errorf("could not apply migrations: %w", err)
		}

		for _, r := range result {
			if r.Error != nil {
				return nil, fmt.Errorf("could not apply migration %s: %w", r.Source.Path, r.Error)
			}
		}

		if len(result) > 0 {
			logger.Info(fmt.Sprintf("Applied %d database migrations", len(result)))
		}

		version, err := prov.GetDBVersion(ctx)
		if err != nil {
			return nil, fmt.Errorf("could not get current database version: %w", err)
		}
		logger.Info("Database setup", slog.Int64("version", version))
	}

	// shrink and defrag the database (must be run before the checkpoint)
	if _, err := db.Exec("VACUUM;"); err != nil {
		return nil, fmt.Errorf("could not vacuum: %w", err)
	}

	// truncate the wal file
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE);"); err != nil {
		return nil, fmt.Errorf("could not truncate wal: %w", err)
	}

	// set synchronous mode to normal as it's recommended for WAL
	if _, err := db.Exec("PRAGMA synchronous(NORMAL);"); err != nil {
		return nil, fmt.Errorf("could not set synchronous: %w", err)
	}

	return db, nil
}

func (db *Database) Close() error {
	err1 := db.writerRAW.Close()
	err2 := db.readerRAW.Close()
	return errors.Join(err1, err2)
}

func (db *Database) GetLastContent(ctx context.Context, name, url string) (int64, []byte, error) {
	watch, err := db.reader.GetWatchByNameAndUrl(ctx, sqlc.GetWatchByNameAndUrlParams{
		Name: name,
		Url:  url,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return -1, nil, ErrNotFound
		}
		return -1, nil, err
	}
	return watch.ID, watch.LastContent, nil
}

func (db *Database) InsertLastContent(ctx context.Context, name, url string, content []byte) (int64, error) {
	res, err := db.writer.InsertWatch(ctx, sqlc.InsertWatchParams{
		Name:        name,
		Url:         url,
		LastContent: content,
	})
	if err != nil {
		return -1, fmt.Errorf("error on insert: %w", err)
	}
	return res.ID, nil
}

func (db *Database) UpdateLastContent(ctx context.Context, id int64, content []byte) error {
	_, err := db.writer.UpdateWatch(ctx, sqlc.UpdateWatchParams{
		LastContent: content,
		ID:          id,
	})
	if err != nil {
		return fmt.Errorf("error on update: %w", err)
	}
	return nil
}

// PrepareDatabase cleans up old entries and returns new ones
func (db *Database) PrepareDatabase(ctx context.Context, c config.Configuration) ([]config.WatchConfig, int, error) {
	var newWatches []config.WatchConfig
	var foundIDs []int64

	for _, c := range c.Watches {
		row, err := db.reader.GetWatchByNameAndUrl(ctx, sqlc.GetWatchByNameAndUrlParams{
			Name: c.Name,
			Url:  c.URL,
		})
		if err != nil {
			// new entry not yet fetched. add to array and continue with the next config entry
			if errors.Is(err, sql.ErrNoRows) {
				newWatches = append(newWatches, c)
				continue
			}
			return nil, -1, fmt.Errorf("error on select: %w", err)
		}
		foundIDs = append(foundIDs, row.ID)
	}

	for _, id := range foundIDs {
		if err := db.writer.DeleteWatch(ctx, id); err != nil {
			return nil, -1, fmt.Errorf("could not delete watch %d: %w", id, err)
		}
	}

	return newWatches, len(foundIDs), nil
}
