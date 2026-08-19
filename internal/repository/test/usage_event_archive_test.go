package test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"

	"gorm.io/gorm"
)

type pgTableColumn struct {
	Name       string         `gorm:"column:column_name"`
	Type       string         `gorm:"column:data_type"`
	NotNull    string         `gorm:"column:is_nullable"`
	DefaultSQL sql.NullString `gorm:"column:column_default"`
}

// loadPGTableColumns 用 PG 目录(information_schema)读列定义,替代上游的 SQLite PRAGMA 内省。
// 必须按 current_schema() 过滤(Step 4.23 #4:测试库累积 test_* schema,跨 schema 同名表会污染)。
func loadPGTableColumns(t *testing.T, db *gorm.DB, table string) []pgTableColumn {
	t.Helper()
	var columns []pgTableColumn
	if err := db.Raw(`SELECT column_name, data_type, is_nullable, column_default
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = ?
		ORDER BY ordinal_position`, table).Scan(&columns).Error; err != nil {
		t.Fatalf("load %s columns: %v", table, err)
	}
	return columns
}

func TestUsageEventArchiveSchemaMatchesHotColumnsWithoutSecondaryIndexes(t *testing.T) {
	db := openTestDatabase(t)

	if !db.Migrator().HasTable("usage_events_archive") {
		t.Fatal("expected fresh database to create usage_events_archive")
	}
	hotColumns := loadPGTableColumns(t, db, "usage_events")
	archiveColumns := loadPGTableColumns(t, db, "usage_events_archive")
	if len(hotColumns) == 0 || len(archiveColumns) == 0 {
		t.Fatalf("expected both tables to have columns, hot=%d archive=%d", len(hotColumns), len(archiveColumns))
	}
	if len(hotColumns) != len(archiveColumns) {
		t.Fatalf("usage event archive schema column count mismatch: hot=%d archive=%d", len(hotColumns), len(archiveColumns))
	}
	hotColumnNames := make([]string, 0, len(hotColumns))
	archiveColumnNames := make([]string, 0, len(archiveColumns))
	for index, column := range hotColumns {
		hotColumnNames = append(hotColumnNames, column.Name)
		archiveColumnNames = append(archiveColumnNames, archiveColumns[index].Name)
		if column.Name != archiveColumns[index].Name {
			t.Fatalf("usage event archive schema mismatch at %d: hot=%s archive=%s", index, column.Name, archiveColumns[index].Name)
		}
	}
	storageColumnNames := strings.Split(strings.ReplaceAll(entities.UsageEventStorageColumns, " ", ""), ",")
	if fmt.Sprint(storageColumnNames) != fmt.Sprint(hotColumnNames) {
		t.Fatalf("usage event archive copy columns mismatch: copy=%v schema=%v", storageColumnNames, hotColumnNames)
	}

	// PG 语义:archive 主键不得用 identity/serial 自增默认值(上游断言无 AUTOINCREMENT 的等价物)。
	var archiveIDDefault sql.NullString
	if err := db.Raw(`SELECT column_default FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'usage_events_archive' AND column_name = 'id'`).Scan(&archiveIDDefault).Error; err != nil {
		t.Fatalf("load archive id default: %v", err)
	}
	if archiveIDDefault.Valid && strings.Contains(strings.ToUpper(archiveIDDefault.String), "NEXTVAL") {
		t.Fatalf("expected archive primary key not to use a serial default, got %s", archiveIDDefault.String)
	}

	// archive 表不得有二级索引(PG 目录按 current_schema 过滤,Step 4.23 #4)。
	var archiveIndexes []string
	if err := db.Raw(`SELECT indexname FROM pg_indexes
		WHERE schemaname = current_schema() AND tablename = 'usage_events_archive'
		ORDER BY indexname`).Scan(&archiveIndexes).Error; err != nil {
		t.Fatalf("load usage_events_archive indexes: %v", err)
	}
	for _, indexName := range archiveIndexes {
		if strings.HasSuffix(indexName, "_pkey") {
			continue
		}
		t.Fatalf("expected archive to have no secondary indexes, got %v", archiveIndexes)
	}
}

