package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Store struct {
	db *sql.DB
}

func Open(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, errors.Join(fmt.Errorf("ping database: %w", err), fmt.Errorf("close database after ping failure: %w", closeErr))
		}
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return db, nil
}

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

func newID(prefix string) (string, error) {
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(raw)), nil
}
