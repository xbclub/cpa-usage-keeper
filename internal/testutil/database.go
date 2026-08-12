package testutil

import (
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const testDatabaseURLEnv = "DATABASE_URL"

// testingTB is a minimal interface shared by *testing.T and *testing.B.
type testingTB interface {
	Helper()
	Fatalf(format string, args ...any)
	Cleanup(f func())
}

// OpenTestDatabase creates an isolated PG schema for the test, runs
// AutoMigrate inside it, and registers t.Cleanup to drop the schema.
func OpenTestDatabase(t *testing.T) *gorm.DB {
	return openTestDatabaseCore(t)
}

// OpenTestDatabaseForB creates an isolated PG schema for benchmarks.
func OpenTestDatabaseForB(b *testing.B) *gorm.DB {
	return openTestDatabaseCore(b)
}

// OpenTestDatabaseURL creates an isolated PG schema (like OpenTestDatabase),
// runs AutoMigrate, and returns the schema-pinned DATABASE_URL so callers can
// hand it to app config (e.g. NewWithConfig startup tests where the app opens
// its own pool into the isolated schema instead of the shared production one).
func OpenTestDatabaseURL(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv(testDatabaseURLEnv)
	if dsn == "" {
		t.Fatalf("%s is required for database tests", testDatabaseURLEnv)
	}
	bootstrap, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		NowFunc: func() time.Time { return timeutil.NormalizeStorageTime(time.Now()) },
	})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	schemaName := fmt.Sprintf("test_%d", rand.Int63())
	if err := bootstrap.Exec(fmt.Sprintf(`CREATE SCHEMA "%s"`, schemaName)).Error; err != nil {
		t.Fatalf("create test schema %s: %v", schemaName, err)
	}
	if sqlDB, err := bootstrap.DB(); err == nil {
		_ = sqlDB.Close()
	}
	testDSN := pinSchemaInDSN(dsn, schemaName)
	migrateDB, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{
		NowFunc: func() time.Time { return timeutil.NormalizeStorageTime(time.Now()) },
	})
	if err != nil {
		t.Fatalf("open migrate database: %v", err)
	}
	if err := migrateDB.AutoMigrate(entities.All()...); err != nil {
		t.Fatalf("auto migrate test schema %s: %v", schemaName, err)
	}
	if sqlDB, err := migrateDB.DB(); err == nil {
		_ = sqlDB.Close()
	}
	t.Cleanup(func() {
		dropDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
			NowFunc: func() time.Time { return timeutil.NormalizeStorageTime(time.Now()) },
		})
		if err == nil {
			_ = dropDB.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schemaName)).Error
			if dropSQL, e := dropDB.DB(); e == nil {
				_ = dropSQL.Close()
			}
		}
	})
	return testDSN
}

// openTestDatabaseCore creates an isolated PG schema for each test, runs
// AutoMigrate inside it, and registers a cleanup to drop the schema.
//
// The search_path is injected via the DSN options parameter (not SET) so that
// every pooled connection adopts it — concurrent tests that hit separate pool
// connections would otherwise land on the public schema and miss the seeded rows.
// See AGENTS.md Step 4.8 for the caveat this addresses.
func openTestDatabaseCore(tb testingTB) *gorm.DB {
	tb.Helper()

	dsn := os.Getenv(testDatabaseURLEnv)
	if dsn == "" {
		tb.Fatalf("%s is required for database tests", testDatabaseURLEnv)
	}

	// First open a raw connection just to create the isolated schema.
	bootstrap, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		NowFunc: func() time.Time { return timeutil.NormalizeStorageTime(time.Now()) },
	})
	if err != nil {
		tb.Fatalf("open test database: %v", err)
	}
	schemaName := fmt.Sprintf("test_%d", rand.Int63())
	if err := bootstrap.Exec(fmt.Sprintf(`CREATE SCHEMA "%s"`, schemaName)).Error; err != nil {
		tb.Fatalf("create test schema %s: %v", schemaName, err)
	}
	if sqlDB, err := bootstrap.DB(); err == nil {
		_ = sqlDB.Close()
	}

	// Reopen with the schema pinned via DSN options so all pooled connections
	// inherit the search_path (SET search_path is session-scoped and won't reach
	// connections handed out by the pool that weren't the one that ran SET).
	testDSN := pinSchemaInDSN(dsn, schemaName)
	db, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{
		NowFunc: func() time.Time { return timeutil.NormalizeStorageTime(time.Now()) },
	})
	if err != nil {
		tb.Fatalf("open test database with schema %s: %v", schemaName, err)
	}

	if err := db.AutoMigrate(entities.All()...); err != nil {
		tb.Fatalf("auto migrate test schema %s: %v", schemaName, err)
	}

	tb.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
		dropDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
			NowFunc: func() time.Time { return timeutil.NormalizeStorageTime(time.Now()) },
		})
		if err == nil {
			_ = dropDB.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schemaName)).Error
			if dropSQL, e := dropDB.DB(); e == nil {
				_ = dropSQL.Close()
			}
		}
	})

	return db
}

// pinSchemaInDSN appends (or merges into) a `options=--search_path=<schema>`
// query parameter so that every connection drawn from the pool adopts the
// isolated test schema regardless of which backend session it lands on.
func pinSchemaInDSN(dsn string, schema string) string {
	option := "--search_path=" + schema
	separator := "&"
	if !containsQuery(dsn) {
		separator = "?"
	}
	return dsn + separator + "options=" + urlEncode(option)
}

func containsQuery(dsn string) bool {
	for i := 0; i < len(dsn); i++ {
		if dsn[i] == '?' {
			return true
		}
	}
	return false
}

func urlEncode(s string) string {
	// lib/pq accepts URL-encoded options; spaces and = are the only chars
	// in --search_path=test_xxx that need escaping in practice.
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' {
			b = append(b, '+')
			continue
		}
		b = append(b, c)
	}
	return string(b)
}