func TestArchiveExpiredUsageEventsPreservesOriginalRowAndHotSequence(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 7, 30, 4, 30, 0, 0, time.Local)
	generate := false
	clientIP := "203.0.113.10"
	ttft := int64(321)
	events := []entities.UsageEvent{
		{
			EventKey: "archive-me", APIGroupKey: "group-a", Provider: "openai", Endpoint: "/v1/responses",
			AuthType: "oauth", RequestID: "request-a", ClientIP: &clientIP, Model: "gpt-5", ReasoningEffort: "high",
			ServiceTier: "priority", ResponseServiceTier: "priority", ExecutorType: "codex", Timestamp: now.AddDate(0, 0, -91),
			Source: "auth-a", AuthIndex: "auth-a", Failed: true, Generate: &generate, LatencyMS: 999, TTFTMS: &ttft,
			InputTokens: 10, OutputTokens: 20, ReasoningTokens: 5, CachedTokens: 4, CacheReadTokens: 3, CacheCreationTokens: 2, TotalTokens: 35,
		},
		{EventKey: "recent", Model: "gpt-5", Timestamp: now.Add(-time.Hour), TotalTokens: 1},
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("seed usage events: %v", err)
	}
	var original entities.UsageEvent
	if err := db.Where("event_key = ?", "archive-me").Take(&original).Error; err != nil {
		t.Fatalf("load original usage event: %v", err)
	}
	previousMaxID := eventsMaxID(t, db)
	seedArchiveCaughtUpCheckpoints(t, db, previousMaxID, now)

	result, err := repository.CleanupStorage(db, now)
	if err != nil {
		t.Fatalf("CleanupStorage returned error: %v", err)
	}
	if result.UsageEventsArchived != 1 {
		t.Fatalf("expected one archived usage event, got %+v", result)
	}

	var archived entities.UsageEventArchive
	if err := db.Where("id = ?", original.ID).Take(&archived).Error; err != nil {
		t.Fatalf("load archived usage event: %v", err)
	}
	if archived.ID != original.ID || archived.EventKey != original.EventKey || archived.RequestID != original.RequestID || archived.TotalTokens != original.TotalTokens {
		t.Fatalf("archive row did not preserve original values: original=%+v archive=%+v", original, archived)
	}
	var oldHotCount int64
	if err := db.Model(&entities.UsageEvent{}).Where("id = ?", original.ID).Count(&oldHotCount).Error; err != nil {
		t.Fatalf("count archived hot event: %v", err)
	}
	if oldHotCount != 0 {
		t.Fatalf("expected archived event to leave hot table, got %d", oldHotCount)
	}

	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{EventKey: "after-archive", Timestamp: now, TotalTokens: 2}}); err != nil {
		t.Fatalf("insert event after archive: %v", err)
	}
	var insertedAfter entities.UsageEvent
	if err := db.Where("event_key = ?", "after-archive").Take(&insertedAfter).Error; err != nil {
		t.Fatalf("load event inserted after archive: %v", err)
	}
	if insertedAfter.ID <= previousMaxID {
		t.Fatalf("expected hot AUTOINCREMENT sequence to continue after archive, got id %d", insertedAfter.ID)
	}
}

