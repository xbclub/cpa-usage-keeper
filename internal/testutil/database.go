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

func openTestDatabaseCore(tb testingTB) *gorm.DB {
	tb.Helper()

	dsn := os.Getenv(testDatabaseURLEnv)
	if dsn == "" {
		tb.Fatalf("%s is required for database tests", testDatabaseURLEnv)
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		NowFunc: func() time.Time { return timeutil.NormalizeStorageTime(time.Now()) },
	})
	if err != nil {
		tb.Fatalf("open test database: %v", err)
	}

	schemaName := fmt.Sprintf("test_%d", rand.Int63())

	if err := db.Exec(fmt.Sprintf(`CREATE SCHEMA "%s"`, schemaName)).Error; err != nil {
		tb.Fatalf("create test schema %s: %v", schemaName, err)
	}
	if err := db.Exec(fmt.Sprintf(`SET search_path TO "%s"`, schemaName)).Error; err != nil {
		tb.Fatalf("set search_path to %s: %v", schemaName, err)
	}

	if err := db.AutoMigrate(entities.All()...); err != nil {
		tb.Fatalf("auto migrate test schema %s: %v", schemaName, err)
	}

	tb.Cleanup(func() {
		_ = db.Exec("SET search_path TO public").Error
		_ = db.Exec(fmt.Sprintf(`DROP SCHEMA "%s" CASCADE`, schemaName)).Error
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}
