package testutil

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	cloudplanestore "mini-cloud/internal/cloudplane/infra/store"
	cloudplanemigrations "mini-cloud/internal/cloudplane/infra/store/migrations"
	controlplanestore "mini-cloud/internal/controlplane/store"
	controlplanemigrations "mini-cloud/internal/controlplane/store/migrations"
)

const testDatabaseURLEnv = "MINICLOUD_TEST_DATABASE_URL"

type TestDatabase struct {
	DatabaseURL string
	DB          *sql.DB
	Store       *cloudplanestore.Store
}

type ControlPlaneTestDatabase struct {
	DatabaseURL string
	DB          *sql.DB
	Store       *controlplanestore.Store
}

func OpenCloudPlaneTestDatabase(t *testing.T) TestDatabase {
	t.Helper()

	db, testURL := openIsolatedTestDatabase(t, cloudplanestore.Open, cloudplanemigrations.Up)

	return TestDatabase{
		DatabaseURL: testURL,
		DB:          db,
		Store:       cloudplanestore.New(db),
	}
}

func OpenControlPlaneTestDatabase(t *testing.T) ControlPlaneTestDatabase {
	t.Helper()

	db, testURL := openIsolatedTestDatabase(t, controlplanestore.Open, controlplanemigrations.Up)

	return ControlPlaneTestDatabase{
		DatabaseURL: testURL,
		DB:          db,
		Store:       controlplanestore.New(db),
	}
}

func openIsolatedTestDatabase(t *testing.T, openDB func(string) (*sql.DB, error), migrate func(*sql.DB) error) (*sql.DB, string) {
	t.Helper()

	grpcEndpoint := os.Getenv(testDatabaseURLEnv)
	if strings.TrimSpace(grpcEndpoint) == "" {
		t.Skipf("set %s to run Postgres integration tests", testDatabaseURLEnv)
	}

	databaseName, err := randomDatabaseName()
	if err != nil {
		t.Fatalf("generate database name: %v", err)
	}

	adminURL, err := rewriteDatabaseName(grpcEndpoint, "postgres")
	if err != nil {
		t.Fatalf("build admin database url: %v", err)
	}

	adminDB, err := openDB(adminURL)
	if err != nil {
		t.Fatalf("open admin database: %v", err)
	}
	defer func() {
		if err := adminDB.Close(); err != nil {
			t.Logf("close admin database: %v", err)
		}
	}()

	if _, err := adminDB.ExecContext(context.Background(), "CREATE DATABASE "+quoteIdentifier(databaseName)); err != nil {
		t.Fatalf("create test database %s: %v", databaseName, err)
	}

	testURL, err := rewriteDatabaseName(grpcEndpoint, databaseName)
	if err != nil {
		t.Fatalf("build test database url: %v", err)
	}

	db, err := openDB(testURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}

	if err := migrate(db); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			t.Logf("close test database after migration failure: %v", closeErr)
		}
		t.Fatalf("run migrations: %v", err)
	}

	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Logf("close test database: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		cleanupDB, err := openDB(adminURL)
		if err != nil {
			t.Fatalf("reopen admin database during cleanup: %v", err)
		}
		defer func() {
			if err := cleanupDB.Close(); err != nil {
				t.Logf("close cleanup admin database: %v", err)
			}
		}()

		if _, err := cleanupDB.ExecContext(ctx, `
			SELECT pg_terminate_backend(pid)
			FROM pg_stat_activity
			WHERE datname = $1 AND pid <> pg_backend_pid()
		`, databaseName); err != nil {
			t.Fatalf("terminate connections for %s: %v", databaseName, err)
		}

		if _, err := cleanupDB.ExecContext(ctx, "DROP DATABASE IF EXISTS "+quoteIdentifier(databaseName)); err != nil {
			t.Fatalf("drop test database %s: %v", databaseName, err)
		}
	})

	return db, testURL
}

func randomDatabaseName() (string, error) {
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return "mini_cloud_test_" + hex.EncodeToString(raw), nil
}

func rewriteDatabaseName(rawURL string, databaseName string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}

	parsed.Path = "/" + databaseName
	return parsed.String(), nil
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
