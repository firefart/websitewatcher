package database

import (
	"fmt"
	"os"
	"sync"

	"github.com/firefart/websitewatcher/internal/config"
	"github.com/firefart/websitewatcher/internal/pb"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

type Database struct {
	mu sync.RWMutex
	db *pb.Database
}

func (db *Database) GetDatabaseEntry(url string) []byte {
	db.mu.RLock()
	defer db.mu.RUnlock()

	value, ok := db.db.Websites[url]
	if !ok {
		return nil
	}
	return value
}

func (db *Database) SetDatabaseEntry(url string, value []byte) {
	db.mu.Lock()
	defer db.mu.Unlock()

	db.db.Websites[url] = value
}

func (db *Database) SetLastRun(lastRun int64) {
	db.mu.Lock()
	defer db.mu.Unlock()

	db.db.LastRun = lastRun
}

func ReadDatabase(database string) (*Database, error) {
	if _, err := os.Stat(database); os.IsNotExist(err) {
		// create database if needed
		return &Database{
			db: &pb.Database{Websites: make(map[string][]byte)},
		}, nil
	}

	b, err := os.ReadFile(database) // nolint: gosec
	if err != nil {
		return nil, fmt.Errorf("could not read database %s: %w", database, err)
	}

	db := &pb.Database{}
	if err := proto.Unmarshal(b, db); err != nil {
		return nil, fmt.Errorf("could not unmarshal database %s: %w", database, err)
	}
	return &Database{db: db}, nil
}

func (db *Database) SaveDatabase(database string) error {
	b, err := proto.Marshal(db.db)
	if err != nil {
		return fmt.Errorf("could not marshal database %s: %w", database, err)
	}
	if err := os.WriteFile(database, b, 0666); err != nil {
		return fmt.Errorf("could not write database %s: %w", database, err)
	}
	return nil
}

// removes old feeds from database
func (db *Database) CleanupDatabase(log *logrus.Logger, c config.Configuration) {
	configURLs := make(map[string]bool)
	for _, x := range c.Watches {
		configURLs[x.URL] = x.Disabled
	}

	newURLs := make(map[string][]byte)
	for url, content := range db.db.Websites {
		disabled, ok := configURLs[url]
		if !ok || disabled {
			log.Debugf("Removing entry %s from database", url)
			continue
		}
		newURLs[url] = content
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	db.db.Websites = newURLs
}
