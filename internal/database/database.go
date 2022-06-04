package database

import (
	"fmt"
	"os"

	"github.com/firefart/websitewatcher/internal/config"
	"github.com/firefart/websitewatcher/internal/pb"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

func NewDatabase() *pb.Database {
	return &pb.Database{Websites: make(map[string][]byte)}
}

func ReadDatabase(database string) (*pb.Database, error) {
	// create database if needed
	if _, err := os.Stat(database); os.IsNotExist(err) {
		return NewDatabase(), nil
	}

	b, err := os.ReadFile(database) // nolint: gosec
	if err != nil {
		return nil, fmt.Errorf("could not read database %s: %v", database, err)
	}

	db := &pb.Database{}
	if err := proto.Unmarshal(b, db); err != nil {
		return nil, fmt.Errorf("could not unmarshal database %s: %v", database, err)
	}
	return db, nil
}

func SaveDatabase(database string, r proto.Message) error {
	b, err := proto.Marshal(r)
	if err != nil {
		return fmt.Errorf("could not marshal database %s: %v", database, err)
	}
	if err := os.WriteFile(database, b, 0666); err != nil {
		return fmt.Errorf("could not write database %s: %v", database, err)
	}
	return nil
}

// removes old feeds from database
func CleanupDatabase(log *logrus.Logger, r *pb.Database, c config.Configuration) {
	configURLs := make(map[string]struct{})
	for _, x := range c.Watches {
		configURLs[x.URL] = struct{}{}
	}

	newURLs := make(map[string][]byte)
	for url, content := range r.Websites {
		_, ok := configURLs[url]
		if !ok {
			log.Debugf("Removing entry %s from database", url)
			continue
		}
		newURLs[url] = content
	}
	r.Websites = newURLs
}
