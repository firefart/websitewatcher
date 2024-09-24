package database_test

import (
	"context"
	"io"
	"log"
	"log/slog"
	"os"
	"testing"

	"github.com/firefart/websitewatcher/internal/config"
	"github.com/firefart/websitewatcher/internal/database"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	file, err := os.CreateTemp("", "*.sqlite")
	if err != nil {
		log.Fatal(err)
	}
	defer os.Remove(file.Name())

	configuration := config.Configuration{
		Database: file.Name(),
	}
	db, err := database.New(context.Background(), configuration, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.Nil(t, err)
	err = db.Close()
	require.Nil(t, err)
}
