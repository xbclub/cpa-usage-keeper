package test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

func TestMarkRedisUsageInboxProcessedBatchMapsKeysAcrossChunks(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load Asia/Shanghai: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })
	db := openTestDatabase(t)
	const rowCount = 601
	processedAt := time.Date(2026, 7, 23, 9, 30, 0, 123456789, location)
	inputs := make([]repositorydto.RedisInboxInsert, 0, rowCount)
	for i := 0; i < rowCount; i++ {
		inputs = append(inputs, repositorydto.RedisInboxInsert{
			Source:     "redis_subscribe:usage",
			RawMessage: fmt.Sprintf(`{"request_id":"batch-map-%d"}`, i),
			PoppedAt:   processedAt.Add(-time.Minute),
		})
	}
	rows, err := repository.InsertRedisUsageInboxMessages(db, inputs)
	if err != nil {
		t.Fatalf("seed redis usage inbox rows: %v", err)
	}
	if err := db.Model(&entities.RedisUsageInbox{}).Where("id > 0").Update("last_error", "stale error").Error; err != nil {
		t.Fatalf("seed stale inbox errors: %v", err)
	}
	oldUpdatedAt := processedAt.Add(-24 * time.Hour)
	if err := db.Model(&entities.RedisUsageInbox{}).Where("id = ?", rows[0].ID).UpdateColumn("updated_at", timeutil.FormatStorageTime(oldUpdatedAt)).Error; err != nil {
		t.Fatalf("seed old updated_at: %v", err)
	}

	updates := make([]repository.RedisUsageInboxProcessedUpdate, 0, len(rows))
	for i, row := range rows {
		updates = append(updates, repository.RedisUsageInboxProcessedUpdate{
			ID:       row.ID,
			EventKey: fmt.Sprintf("event-key-%d", i),
		})
	}
	if err := repository.MarkRedisUsageInboxProcessedBatch(db, updates, processedAt); err != nil {
		t.Fatalf("MarkRedisUsageInboxProcessedBatch returned error: %v", err)
	}

	var stored []entities.RedisUsageInbox
	if err := db.Order("id ASC").Find(&stored).Error; err != nil {
		t.Fatalf("load processed inbox rows: %v", err)
	}
	if len(stored) != rowCount {
		t.Fatalf("expected %d inbox rows, got %d", rowCount, len(stored))
	}
	for i, row := range stored {
		if row.Status != repository.RedisUsageInboxStatusProcessed || row.UsageEventKey != fmt.Sprintf("event-key-%d", i) {
			t.Fatalf("unexpected processed mapping at index %d: %+v", i, row)
		}
		if row.ProcessedAt == nil || !row.ProcessedAt.Equal(processedAt) {
			t.Fatalf("unexpected processed_at at index %d: %+v", i, row.ProcessedAt)
		}
		if row.LastError != "" {
			t.Fatalf("expected last_error cleared at index %d, got %q", i, row.LastError)
		}
		if row.UpdatedAt.IsZero() {
			t.Fatalf("expected updated_at at index %d", i)
		}
	}
	if stored[0].UpdatedAt.Equal(oldUpdatedAt) {
		t.Fatalf("expected batch update to advance updated_at, still %s", stored[0].UpdatedAt)
	}
	for _, field := range []string{"processed_at", "updated_at"} {
		rawValue := rawTimeValue(t, db, "redis_usage_inboxes", field, fmt.Sprintf("id = %d", rows[0].ID))
		assertProjectTimezoneStorageValue(t, rawValue, "redis_usage_inboxes."+field)
		if field == "processed_at" && rawValue != timeutil.FormatStorageTime(processedAt) {
			t.Fatalf("expected processed_at %q, got %q", timeutil.FormatStorageTime(processedAt), rawValue)
		}
	}
}

// rawTimeValue 读取指定表字段的原始字符串时间值，用于校验存储时区格式（PG 适配版，等价于根包 repository.rawTimeValue）。
func rawTimeValue(t *testing.T, db *gorm.DB, table string, field string, where string) string {
	t.Helper()
	var value string
	if err := db.Raw(fmt.Sprintf("SELECT %s FROM %s WHERE %s LIMIT 1", field, table, where)).Scan(&value).Error; err != nil {
		t.Fatalf("read raw time value %s.%s: %v", table, field, err)
	}
	if strings.TrimSpace(value) == "" {
		t.Fatalf("expected raw time value for %s.%s", table, field)
	}
	return value
}

// assertProjectTimezoneStorageValue 校验原始时间值符合项目存储时区（Asia/Shanghai，+08:00）的 RFC3339Nano 格式。
func assertProjectTimezoneStorageValue(t *testing.T, value string, field string) {
	t.Helper()
	if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
		t.Fatalf("expected %s to use RFC3339Nano storage format, got %q: %v", field, value, err)
	}
	if !strings.Contains(value, "T") || !strings.Contains(value, "+08:00") || strings.Contains(value, "Z") || strings.Contains(value, "+00:00") {
		t.Fatalf("expected %s to use project timezone offset storage format, got %q", field, value)
	}
}