func TestArchiveExpiredUsageEventsRollsBackOnArchivePrimaryKeyConflict(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 7, 30, 4, 30, 0, 0, time.Local)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{EventKey: "hot-old", Timestamp: now.AddDate(0, 0, -91), TotalTokens: 9}}); err != nil {
		t.Fatalf("seed old usage event: %v", err)
	}
	var hot entities.UsageEvent
	if err := db.Where("event_key = ?", "hot-old").Take(&hot).Error; err != nil {
		t.Fatalf("load hot usage event: %v", err)
	}
	seedArchiveCaughtUpCheckpoints(t, db, hot.ID, now)
	if err := db.Create(&entities.UsageEventArchive{ID: hot.ID, EventKey: "existing-archive", Timestamp: hot.Timestamp, TotalTokens: 1}).Error; err != nil {
		t.Fatalf("seed conflicting archive row: %v", err)
	}

	if _, err := repository.ArchiveExpiredUsageEvents(context.Background(), db, now); err == nil {
		t.Fatal("expected archive primary key conflict to fail")
	}
	var hotCount int64
	if err := db.Model(&entities.UsageEvent{}).Where("id = ?", hot.ID).Count(&hotCount).Error; err != nil {
		t.Fatalf("count hot row after conflict: %v", err)
	}
	if hotCount != 1 {
		t.Fatalf("expected hot row to remain after archive conflict, got %d", hotCount)
	}
	var archiveKey string
	if err := db.Model(&entities.UsageEventArchive{}).Where("id = ?", hot.ID).Pluck("event_key", &archiveKey).Error; err != nil {
		t.Fatalf("load archive row after conflict: %v", err)
	}
	if archiveKey != "existing-archive" {
		t.Fatalf("expected existing archive row to remain unchanged, got %q", archiveKey)
	}
}

func TestArchiveExpiredUsageEventsProcessesMoreThanOneBatch(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 7, 30, 4, 30, 0, 0, time.Local)
	events := make([]entities.UsageEvent, 5001)
	for index := range events {
		events[index] = entities.UsageEvent{
			EventKey:    fmt.Sprintf("old-%05d", index),
			Timestamp:   now.AddDate(0, 0, -91).Add(time.Duration(index) * time.Nanosecond),
			TotalTokens: int64(index + 1),
		}
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("seed multi-batch usage events: %v", err)
	}
	seedArchiveCaughtUpCheckpoints(t, db, eventsMaxID(t, db), now)

	archiveResult, err := repository.ArchiveExpiredUsageEvents(context.Background(), db, now)
	if err != nil {
		t.Fatalf("ArchiveExpiredUsageEvents returned error: %v", err)
	}
	if archiveResult.Archived != int64(len(events)) {
		t.Fatalf("expected %d archived rows, got %+v", len(events), archiveResult)
	}
	var hotCount int64
	if err := db.Model(&entities.UsageEvent{}).Count(&hotCount).Error; err != nil {
		t.Fatalf("count hot rows after multi-batch archive: %v", err)
	}
	if hotCount != 0 {
		t.Fatalf("expected no hot rows after multi-batch archive, got %d", hotCount)
	}
	var archiveCount int64
	if err := db.Model(&entities.UsageEventArchive{}).Count(&archiveCount).Error; err != nil {
		t.Fatalf("count archive rows after multi-batch archive: %v", err)
	}
	if archiveCount != int64(len(events)) {
		t.Fatalf("expected %d archive rows, got %d", len(events), archiveCount)
	}
}

func eventsMaxID(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var maxID int64
	if err := db.Model(&entities.UsageEvent{}).Select("COALESCE(MAX(id), 0)").Scan(&maxID).Error; err != nil {
		t.Fatalf("load max usage event id: %v", err)
	}
	return maxID
}

func seedArchiveCaughtUpCheckpoints(t *testing.T, db *gorm.DB, maxID int64, now time.Time) {
	t.Helper()
	rows := []entities.UsageAggregationCheckpoint{
		{Name: entities.UsageAggregationCheckpointOverview, LastAggregatedUsageEventID: maxID, CreatedAt: now, UpdatedAt: now},
		{Name: entities.UsageAggregationCheckpointActivity, LastAggregatedUsageEventID: maxID, CreatedAt: now, UpdatedAt: now},
		{Name: entities.UsageAggregationCheckpointLatency, LastAggregatedUsageEventID: maxID, CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed archive cleanup checkpoints: %v", err)
	}
}
