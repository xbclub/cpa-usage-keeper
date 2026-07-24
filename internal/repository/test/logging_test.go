package test

import (
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/logging"
	"cpa-usage-keeper/internal/testutil"
)

// TestOpenDatabaseRoutesGORMErrorsThroughKeeperLogging 验证 PG 连接通过 Keeper 日志中间件输出 GORM 错误。
// (fork 单 PG 池,无 OpenDatabasePools/读写池分离,故上游的 reader 池测试不适用,已省略。)
func TestOpenDatabaseRoutesGORMErrorsThroughKeeperLogging(t *testing.T) {
	previousStderr := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	os.Stderr = writer
	t.Cleanup(func() {
		os.Stderr = previousStderr
		_ = reader.Close()
		_ = writer.Close()
	})

	logCloser, err := logging.Configure(config.Config{LogLevel: "info"})
	if err != nil {
		t.Fatalf("configure logging: %v", err)
	}
	loggingClosed := false
	t.Cleanup(func() {
		if !loggingClosed {
			_ = logCloser.Close()
		}
	})
	db := testutil.OpenTestDatabase(t)
	db.Logger = logging.NewGORMLogger()
	if execErr := db.Exec("INSERT INTO definitely_missing_table DEFAULT VALUES").Error; execErr == nil {
		t.Fatal("expected missing table query to fail")
	}
	if err := logCloser.Close(); err != nil {
		t.Fatalf("close logging: %v", err)
	}
	loggingClosed = true
	if err := writer.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}

	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	plain := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(string(content), "")
	// PG 错误信息是 "relation ... does not exist",SQLite 是 "no such table";表名出现在被记录的 SQL 中,跨方言通用。
	if !strings.Contains(plain, "| error | gorm query failed |") || !strings.Contains(plain, "definitely_missing_table") {
		t.Fatalf("expected GORM error through Keeper logging, got %q", plain)
	}
}
