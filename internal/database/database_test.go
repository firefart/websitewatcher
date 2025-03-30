package database_test

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/firefart/websitewatcher/internal/config"
	"github.com/firefart/websitewatcher/internal/database"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Parallel()

	file, err := os.CreateTemp(t.TempDir(), "*.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer func(name string) {
		err := os.Remove(name)
		if err != nil {
			require.NoError(t, err)
		}
	}(file.Name())

	configuration := config.Configuration{
		Database: file.Name(),
	}
	db, err := database.New(t.Context(), configuration, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	err = db.Close(1 * time.Second)
	require.NoError(t, err)
}

func TestInsertAndGetLastContent(t *testing.T) {
	t.Parallel()

	file, err := os.CreateTemp(t.TempDir(), "*.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer func(name string) {
		err := os.Remove(name)
		if err != nil {
			require.NoError(t, err)
		}
	}(file.Name())

	configuration := config.Configuration{
		Database: file.Name(),
	}
	db, err := database.New(t.Context(), configuration, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	defer func(db *database.Database, timeout time.Duration) {
		err := db.Close(timeout)
		if err != nil {
			require.NoError(t, err)
		}
	}(db, 1*time.Second)

	name := "Test"
	url := "https://google.com"
	content := []byte("test")

	watchID, err := db.InsertWatch(t.Context(), name, url, content)
	require.NoError(t, err)
	require.Positive(t, watchID)

	id, lastFetch, lastContent, err := db.GetLastContent(t.Context(), name, url)
	require.NoError(t, err)
	require.Equal(t, content, lastContent)
	require.Equal(t, watchID, id)
	require.WithinDuration(t, time.Now(), lastFetch, 10*time.Second)
}

func TestUpdateLastContent(t *testing.T) {
	t.Parallel()

	file, err := os.CreateTemp(t.TempDir(), "*.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer func(name string) {
		err := os.Remove(name)
		if err != nil {
			require.NoError(t, err)
		}
	}(file.Name())

	configuration := config.Configuration{
		Database: file.Name(),
	}
	db, err := database.New(t.Context(), configuration, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	defer func(db *database.Database, timeout time.Duration) {
		err := db.Close(timeout)
		if err != nil {
			require.NoError(t, err)
		}
	}(db, 1*time.Second)

	name := "Test"
	url := "https://google.com"
	content := []byte("test")
	newContent := []byte("firefart.at")

	watchID, err := db.InsertWatch(t.Context(), name, url, content)
	require.NoError(t, err)
	require.Positive(t, watchID)

	err = db.UpdateLastContent(t.Context(), watchID, newContent)
	require.NoError(t, err)

	id, lastFetch, lastContent, err := db.GetLastContent(t.Context(), name, url)
	require.NoError(t, err)
	require.Equal(t, newContent, lastContent)
	require.Equal(t, watchID, id)
	require.WithinDuration(t, time.Now(), lastFetch, 10*time.Second)
}

func TestPrepareDatabase(t *testing.T) {
	t.Parallel()

	file, err := os.CreateTemp(t.TempDir(), "*.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer func(name string) {
		err := os.Remove(name)
		if err != nil {
			require.NoError(t, err)
		}
	}(file.Name())

	configuration := config.Configuration{
		Database: file.Name(),
		Watches: []config.WatchConfig{
			{
				Name: "New",
				URL:  "New",
			},
		},
	}
	db, err := database.New(t.Context(), configuration, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	defer func(db *database.Database, timeout time.Duration) {
		err := db.Close(timeout)
		if err != nil {
			require.NoError(t, err)
		}
	}(db, 1*time.Second)

	name := "Test"
	url := "https://google.com"
	content := []byte("test")

	watchID, err := db.InsertWatch(t.Context(), name, url, content)
	require.NoError(t, err)
	require.Positive(t, watchID)

	newWatches, deletedEntries, err := db.PrepareDatabase(t.Context(), configuration)
	require.NoError(t, err)
	require.Equal(t, 1, deletedEntries)
	require.Len(t, newWatches, 1)
	require.Equal(t, "New", newWatches[0].Name)
	require.Equal(t, "New", newWatches[0].URL)
}
