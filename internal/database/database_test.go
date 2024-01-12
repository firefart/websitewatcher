package database_test

import (
	"context"
	"testing"

	"github.com/brianvoe/gofakeit/v6"
	"github.com/firefart/websitewatcher/internal/config"
	"github.com/firefart/websitewatcher/internal/database"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	configuration := config.Configuration{
		Database: ":memory:",
	}
	db, err := database.New(configuration)
	require.Nil(t, err)
	err = db.Close()
	require.Nil(t, err)
}

func TestDatabase(t *testing.T) {
	configuration := config.Configuration{
		Database: ":memory:",
	}
	db, err := database.New(configuration)
	require.Nil(t, err)
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("error on database close: %v", err)
		}
	}()

	name := gofakeit.Name()
	url := gofakeit.URL()
	content := []byte(gofakeit.LetterN(20))
	content2 := []byte(gofakeit.LetterN(20))

	ctx := context.Background()
	id, err := db.InsertLastContent(ctx, name, url, content)
	require.Nil(t, err)
	require.Greater(t, id, int64(0))
	id2, dbContent, err := db.GetLastContent(ctx, name, url)
	require.Nil(t, err)
	require.Equal(t, id, id2)
	require.Equal(t, content, dbContent)
	err = db.UpdateLastContent(ctx, id, content2)
	require.Nil(t, err)
	id4, dbContent2, err := db.GetLastContent(ctx, name, url)
	require.Nil(t, err)
	require.Equal(t, id, id4)
	require.Equal(t, content2, dbContent2)
}

func TestPrepareDatabase(t *testing.T) {
	configuration := config.Configuration{
		Database: ":memory:",
	}
	db, err := database.New(configuration)
	require.Nil(t, err)
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("error on database close: %v", err)
		}
	}()

	ctx := context.Background()
	numberOfDummyEntries := 100

	// insert random data
	for i := 0; i < numberOfDummyEntries; i++ {
		id, err := db.InsertLastContent(ctx, gofakeit.Name(), gofakeit.URL(), []byte(gofakeit.LetterN(20)))
		require.Nil(t, err)
		require.Greater(t, id, int64(0))
	}

	// add a valid entry
	name := gofakeit.Name()
	url := gofakeit.URL()
	validID, err := db.InsertLastContent(ctx, name, url, []byte(gofakeit.LetterN(20)))
	require.Nil(t, err)
	require.Greater(t, validID, int64(0))

	configuration.Watches = make([]config.WatchConfig, 3)
	configuration.Watches[0].Name = name
	configuration.Watches[0].URL = url
	// new entry
	newName := gofakeit.Name()
	newURL := gofakeit.URL()
	configuration.Watches[1].Name = newName
	configuration.Watches[1].URL = newURL
	newName2 := gofakeit.Name()
	newURL2 := gofakeit.URL()
	configuration.Watches[2].Name = newName2
	configuration.Watches[2].URL = newURL2

	returnedConfig, deletedRows, err := db.PrepareDatabase(ctx, configuration)
	require.Nil(t, err)
	require.Len(t, returnedConfig, 2)
	require.Equal(t, deletedRows, int64(numberOfDummyEntries))
	require.Equal(t, returnedConfig[0].Name, newName)
	require.Equal(t, returnedConfig[0].URL, newURL)
	require.Equal(t, returnedConfig[1].Name, newName2)
	require.Equal(t, returnedConfig[1].URL, newURL2)
}
