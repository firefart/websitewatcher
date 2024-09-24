package database

import (
	"context"

	"github.com/firefart/websitewatcher/internal/config"
)

type MockDB struct{}

func NewMockDB() *MockDB {
	mockDB := MockDB{}
	return &mockDB
}

// compile time check that struct implements the interface
var _ Interface = (*MockDB)(nil)

func (*MockDB) Close() error { return nil }

func (*MockDB) GetLastContent(_ context.Context, _, _ string) (int64, []byte, error) {
	return 0, nil, nil
}

func (*MockDB) InsertWatch(_ context.Context, _, _ string, _ []byte) (int64, error) {
	return 0, nil
}

func (*MockDB) UpdateLastContent(_ context.Context, _ int64, _ []byte) error {
	return nil
}

func (*MockDB) PrepareDatabase(_ context.Context, _ config.Configuration) ([]config.WatchConfig, int, error) {
	return nil, 0, nil
}
